package tencentcos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tencentyun/cos-go-sdk-v5"
	"yuntools/internal/cloud"
	"yuntools/internal/storage"
)

type progressListener struct {
	mu     sync.Mutex
	stage  string
	notify func(cloud.Progress)
}

func (l *progressListener) ProgressChangedCallback(event *cos.ProgressEvent) {
	if l.notify == nil || event == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.notify(cloud.Progress{
		Completed: event.ConsumedBytes,
		Total:     event.TotalBytes,
		Stage:     l.stage,
	})
}

func (c *Client) transferDir(t cloud.Transfer, direction string) (string, error) {
	if t.Bucket == "" || t.Key == "" || t.LocalPath == "" || t.CheckpointDir == "" {
		return "", errors.New("transfer requires bucket, key, local path and private checkpoint directory")
	}
	path, err := filepath.Abs(t.LocalPath)
	if err != nil {
		return "", err
	}
	binding, _ := json.Marshal([]string{c.account.ID, c.account.Region, direction, t.Bucket, t.Key, path, t.DestinationRoot})
	hash := sha256.Sum256(binding)
	dir := filepath.Join(t.CheckpointDir, hex.EncodeToString(hash[:]))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func (c *Client) Upload(ctx context.Context, t cloud.Transfer, notify func(cloud.Progress)) error {
	dir, err := c.transferDir(t, "upload")
	if err != nil {
		return err
	}
	bc, err := c.bucketClient(t.Bucket)
	if err != nil {
		return err
	}
	stagedPath := filepath.Join(dir, "upload.staged")
	if _, err := os.Stat(stagedPath); errors.Is(err, os.ErrNotExist) {
		id, err := storage.SnapshotFileContext(ctx, t.LocalPath)
		if err != nil {
			return err
		}
		if err := copyFilePrivate(t.LocalPath, stagedPath); err != nil {
			return err
		}
		if err := storage.VerifyFileContext(ctx, stagedPath, id); err != nil {
			_ = os.Remove(stagedPath)
			return err
		}
	} else if err != nil {
		return err
	}
	opt := &cos.MultiUploadOptions{
		OptIni: &cos.InitiateMultipartUploadOptions{
			ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
				Listener: &progressListener{stage: "uploading", notify: notify},
			},
		},
		ThreadPoolSize: 3,
		CheckPoint:     true,
	}
	if _, _, err := bc.Object.Upload(ctx, t.Key, stagedPath, opt); err != nil {
		return err
	}
	_ = os.RemoveAll(dir)
	return nil
}

func (c *Client) Download(ctx context.Context, t cloud.Transfer, notify func(cloud.Progress)) error {
	dir, err := c.transferDir(t, "download")
	if err != nil {
		return err
	}
	target, err := storage.ResolveDestination(t.DestinationRoot, t.Key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	bc, err := c.bucketClient(t.Bucket)
	if err != nil {
		return err
	}
	remote, err := c.Stat(ctx, t.Bucket, t.Key)
	if err != nil {
		return err
	}
	if t.ETag != "" && !strings.EqualFold(strings.Trim(remote.ETag, `"`), strings.Trim(t.ETag, `"`)) {
		return errors.New("remote object ETag changed; restart download")
	}
	if t.Size > 0 && remote.Size != t.Size {
		return errors.New("remote object size changed; restart download")
	}
	// A stable path lets a paused download resume from the same checkpoint.
	tmpPath := filepath.Join(dir, "download.part")
	cpFile := filepath.Join(dir, "download.cp")
	opt := &cos.MultiDownloadOptions{
		Opt: &cos.ObjectGetOptions{
			Listener: &progressListener{stage: "downloading", notify: notify},
		},
		ThreadPoolSize: 3,
		CheckPoint:     true,
		CheckPointFile: cpFile,
	}
	if _, err := bc.Object.Download(ctx, t.Key, tmpPath, opt); err != nil {
		return err
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		return err
	}
	if remote.Size >= 0 && info.Size() != remote.Size {
		return fmt.Errorf("downloaded size mismatch: got %d want %d", info.Size(), remote.Size)
	}
	if err := os.Chmod(tmpPath, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	_ = os.RemoveAll(dir)
	return nil
}

func copyFilePrivate(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

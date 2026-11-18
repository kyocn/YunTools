package qiniukodo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/qiniu/go-sdk/v7/storage"
	"yuntools/internal/cloud"
	yunstorage "yuntools/internal/storage"
)

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

func (c *Client) Upload(ctx context.Context, t cloud.Transfer, notify func(cloud.Progress)) error {
	dir, err := c.transferDir(t, "upload")
	if err != nil {
		return err
	}
	stagedPath := filepath.Join(dir, "upload.staged")
	if _, err := os.Stat(stagedPath); errors.Is(err, os.ErrNotExist) {
		id, err := yunstorage.SnapshotFileContext(ctx, t.LocalPath)
		if err != nil {
			return err
		}
		if err := copyFilePrivate(t.LocalPath, stagedPath); err != nil {
			return err
		}
		if err := yunstorage.VerifyFileContext(ctx, stagedPath, id); err != nil {
			_ = os.Remove(stagedPath)
			return err
		}
	} else if err != nil {
		return err
	}
	info, err := os.Stat(stagedPath)
	if err != nil {
		return err
	}
	total := info.Size()
	policy := storage.PutPolicy{Scope: fmt.Sprintf("%s:%s", t.Bucket, t.Key)}
	upToken := policy.UploadToken(c.mac)

	recorder, err := storage.NewFileRecorder(dir)
	if err != nil {
		return err
	}
	uploader := storage.NewResumeUploaderV2(c.cfg)
	var mu sync.Mutex
	var completed int64
	const partSize int64 = 4 * 1024 * 1024
	extra := &storage.RputV2Extra{
		Recorder: recorder,
		PartSize: partSize,
		Notify: func(partNumber int64, _ *storage.UploadPartsRet) {
			if notify == nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			completed += partSize
			if completed > total {
				completed = total
			}
			notify(cloud.Progress{Completed: completed, Total: total, Stage: "uploading"})
		},
	}
	var ret storage.PutRet
	if err := uploader.PutFile(ctx, &ret, upToken, t.Key, stagedPath, extra); err != nil {
		return err
	}
	if notify != nil {
		notify(cloud.Progress{Completed: total, Total: total, Stage: "uploading"})
	}
	_ = os.RemoveAll(dir)
	return nil
}

func (c *Client) Download(ctx context.Context, t cloud.Transfer, notify func(cloud.Progress)) error {
	dir, err := c.transferDir(t, "download")
	if err != nil {
		return err
	}
	target, err := yunstorage.ResolveDestination(t.DestinationRoot, t.Key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	remote, err := c.Stat(ctx, t.Bucket, t.Key)
	if err != nil {
		return err
	}
	if t.ETag != "" && remote.ETag != "" && remote.ETag != t.ETag {
		return errors.New("remote object ETag changed; restart download")
	}
	if t.Size > 0 && remote.Size != t.Size {
		return errors.New("remote object size changed; restart download")
	}
	out, err := c.bm.Get(t.Bucket, t.Key, &storage.GetObjectInput{Context: ctx})
	if err != nil {
		return err
	}
	defer out.Close()

	nonce := make([]byte, 8)
	_, _ = rand.Read(nonce)
	tmpPath := filepath.Join(filepath.Dir(target), fmt.Sprintf(".yuntools-dl-%s.part", hex.EncodeToString(nonce)))
	defer os.Remove(tmpPath)

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	buf := make([]byte, 256*1024)
	var written int64
	total := remote.Size
	if total <= 0 && out.ContentLength > 0 {
		total = out.ContentLength
	}
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return err
		}
		n, readErr := out.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				f.Close()
				return writeErr
			}
			written += int64(n)
			if notify != nil {
				notify(cloud.Progress{Completed: written, Total: total, Stage: "downloading"})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			return readErr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if remote.Size >= 0 && written != remote.Size {
		return fmt.Errorf("downloaded size mismatch: got %d want %d", written, remote.Size)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	_ = os.RemoveAll(dir)
	return nil
}

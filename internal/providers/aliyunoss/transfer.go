package aliyunoss

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

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"yuntools/internal/cloud"
	"yuntools/internal/storage"
)

type transferIdentity struct {
	Local           *storage.FileIdentity `json:"local,omitempty"`
	ETag, VersionID string
	Size            int64
}

func (c *Client) transferDir(t cloud.Transfer, direction string) (string, error) {
	if t.Bucket == "" || t.Key == "" || t.LocalPath == "" || t.CheckpointDir == "" {
		return "", errors.New("transfer requires bucket, key, local path and private checkpoint directory")
	}
	path, err := filepath.Abs(t.LocalPath)
	if err != nil {
		return "", err
	}
	binding, _ := json.Marshal([]string{c.accountID, c.region, direction, t.Bucket, t.Key, path, t.DestinationRoot})
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
func readIdentity(dir string) (transferIdentity, error) {
	var identity transferIdentity
	b, err := os.ReadFile(filepath.Join(dir, "identity.json"))
	if err != nil {
		return identity, err
	}
	err = json.Unmarshal(b, &identity)
	return identity, err
}
func saveIdentity(dir string, identity transferIdentity) error {
	b, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".identity-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, filepath.Join(dir, "identity.json"))
}

// Callbacks are serialized because SDK multipart workers can report concurrently.
func progressCallback(stage string, notify func(cloud.Progress)) oss.ProgressFunc {
	var mu sync.Mutex
	return func(_ int64, completed, total int64) {
		if notify == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		notify(cloud.Progress{Completed: completed, Total: total, Stage: stage})
	}
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func snapshotSource(ctx context.Context, source, dest string, identity storage.FileIdentity) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), contextReader{ctx, in})
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n != identity.Size || hex.EncodeToString(h.Sum(nil)) != identity.SHA256 {
		return errors.New("source changed while preparing upload")
	}
	return storage.VerifyFileContext(ctx, source, identity)
}
func (c *Client) Upload(ctx context.Context, t cloud.Transfer, notify func(cloud.Progress)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := c.transferDir(t, "upload")
	if err != nil {
		return err
	}
	saved, err := readIdentity(dir)
	stage := filepath.Join(dir, "upload.data")
	if os.IsNotExist(err) {
		id, e := storage.SnapshotFileContext(ctx, t.LocalPath)
		if e != nil {
			return e
		}
		if t.Size > 0 && id.Size != t.Size {
			return errors.New("upload source size changed")
		}
		if !t.Modified.IsZero() && !t.Modified.Equal(id.Modified) {
			return errors.New("upload source modification time changed")
		}
		// Read an immutable private snapshot so edits during multipart upload cannot
		// produce mixed old/new parts. A paused job still checks the original file.
		if err = snapshotSource(ctx, t.LocalPath, stage, id); err != nil {
			return err
		}
		saved = transferIdentity{Local: &id, Size: id.Size}
		if err = saveIdentity(dir, saved); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		if saved.Local == nil {
			return errors.New("invalid upload checkpoint identity")
		}
		if err = storage.VerifyFileContext(ctx, t.LocalPath, *saved.Local); err != nil {
			return err
		}
		staged, e := storage.SnapshotFileContext(ctx, stage)
		if e != nil {
			return e
		}
		if staged.Size != saved.Local.Size || staged.SHA256 != saved.Local.SHA256 {
			return errors.New("upload checkpoint data changed")
		}
	}
	req := &oss.PutObjectRequest{Bucket: &t.Bucket, Key: &t.Key, ProgressFn: progressCallback("上传", notify)}
	_, err = oss.NewUploader(c.sdk, func(o *oss.UploaderOptions) {
		o.EnableCheckpoint = true
		o.CheckpointDir = dir + string(os.PathSeparator)
		o.LeavePartsOnError = true
	}).UploadFile(ctx, req, stage)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
func (c *Client) Download(ctx context.Context, t cloud.Transfer, notify func(cloud.Progress)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, relative, err := openDownloadRoot(t.LocalPath, t.DestinationRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	dir, err := c.transferDir(t, "download")
	if err != nil {
		return err
	}
	saved, err := readIdentity(dir)
	if os.IsNotExist(err) {
		head, e := c.sdk.HeadObject(ctx, &oss.HeadObjectRequest{Bucket: &t.Bucket, Key: &t.Key, VersionId: optional(t.VersionID), IfMatch: optional(t.ETag)})
		if e != nil {
			return e
		}
		saved = transferIdentity{ETag: value(head.ETag), VersionID: value(head.VersionId), Size: head.ContentLength}
		if saved.ETag == "" {
			return errors.New("download requires a remote source identity")
		}
		if t.ETag != "" && t.ETag != saved.ETag || t.VersionID != "" && t.VersionID != saved.VersionID || t.Size > 0 && t.Size != saved.Size {
			return errors.New("download source changed")
		}
		if !t.Modified.IsZero() && !t.Modified.Equal(value(head.LastModified)) {
			return errors.New("download source modification time changed")
		}
		if err = saveIdentity(dir, saved); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if saved.ETag == "" || t.ETag != "" && t.ETag != saved.ETag || t.VersionID != "" && t.VersionID != saved.VersionID {
		return errors.New("download source differs from checkpoint")
	}
	stage := filepath.Join(dir, "download.data")
	req := &oss.GetObjectRequest{Bucket: &t.Bucket, Key: &t.Key, VersionId: optional(saved.VersionID), IfMatch: &saved.ETag, ProgressFn: progressCallback("下载", notify)}
	_, err = oss.NewDownloader(c.sdk, func(o *oss.DownloaderOptions) {
		o.EnableCheckpoint = true
		o.CheckpointDir = dir + string(os.PathSeparator)
		o.VerifyData = true
		o.UseTempFile = true
	}).DownloadFile(ctx, req, stage)
	if err != nil {
		return err
	}
	info, err := os.Stat(stage)
	if err != nil {
		return err
	}
	if info.Size() != saved.Size {
		return errors.New("download size differs from pinned source")
	}
	if err = publishDownloadInto(ctx, stage, root, relative); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// The SDK writes only inside the private checkpoint directory. Publishing uses
// an os.Root handle and an exclusive temporary file, so a destination symlink
// cannot redirect writes. Folder jobs must resolve every key with ResolveDestination.
func openDownloadRoot(destination, selectedRoot string) (*os.Root, string, error) {
	if selectedRoot == "" {
		return nil, "", errors.New("download requires the selected destination root")
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return nil, "", err
	}
	rootPath, err := filepath.Abs(selectedRoot)
	if err != nil {
		return nil, "", err
	}
	relative, err := filepath.Rel(rootPath, abs)
	if err != nil {
		return nil, "", err
	}
	if !filepath.IsLocal(relative) || relative == "." {
		return nil, "", errors.New("download destination escapes selected directory")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, "", err
	}
	return root, relative, nil
}

func publishDownloadInto(ctx context.Context, source string, root *os.Root, relative string) error {
	var err error
	// MkdirAll/OpenFile/Rename stay relative to one anchored root handle. Even
	// a symlink substituted after job creation cannot escape this directory.
	parent := filepath.Dir(relative)
	if err = root.MkdirAll(parent, 0700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	temp := filepath.Join(parent, ".aliyuntools-"+fmt.Sprintf("%x", random[:])+".tmp")
	out, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	if _, err = io.Copy(out, contextReader{ctx, in}); err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return root.Rename(temp, relative)
}

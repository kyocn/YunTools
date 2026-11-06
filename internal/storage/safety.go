// Package storage contains safety boundaries shared by transfer jobs and the UI.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ResolveDestination accepts only canonical relative object paths. Existing
// symlinks below the selected root are rejected. Recheck immediately before
// use; callers writing untrusted paths should additionally use os.OpenRoot.
func ResolveDestination(root, key string) (string, error) {
	if key == "" || strings.ContainsAny(key, "\\\x00:") || strings.HasPrefix(key, "/") {
		return "", errors.New("unsafe object path")
	}
	for _, s := range strings.Split(key, "/") {
		if s == "" || s == "." || s == ".." {
			return "", errors.New("unsafe object path component")
		}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(abs, filepath.FromSlash(key))
	// Check all ancestors rather than only the relative key: a symlink in the
	// chosen directory is just as capable of escaping the selected destination.
	current := string(filepath.Separator)
	for _, s := range strings.Split(strings.TrimPrefix(dest, string(filepath.Separator)), string(filepath.Separator)) {
		current = filepath.Join(current, s)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("destination contains a symbolic link")
		}
	}
	return dest, nil
}

type FileIdentity struct {
	Size     int64
	Modified time.Time
	SHA256   string
}

func SnapshotFile(path string) (FileIdentity, error) {
	return SnapshotFileContext(context.Background(), path)
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

func SnapshotFileContext(ctx context.Context, path string) (FileIdentity, error) {
	if err := ctx.Err(); err != nil {
		return FileIdentity{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return FileIdentity{}, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return FileIdentity{}, err
	}
	if !before.Mode().IsRegular() {
		return FileIdentity{}, errors.New("source must be a regular file")
	}
	h := sha256.New()
	n, err := io.Copy(h, contextReader{ctx, f})
	if err != nil {
		return FileIdentity{}, err
	}
	after, err := f.Stat()
	if err != nil {
		return FileIdentity{}, err
	}
	if n != before.Size() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return FileIdentity{}, errors.New("source changed while reading")
	}
	return FileIdentity{n, before.ModTime(), hex.EncodeToString(h.Sum(nil))}, nil
}
func VerifyFile(path string, want FileIdentity) error {
	return VerifyFileContext(context.Background(), path, want)
}
func VerifyFileContext(ctx context.Context, path string, want FileIdentity) error {
	got, err := SnapshotFileContext(ctx, path)
	if err != nil {
		return err
	}
	if got.Size != want.Size || !got.Modified.Equal(want.Modified) || got.SHA256 != want.SHA256 {
		return errors.New("source changed; start a new transfer")
	}
	return nil
}

// DeleteManifest owns an immutable copy of the exact keys shown for confirmation.
// It never expands a prefix or enumerates versions after the user confirms.
type DeleteManifest struct {
	accountID, bucket string
	keys              []string
}

func NewDeleteManifest(accountID, bucket string, keys []string) (DeleteManifest, error) {
	if accountID == "" || bucket == "" || len(keys) == 0 {
		return DeleteManifest{}, errors.New("empty deletion manifest")
	}
	copyKeys := append([]string(nil), keys...)
	slices.Sort(copyKeys)
	for i, k := range copyKeys {
		if k == "" || (i > 0 && k == copyKeys[i-1]) {
			return DeleteManifest{}, errors.New("empty or duplicate object key")
		}
	}
	return DeleteManifest{accountID, bucket, copyKeys}, nil
}
func (m DeleteManifest) Keys() []string { return append([]string(nil), m.keys...) }

type ObjectDeleter interface {
	DeleteObjects(context.Context, string, []string) error
}

func ConfirmDelete(ctx context.Context, d ObjectDeleter, m DeleteManifest, accountID, bucket string, confirmedKeys []string) error {
	keys := append([]string(nil), confirmedKeys...)
	slices.Sort(keys)
	if len(m.keys) == 0 || accountID != m.accountID || bucket != m.bucket || !slices.Equal(keys, m.keys) {
		return errors.New("deletion does not match the confirmed account, bucket and object manifest")
	}
	return d.DeleteObjects(ctx, m.bucket, m.Keys())
}

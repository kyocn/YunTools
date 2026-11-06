package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDestinationRejectsTraversalAndSymlinks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../escape", "a/../../escape", "/absolute", `a\..\escape`, "C:/escape", "a//b", "a/./b", ""} {
		if _, err := ResolveDestination(root, key); err == nil {
			t.Errorf("accepted unsafe key %q", key)
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveDestination(root, "link/escape"); err == nil {
		t.Fatal("followed symlink")
	}
	got, err := ResolveDestination(root, "folder/文件.txt")
	if err != nil || got != filepath.Join(root, "folder", "文件.txt") {
		t.Fatalf("safe path %q %v", got, err)
	}
}
func TestSourceIdentityDetectsSameSizeSameTimeMutation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(p, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := SnapshotFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("after!"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, time.Now(), before.Modified); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(p, before); err == nil {
		t.Fatal("accepted changed source with matching size and modification time")
	}
}

type deleter struct{ keys []string }

func (d *deleter) DeleteObjects(_ context.Context, _ string, keys []string) error {
	d.keys = append(d.keys, keys...)
	return nil
}
func TestDeleteRequiresExactConfirmedManifestAndAccount(t *testing.T) {
	keys := []string{"a", "b"}
	m, err := NewDeleteManifest("acct", "bucket", keys)
	if err != nil {
		t.Fatal(err)
	}
	keys[0] = "unrelated"
	d := &deleter{}
	for _, x := range []struct {
		a, b string
		k    []string
	}{{"other", "bucket", []string{"a", "b"}}, {"acct", "bucket", []string{"a"}}, {"acct", "bucket", []string{"a", "b", "c"}}} {
		if err := ConfirmDelete(context.Background(), d, m, x.a, x.b, x.k); err == nil {
			t.Fatal("unconfirmed deletion allowed")
		}
	}
	if len(d.keys) != 0 {
		t.Fatal("delete executed before confirmation")
	}
	copyKeys := m.Keys()
	copyKeys[0] = "unrelated"
	if err := ConfirmDelete(context.Background(), d, m, "acct", "bucket", []string{"b", "a"}); err != nil {
		t.Fatal(err)
	}
	if len(d.keys) != 2 || d.keys[0] != "a" || d.keys[1] != "b" {
		t.Fatalf("wrong manifest %v", d.keys)
	}
}

func TestSourceHashHonorsCancellation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(p, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SnapshotFileContext(ctx, p); !errors.Is(err, context.Canceled) {
		t.Fatalf("hash ignored cancellation: %v", err)
	}
}

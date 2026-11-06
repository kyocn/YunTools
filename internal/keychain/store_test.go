package keychain

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

type memoryBackend struct {
	values  map[string][]byte
	failure error
	sets    int
}

func (b *memoryBackend) Get(k string) ([]byte, error) {
	if b.failure != nil {
		return nil, b.failure
	}
	v, ok := b.values[k]
	if !ok {
		return nil, ErrNotFound
	}
	return bytes.Clone(v), nil
}
func (b *memoryBackend) Set(k string, v []byte) error {
	if b.failure != nil {
		return b.failure
	}
	if b.values == nil {
		b.values = map[string][]byte{}
	}
	b.values[k] = bytes.Clone(v)
	b.sets++
	return nil
}
func (b *memoryBackend) Delete(k string) error { delete(b.values, k); return nil }
func openTest(t *testing.T) (*Store, string, *memoryBackend) {
	t.Helper()
	d := t.TempDir()
	b := &memoryBackend{}
	s, e := Open(d, b)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, d, b
}
func TestEncryptedRoundTripAndIsolation(t *testing.T) {
	s, d, b := openTest(t)
	secret := []byte("secret-credential-no-plaintext")
	if e := s.Put("profile-one", secret); e != nil {
		t.Fatal(e)
	}
	secret[0] = 'X'
	got, e := s.Get("profile-one")
	if e != nil || string(got) != "secret-credential-no-plaintext" {
		t.Fatalf("get: %q %v", got, e)
	}
	got[0] = 'Y'
	raw, e := os.ReadFile(filepath.Join(d, "vault.enc"))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte("secret-credential")) || bytes.Contains(raw, []byte("profile-one")) {
		t.Fatal("plaintext persisted")
	}
	info, _ := os.Stat(filepath.Join(d, "vault.enc"))
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions %v", info.Mode())
	}
	if e := s.Put("profile-two", []byte("other")); e != nil {
		t.Fatal(e)
	}
	if e := s.Delete("profile-two"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Get("profile-two"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(d, b)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got, e = s.Get("profile-one")
	if e != nil || string(got) != "secret-credential-no-plaintext" {
		t.Fatalf("reopen: %q %v", got, e)
	}
}
func TestTamperingWrongKeyAndMissingKeyFailClosed(t *testing.T) {
	for _, kind := range []string{"tamper", "wrong", "missing"} {
		t.Run(kind, func(t *testing.T) {
			s, d, b := openTest(t)
			if e := s.Put("key", []byte("secret")); e != nil {
				t.Fatal(e)
			}
			s.Close()
			switch kind {
			case "tamper":
				p := filepath.Join(d, "vault.enc")
				v, _ := os.ReadFile(p)
				v[len(v)-1] ^= 1
				os.WriteFile(p, v, 0600)
			case "wrong":
				for k := range b.values {
					b.values[k] = make([]byte, 32)
				}
			case "missing":
				b.values = nil
			}
			before := b.sets
			if s, e := Open(d, b); e == nil {
				s.Close()
				t.Fatal("opened invalid vault")
			}
			if b.sets != before {
				t.Fatal("replaced master key")
			}
		})
	}
}
func TestNoncesAndConcurrentWrites(t *testing.T) {
	s, d, b := openTest(t)
	s.Put("same", []byte("same"))
	a, _ := os.ReadFile(filepath.Join(d, "vault.enc"))
	s.Put("same", []byte("same"))
	z, _ := os.ReadFile(filepath.Join(d, "vault.enc"))
	if bytes.Equal(a, z) {
		t.Fatal("nonce reused")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if e := s.Put(fmt.Sprint(i), []byte(fmt.Sprint(i))); e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	s.Close()
	s, e := Open(d, b)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for i := 0; i < 20; i++ {
		v, e := s.Get(fmt.Sprint(i))
		if e != nil || string(v) != fmt.Sprint(i) {
			t.Fatalf("lost write %d", i)
		}
	}
}
func TestLifecycleLock(t *testing.T) {
	s, d, b := openTest(t)
	if other, e := Open(d, b); e == nil {
		other.Close()
		t.Fatal("two owners")
	}
	s.Close()
	other, e := Open(d, b)
	if e != nil {
		t.Fatal(e)
	}
	other.Close()
	if e := s.Put("x", nil); e == nil {
		t.Fatal("closed store accepts writes")
	}
}
func TestLockAcrossProcesses(t *testing.T) {
	if d := os.Getenv("VAULT_LOCK_HELPER"); d != "" {
		s, e := Open(d, &memoryBackend{})
		if e == nil {
			s.Close()
			os.Exit(3)
		}
		if !errors.Is(e, ErrLocked) {
			os.Exit(4)
		}
		return
	}
	_, d, _ := openTest(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockAcrossProcesses$")
	cmd.Env = append(os.Environ(), "VAULT_LOCK_HELPER="+d)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("child %v: %s", e, out)
	}
}
func TestBackendUnavailable(t *testing.T) {
	d := t.TempDir()
	if s, e := Open(d, &memoryBackend{failure: errors.New("locked")}); e == nil {
		s.Close()
		t.Fatal("fallback used")
	}
	if _, e := os.Stat(filepath.Join(d, "vault.enc")); !os.IsNotExist(e) {
		t.Fatal("created file despite failure")
	}
}
func TestRejectsSymlinkVault(t *testing.T) {
	d := t.TempDir()
	outside := filepath.Join(t.TempDir(), "target")
	os.WriteFile(outside, []byte("untouched"), 0600)
	if e := os.Symlink(outside, filepath.Join(d, "vault.enc")); e != nil {
		t.Skip(e)
	}
	if s, e := Open(d, &memoryBackend{}); e == nil {
		s.Close()
		t.Fatal("symlink accepted")
	}
	v, _ := os.ReadFile(outside)
	if string(v) != "untouched" {
		t.Fatal("modified target")
	}
}

func TestFailedSaveDoesNotChangeMemoryOrLeaveTempFile(t *testing.T) {
	s, d, _ := openTest(t)
	if e := s.Put("key", []byte("original")); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(d, "vault.enc")
	if e := os.Rename(path, path+".backup"); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	if e := s.Put("key", []byte("replacement")); e == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	got, e := s.Get("key")
	if e != nil || string(got) != "original" {
		t.Fatalf("failed write changed in-memory state: %q %v", got, e)
	}
	leftovers, e := filepath.Glob(filepath.Join(d, ".vault-*"))
	if e != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files remain: %v %v", leftovers, e)
	}
	if e := s.Delete("key"); e == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	got, e = s.Get("key")
	if e != nil || string(got) != "original" {
		t.Fatal("failed delete changed state")
	}
}
func TestMissingKeyIsDistinctAndKeepsCiphertext(t *testing.T) {
	s, d, b := openTest(t)
	s.Put("key", []byte("secret"))
	s.Close()
	original, e := os.ReadFile(filepath.Join(d, "vault.enc"))
	if e != nil {
		t.Fatal(e)
	}
	b.values = nil
	if s, e := Open(d, b); !errors.Is(e, ErrMissingMasterKey) {
		if s != nil {
			s.Close()
		}
		t.Fatalf("missing key: %v", e)
	}
	after, e := os.ReadFile(filepath.Join(d, "vault.enc"))
	if e != nil || !bytes.Equal(original, after) {
		t.Fatal("missing key modified ciphertext")
	}
}

// Package keychain persists all application secrets in one authenticated encrypted vault.
package keychain

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"yuntools/internal/cloud"
)

var (
	ErrNotFound         = cloud.ErrNotFound
	ErrLocked           = errors.New("vault is already in use")
	ErrClosed           = errors.New("vault is closed")
	ErrMissingMasterKey = errors.New("vault master key is missing; restore the original key to unlock")
	ErrAuthentication   = errors.New("vault authentication failed")
)

// Backend is an OS credential store. Implementations must distinguish absent keys
// from locked/unavailable keychains and must not use a plaintext fallback.
type Backend interface {
	Get(string) ([]byte, error)
	Set(string, []byte) error
	Delete(string) error
}

var header = []byte("ATVAULT\x01")

const vaultName = "vault.enc"

type Store struct {
	mu     sync.Mutex
	dir    string
	key    []byte
	values map[string][]byte
	lock   *os.File
	closed bool
}

func Open(dir string, backend Backend) (_ *Store, err error) {
	if backend == nil {
		return nil, errors.New("system keychain backend is required")
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("vault directory must be a real directory")
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	// Resolve parent aliases so they address the same keychain entry and file lock.
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	lock, err := acquireLock(filepath.Join(dir, "vault.lock"))
	if err != nil {
		return nil, err
	}
	s := &Store{dir: dir, lock: lock, values: map[string][]byte{}}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	path := filepath.Join(dir, vaultName)
	exists := false
	if info, e := os.Lstat(path); e == nil {
		exists = true
		if !info.Mode().IsRegular() {
			return nil, errors.New("vault must be a regular file")
		}
		if e = os.Chmod(path, 0600); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	digest := sha256.Sum256([]byte(dir))
	keyName := "vault-master-" + hex.EncodeToString(digest[:])
	key, e := backend.Get(keyName)
	if errors.Is(e, ErrNotFound) {
		if exists {
			return nil, ErrMissingMasterKey
		}
		key = make([]byte, 32)
		if _, e = rand.Read(key); e != nil {
			return nil, e
		}
		if e = backend.Set(keyName, key); e != nil {
			clear(key)
			return nil, fmt.Errorf("store master key: %w", e)
		}
	} else if e != nil {
		return nil, fmt.Errorf("unlock system keychain: %w", e)
	}
	if len(key) != 32 {
		return nil, errors.New("invalid vault master key length")
	}
	s.key = bytes.Clone(key)
	if exists {
		data, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		aead, e := s.aead()
		if e != nil {
			return nil, e
		}
		if len(data) < len(header)+aead.NonceSize()+aead.Overhead() || !bytes.Equal(data[:len(header)], header) {
			return nil, ErrAuthentication
		}
		nonce := data[len(header) : len(header)+aead.NonceSize()]
		plain, e := aead.Open(nil, nonce, data[len(header)+aead.NonceSize():], header)
		if e != nil {
			return nil, ErrAuthentication
		}
		defer clear(plain)
		if e = json.Unmarshal(plain, &s.values); e != nil || s.values == nil {
			return nil, errors.New("invalid encrypted vault payload")
		}
	} else if e = s.persist(s.values); e != nil {
		return nil, e
	}
	return s, nil
}

func (s *Store) aead() (cipher.AEAD, error) {
	block, e := aes.NewCipher(s.key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func (s *Store) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	v, ok := s.values[key]
	if !ok {
		return nil, ErrNotFound
	}
	return bytes.Clone(v), nil
}
func (s *Store) Put(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if key == "" {
		return errors.New("secret key is empty")
	}
	next := s.clone()
	next[key] = bytes.Clone(value)
	if e := s.persist(next); e != nil {
		return e
	}
	s.values = next
	return nil
}
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if _, ok := s.values[key]; !ok {
		return ErrNotFound
	}
	next := s.clone()
	delete(next, key)
	if e := s.persist(next); e != nil {
		return e
	}
	s.values = next
	return nil
}
func (s *Store) clone() map[string][]byte {
	next := make(map[string][]byte, len(s.values))
	for k, v := range s.values {
		next[k] = v
	}
	return next
}
func (s *Store) persist(values map[string][]byte) error {
	plain, e := json.Marshal(values)
	if e != nil {
		return e
	}
	defer clear(plain)
	aead, e := s.aead()
	if e != nil {
		return e
	}
	nonce := make([]byte, aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	data := append(bytes.Clone(header), nonce...)
	data = aead.Seal(data, nonce, plain, header)
	f, e := os.CreateTemp(s.dir, ".vault-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = os.Rename(name, filepath.Join(s.dir, vaultName)); e != nil {
		return e
	}
	// Rename has committed the new state. Directory fsync is best effort: some
	// platforms/filesystems don't support it, and failure cannot roll back rename.
	if d, e := os.Open(s.dir); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	clear(s.key)
	s.key = nil
	for _, v := range s.values {
		clear(v)
	}
	s.values = nil
	if s.lock != nil {
		return releaseLock(s.lock)
	}
	return nil
}

package accounts

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
	"yuntools/internal/cloud"
	"yuntools/internal/keychain"
)

type backend struct{ v map[string][]byte }

func (b *backend) Get(k string) ([]byte, error) {
	v, ok := b.v[k]
	if !ok {
		return nil, keychain.ErrNotFound
	}
	return bytes.Clone(v), nil
}
func (b *backend) Set(k string, v []byte) error {
	if b.v == nil {
		b.v = map[string][]byte{}
	}
	b.v[k] = bytes.Clone(v)
	return nil
}
func (b *backend) Delete(k string) error { delete(b.v, k); return nil }
func TestProfilesPersistAndUpdateCredentials(t *testing.T) {
	d := t.TempDir()
	b := &backend{}
	s, e := keychain.Open(d, b)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m := New(s)
	a, e := m.Save(cloud.Account{Name: "one", AccessKeyID: "id", AccessKeySecret: "secret"})
	if e != nil || a.ID == "" || a.Provider != "aliyun" {
		t.Fatalf("save %v %v", a, e)
	}
	other, e := m.Save(cloud.Account{Name: "two", AccessKeyID: "other-id", AccessKeySecret: "other-secret"})
	if e != nil {
		t.Fatal(e)
	}
	a.AccessKeySecret = "rotated"
	if _, e = m.Save(a); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = keychain.Open(d, b)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m = New(s)
	list, e := m.List()
	if e != nil || len(list) != 2 {
		t.Fatalf("list count %d %v", len(list), e)
	}
	for _, p := range list {
		if p.ID == a.ID && p.AccessKeySecret != "rotated" {
			t.Fatal("stale credential")
		}
		if p.ID == other.ID && p.AccessKeySecret != "other-secret" {
			t.Fatal("cross profile corruption")
		}
	}
	if e := m.Delete(a.ID); e != nil {
		t.Fatal(e)
	}
	list, e = m.List()
	if e != nil || len(list) != 1 || list[0].ID != other.ID {
		t.Fatal("wrong profile removed")
	}
	if e = m.Delete(a.ID); !errors.Is(e, keychain.ErrNotFound) {
		t.Fatal(e)
	}
}
func TestValidationAndConcurrentSaves(t *testing.T) {
	s, e := keychain.Open(t.TempDir(), &backend{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m := New(s)
	for _, a := range []cloud.Account{{}, {Name: "n", AccessKeyID: "id"}, {Name: "n", AccessKeySecret: "secret"}, {Name: "n", Provider: "unsupported", AccessKeyID: "id", AccessKeySecret: "secret"}} {
		if _, e := m.Save(a); e == nil {
			t.Fatal("invalid accepted")
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := m.Save(cloud.Account{Name: "n", AccessKeyID: "id", AccessKeySecret: "secret"}); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	list, e := m.List()
	if e != nil || len(list) != 15 {
		t.Fatalf("lost accounts: %d %v", len(list), e)
	}
}

func TestDeletedProfileCannotBeResurrectedByStaleSave(t *testing.T) {
	s, e := keychain.Open(t.TempDir(), &backend{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m := New(s)
	a, e := m.Save(cloud.Account{Name: "n", AccessKeyID: "id", AccessKeySecret: "secret"})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Delete(a.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Save(a); !errors.Is(e, keychain.ErrNotFound) {
		t.Fatalf("stale profile save: %v", e)
	}
	list, e := m.List()
	if e != nil || len(list) != 0 {
		t.Fatal("deleted profile reappeared")
	}
}
func TestCorruptProfilesAreNotSilentlyDiscarded(t *testing.T) {
	s, e := keychain.Open(t.TempDir(), &backend{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	original := []byte("invalid-json")
	if e = s.Put(profilesKey, original); e != nil {
		t.Fatal(e)
	}
	m := New(s)
	if _, e = m.List(); e == nil {
		t.Fatal("corrupt profiles ignored")
	}
	if _, e = m.Save(cloud.Account{Name: "n", AccessKeyID: "id", AccessKeySecret: "secret"}); e == nil {
		t.Fatal("corrupt profiles overwritten")
	}
	after, e := s.Get(profilesKey)
	if e != nil || !bytes.Equal(after, original) {
		t.Fatal("corrupt profiles discarded")
	}
}

func TestAccountCreationTimePreservedAndLegacyUnknown(t *testing.T) {
	s, e := keychain.Open(t.TempDir(), &backend{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m := New(s)
	a, e := m.Save(cloud.Account{Name: "new", AccessKeyID: "id", AccessKeySecret: "secret"})
	if e != nil || a.CreatedAt.IsZero() {
		t.Fatalf("%v %v", a.CreatedAt, e)
	}
	created := a.CreatedAt
	a.CreatedAt = time.Time{}
	a, e = m.Save(a)
	if e != nil || !a.CreatedAt.Equal(created) {
		t.Fatalf("edit lost creation time: %v %v", a.CreatedAt, e)
	}
	legacy := []byte(`[{"id":"legacy","name":"old","provider":"aliyun","accessKeyId":"id","accessKeySecret":"secret"}]`)
	if e = s.Put(profilesKey, legacy); e != nil {
		t.Fatal(e)
	}
	list, e := m.List()
	if e != nil || len(list) != 1 || !list[0].CreatedAt.IsZero() {
		t.Fatalf("legacy time invented: %v %v", list, e)
	}
	edited, e := m.Save(list[0])
	if e != nil || !edited.CreatedAt.IsZero() {
		t.Fatalf("legacy edit invented time: %v %v", edited.CreatedAt, e)
	}
}

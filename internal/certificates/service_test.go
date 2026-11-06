package certificates

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"github.com/go-acme/lego/v4/certificate"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"yuntools/internal/cloud"
)

type memoryStore struct {
	data    map[string][]byte
	fail    bool
	putFail bool
}

func (m *memoryStore) Get(k string) ([]byte, error) {
	if m.fail {
		return nil, errors.New("locked")
	}
	v, ok := m.data[k]
	if !ok {
		return nil, cloud.ErrNotFound
	}
	return append([]byte(nil), v...), nil
}
func (m *memoryStore) Put(k string, v []byte) error {
	if m.fail || m.putFail {
		return errors.New("locked")
	}
	m.data[k] = append([]byte(nil), v...)
	return nil
}
func (m *memoryStore) Delete(k string) error { delete(m.data, k); return nil }

type fakeDNS struct {
	records map[string]cloud.Record
	deleted []string
	create  int
}

func (f *fakeDNS) ListDomains(context.Context) ([]cloud.Domain, error) {
	return []cloud.Domain{{Name: "example.com"}}, nil
}
func (f *fakeDNS) AddDomain(context.Context, string) error { return nil }
func (f *fakeDNS) ListRecords(context.Context, string) ([]cloud.Record, error) {
	var r []cloud.Record
	for _, v := range f.records {
		r = append(r, v)
	}
	return r, nil
}
func (f *fakeDNS) AddRecord(ctx context.Context, r cloud.Record) (cloud.Record, error) {
	return f.CreateChallengeRecord(ctx, r)
}
func (f *fakeDNS) CreateChallengeRecord(_ context.Context, r cloud.Record) (cloud.Record, error) {
	f.create++
	r.ID = "created"
	f.records[r.ID] = r
	return r, nil
}
func (f *fakeDNS) GetRecord(_ context.Context, id string) (cloud.Record, error) {
	r, ok := f.records[id]
	if !ok {
		return r, cloud.ErrNotFound
	}
	return r, nil
}
func (f *fakeDNS) DeleteChallengeRecord(_ context.Context, r cloud.Record) error {
	f.deleted = append(f.deleted, r.ID)
	delete(f.records, r.ID)
	return nil
}
func TestCleanupOnlyPersistedExactOwnership(t *testing.T) {
	store := &memoryStore{data: map[string][]byte{}}
	dns := &fakeDNS{records: map[string]cloud.Record{}}
	s := New(store, func(cloud.Account) (cloud.ChallengeDNS, error) { return dns, nil })
	owned := cloud.Record{ID: "owned", Domain: "example.com", RR: "_acme-challenge", Type: "TXT", Value: "ours"}
	changed := owned
	changed.ID = "changed"
	unknown := owned
	unknown.ID = "unknown"
	dns.records[owned.ID] = owned
	dns.records[changed.ID] = changed
	dns.records[unknown.ID] = unknown
	if err := s.saveOwnership("a", []cloud.Record{owned, changed}); err != nil {
		t.Fatal(err)
	}
	v := dns.records[changed.ID]
	v.Value = "new-owner"
	dns.records[changed.ID] = v
	if err := s.Cleanup(context.Background(), cloud.Account{ID: "a"}); err == nil {
		t.Fatal("expected mismatch diagnostic")
	}
	if len(dns.deleted) != 1 || dns.deleted[0] != "owned" || len(dns.records) != 2 {
		t.Fatalf("unsafe cleanup: %v %#v", dns.deleted, dns.records)
	}
}
func TestStorageErrorsFailClosed(t *testing.T) {
	store := &memoryStore{data: map[string][]byte{}, fail: true}
	s := New(store, nil)
	if _, err := s.List("a"); err == nil {
		t.Fatal("swallowed locked-store failure")
	}
	if _, err := s.loadOwnership("a"); err == nil {
		t.Fatal("swallowed journal failure")
	}
}
func TestIssueRequiresTermsBeforeNetwork(t *testing.T) {
	s := New(&memoryStore{data: map[string][]byte{}}, func(cloud.Account) (cloud.ChallengeDNS, error) { t.Fatal("network before consent"); return nil, nil })
	if _, err := s.Issue(context.Background(), cloud.Account{ID: "a"}, Request{Domains: []string{"example.com"}, Email: "a@example.com"}, nil); err == nil {
		t.Fatal("missing terms accepted")
	}
}
func TestCancelledPropagationStopsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &challengeProvider{ctx: ctx}
	stop, err := p.checkPropagation("example.com", "_acme-challenge.example.com.", "token", func(string, string) (bool, error) { t.Fatal("DNS check after cancel"); return false, nil })
	if !stop || !errors.Is(err, context.Canceled) {
		t.Fatalf("%t %v", stop, err)
	}
}
func TestExportRoundTripUsesPrivatePermissionsAndRefusesOverwrite(t *testing.T) {
	store := &memoryStore{data: map[string][]byte{}}
	s := New(store, nil)
	resource := makeCertificate(t)
	info, err := s.save("a", "", Request{Domains: []string{"example.com"}, Email: "a@example.com", Staging: true}, resource, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.List("a")
	if err != nil || len(list) != 1 || list[0].ID != info.ID {
		t.Fatalf("list %v %v", list, err)
	}
	dir := t.TempDir()
	if err = s.Export("a", info.ID, dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, info.ID+".key.pem")
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, resource.PrivateKey) {
		t.Fatalf("private key missing: %v", err)
	}
	stat, _ := os.Stat(path)
	if stat.Mode().Perm() != 0600 {
		t.Fatalf("key permissions %v", stat.Mode())
	}
	if s.Export("a", info.ID, dir) == nil {
		t.Fatal("overwrote existing export")
	}
	if s.Export("other", info.ID, t.TempDir()) == nil {
		t.Fatal("cross-account export")
	}
}
func makeCertificate(t *testing.T) *certificate.Resource {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"example.com"}, NotBefore: time.Now(), NotAfter: time.Now().Add(24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &private.PublicKey, private)
	if err != nil {
		t.Fatal(err)
	}
	pk, _ := x509.MarshalPKCS8PrivateKey(private)
	return &certificate.Resource{Domain: "example.com", Certificate: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), PrivateKey: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk})}
}
func TestCreateThenJournalFailureRetainsUnknownTXT(t *testing.T) {
	store := &memoryStore{data: map[string][]byte{}, putFail: true}
	dns := &fakeDNS{records: map[string]cloud.Record{}}
	s := New(store, func(cloud.Account) (cloud.ChallengeDNS, error) { return dns, nil })
	p := &challengeProvider{service: s, accountID: "a", dns: dns, ctx: context.Background()}
	r := cloud.Record{Domain: "example.com", RR: "_acme-challenge", Type: "TXT", Value: "ours"}
	if p.createAndJournal(r) == nil {
		t.Fatal("journal failure swallowed")
	}
	if len(dns.records) != 1 || len(dns.deleted) != 0 {
		t.Fatal("unpersisted TXT deleted")
	}
	store.putFail = false
	restarted := New(store, func(cloud.Account) (cloud.ChallengeDNS, error) { return dns, nil })
	if err := restarted.Cleanup(context.Background(), cloud.Account{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if len(dns.records) != 1 {
		t.Fatal("restart guessed ownership")
	}
}

func TestRenewAfterUsesActualCertificateLifetime(t *testing.T) {
	s := New(&memoryStore{data: map[string][]byte{}}, nil)
	resource := makeCertificate(t)
	block, _ := pem.Decode(resource.Certificate)
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.save("a", "", Request{Domains: []string{"example.com"}}, resource, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := parsed.NotBefore.Add(parsed.NotAfter.Sub(parsed.NotBefore) * 2 / 3)
	if !info.IssuedAt.Equal(parsed.NotBefore) || !info.RenewAfter.Equal(want) {
		t.Fatalf("issued=%v renewal=%v want=%v", info.IssuedAt, info.RenewAfter, want)
	}
	list, err := s.List("a")
	if err != nil || len(list) != 1 || !list[0].RenewAfter.Equal(want) {
		t.Fatalf("renewal not persisted: %v %v", list, err)
	}
}
func TestQueuedCleanupCancellationDoesNotWaitForActiveOperation(t *testing.T) {
	var enteredOnce sync.Once
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	s := New(&memoryStore{data: map[string][]byte{}}, func(cloud.Account) (cloud.ChallengeDNS, error) {
		enteredOnce.Do(func() { close(entered) })
		<-release
		return &fakeDNS{records: map[string]cloud.Record{}}, nil
	})
	go func() { firstDone <- s.Cleanup(context.Background(), cloud.Account{ID: "a"}) }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	go func() { secondDone <- s.Cleanup(ctx, cloud.Account{ID: "a"}) }()
	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("queued operation returned %v", err)
		}
	case <-time.After(time.Second):
		t.Error("cancelled operation blocked behind active operation")
	}
	close(release)
	<-firstDone
}

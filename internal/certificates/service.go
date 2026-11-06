// Package certificates manages ACME DNS-01 certificates. All persistent secrets go
// through the supplied encrypted SecretStore; only explicit Export writes PEM keys.
package certificates

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"yuntools/internal/cloud"
)

type Request struct {
	Domains              []string
	Email                string
	AcceptTerms, Staging bool
}
type Certificate struct {
	ID, AccountID string
	Domains       []string
	Email         string
	Staging       bool
	ExpiresAt     time.Time
	IssuedAt      time.Time
	RenewAfter    time.Time
}
type DNSFactory func(cloud.Account) (cloud.ChallengeDNS, error)
type Service struct {
	store     cloud.SecretStore
	factory   DNSFactory
	operation chan struct{}
	data      sync.Mutex
}

func New(store cloud.SecretStore, factory DNSFactory) *Service {
	return &Service{store: store, factory: factory, operation: make(chan struct{}, 1)}
}

type storedCertificate struct {
	Certificate
	Resource                     certificate.Resource
	PrivateKey, PEM, Issuer, CSR []byte
}
type acmeAccount struct {
	Email        string
	Key          []byte
	Registration *registration.Resource
}
type acmeUser struct {
	email        string
	key          crypto.PrivateKey
	registration *registration.Resource
}

func (u *acmeUser) GetEmail() string                        { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.registration }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }
func key(prefix, id string) string {
	sum := sha256.Sum256([]byte(id))
	return "certificates/" + prefix + "/" + hex.EncodeToString(sum[:])
}
func (s *Service) read(k string, out any) error {
	data, err := s.store.Get(k)
	if errors.Is(err, cloud.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, out); err != nil {
		return errors.New("invalid encrypted certificate data")
	}
	return nil
}
func (s *Service) write(k string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.store.Put(k, b)
}
func (s *Service) List(accountID string) ([]Certificate, error) {
	s.data.Lock()
	defer s.data.Unlock()
	var stored []storedCertificate
	if err := s.read(key("issued", accountID), &stored); err != nil {
		return nil, err
	}
	out := make([]Certificate, 0, len(stored))
	for _, v := range stored {
		if v.AccountID != accountID {
			return nil, errors.New("certificate account mismatch")
		}
		out = append(out, v.Certificate)
	}
	return out, nil
}
func (s *Service) find(accountID, id string) (storedCertificate, error) {
	var entries []storedCertificate
	if err := s.read(key("issued", accountID), &entries); err != nil {
		return storedCertificate{}, err
	}
	for _, e := range entries {
		if e.ID == id && e.AccountID == accountID {
			e.Resource.PrivateKey = e.PrivateKey
			e.Resource.Certificate = e.PEM
			e.Resource.IssuerCertificate = e.Issuer
			e.Resource.CSR = e.CSR
			return e, nil
		}
	}
	return storedCertificate{}, cloud.ErrNotFound
}

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validateRequest(r Request) (Request, error) {
	if !r.AcceptTerms {
		return r, errors.New("explicit ACME terms agreement is required")
	}
	email, err := mail.ParseAddress(r.Email)
	if err != nil || email.Address != r.Email {
		return r, errors.New("valid ACME account email is required")
	}
	if len(r.Domains) == 0 || len(r.Domains) > 100 {
		return r, errors.New("provide 1–100 domains")
	}
	seen := map[string]bool{}
	var domains []string
	for _, d := range r.Domains {
		d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
		plain := strings.TrimPrefix(d, "*.")
		if !strings.Contains(plain, ".") || len(plain) > 253 {
			return r, errors.New("invalid certificate domain")
		}
		for _, l := range strings.Split(plain, ".") {
			if !labelPattern.MatchString(l) {
				return r, errors.New("invalid certificate domain; use ASCII/punycode")
			}
		}
		if !seen[d] {
			domains = append(domains, d)
			seen[d] = true
		}
	}
	r.Domains = domains
	return r, nil
}

type requestTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t requestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(r.Clone(t.ctx))
}
func (s *Service) client(ctx context.Context, a cloud.Account, r Request, dns cloud.ChallengeDNS, progress func(cloud.Progress)) (*lego.Client, error) {
	accountKey := key("acme-account", fmt.Sprintf("%s|%s|%t", a.ID, r.Email, r.Staging))
	var saved acmeAccount
	if err := s.read(accountKey, &saved); err != nil {
		return nil, err
	}
	if len(saved.Key) == 0 {
		private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalPKCS8PrivateKey(private)
		if err != nil {
			return nil, err
		}
		saved = acmeAccount{Email: r.Email, Key: der}
		if err = s.write(accountKey, saved); err != nil {
			return nil, err
		}
	}
	private, err := x509.ParsePKCS8PrivateKey(saved.Key)
	if err != nil {
		return nil, errors.New("invalid stored ACME account key")
	}
	if saved.Email != r.Email {
		return nil, errors.New("ACME email mismatch")
	}
	user := &acmeUser{email: saved.Email, key: private, registration: saved.Registration}
	cfg := lego.NewConfig(user)
	if r.Staging {
		cfg.CADirURL = lego.LEDirectoryStaging
	}
	cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second, Transport: requestTransport{ctx, http.DefaultTransport}}
	client, err := lego.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	if user.registration == nil {
		emit(progress, "注册 ACME 账户", 0, 0)
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: r.AcceptTerms})
		if err != nil {
			return nil, err
		}
		user.registration = reg
		saved.Registration = reg
		if err = s.write(accountKey, saved); err != nil {
			return nil, err
		}
	}
	provider := &challengeProvider{service: s, accountID: a.ID, dns: dns, ctx: ctx, progress: progress}
	if err = client.Challenge.SetDNS01Provider(provider, dns01.WrapPreCheck(provider.checkPropagation)); err != nil {
		return nil, err
	}
	return client, nil
}
func emit(fn func(cloud.Progress), stage string, done, total int64) {
	if fn != nil {
		fn(cloud.Progress{Stage: stage, Completed: done, Total: total})
	}
}
func (s *Service) Issue(ctx context.Context, a cloud.Account, request Request, progress func(cloud.Progress)) (Certificate, error) {
	r, err := validateRequest(request)
	if err != nil {
		return Certificate{}, err
	}
	if a.ID == "" || s.factory == nil {
		return Certificate{}, errors.New("cloud account is required")
	}
	if err := s.beginOperation(ctx); err != nil {
		return Certificate{}, err
	}
	defer s.endOperation()
	dns, err := s.factory(a)
	if err != nil {
		return Certificate{}, err
	}
	client, err := s.client(ctx, a, r, dns, progress)
	if err != nil {
		return Certificate{}, err
	}
	emit(progress, "申请证书 / DNS 验证", 0, 0)
	resource, err := client.Certificate.Obtain(certificate.ObtainRequest{Domains: r.Domains, Bundle: true})
	if err != nil {
		return Certificate{}, err
	}
	return s.save(a.ID, "", r, resource, progress)
}
func (s *Service) Renew(ctx context.Context, a cloud.Account, id string, progress func(cloud.Progress)) (Certificate, error) {
	if err := s.beginOperation(ctx); err != nil {
		return Certificate{}, err
	}
	defer s.endOperation()
	s.data.Lock()
	previous, err := s.find(a.ID, id)
	s.data.Unlock()
	if err != nil {
		return Certificate{}, err
	}
	if s.factory == nil {
		return Certificate{}, errors.New("cloud provider unavailable")
	}
	dns, err := s.factory(a)
	if err != nil {
		return Certificate{}, err
	}
	r := Request{Domains: previous.Domains, Email: previous.Email, Staging: previous.Staging, AcceptTerms: true}
	client, err := s.client(ctx, a, r, dns, progress)
	if err != nil {
		return Certificate{}, err
	}
	emit(progress, "续签证书 / DNS 验证", 0, 0)
	resource, err := client.Certificate.RenewWithOptions(previous.Resource, &certificate.RenewOptions{Bundle: true})
	if err != nil {
		return Certificate{}, err
	}
	return s.save(a.ID, id, r, resource, progress)
}
func (s *Service) save(accountID, id string, r Request, resource *certificate.Resource, progress func(cloud.Progress)) (Certificate, error) {
	block, _ := pem.Decode(resource.Certificate)
	if block == nil {
		return Certificate{}, errors.New("ACME returned no certificate")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Certificate{}, err
	}
	if id == "" {
		var raw [16]byte
		if _, err = rand.Read(raw[:]); err != nil {
			return Certificate{}, err
		}
		id = hex.EncodeToString(raw[:])
	}
	info := Certificate{ID: id, AccountID: accountID, Domains: r.Domains, Email: r.Email, Staging: r.Staging, ExpiresAt: parsed.NotAfter, IssuedAt: parsed.NotBefore, RenewAfter: parsed.NotBefore.Add(parsed.NotAfter.Sub(parsed.NotBefore) * 2 / 3)}
	entry := storedCertificate{Certificate: info, Resource: *resource, PrivateKey: resource.PrivateKey, PEM: resource.Certificate, Issuer: resource.IssuerCertificate, CSR: resource.CSR}
	s.data.Lock()
	defer s.data.Unlock()
	var entries []storedCertificate
	if err = s.read(key("issued", accountID), &entries); err != nil {
		return Certificate{}, err
	}
	replaced := false
	for i := range entries {
		if entries[i].ID == id {
			entries[i] = entry
			replaced = true
		}
	}
	if !replaced {
		entries = append(entries, entry)
	}
	if err = s.write(key("issued", accountID), entries); err != nil {
		return Certificate{}, err
	}
	emit(progress, "证书已加密保存", 1, 1)
	return info, nil
}

// Export is the sole plaintext private-key write path. Existing files are never overwritten.
func (s *Service) Export(accountID, id, dir string) error {
	s.data.Lock()
	entry, err := s.find(accountID, id)
	s.data.Unlock()
	if err != nil {
		return err
	}
	if dir == "" {
		return errors.New("export directory is required")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	items := []struct {
		name string
		data []byte
	}{{id + ".fullchain.pem", entry.PEM}, {id + ".key.pem", entry.PrivateKey}}
	var created []string
	for _, item := range items {
		path := filepath.Join(dir, item.name)
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			for _, p := range created {
				_ = os.Remove(p)
			}
			return e
		}
		created = append(created, path)
		_, e = f.Write(item.data)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			for _, p := range created {
				_ = os.Remove(p)
			}
			return e
		}
	}
	return nil
}

// beginOperation serializes issuance/recovery without making canceled queued work
// wait for an active ACME challenge to finish.
func (s *Service) beginOperation(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.operation <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-s.operation
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Service) endOperation() { <-s.operation }

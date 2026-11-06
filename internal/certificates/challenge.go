package certificates

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"strings"
	"time"
	"yuntools/internal/cloud"
)

func (s *Service) loadOwnership(accountID string) ([]cloud.Record, error) {
	var records []cloud.Record
	if err := s.read(key("dns-ownership", accountID), &records); err != nil {
		return nil, err
	}
	return records, nil
}
func (s *Service) saveOwnership(accountID string, records []cloud.Record) error {
	return s.write(key("dns-ownership", accountID), records)
}
func exactRecord(a, b cloud.Record) bool {
	return a.ID != "" && a.ID == b.ID && a.Domain == b.Domain && a.RR == b.RR && a.Type == "TXT" && a.Type == b.Type && a.Value != "" && a.Value == b.Value
}
func (s *Service) cleanup(ctx context.Context, accountID string, dns cloud.ChallengeDNS, match func(cloud.Record) bool) error {
	s.data.Lock()
	defer s.data.Unlock()
	records, err := s.loadOwnership(accountID)
	if err != nil {
		return err
	}
	var keep []cloud.Record
	var errs []error
	for _, owned := range records {
		if !match(owned) {
			keep = append(keep, owned)
			continue
		}
		if owned.ID == "" || owned.Type != "TXT" || owned.Value == "" || (owned.RR != "_acme-challenge" && !strings.HasPrefix(owned.RR, "_acme-challenge.")) {
			keep = append(keep, owned)
			errs = append(errs, errors.New("invalid ownership journal entry"))
			continue
		}
		current, e := dns.GetRecord(ctx, owned.ID)
		if errors.Is(e, cloud.ErrNotFound) {
			continue
		}
		if e != nil {
			keep = append(keep, owned)
			errs = append(errs, e)
			continue
		}
		if !exactRecord(owned, current) {
			keep = append(keep, owned)
			errs = append(errs, fmt.Errorf("owned TXT %s changed; retained", owned.ID))
			continue
		}
		if e = dns.DeleteChallengeRecord(ctx, owned); e != nil {
			keep = append(keep, owned)
			errs = append(errs, e)
		}
	}
	if err = s.saveOwnership(accountID, keep); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Cleanup recovers only persisted ownership. Unjournaled records are never discovered or deleted.
func (s *Service) Cleanup(ctx context.Context, a cloud.Account) error {
	if err := s.beginOperation(ctx); err != nil {
		return err
	}
	defer s.endOperation()
	if a.ID == "" || s.factory == nil {
		return errors.New("cloud account required")
	}
	dns, err := s.factory(a)
	if err != nil {
		return err
	}
	return s.cleanup(ctx, a.ID, dns, func(cloud.Record) bool { return true })
}

type challengeProvider struct {
	service   *Service
	accountID string
	dns       cloud.ChallengeDNS
	ctx       context.Context
	progress  func(cloud.Progress)
}

func (p *challengeProvider) Timeout() (time.Duration, time.Duration) {
	return 5 * time.Minute, 5 * time.Second
}
func (p *challengeProvider) Present(domain, token, keyAuth string) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	info := dns01.GetChallengeInfo(domain, keyAuth)
	if info.EffectiveFQDN != info.FQDN {
		return errors.New("CNAME-delegated ACME challenges are not supported; use a directly managed challenge zone")
	}
	name := strings.TrimSuffix(info.FQDN, ".")
	domains, err := p.dns.ListDomains(p.ctx)
	if err != nil {
		return err
	}
	zone := ""
	for _, d := range domains {
		if strings.HasSuffix(name, "."+d.Name) && len(d.Name) > len(zone) {
			zone = d.Name
		}
	}
	if zone == "" {
		return errors.New("no managed DNS zone matches the certificate domain")
	}
	r := cloud.Record{Domain: zone, RR: strings.TrimSuffix(name, "."+zone), Type: "TXT", Value: info.Value, TTL: 600, Line: "default"}
	return p.createAndJournal(r)
}
func (p *challengeProvider) createAndJournal(r cloud.Record) error {
	p.service.data.Lock()
	defer p.service.data.Unlock()
	owned, err := p.service.loadOwnership(p.accountID)
	if err != nil {
		return err
	}
	emit(p.progress, "创建临时 DNS TXT", 0, 0)
	created, err := p.dns.CreateChallengeRecord(p.ctx, r)
	if err != nil {
		return err
	}
	if created.ID == "" || created.Domain != r.Domain || created.RR != r.RR || created.Type != r.Type || created.Value != r.Value {
		return errors.New("DNS create response cannot establish exact ownership; record retained")
	}
	// If persistence fails, leave the record in place. A later run must never infer ownership.
	if err = p.service.saveOwnership(p.accountID, append(owned, created)); err != nil {
		return fmt.Errorf("ownership journal could not be saved; TXT retained for manual inspection: %w", err)
	}
	emit(p.progress, "等待 DNS TXT 传播及 ACME 验证", 0, 0)
	return nil
}
func (p *challengeProvider) CleanUp(domain, token, keyAuth string) error {
	sum := sha256.Sum256([]byte(keyAuth))
	value := base64.RawURLEncoding.EncodeToString(sum[:])
	name := "_acme-challenge." + strings.TrimSuffix(strings.TrimPrefix(domain, "*."), ".")
	// Cleanup gets an independent bounded context so cancellation does not leak an owned TXT.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	emit(p.progress, "清理已登记的临时 TXT", 0, 0)
	return p.service.cleanup(ctx, p.accountID, p.dns, func(r cloud.Record) bool { return r.RR+"."+r.Domain == name && r.Value == value })
}
func (p *challengeProvider) checkPropagation(domain, fqdn, value string, check dns01.PreCheckFunc) (bool, error) {
	if err := p.ctx.Err(); err != nil {
		return true, err
	}
	emit(p.progress, "检查 DNS TXT 传播", 0, 0)
	return check(fqdn, value)
}

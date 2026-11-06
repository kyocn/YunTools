package aliyun

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
	"yuntools/internal/cloud"
)

var domainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validDomain(s string) bool {
	if len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if !domainPattern.MatchString(label) {
			return false
		}
	}
	return true
}
func (c *Client) ListDomains(ctx context.Context) ([]cloud.Domain, error) {
	var out []cloud.Domain
	for page := 1; ; page++ {
		var r struct {
			TotalCount int
			Domains    struct {
				Domain []struct {
					DomainName, DomainId string
					CreateTimestamp      int64
				}
			}
		}
		err := c.call(ctx, "dns", "DescribeDomains", map[string]string{"PageNumber": strconv.Itoa(page), "PageSize": "100"}, &r)
		if err != nil {
			return nil, err
		}
		for _, d := range r.Domains.Domain {
			out = append(out, cloud.Domain{Name: d.DomainName, ID: d.DomainId, Created: parseCloudMillis(d.CreateTimestamp)})
		}
		if len(r.Domains.Domain) == 0 || len(out) >= r.TotalCount {
			break
		}
	}
	return cloud.NewestFirst(out, func(d cloud.Domain) time.Time { return d.Created }), nil
}
func (c *Client) AddDomain(ctx context.Context, name string) error {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if !validDomain(name) {
		return errors.New("invalid DNS zone name (use ASCII/punycode)")
	}
	domains, err := c.ListDomains(ctx)
	if err != nil {
		return err
	}
	for _, d := range domains {
		if strings.EqualFold(d.Name, name) {
			return nil
		}
	}
	var r struct{ DomainId string }
	return c.call(ctx, "dns", "AddDomain", map[string]string{"DomainName": name}, &r)
}

type dnsRecord struct {
	CreateTimestamp, UpdateTimestamp            int64
	RecordId, DomainName, RR, Type, Value, Line string
	TTL, Priority                               int
}

func (d dnsRecord) record() cloud.Record {
	return cloud.Record{Created: parseCloudMillis(d.CreateTimestamp), Updated: parseCloudMillis(d.UpdateTimestamp), ID: d.RecordId, Domain: d.DomainName, RR: d.RR, Type: d.Type, Value: d.Value, Line: d.Line, TTL: d.TTL, Priority: d.Priority}
}
func (c *Client) ListRecords(ctx context.Context, domain string) ([]cloud.Record, error) {
	if !validDomain(domain) {
		return nil, errors.New("invalid DNS zone")
	}
	var out []cloud.Record
	for page := 1; ; page++ {
		var r struct {
			TotalCount    int
			DomainRecords struct{ Record []dnsRecord }
		}
		err := c.call(ctx, "dns", "DescribeDomainRecords", map[string]string{"DomainName": domain, "PageNumber": strconv.Itoa(page), "PageSize": "500"}, &r)
		if err != nil {
			return nil, err
		}
		for _, d := range r.DomainRecords.Record {
			d.DomainName = domain
			out = append(out, d.record())
		}
		if len(r.DomainRecords.Record) == 0 || len(out) >= r.TotalCount {
			break
		}
	}
	return cloud.NewestFirst(out, func(r cloud.Record) time.Time { return r.Created }), nil
}
func normalizeRecord(r cloud.Record) (cloud.Record, error) {
	r.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(r.Domain), "."))
	r.Type = strings.ToUpper(strings.TrimSpace(r.Type))
	r.RR = strings.TrimSpace(r.RR)
	if !validDomain(r.Domain) || r.RR == "" || strings.ContainsAny(r.RR, " \t\n/\\") || r.Value == "" || strings.ContainsAny(r.Value, "\r\n") {
		return r, errors.New("invalid DNS record")
	}
	if r.RR != "@" {
		for _, label := range strings.Split(r.RR, ".") {
			if label == "*" {
				continue
			}
			if !domainPattern.MatchString(strings.ReplaceAll(strings.ToLower(label), "_", "a")) {
				return r, errors.New("invalid DNS record name")
			}
		}
	}
	switch r.Type {
	case "A", "AAAA":
		ip, err := netip.ParseAddr(r.Value)
		if err != nil || (r.Type == "A" && !ip.Is4()) || (r.Type == "AAAA" && !ip.Is6()) {
			return r, errors.New("address does not match DNS record type")
		}
	case "CNAME", "NS", "MX":
		if !validDomain(strings.TrimSuffix(r.Value, ".")) {
			return r, errors.New("invalid record destination")
		}
		if r.Type == "MX" && (r.Priority < 1 || r.Priority > 50) {
			return r, errors.New("MX priority must be 1–50")
		}
	case "TXT", "SRV", "CAA":
	default:
		return r, errors.New("unsupported DNS record type")
	}
	if r.TTL == 0 {
		r.TTL = 600
	}
	if r.TTL < 1 || r.TTL > 86400 {
		return r, errors.New("TTL out of range")
	}
	if r.Line == "" {
		r.Line = "default"
	}
	return r, nil
}
func (c *Client) AddRecord(ctx context.Context, record cloud.Record) (cloud.Record, error) {
	r, err := normalizeRecord(record)
	if err != nil {
		return r, err
	}
	existing, err := c.ListRecords(ctx, r.Domain)
	if err != nil {
		return r, err
	}
	for _, e := range existing {
		line := e.Line
		if line == "" {
			line = "default"
		}
		if e.RR == r.RR && e.Type == r.Type && e.Value == r.Value && line == r.Line && e.Priority == r.Priority {
			return e, nil
		}
	}
	p := map[string]string{"DomainName": r.Domain, "RR": r.RR, "Type": r.Type, "Value": r.Value, "TTL": strconv.Itoa(r.TTL), "Line": r.Line}
	if r.Type == "MX" {
		p["Priority"] = strconv.Itoa(r.Priority)
	}
	var result struct{ RecordId string }
	if err = c.call(ctx, "dns", "AddDomainRecord", p, &result); err != nil {
		return r, err
	}
	if result.RecordId == "" {
		return r, errors.New("cloud did not return the record ID; inspect DNS before retry")
	}
	r.ID = result.RecordId
	return r, nil
}
func (c *Client) GetRecord(ctx context.Context, id string) (cloud.Record, error) {
	if id == "" {
		return cloud.Record{}, errors.New("record ID required")
	}
	var r dnsRecord
	err := c.call(ctx, "dns", "DescribeDomainRecordInfo", map[string]string{"RecordId": id}, &r)
	return r.record(), err
}
func (c *Client) DeleteChallengeRecord(ctx context.Context, owned cloud.Record) error {
	if owned.ID == "" || owned.Type != "TXT" || owned.Value == "" || (owned.RR != "_acme-challenge" && !strings.HasPrefix(owned.RR, "_acme-challenge.")) {
		return errors.New("not an owned ACME TXT record")
	}
	current, err := c.GetRecord(ctx, owned.ID)
	if err != nil {
		return err
	}
	if current.ID != owned.ID || current.Domain != owned.Domain || current.RR != owned.RR || current.Type != owned.Type || current.Value != owned.Value {
		return fmt.Errorf("record %s changed; cleanup refused", owned.ID)
	}
	var result struct{ RecordId string }
	return c.call(ctx, "dns", "DeleteDomainRecord", map[string]string{"RecordId": owned.ID}, &result)
}

// CreateChallengeRecord creates a fresh TXT and never adopts a deduplicated record.
// A timeout is deliberately not retried: ownership is unknown without a returned ID.
func (c *Client) CreateChallengeRecord(ctx context.Context, record cloud.Record) (cloud.Record, error) {
	r, err := normalizeRecord(record)
	if err != nil {
		return r, err
	}
	if r.Type != "TXT" || (r.RR != "_acme-challenge" && !strings.HasPrefix(r.RR, "_acme-challenge.")) {
		return r, errors.New("only ACME TXT creation is allowed")
	}
	existing, err := c.ListRecords(ctx, r.Domain)
	if err != nil {
		return r, err
	}
	for _, e := range existing {
		if e.RR == r.RR && e.Type == r.Type && e.Value == r.Value {
			return r, errors.New("matching TXT already exists; refusing to claim ownership")
		}
	}
	var result struct{ RecordId string }
	err = c.call(ctx, "dns", "AddDomainRecord", map[string]string{"DomainName": r.Domain, "RR": r.RR, "Type": "TXT", "Value": r.Value, "TTL": strconv.Itoa(r.TTL), "Line": r.Line}, &result)
	if err != nil {
		return r, err
	}
	if result.RecordId == "" {
		return r, errors.New("created record ID unknown; manual inspection required")
	}
	r.ID = result.RecordId
	return r, nil
}

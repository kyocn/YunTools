package tencent

import (
	"context"
	"errors"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
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
	case "A":
		ip, err := netip.ParseAddr(strings.TrimSpace(r.Value))
		if err != nil || !ip.Is4() {
			return r, errors.New("A record requires a valid IPv4 address")
		}
		r.Value = ip.String()
	case "AAAA":
		ip, err := netip.ParseAddr(strings.TrimSpace(r.Value))
		if err != nil || !ip.Is6() {
			return r, errors.New("AAAA record requires a valid IPv6 address")
		}
		r.Value = ip.String()
	case "CNAME", "MX", "NS", "TXT", "CAA", "SRV":
	default:
		return r, errors.New("unsupported DNS record type")
	}
	if r.TTL <= 0 {
		r.TTL = 600
	}
	if r.Line == "" {
		r.Line = "默认"
	}
	return r, nil
}

func (c *Client) ListDomains(ctx context.Context) ([]cloud.Domain, error) {
	dc, err := c.dnsClient()
	if err != nil {
		return nil, err
	}
	var out []cloud.Domain
	var offset int64
	const limit int64 = 100
	for {
		req := dnspod.NewDescribeDomainListRequest()
		req.Offset = common.Int64Ptr(offset)
		req.Limit = common.Int64Ptr(limit)
		resp, err := dc.DescribeDomainListWithContext(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp.Response == nil || len(resp.Response.DomainList) == 0 {
			break
		}
		for _, d := range resp.Response.DomainList {
			out = append(out, cloud.Domain{
				Name:    strVal(d.Name),
				ID:      strconv.FormatUint(u64Val(d.DomainId), 10),
				Created: parseTencentTime(strVal(d.CreatedOn)),
			})
		}
		total := uint64(0)
		if resp.Response.DomainCountInfo != nil && resp.Response.DomainCountInfo.AllTotal != nil {
			total = *resp.Response.DomainCountInfo.AllTotal
		}
		offset += int64(len(resp.Response.DomainList))
		if int64(len(resp.Response.DomainList)) < limit || (total > 0 && uint64(offset) >= total) {
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
	dc, err := c.dnsClient()
	if err != nil {
		return err
	}
	req := dnspod.NewCreateDomainRequest()
	req.Domain = common.StringPtr(name)
	_, err = dc.CreateDomainWithContext(ctx, req)
	return err
}

func (c *Client) ListRecords(ctx context.Context, domain string) ([]cloud.Record, error) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if !validDomain(domain) {
		return nil, errors.New("invalid DNS zone")
	}
	dc, err := c.dnsClient()
	if err != nil {
		return nil, err
	}
	var out []cloud.Record
	var offset uint64
	const limit uint64 = 100
	for {
		req := dnspod.NewDescribeRecordListRequest()
		req.Domain = common.StringPtr(domain)
		req.Offset = common.Uint64Ptr(offset)
		req.Limit = common.Uint64Ptr(limit)
		resp, err := dc.DescribeRecordListWithContext(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp.Response == nil || len(resp.Response.RecordList) == 0 {
			break
		}
		for _, item := range resp.Response.RecordList {
			out = append(out, cloud.Record{
				ID:       strconv.FormatUint(u64Val(item.RecordId), 10),
				Domain:   domain,
				RR:       strVal(item.Name),
				Type:     strVal(item.Type),
				Value:    strVal(item.Value),
				Line:     strVal(item.Line),
				TTL:      int(u64Val(item.TTL)),
				Priority: int(u64Val(item.MX)),
				Updated:  parseTencentTime(strVal(item.UpdatedOn)),
			})
		}
		total := uint64(0)
		if resp.Response.RecordCountInfo != nil && resp.Response.RecordCountInfo.TotalCount != nil {
			total = *resp.Response.RecordCountInfo.TotalCount
		}
		offset += uint64(len(resp.Response.RecordList))
		if uint64(len(resp.Response.RecordList)) < limit || (total > 0 && offset >= total) {
			break
		}
	}
	return cloud.NewestFirst(out, func(r cloud.Record) time.Time { return r.Updated }), nil
}

func (c *Client) AddRecord(ctx context.Context, r cloud.Record) (cloud.Record, error) {
	r, err := normalizeRecord(r)
	if err != nil {
		return cloud.Record{}, err
	}
	dc, err := c.dnsClient()
	if err != nil {
		return cloud.Record{}, err
	}
	req := dnspod.NewCreateRecordRequest()
	req.Domain = common.StringPtr(r.Domain)
	req.SubDomain = common.StringPtr(r.RR)
	req.RecordType = common.StringPtr(r.Type)
	req.RecordLine = common.StringPtr(r.Line)
	req.Value = common.StringPtr(r.Value)
	req.TTL = common.Uint64Ptr(uint64(r.TTL))
	if r.Type == "MX" && r.Priority > 0 {
		req.MX = common.Uint64Ptr(uint64(r.Priority))
	}
	resp, err := dc.CreateRecordWithContext(ctx, req)
	if err != nil {
		return cloud.Record{}, err
	}
	if resp.Response != nil && resp.Response.RecordId != nil {
		r.ID = strconv.FormatUint(*resp.Response.RecordId, 10)
	}
	return r, nil
}

func (c *Client) CreateChallengeRecord(ctx context.Context, r cloud.Record) (cloud.Record, error) {
	r.Type = "TXT"
	if r.TTL <= 0 {
		r.TTL = 600
	}
	created, err := c.AddRecord(ctx, r)
	if err != nil {
		return cloud.Record{}, err
	}
	if created.ID != "" {
		created.ID = created.Domain + "|" + created.ID
	}
	return created, nil
}

func splitChallengeID(domain, id string) (string, uint64, error) {
	if strings.Contains(id, "|") {
		parts := strings.SplitN(id, "|", 2)
		domain = parts[0]
		id = parts[1]
	}
	num, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
	if err != nil || domain == "" {
		return "", 0, errors.New("invalid DNSPod record identifier")
	}
	return domain, num, nil
}

func (c *Client) GetRecord(ctx context.Context, id string) (cloud.Record, error) {
	domain, recordID, err := splitChallengeID("", id)
	if err != nil {
		return cloud.Record{}, err
	}
	dc, err := c.dnsClient()
	if err != nil {
		return cloud.Record{}, err
	}
	req := dnspod.NewDescribeRecordRequest()
	req.Domain = common.StringPtr(domain)
	req.RecordId = common.Uint64Ptr(recordID)
	resp, err := dc.DescribeRecordWithContext(ctx, req)
	if err != nil {
		return cloud.Record{}, err
	}
	if resp.Response == nil || resp.Response.RecordInfo == nil {
		return cloud.Record{}, cloud.ErrNotFound
	}
	info := resp.Response.RecordInfo
	return cloud.Record{
		ID:       id,
		Domain:   domain,
		RR:       strVal(info.SubDomain),
		Type:     strVal(info.RecordType),
		Value:    strVal(info.Value),
		Line:     strVal(info.RecordLine),
		TTL:      int(u64Val(info.TTL)),
		Priority: int(u64Val(info.MX)),
		Updated:  parseTencentTime(strVal(info.UpdatedOn)),
	}, nil
}

func (c *Client) DeleteChallengeRecord(ctx context.Context, r cloud.Record) error {
	domain, recordID, err := splitChallengeID(r.Domain, r.ID)
	if err != nil {
		return err
	}
	dc, err := c.dnsClient()
	if err != nil {
		return err
	}
	req := dnspod.NewDeleteRecordRequest()
	req.Domain = common.StringPtr(domain)
	req.RecordId = common.Uint64Ptr(recordID)
	_, err = dc.DeleteRecordWithContext(ctx, req)
	return err
}

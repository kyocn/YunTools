package tencent

import (
	"testing"

	"yuntools/internal/cloud"
)

func TestNewValidation(t *testing.T) {
	if _, err := New(cloud.Account{}); err == nil {
		t.Fatal("expected error for empty account")
	}
	c, err := New(cloud.Account{
		ID:              "tc-1",
		Provider:        cloud.ProviderTencent,
		Region:          "ap-guangzhou",
		AccessKeyID:     "AKID",
		AccessKeySecret: "SECRET",
	})
	if err != nil || c == nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeRecordAndWhitelist(t *testing.T) {
	rec, err := normalizeRecord(cloud.Record{
		Domain: "Example.COM.",
		RR:     "www",
		Type:   "a",
		Value:  "1.2.3.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Domain != "example.com" || rec.Type != "A" || rec.TTL != 600 || rec.Line != "默认" {
		t.Fatalf("unexpected normalized record: %+v", rec)
	}
	cidr, err := normalizeWhitelistCIDR("1.2.3.4")
	if err != nil || cidr != "1.2.3.4/32" {
		t.Fatalf("unexpected cidr: %q %v", cidr, err)
	}
	proto, port := targetPolicySpec(cloud.Target{Kind: "ecs", Protocol: "tcp", Ports: "22/22"})
	if proto != "TCP" || port != "22" {
		t.Fatalf("unexpected policy spec: %s %s", proto, port)
	}
	proto, port = targetPolicySpec(cloud.Target{Kind: "ecs", Protocol: "tcp", Ports: "8000/9000"})
	if proto != "TCP" || port != "8000-9000" {
		t.Fatalf("unexpected range spec: %s %s", proto, port)
	}
}

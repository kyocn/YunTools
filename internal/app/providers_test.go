package app

import (
	"errors"
	"testing"

	"yuntools/internal/cloud"
)

func TestDefaultProviderRoutingAndCapabilities(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	if a.p.Storage == nil || a.p.DNS == nil || a.p.Whitelist == nil {
		t.Fatal("expected default provider factories")
	}

	tc := cloud.Account{
		ID:              "tc-1",
		Name:            "Tencent",
		Provider:        cloud.ProviderTencent,
		Region:          "ap-guangzhou",
		AccessKeyID:     "AKID",
		AccessKeySecret: "SECRET",
	}
	if _, err := a.p.Storage(tc); err != nil {
		t.Fatalf("tencent storage failed: %v", err)
	}
	if _, err := a.p.DNS(tc); err != nil {
		t.Fatalf("tencent dns failed: %v", err)
	}
	if _, err := a.p.Whitelist(tc); err != nil {
		t.Fatalf("tencent whitelist failed: %v", err)
	}

	qn := cloud.Account{
		ID:              "qn-1",
		Name:            "Qiniu",
		Provider:        cloud.ProviderQiniu,
		Region:          "z0",
		AccessKeyID:     "AK",
		AccessKeySecret: "SK",
	}
	if _, err := a.p.Storage(qn); err != nil {
		t.Fatalf("qiniu storage failed: %v", err)
	}
	if _, err := a.p.DNS(qn); !errors.Is(err, cloud.ErrUnsupportedProviderCapability) {
		t.Fatalf("expected ErrUnsupportedProviderCapability for qiniu dns, got %v", err)
	}
	if _, err := a.p.Whitelist(qn); !errors.Is(err, cloud.ErrUnsupportedProviderCapability) {
		t.Fatalf("expected ErrUnsupportedProviderCapability for qiniu whitelist, got %v", err)
	}
}

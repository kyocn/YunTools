package qiniukodo

import (
	"testing"
	"time"

	"yuntools/internal/cloud"
)

func TestNewValidation(t *testing.T) {
	if _, err := New(cloud.Account{}); err == nil {
		t.Fatal("expected error for empty account")
	}
	c, err := New(cloud.Account{
		ID:              "qn-1",
		Provider:        cloud.ProviderQiniu,
		Region:          "z0",
		AccessKeyID:     "AKTEST",
		AccessKeySecret: "SKTEST",
	})
	if err != nil || c == nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegionAndTimestampHelpers(t *testing.T) {
	for _, id := range []string{"z0", "cn-east-2", "z1", "z2", "na0", "as0"} {
		if regionFromID(id) == nil {
			t.Fatalf("expected region for %s", id)
		}
	}
	if storageClassLabel(0) != "STANDARD" || storageClassLabel(2) != "ARCHIVE" {
		t.Fatal("unexpected storage class label")
	}
	ts := parsePutTime(17000000000000000)
	if ts.Year() < 2023 || ts.Location() == nil {
		t.Fatalf("unexpected parsed time: %v", ts)
	}
	_ = time.Second
}

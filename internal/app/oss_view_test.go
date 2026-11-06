package app

import (
	"github.com/egoist/mygo/ui"
	"math"
	"testing"
	"time"
	"yuntools/internal/cloud"
)

func TestOSSDirectoryUsesUnknownMetadataAndAlignedColumns(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	a.accountID, a.page, a.bucket = "a", "oss", "test-bucket"
	a.objects = []cloud.Object{
		{Key: "sample.bin", Size: 4096, Modified: time.Date(2026, 10, 26, 4, 50, 0, 0, time.UTC)},
		{Key: "folder/", IsDir: true},
	}
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if tt.HasText("0001-01-01") || tt.HasText("0 B") {
		t.Fatal("folder displayed invented metadata")
	}
	if !tt.HasText("2026-10-26 12:50") || !tt.HasText("北京时间") {
		t.Fatal("file time not displayed in explicitly labeled Beijing time")
	}
	for _, pair := range [][2]string{{"大小", "4.0 KB"}, {"4.0 KB", "—"}, {"最后修改（北京时间）", "2026-10-26 12:50"}, {"sample.bin", "folder/"}} {
		left, ok1 := tt.Find(pair[0])
		right, ok2 := tt.Find(pair[1])
		if !ok1 || !ok2 || math.Abs(float64(left.X-right.X)) > 1 {
			t.Fatalf("column mismatch %v: %v / %v", pair, left, right)
		}
	}
}

func TestOSSBucketHeadersAlignWithCells(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	a.accountID, a.page = "a", "oss"
	a.buckets = []cloud.Bucket{{Name: "sample-bucket", Region: "cn-shanghai", StorageClass: "Standard"}}
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	for _, pair := range [][2]string{{"地域", "cn-shanghai"}, {"存储类型", "Standard"}} {
		header, ok1 := tt.Find(pair[0])
		cell, ok2 := tt.Find(pair[1])
		if !ok1 || !ok2 || math.Abs(float64(header.X-cell.X)) > 1 {
			t.Fatalf("column mismatch %v: %v / %v", pair, header, cell)
		}
	}
}

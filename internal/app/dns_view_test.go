package app

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
)

func dnsTableFixture() *App {
	a := New(Platform{Store: &memStore{}})
	a.accountID, a.page, a.domain = "dns-test", "dns", "example.com"
	a.profiles = []cloud.Account{{ID: "dns-test", Name: "示例账号", Provider: "aliyun"}}
	a.records = []cloud.Record{
		{ID: "one", RR: "official-account-assistant", Type: "A", Value: "192.0.2.10", TTL: 600},
		{ID: "two", RR: "@", Type: "MX", Value: "mail.example.com", TTL: 3600},
		{ID: "three", RR: "_acme-challenge.production-service-with-a-long-name", Type: "TXT", Value: strings.Repeat("example-verification-value-", 8), TTL: 600},
	}
	return a
}

func TestDNSRecordColumnsStayAlignedAndSingleLine(t *testing.T) {
	for _, width := range []int{960, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := dnsTableFixture()
			defer a.Close()
			tt := ui.NewTester(a.View, width, 820)
			tt.Frame()
			for _, pair := range [][2]string{{"主机记录", a.records[0].RR}, {"类型", "A"}, {"记录值", a.records[0].Value}, {"缓存时间 (TTL)", "600 秒"}} {
				header, ok1 := tt.Find(pair[0])
				cell, ok2 := tt.Find(pair[1])
				if !ok1 || !ok2 || math.Abs(float64(header.X-cell.X)) > 1 {
					t.Fatalf("column mismatch %v: %v / %v", pair, header, cell)
				}
			}
			for _, record := range a.records {
				for _, text := range []string{record.RR, record.Value} {
					bounds, ok := tt.Find(text)
					if !ok || bounds.H > 24 || bounds.X+bounds.W > float32(width-20) {
						t.Fatalf("record wraps or escapes table: %q %v", text, bounds)
					}
				}
			}
			name, _ := tt.Find(a.records[0].RR)
			if name.W < 190 {
				t.Fatalf("host name column remains cramped: %v", name)
			}
			for _, forbidden := range []string{"删除解析", "修改解析", "禁用解析"} {
				if tt.HasText(forbidden) {
					t.Fatalf("unexpected mutating action %q", forbidden)
				}
			}
		})
	}
}

func TestDNSRecordTableVisualScreens(t *testing.T) {
	dir := os.Getenv("YUNTOOLS_RENDER_DIR")
	if dir == "" {
		t.Skip("visual output not requested")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{true, false} {
		for _, width := range []int{960, 1280} {
			a := dnsTableFixture()
			a.dark = dark
			tt := ui.NewTester(a.View, width, 820)
			tt.Frame()
			file, err := os.Create(filepath.Join(dir, fmt.Sprintf("dns-records-%t-%d.png", dark, width)))
			if err != nil {
				a.Close()
				t.Fatal(err)
			}
			err = png.Encode(file, tt.Image())
			file.Close()
			a.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestDNSRecordTableRespectsPaginationAndSearch(t *testing.T) {
	a := dnsTableFixture()
	defer a.Close()
	a.records = nil
	for i := 1; i <= 21; i++ {
		a.records = append(a.records, cloud.Record{ID: fmt.Sprint(i), RR: fmt.Sprintf("host-%02d", i), Type: "A", Value: "192.0.2.10", TTL: 600})
	}
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if tt.HasText("host-21") {
		t.Fatal("record from next page shown")
	}
	if err := tt.Click("下一页"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("host-21") || tt.HasText("host-01") {
		t.Fatal("record page did not change")
	}
	a.search = "host-01"
	tt.Frame()
	if !tt.HasText("host-01") || tt.HasText("host-21") || !tt.HasText("共 1 项") {
		t.Fatal("search did not filter and reset pagination")
	}
}

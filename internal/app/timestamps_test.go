package app

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
)

func TestFormatTimestampUsesBeijingAndMissingPlaceholder(t *testing.T) {
	if got := formatTimestamp(time.Date(2026, 11, 6, 20, 4, 0, 0, time.UTC)); got != "2026-11-07 04:04" {
		t.Fatal(got)
	}
	if got := formatTimestamp(time.Time{}); got != "—" {
		t.Fatal(got)
	}
}

func TestSecurityRulesTimeColumnsAndNewestBeforePaging(t *testing.T) {
	for _, width := range []int{960, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := New(Platform{Store: &memStore{}})
			defer a.Close()
			a.accountID = "a"
			d := &ruleDetails{Target: cloud.Target{AccountID: "a", ID: "sg-test", Kind: "ecs", Name: "测试安全组"}, Direction: "全部方向"}
			d.Rules = append(d.Rules, cloud.SecurityGroupRule{ID: "unknown"})
			for i := 0; i < 21; i++ {
				d.Rules = append(d.Rules, cloud.SecurityGroupRule{ID: fmt.Sprintf("rule-%02d", i), Direction: "ingress", Policy: "Accept", Protocol: "TCP", Ports: "22/22", Source: "192.0.2.1/32", Priority: "1", Description: "服务器登录", Created: time.Date(2026, 11, 6, i, 0, 0, 0, time.UTC)})
			}
			a.ruleDetail = d
			tt := ui.NewTester(a.View, width, 820)
			tt.Frame()
			if !tt.HasText("rule-20") || tt.HasText("rule-00") || tt.HasText("unknown") {
				t.Fatal("rules not sorted before pagination")
			}
			for _, pair := range [][2]string{{"创建时间", "2026-11-07 04:00"}, {"描述 / 规则 ID", "rule-20"}, {"来源 / 目标", "192.0.2.1/32"}} {
				header, ok1 := tt.Find(pair[0])
				cell, ok2 := tt.Find(pair[1])
				if !ok1 || !ok2 || math.Abs(float64(header.X-cell.X)) > 1 {
					t.Fatalf("column mismatch %v: %v / %v", pair, header, cell)
				}
				if cell.W < 50 || cell.X+cell.W > float32(width-15) {
					t.Fatalf("column escapes/clips %v: %v", pair, cell)
				}
			}
			if err := tt.Click("下一页"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if !tt.HasText("rule-00") || !tt.HasText("unknown") {
				t.Fatal("missing oldest/unknown rules on last page")
			}
		})
	}
}

func TestBucketCreationColumnsAndNewestBeforePaging(t *testing.T) {
	for _, width := range []int{960, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := New(Platform{Store: &memStore{}})
			defer a.Close()
			a.accountID, a.page = "a", "oss"
			a.buckets = append(a.buckets, cloud.Bucket{Name: "unknown"})
			for i := 0; i < 21; i++ {
				a.buckets = append(a.buckets, cloud.Bucket{Name: fmt.Sprintf("bucket-%02d", i), Region: "cn-shanghai", StorageClass: "Standard", Created: time.Date(2026, 11, 6, i, 0, 0, 0, time.UTC)})
			}
			tt := ui.NewTester(a.View, width, 820)
			tt.Frame()
			if !tt.HasText("bucket-20") || tt.HasText("bucket-00") || tt.HasText("unknown") {
				t.Fatal("buckets not sorted before pagination")
			}
			header, ok1 := tt.Find("创建时间（北京时间）")
			cell, ok2 := tt.Find("2026-11-07 04:00")
			if !ok1 || !ok2 || math.Abs(float64(header.X-cell.X)) > 1 || cell.X+cell.W > float32(width-20) {
				t.Fatalf("time column mismatch: %v / %v", header, cell)
			}
			if err := tt.Click("下一页"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if !tt.HasText("bucket-00") || !tt.HasText("unknown") || !tt.HasText("—") {
				t.Fatal("oldest/missing bucket timestamps not on final page")
			}
		})
	}
}

func TestDNSCreationAndUpdateTimeColumns(t *testing.T) {
	for _, width := range []int{960, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := dnsTableFixture()
			defer a.Close()
			a.records[0].Created = time.Date(2026, 11, 5, 1, 2, 0, 0, time.UTC)
			a.records[0].Updated = time.Date(2026, 11, 6, 3, 4, 0, 0, time.UTC)
			tt := ui.NewTester(a.View, width, 820)
			tt.Frame()
			header, ok1 := tt.Find("创建 / 更新（北京时间）")
			for _, value := range []string{"2026-11-05 09:02", "2026-11-06 11:04"} {
				cell, ok2 := tt.Find(value)
				if !ok1 || !ok2 || math.Abs(float64(header.X-cell.X)) > 1 || cell.X+cell.W > float32(width-20) {
					t.Fatalf("time column mismatch: %v / %v", header, cell)
				}
			}
			if !tt.HasText("—") || tt.HasText("0001-01-01") {
				t.Fatal("missing time not marked unknown")
			}
		})
	}
}

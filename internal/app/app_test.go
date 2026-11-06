package app

import (
	"context"
	"fmt"
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"yuntools/internal/cloud"
)

type memStore struct {
	sync.Mutex
	d map[string][]byte
}

func (s *memStore) Get(k string) ([]byte, error) {
	s.Lock()
	defer s.Unlock()
	v, ok := s.d[k]
	if !ok {
		return nil, cloud.ErrNotFound
	}
	return append([]byte(nil), v...), nil
}
func (s *memStore) Put(k string, b []byte) error {
	s.Lock()
	defer s.Unlock()
	if s.d == nil {
		s.d = map[string][]byte{}
	}
	s.d[k] = append([]byte(nil), b...)
	return nil
}
func (s *memStore) Delete(k string) error { s.Lock(); defer s.Unlock(); delete(s.d, k); return nil }

type fakeOSS struct {
	cloud.ObjectStorage
	deletes int
}

func (f *fakeOSS) DeleteObjects(context.Context, string, []string) error { f.deletes++; return nil }
func TestEmptyAppHasRealOnboarding(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	tt := ui.NewTester(a.View, 1200, 760)
	tt.Frame()
	for _, s := range []string{"YunTools", "对象存储", "访问白名单", "域名解析", "SSL 证书", "添加云账号"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q", s)
		}
	}
}
func TestDeleteRequiresSecondConfirmation(t *testing.T) {
	f := &fakeOSS{}
	a := New(Platform{Store: &memStore{}, Storage: func(cloud.Account) (cloud.ObjectStorage, error) { return f, nil }})
	defer a.Close()
	a.profiles = []cloud.Account{{ID: "a", Name: "test", Provider: "aliyun", Region: "cn-hangzhou"}}
	a.accountID = "a"
	a.bucket = "bucket"
	a.objects = []cloud.Object{{Key: "file.txt"}}
	a.selected = map[string]bool{"file.txt": true}
	a.requestDelete()
	if a.confirm == nil {
		t.Fatal("missing confirmation")
	}
	if f.deletes != 0 {
		t.Fatal("deleted before confirmation")
	}
	a.confirm = nil
	if f.deletes != 0 {
		t.Fatal("cancel deleted")
	}
}
func TestSwitchDiscardsStaleResults(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	generation := a.generation
	a.switchAccount("b")
	applied := false
	a.applyResult(generation, func() { applied = true })
	if applied {
		t.Fatal("stale account data applied")
	}
}
func TestDNSHasNoDestructiveControls(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	a.page = "dns"
	a.profiles = []cloud.Account{{ID: "a", Name: "test"}}
	a.accountID = "a"
	tt := ui.NewTester(a.View, 1200, 760)
	tt.Frame()
	for _, s := range []string{"删除解析", "修改解析", "禁用解析"} {
		if tt.HasText(s) {
			t.Fatal("forbidden control", s)
		}
	}
}
func TestSwitchClosesAccountBoundDialogs(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	a.accountID = "a"
	a.dialog = &form{Account: cloud.Account{ID: "a"}}
	a.confirm = &confirmation{Title: "delete a"}
	a.switchAccount("b")
	if a.dialog != nil || a.confirm != nil {
		t.Fatal("old-account dialogs survived switch")
	}
}

type recordingDNS struct {
	cloud.ChallengeDNS
	records chan cloud.Record
}

func (d *recordingDNS) AddRecord(_ context.Context, r cloud.Record) (cloud.Record, error) {
	d.records <- r
	return r, nil
}
func TestSubmittedFormIsImmutable(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	d := &recordingDNS{records: make(chan cloud.Record, 1)}
	a := New(Platform{Store: &memStore{}, DNS: func(cloud.Account) (cloud.ChallengeDNS, error) { close(entered); <-release; return d, nil }})
	defer a.Close()
	a.domain = "example.com"
	a.dialog = &form{Kind: "record", Account: cloud.Account{ID: "a"}, Fields: map[string]string{"rr": "www", "type": "A", "value": "192.0.2.1", "ttl": "600"}}
	a.saveForm()
	<-entered
	a.dialog.Fields["value"] = "192.0.2.99"
	close(release)
	select {
	case r := <-d.records:
		if r.Value != "192.0.2.1" {
			t.Fatalf("submitted record changed: %s", r.Value)
		}
	case <-time.After(time.Second):
		t.Fatal("request not sent")
	}
}

func TestAccountEditInvalidatesExistingContext(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	p := cloud.Account{ID: "a", Name: "account", Region: "cn-shanghai"}
	a.accountID = "a"
	a.region = "cn-hangzhou"
	a.bucket = "old"
	generation := a.generation
	a.accountSaved(p, []cloud.Account{p})
	if a.generation == generation || a.bucket != "" || a.region != "cn-shanghai" {
		t.Fatal("old credential context survived edit")
	}
}
func TestCopiedContentDoesNotDependOnMultipartETag(t *testing.T) {
	source := cloud.Object{Size: 300 << 20, ETag: "original-parts", CRC64: "123456"}
	dest := cloud.Object{Size: 300 << 20, ETag: "new-parts", CRC64: "123456"}
	if !sameContent(source, dest) {
		t.Fatal("valid multipart copy rejected")
	}
	dest.CRC64 = "654321"
	if sameContent(source, dest) {
		t.Fatal("different content accepted")
	}
}

type blockedCertificateStore struct {
	*memStore
	release chan struct{}
}

func (s *blockedCertificateStore) Get(k string) ([]byte, error) {
	if strings.HasPrefix(k, "certificates/issued/") {
		<-s.release
	}
	return s.memStore.Get(k)
}
func TestRenewalLookupNeverBlocksUI(t *testing.T) {
	s := &blockedCertificateStore{&memStore{}, make(chan struct{})}
	a := New(Platform{Store: s})
	defer a.Close()
	a.profiles = []cloud.Account{{ID: "a"}}
	a.autoRenew = map[string]bool{"a/cert": true}
	returned := make(chan struct{})
	go func() { a.renewalTick(); close(returned) }()
	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Error("renewal lookup blocked UI")
	}
	close(s.release)
	<-returned
}

// Set YUNTOOLS_RENDER_DIR for reproducible visual QA; these are explicitly test fixtures.
func TestVisualScreens(t *testing.T) {
	dir := os.Getenv("YUNTOOLS_RENDER_DIR")
	if dir == "" {
		t.Skip("visual output not requested")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	for _, dark := range []bool{true, false} {
		for _, page := range []string{"overview", "oss", "oss-objects", "settings", "dns", "whitelist", "rule-details", "long-notice", "account-form"} {
			a := New(Platform{Store: &memStore{}, Version: "0.2.0", UpdateStatus: func() UpdateStatus {
				return UpdateStatus{Enabled: true, Automatic: true, Repository: "kyocn/YunTools"}
			}, CheckUpdates: func() {}, SetAutomaticUpdates: func(bool) {}})
			a.dark = dark
			a.page = page
			a.accountID = "visual-test"
			a.profiles = []cloud.Account{{ID: "visual-test", Name: "示例账号（视觉测试）", Provider: "aliyun", Region: "cn-hangzhou"}}
			a.tabs = []tab{{"visual-test", "overview", "概览"}, {"visual-test", "oss", "对象存储"}, {"visual-test", "dns", "域名解析"}}
			a.activeTab = 0
			for i, tab := range a.tabs {
				if tab.Page == page {
					a.activeTab = i
				}
			}
			a.buckets = []cloud.Bucket{{Name: "example-assets", Region: "cn-hangzhou", StorageClass: "Standard"}, {Name: "example-backups", Region: "cn-shanghai", StorageClass: "Standard"}}
			a.domains = []cloud.Domain{{Name: "example.com"}}
			if page == "rule-details" {
				a.page = "whitelist"
				a.ruleDetail = &ruleDetails{Target: cloud.Target{AccountID: "visual-test", ID: "sg-example", Name: "示例安全组", Region: "cn-shanghai", Kind: "ecs"}, AccountName: "示例账号（视觉测试）", Direction: "全部方向"}
				for i := 0; i < 25; i++ {
					a.ruleDetail.Rules = append(a.ruleDetail.Rules, cloud.SecurityGroupRule{ID: fmt.Sprintf("sgr-example-%02d", i), Direction: "ingress", Policy: "Accept", Protocol: "TCP", Ports: "22/22", Source: fmt.Sprintf("192.0.2.%d/32", i+1), Priority: "1", Description: "允许管理员从指定地址登录服务器"})
				}
				a.ruleDetail.Rules[0].Direction = "egress"
				a.ruleDetail.Rules[0].Destination = "0.0.0.0/0"
				a.ruleDetail.Rules[0].Policy = "Drop"
			}
			if page == "long-notice" {
				a.page = "overview"
				a.notice = strings.Repeat("这是一条较长的提示，用于验证滚动和裁剪，不应遮挡页面内容。\n", 30)
			}
			if page == "oss-objects" {
				a.page, a.bucket = "oss", "example-assets"
				a.activeTab = 1
				a.objects = []cloud.Object{
					{Key: "FileServer", Size: 13946060, Modified: time.Date(2026, 10, 26, 4, 50, 0, 0, time.UTC)},
					{Key: "_.example.com/", IsDir: true},
					{Key: "documents/", IsDir: true},
					{Key: "release-package-with-a-very-long-name-2026-11-06.tar.gz", Size: 20728832, Modified: time.Date(2026, 11, 6, 4, 12, 0, 0, time.UTC)},
				}
			}
			if page == "account-form" {
				a.page = "accounts"
				a.editAccount(cloud.Account{})
			}
			for _, width := range []int{960, 1280} {
				tt := ui.NewTester(a.View, width, 820)
				tt.Frame()
				f, e := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%t-%d.png", page, dark, width)))
				if e != nil {
					t.Fatal(e)
				}
				e = png.Encode(f, tt.Image())
				f.Close()
				if e != nil {
					t.Fatal(e)
				}
			}
			a.Close()
		}
	}
}

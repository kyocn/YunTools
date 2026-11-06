package app

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
)

type linkStorage struct {
	cloud.ObjectStorage
	link func(context.Context, string, string, time.Duration) (cloud.ObjectLink, error)
}

func (s linkStorage) ObjectLink(ctx context.Context, bucket, key string, expiry time.Duration) (cloud.ObjectLink, error) {
	return s.link(ctx, bucket, key, expiry)
}

func newLinkTestApp(t *testing.T, p Platform) *App {
	t.Helper()
	p.Store = &memStore{}
	a := New(p)
	t.Cleanup(a.Close)
	a.profiles = []cloud.Account{{ID: "a", Name: "fixture", Region: "cn-hangzhou"}, {ID: "b", Name: "other"}}
	a.accountID, a.page, a.bucket = "a", "oss", "bucket"
	a.buckets = []cloud.Bucket{{Name: "bucket", Region: "oss-cn-shanghai"}}
	a.objects = []cloud.Object{{Key: "folder/中文 space #?.txt"}, {Key: "directory/", IsDir: true}}
	return a
}

func TestObjectLinkCopyPreservesObjectScopeAndDefault(t *testing.T) {
	copied := ""
	a := newLinkTestApp(t, Platform{
		CopyText: func(s string) error { copied = s; return nil },
		Storage: func(account cloud.Account) (cloud.ObjectStorage, error) {
			if account.ID != "a" || account.Region != "cn-shanghai" {
				t.Error("wrong storage account")
			}
			return linkStorage{link: func(ctx context.Context, bucket, key string, expiry time.Duration) (cloud.ObjectLink, error) {
				if bucket != "bucket" || key != "folder/中文 space #?.txt" || expiry != 300*time.Second {
					t.Error("wrong link object scope or default expiry")
				}
				return cloud.ObjectLink{URL: "https://fixture.invalid/signed?token=sensitive", Signed: true, ExpiresAt: time.Now().Add(expiry)}, nil
			}}, nil
		},
	})
	a.openObjectLink(a.objects[0])
	if a.dialog == nil {
		t.Fatal("missing form")
	}
	a.copyObjectLink(*a.dialog)
	a.workers.Wait()
	a.drain()
	if copied != "https://fixture.invalid/signed?token=sensitive" || a.dialog != nil || a.notice == "" {
		t.Fatal("copy did not complete")
	}
	if strings.Contains(a.notice, "sensitive") || strings.Contains(a.err, "sensitive") {
		t.Fatal("signed URL leaked to UI feedback")
	}
}

func TestObjectLinkRejectsExpiryAndClipboardFailures(t *testing.T) {
	for _, expiry := range []string{"0", "-1", "604801", "1.5", "999999999999999999999999"} {
		t.Run(expiry, func(t *testing.T) {
			a := newLinkTestApp(t, Platform{CopyText: func(string) error { t.Fatal("invalid expiry copied"); return nil }, Storage: func(cloud.Account) (cloud.ObjectStorage, error) {
				t.Fatal("invalid expiry used provider")
				return nil, nil
			}})
			a.openObjectLink(a.objects[0])
			a.dialog.Fields["expires"] = expiry
			a.copyObjectLink(*a.dialog)
			if a.err == "" || a.dialog == nil {
				t.Fatal("invalid expiry not shown in form")
			}
		})
	}
	a := newLinkTestApp(t, Platform{CopyText: func(string) error { return errors.New("clipboard contains signed secret") }, Storage: func(cloud.Account) (cloud.ObjectStorage, error) {
		return linkStorage{link: func(context.Context, string, string, time.Duration) (cloud.ObjectLink, error) {
			return cloud.ObjectLink{URL: "https://fixture.invalid/file"}, nil
		}}, nil
	}})
	a.openObjectLink(a.objects[0])
	a.copyObjectLink(*a.dialog)
	a.workers.Wait()
	a.drain()
	if a.err == "" || a.notice != "" || a.dialog == nil || strings.Contains(a.err, "signed secret") {
		t.Fatal("clipboard failure mishandled")
	}
}

func TestObjectLinkAccountSwitchNeverCopiesStaleLink(t *testing.T) {
	copied := false
	a := newLinkTestApp(t, Platform{CopyText: func(string) error { copied = true; return nil }, Storage: func(cloud.Account) (cloud.ObjectStorage, error) {
		return linkStorage{link: func(context.Context, string, string, time.Duration) (cloud.ObjectLink, error) {
			return cloud.ObjectLink{URL: "https://fixture.invalid/old"}, nil
		}}, nil
	}})
	a.openObjectLink(a.objects[0])
	a.copyObjectLink(*a.dialog)
	a.switchAccount("b")
	a.workers.Wait()
	a.drain()
	if copied || a.dialog != nil || a.notice != "" {
		t.Fatal("stale account link copied")
	}
}

func TestObjectPathCopyDoesNotCallCloud(t *testing.T) {
	copied := ""
	a := newLinkTestApp(t, Platform{CopyText: func(s string) error { copied = s; return nil }, Storage: func(cloud.Account) (cloud.ObjectStorage, error) { t.Fatal("path copy used cloud"); return nil, nil }})
	a.openObjectLink(a.objects[0])
	a.copyObjectPath(*a.dialog)
	if copied != "oss://bucket/folder/中文 space #?.txt" {
		t.Fatal("path changed object key")
	}
}

func TestObjectLinkActionAndForm(t *testing.T) {
	a := newLinkTestApp(t, Platform{CopyText: func(string) error { return nil }})
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if err := tt.Click("复制访问链接 " + a.objects[0].Key); err != nil {
		t.Fatal(err)
	}
	if a.dialog == nil {
		t.Fatal("action did not open form")
	}
	tt.Frame()
	for _, label := range []string{"签名有效期（秒）", "复制 OSS 路径", "复制访问链接"} {
		if !tt.HasText(label) {
			t.Errorf("missing %s", label)
		}
	}
}

func TestObjectLinkProviderFailureKeepsFormAndClipboard(t *testing.T) {
	a := newLinkTestApp(t, Platform{CopyText: func(string) error { t.Fatal("failed request copied"); return nil }, Storage: func(cloud.Account) (cloud.ObjectStorage, error) {
		return linkStorage{link: func(context.Context, string, string, time.Duration) (cloud.ObjectLink, error) {
			return cloud.ObjectLink{}, errors.New("ACL read denied")
		}}, nil
	}})
	a.openObjectLink(a.objects[0])
	a.copyObjectLink(*a.dialog)
	a.workers.Wait()
	a.drain()
	if a.err == "" || a.dialog == nil {
		t.Fatal("provider error silently dismissed")
	}
}

func TestObjectLinkUsesUserExpiryAndSignedFallbackReason(t *testing.T) {
	a := newLinkTestApp(t, Platform{CopyText: func(string) error { return nil }, Storage: func(cloud.Account) (cloud.ObjectStorage, error) {
		return linkStorage{link: func(_ context.Context, _, _ string, expiry time.Duration) (cloud.ObjectLink, error) {
			if expiry != 600*time.Second {
				t.Error("custom expiry ignored")
			}
			return cloud.ObjectLink{URL: "https://fixture.invalid/signed", Signed: true, Reason: "无法确认公共访问限制"}, nil
		}}, nil
	}})
	a.openObjectLink(a.objects[0])
	a.dialog.Fields["expires"] = "600"
	a.saveForm()
	a.workers.Wait()
	a.drain()
	if !strings.Contains(a.notice, "600") || !strings.Contains(a.notice, "无法确认") {
		t.Fatal("expiry or signed fallback reason missing")
	}
}

func TestObjectLinkMissingCapabilityAndClipboard(t *testing.T) {
	for _, clipboard := range []bool{false, true} {
		p := Platform{Storage: func(cloud.Account) (cloud.ObjectStorage, error) { return &fakeOSS{}, nil }}
		if clipboard {
			p.CopyText = func(string) error { t.Fatal("unsupported provider copied"); return nil }
		}
		a := newLinkTestApp(t, p)
		a.openObjectLink(a.objects[0])
		a.copyObjectLink(*a.dialog)
		a.workers.Wait()
		a.drain()
		if a.err == "" || a.dialog == nil {
			t.Fatal("missing capability unhandled")
		}
	}
}

func TestVisualObjectLinkForm(t *testing.T) {
	dir := os.Getenv("YUNTOOLS_RENDER_DIR")
	if dir == "" {
		t.Skip("visual output not requested")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		a := newLinkTestApp(t, Platform{CopyText: func(string) error { return nil }})
		a.dark = dark
		a.openObjectLink(a.objects[0])
		for _, width := range []int{960, 1280} {
			tt := ui.NewTester(a.View, width, 820)
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("oss-link-%t-%d.png", dark, width)))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

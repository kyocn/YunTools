package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"yuntools/internal/cloud"
)

// Different source addresses must be distinct configurations, including after
// saving and reopening the edit dialog; equivalent addresses must not duplicate.
func TestWhitelistTargetIPsAreIndependentAndPersisted(t *testing.T) {
	a, store := targetFixture(t)
	original := a.rules[0]
	a.rules = nil
	for _, ip := range []string{"192.0.2.10", "192.0.2.20"} {
		a.newForm("whitelist", "配置加白目标", map[string]string{"name": "office", "kind": "rds", "id": original.ID, "region": original.Region, "group": original.Group, "sourceIP": ip})
		a.saveForm()
	}
	if len(a.rules) != 2 {
		t.Fatalf("different IPs rejected: count=%d error=%s", len(a.rules), a.err)
	}
	stored := persistedTargets(t, store)
	for i, want := range []string{"192.0.2.10", "192.0.2.20"} {
		a.editWhitelistTarget(stored[i])
		if a.dialog.Fields["sourceIP"] != want {
			t.Fatalf("saved IP not restored: %v", a.dialog.Fields)
		}
	}
	before := append([]byte(nil), mustMarshalTargets(t, a.rules)...)
	a.editWhitelistTarget(a.rules[1])
	a.dialog.Fields["sourceIP"] = " ::ffff:192.0.2.10 "
	a.saveForm()
	if a.err == "" || !reflect.DeepEqual(before, mustMarshalTargets(t, a.rules)) {
		t.Fatal("equivalent duplicate IP accepted")
	}
}
func mustMarshalTargets(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestWhitelistInvalidIPDoesNotSave(t *testing.T) {
	for _, value := range []string{"not-an-ip", "192.0.2.1,192.0.2.2", "0.0.0.0", "224.0.0.1", "fe80::1%en0", "192.0.2.1/24"} {
		t.Run(value, func(t *testing.T) {
			a, store := targetFixture(t)
			before := mustMarshalTargets(t, persistedTargets(t, store))
			a.editWhitelistTarget(a.rules[0])
			a.dialog.Fields["sourceIP"] = value
			a.saveForm()
			if a.err == "" || a.dialog == nil || !reflect.DeepEqual(before, mustMarshalTargets(t, persistedTargets(t, store))) {
				t.Fatal("invalid IP saved or dialog lost")
			}
		})
	}
}

type ipWhitelistFake struct {
	cloud.Whitelist
	calls      []string
	syncs      []string
	fail       error
	beforeSync func(cloud.Target)
}

func (w *ipWhitelistFake) AddIP(_ context.Context, t cloud.Target, ip string) error {
	w.calls = append(w.calls, t.Name+":"+ip)
	return w.fail
}
func (w *ipWhitelistFake) SyncIP(_ context.Context, t cloud.Target, ip string, _ cloud.SecretStore) error {
	if w.beforeSync != nil {
		w.beforeSync(t)
	}
	w.syncs = append(w.syncs, t.Name+":"+ip)
	return w.fail
}
func ipFixture(t *testing.T) (*App, *ipWhitelistFake) {
	t.Helper()
	w := &ipWhitelistFake{}
	a := New(Platform{Store: &memStore{}, Whitelist: func(cloud.Account) (cloud.Whitelist, error) { return w, nil }})
	t.Cleanup(a.Close)
	p, e := a.accounts.Save(cloud.Account{Name: "Test", AccessKeyID: "fixture", AccessKeySecret: "fixture-secret"})
	if e != nil {
		t.Fatal(e)
	}
	a.profiles = []cloud.Account{p}
	a.accountID = p.ID
	return a, w
}
func TestManualWhitelistUsesEachTargetIPAndAutomaticUsesDetectedIP(t *testing.T) {
	a, w := ipFixture(t)
	a.publicIP = "192.0.2.1"
	a.rules = []cloud.Target{{ConfigID: "a", AccountID: a.accountID, Name: "fixed", SourceIP: "192.0.2.2"}, {ConfigID: "b", AccountID: a.accountID, Name: "follow", SourceIP: "192.0.2.3", Automatic: true}, {ConfigID: "c", AccountID: a.accountID, Name: "legacy"}}
	detected := 0
	a.runManualWhitelist(a.rules, func(context.Context) (string, error) { detected++; return "192.0.2.4", nil })
	a.workers.Wait()
	a.drain()
	if !reflect.DeepEqual(w.calls, []string{"fixed:192.0.2.2", "legacy:192.0.2.1"}) || !reflect.DeepEqual(w.syncs, []string{"follow:192.0.2.4"}) || detected != 1 {
		t.Fatalf("wrong target routing: adds=%v syncs=%v detected=%d", w.calls, w.syncs, detected)
	}
	if a.rules[1].SourceIP != "192.0.2.4" || persistedTargets(t, a.store)[1].SourceIP != "192.0.2.4" {
		t.Fatal("confirmed automatic IP was not persisted")
	}
	if !strings.Contains(a.notice, "fixed：已确认添加 IP 192.0.2.2") || !strings.Contains(a.notice, "follow：已确认添加 IP 192.0.2.4") {
		t.Fatalf("per-target results misleading: %s", a.notice)
	}
}
func TestFixedTargetDoesNotNeedPublicIPDetection(t *testing.T) {
	a, w := ipFixture(t)
	a.runManualWhitelist([]cloud.Target{{AccountID: a.accountID, SourceIP: "192.0.2.20"}}, func(context.Context) (string, error) {
		t.Error("fixed IP triggered detection")
		return "", errors.New("offline")
	})
	a.workers.Wait()
	a.drain()
	if len(w.calls) != 1 || a.err != "" {
		t.Fatalf("fixed IP failed: %s", a.err)
	}
}
func TestAutomaticPersistsIdentityBeforeCloudAndIPAfterSuccess(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			a, w := ipFixture(t)
			a.rules = []cloud.Target{{AccountID: a.accountID, ID: "resource", Name: "auto", Automatic: true, SourceIP: "192.0.2.1"}}
			if failure {
				w.fail = errors.New("cloud refused")
			}
			w.beforeSync = func(target cloud.Target) {
				stored := persistedTargets(t, a.store)
				if target.ConfigID == "" || len(stored) != 1 || stored[0].ConfigID != target.ConfigID || stored[0].SourceIP != "192.0.2.1" {
					t.Error("ownership not durable before synchronization")
				}
			}
			a.runAutomaticWhitelist(a.rules, func(context.Context) (string, error) { return "192.0.2.9", nil })
			a.generation++
			a.workers.Wait()
			a.drain()
			want := "192.0.2.9"
			if failure {
				want = "192.0.2.1"
			}
			if a.rules[0].SourceIP != want || persistedTargets(t, a.store)[0].SourceIP != want {
				t.Fatal("saved IP disagrees with confirmed result")
			}
		})
	}
}

func TestWhitelistOverlappingRunsDoNotLeaveCloudAndSavedIPOutOfSync(t *testing.T) {
	for _, firstAutomatic := range []bool{false, true} {
		t.Run(fmt.Sprint(firstAutomatic), func(t *testing.T) {
			a, w := ipFixture(t)
			a.rules = []cloud.Target{{ConfigID: "0123456789abcdef0123456789abcdef", AccountID: a.accountID, ID: "r", Automatic: true, SourceIP: "192.0.2.1"}}
			entered, release := make(chan struct{}), make(chan struct{})
			detect := func(context.Context) (string, error) { close(entered); <-release; return "192.0.2.2", nil }
			if firstAutomatic {
				a.runAutomaticWhitelist(a.rules, detect)
			} else {
				a.runManualWhitelist(a.rules, detect)
			}
			<-entered
			a.runManualWhitelist(a.rules, func(context.Context) (string, error) {
				t.Error("overlapping manual sync started")
				return "192.0.2.3", nil
			})
			a.runAutomaticWhitelist(a.rules, func(context.Context) (string, error) {
				t.Error("overlapping automatic sync started")
				return "192.0.2.4", nil
			})
			close(release)
			a.workers.Wait()
			a.drain()
			if len(w.syncs) != 1 || a.rules[0].SourceIP != "192.0.2.2" || persistedTargets(t, a.store)[0].SourceIP != "192.0.2.2" || a.whitelistManualRunning || a.whitelistAutomaticRunning {
				t.Fatal("cloud/saved IP diverged or sync stayed locked")
			}
		})
	}
}
func TestIPOnlyEditKeepsManagedIdentity(t *testing.T) {
	a, _ := targetFixture(t)
	original := a.rules[0]
	a.editWhitelistTarget(original)
	a.dialog.Fields["sourceIP"] = "192.0.2.10"
	a.saveForm()
	if a.err != "" || a.rules[0].ConfigID != original.ConfigID || a.rules[0].SourceIP != "192.0.2.10" {
		t.Fatal("IP edit discarded managed ownership")
	}
}

func TestWhitelistIPFormFitsAndShowsMode(t *testing.T) {
	for _, height := range []int{640, 820} {
		a, _ := targetFixture(t)
		a.autoNext = time.Now().Add(time.Hour)
		a.editWhitelistTarget(a.rules[0])
		tt := ui.NewTester(a.View, 960, height)
		tt.Frame()
		for _, label := range []string{"指定 IP（可选）", "自动跟随公网 IP（替换本工具管理的旧 IP）", "保存", "取消"} {
			rect, ok := tt.Find(label)
			if !ok || rect.Y < 0 || rect.Y+rect.H > float32(height) {
				t.Fatalf("%s not visible at height %d: %+v", label, height, rect)
			}
		}
		if dir := os.Getenv("YUNTOOLS_RENDER_DIR"); dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("whitelist-ip-%d.png", height)))
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

package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
)

func TestRestoredAccountUsesItsOwnDiscoveryRegion(t *testing.T) {
	profiles := []cloud.Account{{ID: "beijing", Name: "A fixture", Region: "cn-beijing"}, {ID: "shanghai", Name: "B fixture", Region: "cn-shanghai"}}
	profilesJSON, _ := json.Marshal(profiles)
	prefsJSON, _ := json.Marshal(preferences{Tabs: []tab{{AccountID: "shanghai", Page: "whitelist", Label: "访问白名单"}}, Active: 0})
	a := New(Platform{Store: &memStore{d: map[string][]byte{"accounts/profiles/v1": profilesJSON, "preferences/v1": prefsJSON}}})
	defer a.Close()
	if a.accountID != "shanghai" || a.region != "cn-shanghai" {
		t.Fatalf("restored %s using region %s", a.accountID, a.region)
	}
}

type discoveryFixture struct {
	cloud.Whitelist
	list func(context.Context, string, string) ([]cloud.Target, error)
}

func (f discoveryFixture) ListTargets(ctx context.Context, kind, region string) ([]cloud.Target, error) {
	return f.list(ctx, kind, region)
}
func discoveryApp(f discoveryFixture) *App {
	a := New(Platform{Store: &memStore{}, Whitelist: func(cloud.Account) (cloud.Whitelist, error) { return f, nil }})
	a.profiles = []cloud.Account{{ID: "fixture", Name: "fixture", Region: "cn-shanghai"}}
	a.accountID, a.region, a.resourceKind, a.page = "fixture", "cn-shanghai", "ecs", "whitelist"
	return a
}
func TestDiscoveryEmptyResultHasVisibleRegionAndNextStep(t *testing.T) {
	a := discoveryApp(discoveryFixture{list: func(context.Context, string, string) ([]cloud.Target, error) { return nil, nil }})
	defer a.Close()
	a.refreshTargets()
	a.workers.Wait()
	a.drain()
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if !tt.HasText("上海（cn-shanghai）") || !tt.HasText("未找到资源") || !tt.HasText("请核对地域和账号") {
		t.Fatal("empty discovery appears to do nothing")
	}
}
func TestDiscoveryLoadIgnoresUnrelatedBusy(t *testing.T) {
	called := make(chan string, 1)
	a := discoveryApp(discoveryFixture{list: func(_ context.Context, kind, region string) ([]cloud.Target, error) {
		called <- kind + "/" + region
		return nil, nil
	}})
	defer a.Close()
	a.busy = 1
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if err := tt.Click("加载资源"); err != nil {
		t.Fatal(err)
	}
	select {
	case query := <-called:
		if query != "ecs/cn-shanghai" {
			t.Fatal(query)
		}
	case <-time.After(time.Second):
		t.Fatal("unrelated busy disabled discovery")
	}
}

func TestDiscoveryStaleRegionCannotOverwriteNewResult(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	a := discoveryApp(discoveryFixture{list: func(_ context.Context, kind, region string) ([]cloud.Target, error) {
		if region == "cn-beijing" {
			close(entered)
			<-release
			return nil, errors.New("old region failed")
		}
		return []cloud.Target{{ID: "sg-shanghai", Kind: kind, Region: region}}, nil
	}})
	defer a.Close()
	a.region = "cn-beijing"
	a.refreshTargets()
	<-entered
	a.region = "cn-shanghai"
	a.refreshTargets()
	close(release)
	a.workers.Wait()
	a.drain()
	if len(a.targets) != 1 || a.targets[0].ID != "sg-shanghai" || strings.Contains(a.err, "old region") {
		t.Fatalf("stale response applied: %+v %q", a.targets, a.err)
	}
}

func TestDiscoveryRegionDropdownPreservesSavedSelectionAndUsesCode(t *testing.T) {
	called := make(chan string, 1)
	a := discoveryApp(discoveryFixture{list: func(_ context.Context, _ string, region string) ([]cloud.Target, error) {
		called <- region
		return nil, nil
	}})
	defer a.Close()
	a.targets = []cloud.Target{{ID: "old-resource", Region: "cn-shanghai"}}
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if a.region != "cn-shanghai" || !tt.HasText("上海（cn-shanghai）") {
		t.Fatal("saved region was not preselected")
	}
	if err := tt.Click("资源地域"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("北京（cn-beijing）"); err != nil {
		t.Fatal(err)
	}
	if a.region != "cn-beijing" || len(a.targets) != 0 {
		t.Fatalf("selected label leaked or stale results survived: %q %+v", a.region, a.targets)
	}
	select {
	case <-called:
		t.Fatal("changing region implicitly queried cloud")
	default:
	}
	if err := tt.Click("加载资源"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-called:
		if got != "cn-beijing" {
			t.Fatalf("API received label %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no discovery request")
	}
}

func TestAccountRegionDropdownPreservesUnknownSavedRegion(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	a.editAccount(cloud.Account{ID: "fixture", Name: "fixture", Region: "custom-region-7"})
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if a.dialog.Fields["region"] != "custom-region-7" {
		t.Fatal("unknown region silently replaced")
	}
	if err := tt.Click("已保存地域（custom-region-7）"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("上海（cn-shanghai）"); err != nil {
		t.Fatal(err)
	}
	if a.dialog.Fields["region"] != "cn-shanghai" {
		t.Fatalf("stored label instead of code: %q", a.dialog.Fields["region"])
	}
}

func TestDiscoveryFailureEndsLoadingAndExplainsActualQuery(t *testing.T) {
	a := discoveryApp(discoveryFixture{list: func(context.Context, string, string) ([]cloud.Target, error) {
		return nil, errors.New("fixture permission denied")
	}})
	defer a.Close()
	a.targets = []cloud.Target{{ID: "stale"}}
	a.refreshTargets()
	if !a.targetDiscovery.loading || len(a.targets) != 0 || !strings.Contains(a.targetDiscoveryText(), "正在加载 上海（cn-shanghai）") {
		t.Fatal("missing loading state or stale results visible")
	}
	a.workers.Wait()
	a.drain()
	if a.targetDiscovery.loading || !a.targetDiscovery.failed || len(a.targets) != 0 {
		t.Fatal("failure left loading or old results")
	}
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if !tt.HasText("加载失败") || !tt.HasText("上海（cn-shanghai）") || !tt.HasText("权限与地域后重试") {
		t.Fatal("failure has no actionable query-specific feedback")
	}
}

func TestDiscoveryChangedKindDiscardsOldSuccessfulPage(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	a := discoveryApp(discoveryFixture{list: func(_ context.Context, kind, region string) ([]cloud.Target, error) {
		if kind == "rds" {
			close(entered)
			<-release
			return []cloud.Target{{ID: "old-rds", Kind: kind, Region: region}}, nil
		}
		return []cloud.Target{{ID: "current-ecs", Kind: kind, Region: region}}, nil
	}})
	defer a.Close()
	a.resourceKind = "rds"
	a.refreshTargets()
	<-entered
	a.resourceKind = "ecs"
	a.refreshTargets()
	close(release)
	a.workers.Wait()
	a.drain()
	if len(a.targets) != 1 || a.targets[0].ID != "current-ecs" || a.targetDiscovery.loading || !strings.Contains(a.targetDiscoveryText(), "已加载 1 项资源") {
		t.Fatalf("outdated successful result applied: %+v %q", a.targets, a.targetDiscoveryText())
	}
}

func TestSwitchAccountResetsDiscoveryAndPreselectsSavedRegion(t *testing.T) {
	a := discoveryApp(discoveryFixture{list: func(context.Context, string, string) ([]cloud.Target, error) { return nil, nil }})
	defer a.Close()
	a.profiles = append(a.profiles, cloud.Account{ID: "other", Name: "other", Region: "ap-southeast-8"})
	a.targets = []cloud.Target{{ID: "previous-account"}}
	a.targetDiscovery.loading = true
	a.switchAccount("other")
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if a.region != "ap-southeast-8" || len(a.targets) != 0 || a.targetDiscovery.loading || !tt.HasText("柔佛州（ap-southeast-8）") {
		t.Fatal("account switch did not reset discovery to its saved region")
	}
}

package app

import (
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
)

func TestHistoryContextMenuClosesRelativeToClickedTab(t *testing.T) {
	for _, tc := range []struct {
		name, action string
		active       int
		wantPages    []string
		wantActive   int
		wantPage     string
	}{
		{"current-inactive", "关闭当前", 2, []string{"accounts", "overview"}, 1, "overview"},
		{"right-keeps-active", "关闭右侧", 0, []string{"accounts", "settings"}, 0, "accounts"},
		{"right-closes-active", "关闭右侧", 2, []string{"accounts", "settings"}, 1, "settings"},
		{"all", "全部关闭", 2, []string{}, -1, "overview"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &memStore{}
			a := New(Platform{Store: store})
			defer a.Close()
			a.profiles = []cloud.Account{{ID: "a", Name: "test", Region: "cn-beijing"}}
			a.accountID = "a"
			a.tabs = []tab{{"a", "accounts", "云账号"}, {"a", "settings", "设置"}, {"a", "overview", "概览"}}
			a.activeTab, a.page = tc.active, a.tabs[tc.active].Page
			tt := ui.NewTester(a.View, 1280, 820)
			tt.Frame()
			if err := tt.RightClick("设置 · test"); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem(tc.action); err != nil {
				t.Fatal(err)
			}
			pages := []string{}
			for _, tab := range a.tabs {
				pages = append(pages, tab.Page)
			}
			if !reflect.DeepEqual(pages, tc.wantPages) || a.activeTab != tc.wantActive || a.page != tc.wantPage || a.accountID != "a" {
				t.Fatalf("tabs=%v active=%d page=%s account=%s", pages, a.activeTab, a.page, a.accountID)
			}
			restored := &App{store: store, profiles: a.profiles}
			if err := restored.loadPreferences(); err != nil {
				t.Fatal(err)
			}
			if len(restored.tabs) != len(a.tabs) || restored.activeTab != a.activeTab {
				t.Fatal("menu closure not persisted")
			}
		})
	}
}

func TestHistoryContextMenuDisablesCloseRightOnLastTab(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	a.tabs = []tab{{"", "settings", "设置"}}
	a.activeTab, a.page = 0, "settings"
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if err := tt.RightClick("设置"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tt.Menu(), []string{"关闭当前", "关闭右侧", "全部关闭"}) {
		t.Fatalf("menu=%v", tt.Menu())
	}
	if err := tt.ChooseMenuItem("关闭右侧"); err == nil {
		t.Fatal("close right enabled on last tab")
	}
}

func TestLegacyHistoryTabKeepsCurrentCloudAccount(t *testing.T) {
	for _, closeCurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "activate", true: "close-neighbor"}[closeCurrent], func(t *testing.T) {
			a := New(Platform{Store: &memStore{}})
			defer a.Close()
			a.profiles = []cloud.Account{{ID: "a", Name: "test", Region: "cn-beijing"}}
			a.accountID, a.page = "a", "settings"
			a.tabs = []tab{{"", "accounts", "云账号"}, {"a", "settings", "设置"}}
			a.activeTab = 1
			tt := ui.NewTester(a.View, 1280, 820)
			tt.Frame()
			label := "云账号"
			if closeCurrent {
				label = "关闭标签：设置 · test"
			}
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			if a.accountID != "a" || a.page != "accounts" {
				t.Fatalf("legacy tab cleared account: %s/%s", a.accountID, a.page)
			}
		})
	}
}

func TestHistoryTabCloseKeepsActiveViewAndPersistsRemoval(t *testing.T) {
	store := &memStore{}
	a := New(Platform{Store: store})
	defer a.Close()
	a.profiles = []cloud.Account{{ID: "a", Name: "test", Region: "cn-beijing"}}
	a.accountID, a.page = "a", "settings"
	a.tabs = []tab{{"a", "accounts", "云账号"}, {"a", "settings", "设置"}, {"a", "overview", "概览"}}
	a.activeTab = 1
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	jobs := a.jobs
	gen := a.generation
	if err := tt.Click("关闭标签：云账号 · test"); err != nil {
		t.Fatal(err)
	}
	if len(a.tabs) != 2 || a.activeTab != 0 || a.page != "settings" || a.generation != gen {
		t.Fatalf("closing inactive tab changed active view: tabs=%v active=%d page=%s", a.tabs, a.activeTab, a.page)
	}
	restored := &App{store: store, profiles: a.profiles}
	if err := restored.loadPreferences(); err != nil {
		t.Fatal(err)
	}
	if len(restored.tabs) != 2 || restored.tabs[0].Page != "settings" || restored.activeTab != 0 {
		t.Fatalf("closed tab restored: %+v", restored.tabs)
	}
	if err := tt.Click("关闭标签：设置 · test"); err != nil {
		t.Fatal(err)
	}
	if len(a.tabs) != 1 || a.activeTab != 0 || a.page != "overview" {
		t.Fatalf("active close did not select neighbor: %+v", a.tabs)
	}
	applied := false
	a.applyResult(gen, func() { applied = true })
	if applied {
		t.Fatal("closed view accepted a stale result")
	}
	if err := tt.Click("关闭标签：概览 · test"); err != nil {
		t.Fatal(err)
	}
	if len(a.tabs) != 0 || a.activeTab != -1 || a.page != "overview" || a.accountID != "a" || a.jobs != jobs {
		t.Fatal("last tab close did not return to overview while keeping account and tasks")
	}
	if err := restored.loadPreferences(); err != nil {
		t.Fatal(err)
	}
	if len(restored.tabs) != 0 || restored.activeTab != -1 {
		t.Fatal("empty tab state was not saved")
	}
}

func TestClosingCurrentHistoryTabSwitchesToNeighborAccount(t *testing.T) {
	a := New(Platform{Store: &memStore{}})
	defer a.Close()
	a.profiles = []cloud.Account{{ID: "a", Name: "first", Region: "cn-beijing"}, {ID: "b", Name: "second", Region: "cn-shanghai"}}
	a.accountID, a.page = "b", "settings"
	a.tabs = []tab{{"a", "accounts", "云账号"}, {"b", "settings", "设置"}}
	a.activeTab = 1
	a.objects = []cloud.Object{{Key: "other-account-object"}}
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if err := tt.Click("关闭标签：设置 · second"); err != nil {
		t.Fatal(err)
	}
	if a.accountID != "a" || a.page != "accounts" || a.region != "cn-beijing" || a.activeTab != 0 || len(a.objects) != 0 {
		t.Fatalf("neighbor account not selected safely: %s %s %s %d", a.accountID, a.page, a.region, a.activeTab)
	}
}

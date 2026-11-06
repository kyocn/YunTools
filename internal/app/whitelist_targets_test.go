package app

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
)

type targetStore struct {
	memStore
	fail bool
}

func (s *targetStore) Put(key string, value []byte) error {
	if s.fail && key == "whitelist/rules" {
		return errors.New("fixture storage failure")
	}
	return s.memStore.Put(key, value)
}

func targetFixture(t *testing.T) (*App, *targetStore) {
	t.Helper()
	store := &targetStore{}
	a := New(Platform{Store: store, Whitelist: func(cloud.Account) (cloud.Whitelist, error) {
		t.Error("local configuration operation contacted cloud provider")
		return nil, errors.New("unexpected cloud operation")
	}})
	t.Cleanup(a.Close)
	a.profiles = []cloud.Account{{ID: "account", Name: "Test"}}
	a.accountID, a.page = "account", "whitelist"
	a.rules = []cloud.Target{
		{AccountID: "account", Kind: "rds", Region: "cn-shanghai", ID: "db-test", Name: "Database", Group: "office", Automatic: true},
		{AccountID: "other", Kind: "rds", Region: "cn-shanghai", ID: "db-test", Name: "Other account", Group: "office"},
	}
	b, err := json.Marshal(a.rules)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put("whitelist/rules", b); err != nil {
		t.Fatal(err)
	}
	return a, store
}

func persistedTargets(t *testing.T, store cloud.SecretStore) []cloud.Target {
	t.Helper()
	b, err := store.Get("whitelist/rules")
	if err != nil {
		t.Fatal(err)
	}
	var targets []cloud.Target
	if err = json.Unmarshal(b, &targets); err != nil {
		t.Fatal(err)
	}
	return targets
}

func TestConfiguredWhitelistTargetCanBeEdited(t *testing.T) {
	a := New(Platform{Store: &memStore{}, Whitelist: func(cloud.Account) (cloud.Whitelist, error) {
		t.Error("local target edit contacted cloud provider")
		return nil, nil
	}})
	defer a.Close()
	a.profiles = []cloud.Account{{ID: "account", Name: "Test"}}
	a.accountID, a.page = "account", "whitelist"
	a.rules = []cloud.Target{{AccountID: "account", Kind: "ecs", Region: "cn-shanghai", ID: "sg-test", Name: "SSH", Protocol: "tcp", Ports: "22/22", Automatic: true}}
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if err := tt.Click("编辑"); err != nil {
		t.Fatal(err)
	}
	if a.dialog == nil {
		t.Fatal("configured target did not open an editor")
	}
	a.dialog.Fields["name"] = "HTTPS"
	a.dialog.Fields["ports"] = "443/443"
	a.saveForm()
	if len(a.rules) != 1 || a.rules[0].Name != "HTTPS" || a.rules[0].Ports != "443/443" || !a.rules[0].Automatic {
		t.Fatalf("edit did not replace the target preserving automatic mode: %+v; error=%s", a.rules, a.err)
	}
}

func TestWhitelistTargetEditPreservesIdentityAndOtherAccounts(t *testing.T) {
	a, store := targetFixture(t)
	original, other := a.rules[0], a.rules[1]
	a.editWhitelistTarget(original)
	a.dialog.Fields["name"], a.dialog.Fields["group"] = "Production", "home"
	a.dialog.Fields["id"], a.dialog.Fields["region"], a.dialog.Fields["kind"] = "changed", "changed", "ecs"
	a.dialog.Account.ID = "changed"
	a.saveForm()
	want := original
	want.Name, want.Group = "Production", "home"
	if !reflect.DeepEqual(a.rules, []cloud.Target{want, other}) {
		t.Fatalf("edit changed identity, automatic mode or another target: %+v", a.rules)
	}
	if !reflect.DeepEqual(persistedTargets(t, store), a.rules) {
		t.Fatal("edited target was not persisted")
	}
}

func TestWhitelistTargetRemovalRequiresConfirmationAndCanCancel(t *testing.T) {
	a, store := targetFixture(t)
	original := append([]cloud.Target(nil), a.rules...)
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if err := tt.Click("移除"); err != nil {
		t.Fatal(err)
	}
	if a.confirm == nil || !strings.Contains(a.confirm.Detail, "不会删除或修改云端") {
		t.Fatal("removal did not explain its local-only scope")
	}
	if !reflect.DeepEqual(a.rules, original) || !reflect.DeepEqual(persistedTargets(t, store), original) {
		t.Fatal("target removed before confirmation")
	}
	tt.Frame()
	if err := tt.Click("取消操作"); err != nil {
		t.Fatal(err)
	}
	if a.confirm != nil || !reflect.DeepEqual(a.rules, original) {
		t.Fatal("cancel changed configured targets")
	}
	a.removeWhitelistTarget(original[0])
	a.confirm.Run()
	want := []cloud.Target{original[1]}
	if !reflect.DeepEqual(a.rules, want) || !reflect.DeepEqual(persistedTargets(t, store), want) {
		t.Fatalf("confirmation removed wrong target: %+v", a.rules)
	}
}

func TestWhitelistTargetDialogsRejectStaleSnapshots(t *testing.T) {
	for _, action := range []string{"edit", "remove"} {
		for _, change := range []string{"removed", "changed"} {
			t.Run(action+"/"+change, func(t *testing.T) {
				a, store := targetFixture(t)
				original := a.rules[0]
				var finish func()
				if action == "edit" {
					a.editWhitelistTarget(original)
					a.dialog.Fields["name"] = "Stale edit"
					finish = a.saveForm
				} else {
					a.removeWhitelistTarget(original)
					finish = a.confirm.Run
				}
				if change == "removed" {
					a.rules = a.rules[1:]
				} else {
					a.rules[0].Automatic = false
				}
				before := append([]cloud.Target(nil), a.rules...)
				stored := persistedTargets(t, store)
				finish()
				if a.err == "" || !reflect.DeepEqual(a.rules, before) || !reflect.DeepEqual(persistedTargets(t, store), stored) {
					t.Fatalf("stale %s changed targets: %+v", action, a.rules)
				}
			})
		}
	}
}

func TestWhitelistTargetStorageFailurePreservesOriginal(t *testing.T) {
	for _, action := range []string{"edit", "remove"} {
		t.Run(action, func(t *testing.T) {
			a, store := targetFixture(t)
			before := append([]cloud.Target(nil), a.rules...)
			store.fail = true
			if action == "edit" {
				a.editWhitelistTarget(a.rules[0])
				a.dialog.Fields["group"] = "new-group"
				a.saveForm()
				if a.dialog == nil {
					t.Fatal("failed edit lost user's form")
				}
			} else {
				a.removeWhitelistTarget(a.rules[0])
				a.confirm.Run()
			}
			if a.err == "" || !reflect.DeepEqual(a.rules, before) || !reflect.DeepEqual(persistedTargets(t, store), before) {
				t.Fatal("storage failure changed configured targets")
			}
		})
	}
}

func TestWhitelistTargetEditRejectsDuplicateConfiguration(t *testing.T) {
	a, store := targetFixture(t)
	second := a.rules[0]
	second.Group = "home"
	a.rules = append(a.rules, second)
	before := append([]cloud.Target(nil), a.rules...)
	stored := persistedTargets(t, store)
	a.editWhitelistTarget(a.rules[0])
	a.dialog.Fields["group"] = "home"
	a.saveForm()
	if a.err == "" || !reflect.DeepEqual(a.rules, before) || !reflect.DeepEqual(persistedTargets(t, store), stored) {
		t.Fatal("edit accepted a duplicate target configuration")
	}
}

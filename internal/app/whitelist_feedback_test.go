package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
)

type automaticWhitelistFake struct {
	cloud.Whitelist
	calls int
	err   error
}

func (w *automaticWhitelistFake) AddIP(context.Context, cloud.Target, string) error {
	w.calls++
	return w.err
}

func (w *automaticWhitelistFake) SyncIP(ctx context.Context, t cloud.Target, ip string, _ cloud.SecretStore) error {
	return w.AddIP(ctx, t, ip)
}

func automaticWhitelistFixture(t *testing.T, result error) (*App, *automaticWhitelistFake, cloud.Target) {
	t.Helper()
	w := &automaticWhitelistFake{err: result}
	a := New(Platform{Store: &memStore{}, Whitelist: func(cloud.Account) (cloud.Whitelist, error) { return w, nil }})
	t.Cleanup(a.Close)
	profile, err := a.accounts.Save(cloud.Account{Name: "测试账号", AccessKeyID: "fake-key-id", AccessKeySecret: "fake-secret-value"})
	if err != nil {
		t.Fatal(err)
	}
	a.profiles = []cloud.Account{profile}
	target := cloud.Target{ConfigID: "fixture", AccountID: profile.ID, ID: "test-target", Name: "测试目标", Kind: "ecs", Protocol: "tcp", Ports: "22/22", Automatic: true}
	a.rules = []cloud.Target{target}
	a.err, a.notice = "保留手动错误", "保留手动提示"
	return a, w, target
}

func TestAutomaticWhitelistKeepsGlobalBannersAndChecksUnchangedIP(t *testing.T) {
	for _, failure := range []error{nil, errors.New("Forbidden fake-secret-value")} {
		a, w, target := automaticWhitelistFixture(t, failure)
		for check := 0; check < 2; check++ {
			target = a.rules[0]
			a.runAutomaticWhitelist([]cloud.Target{target}, func(context.Context) (string, error) { return "192.0.2.1", nil })
			if a.err != "保留手动错误" || a.notice != "保留手动提示" {
				t.Fatal("background start cleared a manual banner")
			}
			a.generation++ // Navigating elsewhere must not lose the target's background result.
			a.workers.Wait()
			a.drain()
			if a.err != "保留手动错误" || a.notice != "保留手动提示" {
				t.Fatal("background completion changed a manual banner")
			}
		}
		if w.calls != 2 {
			t.Fatalf("same IP skipped remote validation: calls=%d", w.calls)
		}
		target = a.rules[0]
		status, ok := a.whitelistFeedback[target]
		if !ok || status.CheckedAt.IsZero() || status.IP != "192.0.2.1" || (status.Error != "") != (failure != nil) || strings.Contains(status.Error, "fake-secret-value") {
			t.Fatalf("missing or unsafe inline status: %#v", status)
		}
		if a.busy != 0 {
			t.Fatalf("background task left busy=%d", a.busy)
		}
		tt := ui.NewTester(func(c *ui.Context) { a.whitelistAutomaticStatus(c, target) }, 700, 180)
		tt.Frame()
		if !tt.HasText("自动检查") {
			t.Fatal("inline status not displayed")
		}
		a.accountID, a.page = target.AccountID, "whitelist"
		page := ui.NewTester(a.View, 1280, 820)
		page.Frame()
		if !page.HasText("自动检查") {
			t.Fatal("configured target card did not show background status")
		}
	}
}

func TestAutomaticWhitelistIPFailureStaysInline(t *testing.T) {
	a, w, target := automaticWhitelistFixture(t, nil)
	a.runAutomaticWhitelist([]cloud.Target{target}, func(context.Context) (string, error) { return "", errors.New("public IP service returned an error") })
	a.workers.Wait()
	a.drain()
	if w.calls != 0 || a.whitelistFeedback[target].Error == "" || a.err != "保留手动错误" || a.notice != "保留手动提示" {
		t.Fatal("public IP failure was not isolated to inline feedback")
	}
}

func TestAutomaticWhitelistDiscardsChangedOrRemovedTargetsAndAccounts(t *testing.T) {
	for _, mutate := range []struct {
		name  string
		apply func(*App)
	}{
		{"edited", func(a *App) { a.rules[0].Ports = "443/443" }},
		{"removed", func(a *App) { a.rules = nil }},
		{"disabled", func(a *App) { a.rules[0].Automatic = false }},
		{"account edited", func(a *App) { a.profiles[0].AccessKeySecret = "replacement-secret" }},
		{"account removed", func(a *App) { a.profiles = nil }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			a, _, target := automaticWhitelistFixture(t, nil)
			a.runAutomaticWhitelist([]cloud.Target{target}, func(context.Context) (string, error) { return "192.0.2.1", nil })
			a.workers.Wait()
			mutate.apply(a)
			a.drain()
			if len(a.whitelistFeedback) != 0 {
				t.Fatal("stale background result attached to a changed target or account")
			}
		})
	}
}

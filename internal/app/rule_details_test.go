package app

import (
	"context"
	"github.com/egoist/mygo/ui"
	"testing"
	"yuntools/internal/cloud"
)

type detailProvider struct{ cloud.Whitelist }

func (detailProvider) ListSecurityGroupRules(context.Context, cloud.Target) ([]cloud.SecurityGroupRule, error) {
	return []cloud.SecurityGroupRule{{ID: "r-test", Direction: "ingress", Policy: "Accept", Protocol: "TCP", Ports: "22/22", Source: "8.8.8.8/32", Priority: "1", Description: "服务器登录"}}, nil
}
func TestResourceDoubleClickShowsReadableRuleDetails(t *testing.T) {
	a := New(Platform{Store: &memStore{}, Whitelist: func(cloud.Account) (cloud.Whitelist, error) { return detailProvider{}, nil }})
	defer a.Close()
	p, err := a.accounts.Save(cloud.Account{Name: "测试账号", AccessKeyID: "fixture", AccessKeySecret: "fixture-secret", Region: "cn-shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	a.profiles = []cloud.Account{p}
	a.accountID, a.page = p.ID, "whitelist"
	a.targets = []cloud.Target{{AccountID: p.ID, ID: "sg-test", Name: "测试安全组", Kind: "ecs", Region: "cn-shanghai"}}
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if err = tt.Click("资源规则 sg-test"); err != nil {
		t.Fatal(err)
	}
	if a.ruleDetail != nil {
		t.Fatal("single click opened details")
	}
	if err = tt.Click("资源规则 sg-test"); err != nil {
		t.Fatal(err)
	}
	if a.ruleDetail == nil {
		t.Fatal("double click did not open details")
	}
	a.workers.Wait()
	tt.Frame()
	for _, text := range []string{"安全组规则", "入站", "允许", "22/22", "8.8.8.8/32", "服务器登录"} {
		if !tt.HasText(text) {
			t.Errorf("missing detail %q", text)
		}
	}
	if a.notice != "" {
		t.Fatal("rules leaked into notification banner")
	}
	if err = tt.Click("关闭规则详情"); err != nil {
		t.Fatal(err)
	}
	if a.ruleDetail != nil {
		t.Fatal("details did not close")
	}
}

func TestAccountSwitchDiscardsRuleDetailsInFlight(t *testing.T) {
	a := New(Platform{Store: &memStore{}, Whitelist: func(cloud.Account) (cloud.Whitelist, error) { return detailProvider{}, nil }})
	defer a.Close()
	p, err := a.accounts.Save(cloud.Account{Name: "测试账号", AccessKeyID: "fixture", AccessKeySecret: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	a.profiles = []cloud.Account{p}
	a.accountID = p.ID
	a.openRuleDetails(cloud.Target{AccountID: p.ID, Kind: "ecs", ID: "sg-test"})
	a.switchAccount("another-account")
	a.workers.Wait()
	a.drain()
	if a.ruleDetail != nil {
		t.Fatal("old account details survived switch")
	}
}

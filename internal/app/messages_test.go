package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"yuntools/internal/cloud"
)

func TestFriendlyErrorMessages(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"context deadline exceeded", "先刷新"},
		{"context canceled", "已取消"},
		{"dial tcp: connection refused", "网络"},
		{"DescribeSecurityGroups: Forbidden。请核对 RAM 权限 ecs:DescribeSecurityGroups、授权资源范围及显式 Deny 策略。", "管理员"},
		{"ModifySecurityIps: Forbidden。请核对 RAM 权限 kvstore:ModifySecurityIps、授权资源范围及显式 Deny 策略。", "kvstore:ModifySecurityIps"},
		{"DescribeInstances: InvalidAccessKeyId.NotFound", "AccessKey ID"},
		{"SignatureDoesNotMatch", "AccessKey Secret"},
		{"InvalidSecurityToken.Expired", "临时凭证"},
		{"account credentials are required", "账号管理"},
		{"name, access key ID and secret are required", "账号名称"},
		{"unsupported provider", "阿里云"},
		{"explicit tcp or udp protocol is required", "TCP 或 UDP"},
		{"ports must be start/end", "22/22"},
		{"ports must be between 1 and 65535", "1–65535"},
		{"invalid DNS zone name (use ASCII/punycode)", "example.com"},
		{"invalid certificate domain; use ASCII/punycode", "中文域名"},
		{"invalid DNS record", "记录值"},
		{"invalid DNS record name", "www"},
		{"address does not match DNS record type", "IPv4"},
		{"invalid record destination", "域名"},
		{"MX priority must be 1–50", "1–50"},
		{"unsupported DNS record type", "记录类型"},
		{"TTL out of range", "1–86400"},
		{"任务绑定的账号不存在，请重新添加原账号", "重新添加原账号"},
		{"vault is already in use", "另一个 YunTools"},
		{"vault is closed", "解锁系统钥匙串"},
		{"vault authentication failed", "钥匙串"},
		{"vault master key is missing; restore the original key to unlock", "原来的"},
		{"source changed; start a new transfer", "重新创建传输任务"},
		{"download source differs from checkpoint", "重新创建传输任务"},
		{"appended IP not visible yet; refresh before retry", "不要重复添加"},
		{"新增请求已提交，但暂未查询到匹配规则；请先刷新规则列表，勿重复添加", "勿重复添加"},
		{"previous whitelist entry no longer present; inspect concurrent cloud changes", "原有"},
		{"AuthorizeSecurityGroup failed (network or cloud service error); inspect remote state before retrying", "先刷新"},
		{"OSS did not confirm deletion of \"a.txt\"; some objects may already have been deleted", "部分文件可能已经删除"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got := (&App{}).safeError(errors.New(tc.input))
			if !strings.Contains(got, tc.want) {
				t.Fatalf("expected %q in %q", tc.want, got)
			}
		})
	}
}

func TestUncertainWritesNeverSuggestImmediateRetry(t *testing.T) {
	for _, input := range []string{
		"context deadline exceeded",
		"context canceled",
		"AuthorizeSecurityGroup failed (network or cloud service error); inspect remote state before retrying",
		"appended IP not visible yet; refresh before retry",
	} {
		got := (&App{}).safeError(errors.New(input))
		if !strings.Contains(got, "先刷新") || strings.Contains(got, "未提交") || strings.Contains(got, "没有生效") {
			t.Fatalf("unsafe guidance for uncertain write: %q", got)
		}
	}
}

func TestSafeErrorRedactsBeforeAddingHelpfulDetails(t *testing.T) {
	a := &App{profiles: []cloud.Account{{AccessKeyID: "test-key-1234", AccessKeySecret: "test-secret-5678", SecurityToken: "test-token-9012"}}}
	input := "Forbidden test-key-1234 test-secret-5678 test-token-9012"
	got := a.safeError(errors.New(input))
	for _, secret := range []string{"test-key-1234", "test-secret-5678", "test-token-9012"} {
		if strings.Contains(got, secret) {
			t.Fatalf("credential exposed in %q", got)
		}
	}
	if strings.Count(got, "[已隐藏]") != 3 || !strings.Contains(got, "管理员") {
		t.Fatalf("missing explanation or redacted details: %q", got)
	}
	unknown := "未识别的服务错误 CustomCode: 请联系管理员"
	if got := a.safeError(errors.New(unknown)); got != unknown {
		t.Fatalf("unknown helpful error changed: %q", got)
	}
	for _, original := range []string{"UnexpectedServiceReply: custom detail", "UnexpectedServiceReply: test-secret-5678"} {
		got := a.safeError(errors.New(original))
		redacted := strings.ReplaceAll(original, "test-secret-5678", "[已隐藏]")
		if !strings.Contains(got, redacted) || !strings.Contains(got, "详细信息") || strings.Contains(got, "test-secret-5678") {
			t.Fatalf("unknown English error lacks safe context or original detail: %q", got)
		}
	}
	got = a.safeError(errors.New(strings.Repeat("中", 300)))
	if len(got) > 703 || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Fatalf("error was not safely shortened: bytes=%d valid=%v", len(got), utf8.ValidString(got))
	}
}

type resultWhitelist struct {
	cloud.Whitelist
	results map[string]error
}

func (w *resultWhitelist) AddIP(_ context.Context, t cloud.Target, _ string) error {
	return w.results[t.ID]
}

func TestWhitelistResultsUseErrorBannerForAnyFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failures  map[string]error
		wantError bool
	}{
		{"success", nil, false},
		{"mixed", map[string]error{"two": errors.New("Forbidden fake-secret-value")}, true},
		{"failure", map[string]error{"one": errors.New("appended IP not visible yet; refresh before retry"), "two": errors.New("Forbidden fake-secret-value")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &resultWhitelist{results: tc.failures}
			a := New(Platform{Store: &memStore{}, Whitelist: func(cloud.Account) (cloud.Whitelist, error) { return w, nil }})
			defer a.Close()
			profile, err := a.accounts.Save(cloud.Account{Name: "测试账号", AccessKeyID: "fake-key-id", AccessKeySecret: "fake-secret-value"})
			if err != nil {
				t.Fatal(err)
			}
			a.profiles = []cloud.Account{profile}
			a.publicIP = "192.0.2.1"
			a.notice = "previous success"
			a.addWhitelist([]cloud.Target{{AccountID: profile.ID, ID: "one", Name: "甲"}, {AccountID: profile.ID, ID: "two", Name: "乙"}}, false)
			a.workers.Wait()
			a.drain()
			if (a.err != "") != tc.wantError || (tc.wantError && a.notice != "") {
				t.Fatalf("incorrect banner: error=%q notice=%q", a.err, a.notice)
			}
			result := a.err + a.notice
			if strings.Contains(result, "fake-secret-value") || !strings.Contains(result, "甲") || !strings.Contains(result, "乙") {
				t.Fatalf("unsafe or incomplete result: %q", result)
			}
			if tc.name == "mixed" && (!strings.Contains(a.err, "1 项") || !strings.Contains(a.err, "已确认添加")) {
				t.Fatalf("partial success not represented: %q", a.err)
			}
		})
	}
}

func TestManagedWhitelistErrorsExplainRecoveryAndKeepAction(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"managed whitelist ownership scope changed or journal is invalid; inspect the existing configuration", "本地受管白名单记录"},
		{"managed whitelist ownership journal is invalid; refusing cloud changes", "停止同步"},
		{"cannot persist managed whitelist ownership: disk full", "保存所有权记录"},
		{"cannot read managed whitelist ownership: store unavailable", "读取所有权记录"},
		{"dedicated whitelist group already exists without ownership proof; it will not be adopted", "不会接管"},
		{"dedicated whitelist group has no confirmed ownership; existing group will not be adopted", "云控制台"},
		{"whitelist group creation outcome is uncertain; existing group will not be adopted", "不会接管"},
		{"managed security-group rule was modified; refusing automatic cleanup", "其他修改"},
		{"managed whitelist group metadata changed; refusing cloud changes", "其他修改"},
		{"managed whitelist group disappeared; refusing to adopt or recreate it", "其他修改"},
		{"new managed security-group rule is not visible; old rules retained", "暂停后续清理"},
		{"submitted managed IP is not visible; old entries retained", "先刷新"},
		{"new managed IP disappeared; old entries retained", "暂停后续清理"},
		{"old managed rule is still visible; retry cleanup later", "清理尚未确认"},
		{"new IP verified; previous managed rule cleanup failed: RevokeSecurityGroup: Forbidden。请核对 RAM 权限 ecs:RevokeSecurityGroup。", "新 IP 已核验"},
		{"managed RDS/Redis whitelists require an IPv4 address", "IPv4"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got := (&App{}).safeError(errors.New(tc.input))
			if !strings.Contains(got, tc.want) || !strings.Contains(got, tc.input) {
				t.Fatalf("missing recovery or original action in %q", got)
			}
		})
	}
}

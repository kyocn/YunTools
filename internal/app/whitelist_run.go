package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"yuntools/internal/cloud"
)

type whitelistResult struct {
	target     cloud.Target
	account    cloud.Account
	hasAccount bool
	ip         string
	err        error
}

func (a *App) whitelistChecks(targets []cloud.Target) []whitelistResult {
	checks := make([]whitelistResult, 0, len(targets))
	for _, target := range targets {
		account, ok := a.whitelistAccountSnapshot(target.AccountID)
		checks = append(checks, whitelistResult{target: target, account: account, hasAccount: ok})
	}
	return checks
}

func (a *App) executeWhitelist(ctx context.Context, checks []whitelistResult, fallback string, detectIP func(context.Context) (string, error)) []whitelistResult {
	var detected string
	var detectionError error
	detectionDone := false
	for i := range checks {
		result := &checks[i]
		ip := result.target.SourceIP
		if result.target.Automatic {
			ip = ""
		} else if ip == "" {
			ip = fallback
		}
		if strings.TrimSpace(ip) == "" {
			if !detectionDone {
				detected, detectionError = detectIP(ctx)
				detectionDone = true
			}
			ip = detected
			result.err = detectionError
		}
		if result.err != nil {
			continue
		}
		result.ip, result.err = normalizeTargetIP(ip, result.target.Kind)
		if result.err == nil && result.ip == "" {
			result.err = errors.New("未检测到有效公网 IP，请稍后重试或关闭自动跟随后手动填写")
		}
		if result.err != nil {
			continue
		}
		if !result.hasAccount {
			result.err = errors.New("任务绑定的账号不存在，请重新添加原账号")
			continue
		}
		provider, err := a.p.Whitelist(result.account)
		result.err = err
		if err != nil {
			continue
		}
		if result.target.Automatic {
			dynamic, ok := provider.(cloud.DynamicWhitelist)
			if !ok {
				result.err = errors.New("此云平台暂不支持自动替换 IP，请使用手动加白")
				continue
			}
			result.err = dynamic.SyncIP(ctx, result.target, result.ip, a.store)
		} else {
			result.err = provider.AddIP(ctx, result.target, result.ip)
		}
	}
	return checks
}

// Configuration advances only after the corresponding cloud rule is verified.
// Stale completions cannot rewrite edits, removed targets or another account.
func (a *App) applyWhitelistResults(checks []whitelistResult) {
	next := append([]cloud.Target(nil), a.rules...)
	changed := false
	for _, result := range checks {
		if !result.target.Automatic || result.err != nil {
			continue
		}
		account, ok := a.whitelistAccountSnapshot(result.target.AccountID)
		if !ok || account != result.account {
			continue
		}
		for i, target := range next {
			if target == result.target && target.SourceIP != result.ip {
				next[i].SourceIP = result.ip
				changed = true
				break
			}
		}
	}
	var saveError error
	if changed {
		saveError = a.writeWhitelistTargets(next)
	}
	if a.whitelistFeedback == nil {
		a.whitelistFeedback = make(map[cloud.Target]whitelistAutomaticFeedback)
	}
	for i := range checks {
		result := &checks[i]
		if !result.target.Automatic {
			continue
		}
		account, ok := a.whitelistAccountSnapshot(result.target.AccountID)
		if !ok || account != result.account {
			continue
		}
		target := result.target
		if result.err == nil && saveError == nil {
			target.SourceIP = result.ip
		}
		if !a.hasAutomaticWhitelistTarget(target) {
			continue
		}
		if result.err == nil && saveError != nil {
			result.err = fmt.Errorf("云端已同步，但本机 IP 保存失败，将在下次检查重试：%w", saveError)
		}
		status := whitelistAutomaticFeedback{CheckedAt: time.Now(), IP: result.ip}
		if result.err != nil {
			status.Error = a.safeError(result.err)
		}
		delete(a.whitelistFeedback, result.target)
		a.whitelistFeedback[target] = status
	}
}

func (a *App) runManualWhitelist(targets []cloud.Target, detectIP func(context.Context) (string, error)) {
	if a.whitelistManualRunning || a.whitelistAutomaticRunning {
		return
	}
	prepared, err := a.prepareWhitelistTargets(targets)
	if err != nil {
		a.err = a.safeError(err)
		return
	}
	checks := a.whitelistChecks(prepared)
	fallback, gen := strings.TrimSpace(a.publicIP), a.generation
	a.notice = ""
	a.whitelistManualRunning = true
	a.asyncGlobal(func(ctx context.Context) (func(), error) {
		checks = a.executeWhitelist(ctx, checks, fallback, detectIP)
		return func() {
			a.whitelistManualRunning = false
			a.applyWhitelistResults(checks)
			if a.generation != gen {
				return
			}
			var lines []string
			failed := 0
			for _, result := range checks {
				name := result.target.Name
				if name == "" {
					name = result.target.ID
				}
				if result.err != nil {
					failed++
					lines = append(lines, name+"："+a.safeError(result.err))
				} else {
					lines = append(lines, name+"：已确认添加 IP "+result.ip)
				}
			}
			if failed > 0 {
				a.err = fmt.Sprintf("已确认 %d 项，%d 项未能确认完成。请查看下方原因和处理方法。\n", len(checks)-failed, failed) + strings.Join(lines, "\n")
			} else {
				a.notice = strings.Join(lines, "\n")
			}
		}, nil
	})
}

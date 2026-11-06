package app

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
	"yuntools/internal/theme"
)

type whitelistAutomaticFeedback struct {
	CheckedAt time.Time
	IP, Error string
}

func (a *App) whitelistAccountSnapshot(id string) (cloud.Account, bool) {
	for _, account := range a.profiles {
		if account.ID == id {
			return account, true
		}
	}
	return cloud.Account{}, false
}

func (a *App) hasAutomaticWhitelistTarget(target cloud.Target) bool {
	for _, current := range a.rules {
		if current == target && current.Automatic {
			return true
		}
	}
	return false
}

// Unlike foreground dispatch, this worker never clears or writes global
// banners. Snapshots belong to the configured target, not the currently open
// page, and results are applied only while the target and account still match.
func (a *App) runAutomaticWhitelist(targets []cloud.Target, detectIP func(context.Context) (string, error)) {
	if a.whitelistAutomaticRunning {
		return
	}
	type check struct {
		target     cloud.Target
		account    cloud.Account
		hasAccount bool
		err        error
	}
	var checks []check
	for _, target := range targets {
		if a.hasAutomaticWhitelistTarget(target) {
			account, ok := a.whitelistAccountSnapshot(target.AccountID)
			checks = append(checks, check{target: target, account: account, hasAccount: ok})
		}
	}
	if len(checks) == 0 {
		return
	}
	for target := range a.whitelistFeedback {
		if !a.hasAutomaticWhitelistTarget(target) {
			delete(a.whitelistFeedback, target)
		}
	}
	a.whitelistAutomaticRunning = true
	a.busy++
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
		defer cancel()
		ip, detectionError := detectIP(ctx)
		for i := range checks {
			result := &checks[i]
			result.err = detectionError
			if result.err != nil {
				continue
			}
			if !result.hasAccount {
				result.err = errors.New("任务绑定的账号不存在，请重新添加原账号")
				continue
			}
			provider, err := a.p.Whitelist(result.account)
			result.err = err
			if err == nil {
				// Always verify remote rules, even when the public IP is unchanged.
				result.err = provider.AddIP(ctx, result.target, ip)
			}
		}
		checkedAt := time.Now()
		a.post(func() {
			a.busy--
			a.whitelistAutomaticRunning = false
			for _, result := range checks {
				account, ok := a.whitelistAccountSnapshot(result.target.AccountID)
				if !a.hasAutomaticWhitelistTarget(result.target) || ok != result.hasAccount || account != result.account {
					continue
				}
				status := whitelistAutomaticFeedback{CheckedAt: checkedAt, IP: ip}
				if result.err != nil {
					status.Error = a.safeError(result.err)
				}
				if a.whitelistFeedback == nil {
					a.whitelistFeedback = make(map[cloud.Target]whitelistAutomaticFeedback)
				}
				a.whitelistFeedback[result.target] = status
			}
		})
	}()
}

func (a *App) whitelistAutomaticStatus(c *ui.Context, target cloud.Target) {
	if !target.Automatic {
		return
	}
	p := theme.Colors(a.dark)
	status, ok := a.whitelistFeedback[target]
	if !ok {
		ui.Text(c, "自动检查：等待下一次检查").FontSize(11).TextColor(p.Muted)
		return
	}
	when := status.CheckedAt.Local().Format("01-02 15:04:05")
	if status.Error == "" {
		ui.Text(c, "自动检查：已确认 IP "+status.IP+" · "+when).FontSize(11).TextColor(p.Green)
		return
	}
	ui.Text(c, "自动检查：暂未确认完成 · "+when).FontSize(11).TextColor(p.Orange)
	ui.Text(c, status.Error).FillWidth().MaxLines(2).FontSize(11).TextColor(p.Muted).Selectable().Tooltip(status.Error)
}

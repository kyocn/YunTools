package app

import (
	"context"
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
	if a.whitelistAutomaticRunning || a.whitelistManualRunning {
		return
	}
	var selected []cloud.Target
	for _, target := range targets {
		if a.hasAutomaticWhitelistTarget(target) {
			selected = append(selected, target)
		}
	}
	if len(selected) == 0 {
		return
	}
	selected, err := a.prepareWhitelistTargets(selected)
	if err != nil {
		if a.whitelistFeedback == nil {
			a.whitelistFeedback = make(map[cloud.Target]whitelistAutomaticFeedback)
		}
		for _, target := range targets {
			if a.hasAutomaticWhitelistTarget(target) {
				a.whitelistFeedback[target] = whitelistAutomaticFeedback{CheckedAt: time.Now(), Error: a.safeError(err)}
			}
		}
		return
	}
	checks := a.whitelistChecks(selected)
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
		checks = a.executeWhitelist(ctx, checks, "", detectIP)
		a.post(func() { a.busy--; a.whitelistAutomaticRunning = false; a.applyWhitelistResults(checks) })
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

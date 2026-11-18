package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
	"yuntools/internal/certificates"
	"yuntools/internal/cloud"
	"yuntools/internal/tasks"
)

const (
	appearanceAuto  = "auto"
	appearanceLight = "light"
	appearanceDark  = "dark"
)

type preferences struct {
	Dark       bool
	Tabs       []tab
	Active     int
	AutoRenew  map[string]bool
	Appearance string
}

func (a *App) loadPreferences() error {
	b, e := a.store.Get("preferences/v1")
	if errors.Is(e, cloud.ErrNotFound) {
		a.autoRenew = map[string]bool{}
		return nil
	}
	if e != nil {
		return e
	}
	var p preferences
	if e = json.Unmarshal(b, &p); e != nil {
		return e
	}
	if validAppearance(p.Appearance) {
		a.appearance = p.Appearance
		a.saveAppearanceFile()
	}
	a.autoRenew = p.AutoRenew
	if a.autoRenew == nil {
		a.autoRenew = map[string]bool{}
	}
	a.tabs = p.Tabs
	a.activeTab = p.Active
	if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
		t := a.tabs[a.activeTab]
		for _, profile := range a.profiles {
			if profile.ID == t.AccountID {
				a.accountID = t.AccountID
				a.region = profile.Region
				a.page = t.Page
				break
			}
		}
	}
	return nil
}
func (a *App) savePreferences() {
	if a.store == nil || a.locked {
		return
	}
	b, e := json.Marshal(preferences{Dark: a.dark, Tabs: a.tabs, Active: a.activeTab, AutoRenew: a.autoRenew, Appearance: a.appearance})
	if e == nil {
		e = a.store.Put("preferences/v1", b)
	}
	if e != nil {
		a.err = "无法保存设置：" + e.Error()
	}
}
func validAppearance(mode string) bool {
	return mode == appearanceAuto || mode == appearanceLight || mode == appearanceDark
}

func (a *App) resolvedDark(systemDark bool) bool {
	switch a.appearance {
	case appearanceLight:
		return false
	case appearanceDark:
		return true
	default:
		return systemDark
	}
}

func (a *App) loadAppearanceFile() {
	a.appearance = appearanceAuto
	if a.p.DataDir == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(a.p.DataDir, "appearance.json"))
	if err != nil {
		return
	}
	var saved struct{ Appearance string }
	if json.Unmarshal(b, &saved) == nil && validAppearance(saved.Appearance) {
		a.appearance = saved.Appearance
	}
}

func (a *App) saveAppearanceFile() {
	if a.p.DataDir == "" {
		return
	}
	b, err := json.Marshal(struct {
		Appearance string `json:"appearance"`
	}{a.appearance})
	if err != nil {
		return
	}
	if os.MkdirAll(a.p.DataDir, 0700) != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(a.p.DataDir, "appearance.json"), b, 0600)
}

func (a *App) setAppearance(mode string) {
	if !validAppearance(mode) {
		return
	}
	a.appearance = mode
	a.saveAppearanceFile()
	a.savePreferences()
}

func (a *App) cycleAppearance() {
	switch a.appearance {
	case appearanceLight:
		a.setAppearance(appearanceDark)
	case appearanceDark:
		a.setAppearance(appearanceAuto)
	default:
		a.setAppearance(appearanceLight)
	}
}

func (a *App) appearanceLabel() string {
	switch a.appearance {
	case appearanceLight:
		return "浅色"
	case appearanceDark:
		return "深色"
	default:
		return "跟随系统"
	}
}
func (a *App) toggleRenew(c certificates.Certificate) {
	key := c.AccountID + "/" + c.ID
	if a.autoRenew == nil {
		a.autoRenew = map[string]bool{}
	}
	a.autoRenew[key] = !a.autoRenew[key]
	a.savePreferences()
	a.certNext = time.Time{}
}
func renewalDue(c certificates.Certificate, enabled bool, jobs []tasks.Job, now time.Time) bool {
	if !enabled {
		return false
	}
	due := c.RenewAfter
	if due.IsZero() {
		due = c.ExpiresAt.Add(-7 * 24 * time.Hour)
	}
	if now.Before(due) {
		return false
	}
	for _, j := range jobs {
		if j.AccountID != c.AccountID || j.Kind != "renew" || j.State == tasks.Done {
			continue
		}
		var id string
		if json.Unmarshal(j.Payload, &id) == nil && id == c.ID {
			return false
		}
	}
	return true
}
func (a *App) renewalTick() {
	if a.locked || a.certs == nil || a.jobs == nil || time.Now().Before(a.certNext) {
		return
	}
	a.certNext = time.Now().Add(time.Hour)
	profiles := append([]cloud.Account(nil), a.profiles...)
	a.asyncGlobal(func(ctx context.Context) (func(), error) {
		var certificates []certificates.Certificate
		for _, p := range profiles {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			list, e := a.certs.List(p.ID)
			if e != nil {
				return nil, e
			}
			certificates = append(certificates, list...)
		}
		return func() {
			for _, cert := range certificates {
				if renewalDue(cert, a.autoRenew[cert.AccountID+"/"+cert.ID], a.jobs.Snapshot(), time.Now()) {
					a.renewCertificate(cert)
				}
			}
		}, nil
	})
}

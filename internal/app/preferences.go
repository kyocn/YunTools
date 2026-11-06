package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"yuntools/internal/certificates"
	"yuntools/internal/cloud"
	"yuntools/internal/tasks"
)

type preferences struct {
	Dark      bool
	Tabs      []tab
	Active    int
	AutoRenew map[string]bool
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
	a.dark = p.Dark
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
	b, e := json.Marshal(preferences{a.dark, a.tabs, a.activeTab, a.autoRenew})
	if e == nil {
		e = a.store.Put("preferences/v1", b)
	}
	if e != nil {
		a.err = "无法保存设置：" + e.Error()
	}
}
func (a *App) toggleTheme() { a.dark = !a.dark; a.savePreferences() }
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

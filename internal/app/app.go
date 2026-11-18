package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/accounts"
	"yuntools/internal/certificates"
	"yuntools/internal/cloud"
	"yuntools/internal/providers/aliyun"
	"yuntools/internal/providers/aliyunoss"
	"yuntools/internal/providers/qiniukodo"
	"yuntools/internal/providers/tencent"
	"yuntools/internal/providers/tencentcos"
	"yuntools/internal/tasks"
)

type Platform struct {
	Version             string
	UpdateStatus        func() UpdateStatus
	CheckUpdates        func()
	SetAutomaticUpdates func(bool)
	// Post schedules asynchronously on the UI thread, even while the window is hidden.
	Post       func(func())
	Store      cloud.SecretStore
	Unlock     func() (cloud.SecretStore, error)
	PickFiles  func() ([]string, error)
	PickFolder func() (string, error)
	SaveFile   func(string) (string, error)
	CopyText   func(string) error
	DataDir    string
	Storage    func(cloud.Account) (cloud.ObjectStorage, error)
	DNS        func(cloud.Account) (cloud.ChallengeDNS, error)
	Whitelist  func(cloud.Account) (cloud.Whitelist, error)
}
type UpdateStatus struct {
	Enabled, Automatic           bool
	Repository, AvailableVersion string
	LastCheck                    time.Time
}
type confirmation struct {
	Title, Detail string
	Run           func()
}
type form struct {
	Kind, Title    string
	Fields         map[string]string
	Account        cloud.Account
	OriginalTarget *cloud.Target
	Terms, Staging bool
}
type tab struct{ AccountID, Page, Label string }
type App struct {
	p                                 Platform
	store                             cloud.SecretStore
	accounts                          *accounts.Manager
	jobs                              *tasks.Manager
	certs                             *certificates.Service
	ctx                               context.Context
	cancel                            context.CancelFunc
	workers                           sync.WaitGroup
	queue                             chan func()
	profiles                          []cloud.Account
	accountID, page                   string
	generation                        uint64
	busy                              int
	err, notice                       string
	dark, locked                      bool
	appearance                        string
	tabs                              []tab
	activeTab                         int
	showTasks                         bool
	dialog                            *form
	ruleDetail                        *ruleDetails
	securityRuleTable                 ui.ListState
	confirm                           *confirmation
	bucket, prefix, nextToken, search string
	buckets                           []cloud.Bucket
	objects                           []cloud.Object
	selected                          map[string]bool
	pages                             map[string]*listPager
	objectPager                       objectPagination
	domain                            string
	domains                           []cloud.Domain
	records                           []cloud.Record
	queryResult                       string
	resourceKind, region, publicIP    string
	targets                           []cloud.Target
	targetDiscovery                   discoveryState
	rules                             []cloud.Target
	targetRules                       map[string][]string
	whitelistFeedback                 map[cloud.Target]whitelistAutomaticFeedback
	whitelistAutomaticRunning         bool
	whitelistManualRunning            bool
	certList                          []certificates.Certificate
	autoBusy                          bool
	autoNext                          time.Time
	autoRenew                         map[string]bool
	certNext                          time.Time
	svc                               ui.Services
	live                              bool
}

func New(p Platform) *App {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{p: p, ctx: ctx, cancel: cancel, queue: make(chan func(), 256), page: "overview", appearance: appearanceAuto, locked: true, selected: map[string]bool{}, resourceKind: "rds", region: "cn-hangzhou", targetRules: map[string][]string{}, activeTab: -1}
	a.loadAppearanceFile()
	if a.p.Storage == nil {
		a.p.Storage = func(c cloud.Account) (cloud.ObjectStorage, error) {
			switch cloud.NormalizeProvider(c.Provider) {
			case cloud.ProviderTencent:
				return tencentcos.New(c)
			case cloud.ProviderQiniu:
				return qiniukodo.New(c)
			default:
				return aliyunoss.New(c)
			}
		}
	}
	if a.p.DNS == nil {
		a.p.DNS = func(c cloud.Account) (cloud.ChallengeDNS, error) {
			switch cloud.NormalizeProvider(c.Provider) {
			case cloud.ProviderTencent:
				return tencent.New(c)
			case cloud.ProviderQiniu:
				return nil, cloud.ErrUnsupportedProviderCapability
			default:
				return aliyun.New(c)
			}
		}
	}
	if a.p.Whitelist == nil {
		a.p.Whitelist = func(c cloud.Account) (cloud.Whitelist, error) {
			switch cloud.NormalizeProvider(c.Provider) {
			case cloud.ProviderTencent:
				return tencent.New(c)
			case cloud.ProviderQiniu:
				return nil, cloud.ErrUnsupportedProviderCapability
			default:
				return aliyun.New(c)
			}
		}
	}
	if p.Store != nil {
		a.attach(p.Store)
	}
	if p.Post != nil {
		a.workers.Add(1)
		go func() {
			defer a.workers.Done()
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					a.post(func() { a.autoTick(); a.renewalTick() })
				}
			}
		}()
	}
	return a
}
func (a *App) attach(s cloud.SecretStore) {
	a.store = s
	a.accounts = accounts.New(s)
	profiles, e := a.accounts.List()
	if e != nil {
		a.err = a.safeError(e)
		return
	}
	a.profiles = profiles
	a.certs = certificates.New(s, a.p.DNS)
	a.jobs, e = tasks.New(s, a.runTask)
	if e != nil {
		a.err = a.safeError(e)
		return
	}
	if b, e := s.Get("whitelist/rules"); e == nil {
		if e = json.Unmarshal(b, &a.rules); e != nil {
			a.err = "白名单规则存储损坏"
			return
		}
	} else if !errors.Is(e, cloud.ErrNotFound) {
		a.err = a.safeError(e)
		return
	}
	a.locked = false
	if len(profiles) > 0 {
		a.accountID = profiles[0].ID
		a.region = profiles[0].Region
	}
	a.autoNext = time.Now().Add(time.Minute)
	if e = a.loadPreferences(); e != nil {
		a.err = "无法加载设置：" + e.Error()
	}
}
func (a *App) Close() {
	a.cancel()
	if a.jobs != nil {
		a.jobs.Close()
	}
	a.workers.Wait()
}
func (a *App) post(fn func()) {
	if a.p.Post != nil {
		a.p.Post(func() {
			if a.ctx.Err() != nil {
				return
			}
			fn()
			if a.live {
				a.svc.Invalidate()
			}
		})
		return
	}
	select {
	case a.queue <- fn:
	case <-a.ctx.Done():
	}
}
func (a *App) drain() {
	for {
		select {
		case fn := <-a.queue:
			fn()
		default:
			return
		}
	}
}
func (a *App) current() (cloud.Account, error) {
	for _, p := range a.profiles {
		if p.ID == a.accountID {
			return p, nil
		}
	}
	return cloud.Account{}, errors.New("请先添加并选择云账号")
}
func (a *App) profile(id string) (cloud.Account, error) {
	ps, e := a.accounts.List()
	if e != nil {
		return cloud.Account{}, e
	}
	for _, p := range ps {
		if p.ID == id {
			return p, nil
		}
	}
	return cloud.Account{}, errors.New("任务绑定的账号不存在，请重新添加原账号")
}
func (a *App) applyResult(gen uint64, fn func()) {
	if a.generation == gen {
		fn()
	}
}
func (a *App) switchAccount(id string) {
	if id == a.accountID {
		return
	}
	a.accountID = id
	a.generation++
	a.resetResources()
	if p, e := a.current(); e == nil {
		a.region = p.Region
	}
}

// selectAccount switches the working account and keeps the current page.
// Resource pages get their own tab per account, so the highlighted tab matches the data.
func (a *App) selectAccount(id string) {
	if id == "" || id == a.accountID {
		return
	}
	page := a.page
	a.switchAccount(id)
	if pageUsesAccount(page) {
		a.openPage(page)
		return
	}
	a.refresh()
	a.savePreferences()
}
func (a *App) resetResources() {
	a.resetTargetDiscovery()
	a.objectPager = objectPagination{}
	a.pages = nil
	a.dialog = nil
	a.ruleDetail = nil
	a.confirm = nil
	a.queryResult = ""
	a.buckets = nil
	a.objects = nil
	a.records = nil
	a.domains = nil
	a.targets = nil
	a.certList = nil
	a.bucket = ""
	a.prefix = ""
	a.domain = ""
	a.nextToken = ""
	a.selected = map[string]bool{}
	a.search = ""
}

// async captures the current view generation; results cannot leak into another account.
func (a *App) async(work func(context.Context) (func(), error)) {
	a.dispatch(true, work)
}
func (a *App) asyncGlobal(work func(context.Context) (func(), error)) { a.dispatch(false, work) }
func (a *App) dispatch(guarded bool, work func(context.Context) (func(), error)) {
	gen := a.generation
	a.busy++
	a.err = ""
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
		defer cancel()
		done, e := work(ctx)
		a.post(func() {
			a.busy--
			if guarded && a.generation != gen {
				return
			}
			if e != nil {
				a.err = a.safeError(e)
				return
			}
			if done != nil {
				done()
			}
		})
	}()
}
func (a *App) safeError(e error) string {
	s := e.Error()
	for _, p := range a.profiles {
		for _, v := range []string{p.AccessKeyID, p.AccessKeySecret, p.SecurityToken} {
			if len(v) > 3 {
				s = strings.ReplaceAll(s, v, "[已隐藏]")
			}
		}
	}
	return shortenError(friendlyError(s))
}
func pageUsesAccount(page string) bool {
	switch page {
	case "oss", "whitelist", "dns", "certificates":
		return true
	default:
		return false
	}
}

func (a *App) openPage(page string) {
	defer a.savePreferences()
	a.generation++
	a.page = page
	a.search = ""
	accountID := ""
	if pageUsesAccount(page) {
		accountID = a.accountID
	}
	for i, t := range a.tabs {
		samePage := t.Page == page
		sameAccount := !pageUsesAccount(page) || t.AccountID == accountID
		if samePage && sameAccount {
			a.tabs[i].AccountID = accountID
			a.tabs[i].Label = pageTitle(page)
			a.activeTab = i
			a.refresh()
			return
		}
	}
	a.tabs = append(a.tabs, tab{AccountID: accountID, Page: page, Label: pageTitle(page)})
	a.activeTab = len(a.tabs) - 1
	a.refresh()
}
func (a *App) closeTab(index int) {
	a.closeTabs(index, index+1)
}
func (a *App) closeTabs(start, end int) {
	if start < 0 || end > len(a.tabs) || start >= end {
		return
	}
	active := a.activeTab
	a.tabs = append(a.tabs[:start], a.tabs[end:]...)
	if len(a.tabs) == 0 {
		a.generation++
		a.resetResources()
		a.page = "overview"
		a.activeTab = -1
		a.err, a.notice = "", ""
	} else if active >= start && active < end {
		a.generation++
		a.resetResources()
		a.activeTab = min(start, len(a.tabs)-1)
		next := a.tabs[a.activeTab]
		if next.AccountID != "" {
			a.switchAccount(next.AccountID)
		}
		a.page = next.Page
		a.err, a.notice = "", ""
		a.refresh()
	} else if end <= active {
		a.activeTab -= end - start
	}
	a.savePreferences()
}
func (a *App) currentProvider() string {
	for _, p := range a.profiles {
		if p.ID == a.accountID {
			return cloud.NormalizeProvider(p.Provider)
		}
	}
	return cloud.ProviderAliyun
}

func (a *App) refresh() {
	if a.locked {
		return
	}
	provider := a.currentProvider()
	switch a.page {
	case "oss":
		a.refreshOSS(false)
	case "dns":
		if cloud.ProviderSupportsDNS(provider) {
			a.refreshDNS()
		}
	case "whitelist":
		if cloud.ProviderSupportsWhitelist(provider) {
			a.refreshTargets()
		}
	case "certificates":
		if cloud.ProviderSupportsCertificates(provider) {
			a.refreshCertificates()
		}
	}
}
func (a *App) storageAccount() (cloud.Account, error) {
	p, e := a.current()
	if e != nil {
		return p, e
	}
	for _, b := range a.buckets {
		if b.Name == a.bucket && b.Region != "" {
			p.Region = strings.TrimPrefix(b.Region, "oss-")
		}
	}
	return p, nil
}
func (a *App) runTask(ctx context.Context, j tasks.Job, emit func(cloud.Progress)) error {
	p, e := a.profile(j.AccountID)
	if e != nil {
		return e
	}
	switch j.Kind {
	case "upload", "download":
		var payload struct {
			Transfer cloud.Transfer
			Region   string
		}
		if e = json.Unmarshal(j.Payload, &payload); e != nil {
			return e
		}
		if payload.Region != "" {
			p.Region = payload.Region
		}
		payload.Transfer.CheckpointDir = filepath.Join(a.p.DataDir, "checkpoints", p.ID, j.ID)
		if e = os.MkdirAll(payload.Transfer.CheckpointDir, 0700); e != nil {
			return e
		}
		client, e := a.p.Storage(p)
		if e != nil {
			return e
		}
		if j.Kind == "upload" {
			return client.Upload(ctx, payload.Transfer, emit)
		}
		return client.Download(ctx, payload.Transfer, emit)
	case "certificate":
		var req certificates.Request
		if e = json.Unmarshal(j.Payload, &req); e != nil {
			return e
		}
		_, e = a.certs.Issue(ctx, p, req, emit)
		return e
	case "renew":
		var id string
		if e = json.Unmarshal(j.Payload, &id); e != nil {
			return e
		}
		_, e = a.certs.Renew(ctx, p, id, emit)
		return e
	}
	return fmt.Errorf("未知任务类型 %s", j.Kind)
}
func (a *App) submitTransfer(kind string, p cloud.Account, tr cloud.Transfer) {
	b, e := json.Marshal(struct {
		Transfer cloud.Transfer
		Region   string
	}{tr, p.Region})
	if e == nil {
		_, e = a.jobs.Submit(p.ID, kind, filepath.Base(tr.LocalPath), b)
	}
	if e != nil {
		a.err = a.safeError(e)
	} else {
		a.showTasks = true
		a.notice = "任务已加入传输中心"
	}
}

func pageTitle(p string) string {
	switch p {
	case "oss":
		return "对象存储"
	case "whitelist":
		return "访问白名单"
	case "dns":
		return "域名解析"
	case "certificates":
		return "SSL 证书"
	case "accounts":
		return "云账号"
	case "settings":
		return "设置"
	}
	return "概览"
}

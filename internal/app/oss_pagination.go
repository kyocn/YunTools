package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
	"yuntools/internal/theme"
)

// Keep only the current object page and up to 256 request tokens. Old pages are
// fetched again when navigating back; no object cache grows with browsing.
type objectPagination struct {
	scope, query, next string
	page, base, size   int
	tokens             []string
	request            uint64
}

func (p *objectPagination) reset(scope string) {
	p.scope = scope
	p.page, p.base = 0, 0
	p.tokens = []string{""}
	p.next = ""
	p.request++
	if p.size == 0 {
		p.size = 20
	}
}
func (p *objectPagination) token(page int) (string, bool) {
	if page == 0 {
		return "", true
	}
	i := page - p.base
	if i < 0 || i >= len(p.tokens) {
		return "", false
	}
	return p.tokens[i], true
}
func (p *objectPagination) commit(page int, token, next string) {
	if page == 0 {
		p.base = 0
		p.tokens = []string{""}
	} else {
		i := page - p.base
		if i < 0 || i > len(p.tokens) {
			p.base = page
			p.tokens = []string{token}
		} else {
			p.tokens = append(p.tokens[:i], token)
		}
		if len(p.tokens) > 256 {
			p.tokens = append([]string(nil), p.tokens[len(p.tokens)-256:]...)
			p.base = page - 255
		}
	}
	p.page, p.next = page, next
}
func (a *App) ossScope() string { return a.accountID + "\x00" + a.bucket + "\x00" + a.prefix }
func (a *App) prepareOSSPager() {
	scope := a.ossScope()
	if a.objectPager.scope != scope {
		a.objectPager.query = ""
		a.objectPager.reset(scope)
		a.search = ""
	}
}
func (a *App) ossSearchChanged() {
	query := strings.TrimSpace(a.search)
	a.prepareOSSPager()
	a.search = query
	a.objectPager.query = query
	a.refreshOSS(false)
}
func (a *App) previousOSSPage() {
	p := &a.objectPager
	if token, ok := p.token(p.page - 1); ok && p.page > 0 {
		a.loadOSSObjectPage(p.page-1, token)
	}
}
func (a *App) loadOSSObjectPage(page int, token string) {
	account, err := a.storageAccount()
	if err != nil {
		return
	}
	p := &a.objectPager
	p.request++
	request, scope, query := p.request, p.scope, p.query
	bucket, prefix, size := a.bucket, a.prefix+query, p.size
	a.selected = map[string]bool{}
	a.async(func(ctx context.Context) (func(), error) {
		client, err := a.p.Storage(account)
		var result cloud.ObjectPage
		if err == nil {
			if pager, ok := client.(cloud.ObjectPageLister); ok {
				result, err = pager.ListObjectsPage(ctx, bucket, prefix, token, size)
			} else {
				result, err = client.ListObjects(ctx, bucket, prefix, token)
			}
		}
		// Handle errors only after the same request checks as successful data.
		return func() {
			if p.request != request || p.scope != scope || a.ossScope() != scope || p.query != query {
				return
			}
			if err != nil {
				a.err = a.safeError(err)
				return
			}
			p.commit(page, token, result.NextToken)
			a.selected = map[string]bool{}
			a.objects = result.Objects
			a.nextToken = result.NextToken
		}, nil
	})
}
func (a *App) ossPaginationFooter(c *ui.Context) {
	p := &a.objectPager
	color := theme.Colors(a.dark)
	ui.Row(c.Key("oss-remote-pager")).FillWidth().Gap(8).Padding(7, 0).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("第 %d 页 · 本页 %d 项", p.page+1, len(a.objects))).FontSize(11).TextColor(color.Muted)
		ui.Box(c).Grow(1)
		size := strconv.Itoa(max(20, p.size))
		ui.Text(c, "每页").FontSize(11).TextColor(color.Muted)
		if ui.Select(c, &size, []string{"20", "50", "100"}).Width(70).Label("每页对象数").Disabled(a.busy > 0).Changed() {
			p.size, _ = strconv.Atoi(size)
			a.refreshOSS(false)
		}
		if ui.Button(c, "首页").Disabled(p.page == 0 || a.busy > 0).Clicked() {
			a.refreshOSS(false)
		}
		_, previous := p.token(p.page - 1)
		if ui.Button(c, "上一页").Disabled(p.page == 0 || !previous || a.busy > 0).Tooltip("可返回最近浏览的 256 页；更早内容请回首页").Clicked() {
			a.previousOSSPage()
		}
		if ui.Button(c, "下一页").Disabled(p.next == "" || a.busy > 0).Clicked() {
			a.refreshOSS(true)
		}
	})
}

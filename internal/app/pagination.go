package app

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/theme"
)

type listPager struct {
	filter     string
	page, size int
}

// pageRange paginates an already-filtered list. Use a stable key for each list
// and include account, view and search in filter to reset when its scope changes.
func (a *App) pageRange(key, filter string, total int) (int, int) {
	if a.pages == nil {
		a.pages = make(map[string]*listPager)
	}
	p := a.pages[key]
	if p == nil {
		p = &listPager{size: 20}
		a.pages[key] = p
	}
	if p.filter != filter {
		p.filter = filter
		p.page = 0
	}
	if p.size <= 0 {
		p.size = 20
	}
	total = max(0, total)
	p.page = max(0, min(p.page, max(0, (total-1)/p.size)))
	start := p.page * p.size
	return start, min(start+p.size, total)
}

func (a *App) pageFooter(c *ui.Context, key, filter string, total int) {
	start, end := a.pageRange(key, filter, total)
	p := a.pages[key]
	color := theme.Colors(a.dark)
	ui.Row(c.Key("pager:"+key)).FillWidth().AlignItems(ui.Center).Gap(8).Padding(7, 0).Children(func() {
		first := start + 1
		if total == 0 {
			first = 0
		}
		ui.Text(c, fmt.Sprintf("共 %d 项 · 第 %d–%d 项", total, first, end)).FontSize(11).TextColor(color.Muted)
		ui.Box(c).Grow(1)
		size := strconv.Itoa(p.size)
		ui.Text(c, "每页").FontSize(11).TextColor(color.Muted)
		if ui.Select(c, &size, []string{"20", "50", "100"}).Width(70).Label("每页显示条数").Changed() {
			p.size, _ = strconv.Atoi(size)
			p.page = 0
		}
		if ui.Button(c, "上一页").Disabled(p.page == 0).Clicked() {
			p.page--
		}
		ui.Text(c, fmt.Sprintf("%d / %d", p.page+1, max(1, (total+p.size-1)/p.size))).FontSize(11)
		if ui.Button(c, "下一页").Disabled(end >= total).Clicked() {
			p.page++
		}
	})
}

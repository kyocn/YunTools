package app

import (
	"github.com/egoist/mygo/ui"
	"strings"
	"time"
	"yuntools/internal/cloud"
	"yuntools/internal/icons"
	"yuntools/internal/theme"
)

var beijingTime = time.FixedZone("北京时间", 8*60*60)

func objectModified(o cloud.Object) string {
	return formatTimestamp(o.Modified)
}

func formatTimestamp(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.In(beijingTime).Format("2006-01-02 15:04")
}
func objectSize(o cloud.Object) string {
	if o.IsDir {
		return "—"
	}
	return formatBytes(o.Size)
}

// Every header and data row uses the same cell widths, including empty actions.
func ossCell(c *ui.Context, width float32) ui.Element {
	cell := ui.Row(c).Gap(8).AlignItems(ui.Center)
	if width == 0 {
		return cell.Grow(1).MinWidth(0)
	}
	return cell.Width(width).Shrink(0)
}
func ossName(c *ui.Context, label, icon string, accent bool) bool {
	p := theme.Colors(c.Theme().Dark)
	ui.Icon(c, icons.Get(icon)).Size(18, 18).Shrink(0).TextColor(p.Accent)
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Grow(1).MinWidth(0).Padding(8, 0)
	b.Children(func() {
		color := p.Text
		if accent || b.Hovered() {
			color = p.Accent
		}
		ui.Text(c, label).FillWidth().SingleLine().FontSize(12).TextColor(color)
	})
	return b.Clicked()
}
func ossTable(c *ui.Context, widths []float32, labels []string, nameInset float32, rows func()) {
	p := theme.Colors(c.Theme().Dark)
	ui.Scroll(c).Grow(1).FillWidth().Background(p.Card).Border(1, p.Line).Radius(10).Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Children(func() {
			ui.Row(c).FillWidth().Height(38).Padding(0, 14).Gap(12).AlignItems(ui.Center).Background(p.Sidebar.Alpha(.55)).Children(func() {
				for i, label := range labels {
					ossCell(c, widths[i]).Children(func() {
						if i == 0 {
							ui.Box(c).Width(nameInset).Shrink(0)
						}
						ui.Text(c, label).FontSize(11).TextColor(p.Muted)
					})
				}
			})
			rows()
		})
	})
}
func ossDataRow(c *ui.Context, key string, fn func()) {
	p := theme.Colors(c.Theme().Dark)
	ui.Box(c).FillWidth().Height(1).Background(p.Line.Alpha(.6))
	row := ui.Row(c.Key(key)).FillWidth().Height(46).Padding(0, 14).Gap(12).AlignItems(ui.Center)
	if row.Hovered() {
		row.Background(p.Field.Alpha(.65))
	}
	row.Children(fn)
}
func (a *App) bucketTable(c *ui.Context) {
	p := theme.Colors(a.dark)
	widths := []float32{0, 110, 85, 152}
	filtered := make([]cloud.Bucket, 0, len(a.buckets))
	for _, b := range cloud.NewestFirst(a.buckets, func(b cloud.Bucket) time.Time { return b.Created }) {
		if matches(a.search, b.Name, b.Region) {
			filtered = append(filtered, b)
		}
	}
	filter := a.accountID + "\x00" + a.search
	start, end := a.pageRange("oss-buckets", filter, len(filtered))
	ossTable(c, widths, []string{"存储空间", "地域", "存储类型", "创建时间（北京时间）"}, 18, func() {
		for _, b := range filtered[start:end] {
			b := b
			ossDataRow(c, b.Name, func() {
				ossCell(c, widths[0]).Children(func() {
					if ossName(c, b.Name, "box-3-line", false) {
						a.bucket = b.Name
						a.prefix = ""
						a.generation++
						a.refreshOSS(false)
					}
				})
				ossCell(c, widths[1]).Children(func() { ui.Text(c, b.Region).FontSize(12).TextColor(p.Muted) })
				ossCell(c, widths[2]).Children(func() { ui.Text(c, b.StorageClass).FontSize(12).TextColor(p.Muted) })
				ossCell(c, widths[3]).Children(func() { ui.Text(c, formatTimestamp(b.Created)).FontSize(12).TextColor(p.Muted) })
			})
		}
	})
	a.pageFooter(c, "oss-buckets", filter, len(filtered))
}
func (a *App) objectTable(c *ui.Context) {
	p := theme.Colors(a.dark)
	widths := []float32{0, 76, 148, 108, 40}
	ossTable(c, widths, []string{"对象名称", "大小", "最后修改（北京时间）", "操作", "删除"}, 48, func() {
		for _, o := range a.objects {
			o := o
			ossDataRow(c, o.Key, func() {
				ossCell(c, widths[0]).Children(func() {
					chosen := a.selected[o.Key]
					ui.Box(c).Width(22).Shrink(0).Children(func() {
						if ui.Checkbox(c.Key("select:"+o.Key), &chosen, "").Label("选择 " + o.Key).Disabled(a.busy > 0).Changed() {
							a.selected[o.Key] = chosen
						}
					})
					name := strings.TrimPrefix(o.Key, a.prefix)
					icon := "file-3-line"
					if o.IsDir {
						icon = "folder-3-line"
					}
					if ossName(c, name, icon, o.IsDir) {
						if o.IsDir {
							a.prefix = o.Key
							a.generation++
							a.refreshOSS(false)
						} else {
							a.inspectObject(o)
						}
					}
				})
				ossCell(c, widths[1]).Children(func() { ui.Text(c, objectSize(o)).FontSize(12).TextColor(p.Muted) })
				ossCell(c, widths[2]).Children(func() { ui.Text(c, objectModified(o)).FontSize(12).TextColor(p.Muted) })
				ossCell(c, widths[3]).Children(func() {
					if !o.IsDir {
						iconButton(c, "复制访问链接 "+o.Key, "global-line", a.busy > 0, func() { a.openObjectLink(o) })
						iconButton(c, "复制 "+o.Key, "file-copy-line", false, func() {
							a.newForm("copy", "复制对象", map[string]string{"source": o.Key, "destination": o.Key + ".copy"})
						})
						iconButton(c, "移动 "+o.Key, "arrow-left-right-line", false, func() {
							a.newForm("move", "移动 / 重命名对象", map[string]string{"source": o.Key, "destination": o.Key})
						})
					}
				})
				ossCell(c, widths[4]).Children(func() {
					iconButton(c, "删除对象 "+o.Key, "delete-bin-line", a.busy > 0, func() { a.requestDeleteKeys([]string{o.Key}) })
				})
			})
		}
	})
}

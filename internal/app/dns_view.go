package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
	"yuntools/internal/theme"
)

// Use the same column constraints for headings and data. Host names receive
// roughly a third of the row, while record values take the remaining space.
func dnsRecordCell(c *ui.Context, column int) ui.Element {
	cell := ui.Row(c).AlignItems(ui.Center)
	switch column {
	case 0:
		return cell.WidthPercent(28).MinWidth(190).MaxWidth(300).Shrink(0)
	case 1:
		return cell.Width(42).Shrink(0)
	case 3:
		return cell.Width(80).Shrink(0)
	case 4:
		return cell.Width(152).Shrink(0)
	default:
		return cell.Grow(1).MinWidth(0)
	}
}

func (a *App) dnsRecordTable(c *ui.Context, records []cloud.Record) {
	p := theme.Colors(a.dark)
	ui.Text(c, "主机记录是域名前缀，@ 表示根域名。悬停可查看完整内容，选中文字可复制。").FontSize(11).TextColor(p.Muted)
	ui.Scroll(c).Grow(1).FillWidth().Background(p.Card).Border(1, p.Line).Radius(10).Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Children(func() {
			ui.Row(c).FillWidth().Height(38).Padding(0, 14).Gap(12).AlignItems(ui.Center).Background(p.Sidebar.Alpha(.55)).Children(func() {
				for i, label := range []string{"主机记录", "类型", "记录值", "缓存时间 (TTL)", "创建 / 更新（北京时间）"} {
					dnsRecordCell(c, i).Children(func() {
						ui.Text(c, label).FillWidth().SingleLine().FontSize(11).TextColor(p.Muted)
					})
				}
			})
			for _, record := range records {
				ui.Box(c).FillWidth().Height(1).Background(p.Line.Alpha(.6))
				ui.Row(c.Key("dns:"+record.ID)).FillWidth().Height(58).Padding(0, 14).Gap(12).AlignItems(ui.Center).Children(func() {
					for i, value := range []string{record.RR, record.Type, record.Value, fmt.Sprintf("%d 秒", record.TTL), ""} {
						dnsRecordCell(c, i).Children(func() {
							if i == 4 {
								ui.Column(c).Gap(3).Children(func() {
									ui.Text(c, formatTimestamp(record.Created)).FontSize(11).TextColor(p.Muted).Tooltip("创建时间（北京时间）").Selectable()
									ui.Text(c, formatTimestamp(record.Updated)).FontSize(11).TextColor(p.Muted).Tooltip("最后更新（北京时间）").Selectable()
								})
								return
							}
							text := ui.Text(c, value).FillWidth().SingleLine().FontSize(12).Selectable().Tooltip(value)
							switch i {
							case 0:
								text.Bold()
							case 1:
								text.TextColor(p.Accent)
							case 3:
								text.TextColor(p.Muted)
							}
						})
					}
				})
			}
		})
	})
}

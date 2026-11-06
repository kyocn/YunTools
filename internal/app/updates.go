package app

import (
	"github.com/egoist/mygo/ui"
	"yuntools/internal/theme"
)

func (a *App) versionLabel() string {
	if a.p.Version == "" {
		return "开发版本"
	}
	return "v" + a.p.Version
}

func (a *App) updateSettings(c *ui.Context) {
	p := theme.Colors(a.dark)
	var status UpdateStatus
	if a.p.UpdateStatus != nil {
		status = a.p.UpdateStatus()
	}
	card(c).FillWidth().Padding(20).Gap(12).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, "YunTools").Bold().FontSize(18)
			ui.Text(c, a.versionLabel()).TextColor(p.Accent).Grow(1)
			if ui.Button(c, "检查更新").Disabled(a.p.CheckUpdates == nil).Clicked() {
				a.p.CheckUpdates()
			}
		})
		if status.Repository != "" {
			ui.Text(c, "更新来源 · GitHub Releases / "+status.Repository).FontSize(12).TextColor(p.Muted)
		}
		if !status.Enabled {
			ui.Text(c, "当前运行方式不支持自动安装更新，请使用正式打包版本。").FontSize(12).TextColor(p.Muted)
		} else {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
				automatic := status.Automatic
				if ui.Checkbox(c, &automatic, "自动检查更新").Disabled(a.p.SetAutomaticUpdates == nil).Changed() {
					a.p.SetAutomaticUpdates(automatic)
				}
				last := "尚未检查"
				if !status.LastCheck.IsZero() {
					last = "上次检查 · " + status.LastCheck.Local().Format("2006-01-02 15:04")
				}
				ui.Text(c, last).FontSize(11).TextColor(p.Muted)
			})
			ui.Text(c, "启动后检查，运行期间每天检查一次。发现新版本后可查看更新说明并安装，下载过程显示进度。").FontSize(12).TextColor(p.Muted)
		}
		if status.AvailableVersion != "" {
			ui.Text(c, "发现新版本 v"+status.AvailableVersion+"，点击“检查更新”查看详情。").FontSize(12).TextColor(p.Accent)
		}
	})
}

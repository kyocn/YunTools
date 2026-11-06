package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
	"yuntools/internal/tasks"
	"yuntools/internal/theme"
)

func (a *App) taskCenter(c *ui.Context) {
	if !a.showTasks {
		return
	}
	p := theme.Colors(a.dark)
	open := true
	panel := ui.Modal(c.Key("task-center-dialog"), &open, func() { a.taskCenterContent(c) })
	panel.Radius(14).Background(p.Card).Border(1, p.Line).Padding(24)
	if !open {
		a.showTasks = false
	}
}

func (a *App) taskCenterContent(c *ui.Context) {
	p := theme.Colors(a.dark)
	var jobs []tasks.Job
	finished := 0
	if a.jobs != nil {
		jobs = cloud.NewestFirst(a.jobs.Snapshot(), func(j tasks.Job) time.Time { return j.Created })
		for _, job := range jobs {
			if job.State == tasks.Done || job.State == tasks.Failed {
				finished++
			}
		}
	}
	w, h := c.Size()
	ui.Column(c).Width(min(1080, w-96)).Height(min(660, h-112)).Gap(14).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "任务中心").FontSize(19).Bold().Grow(1)
			if ui.Button(c, "清空已结束任务").FontSize(11).Disabled(finished == 0).Clicked() && finished > 0 {
				a.requestClearFinishedTasks()
			}
			iconButton(c, "关闭任务中心", "close-line", false, func() { a.showTasks = false })
		})
		ui.Text(c, "查看所有账号的任务进度。关闭弹窗不会停止任务，任务仍使用创建时的账号。").FontSize(11).TextColor(p.Muted)
		if a.err != "" {
			ui.Text(c, a.err).FillWidth().MaxLines(3).Tooltip(a.err).FontSize(11).TextColor(p.Red)
		}
		if a.jobs == nil {
			return
		}
		if len(jobs) == 0 {
			ui.Text(c, "暂无任务。上传、下载与证书签发进度将在这里显示。").FontSize(12).TextColor(p.Muted)
			return
		}
		start, end := a.pageRange("tasks", "", len(jobs))
		ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				for _, j := range jobs[start:end] {
					j := j
					ui.Row(c.Key(j.ID)).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Column(c).Width(280).Shrink(0).Gap(4).Children(func() {
							ui.Text(c, j.Title).FontSize(12).SingleLine().Tooltip(j.Title)
							name := j.AccountID
							for _, p := range a.profiles {
								if p.ID == j.AccountID {
									name = p.Name
								}
							}
							ui.Text(c, name+" · "+stateLabel(j.State)).FontSize(10).TextColor(p.Muted)
							ui.Text(c, "创建 "+formatTimestamp(j.Created)+" · 北京时间").FontSize(10).TextColor(p.Muted)
							ui.Text(c, "更新 "+formatTimestamp(j.Updated)+" · 北京时间").FontSize(10).TextColor(p.Muted)
						})
						ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
							ratio := 0.0
							if j.Total > 0 {
								ratio = float64(j.Completed) / float64(j.Total)
							}
							ui.Progress(c, ratio).FillWidth().Height(4)
							line := j.Stage
							if j.Total > 0 {
								line = fmt.Sprintf("%s / %s · %s/s", formatBytes(j.Completed), formatBytes(j.Total), formatBytes(int64(j.Speed)))
								if j.Speed > 0 {
									line += fmt.Sprintf(" · 剩余 %s", (time.Duration(float64(j.Total-j.Completed)/j.Speed) * time.Second).Round(time.Second))
								}
							}
							if j.Error != "" {
								line = a.safeError(fmt.Errorf("%s", j.Error))
							}
							ui.Text(c, line).FontSize(10).TextColor(p.Muted).SingleLine().Tooltip(line)
						})
						switch j.State {
						case tasks.Running, tasks.Queued:
							if ui.Button(c, "暂停").FontSize(11).Clicked() {
								if e := a.jobs.Pause(j.ID); e != nil {
									a.err = a.safeError(e)
								}
							}
						case tasks.Paused, tasks.Failed:
							if ui.Button(c, "继续").FontSize(11).Clicked() {
								if e := a.jobs.Resume(j.ID); e != nil {
									a.err = a.safeError(e)
								}
							}
						default:
							ui.Text(c, stateLabel(j.State)).FontSize(11).TextColor(p.Green)
						}
						if j.State == tasks.Done || j.State == tasks.Failed {
							iconButton(c, "删除任务记录 "+j.Title, "delete-bin-line", false, func() { a.requestRemoveTask(j) })
						}
					})
				}
			})
		})
		a.pageFooter(c, "tasks", "", len(jobs))
	})
}

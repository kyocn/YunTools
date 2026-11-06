package app

import (
	"fmt"
	"yuntools/internal/tasks"
)

func (a *App) requestRemoveTask(job tasks.Job) {
	if a.jobs == nil || (job.State != tasks.Done && job.State != tasks.Failed) {
		return
	}
	a.err, a.notice = "", ""
	a.confirm = &confirmation{
		Title:  "删除任务记录",
		Detail: fmt.Sprintf("确定删除「%s」的任务记录？\n\n只删除本机任务记录，不会删除云端对象、证书或已下载的文件。删除失败任务后，无法再从这条记录重试。删除记录不会自动清理上传残留分片。", job.Title),
		Run: func() {
			if err := a.jobs.Remove(job.ID); err != nil {
				a.err = "删除任务记录失败：" + a.safeError(err)
				return
			}
			a.notice = "已删除任务记录"
		},
	}
}

func (a *App) requestClearFinishedTasks() {
	if a.jobs == nil {
		return
	}
	count := 0
	for _, job := range a.jobs.Snapshot() {
		if job.State == tasks.Done || job.State == tasks.Failed {
			count++
		}
	}
	a.err, a.notice = "", ""
	if count == 0 {
		a.notice = "暂无可清空的已结束任务"
		return
	}
	a.confirm = &confirmation{
		Title:  "清空已结束任务",
		Detail: fmt.Sprintf("当前共有 %d 条已完成或失败的任务记录。确认后将清空所有账号、所有分页中已结束的任务记录；进行中和已暂停的任务会保留。\n\n只清理本机记录，不会删除云端对象、证书或已下载的文件。清空失败任务后，无法再从这些记录重试。删除记录不会自动清理上传残留分片。", count),
		Run: func() {
			count, err := a.jobs.ClearFinished()
			if err != nil {
				a.err = "清空任务记录失败：" + a.safeError(err)
				return
			}
			a.notice = fmt.Sprintf("已清空 %d 条已结束任务记录", count)
		},
	}
}

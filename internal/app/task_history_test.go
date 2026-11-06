package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
	"yuntools/internal/tasks"
)

func newTaskHistoryApp(t *testing.T, store cloud.SecretStore, jobs []tasks.Job) *App {
	t.Helper()
	b, err := json.Marshal(jobs)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put("tasks/v1", b); err != nil {
		t.Fatal(err)
	}
	a := New(Platform{Store: store})
	t.Cleanup(a.Close)
	a.showTasks = true
	return a
}

func TestTaskCenterDeleteCompletedRequiresConfirmationAndPersists(t *testing.T) {
	s := &memStore{}
	a := newTaskHistoryApp(t, s, []tasks.Job{{ID: "done", Title: "已下载文件", State: tasks.Done}, {ID: "paused", Title: "保留断点", State: tasks.Paused}})
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if err := tt.Click("删除任务记录 已下载文件"); err != nil {
		t.Fatal(err)
	}
	if a.confirm == nil || !strings.Contains(a.confirm.Detail, "已下载文件") || len(a.jobs.Snapshot()) != 2 {
		t.Fatal("delete did not wait for confirmation")
	}
	if err := tt.Click("取消操作"); err != nil {
		t.Fatal(err)
	}
	if a.confirm != nil || len(a.jobs.Snapshot()) != 2 {
		t.Fatal("cancel removed task")
	}
	if err := tt.Click("删除任务记录 已下载文件"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	if got := a.jobs.Snapshot(); len(got) != 1 || got[0].ID != "paused" {
		t.Fatalf("deleted wrong task: %#v", got)
	}
	reloaded, err := tasks.New(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Snapshot(); len(got) != 1 || got[0].ID != "paused" {
		t.Fatal("deleted task restored")
	}
	if a.notice == "" || a.err != "" {
		t.Fatal("missing success feedback")
	}
}

func TestTaskCenterClearFinishedCoversAllPagesAndKeepsPaused(t *testing.T) {
	s := &memStore{}
	jobs := []tasks.Job{{ID: "done", Title: "完成的下载", State: tasks.Done, Created: time.Now()}, {ID: "failed", Title: "失败的上传", State: tasks.Failed}, {ID: "paused", Title: "保留断点", State: tasks.Paused}}
	a := newTaskHistoryApp(t, s, jobs)
	a.pages = map[string]*listPager{"tasks": {size: 1}}
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	if err := tt.Click("清空已结束任务"); err != nil {
		t.Fatal(err)
	}
	if a.confirm == nil || !strings.Contains(a.confirm.Detail, "2") || !strings.Contains(a.confirm.Detail, "重试") || len(a.jobs.Snapshot()) != 3 {
		t.Fatal("clear scope or confirmation missing")
	}
	if err := tt.Click("取消操作"); err != nil {
		t.Fatal(err)
	}
	if len(a.jobs.Snapshot()) != 3 {
		t.Fatal("cancel cleared jobs")
	}
	if err := tt.Click("清空已结束任务"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("确认执行"); err != nil {
		t.Fatal(err)
	}
	if got := a.jobs.Snapshot(); len(got) != 1 || got[0].ID != "paused" {
		t.Fatalf("clear missed off-page task or removed paused: %#v", got)
	}
	if _, ok := tt.Find("删除任务记录 保留断点"); ok {
		t.Fatal("paused task exposes delete")
	}
	if a.notice == "" || a.err != "" {
		t.Fatal("missing clear feedback")
	}
}

type taskHistoryFailStore struct {
	memStore
	fail bool
}

func (s *taskHistoryFailStore) Put(k string, b []byte) error {
	if s.fail {
		return errors.New("task journal unavailable")
	}
	return s.memStore.Put(k, b)
}

func TestTaskHistoryStorageFailurePreservesRecordsAndShowsError(t *testing.T) {
	for _, clear := range []bool{false, true} {
		t.Run(map[bool]string{false: "remove", true: "clear"}[clear], func(t *testing.T) {
			s := &taskHistoryFailStore{}
			a := newTaskHistoryApp(t, s, []tasks.Job{{ID: "done", Title: "完成的下载", State: tasks.Done}})
			tt := ui.NewTester(a.View, 1280, 820)
			tt.Frame()
			a.notice = "previous success"
			label, confirm := "删除任务记录 完成的下载", "确认删除"
			if clear {
				label, confirm = "清空已结束任务", "确认执行"
			}
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			s.fail = true
			if err := tt.Click(confirm); err != nil {
				t.Fatal(err)
			}
			if len(a.jobs.Snapshot()) != 1 || a.err == "" || a.notice != "" {
				t.Fatal("failed persistence lost task or reported success")
			}
		})
	}
}

func TestTaskHistoryVisualScreens(t *testing.T) {
	dir := os.Getenv("YUNTOOLS_RENDER_DIR")
	if dir == "" {
		t.Skip("visual output not requested")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{960, 1280} {
		s := &memStore{}
		now := time.Date(2026, 11, 6, 8, 15, 0, 0, time.UTC)
		a := newTaskHistoryApp(t, s, []tasks.Job{
			{ID: "done", AccountID: "fixture", Title: "下载 report.csv", State: tasks.Done, Created: now, Updated: now, Completed: 4096, Total: 4096},
			{ID: "failed", AccountID: "fixture", Title: "上传 archive.zip", State: tasks.Failed, Created: now.Add(-time.Minute), Updated: now, Error: "网络连接中断，可重试"},
			{ID: "paused", AccountID: "fixture", Title: "上传 backup.zip", State: tasks.Paused, Created: now.Add(-2 * time.Minute), Updated: now, Completed: 128, Total: 1024},
			{ID: "running", AccountID: "fixture", Title: "下载 video.mp4", State: tasks.Paused, Created: now.Add(-3 * time.Minute), Updated: now},
		})
		a.profiles = []cloud.Account{{ID: "fixture", Name: "示例账号（视觉测试）", Provider: "aliyun", Region: "cn-hangzhou"}}
		a.accountID = "fixture"
		if err := a.jobs.Close(); err != nil {
			t.Fatal(err)
		}
		started := make(chan struct{})
		m, err := tasks.New(s, func(ctx context.Context, _ tasks.Job, emit func(cloud.Progress)) error {
			emit(cloud.Progress{Completed: 4096, Total: 8192, Stage: "下载中"})
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		a.jobs = m
		if err := m.Resume("running"); err != nil {
			t.Fatal(err)
		}
		<-started
		a.pages = map[string]*listPager{"tasks": {size: 2}}
		tt := ui.NewTester(a.View, width, 820)
		for page := 0; page < 2; page++ {
			a.pages["tasks"].page = page
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("tasks-%d-page%d.png", width, page+1)))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			_ = f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		a.requestClearFinishedTasks()
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("tasks-%d-confirm.png", width)))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

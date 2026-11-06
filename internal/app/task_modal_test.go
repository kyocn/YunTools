package app

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
	"yuntools/internal/tasks"
)

func TestTaskCenterModalKeepsPageSizeAndShowsMoreRows(t *testing.T) {
	for _, height := range []int{640, 820} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			var jobs []tasks.Job
			for i := 0; i < 5; i++ {
				jobs = append(jobs, tasks.Job{ID: fmt.Sprint(i), Title: fmt.Sprintf("下载文件-%d.txt", i), State: tasks.Done, Created: time.Date(2026, 11, 6, 1, i, 0, 0, time.UTC)})
			}
			a := newTaskHistoryApp(t, &memStore{}, jobs)
			a.showTasks = false
			a.accountID, a.page = "a", "oss"
			a.buckets = []cloud.Bucket{{Name: "example-bucket", Region: "cn-shanghai"}}
			tt := ui.NewTester(a.View, 960, height)
			tt.Frame()
			before, ok := tt.Find("共 1 项 · 第 1–1 项")
			if !ok {
				t.Fatal("missing underlying bucket footer")
			}
			if err := tt.Click("任务中心  0"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			after, ok := tt.Find("共 1 项 · 第 1–1 项")
			if !ok || math.Abs(float64(before.Y-after.Y)) > 1 {
				t.Fatalf("task center reduced page height: %v -> %v", before, after)
			}
			title, _ := tt.Find("任务中心")
			footer, _ := tt.Find("共 5 项 · 第 1–5 项")
			if footer.Y-title.Y < 400 {
				t.Fatalf("task dialog is still too short: %v -> %v", title, footer)
			}
			if footer.Y+footer.H > float32(height) {
				t.Fatal("task footer escapes window")
			}
			if height == 820 {
				for i := 0; i < 5; i++ {
					row, ok := tt.Find(fmt.Sprintf("下载文件-%d.txt", i))
					if !ok || row.Y < title.Y || row.Y+row.H > footer.Y {
						t.Fatal("five tasks do not fit in large dialog", i, row)
					}
				}
			}
			if err := tt.Click("关闭任务中心"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.showTasks {
				t.Fatal("close did not dismiss dialog")
			}
			closed, _ := tt.Find("共 1 项 · 第 1–1 项")
			if closed.Y != before.Y {
				t.Fatal("close changed page size")
			}
			if err := tt.Click("任务中心  0"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			if a.showTasks {
				t.Fatal("Escape did not close task dialog")
			}
			if len(a.jobs.Snapshot()) != 5 {
				t.Fatal("closing dialog changed tasks")
			}
		})
	}
}

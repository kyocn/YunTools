package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"yuntools/internal/cloud"
)

func historyManager(t *testing.T, store cloud.SecretStore, jobs []Job, runner Runner) *Manager {
	t.Helper()
	b, err := json.Marshal(jobs)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put("tasks/v1", b); err != nil {
		t.Fatal(err)
	}
	m, err := New(store, runner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func TestRemovePreservesUnfinishedTasks(t *testing.T) {
	for _, state := range []State{Paused, Queued, Running, Pausing, State("future-state")} {
		t.Run(string(state), func(t *testing.T) {
			m := historyManager(t, &memory{}, []Job{{ID: "keep", State: state}}, nil)
			if err := m.Remove("keep"); err == nil {
				t.Fatal("unfinished task was removed")
			}
			if len(m.Snapshot()) != 1 {
				t.Fatal("unfinished task disappeared")
			}
		})
	}
}

func TestRemoveFinishedPersistsAndDoesNotRemoveOtherJobs(t *testing.T) {
	for _, state := range []State{Done, Failed} {
		t.Run(string(state), func(t *testing.T) {
			s := &memory{}
			m := historyManager(t, s, []Job{{ID: "delete", State: state}, {ID: "keep", State: Paused}}, nil)
			if err := m.Remove("delete"); err != nil {
				t.Fatal(err)
			}
			reloaded, err := New(s, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := reloaded.Snapshot(); len(got) != 1 || got[0].ID != "keep" {
				t.Fatalf("deleted task restored: %#v", got)
			}
			if err := m.Remove("missing"); !errors.Is(err, cloud.ErrNotFound) {
				t.Fatalf("missing task error = %v", err)
			}
		})
	}
}

type failingHistoryStore struct {
	memory
	fail bool
}

func (s *failingHistoryStore) Put(k string, b []byte) error {
	if s.fail {
		return errors.New("disk unavailable")
	}
	return s.memory.Put(k, b)
}

func TestRemoveRollsBackWhenPersistenceFails(t *testing.T) {
	s := &failingHistoryStore{}
	m := historyManager(t, s, []Job{{ID: "first", State: Done}, {ID: "second", State: Failed}, {ID: "keep", State: Paused}}, nil)
	before := m.Snapshot()
	s.fail = true
	if err := m.Remove("first"); err == nil {
		t.Fatal("storage failure swallowed")
	}
	if !reflect.DeepEqual(m.Snapshot(), before) {
		t.Fatal("failed removal changed memory")
	}
	reloaded, err := New(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.Snapshot(), before) {
		t.Fatal("failed removal changed journal")
	}
}

func TestClearFinishedPersistsAndPreservesPaused(t *testing.T) {
	s := &memory{}
	m := historyManager(t, s, []Job{{ID: "done", State: Done}, {ID: "failed", State: Failed}, {ID: "paused", State: Paused}}, nil)
	n, err := m.ClearFinished()
	if err != nil || n != 2 {
		t.Fatalf("cleared %d: %v", n, err)
	}
	reloaded, err := New(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Snapshot(); len(got) != 1 || got[0].ID != "paused" {
		t.Fatalf("wrong persisted jobs: %#v", got)
	}
	if n, err = m.ClearFinished(); err != nil || n != 0 {
		t.Fatalf("empty clear = %d, %v", n, err)
	}
}

func TestClearFinishedRollsBackWhenPersistenceFails(t *testing.T) {
	s := &failingHistoryStore{}
	m := historyManager(t, s, []Job{{ID: "done", State: Done}, {ID: "paused", State: Paused}, {ID: "failed", State: Failed}}, nil)
	before := m.Snapshot()
	s.fail = true
	if n, err := m.ClearFinished(); err == nil || n != 0 {
		t.Fatalf("failed clear = %d, %v", n, err)
	}
	if !reflect.DeepEqual(m.Snapshot(), before) {
		t.Fatal("failed clear changed memory")
	}
	reloaded, err := New(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.Snapshot(), before) {
		t.Fatal("failed clear changed journal")
	}
}

func TestClearFinishedConcurrentWithRunningProgressAndResume(t *testing.T) {
	started := make(chan struct{}, 1)
	m := historyManager(t, &memory{}, []Job{{ID: "retry", State: Failed}, {ID: "done", State: Done}}, func(ctx context.Context, _ Job, emit func(cloud.Progress)) error {
		started <- struct{}{}
		for i := 0; i < 100; i++ {
			emit(cloud.Progress{Completed: int64(i), Total: 100})
		}
		<-ctx.Done()
		return ctx.Err()
	})
	if err := m.Resume("retry"); err != nil {
		t.Fatal(err)
	}
	<-started
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.ClearFinished(); err != nil {
				t.Error(err)
			}
			_ = m.Snapshot()
		}()
	}
	wg.Wait()
	if err := m.Remove("retry"); err == nil {
		t.Fatal("running task removed")
	}
	if got := m.Snapshot(); len(got) != 1 || got[0].ID != "retry" || got[0].State != Running {
		t.Fatalf("running task lost: %#v", got)
	}
}

func TestClearFinishedRacingResumeKeepsAnyResumedTask(t *testing.T) {
	for i := 0; i < 40; i++ {
		m := historyManager(t, &memory{}, []Job{{ID: "retry", State: Failed}}, func(ctx context.Context, _ Job, _ func(cloud.Progress)) error {
			<-ctx.Done()
			return ctx.Err()
		})
		start := make(chan struct{})
		resumed := make(chan error, 1)
		cleared := make(chan error, 1)
		go func() { <-start; resumed <- m.Resume("retry") }()
		go func() { <-start; _, err := m.ClearFinished(); cleared <- err }()
		close(start)
		resumeErr, clearErr := <-resumed, <-cleared
		if clearErr != nil {
			t.Fatal(clearErr)
		}
		got := m.Snapshot()
		if resumeErr == nil {
			if len(got) != 1 || got[0].ID != "retry" || (got[0].State != Running && got[0].State != Queued) {
				t.Fatalf("resumed task was cleared: %#v", got)
			}
		} else if !errors.Is(resumeErr, cloud.ErrNotFound) || len(got) != 0 {
			t.Fatalf("unexpected race outcome: %v, %#v", resumeErr, got)
		}
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

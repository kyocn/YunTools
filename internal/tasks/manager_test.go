package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
	"yuntools/internal/cloud"
)

type memory struct {
	sync.Mutex
	data map[string][]byte
}

func (m *memory) Get(k string) ([]byte, error) {
	m.Lock()
	defer m.Unlock()
	b, ok := m.data[k]
	if !ok {
		return nil, cloud.ErrNotFound
	}
	return append([]byte(nil), b...), nil
}
func (m *memory) Put(k string, b []byte) error {
	m.Lock()
	defer m.Unlock()
	if m.data == nil {
		m.data = map[string][]byte{}
	}
	m.data[k] = append([]byte(nil), b...)
	return nil
}
func (m *memory) Delete(k string) error { m.Lock(); defer m.Unlock(); delete(m.data, k); return nil }
func await(t *testing.T, fn func() bool) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for !fn() {
		if time.Now().After(until) {
			t.Fatal("condition timeout")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestPauseResumeAndAccountBinding(t *testing.T) {
	s := &memory{}
	started := make(chan Job, 2)
	m, err := New(s, func(ctx context.Context, j Job, emit func(cloud.Progress)) error {
		started <- j
		emit(cloud.Progress{Completed: 42, Total: 100})
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	j, err := m.Submit("account-a", "upload", "sample", json.RawMessage(`{"key":"a"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := <-started
	if got.AccountID != "account-a" {
		t.Fatal(got)
	}
	if err = m.Pause(j.ID); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool { return m.Snapshot()[0].State == Paused })
	if err = m.Resume(j.ID); err != nil {
		t.Fatal(err)
	}
	got = <-started
	if got.AccountID != "account-a" {
		t.Fatal("account changed")
	}
}
func TestRestartRecoversAsPaused(t *testing.T) {
	s := &memory{}
	started := make(chan struct{})
	m, _ := New(s, func(ctx context.Context, j Job, e func(cloud.Progress)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	_, err := m.Submit("a", "download", "file", nil)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	n, err := New(s, func(context.Context, Job, func(cloud.Progress)) error { t.Error("must not auto resume"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	jobs := n.Snapshot()
	if len(jobs) != 1 || jobs[0].State != Paused {
		t.Fatalf("recovered %#v", jobs)
	}
}
func TestProgressAndFailure(t *testing.T) {
	m, _ := New(&memory{}, func(ctx context.Context, j Job, e func(cloud.Progress)) error {
		e(cloud.Progress{Completed: 120, Total: 100})
		return errors.New("network unavailable")
	})
	defer m.Close()
	m.Submit("a", "upload", "test", nil)
	await(t, func() bool { return m.Snapshot()[0].State == Failed })
	j := m.Snapshot()[0]
	if j.Completed > j.Total || j.Error == "" {
		t.Fatal(j)
	}
}
func TestCorruptJournalFailsClosed(t *testing.T) {
	s := &memory{}
	s.Put("tasks/v1", []byte("broken"))
	if _, err := New(s, nil); err == nil {
		t.Fatal("corrupt tasks silently discarded")
	}
}

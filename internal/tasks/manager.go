// Package tasks keeps account-bound, restartable work in the encrypted store.
package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"yuntools/internal/cloud"
)

type State string

const (
	Queued  State = "queued"
	Running State = "running"
	Pausing State = "pausing"
	Paused  State = "paused"
	Done    State = "done"
	Failed  State = "failed"
)

type Job struct {
	ID, AccountID, Kind, Title string
	Payload                    json.RawMessage
	State                      State
	Completed, Total           int64
	Stage, Error               string
	Created, Updated           time.Time
	Speed                      float64 `json:"-"`
}
type Runner func(context.Context, Job, func(cloud.Progress)) error
type Manager struct {
	mu         sync.Mutex
	store      cloud.SecretStore
	run        Runner
	jobs       []Job
	cancels    map[string]context.CancelFunc
	wg         sync.WaitGroup
	closed     bool
	slots      chan struct{}
	persistErr error
}

func New(store cloud.SecretStore, run Runner) (*Manager, error) {
	m := &Manager{store: store, run: run, cancels: map[string]context.CancelFunc{}, slots: make(chan struct{}, 3)}
	b, e := store.Get("tasks/v1")
	if e != nil && !errors.Is(e, cloud.ErrNotFound) {
		return nil, e
	}
	if e == nil {
		if e = json.Unmarshal(b, &m.jobs); e != nil {
			return nil, fmt.Errorf("任务记录损坏: %w", e)
		}
	}
	for i := range m.jobs {
		if m.jobs[i].State == Running || m.jobs[i].State == Queued || m.jobs[i].State == Pausing {
			m.jobs[i].State = Paused
		}
	}
	return m, nil
}
func (m *Manager) save() error {
	b, e := json.Marshal(m.jobs)
	if e == nil {
		e = m.store.Put("tasks/v1", b)
	}
	m.persistErr = e
	return e
}
func (m *Manager) Submit(account, kind, title string, payload json.RawMessage) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Job{}, errors.New("任务中心已关闭")
	}
	if account == "" {
		return Job{}, errors.New("请选择账号")
	}
	id := make([]byte, 12)
	if _, e := rand.Read(id); e != nil {
		return Job{}, e
	}
	j := Job{ID: hex.EncodeToString(id), AccountID: account, Kind: kind, Title: title, Payload: append(json.RawMessage(nil), payload...), State: Queued, Created: time.Now(), Updated: time.Now()}
	m.jobs = append(m.jobs, j)
	if e := m.save(); e != nil {
		m.jobs = m.jobs[:len(m.jobs)-1]
		return Job{}, e
	}
	m.start(j)
	j.Payload = append(json.RawMessage(nil), j.Payload...)
	return j, nil
}
func (m *Manager) find(id string) *Job {
	for i := range m.jobs {
		if m.jobs[i].ID == id {
			return &m.jobs[i]
		}
	}
	return nil
}
func (m *Manager) start(j Job) {
	j.Payload = append(json.RawMessage(nil), j.Payload...)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancels[j.ID] = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		select {
		case m.slots <- struct{}{}:
			defer func() { <-m.slots }()
		case <-ctx.Done():
			m.finish(j.ID, ctx.Err())
			return
		}
		m.mu.Lock()
		x := m.find(j.ID)
		if ctx.Err() != nil {
			m.mu.Unlock()
			m.finish(j.ID, ctx.Err())
			return
		}
		x.State = Running
		x.Error = ""
		x.Speed = 0
		e := m.save()
		m.mu.Unlock()
		if e != nil {
			m.finish(j.ID, e)
			return
		}
		last := time.Now()
		saved := last
		var bytes int64
		emit := func(p cloud.Progress) {
			m.mu.Lock()
			defer m.mu.Unlock()
			x := m.find(j.ID)
			if x == nil {
				return
			}
			now := time.Now()
			x.Completed = max(0, p.Completed)
			x.Total = max(0, p.Total)
			if x.Total > 0 {
				x.Completed = min(x.Completed, x.Total)
			}
			if p.Stage != "" {
				x.Stage = p.Stage
			}
			d := now.Sub(last).Seconds()
			if d >= 0.2 {
				x.Speed = max(0, float64(x.Completed-bytes)/d)
				last = now
				bytes = x.Completed
			}
			x.Updated = now
			if now.Sub(saved) >= time.Second {
				if err := m.save(); err != nil {
					cancel()
				}
				saved = now
			}
		}
		if m.run == nil {
			e = errors.New("任务执行器未初始化")
		} else {
			e = m.run(ctx, j, emit)
		}
		m.finish(j.ID, e)
	}()
}
func (m *Manager) finish(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.find(id)
	if j == nil {
		return
	}
	delete(m.cancels, id)
	j.Updated = time.Now()
	j.Speed = 0
	if err == nil {
		j.State = Done
		j.Completed = j.Total
		j.Stage = "完成"
	} else if j.State == Pausing || errors.Is(err, context.Canceled) {
		j.State = Paused
	} else {
		j.State = Failed
		j.Error = err.Error()
	}
	if e := m.save(); e != nil {
		j.State = Failed
		j.Error = "无法保存任务状态: " + e.Error()
	}
}
func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.find(id)
	if j == nil {
		return cloud.ErrNotFound
	}
	if c := m.cancels[id]; c != nil {
		j.State = Pausing
		c()
		return m.save()
	}
	return nil
}
func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("任务中心已关闭")
	}
	j := m.find(id)
	if j == nil {
		return cloud.ErrNotFound
	}
	if j.State != Paused && j.State != Failed {
		return errors.New("任务尚未暂停或已经完成")
	}
	before := *j
	j.State = Queued
	j.Error = ""
	if e := m.save(); e != nil {
		*j = before
		return e
	}
	m.start(*j)
	return nil
}
func (m *Manager) Snapshot() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]Job(nil), m.jobs...)
	for i := range out {
		out[i].Payload = append(json.RawMessage(nil), out[i].Payload...)
	}
	return out
}
func (m *Manager) HasActive(account string) bool {
	for _, j := range m.Snapshot() {
		if j.AccountID == account && j.State != Done {
			return true
		}
	}
	return false
}
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, j := range m.jobs {
		if j.ID == id {
			if m.cancels[id] != nil || (j.State != Done && j.State != Failed) {
				return errors.New("只能删除已完成或失败的任务记录；进行中和已暂停任务将保留")
			}
			before := append([]Job(nil), m.jobs...)
			m.jobs = append(m.jobs[:i], m.jobs[i+1:]...)
			if e := m.save(); e != nil {
				m.jobs = before
				return e
			}
			return nil
		}
	}
	return cloud.ErrNotFound
}

// ClearFinished removes completed and failed records, retaining resumable work.
// The mutex covers both the state check and journal write so that a concurrent
// Resume cannot lose its running job. Failed writes leave memory unchanged.
func (m *Manager) ClearFinished() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	before := m.jobs
	kept := make([]Job, 0, len(before))
	for _, j := range before {
		if m.cancels[j.ID] != nil || (j.State != Done && j.State != Failed) {
			kept = append(kept, j)
		}
	}
	removed := len(before) - len(kept)
	if removed == 0 {
		return 0, nil
	}
	m.jobs = kept
	if err := m.save(); err != nil {
		m.jobs = before
		return 0, err
	}
	return removed, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	for id, c := range m.cancels {
		m.find(id).State = Pausing
		c()
	}
	m.mu.Unlock()
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.save()
}

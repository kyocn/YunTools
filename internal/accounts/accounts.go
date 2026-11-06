// Package accounts stores complete provider profiles inside the encrypted vault.
package accounts

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"yuntools/internal/cloud"
)

const profilesKey = "accounts/profiles/v1"

type Manager struct {
	mu    sync.Mutex
	store cloud.SecretStore
}

func New(store cloud.SecretStore) *Manager { return &Manager{store: store} }
func (m *Manager) load() ([]cloud.Account, error) {
	data, e := m.store.Get(profilesKey)
	if errors.Is(e, cloud.ErrNotFound) {
		return []cloud.Account{}, nil
	}
	if e != nil {
		return nil, e
	}
	defer clear(data)
	var result []cloud.Account
	if e = json.Unmarshal(data, &result); e != nil {
		return nil, errors.New("invalid encrypted account profiles")
	}
	return result, nil
}
func (m *Manager) List() ([]cloud.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list, e := m.load()
	if e != nil {
		return nil, e
	}
	list = cloud.NewestFirst(list, func(a cloud.Account) time.Time { return a.CreatedAt })
	return list, nil
}
func (m *Manager) Save(a cloud.Account) (cloud.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.Name = strings.TrimSpace(a.Name)
	a.Provider = strings.TrimSpace(a.Provider)
	a.Region = strings.TrimSpace(a.Region)
	a.AccessKeyID = strings.TrimSpace(a.AccessKeyID)
	if a.Provider == "" {
		a.Provider = "aliyun"
	}
	if a.Provider != "aliyun" {
		return cloud.Account{}, errors.New("unsupported provider")
	}
	if a.Name == "" || a.AccessKeyID == "" || strings.TrimSpace(a.AccessKeySecret) == "" {
		return cloud.Account{}, errors.New("name, access key ID and secret are required")
	}
	profiles, e := m.load()
	if e != nil {
		return cloud.Account{}, e
	}
	if a.ID == "" {
		id := make([]byte, 16)
		if _, e = rand.Read(id); e != nil {
			return cloud.Account{}, e
		}
		a.ID = hex.EncodeToString(id)
		a.CreatedAt = time.Now().UTC()
		profiles = append(profiles, a)
	} else {
		found := false
		for i := range profiles {
			if profiles[i].ID == a.ID {
				a.CreatedAt = profiles[i].CreatedAt
				profiles[i] = a
				found = true
				break
			}
		}
		if !found {
			return cloud.Account{}, cloud.ErrNotFound
		}
	}
	data, e := json.Marshal(profiles)
	if e != nil {
		return cloud.Account{}, e
	}
	defer clear(data)
	if e = m.store.Put(profilesKey, data); e != nil {
		return cloud.Account{}, e
	}
	return a, nil
}
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	profiles, e := m.load()
	if e != nil {
		return e
	}
	found := false
	for i := range profiles {
		if profiles[i].ID == id {
			profiles = append(profiles[:i], profiles[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return cloud.ErrNotFound
	}
	data, e := json.Marshal(profiles)
	if e != nil {
		return e
	}
	defer clear(data)
	return m.store.Put(profilesKey, data)
}

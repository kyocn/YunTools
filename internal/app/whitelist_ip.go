package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"

	"yuntools/internal/cloud"
)

func normalizeTargetIP(value, kind string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	ip, err := netip.ParseAddr(value)
	if err != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
		return "", errors.New("请输入单个有效 IP 地址，不要填写端口、网段或多个地址")
	}
	ip = ip.Unmap()
	if ip.IsUnspecified() || ip.IsMulticast() {
		return "", errors.New("请填写有效的单个 IP 地址，不能使用未指定地址或组播地址")
	}
	if (kind == "rds" || kind == "redis") && !ip.Is4() {
		return "", errors.New("RDS / Redis 加白请填写 IPv4 地址")
	}
	return ip.String(), nil
}

func newWhitelistConfigID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func ruleScopeKey(t cloud.Target) string {
	group := strings.TrimSpace(t.Group)
	if t.Kind == "ecs" {
		group = ""
	} else if group == "" {
		group = "default"
	}
	return strings.Join([]string{t.AccountID, t.Kind, t.Region, t.ID, group, strings.ToLower(t.Protocol), t.Ports}, "|")
}
func ruleKey(t cloud.Target) string {
	ip, err := normalizeTargetIP(t.SourceIP, t.Kind)
	if err != nil {
		ip = t.SourceIP
	}
	return ruleScopeKey(t) + "|" + ip
}
func duplicateWhitelistTarget(a, b cloud.Target) bool {
	return ruleKey(a) == ruleKey(b) || (a.Automatic && b.Automatic && ruleScopeKey(a) == ruleScopeKey(b))
}

func (a *App) writeWhitelistTargets(next []cloud.Target) error {
	if a.store == nil || a.locked {
		return errors.New("请先解锁账号保险库")
	}
	data, err := json.Marshal(next)
	if err == nil {
		err = a.store.Put("whitelist/rules", data)
	}
	if err == nil {
		a.rules = next
	}
	return err
}

// Migrate legacy automatic targets before granting any cloud ownership. IDs
// are durable even if the application stops during its first synchronization.
func (a *App) prepareWhitelistTargets(targets []cloud.Target) ([]cloud.Target, error) {
	targets = append([]cloud.Target(nil), targets...)
	next := append([]cloud.Target(nil), a.rules...)
	changed := false
	for i, t := range targets {
		if !t.Automatic || t.ConfigID != "" {
			continue
		}
		found := -1
		for j, current := range next {
			if current == t {
				found = j
				break
			}
		}
		if found < 0 {
			return nil, errors.New("目标配置已变更，请刷新后重试")
		}
		id, err := newWhitelistConfigID()
		if err != nil {
			return nil, err
		}
		next[found].ConfigID = id
		targets[i] = next[found]
		changed = true
	}
	if changed {
		if err := a.writeWhitelistTargets(next); err != nil {
			return nil, err
		}
	}
	return targets, nil
}

func whitelistIPDescription(t cloud.Target) string {
	if t.Automatic {
		if t.SourceIP == "" {
			return "IP：自动跟随公网 IP（等待首次同步）"
		}
		return "IP：" + t.SourceIP + "（自动跟随公网 IP）"
	}
	if t.SourceIP == "" {
		return "IP：使用页面上方的当前 IP"
	}
	return "IP：" + t.SourceIP + "（手动指定）"
}

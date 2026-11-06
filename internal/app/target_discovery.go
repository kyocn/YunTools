package app

import "strings"

type discoveryState struct {
	request uint64
	scope   string
	loading bool
	failed  bool
	message string
}

func resourceKindName(kind string) string {
	switch kind {
	case "rds":
		return "云数据库 RDS"
	case "redis":
		return "缓存 Redis"
	case "ecs":
		return "服务器安全组 ECS"
	default:
		return kind
	}
}

func (a *App) targetDiscoveryScope() string {
	return a.accountID + "\x00" + a.resourceKind + "\x00" + strings.TrimSpace(a.region)
}
func (a *App) resetTargetDiscovery() {
	a.targetDiscovery.request++
	a.targetDiscovery.scope = a.targetDiscoveryScope()
	a.targetDiscovery.loading = false
	a.targetDiscovery.failed = false
	a.targetDiscovery.message = ""
	a.targets = nil
}
func (a *App) targetDiscoveryText() string {
	if a.targetDiscovery.message != "" && a.targetDiscovery.scope == a.targetDiscoveryScope() {
		return a.targetDiscovery.message
	}
	if a.accountID == "" {
		return "请先选择云账号，再加载该账号指定地域的资源。"
	}
	return "查询范围：" + resourceRegionName(strings.TrimSpace(a.region)) + " · " + resourceKindName(a.resourceKind) + "。点击“加载资源”开始查询；已配置目标可来自其他地域。"
}

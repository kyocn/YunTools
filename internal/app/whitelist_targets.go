package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"yuntools/internal/cloud"
)

// A dialog keeps the complete original value so a later edit or removal cannot
// silently overwrite another change, or recreate a target that was removed.
func (a *App) configuredTargetIndex(original cloud.Target) int {
	for i, target := range a.rules {
		if target == original {
			return i
		}
	}
	a.err = "目标配置已变更或已移除，请重新打开后操作"
	return -1
}

func (a *App) editWhitelistTarget(target cloud.Target) {
	if a.configuredTargetIndex(target) < 0 {
		return
	}
	a.newForm("whitelist", "编辑加白目标", map[string]string{
		"name": target.Name, "kind": target.Kind, "id": target.ID,
		"region": target.Region, "group": target.Group,
		"protocol": target.Protocol, "ports": target.Ports,
	})
	a.dialog.Account = cloud.Account{ID: target.AccountID}
	for _, account := range a.profiles {
		if account.ID == target.AccountID {
			a.dialog.Account = account
			break
		}
	}
	a.dialog.OriginalTarget = &target
}

func (a *App) saveWhitelistTarget(f form, fields map[string]string) {
	target := cloud.Target{AccountID: f.Account.ID, Kind: fields["kind"], ID: fields["id"], Region: fields["region"], Group: fields["group"], Protocol: fields["protocol"], Ports: fields["ports"]}
	index := -1
	if f.OriginalTarget != nil {
		index = a.configuredTargetIndex(*f.OriginalTarget)
		if index < 0 {
			return
		}
		// Resource identity and automatic mode are not editable in this form.
		target = a.rules[index]
	}
	if target.ID == "" {
		a.err = "请选择实例"
		return
	}
	target.Name = strings.TrimSpace(fields["name"])
	if target.Kind == "ecs" {
		target.Protocol = strings.TrimSpace(fields["protocol"])
		target.Ports = strings.TrimSpace(fields["ports"])
	} else {
		target.Group = strings.TrimSpace(fields["group"])
	}
	for i, existing := range a.rules {
		if i != index && ruleKey(existing) == ruleKey(target) {
			a.err = "该规则已经存在"
			return
		}
	}
	next := append([]cloud.Target(nil), a.rules...)
	if index < 0 {
		next = append(next, target)
	} else {
		next[index] = target
	}
	if !a.persistWhitelistTargets(next) {
		return
	}
	a.dialog = nil
	a.notice = "已添加目标；可手动加白或开启自动模式"
	if index >= 0 {
		a.notice = "目标配置已保存，仅影响后续加白；云端现有规则未修改"
	}
}

func (a *App) persistWhitelistTargets(next []cloud.Target) bool {
	if a.store == nil || a.locked {
		a.err = "请先解锁账号保险库"
		return false
	}
	b, err := json.Marshal(next)
	if err == nil {
		err = a.store.Put("whitelist/rules", b)
	}
	if err != nil {
		a.err = a.safeError(err)
		return false
	}
	a.rules = next
	a.err = ""
	return true
}

func (a *App) removeWhitelistTarget(target cloud.Target) {
	if a.configuredTargetIndex(target) < 0 {
		return
	}
	a.confirm = &confirmation{
		Title:  "确认移除加白目标",
		Detail: fmt.Sprintf("目标：%s\n资源：%s\n地域：%s\n\n仅移除本机加白目标配置，停止此目标后续的自动加白。已经开始的加白操作仍可能完成。\n不会删除或修改云端已有的白名单或安全组规则。", target.Name, target.ID, target.Region),
		Run: func() {
			index := a.configuredTargetIndex(target)
			if index < 0 {
				return
			}
			next := make([]cloud.Target, 0, len(a.rules)-1)
			next = append(next, a.rules[:index]...)
			next = append(next, a.rules[index+1:]...)
			if a.persistWhitelistTargets(next) {
				a.notice = "目标配置已移除，后续自动加白已停止；云端现有规则未修改"
			}
		},
	}
}

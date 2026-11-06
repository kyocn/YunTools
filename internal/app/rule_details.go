package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
	"yuntools/internal/icons"
	"yuntools/internal/theme"
)

type ruleDetails struct {
	Target                   cloud.Target
	AccountName              string
	Loading                  bool
	Error, Search, Direction string
	Rules                    []cloud.SecurityGroupRule
	IPs                      []string
}

func (a *App) resourceRuleLink(c *ui.Context, t cloud.Target) {
	p := theme.Colors(a.dark)
	b := ui.ButtonBase(c.Key("rules:" + t.ID)).Label("资源规则 " + t.ID).Tooltip("双击查看规则，也可以点击右侧“查看规则”").Grow(1).MinWidth(0).Padding(4).Radius(6)
	if b.Hovered() {
		b.Background(p.Field)
	}
	b.Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			icon := "database-2-line"
			if t.Kind == "ecs" {
				icon = "shield-check-line"
			}
			ui.Icon(c, icons.Get(icon)).Size(18, 18).TextColor(p.Accent)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, t.Name).FontSize(12).SingleLine()
				ui.Text(c, t.ID).FontSize(10).TextColor(p.Muted).SingleLine()
			})
		})
	})
	if b.DoubleClicked() {
		a.openRuleDetails(t)
	}
}

func (a *App) openRuleDetails(t cloud.Target) {
	if t.AccountID != a.accountID {
		return
	}
	d := &ruleDetails{Target: t, Direction: "全部方向"}
	for _, p := range a.profiles {
		if p.ID == t.AccountID {
			d.AccountName = p.Name
		}
	}
	a.ruleDetail = d
	a.loadRuleDetails(d)
}

func (a *App) loadRuleDetails(d *ruleDetails) {
	if d.Loading {
		return
	}
	d.Loading, d.Error = true, ""
	a.async(func(ctx context.Context) (func(), error) {
		var rules []cloud.SecurityGroupRule
		var ips []string
		p, err := a.profile(d.Target.AccountID)
		if err == nil {
			var w cloud.Whitelist
			w, err = a.p.Whitelist(p)
			if err == nil {
				if d.Target.Kind == "ecs" {
					if inspector, ok := w.(cloud.SecurityGroupInspector); ok {
						rules, err = inspector.ListSecurityGroupRules(ctx, d.Target)
					} else {
						err = errors.New("此云服务暂不支持安全组详情")
					}
				} else {
					ips, err = w.ListRules(ctx, d.Target)
				}
			}
		}
		return func() {
			// Closing or switching resource while loading must not reopen stale details.
			if a.ruleDetail != d {
				return
			}
			d.Loading = false
			if err != nil {
				d.Error = a.safeError(err)
				return
			}
			d.Rules, d.IPs = rules, ips
		}, nil
	})
}

func directionLabel(s string) string {
	switch strings.ToLower(s) {
	case "ingress":
		return "入站"
	case "egress":
		return "出站"
	default:
		return "未提供"
	}
}
func policyLabel(s string) string {
	switch strings.ToLower(s) {
	case "accept":
		return "允许"
	case "drop":
		return "拒绝"
	default:
		return s
	}
}
func nonEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
func rulePeer(r cloud.SecurityGroupRule) string {
	if strings.EqualFold(r.Direction, "egress") {
		return nonEmpty(r.Destination)
	}
	return nonEmpty(r.Source)
}

func (a *App) ruleDetailsView(c *ui.Context) {
	d := a.ruleDetail
	if d == nil {
		return
	}
	p := theme.Colors(a.dark)
	w, h := c.Size()
	open := true
	panel := ui.Modal(c.Key("rule-details"), &open, func() {
		ui.Column(c).Width(min(1400, w-80)).Height(min(640, h-100)).Gap(12).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
				title := "白名单详情"
				if d.Target.Kind == "ecs" {
					title = "安全组规则"
				}
				ui.Text(c, title).FontSize(19).Bold().Grow(1)
				if ui.Button(c, "刷新规则").Disabled(d.Loading).Clicked() {
					a.loadRuleDetails(d)
				}
				iconButton(c, "关闭规则详情", "close-line", false, func() { a.ruleDetail = nil })
			})
			ui.Text(c, d.Target.Name+" · "+d.Target.Region+" · 账号："+d.AccountName).FontSize(12).Bold()
			ui.Text(c, d.Target.ID).FontSize(11).TextColor(p.Muted).Selectable()
			if d.Target.Kind == "ecs" {
				ui.Text(c, "入站：谁可以访问服务器；出站：服务器可以访问哪里。此处仅查看规则，不会修改云端设置。").FontSize(11).TextColor(p.Muted)
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
				if d.Target.Kind == "ecs" {
					ui.Select(c, &d.Direction, []string{"全部方向", "入站", "出站"}).Width(120)
				}
				ui.TextInput(c, &d.Search).Placeholder("搜索地址、端口或描述…").Grow(1)
				if d.Target.Kind == "ecs" && ui.Button(c, "重置列宽").Tooltip("恢复默认列宽与列顺序").Clicked() {
					a.securityRuleTable = ui.ListState{}
				}
			})
			if d.Loading {
				ui.Text(c, "正在读取云端规则，请稍候…").FontSize(12).TextColor(p.Muted)
			}
			if d.Error != "" {
				ui.Text(c, "读取失败："+d.Error+"\n处理后点击“刷新规则”重试。").FontSize(12).TextColor(p.Red)
			}
			if d.Target.Kind != "ecs" {
				scope := ruleKey(d.Target) + "/" + d.Search
				ips, total := pagedItems(a, "rule-ips", scope, d.IPs, func(ip string) bool { return matches(d.Search, ip) })
				ui.Scroll(c).Grow(1).FillWidth().Children(func() {
					for _, ip := range ips {
						if matches(d.Search, ip) {
							ui.Text(c, ip).FontSize(12).Selectable().Padding(6)
						}
					}
				})
				a.pageFooter(c, "rule-ips", scope, total)
				return
			}
			filtered := make([]cloud.SecurityGroupRule, 0, len(d.Rules))
			for _, r := range cloud.NewestFirst(d.Rules, func(r cloud.SecurityGroupRule) time.Time { return r.Created }) {
				if d.Direction != "全部方向" && directionLabel(r.Direction) != d.Direction {
					continue
				}
				if matches(d.Search, r.ID, r.Protocol, r.Ports, r.Source, r.Destination, r.Description) {
					filtered = append(filtered, r)
				}
			}
			scope := ruleKey(d.Target) + "/" + d.Search + "/" + d.Direction
			start, end := a.pageRange("security-rules", scope, len(filtered))
			a.securityRulesTable(c, filtered[start:end], min(1400, w-80))
			if len(filtered) == 0 && !d.Loading && d.Error == "" {
				ui.Text(c, "没有符合条件的规则。可清空搜索或切换方向。").FontSize(12).TextColor(p.Muted)
			}

			ui.Text(c, fmt.Sprintf("云端共 %d 条规则 · 创建时间倒序 · 北京时间 · 拖动表头分隔线调宽，双击适应内容；超出宽度可横向滚动", len(d.Rules))).FontSize(11).TextColor(p.Muted)
			a.pageFooter(c, "security-rules", scope, len(filtered))
		})
	})
	panel.Radius(14).Background(p.Card).Border(1, p.Line).Padding(20)
	if !open {
		a.ruleDetail = nil
	}
}

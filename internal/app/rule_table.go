package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
	"yuntools/internal/theme"
)

func (a *App) securityRulesTable(c *ui.Context, rules []cloud.SecurityGroupRule, available float32) {
	p := theme.Colors(a.dark)
	columns := []ui.TableColumn{
		{ID: "direction", Title: "方向", Width: 52, MinWidth: 48},
		{ID: "policy", Title: "策略", Width: 52, MinWidth: 48},
		{ID: "protocol", Title: "协议", Width: 60, MinWidth: 54},
		{ID: "ports", Title: "端口", Width: 108, MinWidth: 100},
		{ID: "peer", Title: "来源 / 目标", Width: 180, MinWidth: 120},
		{ID: "priority", Title: "优先级", Width: 68, MinWidth: 64},
		{ID: "created", Title: "创建时间", Width: 158, MinWidth: 150},
		{ID: "description", Title: "描述 / 规则 ID", Width: max(260, available-778), MinWidth: 180},
	}
	ui.Table(c.Key("security-rule-table"), &a.securityRuleTable, columns, len(rules), func(row, col int) {
		r := rules[row]
		ports, protocol := r.Ports, strings.ToUpper(r.Protocol)
		if ports == "-1/-1" {
			ports = "全部"
		}
		if protocol == "ALL" {
			protocol = "全部"
		}
		values := []string{directionLabel(r.Direction), policyLabel(r.Policy), protocol, ports, rulePeer(r), nonEmpty(r.Priority), formatTimestamp(r.Created), nonEmpty(r.Description) + "\n" + nonEmpty(r.ID)}
		color := p.Text
		if col == 1 {
			if values[col] == "允许" {
				color = p.Green
			}
			if values[col] == "拒绝" {
				color = p.Red
			}
		}
		ui.Column(c).FillWidth().MinWidth(0).Gap(3).Tooltip(values[col]).Children(func() {
			for _, line := range strings.Split(values[col], "\n") {
				ui.Text(c, line).FillWidth().SingleLine().FontSize(11).TextColor(color).Selectable()
			}
		})
	}).Grow(1).FillWidth().Background(p.BG).Border(1, p.Line).Radius(8)
}

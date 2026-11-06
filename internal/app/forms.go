package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/cloud"
	"yuntools/internal/theme"
)

func (a *App) editAccount(p cloud.Account) {
	if p.Region == "" {
		p.Region = "cn-hangzhou"
	}
	title := "添加云账号"
	if p.ID != "" {
		title = "编辑云账号"
	}
	a.dialog = &form{Kind: "account", Title: title, Account: p, Fields: map[string]string{"name": p.Name, "region": p.Region, "ak": p.AccessKeyID, "secret": p.AccessKeySecret, "token": p.SecurityToken}}
}
func (a *App) validateAccount(p cloud.Account) {
	a.async(func(ctx context.Context) (func(), error) {
		c, e := a.p.Storage(p)
		if e != nil {
			return nil, e
		}
		b, e := c.ListBuckets(ctx)
		return func() {
			a.notice = fmt.Sprintf("%s：OSS 连接成功，发现 %d 个 Bucket。其他产品权限在访问时验证。", p.Name, len(b))
		}, e
	})
}
func (a *App) removeAccount(p cloud.Account) {
	if a.jobs != nil && a.jobs.HasActive(p.ID) {
		a.err = "该账号还有未完成任务，请先完成或移除这些任务"
		return
	}
	a.confirm = &confirmation{Title: "确认移除账号", Detail: "将从本机保险库移除 " + p.Name + " 的账号凭据。\n不会删除任何云资源。", Run: func() {
		a.async(func(ctx context.Context) (func(), error) {
			e := a.accounts.Delete(p.ID)
			if e != nil {
				return nil, e
			}
			profiles, e := a.accounts.List()
			return func() {
				a.profiles = profiles
				if a.accountID == p.ID {
					a.switchAccount("")
					if len(profiles) > 0 {
						a.switchAccount(profiles[0].ID)
					}
				}
				a.notice = "账号凭据已移除"
			}, e
		})
	}}
}

type fieldSpec struct {
	key, label, placeholder     string
	secret, readOnly, multiline bool
}

func (a *App) dialogView(c *ui.Context) {
	if a.dialog == nil {
		return
	}
	f := a.dialog
	p := theme.Colors(a.dark)
	open := true
	panel := ui.Modal(c.Key("form:"+f.Kind), &open, func() {
		ui.Column(c).Width(520).MaxHeight(620).Gap(18).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, f.Title).FontSize(19).FontWeight(650).Grow(1)
				iconButton(c, "关闭对话框", "close-line", a.busy > 0, func() { a.dialog = nil })
			})
			if f.Kind != "account" {
				ui.Text(c, "账号："+f.Account.Name).FontSize(11).TextColor(p.Muted)
			}
			var specs []fieldSpec
			switch f.Kind {
			case "account":
				specs = []fieldSpec{{key: "name", label: "账号名称", placeholder: "例如：生产环境 / 个人账号"}, {key: "region", label: "默认地域", placeholder: "cn-hangzhou"}, {key: "ak", label: "AccessKey ID", placeholder: "RAM 用户 AccessKey ID", secret: true}, {key: "secret", label: "AccessKey Secret", placeholder: "仅加密保存到本机", secret: true}, {key: "token", label: "STS Token（可选）", placeholder: "临时凭据的 SecurityToken", secret: true}}
			case "bucket":
				specs = []fieldSpec{{key: "name", label: "Bucket 名称", placeholder: "全局唯一的小写名称"}, {key: "region", label: "地域", placeholder: "cn-hangzhou"}}
			case "directory":
				specs = []fieldSpec{{key: "key", label: "文件夹路径", placeholder: "documents/"}}
			case "domain":
				specs = []fieldSpec{{key: "domain", label: "解析域名", placeholder: "example.com"}}
			case "record":
				specs = []fieldSpec{{key: "rr", label: "主机记录", placeholder: "@ / www / api"}, {key: "type", label: "记录类型", placeholder: "A"}, {key: "value", label: "记录值", placeholder: "IP 地址或目标域名"}, {key: "ttl", label: "TTL（秒）", placeholder: "600"}, {key: "priority", label: "MX 优先级", placeholder: "10"}}
			case "copy", "move":
				specs = []fieldSpec{{key: "source", label: "源对象", readOnly: true}, {key: "destination", label: "目标对象路径", placeholder: "folder/file.txt"}}
			case "metadata":
				specs = []fieldSpec{{key: "source", label: "对象路径", readOnly: true}, {key: "size", label: "对象大小", readOnly: true}, {key: "etag", label: "ETag", readOnly: true}, {key: "metadata", label: "自定义元数据（JSON）", placeholder: "{}", multiline: true}}
			case "whitelist":
				specs = []fieldSpec{{key: "name", label: "目标名称"}, {key: "kind", label: "资源类型", readOnly: true}, {key: "id", label: "资源 ID", readOnly: true}, {key: "region", label: "地域", readOnly: true}}
				if f.Fields["kind"] == "ecs" {
					specs = append(specs, fieldSpec{key: "protocol", label: "协议（tcp / udp）"}, fieldSpec{key: "ports", label: "目的端口范围", placeholder: "22/22 或 443/443"})
				} else {
					specs = append(specs, fieldSpec{key: "group", label: "白名单分组", placeholder: "yuntools"})
				}
			case "certificate":
				specs = []fieldSpec{{key: "domains", label: "证书域名", placeholder: "example.com, *.example.com", multiline: true}, {key: "email", label: "ACME 联系邮箱", placeholder: "admin@example.com"}}
			}
			ui.Scroll(c).MaxHeight(440).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(14).PaddingEnd(12).Children(func() {
					for i, spec := range specs {
						spec := spec
						ui.Column(c.Key(spec.key)).FillWidth().Gap(6).Children(func() {
							ui.Text(c, spec.label).FontSize(12).TextColor(p.Muted)
							value := f.Fields[spec.key]
							var input ui.Element
							if spec.key == "type" {
								input = ui.Select(c, &value, []string{"A", "AAAA", "CNAME", "TXT", "MX", "CAA", "NS", "SRV"})
							} else if spec.key == "region" && !spec.readOnly {
								input = regionSelect(c, &value)
							} else if spec.multiline {
								input = ui.TextArea(c, &value).Height(90)
							} else {
								input = ui.TextInput(c, &value)
							}
							input.FillWidth().Label(spec.label).Placeholder(spec.placeholder).ReadOnly(spec.readOnly).Disabled(a.busy > 0)
							if spec.secret {
								input.Password()
							}
							if i == 0 {
								input.AutoFocus()
							}
							if input.Changed() {
								f.Fields[spec.key] = value
							}
						})
					}
					if f.Kind == "certificate" {
						ui.Checkbox(c, &f.Staging, "使用测试环境（证书不受浏览器信任）")
						ui.Checkbox(c, &f.Terms, "我已阅读并同意 Let’s Encrypt 服务条款")
						ui.Text(c, "服务条款：https://letsencrypt.org/repository/\nDNS 验证会添加临时 TXT，完成后仅清理本工具创建的记录。").FontSize(10).TextColor(p.Muted)
					}
					if f.Kind == "whitelist" && f.OriginalTarget != nil {
						ui.Text(c, "仅修改本机配置，供后续加白使用。账号、资源和地域不变；云端已有规则不会被修改或删除。").FontSize(11).TextColor(p.Muted)
					}
				})
			})
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Radius(999).Disabled(a.busy > 0).Clicked() {
					a.dialog = nil
				}
				label := "保存"
				if f.Kind == "certificate" {
					label = "开始签发"
				}
				if f.Kind == "copy" || f.Kind == "move" {
					label = "继续"
				}
				if primary(c, label, a.busy > 0) {
					a.saveForm()
				}
			})
		})
	})
	panel.Radius(16).Background(p.Card).Border(1, p.Line).Padding(24)
	if !open && a.busy == 0 {
		a.dialog = nil
	}
}
func (a *App) confirmView(c *ui.Context) {
	if a.confirm == nil {
		return
	}
	p := theme.Colors(a.dark)
	confirm := a.confirm
	open := true
	panel := ui.Modal(c.Key("confirmation"), &open, func() {
		ui.Column(c).Width(490).MaxHeight(570).Gap(18).Children(func() {
			ui.Text(c, confirm.Title).FontSize(19).FontWeight(650)
			ui.Scroll(c).FillWidth().MaxHeight(390).Children(func() { ui.Text(c, confirm.Detail).FontSize(12).TextColor(p.Muted) })
			ui.Row(c).FillWidth().Gap(10).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消操作").Radius(999).Clicked() {
					a.confirm = nil
				}
				label := "确认执行"
				if strings.Contains(confirm.Title, "删除") || strings.Contains(confirm.Title, "移除") {
					label = "确认删除"
				}
				if primary(c, label, false) {
					a.confirm = nil
					confirm.Run()
				}
			})
		})
	})
	panel.Radius(16).Background(p.Card).Border(1, p.Line).Padding(24)
	if !open {
		a.confirm = nil
	}
}

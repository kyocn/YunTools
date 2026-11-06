package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"yuntools/internal/certificates"
	"yuntools/internal/cloud"
	"yuntools/internal/icons"
	"yuntools/internal/tasks"
	"yuntools/internal/theme"
)

func (a *App) View(c *ui.Context) {
	a.svc = c.Services()
	a.live = true
	a.drain()
	a.autoTick()
	a.renewalTick()
	c.After(250 * time.Millisecond)
	c.SetTheme(theme.Apply(*c.Theme(), a.dark))
	p := theme.Colors(a.dark)
	c.Root().Background(p.BG)
	ui.Column(c).Fill().TextColor(p.Text).Children(func() {
		a.titlebar(c)
		ui.Row(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			a.sidebar(c)
			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
				a.tabbar(c)
				ui.Column(c.Key(a.page)).Grow(1).MinHeight(0).FillWidth().Padding(24, 28).Gap(18).Children(func() {
					if a.err != "" {
						a.banner(c, a.err, true)
					} else if a.notice != "" {
						a.banner(c, a.notice, false)
					}
					if a.locked && a.page != "settings" {
						a.lockScreen(c)
						return
					}
					switch a.page {
					case "oss":
						a.ossPage(c)
					case "dns":
						a.dnsPage(c)
					case "whitelist":
						a.whitelistPage(c)
					case "certificates":
						a.certPage(c)
					case "accounts":
						a.accountsPage(c)
					case "settings":
						ui.Scroll(c).Grow(1).FillWidth().Children(func() { ui.Column(c).FillWidth().Gap(18).Children(func() { a.settingsPage(c) }) })
					default:
						ui.Scroll(c).Grow(1).FillWidth().Children(func() { ui.Column(c).FillWidth().Gap(18).Children(func() { a.overview(c) }) })
					}
				})
				if a.showTasks {
					a.taskCenter(c)
				}
			})
		})
		a.statusbar(c)
	})
	a.dialogView(c)
	a.confirmView(c)
	a.ruleDetailsView(c)
}
func (a *App) titlebar(c *ui.Context) {
	p := theme.Colors(a.dark)
	bar := c.TitleBar()
	height := float32(48)
	if bar.Height > 0 {
		height = bar.Height
	}
	ui.Row(c).Height(height).Shrink(0).FillWidth().Background(p.Sidebar).Border(1, p.Line).Padding(0, max(18, bar.Right+8), 0, max(18, bar.Left+8)).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Icon(c, icons.Get("cloud-line")).Size(20, 20).TextColor(p.Accent)
		ui.Text(c, "YunTools").FontSize(14).FontWeight(650)
		ui.Text(c, "云资源工作空间").FontSize(11).TextColor(p.Muted)
		ui.Box(c).Grow(1).Height(max(24, height-4)).DragWindow()
		if a.busy > 0 {
			ui.Text(c, "正在同步…").FontSize(11).TextColor(p.Accent)
		}
		iconButton(c, "外观切换", map[bool]string{true: "sun-line", false: "moon-line"}[a.dark], false, a.toggleTheme)
		iconButton(c, "打开设置", "settings-3-line", false, func() { a.openPage("settings") })
	})
}
func (a *App) sidebar(c *ui.Context) {
	p := theme.Colors(a.dark)
	ui.Column(c).Width(222).Shrink(0).FillHeight().Background(p.Sidebar).Border(1, p.Line).Padding(16, 12).Gap(8).Children(func() {
		ui.Text(c, "工作账号").FontSize(11).TextColor(p.Muted).Padding(0, 8)
		ui.Column(c).FillWidth().Padding(12).Gap(8).Radius(10).Background(p.Card).Border(1, p.Line).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.Get("cloud-line")).Size(22, 22).TextColor(p.Orange)
				ui.Text(c, "阿里云").Bold().Grow(1)
				ui.Text(c, "ALIYUN").FontSize(9).TextColor(p.Muted)
			})
			if a.locked {
				ui.Text(c, "账号保险库已锁定").FontSize(11).TextColor(p.Muted)
			} else if len(a.profiles) == 0 {
				ui.Text(c, "尚未连接云账号").FontSize(11).TextColor(p.Muted)
			} else {
				labels := make([]string, 0, len(a.profiles))
				current := ""
				for _, profile := range a.profiles {
					label := profile.Name + " · " + profile.ID[:min(6, len(profile.ID))]
					labels = append(labels, label)
					if profile.ID == a.accountID {
						current = label
					}
				}
				if ui.Select(c, &current, labels).FillWidth().Label("当前云账号").Changed() {
					for i, label := range labels {
						if current == label {
							a.switchAccount(a.profiles[i].ID)
							a.refresh()
						}
					}
				}
				ui.Text(c, a.region).FontSize(11).TextColor(p.Muted)
			}
		})
		ui.Box(c).Height(16)
		for _, item := range []struct{ id, label, icon string }{{"overview", "概览", "dashboard-3-line"}, {"oss", "对象存储", "box-3-line"}, {"whitelist", "访问白名单", "shield-check-line"}, {"dns", "域名解析", "global-line"}, {"certificates", "SSL 证书", "key-2-line"}} {
			item := item
			b := ui.ButtonBase(c.Key(item.id)).Label(item.label).FillWidth().Height(42).Radius(8).Padding(0, 12).AlignItems(ui.Center)
			if a.page == item.id {
				b.Background(p.Accent.Alpha(.16))
			} else if b.Hovered() {
				b.Background(p.Field)
			}
			color := p.Muted
			if a.page == item.id {
				color = p.Accent
			}
			b.Children(func() {
				ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icons.Get(item.icon)).Size(18, 18).TextColor(color)
					ui.Text(c, item.label).FontWeight(500).TextColor(map[bool]ui.Color{true: p.Accent, false: p.Text}[a.page == item.id])
				})
			})
			if b.Clicked() {
				a.openPage(item.id)
			}
		}
		ui.Box(c).Grow(1)
		ui.Column(c).Padding(12).Gap(6).Radius(10).Background(p.Accent.Alpha(.07)).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				ui.Icon(c, icons.Get("shield-check-line")).Size(14, 14).TextColor(p.Accent)
				ui.Text(c, "凭据本地加密").FontSize(11).TextColor(p.Accent)
			})
			ui.Text(c, "密钥由系统钥匙串保护").FontSize(10).TextColor(p.Muted)
		})
		if ui.Button(c, "云账号管理").FillWidth().Radius(8).Clicked() {
			a.openPage("accounts")
		}
	})
}
func (a *App) tabbar(c *ui.Context) {
	p := theme.Colors(a.dark)
	var closeAction func()
	ui.ScrollHorizontal(c).Height(38).Shrink(0).FillWidth().Background(p.Sidebar.Alpha(.5)).Border(1, p.Line).Children(func() {
		ui.Row(c).Padding(4, 12).Gap(6).Children(func() {
			if len(a.tabs) == 0 {
				ui.Text(c, "工作空间 / "+pageTitle(a.page)).FontSize(11).TextColor(p.Muted).Padding(6, 8)
			}
			for i, t := range a.tabs {
				i, t := i, t
				label := t.Label
				for _, profile := range a.profiles {
					if profile.ID == t.AccountID {
						label += " · " + profile.Name
					}
				}
				row := ui.Row(c.Key("tab:"+t.AccountID+":"+t.Page)).Height(28).Padding(0, 3).Gap(1).AlignItems(ui.Center).Radius(6).Border(1, p.Line)
				if i == a.activeTab {
					row.Background(p.Card)
				}
				row.ContextMenu(func(m *ui.Menu) {
					if m.Item("关闭当前").Chosen() {
						closeAction = func() { a.closeTab(i) }
					}
					if m.Item("关闭右侧").Disabled(i == len(a.tabs)-1).Chosen() {
						closeAction = func() { a.closeTabs(i+1, len(a.tabs)) }
					}
					if m.Item("全部关闭").Chosen() {
						closeAction = func() { a.closeTabs(0, len(a.tabs)) }
					}
				})
				row.Children(func() {
					b := ui.ButtonBase(c.Key("open")).Label(label).Padding(3, 7).Center()
					b.Children(func() {
						color := p.Text
						if i == a.activeTab {
							color = p.Accent
						}
						ui.Text(c, label).FontSize(11).TextColor(color)
					})
					if b.Clicked() {
						a.generation++
						if t.AccountID != "" {
							a.switchAccount(t.AccountID)
						}
						a.page = t.Page
						a.activeTab = i
						a.savePreferences()
						a.refresh()
					}
					close := ui.ButtonBase(c.Key("close")).Label("关闭标签："+label).Tooltip("关闭标签").Size(22, 22).Radius(4).Center()
					if close.Hovered() {
						close.Background(p.Field)
					}
					close.Children(func() { ui.Icon(c, icons.Get("close-line")).Size(13, 13).TextColor(p.Muted) })
					if close.Clicked() {
						closeAction = func() { a.closeTab(i) }
					}
				})
			}
		})
	})
	// Remove only after rendering the list, so remaining indices stay stable.
	if closeAction != nil {
		closeAction()
	}
}
func (a *App) statusbar(c *ui.Context) {
	p := theme.Colors(a.dark)
	ui.Row(c).Height(30).Shrink(0).FillWidth().Background(p.Sidebar).Border(1, p.Line).Padding(0, 16).AlignItems(ui.Center).Gap(8).Children(func() {
		dot(c, p.Green)
		state := "保险库已解锁"
		if a.locked {
			state = "保险库已锁定"
		}
		ui.Text(c, state).FontSize(10).TextColor(p.Muted)
		ui.Text(c, fmt.Sprintf("· %d 个云账号", len(a.profiles))).FontSize(10).TextColor(p.Muted)
		ui.Box(c).Grow(1)
		count := 0
		if a.jobs != nil {
			for _, j := range a.jobs.Snapshot() {
				if j.State == tasks.Running || j.State == tasks.Queued {
					count++
				}
			}
		}
		if ui.Button(c, fmt.Sprintf("任务中心  %d", count)).FontSize(10).Padding(2, 10).Clicked() {
			a.showTasks = !a.showTasks
		}
		ui.Text(c, a.versionLabel()).FontSize(10).TextColor(p.Muted)
	})
}
func (a *App) banner(c *ui.Context, s string, danger bool) {
	p := theme.Colors(a.dark)
	color := p.Accent
	if danger {
		color = p.Red
	}
	ui.Row(c).FillWidth().Shrink(0).Radius(8).Padding(10, 12).Gap(8).Background(color.Alpha(.09)).Border(1, color.Alpha(.25)).AlignItems(ui.Center).Children(func() {
		ui.Scroll(c).Grow(1).MinWidth(0).MaxHeight(96).Children(func() { ui.Text(c, s).FillWidth().FontSize(12).Selectable() })
		iconButton(c, "关闭提示", "close-line", false, func() { a.err = ""; a.notice = "" })
	})
}
func (a *App) lockScreen(c *ui.Context) {
	p := theme.Colors(a.dark)
	ui.Column(c).Fill().Center().Gap(16).Children(func() {
		ui.Box(c).Size(76, 76).Radius(22).Background(p.Accent.Alpha(.13)).Center().Children(func() { ui.Icon(c, icons.Get("shield-check-line")).Size(34, 34).TextColor(p.Accent) })
		ui.Text(c, "你的云账号，安全保存在本机").FontSize(24).FontWeight(650)
		ui.Text(c, "解锁保险库后，连接账号并开始管理云资源。").TextColor(p.Muted)
		ui.Text(c, "AccessKey 与证书私钥均加密保存，主密钥由系统钥匙串保护。").FontSize(12).TextColor(p.Muted)
		if primary(c, "解锁账号保险库", a.busy > 0) {
			a.async(func(ctx context.Context) (func(), error) {
				if a.p.Unlock == nil {
					return nil, fmt.Errorf("系统钥匙串未配置")
				}
				s, e := a.p.Unlock()
				return func() { a.attach(s) }, e
			})
		}
	})
}
func (a *App) heading(c *ui.Context, title, subtitle string, actions func()) {
	p := theme.Colors(a.dark)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			ui.Text(c, title).FontSize(24).FontWeight(650)
			ui.Text(c, subtitle).FontSize(12).TextColor(p.Muted)
		})
		if actions != nil {
			actions()
		}
	})
}
func (a *App) overview(c *ui.Context) {
	p := theme.Colors(a.dark)
	a.heading(c, "所有云资源，井然有序。", "在一个原生工作空间中，管理存储、访问权限、域名和证书。", func() {
		if primary(c, "添加云账号", false) {
			a.editAccount(cloud.Account{})
		}
	})
	ui.Row(c).FillWidth().Gap(14).Children(func() {
		jobCount := 0
		if a.jobs != nil {
			jobCount = len(a.jobs.Snapshot())
		}
		for _, m := range []struct {
			n           int
			label, icon string
		}{{len(a.profiles), "已连接账号", "cloud-line"}, {jobCount, "传输与后台任务", "box-3-line"}, {len(a.rules), "白名单规则", "shield-check-line"}, {len(a.certList), "当前账号证书", "key-2-line"}} {
			m := m
			card(c).Grow(1).Padding(18).Gap(12).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					ui.Text(c, m.label).FontSize(12).TextColor(p.Muted).Grow(1)
					ui.Icon(c, icons.Get(m.icon)).Size(18, 18).TextColor(p.Accent)
				})
				ui.Text(c, fmt.Sprint(m.n)).FontSize(30).FontWeight(600)
			})
		}
	})
	ui.Text(c, "开始工作").FontSize(15).FontWeight(600).Margin(8, 0, 0, 0)
	ui.Row(c).FillWidth().Gap(14).Children(func() {
		for _, m := range []struct{ id, title, body, icon string }{{"oss", "对象存储", "浏览存储空间、上传文件\n传输进度与断点续传", "box-3-line"}, {"whitelist", "动态访问白名单", "连接 RDS、Redis 和 ECS\n同步当前公网 IP", "shield-check-line"}, {"dns", "域名与 SSL", "新增解析记录\n一键申请 Let’s Encrypt 证书", "global-line"}} {
			m := m
			card(c).Grow(1).Padding(20).Gap(14).Children(func() {
				ui.Icon(c, icons.Get(m.icon)).Size(28, 28).TextColor(p.Accent)
				ui.Text(c, m.title).FontSize(16).FontWeight(600)
				ui.Text(c, m.body).FontSize(12).TextColor(p.Muted)
				if ui.Button(c, "打开 "+m.title).Radius(999).AlignSelf(ui.Start).Clicked() {
					a.openPage(m.id)
				}
			})
		}
	})
	card(c).FillWidth().Padding(18, 20).Gap(8).Children(func() {
		ui.Text(c, "安全边界，始终明确").FontSize(13).Bold()
		ui.Text(c, "OSS 删除前再次确认 · DNS 仅新增与查询 · 证书仅清理本工具创建的验证 TXT").FontSize(12).TextColor(p.Muted)
	})
}
func (a *App) empty(c *ui.Context, icon, title, body string) {
	p := theme.Colors(a.dark)
	ui.Column(c).Grow(1).FillWidth().Center().Gap(12).Children(func() {
		ui.Box(c).Size(62, 62).Radius(18).Background(p.Accent.Alpha(.1)).Center().Children(func() { ui.Icon(c, icons.Get(icon)).Size(28, 28).TextColor(p.Accent) })
		ui.Text(c, title).FontSize(17).FontWeight(600)
		ui.Text(c, body).FontSize(12).TextColor(p.Muted)
	})
}
func (a *App) toolbar(c *ui.Context, fn func()) {
	p := theme.Colors(a.dark)
	ui.Row(c).FillWidth().Padding(10, 12).Gap(8).Radius(10).Background(p.Card).Border(1, p.Line).AlignItems(ui.Center).Children(fn)
}
func (a *App) ossPage(c *ui.Context) {
	p := theme.Colors(a.dark)
	subtitle := "浏览存储空间，管理文件与文件夹。"
	if a.bucket != "" {
		subtitle = "oss://" + a.bucket + "/" + a.prefix
	}
	a.heading(c, "对象存储", subtitle, func() {
		iconButton(c, "刷新存储", "refresh-line", a.busy > 0, func() { a.generation++; a.refreshOSS(false) })
		if a.bucket == "" {
			if primary(c, "新建存储空间", a.accountID == "") {
				a.newForm("bucket", "新建存储空间", map[string]string{"name": "", "region": a.region})
			}
		} else {
			if primary(c, "上传文件", false) {
				a.pickUploads(false)
			}
		}
	})
	a.toolbar(c, func() {
		if a.bucket != "" {
			iconButton(c, "返回上一级", "arrow-left-line", false, func() {
				a.generation++
				if a.prefix == "" {
					a.bucket = ""
					a.objects = nil
				} else {
					a.prefix = parentPrefix(a.prefix)
				}
				a.refreshOSS(false)
			})
			if ui.Button(c, "上传文件夹").Clicked() {
				a.pickUploads(true)
			}
			if ui.Button(c, "新建文件夹").Clicked() {
				a.newForm("directory", "新建文件夹", map[string]string{"key": a.prefix})
			}
			if ui.Button(c, "下载选中").Disabled(len(a.selectedKeys()) == 0).Clicked() {
				a.downloadSelected()
			}
			if ui.Button(c, "删除选中").Disabled(len(a.selectedKeys()) == 0).TextColor(p.Red).Clicked() {
				a.requestDelete()
			}
		}
		ui.Box(c).Grow(1)
		if a.bucket == "" {
			ui.TextInput(c, &a.search).Placeholder("筛选存储空间…").Width(200).Label("筛选存储空间")
		} else {
			input := ui.TextInput(c, &a.search).Placeholder("按名称开头查找…").Width(140).Label("按对象名称前缀查找").Tooltip("输入文件或文件夹名称的开头，按回车或点击查找；清空后查找可显示全部。")
			submit := input.Submitted()
			if ui.Button(c, "查找").Disabled(a.busy > 0).Clicked() || submit {
				a.ossSearchChanged()
			}
		}
	})
	if a.accountID == "" {
		a.empty(c, "cloud-line", "连接一个云账号", "添加账号后，即可查看你的 OSS 存储空间。")
		return
	}
	if a.bucket == "" {
		if len(a.buckets) == 0 {
			a.empty(c, "box-3-line", "这里还没有存储空间", "点击刷新获取 Bucket，或创建一个新的存储空间。")
			return
		}
		a.bucketTable(c)
		return
	}
	if len(a.objects) == 0 {
		a.empty(c, "box-3-line", "没有找到文件", "目录可能为空，或没有名称以搜索内容开头的文件。可清空搜索后点击“查找”。")
		a.ossPaginationFooter(c)
		return
	}
	a.objectTable(c)
	a.ossPaginationFooter(c)
}
func (a *App) dnsPage(c *ui.Context) {
	p := theme.Colors(a.dark)
	a.heading(c, "域名解析", "仅新增与查询。已有解析记录保持不变。", func() {
		iconButton(c, "刷新域名", "refresh-line", a.busy > 0, func() { a.refreshDNS() })
		if a.domain == "" {
			if primary(c, "添加域名", a.accountID == "") {
				a.newForm("domain", "添加解析域名", map[string]string{"domain": ""})
			}
		} else {
			if primary(c, "新增解析", false) {
				a.newForm("record", "新增 DNS 解析", map[string]string{"rr": "@", "type": "A", "value": "", "ttl": "600", "priority": "10"})
			}
		}
	})
	a.toolbar(c, func() {
		if a.domain != "" {
			iconButton(c, "返回域名列表", "arrow-left-line", false, func() { a.domain = ""; a.generation++; a.refreshDNS() })
			ui.Text(c, a.domain).Bold()
			if ui.Button(c, "查询公网解析").Clicked() {
				a.lookupDNS()
			}
		}
		ui.Box(c).Grow(1)
		ui.Text(c, "只增不改").FontSize(11).TextColor(p.Green)
		ui.TextInput(c, &a.search).Placeholder("筛选域名 / 记录…").Width(190)
	})
	if a.queryResult != "" && a.domain != "" {
		ui.Text(c, a.queryResult).FontSize(12).TextColor(p.Accent)
	}
	if a.domain == "" {
		if len(a.domains) == 0 {
			a.empty(c, "global-line", "管理你的域名解析", "刷新已接入阿里云 DNS 的域名，或添加一个解析域名。")
			return
		}
		domains, total := pagedItems(a, "domains", a.accountID+"/"+a.search, cloud.NewestFirst(a.domains, func(d cloud.Domain) time.Time { return d.Created }), func(d cloud.Domain) bool { return matches(a.search, d.Name) })
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				for _, d := range domains {
					d := d
					if !matches(a.search, d.Name) {
						continue
					}
					resourceRow(c, d.Name, func() {
						ui.Icon(c, icons.Get("global-line")).Size(20, 20).TextColor(p.Accent)
						ui.Column(c).Grow(1).Gap(4).Children(func() {
							ui.Text(c, d.Name).Bold()
							ui.Text(c, "添加时间（北京时间） "+formatTimestamp(d.Created)).FontSize(11).TextColor(p.Muted)
						})
						if ui.Button(c, "查看解析").Clicked() {
							a.domain = d.Name
							a.records = nil
							a.generation++
							a.refreshDNS()
						}
						if ui.Button(c, "申请证书").Clicked() {
							a.newForm("certificate", "申请 SSL 证书", map[string]string{"domains": d.Name, "email": ""})
						}
					})
				}
			})
		})
		a.pageFooter(c, "domains", a.accountID+"/"+a.search, total)
		return
	}
	if len(a.records) == 0 {
		a.empty(c, "global-line", "暂无解析记录", "新增 A、AAAA、CNAME、TXT 或 MX 记录。")
		return
	}
	records, total := pagedItems(a, "dns-records", a.accountID+"/"+a.domain+"/"+a.search, cloud.NewestFirst(a.records, func(r cloud.Record) time.Time { return r.Created }), func(r cloud.Record) bool { return matches(a.search, r.RR, r.Type, r.Value) })
	a.dnsRecordTable(c, records)
	a.pageFooter(c, "dns-records", a.accountID+"/"+a.domain+"/"+a.search, total)
}
func (a *App) whitelistPage(c *ui.Context) {
	p := theme.Colors(a.dark)
	a.heading(c, "访问白名单", "RDS、Redis 与 ECS 的访问规则，在这里统一管理。", func() {
		if primary(c, "检测公网 IP", a.busy > 0) {
			a.detectIP()
		}
	})
	a.toolbar(c, func() {
		ui.Text(c, "当前 IP").FontSize(12).TextColor(p.Muted)
		ui.TextInput(c, &a.publicIP).Placeholder("自动检测或手动输入 IP").Width(240)
		ui.Box(c).Grow(1)
		if ui.Button(c, "为本账号全部目标加白").Disabled(a.accountID == "" || a.whitelistManualRunning || a.whitelistAutomaticRunning).Clicked() {
			var targets []cloud.Target
			for _, t := range a.rules {
				if t.AccountID == a.accountID {
					targets = append(targets, t)
				}
			}
			a.addWhitelist(targets, false)
		}
	})
	ui.Text(c, "检测来源：myip.ipip.net · IPv4 直连（不使用 HTTP 代理）；VPN / TUN 仍可能影响出口，可手动填写。").FontSize(11).TextColor(p.Muted)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "发现资源").Bold()
		kindNames := map[string]string{"rds": "云数据库 RDS", "redis": "缓存 Redis", "ecs": "服务器安全组 ECS"}
		kind := kindNames[a.resourceKind]
		if ui.Select(c, &kind, []string{kindNames["rds"], kindNames["redis"], kindNames["ecs"]}).Width(170).Changed() {
			for key, name := range kindNames {
				if name == kind {
					a.resourceKind = key
				}
			}
			a.resetTargetDiscovery()
		}
		regionInput := regionSelect(c, &a.region).Width(260).Label("资源地域").Tooltip("默认使用当前账号保存的地域。请选择资源实际所在地域；不同云产品支持的地域可能不同。")
		if regionInput.Changed() {
			a.resetTargetDiscovery()
		}
		loadLabel := "加载资源"
		if a.targetDiscovery.loading {
			loadLabel = "正在加载…"
		}
		if ui.Button(c, loadLabel).Disabled(a.targetDiscovery.loading || a.accountID == "").Clicked() {
			a.refreshTargets()
		}
	})
	statusColor := p.Muted
	if a.targetDiscovery.failed {
		statusColor = p.Red
	}
	ui.Text(c, a.targetDiscoveryText()).FillWidth().FontSize(12).TextColor(statusColor)
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			if len(a.targets) > 0 {
				ui.Text(c, "双击资源名称或点击“查看规则”，了解谁可以访问；“添加目标”用于配置自动加白。").FontSize(11).TextColor(p.Muted)
				scope := a.accountID + "/" + a.resourceKind + "/" + a.region
				targets, total := pagedItems(a, "whitelist-resources", scope, cloud.NewestFirst(a.targets, func(t cloud.Target) time.Time { return t.Created }), nil)
				card(c).FillWidth().Padding(12).Gap(6).Children(func() {
					for _, t := range targets {
						t := t
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							a.resourceRuleLink(c, t)
							ui.Text(c, "创建时间（北京时间）\n"+formatTimestamp(t.Created)).Width(148).FontSize(10).TextColor(p.Muted)
							if ui.Button(c, "查看规则").FontSize(11).Clicked() {
								a.openRuleDetails(t)
							}
							if ui.Button(c, "添加目标").FontSize(11).Clicked() {
								a.newForm("whitelist", "配置加白目标", map[string]string{"kind": t.Kind, "id": t.ID, "name": t.Name, "region": t.Region, "group": "yuntools", "protocol": "tcp", "ports": "22/22"})
							}
						})
					}
					a.pageFooter(c, "whitelist-resources", scope, total)
				})
			}
			ui.Text(c, "已配置目标").FontSize(15).FontWeight(600)
			var indices []int
			for i, t := range a.rules {
				if t.AccountID == a.accountID {
					indices = append(indices, i)
				}
			}
			start, end := a.pageRange("whitelist-targets", a.accountID, len(indices))
			for _, index := range indices[start:end] {
				i, t := index, a.rules[index]
				card(c).FillWidth().Padding(16).Gap(10).Children(func() {
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, icons.Get("shield-check-line")).Size(20, 20).TextColor(p.Green)
						ui.Text(c, t.Name).Bold().Grow(1)
						ui.Text(c, strings.ToUpper(t.Kind)+" · "+resourceRegionName(t.Region)).FontSize(11).TextColor(p.Muted)
					})
					ui.Text(c, t.ID+"  "+t.Group+"  "+t.Protocol+" "+t.Ports).FontSize(11).TextColor(p.Muted)
					ui.Text(c, whitelistIPDescription(t)).FontSize(12).TextColor(p.Muted).Selectable()
					if t.Automatic && t.Kind != "ecs" {
						ui.Text(c, "自动跟随使用本工具的独立分组，原分组已有规则保留。").FontSize(11).TextColor(p.Muted)
					}
					a.whitelistAutomaticStatus(c, t)
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						on := t.Automatic
						if ui.Checkbox(c, &on, "自动跟随公网 IP").Changed() {
							a.toggleAutomatic(i)
						}
						ui.Box(c).Grow(1)
						if ui.Button(c, "编辑").Clicked() {
							a.editWhitelistTarget(t)
						}
						if ui.Button(c, "移除").Clicked() {
							a.removeWhitelistTarget(t)
						}
						if ui.Button(c, "查看现有规则").Clicked() {
							a.viewRules(t)
						}
						if ui.Button(c, "立即加白").Disabled(a.whitelistManualRunning || a.whitelistAutomaticRunning).Clicked() {
							a.addWhitelist([]cloud.Target{t}, false)
						}
					})
				})
			}
			if len(indices) == 0 {
				ui.Text(c, "选择地域并加载资源，将需要访问的实例添加为目标。").FontSize(12).TextColor(p.Muted)
			} else {
				a.pageFooter(c, "whitelist-targets", a.accountID, len(indices))
			}
		})
	})
}
func (a *App) certPage(c *ui.Context) {
	p := theme.Colors(a.dark)
	a.heading(c, "SSL 证书", "通过 Let’s Encrypt DNS 验证，申请与续期域名证书。", func() {
		iconButton(c, "刷新证书", "refresh-line", false, a.refreshCertificates)
		if primary(c, "申请证书", a.accountID == "") {
			a.newForm("certificate", "申请 SSL 证书", map[string]string{"domains": "", "email": ""})
		}
	})
	a.toolbar(c, func() {
		ui.Icon(c, icons.Get("shield-check-line")).Size(16, 16).TextColor(p.Green)
		ui.Text(c, "私钥加密保存 · 仅清理本工具创建的验证 TXT").FontSize(12).Grow(1).TextColor(p.Muted)
		if ui.Button(c, "重试验证记录清理").Disabled(a.accountID == "").Clicked() {
			a.cleanupCertificates()
		}
	})
	if len(a.certList) == 0 {
		a.empty(c, "key-2-line", "为你的域名签发证书", "支持普通域名和泛域名。申请进度会显示在任务中心。")
		return
	}
	certs := cloud.NewestFirst(a.certList, func(c certificates.Certificate) time.Time { return c.IssuedAt })
	certStart, certEnd := a.pageRange("certificates", a.accountID, len(a.certList))
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			for _, cert := range certs[certStart:certEnd] {
				cert := cert
				card(c).FillWidth().Padding(20).Gap(12).Children(func() {
					ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, icons.Get("key-2-line")).Size(26, 26).TextColor(p.Green)
						ui.Column(c).Grow(1).Gap(5).Children(func() {
							ui.Text(c, strings.Join(cert.Domains, ", ")).Bold()
							ui.Text(c, "签发（北京时间） "+formatTimestamp(cert.IssuedAt)+" · 到期 "+formatTimestamp(cert.ExpiresAt)).FontSize(11).TextColor(p.Muted)
						})
						label := "正式证书"
						if cert.Staging {
							label = "测试证书 · 浏览器不信任"
						}
						ui.Text(c, label).FontSize(11).TextColor(p.Orange)
					})
					ui.Row(c).Gap(8).Children(func() {
						on := a.autoRenew[cert.AccountID+"/"+cert.ID]
						if ui.Checkbox(c, &on, "自动续期").Changed() {
							a.toggleRenew(cert)
						}
						ui.Text(c, cert.Email).FontSize(11).TextColor(p.Muted).Grow(1)
						if ui.Button(c, "导出证书与私钥").Clicked() {
							a.exportCertificate(cert)
						}
						if ui.Button(c, "续期").Clicked() {
							a.renewCertificate(cert)
						}
					})
				})
			}
		})
	})
	a.pageFooter(c, "certificates", a.accountID, len(a.certList))
}
func (a *App) accountsPage(c *ui.Context) {
	p := theme.Colors(a.dark)
	a.heading(c, "云账号", "每个账号独立连接，凭据只在本机解密使用。", func() {
		if primary(c, "添加云账号", false) {
			a.editAccount(cloud.Account{})
		}
	})
	if len(a.profiles) == 0 {
		a.empty(c, "cloud-line", "连接你的第一个账号", "添加阿里云 RAM 用户的 AccessKey，开始管理资源。")
		return
	}
	accountStart, accountEnd := a.pageRange("accounts", "", len(a.profiles))
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			for _, profile := range a.profiles[accountStart:accountEnd] {
				profile := profile
				card(c).FillWidth().Padding(20).Gap(12).Children(func() {
					ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, icons.Get("cloud-line")).Size(28, 28).TextColor(p.Orange)
						ui.Column(c).Grow(1).Gap(4).Children(func() {
							ui.Text(c, profile.Name).FontSize(16).Bold()
							ui.Text(c, "阿里云 · "+profile.Region).FontSize(12).TextColor(p.Muted)
							ui.Text(c, "本机添加（北京时间） "+formatTimestamp(profile.CreatedAt)).FontSize(11).TextColor(p.Muted)
						})
						ui.Text(c, "已加密").FontSize(11).TextColor(p.Green)
					})
					ui.Row(c).Gap(8).Children(func() {
						if ui.Button(c, "编辑账号").Clicked() {
							a.editAccount(profile)
						}
						if ui.Button(c, "验证连接").Clicked() {
							a.validateAccount(profile)
						}
						ui.Box(c).Grow(1)
						if ui.Button(c, "移除账号").TextColor(p.Red).Clicked() {
							a.removeAccount(profile)
						}
					})
				})
			}
		})
	})
	a.pageFooter(c, "accounts", "", len(a.profiles))
}
func (a *App) settingsPage(c *ui.Context) {
	p := theme.Colors(a.dark)
	a.heading(c, "设置", "版本更新、工作空间外观与后台运行。", nil)
	a.updateSettings(c)
	card(c).FillWidth().Padding(20).Gap(18).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "深色外观").Grow(1)
			if ui.Checkbox(c, &a.dark, "启用深色主题").Changed() {
				a.savePreferences()
			}
		})
		ui.Text(c, "后台任务").Bold()
		ui.Text(c, "暂停和关闭应用会保留传输断点。重新打开后，在任务中心继续。\n动态白名单每分钟检查一次，自动续期每小时检查一次；均仅在应用运行且保险库解锁时执行。").FontSize(12).TextColor(p.Muted)
		ui.Text(c, "域名操作边界").Bold()
		ui.Text(c, "普通 DNS 记录只可新增、列出和查询。证书验证记录清理会核对创建归属、记录 ID、名称与内容。").FontSize(12).TextColor(p.Muted)
	})
}
func (a *App) taskCenter(c *ui.Context) {
	p := theme.Colors(a.dark)
	ui.Column(c).Height(220).Shrink(0).FillWidth().Background(p.Card).Border(1, p.Line).Padding(12, 20).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "任务中心").Bold().Grow(1)
			ui.Text(c, "任务固定绑定原账号").FontSize(10).TextColor(p.Muted)
			iconButton(c, "收起任务中心", "close-line", false, func() { a.showTasks = false })
		})
		if a.jobs == nil {
			return
		}
		jobs := cloud.NewestFirst(a.jobs.Snapshot(), func(j tasks.Job) time.Time { return j.Created })
		if len(jobs) == 0 {
			ui.Text(c, "暂无任务。上传、下载与证书签发进度将在这里显示。").FontSize(12).TextColor(p.Muted)
			return
		}
		start, end := a.pageRange("tasks", "", len(jobs))
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				for _, j := range jobs[start:end] {
					j := j
					ui.Row(c.Key(j.ID)).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Column(c).Width(220).Gap(4).Children(func() {
							ui.Text(c, j.Title).FontSize(12).SingleLine()
							name := j.AccountID
							for _, p := range a.profiles {
								if p.ID == j.AccountID {
									name = p.Name
								}
							}
							ui.Text(c, name+" · "+stateLabel(j.State)).FontSize(10).TextColor(p.Muted)
							ui.Text(c, "创建（北京时间） "+formatTimestamp(j.Created)+" · 更新 "+formatTimestamp(j.Updated)).FontSize(10).TextColor(p.Muted)
						})
						ui.Column(c).Grow(1).Gap(4).Children(func() {
							ratio := 0.0
							if j.Total > 0 {
								ratio = float64(j.Completed) / float64(j.Total)
							}
							ui.Progress(c, ratio).FillWidth().Height(4)
							line := j.Stage
							if j.Total > 0 {
								line = fmt.Sprintf("%s / %s · %s/s", formatBytes(j.Completed), formatBytes(j.Total), formatBytes(int64(j.Speed)))
								if j.Speed > 0 {
									line += fmt.Sprintf(" · 剩余 %s", (time.Duration(float64(j.Total-j.Completed)/j.Speed) * time.Second).Round(time.Second))
								}
							}
							if j.Error != "" {
								line = a.safeError(fmt.Errorf("%s", j.Error))
							}
							ui.Text(c, line).FontSize(10).TextColor(p.Muted).SingleLine()
						})
						switch j.State {
						case tasks.Running, tasks.Queued:
							if ui.Button(c, "暂停").FontSize(11).Clicked() {
								if e := a.jobs.Pause(j.ID); e != nil {
									a.err = a.safeError(e)
								}
							}
						case tasks.Paused, tasks.Failed:
							if ui.Button(c, "继续").FontSize(11).Clicked() {
								if e := a.jobs.Resume(j.ID); e != nil {
									a.err = a.safeError(e)
								}
							}
							if ui.Button(c, "移除任务").FontSize(11).Clicked() {
								a.confirm = &confirmation{Title: "移除任务", Detail: "移除任务记录。已上传分片不会自动删除，可在 OSS 控制台清理。", Run: func() {
									if e := a.jobs.Remove(j.ID); e != nil {
										a.err = a.safeError(e)
									}
								}}
							}
						default:
							ui.Text(c, stateLabel(j.State)).FontSize(11).TextColor(p.Green)
						}
					})
				}
			})
		})
		a.pageFooter(c, "tasks", "", len(jobs))
	})
}

func iconButton(c *ui.Context, label, icon string, disabled bool, fn func()) {
	p := theme.Colors(c.Theme().Dark)
	b := ui.ButtonBase(c.Key(label)).Label(label).Tooltip(label).Size(30, 30).Radius(7).Center().Disabled(disabled)
	if b.Hovered() {
		b.Background(p.Field)
	}
	b.Children(func() { ui.Icon(c, icons.Get(icon)).Size(17, 17).TextColor(p.Muted) })
	if b.Clicked() && !disabled {
		fn()
	}
}
func primary(c *ui.Context, label string, disabled bool) bool {
	return ui.PrimaryButton(c, label).Padding(9, 16).Radius(999).Disabled(disabled).Clicked() && !disabled
}
func card(c *ui.Context) ui.Element {
	p := theme.Colors(c.Theme().Dark)
	return ui.Column(c).Radius(12).Background(p.Card).Border(1, p.Line)
}
func dot(c *ui.Context, color ui.Color) { ui.Box(c).Size(5, 5).Radius(3).Background(color) }
func resourceRow(c *ui.Context, key string, fn func()) {
	p := theme.Colors(c.Theme().Dark)
	ui.Row(c.Key(key)).FillWidth().MinHeight(44).Padding(8, 12).Gap(10).AlignItems(ui.Center).Radius(7).Background(p.Card).Children(fn)
}
func tableHeader(c *ui.Context, labels []string) {
	p := theme.Colors(c.Theme().Dark)
	ui.Row(c).FillWidth().Padding(0, 12).Gap(10).Children(func() {
		for i, s := range labels {
			t := ui.Text(c, s).FontSize(10).TextColor(p.Muted)
			if i == 0 {
				t.Grow(1)
			} else {
				t.Width(110)
			}
		}
	})
}
func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	for _, unit := range []string{"KB", "MB", "GB", "TB"} {
		v /= 1024
		if v < 1024 || unit == "TB" {
			return fmt.Sprintf("%.1f %s", v, unit)
		}
	}
	return ""
}
func matches(q string, values ...string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	for _, v := range values {
		if strings.Contains(strings.ToLower(v), q) {
			return true
		}
	}
	return false
}
func stateLabel(s tasks.State) string {
	switch s {
	case tasks.Queued:
		return "等待中"
	case tasks.Running:
		return "进行中"
	case tasks.Pausing:
		return "正在暂停"
	case tasks.Paused:
		return "已暂停"
	case tasks.Done:
		return "已完成"
	case tasks.Failed:
		return "失败"
	}
	return string(s)
}

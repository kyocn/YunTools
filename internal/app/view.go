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
	a.dark = a.resolvedDark(c.Theme().Dark)
	c.SetTheme(theme.Apply(*c.Theme(), a.dark))
	p := theme.Colors(a.dark)
	c.Root().Background(p.BG)
	if a.locked {
		a.unlockScreen(c)
		return
	}
	ui.Column(c).Fill().TextColor(p.Text).Children(func() {
		a.titlebar(c)
		ui.Row(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			a.sidebar(c)
			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
				a.tabbar(c)
				ui.Column(c.Key(a.page)).Grow(1).MinHeight(0).FillWidth().Padding(20, 24).Gap(16).Children(func() {
					if a.err != "" {
						a.banner(c, a.err, true)
					} else if a.notice != "" {
						a.banner(c, a.notice, false)
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
			})
		})
		a.statusbar(c)
	})
	a.dialogView(c)
	a.ruleDetailsView(c)
	a.taskCenter(c)
	a.confirmView(c)
}
func (a *App) titlebar(c *ui.Context) {
	p := theme.Colors(a.dark)
	bar := c.TitleBar()
	height := float32(48)
	if bar.Height > 0 {
		height = bar.Height
	}
	ui.Row(c).Height(height).Shrink(0).FillWidth().Background(p.Sidebar).Border(1, p.Line).Padding(0, max(16, bar.Right+8), 0, max(16, bar.Left+8)).AlignItems(ui.Center).Gap(10).Children(func() {
		navTile(c, "cloud-line", p.Accent, true)
		ui.Text(c, "YunTools").FontSize(15).FontWeight(650)
		ui.Text(c, a.windowContext()).FontSize(11).TextColor(p.Muted)
		ui.Box(c).Grow(1).Height(max(24, height-4)).DragWindow()
		if a.busy > 0 {
			ui.Text(c, "正在处理…").FontSize(11).TextColor(p.Accent)
		}
		a.appearanceButton(c)
		iconButton(c, "打开设置", "settings-3-line", false, func() { a.openPage("settings") })
	})
}
func (a *App) windowContext() string {
	if a.locked {
		return "保险库已锁定"
	}
	if a.accountID == "" {
		return "还没有选择账号"
	}
	for _, profile := range a.profiles {
		if profile.ID == a.accountID {
			name := strings.TrimSpace(profile.Name)
			if name == "" {
				name = "未命名账号"
			}
			return name + " · " + cloud.ProviderDisplayName(profile.Provider)
		}
	}
	return "云资源"
}
func (a *App) sidebar(c *ui.Context) {
	p := theme.Colors(a.dark)
	ui.Column(c).Width(232).Shrink(0).FillHeight().Background(p.Sidebar).Border(1, p.Line).Padding(14, 10).Gap(8).Children(func() {
		ui.Text(c, "当前账号").FontSize(11).TextColor(p.Muted).Padding(0, 8)
		ui.Column(c).FillWidth().Padding(12).Gap(8).Radius(12).Background(p.Card).Border(1, p.Line).Children(func() {
			provider := a.currentProvider()
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				navTile(c, "cloud-line", providerTint(p, provider), true)
				ui.Text(c, a.capabilitySummary()).FontSize(11).TextColor(p.Muted).Grow(1)
			})
			if a.locked {
				ui.Text(c, "先解锁保险库，再选择账号。").FontSize(11).TextColor(p.Muted)
			} else if len(a.profiles) == 0 {
				ui.Text(c, "还没有云账号。添加后即可开始。").FontSize(11).TextColor(p.Muted)
			} else {
				labels := accountMenuLabels(a.profiles)
				current := ""
				for i, profile := range a.profiles {
					if profile.ID == a.accountID {
						current = labels[i]
					}
				}
				if ui.Select(c, &current, labels).FillWidth().Label("当前云账号").Changed() {
					for i, label := range labels {
						if current == label {
							a.selectAccount(a.profiles[i].ID)
						}
					}
				}
				if a.accountID != "" {
					ui.Text(c, "默认地域 "+a.regionLabel(a.region)).FontSize(11).TextColor(p.Muted)
				}
			}
		})
		ui.Box(c).Height(8)
		for _, item := range a.navItems(p) {
			item := item
			available := a.navAvailable(item.id)
			b := ui.ButtonBase(c.Key(item.id)).Label(item.label).FillWidth().Height(40).Radius(8).Padding(0, 8).AlignItems(ui.Center)
			if !available {
				b.Tooltip(cloud.ProviderDisplayName(a.currentProvider()) + "目前只支持对象存储，点进去可以切换账号")
			}
			if a.page == item.id {
				b.Background(item.tint.Alpha(.14))
			} else if b.Hovered() {
				b.Background(p.Field)
			}
			b.Children(func() {
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					navTile(c, item.icon, item.tint, a.page == item.id)
					ui.Text(c, item.label).FontWeight(500).Grow(1).TextColor(map[bool]ui.Color{true: p.Text, false: p.Muted}[available || a.page == item.id])
					if !available {
						ui.Text(c, "暂不可用").FontSize(10).TextColor(p.Muted)
					}
				})
			})
			if b.Clicked() {
				a.openPage(item.id)
			}
		}
		ui.Box(c).Grow(1)
		ui.Box(c).Height(1).FillWidth().Background(p.Line)
		ui.Row(c).Padding(4, 8).Gap(8).AlignItems(ui.Center).Children(func() {
			dot(c, map[bool]ui.Color{true: p.Muted, false: p.Green}[a.locked])
			ui.Text(c, "密钥只保存在这台电脑").FontSize(11).TextColor(p.Muted).Grow(1)
		})
		a.navButton(c, p, navItem{id: "accounts", label: "云账号", icon: "cloud-line", tint: p.Accent})
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
				label := a.tabLabel(t)
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
						if pageUsesAccount(t.Page) && t.AccountID != "" {
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
	ui.Row(c).Height(30).Shrink(0).FillWidth().Background(p.Sidebar).Border(1, p.Line).Padding(0, 14).AlignItems(ui.Center).Gap(8).Children(func() {
		dot(c, map[bool]ui.Color{true: p.Orange, false: p.Green}[a.locked])
		state := "已解锁"
		if a.locked {
			state = "已锁定"
		}
		ui.Text(c, state).FontSize(11).TextColor(p.Muted)
		ui.Text(c, fmt.Sprintf("%d 个账号", len(a.profiles))).FontSize(11).TextColor(p.Muted)
		ui.Box(c).Grow(1)
		count := 0
		if a.jobs != nil {
			for _, j := range a.jobs.Snapshot() {
				if j.State == tasks.Running || j.State == tasks.Queued {
					count++
				}
			}
		}
		label := "打开任务中心"
		if count > 0 {
			label = fmt.Sprintf("任务中心 %d", count)
		}
		if ui.Button(c, label).FontSize(11).Padding(2, 10).Radius(999).Tooltip("查看上传、下载和后台任务").Clicked() {
			a.showTasks = !a.showTasks
		}
		ui.Text(c, a.versionLabel()).FontSize(11).TextColor(p.Muted)
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
func (a *App) appearanceButton(c *ui.Context) {
	iconButton(c, a.appearanceLabel(), map[bool]string{true: "sun-line", false: "moon-line"}[a.dark], false, a.cycleAppearance)
}

func (a *App) unlockScreen(c *ui.Context) {
	p := theme.Colors(a.dark)
	bar := c.TitleBar()
	height := float32(48)
	if bar.Height > 0 {
		height = bar.Height
	}
	ui.Column(c).Fill().Background(p.BG).TextColor(p.Text).Children(func() {
		ui.Row(c).Height(height).Shrink(0).FillWidth().Padding(0, max(16, bar.Right+8), 0, max(16, bar.Left+8)).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Grow(1).Height(height).DragWindow()
			a.appearanceButton(c)
		})
		ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Gap(14).Padding(24).Children(func() {
			ui.Box(c).Size(72, 72).Radius(20).Background(p.Accent).Center().Children(func() {
				ui.Icon(c, icons.Get("shield-check-line")).Size(32, 32).TextColor(ui.Hex("#FFFFFF"))
			})
			ui.Text(c, "已保存的账号尚未解锁").FontSize(22).FontWeight(650)
			ui.Text(c, "密钥仍是加密的。授权系统钥匙串之后，才能查看云账号和管理资源。").MaxWidth(440).FontSize(13).TextColor(p.Muted)
			ui.Text(c, "当前外观："+a.appearanceLabel()+"。授权页和进入后的主界面使用同一种外观。").MaxWidth(440).FontSize(12).TextColor(p.Muted)
			label := "授权并解锁"
			if a.busy > 0 {
				label = "正在等待授权…"
			}
			if primary(c, label, a.busy > 0) {
				a.async(func(ctx context.Context) (func(), error) {
					if a.p.Unlock == nil {
						return nil, fmt.Errorf("系统钥匙串未配置")
					}
					s, e := a.p.Unlock()
					return func() { a.attach(s) }, e
				})
			}
			if a.err != "" {
				ui.Text(c, a.err).MaxWidth(440).FontSize(12).TextColor(p.Red)
			}
		})
	})
}
func (a *App) heading(c *ui.Context, title, subtitle string, actions func()) {
	p := theme.Colors(a.dark)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			ui.Text(c, title).FontSize(22).FontWeight(650)
			ui.Text(c, subtitle).FontSize(12).TextColor(p.Muted)
		})
		if actions != nil {
			actions()
		}
	})
}
func (a *App) overview(c *ui.Context) {
	p := theme.Colors(a.dark)
	a.heading(c, "从这里开始", "管理存储、访问权限、域名和证书。密钥只保存在这台电脑上。", func() {
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
		for _, m := range []struct {
			id, title, body, icon string
			tint                  ui.Color
		}{
			{"oss", "对象存储", "浏览空间、上传和下载文件。\n阿里云、腾讯云、七牛云都可以。", "box-3-line", p.Orange},
			{"whitelist", "访问白名单", "把当前公网 IP 放行到数据库或安全组。\n阿里云和腾讯云可用。", "shield-check-line", p.Green},
			{"dns", "域名与证书", "添加解析记录，或申请证书。\n已有记录不会被修改。", "global-line", ui.Hex("#14A3BE")},
		} {
			m := m
			card(c).Grow(1).Padding(18).Gap(12).Children(func() {
				navTile(c, m.icon, m.tint, true)
				ui.Text(c, m.title).FontSize(16).FontWeight(600)
				ui.Text(c, m.body).FontSize(12).TextColor(p.Muted)
				if ui.Button(c, "进入"+m.title).Radius(999).AlignSelf(ui.Start).Clicked() {
					a.openPage(m.id)
				}
			})
		}
	})
	card(c).FillWidth().Padding(16, 18).Gap(6).Children(func() {
		ui.Text(c, "改之前会再问你一次").FontSize(13).Bold()
		ui.Text(c, "删除文件和移除账号都会先确认。域名记录只增加、不修改。证书清理只动本工具自己创建的验证记录。").FontSize(12).TextColor(p.Muted)
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
	subtitle := "先选一个存储空间，再管理里面的文件。"
	if a.bucket != "" {
		subtitle = "当前位置 " + cloud.ProviderStorageScheme(a.currentProvider()) + "://" + a.bucket + "/" + a.prefix
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
			if ui.Button(c, "上传文件夹").Tooltip("按文件夹里的相对路径上传，同名文件会覆盖").Clicked() {
				a.pickUploads(true)
			}
			if ui.Button(c, "新建文件夹").Tooltip("创建一个空文件夹占位").Clicked() {
				a.newForm("directory", "新建文件夹", map[string]string{"key": a.prefix})
			}
			if ui.Button(c, "下载选中").Disabled(len(a.selectedKeys()) == 0).Tooltip("先勾选文件，再下载到本机").Clicked() {
				a.downloadSelected()
			}
			if ui.Button(c, "删除选中").Disabled(len(a.selectedKeys()) == 0).TextColor(p.Red).Tooltip("删除前会再确认一次").Clicked() {
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
		a.empty(c, "cloud-line", "先连接一个云账号", "添加阿里云、腾讯云或七牛云后，这里会列出存储空间。")
		return
	}
	if a.bucket == "" {
		if len(a.buckets) == 0 {
			a.empty(c, "box-3-line", "还没有存储空间", "点右上角「新建存储空间」，或点刷新图标重新获取。")
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
func (a *App) unsupportedFeature(c *ui.Context, icon, title, feature string) {
	p := theme.Colors(a.dark)
	providerName := cloud.ProviderDisplayName(a.currentProvider())
	ui.Column(c).Fill().Center().Gap(12).Children(func() {
		ui.Icon(c, icons.Get(icon)).Size(42, 42).TextColor(p.Muted)
		ui.Text(c, fmt.Sprintf("当前云厂商（%s）不支持%s", providerName, title)).FontSize(17).FontWeight(650)
		ui.Text(c, fmt.Sprintf("%s目前可以管理对象存储（%s）。%s请换到阿里云或腾讯云账号。", providerName, strings.ToUpper(cloud.ProviderStorageScheme(a.currentProvider())), feature)).FontSize(12).TextColor(p.Muted)
		ui.Row(c).Gap(8).Children(func() {
			if primary(c, "前往对象存储", false) {
				a.openPage("oss")
			}
			if ui.Button(c, "切换云账号").Radius(999).Clicked() {
				a.openPage("accounts")
			}
		})
	})
}

func (a *App) dnsPage(c *ui.Context) {
	p := theme.Colors(a.dark)
	if a.accountID != "" && !cloud.ProviderSupportsDNS(a.currentProvider()) {
		a.heading(c, "域名解析", "可以添加和查看。已经存在的记录不会被修改或删除。", nil)
		a.unsupportedFeature(c, "global-line", "域名解析", "域名解析")
		return
	}
	a.heading(c, "域名解析", "可以添加和查看。已经存在的记录不会被修改或删除。", func() {
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
			a.empty(c, "global-line", "还没有解析域名", "点右上角添加一个域名，或点刷新图标获取当前账号下已有的域名。")
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
	provider := a.currentProvider()
	if a.accountID != "" && !cloud.ProviderSupportsWhitelist(provider) {
		a.heading(c, "访问白名单", "数据库与安全组访问规则，在这里统一管理。", nil)
		a.unsupportedFeature(c, "shield-check-line", "访问白名单", "访问白名单")
		return
	}
	a.heading(c, "访问白名单", a.whitelistSubtitle(), func() {
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
		kindNames := map[string]string{
			"rds":   resourceKindNameForProvider(provider, "rds"),
			"redis": resourceKindNameForProvider(provider, "redis"),
			"ecs":   resourceKindNameForProvider(provider, "ecs"),
		}
		kind := kindNames[a.resourceKind]
		if ui.Select(c, &kind, []string{kindNames["rds"], kindNames["redis"], kindNames["ecs"]}).Width(170).Changed() {
			for key, name := range kindNames {
				if name == kind {
					a.resourceKind = key
				}
			}
			a.resetTargetDiscovery()
		}
		regionInput := regionSelectForProvider(c, provider, &a.region).Width(260).Label("资源地域").Tooltip("默认使用当前账号保存的地域。请选择资源实际所在地域；不同云产品支持的地域可能不同。")
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
						ui.Text(c, resourceKindNameForProvider(provider, t.Kind)+" · "+resourceRegionNameForProvider(provider, t.Region)).FontSize(11).TextColor(p.Muted)
					})
					ui.Text(c, t.ID+"  "+t.Group+"  "+t.Protocol+" "+t.Ports).FontSize(11).TextColor(p.Muted)
					ui.Text(c, whitelistIPDescription(t)).FontSize(12).TextColor(p.Muted).Selectable()
					if t.Automatic {
						ui.Text(c, a.whitelistFollowHint(t)).FontSize(11).TextColor(p.Muted)
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
	if a.accountID != "" && !cloud.ProviderSupportsCertificates(a.currentProvider()) {
		a.heading(c, "SSL 证书", "通过 Let’s Encrypt DNS 验证，申请与续期域名证书。", nil)
		a.unsupportedFeature(c, "key-2-line", "SSL 证书", "SSL 证书管理")
		return
	}
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
		a.empty(c, "cloud-line", "连接你的第一个账号", "支持阿里云、腾讯云、七牛云，添加密钥后即可开始管理资源。")
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
							ui.Text(c, cloud.ProviderDisplayName(profile.Provider)+" · "+resourceRegionNameForProvider(profile.Provider, profile.Region)).FontSize(12).TextColor(p.Muted)
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
	a.heading(c, "设置", "外观、更新，以及后台会在什么时候继续工作。", nil)
	a.updateSettings(c)
	card(c).FillWidth().Padding(20).Gap(18).Children(func() {
		ui.Text(c, "外观").Bold()
		ui.Text(c, "跟随系统时，授权页和主界面保持同一种外观。也可以固定为浅色或深色。").FontSize(12).TextColor(p.Muted)
		appearance := a.appearanceLabel()
		if ui.Select(c, &appearance, []string{"跟随系统", "浅色", "深色"}).Width(180).Label("外观").Changed() {
			switch appearance {
			case "浅色":
				a.setAppearance(appearanceLight)
			case "深色":
				a.setAppearance(appearanceDark)
			default:
				a.setAppearance(appearanceAuto)
			}
		}
		ui.Text(c, "后台任务").Bold()
		ui.Text(c, "关掉窗口不会丢掉上传进度。重新打开后，到任务里继续。\n自动加白大约每分钟看一次公网 IP，证书续期大约每小时看一次。应用没开、或保险库还锁着时，都不会在后台运行。").FontSize(12).TextColor(p.Muted)
		ui.Text(c, "域名操作边界").Bold()
		ui.Text(c, "普通 DNS 记录只可新增、列出和查询。证书验证记录清理会核对创建归属、记录 ID、名称与内容。").FontSize(12).TextColor(p.Muted)
	})
}

type navItem struct {
	id, label, icon string
	tint            ui.Color
}

func (a *App) navItems(p theme.Palette) []navItem {
	return []navItem{
		{id: "overview", label: "概览", icon: "dashboard-3-line", tint: p.Accent},
		{id: "oss", label: "对象存储", icon: "box-3-line", tint: p.Orange},
		{id: "whitelist", label: "访问白名单", icon: "shield-check-line", tint: p.Green},
		{id: "dns", label: "域名解析", icon: "global-line", tint: ui.Hex("#14A3BE")},
		{id: "certificates", label: "SSL 证书", icon: "key-2-line", tint: ui.Hex("#AF52DE")},
	}
}

func (a *App) navAvailable(id string) bool {
	if a.accountID == "" || a.locked {
		return true
	}
	switch id {
	case "whitelist":
		return cloud.ProviderSupportsWhitelist(a.currentProvider())
	case "dns", "certificates":
		return cloud.ProviderSupportsDNS(a.currentProvider())
	default:
		return true
	}
}

func (a *App) navButton(c *ui.Context, p theme.Palette, item navItem) {
	b := ui.ButtonBase(c.Key(item.id)).Label(item.label).FillWidth().Height(40).Radius(8).Padding(0, 8).AlignItems(ui.Center)
	if a.page == item.id {
		b.Background(item.tint.Alpha(.14))
	} else if b.Hovered() {
		b.Background(p.Field)
	}
	b.Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			navTile(c, item.icon, item.tint, a.page == item.id)
			ui.Text(c, item.label).FontWeight(500).TextColor(p.Text)
		})
	})
	if b.Clicked() {
		a.openPage(item.id)
	}
}

func navTile(c *ui.Context, icon string, tint ui.Color, filled bool) {
	bg := tint.Alpha(.16)
	fg := tint
	if filled {
		bg = tint
		fg = ui.Hex("#FFFFFF")
	}
	ui.Box(c).Size(22, 22).Shrink(0).Radius(6).Background(bg).Center().Children(func() {
		ui.Icon(c, icons.Get(icon)).Size(13, 13).TextColor(fg)
	})
}

func providerTint(p theme.Palette, provider string) ui.Color {
	switch cloud.NormalizeProvider(provider) {
	case cloud.ProviderTencent:
		return p.Accent
	case cloud.ProviderQiniu:
		return ui.Hex("#14A3BE")
	default:
		return p.Orange
	}
}

func (a *App) capabilitySummary() string {
	if a.locked || a.accountID == "" {
		return "阿里云、腾讯云、七牛云"
	}
	if cloud.ProviderSupportsDNS(a.currentProvider()) {
		return "存储、白名单、域名、证书"
	}
	return "目前可用对象存储"
}

func (a *App) accountName(id string) string {
	for _, profile := range a.profiles {
		if profile.ID == id {
			name := strings.TrimSpace(profile.Name)
			if name == "" {
				return "未命名"
			}
			return name
		}
	}
	return ""
}

func (a *App) tabLabel(t tab) string {
	label := t.Label
	if label == "" {
		label = pageTitle(t.Page)
	}
	if !pageUsesAccount(t.Page) || t.AccountID == "" || !a.tabsUseMultipleAccounts() {
		return label
	}
	if name := a.accountName(t.AccountID); name != "" {
		return label + " · " + name
	}
	return label
}

func (a *App) tabsUseMultipleAccounts() bool {
	seen := map[string]bool{}
	for _, t := range a.tabs {
		if pageUsesAccount(t.Page) && t.AccountID != "" {
			seen[t.AccountID] = true
		}
	}
	return len(seen) > 1
}

func accountMenuLabels(profiles []cloud.Account) []string {
	base := make([]string, len(profiles))
	count := map[string]int{}
	for i, profile := range profiles {
		base[i] = accountMenuBase(profile)
		count[base[i]]++
	}
	labels := make([]string, len(profiles))
	for i, profile := range profiles {
		labels[i] = base[i]
		if count[base[i]] > 1 {
			id := profile.ID
			if len(id) > 4 {
				id = id[:4]
			}
			labels[i] += " · " + id
		}
	}
	return labels
}

func accountMenuBase(profile cloud.Account) string {
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name = "未命名"
	}
	return name + " · " + cloud.ProviderDisplayName(profile.Provider)
}

func (a *App) whitelistSubtitle() string {
	if a.currentProvider() == cloud.ProviderTencent {
		return "把当前 IP 放进已绑定的安全组。数据库和 Redis 需要先在腾讯云绑定安全组。"
	}
	return "把当前 IP 放行到 RDS、Redis 或安全组。其他已有规则会保留。"
}

func (a *App) whitelistFollowHint(t cloud.Target) string {
	if a.currentProvider() == cloud.ProviderTencent || t.Kind == "ecs" {
		return "开启后会先确认新 IP 已生效，再清理本工具添加的旧规则。其他规则保留。"
	}
	return "开启后使用本工具自己的白名单分组，原来分组里的规则会保留。"
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

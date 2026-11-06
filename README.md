# YunTools

Go + MyGo 原生桌面云管理工具。首期支持阿里云，多账号加密保存，云能力通过独立接口适配。

## 运行

本次生成的应用：

| 平台 | 路径 | 验证状态 |
| --- | --- | --- |
| macOS Apple Silicon | `dist/darwin-arm64/YunTools.app` | 构建通过，已启动检查原生窗口 |
| Windows x64 | `dist/windows-amd64/YunTools.exe` | 构建通过，未在 Windows 运行 |
| Linux x64 | `dist/linux-amd64/yuntools_0.2.1_amd64.deb` 或同目录 `.tar.gz` | 构建通过，未在 Linux 运行 |

源码运行需要 Go 1.27.3（Go 的自动工具链可下载），不需要 Node.js 或 cgo：

```sh
go run .
```

首次打开点击「解锁账号保险库」，系统可能请求钥匙串授权。然后添加阿里云 RAM 用户的 AccessKey。程序未内置任何真实账号或示例资源。

新安装默认数据目录是系统应用配置目录下的 `YunTools`；macOS 为 `~/Library/Application Support/YunTools`。从旧版升级时，如存在 `AliyunTools/vault.enc`，会继续使用原目录，不移动密文或更换主密钥。系统应用标识和钥匙串服务名保留 `dev.aliyuntools.desktop`，用于兼容已有账号。

开发时可用 `YUNTOOLS_DATA_DIR` 指定独立目录，同时兼容旧变量 `ALIYUNTOOLS_DATA_DIR`（新变量优先）。不同目录使用不同主密钥名称，不会混用保险库。

## 功能

- 版本更新：设置与状态栏显示版本，自动/手动检测 `kyocn/YunTools` 的 GitHub Releases；下载进度、签名验证、安装及重启。签名密钥、CI 和发布步骤见 [版本与发布说明](docs/releases.md)。
- OSS 表格：表头与行共用列宽，文件/目录图标、左对齐名称；未知目录大小/时间为「—」，文件修改时间明确按北京时间显示。
- 列表分页：默认每页 20，可选 50/100。OSS 文件使用云端游标分页，仅保留当前页与最近 256 页的回退位置，支持回首页；名称前缀查询按回车或「查找」提交。存储空间、DNS、资源/目标、规则、账号、证书和任务使用已读取数据的本地分页，搜索先筛选全部已读取结果，再分页。这些接口仍会在后台读完云端分页，不代表它们也按界面页数请求。
- 安全组详情：双击发现的资源，或点击「查看规则/查看现有规则」，查看完整入站与出站规则、协议、端口、来源/目标、优先级和描述；支持搜索、方向筛选、分页及刷新。此详情入口只读。
- 多账号：添加、编辑、切换、连接验证、移除；跨账号资源结果隔离。
- OSS：Bucket 列表/创建/删除空 Bucket，对象分页、前缀导航、文件和文件夹上传/下载、目录占位对象、元数据、复制/移动、批量删除。
- 删除：执行前二次确认，目录先冻结对象清单；不会隐式清除历史版本。
- 传输：实时字节/速度/剩余时间，暂停/继续/失败重试，重启后恢复；下载验证远端身份，上传验证本地文件身份。
- 白名单：RDS、Redis、ECS 安全组，读取/追加/验证，按账号和地域配置目标；每分钟自动检测公网 IP 为启用的目标加白，保留原规则与旧 IP。
- DNS：列出/添加解析域名，列出/新增记录、公网地址查询；没有普通记录修改、删除、禁用操作。
- SSL：Let’s Encrypt 普通/泛域名签发，staging 模式，手动/可选自动续期，显式导出 PEM；仅清理已登记且内容仍匹配的自建临时 TXT。
- 原生 UI：中文界面、浅色/深色、左对齐侧栏、账号与模块标签页、任务中心、设置恢复。历史标签右侧可关闭，右键支持「关闭当前」「关闭右侧」「全部关闭」；以右键目标为准，最右标签禁用关闭右侧。关闭当前标签切到相邻标签，全部关闭回概览，关闭状态会保存，后台任务继续执行。

## 安全与行为边界

完整账号、令牌、ACME 账号密钥、证书私钥、任务与规则配置保存在 AES-256-GCM 加密的 `vault.enc`。随机主密钥保管在系统钥匙串，不和密文文件放在一起。保险库不可解锁时不会降级为明文或内存存储。复制 `vault.enc` 到另一台电脑不能直接恢复账号；不要删除或手工替换对应钥匙串条目。

仅点击「导出证书与私钥」会把 PEM 私钥写到选择的目录，权限为 0600（Windows 使用目录继承的 ACL）。已存在的导出文件不会被覆盖。任务断点/传输缓存不含云凭据；上传内容本身会暂存为私有本地副本，因此需要额外磁盘空间，暂停时保留。

云账号凭据轮换后，暂停任务恢复时读取当前凭据；已经运行的请求使用开始时的凭据快照。不要把某个账号配置直接替换成另一个云身份，应为新身份创建独立账号。

暂停或移除上传任务不会自动中止 OSS 已上传分片。移除任务只删除任务记录；需要时可在 OSS 控制台或通过 Bucket 分片生命周期规则清理。移动通过「复制、校验、删除源对象」执行，不是云端原子重命名；存在并发外部修改时可能保留源对象。删除标记和历史版本遵循 OSS 的版本控制语义。

公网 IP 检测通过 HTTPS `myip.ipip.net` 获取国内连接出口 IPv4，独立连接会禁用 HTTP(S) 代理，不改变云 SDK 的代理配置。系统 VPN/TUN、分流和多出口仍可能影响结果，无法保证检测服务与数据库走同一出口，可在界面手动指定 IP。检测失败不会回退到海外代理出口；自动检测拒绝内网、CGNAT 与 `198.18.0.0/15` 等代理合成地址。RDS/Redis/ECS 的实际 IPv6 支持由产品与实例决定，手动输入仍可使用实例支持的 IPv6；ECS 仅接受显式 TCP/UDP 端口范围。

证书要求域名的权威 DNS 由所选阿里云账号直接管理；当前明确拒绝 CNAME 委派验证。临时 TXT 创建响应不确定、所有权无法持久化或记录内容被外部修改时，程序保留记录，不猜测归属删除。可以用「重试验证记录清理」恢复已登记的清理任务。

自动白名单和自动续期仅在应用运行且保险库解锁时执行；窗口最小化仍可运行。退出应用不会在操作系统中安装常驻服务。证书续期时间按实际有效期的 2/3 计算，每小时检查；失败或暂停的续期任务需要在任务中心处理，避免无限重试消耗签发额度。证书不会自动部署到服务器。

## 云权限

按使用的模块授权 RAM 身份。没有相应产品权限的账号仍可保存；「验证连接」只验证 OSS 的 ListBuckets 权限。不要为方便授予全账号管理员权限。

现有策略缺少 RDS/Redis 时，见 [只读补充及修正版策略](docs/ram/README.md)。Redis 的 RAM Action 前缀为 `kvstore:`。

| 模块 | 主要 API 权限 |
| --- | --- |
| OSS | ListBuckets、GetBucketLocation、ListObjects、GetObject/HeadObject、PutObject、DeleteObject、PutBucket、DeleteBucket，以及分片上传和复制相关权限 |
| RDS | DescribeDBInstances、DescribeDBInstanceIPArrayList、ModifySecurityIps |
| Redis | DescribeInstances、DescribeSecurityIps、ModifySecurityIps |
| ECS | DescribeSecurityGroups、DescribeSecurityGroupAttribute、AuthorizeSecurityGroup |
| DNS | DescribeDomains、AddDomain、DescribeDomainRecords、AddDomainRecord |
| 证书验证 | DNS 查询/新增权限，加 DescribeDomainRecordInfo、DeleteDomainRecord |

上表列出 API 能力，具体 RAM Action 与可限制的资源范围需按阿里云产品授权表配置。应用自身的 TXT 所有权校验不会缩小账号在阿里云侧已有的权限；可以为证书验证配置独立的最小权限 RAM 身份。

## 开发与验证

```sh
./scripts/check.sh
go test -race ./...
go vet ./...
go run ./cmd/icon
go tool mygo build -platform darwin/arm64 -skip-dmg -o dist
```

MyGo 支持多平台打包，例如 `-platform darwin/arm64,windows/amd64,linux/amd64`。本地 ad-hoc 签名的 macOS 包用于本机验证，公开分发需配置自己的签名/公证。

应用图标由矢量图形直接渲染为 1024×1024 透明 PNG，四周保留 100 像素留白；macOS 包含最高 1024 像素的多尺寸 ICNS。

测试使用本地内存 HTTP transport 和 `ui.Tester`，覆盖加密篡改、并发、账号隔离、精确删除、分片续传、源变化、路径穿越/符号链接、DNS 只增、TXT 所有权和导出权限。不会创建真实云资源。视觉预览可重现：

```sh
YUNTOOLS_RENDER_DIR=/tmp/yuntools-preview go test ./internal/app -run TestVisualScreens -count=1
```

预览里的「示例账号（视觉测试）」仅存在于测试中。已通过用户账号完成一次只读联调：OSS 列表、ECS 安全组发现、DNS 域名和记录查询；RDS/Redis 在当前权限下被拒绝，待补充策略后复测。本轮没有执行云端写操作，正式 ACME 签发和传输写入仍未由开发流程实测。

## 架构

`main.go` 注入 MyGo 系统能力；`internal/app` 只在 UI 线程维护界面状态；耗时工作通过后台任务或异步请求执行。`internal/cloud` 按对象存储、DNS、白名单定义能力，`providers/aliyun` 与 `providers/aliyunoss` 封装云 SDK。后续云厂商实现对应能力并注册应用工厂即可。

设计与任务记录见 `docs/superpowers/`。界面 action 图标使用 Remix Icon v4.8.0，原始 Apache-2.0 许可和来源说明保留于 `internal/icons/` 与分发资源中。

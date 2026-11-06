# 版本与 GitHub 更新

更新仓库：<https://github.com/kyocn/YunTools>。`mygo.json` 是版本号的唯一来源；应用通过编译时嵌入该配置显示版本，MyGo 用同一配置生成更新包。当前版本 `0.2.1`。

## 使用

打开「设置 → 检查更新」。正式打包版本默认启动约 10 秒后检查，随后每天检查；可在设置关闭自动检查。发现新版本会显示中文原生窗口、版本说明和安装按钮，下载时显示进度。更新包经 Ed25519 签名验证后安装，用户可选择重启或下次启动使用。更新窗口还可选择以后自动下载安装。

应用读取公开仓库最新正式 Release 的 `update-<platform>.json`，例如 `https://github.com/kyocn/YunTools/releases/latest/download/update-darwin-arm64.json`。普通源码压缩包不是更新包；草稿和预发布版本不会作为默认更新推送。应用不携带 GitHub Token，私有仓库不能用于当前更新源。

直接 `go run .` 不支持替换安装包，设置会提示使用正式打包版本。没有已发布的更新清单、网络不可达或签名验证失败时，检查窗口会显示错误，不将失败当作「已是最新版」。只有高于当前版本的正式版本才会触发升级。

## 签名密钥

本机已生成专用更新密钥：`.local/update-keys/mygo-update.key`。文件权限为 0600，目录为 0700，已被 `.gitignore` 排除。公钥已写入 `mygo.json`；私钥不会打包进应用。请在密码管理器中备份私钥，并将其内容设置为仓库 Actions Secret `MYGO_UPDATER_PRIVATE_KEY`。后续版本必须使用同一把密钥，否则现有安装无法验证升级。

不要重新运行 keygen 覆盖这把密钥，不要将其放入源码、Release 附件或日志。CI 的环境变量优先于配置中的本地私钥路径。

## 发布流程

1. 修改 `mygo.json` 的 `version`，并在 `CHANGELOG.md` 添加完全相同版本的二级标题，例如 `## 0.2.1`。
2. 运行 `./scripts/check.sh`。本地打包可用 `go tool mygo build -platform darwin/arm64,windows/amd64,linux/amd64 -skip-dmg -skip-installer -o dist`。
3. 将代码提交到 `kyocn/YunTools`，推送对应标签（如 `v0.2.1`）。
4. `.github/workflows/release.yml` 校验标签与版本、运行测试，创建草稿 Release，并构建 macOS/Windows/Linux 的 arm64 与 amd64 版本。CI 自动打包须事先配置签名 Secret；未配置时只运行校验，并跳过打包。此时可以使用本机同一签名密钥打包，再手动上传附件发布。
5. 所有构建成功后检查草稿中的安装包、`yuntools-<version>-<platform>.tar.gz` 和对应 `update-<platform>.json`，再发布草稿为最新正式 Release。不要提前发布部分平台的附件；工作流拒绝覆盖已发布版本。
6. 从旧版本「检查更新」，确认版本说明、下载、签名校验、重启与版本变化。这一步需要实际发布比旧版更新的 Release。

更新签名与操作系统代码签名是两件事。当前 macOS 构建使用本机 ad-hoc 签名；对外分发如需通过 Gatekeeper，应配置 Apple Developer ID 签名与公证。Windows 构建未配置 Authenticode。CI 已准备更新签名，不代表已取得这些系统签名证书。

账号仍使用原有加密保险库和钥匙串身份，升级不迁移或清除账号数据。进程重启会遵循现有任务恢复逻辑；传输断点保留，自动白名单/续期仍仅在应用运行且保险库解锁时执行。

YunTools 是基于 Go / MyGo 的多账号云资源桌面管理工具，支持阿里云、腾讯云和七牛云。

### 本次更新
- 新增腾讯云：COS 对象存储、DNSPod 解析、数据库与安全组白名单，以及基于 DNS 的证书申请。
- 新增七牛云对象存储。域名、证书和白名单会标明当前不可用，并提示改用阿里云或腾讯云。
- 外观默认跟随系统。授权页和主界面使用同一主题，也可以固定为浅色或深色。
- 切换账号时留在当前页面。对象存储等资源页按账号分开保存标签；概览、设置和云账号不再附带账号名。
- 腾讯云下载中断后可以继续；七牛云下载会跟随取消。上传确认按云厂商显示 `oss://`、`cos://` 或 `kodo://`。

### 使用说明
- 已安装的正式版本可在「设置 → 检查更新」升级到 0.2.4。升级保留已有账号及加密配置。
- 七牛云目前只提供对象存储。白名单、域名和证书请使用阿里云或腾讯云账号。
- 阿里云安全组自动跟随需要 `ecs:RevokeSecurityGroup`，否则新 IP 写入后无法清理本工具管理的旧规则。

### 下载
- Apple Silicon Mac：`YunTools-0.2.4-macOS-arm64.zip`，解压后将 YunTools.app 放入 Applications。
- Windows x64：`YunTools-0.2.4-Windows-x64.zip`，完整解压并运行 YunTools.exe，保留同目录资源。
- Debian / Ubuntu x64：`yuntools_0.2.4_amd64.deb`。
- `update-*.json` 和 `yuntools-*.tar.gz` 为应用自动更新所需附件；`SHA256SUMS` 提供全部附件的校验值。

### 验证与限制
- 更新包使用既有公钥完成 Ed25519 签名校验。标签 `v0.2.4` 的源码校验已通过。
- macOS 使用 ad-hoc 签名，尚未进行 Apple Developer ID 公证；Windows 尚未配置 Authenticode。
- 账号数据与更新签名私钥不包含在源码或发布附件中。

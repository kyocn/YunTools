package app

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// friendlyError receives only redacted text. Keep service details available for
// support, especially the exact RAM action, without guessing whether a write ran.
func friendlyError(detail string) string {
	lower := strings.ToLower(detail)
	has := func(parts ...string) bool {
		for _, part := range parts {
			if strings.Contains(lower, part) {
				return true
			}
		}
		return false
	}
	var message string
	switch {
	case has("vault master key is missing"):
		message = "无法解锁已保存的账号：系统钥匙串中缺少原来的密钥。请恢复原来的系统钥匙串或备份；不要删除现有账号数据。"
	case has("vault authentication failed", "invalid vault master key", "invalid encrypted vault", "invalid encrypted account"):
		message = "无法读取已保存的账号，可能是系统钥匙串与本地数据不匹配或数据损坏。请检查原来的钥匙串与备份，不要删除现有数据。"
	case has("vault is already in use"):
		message = "账号数据正在被另一个 YunTools 窗口使用。请先退出另一个 YunTools，再重新打开。"
	case has("vault is closed", "unlock system keychain", "system keychain backend is required"):
		message = "暂时无法访问已保存的账号。请解锁系统钥匙串，并允许 YunTools 访问后重新打开应用。"
	case has("invalidsecuritytoken", "securitytokenexpired", "expiredtoken"):
		message = "临时凭证无效或已过期。请在「账号管理」中更新临时凭证后重试。"
	case has("invalidaccesskeyid", "invalidaccesskey", "accesskeyidnotfound"):
		message = "阿里云无法识别这个 AccessKey ID。请在「账号管理」中检查是否填写正确，以及密钥是否已启用。"
	case has("signaturedoesnotmatch", "signature mismatch", "incompletesignature"):
		message = "账号验证没有通过。请在「账号管理」中核对 AccessKey ID 与 AccessKey Secret 是否配套，并检查电脑时间是否准确。"
	case has("previous managed rule cleanup failed", "previous managed ip cleanup failed"):
		message = "新 IP 已核验，但旧受管记录的清理尚未确认。请先刷新规则列表核对，按下方信息检查权限或网络，再次同步可继续核验与清理。"
	case has("forbidden", "accessdenied", "unauthorized", "nopermission"):
		message = "当前账号没有执行此操作的权限。请联系阿里云账号管理员，按下方信息检查访问权限（RAM）和允许操作的资源范围。"
	case has("account credentials are required", "name, access key id and secret are required", "oss requires account", "cloud account required", "cloud account is required"):
		message = "账号信息不完整。请打开「账号管理」，填写账号名称、AccessKey ID 和 AccessKey Secret，再选择该账号。"
	case has("unsupported provider"):
		message = "目前只支持阿里云账号。请在「账号管理」中选择阿里云，并填写对应的账号信息。"
	case has("explicit tcp or udp protocol is required"):
		message = "请选择要放行的连接协议：TCP 或 UDP。请根据所用服务的说明选择，不确定时先咨询服务器管理员。"
	case has("ports must be start/end", "ports must be between 1 and 65535"):
		message = "端口格式不正确。请填写“起始端口/结束端口”，范围为 1–65535，起始端口不能大于结束端口。只放行端口 22 时填写 22/22。"
	case has("invalid dns zone", "invalid certificate domain"):
		message = "域名格式不正确。请填写域名，例如 example.com，不要带 https:// 或网页路径；中文域名请先转换为 Punycode 格式。"
	case has("invalid dns record name"):
		message = "主机记录格式不正确。请填写 www 这样的子域名前缀；根域名填写 @，不要包含空格或网页路径。"
	case has("invalid dns record"):
		message = "解析记录信息不完整或格式不正确。请检查域名、主机记录和记录值；主机记录可填 www 或 @，记录值不能为空。"
	case has("address does not match dns record type"):
		message = "IP 地址与记录类型不匹配。A 记录请填写 IPv4 地址，AAAA 记录请填写 IPv6 地址。"
	case has("invalid record destination"):
		message = "记录值需要填写有效域名，例如 example.com；不要包含 https:// 或网页路径。"
	case has("mx priority must be"):
		message = "邮件服务器优先级（MX）超出范围。请填写 1–50 的整数，数值越小优先级越高。"
	case has("unsupported dns record type"):
		message = "不支持这个解析记录类型。请选择 A、AAAA、CNAME、NS、MX、TXT、SRV 或 CAA，并按对应格式填写记录值。"
	case has("ttl out of range"):
		message = "缓存时间（TTL）超出范围。请填写 1–86400 的整数，单位是秒；不确定时可填写 600。"
	case has("context canceled", "context cancelled"):
		message = "操作已取消。如果刚才提交了添加、修改或删除，请先刷新列表确认云端结果，再决定是否重试。"
	case has("deadline exceeded", "timeout", "timed out"):
		message = "等待响应超时，暂时无法确认操作结果。请检查网络并先刷新列表；如果刚才提交了更改，请确认云端结果后再重试。"
	case has("network or cloud service error", "connection refused", "connection reset", "no such host", "network is unreachable", "network unavailable", "tls handshake", "dial tcp"):
		message = "暂时无法与云服务正常通信。请检查网络并先刷新列表；如果刚才提交了更改，请确认云端结果后再重试。"
	case has("cannot persist managed whitelist ownership"):
		message = "无法保存所有权记录，已暂停同步。请检查本地存储并保留现有数据；若刚提交了云端更改，请先刷新规则列表核对结果。"
	case has("cannot read managed whitelist ownership", "managed whitelist requires an ownership store"):
		message = "无法读取所有权记录，已停止同步。请检查本地加密存储和系统钥匙串，不要删除原有数据。"
	case has("managed whitelist ownership scope changed", "managed whitelist ownership journal is invalid"):
		message = "本地受管白名单记录与当前配置不匹配或已损坏，已停止同步。请保留原有数据并核对云端规则；需要更改资源范围时请新建配置。"
	case has("dedicated whitelist group", "whitelist group creation outcome is uncertain"):
		message = "专用白名单分组已存在，但无法确认它由此配置创建，应用不会接管或删除其中的 IP。请先在云控制台核查并保留现有分组；需要重新配置时可新建一个本地配置。"
	case has("managed security-group rule was modified", "managed rule ownership changed", "managed whitelist group metadata changed", "managed whitelist group disappeared", "managed whitelist changed during cleanup", "ambiguous duplicate whitelist group name"):
		message = "受管规则或专用白名单分组发生了其他修改，已暂停同步。请先在云控制台核对规则及其他修改来源，再决定是否新建配置。"
	case has("new managed security-group rule is not visible", "new managed ip is not visible", "submitted managed ip is not visible", "new managed ip disappeared", "new rule disappeared"):
		message = "新 IP 尚未核验通过或已发生变化，已暂停后续清理。请先刷新规则列表确认云端结果，再次同步会重新核验。"
	case has("old managed rule is still visible", "old managed ip is still visible"):
		message = "新 IP 已核验，旧受管记录的清理尚未确认。请先刷新规则列表核对，再次同步可继续核验与清理。"
	case has("managed rds/redis whitelists require an ipv4 address"):
		message = "RDS/Redis 的动态白名单需要填写一个 IPv4 地址，例如 203.0.113.8。ECS 安全组也支持 IPv6。"
	case has("persist a valid whitelist configuration id before syncing"):
		message = "白名单配置尚未完整保存。请先保存配置，再执行同步。"
	case has("appended ip not visible yet"):
		message = "添加请求已提交，但还未在云端查到新规则。请先刷新规则列表确认，不要重复添加。"
	case has("previous whitelist entry no longer present"):
		message = "新增操作后，发现部分原有白名单规则发生了变化。请刷新规则列表，并与管理员核对是否有其他人同时修改。"
	case has("oss did not confirm deletion"):
		message = "云端未确认全部删除结果，部分文件可能已经删除。请先刷新文件列表，核对剩余文件后再操作。"
	case has("cloud did not return the record id", "created record id unknown", "dns create response cannot establish exact ownership"):
		message = "添加请求已提交，但暂时无法确认新解析记录。请先刷新域名解析列表核对，不要重复添加。"
	case has("source changed", "source size changed", "source modification time changed", "source differs from checkpoint", "upload checkpoint", "download size differs from pinned source"):
		message = "传输使用的文件或续传记录发生变化，无法安全继续。请确认文件内容后，重新创建传输任务。"
	case has("unsafe object path", "destination contains a symbolic link", "download destination escapes"):
		message = "文件路径不安全，已停止操作。请改选一个普通文件夹作为下载位置，并检查文件名。"
	case has("source must be a regular file"):
		message = "所选内容不是普通文件。请重新选择需要上传的文件。"
	case has("invalid region", "bucket region differs from client region"):
		message = "地域设置与资源不匹配或格式不正确。请选择资源实际所在的地域后刷新。"
	case has("target belongs to a different account", "certificate account mismatch"):
		message = "这个资源属于另一个账号。请切换到对应账号，刷新列表后重新选择资源。"
	case has("a single valid ip address is required"):
		message = "IP 地址格式不正确。请填写一个有效的 IP 地址，或使用自动检测公网 IP。"
	case has("public ip service"):
		message = "暂时无法自动检测公网 IP。请稍后重试，或手动填写实际连接云服务时使用的出口 IP。"
	case has("returned invalid response", "invalid bucket pagination", "invalid object pagination", "repeated security group page token"):
		message = "云服务返回的数据暂时无法读取。请先刷新列表；如果刚才提交了更改，请确认云端结果后再重试。"
	default:
		// The redaction placeholder does not make an otherwise English error
		// understandable; add context while preserving the sanitized details.
		withoutPlaceholder := strings.ReplaceAll(detail, "[已隐藏]", "")
		if strings.ContainsFunc(withoutPlaceholder, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			return detail
		}
		message = "操作遇到问题，暂时无法确认结果。请保留下方详细信息以便排查；若刚提交了更改，请先核对云端状态。"
	}
	return message + "\n详细信息：" + detail
}

func shortenError(s string) string {
	if len(s) <= 700 {
		return s
	}
	end := 700
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "…"
}

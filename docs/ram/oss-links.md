# OSS 路径与访问链接

在对象行点击「复制访问链接」，可复制 `oss://bucket/key` 路径，或复制供浏览器访问的 HTTPS 链接。路径保留完整对象名；HTTPS 链接由官方 OSS Go SDK V2 编码，支持空格、中文、`#`、`?` 等字符。

访问链接先读取对象 ACL。`default` 继承 Bucket ACL；对象明确设置的 `private`、`public-read` 或 `public-read-write` 优先于 Bucket ACL。对于公开 ACL，还会查询账号及 Bucket 的「阻止公共访问」状态；只有两者均明确关闭时才复制无签名链接。任何一层已阻止公共访问，或状态因缺少权限、网络错误、未知响应而无法确认时，都会使用签名链接并说明原因。对象或 Bucket 的 ACL 查询失败、缺少查询权限或返回未知值时，同样回退到签名链接；只有明确确认可公开访问时才省略签名。

私有或回退场景使用 SDK V4 的预签名 GET URL。有效期默认 **300 秒**，可调整为 **1–604800 秒（7 天）**的整数。STS Token 随签名保留，但临时凭据提前过期会让链接提前失效。生成链接不会修改 ACL 或 Bucket 策略，也不会下载对象。签名链接可供持有者在权限和有效期内访问，因此只复制到系统剪贴板，不保存到配置、任务记录或反馈消息中。

访问仍取决于实际 RAM 权限、Bucket 策略等限制。本工具不解析任意 Bucket 策略，也不保证签名能绕过显式拒绝；私有 ACL 但经 Bucket 策略公开的对象会保守使用签名链接。

## 相关权限

以下四项仅用于自动判断公开访问状态，不是生成签名链接的必要权限：

- `oss:GetObjectAcl`：读取对象 ACL。
- `oss:GetBucketAcl`：对象 ACL 为 `default` 时读取 Bucket ACL。
- `oss:GetPublicAccessBlock`：判断账号层级是否阻止公共访问。
- `oss:GetBucketPublicAccessBlock`：判断 Bucket 层级是否阻止公共访问。

生成签名在本地完成，本身不要求 ACL 查询权限。实际通过链接下载时，签名身份必须具备 `oss:GetObject` 授权，并满足 Bucket 策略等访问限制。

`readonly-supplement.json` 已补充上述四项状态读取权限；`user-policy-fixed.json` 的 OSS 查询授权也覆盖这些操作。只需要签名链接时，ACL 或公共访问状态读取失败会回退到签名链接，不要求扩大写权限。本文与 JSON 仅是本地授权参考，没有创建、绑定或修改云端策略。

## 官方资料

- [GetObjectACL 与 default 继承](https://www.alibabacloud.com/help/en/oss/developer-reference/getobjectacl)
- [对象 ACL 优先级](https://www.alibabacloud.com/help/en/oss/user-guide/object-acl)
- [阻止公共访问及层级优先级](https://www.alibabacloud.com/help/en/oss/user-guide/block-public-access)
- [GetPublicAccessBlock](https://help.aliyun.com/en/oss/developer-reference/getpublicaccessblock)
- [GetBucketPublicAccessBlock](https://help.aliyun.com/en/oss/developer-reference/getbucketpublicaccessblock)
- [Go SDK V2 预签名下载及有效期限制](https://www.alibabacloud.com/help/en/oss/developer-reference/v2-presign-download)

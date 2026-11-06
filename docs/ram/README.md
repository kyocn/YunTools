# RAM 策略补充

本轮仅生成策略文件，没有创建、绑定或修改云端 RAM 策略。

- `readonly-supplement.json`：在已有策略之外附加的只读补充。包含 RDS/Redis 资源发现与现有白名单查询，以及 ECS 现有安全组规则查询，适用于当前只读联调。
- `user-policy-fixed.json`：基于用户提供策略的修正版，保留原有权限，补充 RDS、Redis 的查询与加白权限、ECS 规则查询与受管旧规则清理、证书清理前的 DNS 单条记录核验权限。此文件包含写权限，当前联调不需要应用它。

Redis/Tair 的 RAM 前缀为 `kvstore:`，不是 `redis:`；RDS 为 `rds:`。用户当前的 `DescribeDBInstances: Forbidden` 与 `DescribeInstances: Forbidden.RAM` 分别缺少资源发现权限。`Resource: "*"` 沿用原策略；有具体实例范围时可以按官方资源 ARN 进一步限制。

ECS 的「查看现有规则」和「立即加白」均需要 `ecs:DescribeSecurityGroupAttribute`。加白先读取规则去重，随后用 `ecs:AuthorizeSecurityGroup` 追加，最后再次读取验证。只有 `ecs:DescribeSecurityGroups`（发现安全组）和写权限仍不足以完成此流程。若 Action 已具备但仍被拒绝，需核对当前 AccessKey 对应的身份、地域/安全组资源范围与显式 Deny；应用不会绕过读取核验强行写入。

修正版没有新增 `UpdateDomainRecord`，也没有新增 RDS/Redis 删除或替换实例权限。`ModifySecurityIps` 是云侧统一的白名单写权限；普通手动追加只使用 Append 模式，受管动态同步使用 Append 与 Delete，仅清理本配置拥有的旧 IP，不使用 Cover。动态 ECS 同步另需 `ecs:RevokeSecurityGroup`，以精确规则 ID 清理旧规则。`DeleteDomainRecord` 是用户原策略已有权限；阿里云侧授权范围与应用内“仅删除自建临时 TXT”的所有权校验不同，不能宣称此 RAM Action 本身只允许删除临时 TXT。

此修正版不是应用全部能力的宽泛授权模板：原策略未包含的创建/删除 Bucket、添加解析域名、分片列举等权限仍需按实际启用功能单独配置。

应用方式：管理员可在 RAM 中将只读补充创建为自定义策略并附加到当前 AccessKey 对应身份，或合并其 Action 到现有策略。不要为了此次只读联调授权 `ram:*` 或管理员权限。权限生效后直接在应用中重新加载资源，无需重新输入 AccessKey。

核对来源：

- [ECS DescribeSecurityGroupAttribute](https://www.alibabacloud.com/help/en/ecs/developer-reference/api-ecs-2014-05-26-describesecuritygroupattribute)
- [RDS DescribeDBInstances](https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-describedbinstances)
- [RDS DescribeDBInstanceIPArrayList](https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-describedbinstanceiparraylist)
- [RDS ModifySecurityIps](https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-modifysecurityips)
- [Redis/Tair RAM 授权表](https://www.alibabacloud.com/help/en/redis/api-r-kvstore-2015-01-01-ram-redis)

## 受管动态白名单

每个保存配置具有独立的随机 `ConfigID`。加密本地存储里的所有权日志绑定账号、类型、地域、资源 ID、原分组、协议与端口；手动 IP 和自动模式开关不参与所有权身份。首次调用云 API 前先持久化配置 ID 和操作日志。改变绑定的范围会停止同步，需要新建配置并单独处理旧配置，不能通过更改 IP 或重新命名规则取得所有权。

- ECS：新规则包含配置与范围相关的唯一描述标记，日志记录精确规则 ID。先查询、追加并验证新 IP，再逐条重新核验旧规则 ID、标记、协议、端口、方向、优先级及地址后调用 `RevokeSecurityGroup`。已有相同来源的外部规则可满足此次访问，但不会被接管；旧版本的 `YunTools appended IP` 描述也不是所有权证据。
- RDS/Redis：每个配置新建独立的 `yuntools_` 加 22 位哈希分组（31 字符），不会修改用户原分组。哈希包含配置 ID 与绑定范围，因此相同资源、相同 IP 的不同配置也使用不同分组。新分组存在且没有确认的本地所有权时会停止。只记录本工具成功追加并核验的单个 IP；同步使用 Append 加入新 IP，核验后以 Delete 删除日志中已知的旧 IP。其他分组、受管分组内额外手工加入的 IP 均保留。
- 新 IP 写入失败、没有查询到新 IP、取消操作或日志无法持久化时，不删除旧 IP。旧 IP 删除失败时保留所有权信息，下次同步重试；未变化的 IP 不重复调用云端写 API。相同配置或相同资源范围在同一进程内串行执行。

RDS/Redis 的受管模式只接受 IPv4；ECS 支持单个 IPv4/IPv6。RDS/Redis 配置数量会占用云端白名单分组与 IP 配额。系统隐藏分组不会被接管，也不会整组覆盖。关闭自动模式或移除本地配置不会触发云端清理。

### 中断、丢失日志与外部修改

ECS 可通过持久化的操作意图和唯一描述标记恢复响应丢失的新增规则。RDS/Redis 没有每条 IP 的不可变规则 ID：若创建分组的响应丢失，或成功响应之后、本地确认写入之前退出，无法证明现有分组由本工具创建，因此保留云端记录并停止接管；请在云控制台核查，必要时新建本地配置。后续追加响应不明确的 IP 同样不会被自动接管或删除，可能需要人工清理。新 IP 已成功加入但旧记录清理失败时，界面会报告错误；重新同步可继续清理。

请保留加密存储，不要在另一设备复制同一配置 ID 却丢弃所有权日志。云端规则被修改、受管分组消失或分组元数据改变时，应用停止相关自动写操作。RDS/Redis API 不提供分组版本、单个 IP 的创建者或条件删除机制：外部系统在两次读取间删掉再重建同名分组／相同 IP，或者在核验与删除之间修改规则，无法由客户端完全识别。应避免其他系统改写这些专用分组；检测到的新增无关 IP会原样保留。跨设备并发不在进程锁保护范围内。

动态同步额外核对来源（2026-11-06）：

- [ECS AuthorizeSecurityGroup：创建与去重语义](https://www.alibabacloud.com/help/en/ecs/developer-reference/api-ecs-2014-05-26-authorizesecuritygroup)
- [ECS RevokeSecurityGroup：精确规则 ID 删除与 RAM Action](https://www.alibabacloud.com/help/en/ecs/developer-reference/api-ecs-2014-05-26-revokesecuritygroup)
- [RDS ModifySecurityIps：分组创建、IPv4、Append/Delete 与至少保留一条 IP](https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-modifysecurityips)
- [Redis/Tair ModifySecurityIps：分组、写入模式与 RAM Action](https://www.alibabacloud.com/help/en/redis/developer-reference/api-r-kvstore-2015-01-01-modifysecurityips-redis)

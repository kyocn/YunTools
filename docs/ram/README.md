# RAM 策略补充

本轮仅生成策略文件，没有创建、绑定或修改云端 RAM 策略。

- `readonly-supplement.json`：在已有策略之外附加的只读补充。包含 RDS/Redis 资源发现与现有白名单查询，以及 ECS 现有安全组规则查询，适用于当前只读联调。
- `user-policy-fixed.json`：基于用户提供策略的修正版，保留原有权限，补充 RDS、Redis 的查询与加白权限、ECS 规则查询、证书清理前的 DNS 单条记录核验权限。此文件包含写权限，当前联调不需要应用它。

Redis/Tair 的 RAM 前缀为 `kvstore:`，不是 `redis:`；RDS 为 `rds:`。用户当前的 `DescribeDBInstances: Forbidden` 与 `DescribeInstances: Forbidden.RAM` 分别缺少资源发现权限。`Resource: "*"` 沿用原策略；有具体实例范围时可以按官方资源 ARN 进一步限制。

ECS 的「查看现有规则」和「立即加白」均需要 `ecs:DescribeSecurityGroupAttribute`。加白先读取规则去重，随后用 `ecs:AuthorizeSecurityGroup` 追加，最后再次读取验证。只有 `ecs:DescribeSecurityGroups`（发现安全组）和写权限仍不足以完成此流程。若 Action 已具备但仍被拒绝，需核对当前 AccessKey 对应的身份、地域/安全组资源范围与显式 Deny；应用不会绕过读取核验强行写入。

修正版没有新增 `UpdateDomainRecord`，也没有新增 RDS/Redis 删除或替换实例权限。`ModifySecurityIps` 是云侧统一的白名单写权限，应用只使用 Append 模式。`DeleteDomainRecord` 是用户原策略已有权限；阿里云侧授权范围与应用内“仅删除自建临时 TXT”的所有权校验不同，不能宣称此 RAM Action 本身只允许删除临时 TXT。

此修正版不是应用全部能力的宽泛授权模板：原策略未包含的创建/删除 Bucket、添加解析域名、分片列举等权限仍需按实际启用功能单独配置。

应用方式：管理员可在 RAM 中将只读补充创建为自定义策略并附加到当前 AccessKey 对应身份，或合并其 Action 到现有策略。不要为了此次只读联调授权 `ram:*` 或管理员权限。权限生效后直接在应用中重新加载资源，无需重新输入 AccessKey。

核对来源：

- [ECS DescribeSecurityGroupAttribute](https://www.alibabacloud.com/help/en/ecs/developer-reference/api-ecs-2014-05-26-describesecuritygroupattribute)
- [RDS DescribeDBInstances](https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-describedbinstances)
- [RDS DescribeDBInstanceIPArrayList](https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-describedbinstanceiparraylist)
- [RDS ModifySecurityIps](https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-modifysecurityips)
- [Redis/Tair RAM 授权表](https://www.alibabacloud.com/help/en/redis/api-r-kvstore-2015-01-01-ram-redis)

# rc49 后端本地集成说明

本次以 rc41 `2035a82aeb5414253a728bd937d4b8f97aa99b9b` 为共同基线，将官方 rc49 `48a6c87f9e7757fdc58e5be9bcb4f8281ad0dfd0` 的后端差异适配到 KKAI。逐文件决策和原始三方快照位于 `.local-releases/rc49-integration/backend-decisions.json` 和 `backend-three-way/`。没有创建分支、工作树、提交或部署。

## 行为边界

- 令牌的 `allow_user_quota_query`：存量默认为关闭，新建 API 默认为开启，编辑省略时保留原值。关闭后，旧版 `user_total_*` 和新版 `user_quota` 均不返回余额。权限位纳入 Redis MAC 和保留实时额度的元数据修复。
- 日志导出采用 NDJSON，结尾提供成功/计数记录供前端生成 CSV。分批读取关闭游标后再投影账本，保留真实行号进行信用纪元换算，再改为用户可见序号。计费来源、租户结算及管理员/根管理员字段隐私沿用本地规则。
- 看板新增 BIGINT 缓存读写 Token 计数；消费日志使用结算后的缓存创建总数，不重复相加 5m/1h 分类。异步任务没有统一的缓存 Token 事实，不从价格、视频时长等维度推算。
- 原生插件内部 Gin 分发先继承外层可信 Host 租户上下文，再执行令牌鉴权。已发送流式响应后禁止重试、退款和再次输出 JSON。
- `SubmissionInline` 在提交接受时冻结于既有任务私有 JSON，经恢复回执和异步账务传递。后续轮询成功不会把异步提交改成同步交付。
- 重试需要更多预扣额时，使用原钱包/订阅和原令牌在同一事务补足，余额或令牌不足整体回滚。保留会员定价快照、int64 钱包、outbox、CAS、任务租约及 Studio 媒体权限。
- 插件媒体内容支持有界 JSON operation 路径、标准与 URL-safe Base64、HEAD；继续执行本地 SSRF、可信来源及下载头策略。
- 渠道重排采用独立读/写权限；模型定价转换支持端点数组及映射，并加入 12h 缓存创建计价。映射键是协议类型，映射值仍可为自定义路径对象。

## Schema 与部署限制

两个构建 profile 均要求 **PostgreSQL canonical21 + `rc49-online-v1` 扩展回执**，机器契约为 `(21,21,21)` 并明确声明 `required_expansions`。仅有版本21不能通过。独立专用扩展添加令牌余额权限和看板计数字段，保留所有 canonical21 账本记录；应用启动和普通 stage 不执行 DDL。

同库日志的审计索引用 `CREATE INDEX CONCURRENTLY` 创建，先写归属回执，完成后验证真实索引再写完成回执。主表使用 NOWAIT 短事务；锁冲突、授权过期、未知对象或无效索引都拒绝继续。无效索引只能通过独立确认的修复入口处理。当前生产专用流程只支持已确认的同库日志；独立日志库不是本次发布范围。

线上旧 max21 版本允许这些附加列，因此扩展后仍保留旧版回退能力；必须再以精确旧/新二进制混跑验证，不能仅由源码推断。canonical22 推进不属于本次交付。版本专属流程见同级基础设施 [runbook33](../../../kkai-infra/docs/runbooks/33-newapi-rc49-online-expansion.md)。本轮已获执行授权，但生产迁移与切流尚未执行。

## 定向验证

使用父任务新建的本机 PostgreSQL 18.4 隔离 schema 和本地 Go 1.26.10 工具链。以下是已验证用例范围的复现命令（实际执行分组及失败后的缩小重跑见原始日志）。`go` 指 `.local-releases/rc49-integration/toolchain/go/bin/go`，数据库变量只指向临时本机测试数据库。

```sh
go test ./model -run '^Test(IncreaseTaskBillingReservation|LogExportPreserves|TokenCacheIntegrityRepairs)' -count=1 -timeout=120s
go test ./pkg/kkaimigrate -run '^TestRC49(PostgresColumns|Expansion)' -count=1 -timeout=90s
go test ./model -run '^Test(ConsumeLogPersistsDashboardCacheTokens|TaskSubmissionInlineFact|LogOtherStructuredContent)' -count=1 -timeout=60s
go test ./controller ./router -run '^Test(TokenQuotaPermissionDefaultsAndPrivacyPostgres|NativeTaskStartedResponse|NativePluginDispatchPreservesTrustedTenant|LogExportPostgres|LogExportRules)' -count=1 -timeout=90s
go test ./controller ./router -run '^Test(TokenQuotaPermissionDefaultsAndPrivacyPostgres|NativePluginDispatchPreservesTrustedTenant|ChannelPriorityRoutesRequireScopedPermissions)' -count=1 -timeout=90s
go test ./controller -run '^TestModelPricingConversion(AcceptsDeclaredEndpointArray|DatabaseMatrix/postgres)$' -count=1 -timeout=60s
go test ./model -run '^TestChannelPriorityDatabaseMatrix$/postgres$' -count=1 -timeout=60s
go test -tags=kkai_bridge ./pkg/kkaimigrate -run '^Test(ContractForDialectRequiresRC49Schema|RuntimePlansUseReviewedTenantCompatibilityPath)$' -count=1 -timeout=60s
go test ./common -run '^TestWalletQuotaValidation' -count=1 -timeout=30s
go test ./controller -run '^TestTaskMediaContentPathPreserves' -count=1 -timeout=30s
```

相关用例均已通过；最初暴露的合并编译错误、PG fixture 初始化以及枚举类型断言已修复，并仅重跑失败范围。模型定价命令验证端点形状用例，未完整执行旧数据库矩阵。原始输出保存在同目录的 `*-targeted.log` 等日志中。

上述是初轮定向证据；后续已执行后端根模块全量和四个独立模块测试，失败按包修复重验，进展见[发布准备记录](rc49-release-readiness.md)。生产验收及旧版上游大型数据库测试布局不由这些本地结果证明。台账明确记录未恢复的历史上游测试路径，不能将定向验证解释为所有上游用例等价覆盖。

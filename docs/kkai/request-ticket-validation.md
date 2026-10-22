# 需求工单 V1：本地验收记录

本次实现客户工单列表、提交弹窗、详情时间线、管理员状态流转与必填反馈、
铃铛工单动态、持久化已读状态及异步邮件。V1 不含附件，仅开放平台主站。
客户页面使用云朵插画，适配移动端、暗色和 reduced-motion；未新增依赖。

状态、反馈事件和通知共用一个事务提交。事件表同时作为通知收件箱，避免
维护第二套通知写入流程。服务端按 tenant/owner 隔离，管理员更新使用版本
冲突检查，邮件在事务提交后尽力发送。独立前后端审查未发现剩余阻断问题。

## 已通过的后端检查

从仓库根目录执行，以下命令均退出 0：

```sh
go test ./model ./controller ./middleware ./router -run '^TestRequestTicket' -count=1 -timeout=90s
go test ./controller -run '^TestRequestTicketHTTP' -count=1 -timeout=90s
go test ./service -run '^TestRequestTicketEmail' -count=1 -timeout=90s
go test ./pkg/kkaimigrate -run 'Test(RequestTicket|ContractForDialectRequiresTenantSchema|RuntimeRejectsV11UntilExplicitTenantMigration|ResellerApplicationExpand)' -count=1 -timeout=120s
go test ./cmd/kkai-migrate -run 'Test(RequestTicket|DescribeContractJSONRequiresTenantSchema|ResellerPrefix)' -count=1 -timeout=90s
go test -tags kkai_bridge ./pkg/kkaimigrate ./cmd/kkai-migrate -run 'Test(RequestTicket|ContractForDialectRequiresExpandedTenantBridge|DescribeContractJSONRequiresExpandedTenantBridge|ContractForDialect)' -count=1 -timeout=120s
go test ./pkg/kkaimigrate ./cmd/kkai-migrate -run 'Test(MigrationCatalog|MigrationContractChecksum|ContractForDialect|RuntimePlansUseReviewedTenantCompatibilityPath|ResellerPrefix|TenantOnlineRuntimeDeclaration)' -count=1 -timeout=90s
go test ./middleware -run '^TestRequestTicketSchemaGate' -count=1 -timeout=90s
go test ./pkg/kkaimigrate -run 'Test(PlanHasImmutableChecksums|TenantSchemaImmutableChecksum|VideoStudioV5ChecksumRemainsImmutableAfterV6|TenantOnlineLedgerRejectsHistoricalBranches)' -count=1 -timeout=90s
git diff --check
```

覆盖真实状态转换、反馈校验、409 冲突、写入失败整体回滚、owner/tenant
隔离、会话/PAT 权限、GET 不改变已读状态、全部已读的并发边界、历史迁移
checksum，以及本地 SMTP 的单收件人校验和内容转义。

## 已通过的前端检查

以下命令从 `web/default` 执行：

```sh
bunx --no-install vitest run src/features/requests/admin-detail.test.tsx src/features/requests/admin.test.tsx src/features/requests/compose-dialog.test.tsx src/features/requests/detail.test.tsx src/hooks/__tests__/use-notifications.test.tsx src/components/request-notification-content.test.tsx
bunx --no-install oxlint -c .oxlintrc.json src/features/requests src/routes/_authenticated/requests src/routes/_authenticated/request-admin.tsx src/components/request-notification-content.tsx src/components/request-notification-content.test.tsx src/components/notification-popover.tsx src/components/layout/components/app-header.tsx src/components/layout/components/public-header.tsx src/hooks/use-notifications.ts src/hooks/use-sidebar-data.ts src/hooks/__tests__/use-notifications.test.tsx
bunx --no-install tsgo -p tsconfig.requests.tmp.json --noEmit
bun run i18n:sync
```

Vitest 为 6 个文件、19 项测试全部通过；lint 与类型检查退出 0。
类型检查使用继承 `tsconfig.app.json` 的临时配置，include 为本功能、变更的
通知/导航/头部和路由文件，以及真实的 `src/env.d.ts`、
`src/tanstack-table.d.ts`、`src/test-setup.ts`、`src/main.tsx` 注册入口。
临时配置与翻译脚本在完成后删除，未增加项目构建配置。
六种语言新增 66 个键，另定向修复本页面使用的 9 处旧翻译占位；占位符匹配。

浏览器使用真实开发页面与隔离的模拟 API，验证了提交、成功提示、列表、
时间线定位及键盘聚焦、铃铛切页签不标记已读、管理员反馈及流转、空状态、
390px 移动布局、暗色、法语/俄语长标签与 reduced-motion。此检查不代替
真实后端的浏览器端到端联调；后端 HTTP 行为由上述定向集成测试覆盖。

## 数据库与交付边界

完整可执行的本地初始化与 v16 启用说明见 [Schema 文档](request-ticket-schema.md)。
文档中的独占 SQLite 初始化示例实际执行成功，重复执行拒绝已有文件且文件
摘要未变；全局 schema、工单 schema、主库/日志库 prerequisites 均通过。

在该隔离库上另执行一次真实应用启动：

```sh
SQL_DSN=local LOG_SQL_DSN= \
SQLITE_PATH=/tmp/kkai-request-doccheck.NxZB45/local3.db \
TENANT_ROUTING_ENABLED=false TENANT_PLATFORM_HOSTS=localhost \
KKAI_NODE_ROLE=leader DISABLE_BACKGROUND_TASKS=true \
REDIS_CONN_STRING= PORT=50159 \
go run . --port 50159 \
  --log-dir /tmp/kkai-request-doccheck.NxZB45/logs-host-check
```

本地 HTTP harness 仅连接 `127.0.0.1:50159`，设置
`Host: localhost:50159`。`GET /api/status` 返回 200、`success: true`；
未登录访问 `/api/requests`、`/api/requests/notifications`、
`/api/requests/admin` 均返回 401、`AUTH_UNAUTHORIZED`。进程组已停止，
端口已关闭。该检查证明真实应用可在 canonical v16 本地库启动并挂载受保护
接口；完整登录后的浏览器联调仍未进行。

未运行全量测试、全量 lint、全量类型检查或生产构建。真实 PostgreSQL/MySQL
测试因未配置隔离 DSN 跳过；外部 SMTP 服务未联调，共享邮件工具现有的网络
超时行为未作扩大修改。

本次没有提交、部署或迁移生产。生产启用仍需单独完成 v16 controller/proof
流程的实现、评审、安装和授权迁移；普通发布不会自动建表或开启工单接口。

## 2026-10-22 补充：前端 schema 可用性门禁

以上为初版实现时的验证快照，保留其当时的范围和边界。以下是后续 schema
门禁修改已完成的本地检查；本节不记录生产安装、迁移或上线结果。

共享 `useRequestAvailability` 只为主站登录用户查询工单通知，每 30 秒轮询并在
窗口聚焦时重取。查询成功后才展示客户/管理员入口和铃铛工单页签；收到
`REQUEST_TICKET_SCHEMA_NOT_READY` 时隐藏入口、旧缓存内容及未读计数，列表、
详情和管理页面显示不可用提示并禁止提交，后续查询成功会自动恢复。自动查询
不触发错误 toast。新增文案已同步到全部维护的 locale（含自动生成的 zh-TW）。

从 `web/default` 执行，以下命令均退出 0：

```sh
bun run test src/hooks/__tests__/use-notifications.test.tsx src/hooks/__tests__/sidebar-config.test.tsx src/features/requests/detail.test.tsx src/features/requests/admin.test.tsx src/features/requests/admin-detail.test.tsx src/features/requests/compose-dialog.test.tsx
bun run test src/features/requests/index.test.tsx src/features/requests/admin-detail.test.tsx src/hooks/__tests__/sidebar-config.test.tsx
bun run test src/lib/__tests__/server-error-notifications.test.ts
bun run typecheck
bun run i18n:sync
bun x oxlint -c .oxlintrc.json \
  src/features/requests/use-request-availability.ts \
  src/features/requests/api.ts \
  src/features/requests/index.tsx \
  src/features/requests/index.test.tsx \
  src/features/requests/detail.tsx \
  src/features/requests/detail.test.tsx \
  src/features/requests/admin.tsx \
  src/features/requests/admin.test.tsx \
  src/features/requests/admin-detail.tsx \
  src/features/requests/admin-detail.test.tsx \
  src/hooks/use-notifications.ts \
  src/hooks/use-sidebar-data.ts \
  src/hooks/__tests__/use-notifications.test.tsx \
  src/hooks/__tests__/sidebar-config.test.tsx \
  src/lib/server-error-message.ts
bun x oxfmt --check \
  src/features/requests/use-request-availability.ts \
  src/features/requests/api.ts \
  src/features/requests/index.tsx \
  src/features/requests/index.test.tsx \
  src/features/requests/detail.tsx \
  src/features/requests/detail.test.tsx \
  src/features/requests/admin.tsx \
  src/features/requests/admin.test.tsx \
  src/features/requests/admin-detail.tsx \
  src/features/requests/admin-detail.test.tsx \
  src/hooks/use-notifications.ts \
  src/hooks/use-sidebar-data.ts \
  src/hooks/__tests__/use-notifications.test.tsx \
  src/hooks/__tests__/sidebar-config.test.tsx \
  src/lib/server-error-message.ts
```

第一条测试命令为 6 文件、36 项通过；补充测试和 lint 修正后的第二条为
3 文件、22 项通过，其中 2 文件与前一条重复。工单直接相关测试去重后为
7 文件、38 项；服务端错误消息直接依赖另有 1 文件、25 项，合计去重为
8 文件、63 项通过。错误消息测试中的 jsdom 输出两条
`Not implemented: navigation to another Document`，测试仍全部通过。

这里的 `bun run typecheck` 为完整前端类型检查，约 5 秒完成，补充了初版仅
定向类型检查的范围。15 个文件的 oxlint/oxfmt 均通过且无 warning，i18n
同步通过，仓库根目录 `git diff --check` 通过。未重复运行已通过且输入未变的
命令；未运行全量测试、全量 lint 或生产构建，真实专用账号的候选 HTTP
业务验收结果尚未写入此记录。

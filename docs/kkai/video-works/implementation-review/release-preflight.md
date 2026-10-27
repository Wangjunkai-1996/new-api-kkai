# 视频作品发布兼容性预检

2026-10-27。本记录只覆盖发布前的 Console API 契约与前端检查，不代表已完成生产构建、部署或现场业务验收。

## 必需接口与版本声明

视频作品页面要求 `/api/task/`、`/api/task/self` 的 `media_type=video` 查询具有服务器端视频分类、筛选和状态聚合，并要求响应包含 `status_counts` 及每条记录的 `billing.amount_available`。旧后端忽略新增查询字段，不能满足新前端的严格响应校验；继续把新前端声明为 Console API 2 会错误地允许不兼容组合。

本次将 default 前端要求提升为 Console API 3，后端 bridge/feature 同时支持 `[2, 3]`，保留旧前端在后端灰度期间的兼容性。原 capabilities 列表未改动。这里的版本是控制台接口契约，不是数据库 schema 或模型 Relay 协议。

在已安装并验收的 format 2 控制器上，联合更新应先完成兼容后端的 stage、候选验收、灰度和 promote，再在后端事务之外 prepare/activate 新前端。必须从现场确认当前前端格式、已安装控制器版本及其真实契约规则，不能从本地源码推断安装状态。

当前基础设施源码的 `publish_runtime` 将两槽的 capabilities 求交集，因此没有把新视频功能声明成全局 required capability。前端 API 3 明确阻止旧后端；后端回退需要先激活仍保留、同时兼容两个后端的 API 2 前端。实际 `rollback-plan` 必须确认 `frontend-then-backend` 的安全组合，不能只凭 previous 指针声明可回滚。

## 精确验证

项目根目录：

```sh
go test ./common -run '^TestConsoleContract$' -count=1 -timeout=120s
go test -tags kkai_bridge ./common -run '^TestConsoleContract$' -count=1 -timeout=120s
git diff --check -- common/console_contract.json common/console_contract_test.go web/default/console-contract.json web/default/src/lib/console-contract.test.ts web/default/src/hooks/use-console-contract.test.tsx
```

均退出 0；两个 Go profile 分别为 `0.024s`、`0.019s`。

`web/default/`：

```sh
bun run test src/lib/console-contract.test.ts src/hooks/use-console-contract.test.tsx src/components/console-compatibility-boundary.test.tsx
bunx --no-install oxlint -c .oxlintrc.json src/lib/console-contract.test.ts src/hooks/use-console-contract.test.tsx
bunx --no-install oxfmt --check console-contract.json src/lib/console-contract.test.ts src/hooks/use-console-contract.test.tsx
bun run typecheck
```

前三项退出 0；契约与边界测试为 23/23，耗时 `4.14s`。测试明确覆盖新后端支持现有前端需求、旧 API 2 后端被拒绝、不能推断更高版本向后兼容，以及状态刷新发现后端回退后关闭兼容状态。既有 capability 测试夹具补齐了已经要求的 `manual_invoice_requests`。

完整 `bun run typecheck` 已执行，退出 1：并行任务未跟踪的 `src/features/shop/pages/catalog.tsx` 在第 29、48、59 行共 7 条诊断，均为 `/shop` 系列路由尚未注册而产生的路径/参数类型错误。本次没有修改该任务的文件；检查中没有视频作品或 Console 契约相关诊断。发布前应隔离并行工作，并对最终待发布内容重跑完整类型检查；此次失败不能计作通过。

未运行全套测试、全站 lint/format、生产构建或生产命令。已完成的其他视频作品定向验证见 [实现记录](README.md)。

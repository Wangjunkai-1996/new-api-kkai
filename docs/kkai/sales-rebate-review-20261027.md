# 销售返利模块专项审查 — 2026-10-27

本轮由资金账本、权限与并发、前端交互三个审查角色并行检查，主审负责跨服务对账和交叉复核。确认一项 P1、五项 P2，以及一项有明确触发时序的低概率并发风险。优先修补申请状态转换与金额一致性，无需重构结算系统。

本报告是源码审查和本地隔离复现，未查询生产资金数据，未执行发布、重算、付款或业务代码修改。它不代表生产已经发生了下述异常。

## 基线与范围

- Invitations：`/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src`，`production/kkrich`，`e44b0a986a1e47f9a70432ed61889beed4e8cc1a`。
- NewAPI：`/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai`，`production/kkrich`；审查开始为 `47ba230e1`，结束为 `1ff0b81b9e38a8d1da3e297bd2e7acfbfd872516`。期间其他对话提交了代码，两个版本的 `web/default/src/features/invitations` 无差异。
- 覆盖销售身份、15% 规则、历史更正回执、申请/审批/拒绝/重开、人工发放、业绩统计、充值事件对账和前端失败处理；普通注册奖励作为共用流程一并检查。
- 已加载 `code-review` 和 `ponytail`；前端角色另按项目 React/shadcn 约定审查。

## 发现

### 1. P1：重复拒绝旧申请，可以改坏新申请中已发放的返利记录

位置：[admin.rs:1193](/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src/src/routes/admin.rs:1193)，入口保护在 [admin.rs:3562](/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src/src/routes/admin.rs:3562)。

触发：申请 A 被拒绝 → 同一返利记录重新提交到 B → B 获批并确认人工发放 → 再次调用 A 的拒绝接口。A 自身没有发放凭据，也不是 completed，因此检查放行；随后 handler 按 A 保存的旧记录 IDs 更新状态，没有限定当前归属必须仍是 A。

在装有全部 8 个迁移的隔离 PostgreSQL 18.4 中，执行对应 handler 的 SQL，成功将 B 已发放的记录从 `approved` 改成 `pending`。B、active ownership 和 7,500,000 quota 发放凭据均未变化。该记录随后符合现有金额调整接口的可编辑状态，能被改成 5,000,000 quota。管理员待申请/已发放统计可能重叠，可申请查询也会错误包含该记录；唯一归属约束仍会拦截重复申请，不能由此声称能够重复付款。

最小修复：对已拒绝申请重复拒绝做幂等返回；实际状态转换在事务内锁定记录并核对其当前归属，只改变本申请持有的记录。金额调整/关闭操作也应校验实际结算与归属，不能只信 `status`。增加一条“旧 A 重试不得修改已发放 B”的数据库回归测试。

### 2. P2：旧申请重开后，发放凭据与已发放统计可能使用不同金额

位置：[admin.rs:1028](/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src/src/routes/admin.rs:1028)、[sales.rs:220](/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src/src/sales.rs:220)、[sales_customers.rs:349](/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src/src/sales_customers.rs:349)。

触发：以 7,500,000 quota 提交 A → 拒绝 → 合法调整返利记录为 5,000,000 quota → 重新批准旧 A → 确认发放。重新审批没有核对当前记录与不可变申请快照是否一致。确认凭据仍按旧申请金额写入，客户业绩、个人已到账和管理员已发放统计却汇总当前记录金额。

隔离数据库复现：同一记录的确认凭据为 7,500,000 quota，业绩统计依据为 5,000,000 quota；对应展示值分别为 1,500 分和 1,000 分。申请金额冻结是预期设计，但确认和统计不能各用一套金额。人工确认接口仅记录外部发放事实，本轮没有证据表明系统自动多付款。

最小修复：重开/重新批准时锁定并核对记录金额与冻结快照；不一致则要求重新申请，确认发放时再做兜底。不改写旧快照；已发放统计应以不可变凭据作为事实依据。

### 3. P2：对账扫描范围与充值事件范围不一致，分销商订单被误报为漏返利

位置：[system.rs:490](/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src/src/routes/system.rs:490)、[topup_finalize.go:242](/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/model/topup_finalize.go:242)。

NewAPI 对 `tenant_id > 0` 的充值走分销商资金账本，仅主站分支产生返利 outbox 事件。Invitations 对账却扫描所有成功充值，没有排除分销商订单。满足时间与 ID 条件后，这些正常订单必然被判为 `topup_outbox_missing`、`topup_event_receipt_missing`。

这是源码仍存在的遗留问题，旧调查也曾记录过相同误报，本轮未重新查询生产。直接影响是对账噪声，以及要求未结案例为零的维护/重新激活流程受阻；不等同于实际漏付销售返利，也不意味着每次正常发布都会被阻断。

最小修复：统一生产事件与对账的订单资格条件，并明确旧 NewAPI schema 的兼容要求。对于历史误报，按订单身份和证据定向处理，不要批量关闭全部未结案例。

### 4. P2：刷新失败后，部分财务操作仍可用缓存数据提交

位置：[rebate-order-records.tsx:129](/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/invitations/components/admin/rebate-order-records.tsx:129)、[rebate-payout-records.tsx:140](/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/invitations/components/admin/rebate-payout-records.tsx:140)、[rebate-transfer.tsx:165](/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/invitations/components/user/rebate-transfer.tsx:165)。

初次加载成功后，后台刷新失败仍保留旧表格或已经打开的确认弹窗。最小组件复现确认：出现加载错误后，订单列表仍能执行 PATCH 调整；销售申请弹窗仍能 POST 旧记录 IDs 和旧金额。余额发放列表具有相同代码模式，本轮没有为其另做组件复现。

后端仍校验归属、状态和重复结算；但 [user.rs:496](/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src/src/routes/user.rs:496) 对客户端金额差异仅记录日志并使用服务端金额，因此用户实际提交的申请可能与刚确认的金额不同。

最小修复：复用已有审批列表的查询成功保护；查询失败时关闭操作弹窗，禁止提交，查询恢复后要求重新打开确认。mutation 入口同样检查，避免只禁用一个按钮。

### 5. P2：销售身份查询失败被当作没有销售身份

位置：[use-invitation-feature-status.ts:59](/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/invitations/hooks/use-invitation-feature-status.ts:59)、[index.tsx:54](/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/invitations/index.tsx:54)。

触发：首次进入时 `/status` 成功、`/sales-profile` 超时或报错。hook 不暴露 profile 查询错误，而以缺失数据推导身份，且不重试。普通邀请入口关闭时，销售账号会被跳回钱包并隐藏销售菜单；入口开启时可能误显示普通邀请视图。

组件复现确认 `profileQuery` 为 error，但对外 `isPending=false`、`userVisible=false`、`query.isError=false`。

最小修复：区分“查询失败”与“成功确认未注册”；失败展示错误和重试，只有成功确认无访问资格才跳转。

### 6. P2：未申请、未发放的注册奖励也无法撤回

这是共用返利流程的周边问题。位置：[admin.rs:3530](/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src/src/routes/admin.rs:3530)、[settlement 迁移:208](/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src/migrations/202607110002_request_ownership_and_settlement_state.sql:208)。

撤回操作最终物理删除 `rebate_records`。但每条记录自动创建 settlement，其外键是 `ON DELETE RESTRICT`，settlement 本身也不可删除。隔离 PostgreSQL 中，一条从未申请的 `pending invite_inviter` 删除即报 `SQLSTATE 23001 / fk_rebate_record_settlements_record`；按现有错误映射，接口返回 500。前端仍暴露对应撤回操作。

最小修复：改为可审计的关闭/撤销状态，保留账本与申请历史；不能通过删掉数据库保护约束解决。增加正常撤回的回归覆盖。

### 7. P2，低概率并发风险：销售身份当前状态与费率历史可能相反

位置：[sales.rs:144](/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src/src/sales.rs:144)、[rebate_lifecycle.rs:218](/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src/src/rebate_lifecycle.rs:218)。

更新账户在取得数据库连接和账户行锁之前捕获时间。合法交错为：停用请求在 t 捕获时间后延迟，启用请求在 t+1 先提交，旧停用请求随后提交。最终账户为停用，但历史事件按 `created_at DESC, id DESC` 选中较新时间的启用；后续订单仍可按销售规则计返利，直至下次状态更新。

隔离数据库按这一合法写入顺序复现 `profile_active=false`、`rate_selection_active=true`。这是受控时序复现，没有运行多线程压力测试，也没有证据证明生产出现过。

最小修复：同一账户完成串行化后再确定有效事件时间，并确保历史时间不倒退；不要只调整前端按钮。

## 高性价比改进

| 顺序 | 改进 | 收益与边界 |
| --- | --- | --- |
| 1 | 发放确认弹窗展示用户 ID、申请 ID、记录数 | 复用已有字段，减少同名销售或同金额申请误确认；无需新增 API。入口为 `request-action-dialog.tsx:100`。 |
| 2 | 用户返利明细展示每笔实际比例和订单 ID | 现有响应已有 `rebateRatio`，可直接核对具体订单是否适用 15%；不要以当前规则覆盖历史记录。入口为 `rebate-records.tsx:159`。 |
| 3 | 审批列表增加手动刷新 | 沿用销售账号页已有模式，降低错过新申请的概率，无需新增定时轮询。 |
| 4 | 批量处理充值对账 issue 写入 | 现有每轮对每笔订单顺序执行 6 条 SQL，正常记录也执行 UPDATE；按 issue 批量处理可减少往返。先测数量和延迟，保留锁、证据唯一性和游标语义。本轮未做性能基准，不认定它已经是生产瓶颈。 |

建议实施顺序：先修第 1 项；把第 2 项的金额一致性和第 7 项的身份事件顺序一并补牢；然后处理前端失败保护、对账范围和注册奖励撤回。展示字段等小改动可随前端修复一起交付。

## 已检查但未发现新的明确问题

- 15% 规则、订单完成时的销售身份选择、正常入账与补单共用规则选择，以及历史 21 笔更正回执的重放兼容链路。
- 销售外部确认已有申请行锁、唯一发放凭据和禁止余额发放的数据库约束；本轮不把它们误报成缺失，也没有复现重复转账。
- 请求类型按申请时身份冻结、停用销售不能新申请、不能退回普通返利结算，均有相应限制。
- 原有销售业绩 SQL 同名列问题和审批列表错误后缓存保护已修复，不重复报告。
- 分销商注册路径清空 inviter_id，尚无证据支持“正常分销商用户会泄露到销售客户业绩”的猜测；未将其列为确定问题。

## 验证命令、结果与限制

Invitations 工作目录下：

```sh
cargo test --offline --locked routes::system::tests -- --nocapture
```

结果：5 项通过，编译耗时 8.33 秒；其余测试被过滤。

NewAPI 的 `web/default` 工作目录下：

```sh
bun run test src/features/invitations/__tests__/sales-accounts.test.tsx src/features/invitations/__tests__/sales-customers.test.tsx src/features/invitations/__tests__/sales-records.test.tsx src/features/invitations/__tests__/sales-approvals.test.tsx src/features/invitations/status.test.ts src/features/invitations/admin-action-policy.test.ts src/features/invitations/payout-command.test.ts src/features/invitations/settlement-result.test.ts
./node_modules/.bin/vitest run --config /private/tmp/invitations-front-review/vitest.config.mts
```

结果：现有 8 文件、29 项测试通过（9.43 秒）；临时组件缺陷复现 3/3 通过（3.02 秒）。复现中的“通过”表示确认当前错误行为，不表示问题已经修复。

完整迁移的本地隔离 PostgreSQL 18.4：

```sh
docker exec -i codex-sales-accounting-review-20261027 psql -X -q -U postgres < /tmp/sales-rebate-stale-reject-repro.sql
docker exec -i codex-sales-accounting-review-20261027 psql -X -q -U postgres < /tmp/sales-rebate-reapprove-amount-repro.sql
docker exec -i codex-sales-accounting-review-20261027 psql -U postgres -X -v ON_ERROR_STOP=1
```

前两条均退出 0，验证第 1、2 项。第三条从 stdin 输入事务 SQL，验证第 6、7 项并回滚；首次错误捕获未涵盖 `restrict_violation`，修正预期 SQLSTATE 后断言通过。证据保留于 `/tmp/sales-rebate-accounting-review-results.txt`，前端临时复现位于 `/private/tmp/invitations-front-review/`。临时数据库容器已删除，复跑须重新创建并加载迁移，不应对生产执行这些造数脚本。

没有运行全套测试、`make check`、生产镜像构建或浏览器真实登录端到端验证。SQL 复现使用对应 handler 的 SQL 和真实数据库约束，未经过 HTTP handler；前端复现 mock 了 API。第 3 项为跨服务静态逻辑确认，性能建议未做基准。提交本报告前两业务仓库均干净，本轮仅新增此审查报告。

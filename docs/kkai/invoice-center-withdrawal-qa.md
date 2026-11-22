# KKAI 发票申请撤销测试与验收方案

- **版本**：v0.1
- **日期**：2026-11-22
- **状态**：本地定向验收已执行，发布回滚演练待执行
- **适用方案**：[发票申请撤销功能实施方案](./invoice-center-withdrawal-plan.md)
- **测试基线**：[发票中心开发技术文档](./invoice-center-development.md)、[PRD](./invoice-center-prd.md)、[UI规格](./invoice-center-ui-spec.md)

本文只定义撤销能力的测试、验收、数据库兼容和回滚验证要求。下列矩阵描述验收目标，本次实测范围列在文末。

## 1. 验收对象

撤销能力的目标行为如下：

- 用户只能撤销本人、当前 tenant 下的 `pending_review` 申请
- 申请从未交付、没有实际开具证据时才允许撤销
- 撤销将申请改为 `withdrawn` 终态
- 申请记录、订单明细、资料快照、事件和幂等记录继续保留
- 关联 `invoice_order_claims` 在同一事务内全部释放
- 释放后的付款重新出现在候选列表，并可创建新的申请
- 已审批、财务暂停、已交付、已确认、异议和已拒绝申请不能通过撤销接口释放付款
- 同一次请求重试返回相同结果，不重复写事件或产生第二张申请

## 2. 后端服务测试

### 2.1 正常流程

| 场景 | 前置状态 | 操作 | 预期结果 |
| --- | --- | --- | --- |
| 用户撤销待审核申请 | `pending_review`，存在一条或多条 claim | 调用 `Mutate(..., "withdraw", ...)` | 返回 `withdrawn` 详情，version 加一 |
| claim 释放 | 同上 | 查询 `invoice_order_claims` | 该 request 的 claim 数量为 0，订单明细仍存在 |
| 付款重新可选 | 已完成撤销 | 查询 `Candidates` | 原付款重新成为 eligible，金额只出现一次 |
| 重新申请 | 已完成撤销 | 使用原付款创建新申请 | 新申请成功，旧申请仍为 `withdrawn` |
| 审计留痕 | 已完成撤销 | 查询事件 | 保留 `submitted` 和 `withdraw` 事件，撤销事件 actor 为用户 |
| DTO 动作 | 已完成撤销 | 获取详情 | `allowed_actions` 为空，状态为 `withdrawn` |
| 完成分组 | 已完成撤销 | 使用 `group=completed` 查询记录 | 返回该撤销申请 |

### 2.2 非法状态和权限

每个场景都要验证数据库状态、claim 数量和事件数量没有异常变化。

| 场景 | 预期结果 |
| --- | --- |
| 用户撤销 `approved_for_issue` | `409 INVALID_STATE`，claim 保留 |
| 用户撤销 `finance_hold` | `409 INVALID_STATE`，claim 保留 |
| 用户撤销 `delivered`、`confirmed`、`disputed` 或 `awaiting_reconfirmation` | `409 INVALID_STATE`，正式文件和 claim 保留 |
| 用户撤销 `rejected` 或 `withdrawn` | `409 INVALID_STATE`，不追加事件 |
| 管理员调用用户撤销动作 | `409 INVALID_STATE` 或等效权限拒绝，不能代替用户撤销 |
| 其他用户读取或撤销申请 | `404 NOT_FOUND`，不泄露申请存在性 |
| 其他 tenant 读取或撤销申请 | `403 FORBIDDEN` 或资源不可见，不能修改 claim |
| 非 session、PAT 或不可信 Host 调用 | 按现有发票接口认证和 tenant 守卫拒绝 |
| `ever_delivered=true` 或 `issued_evidence=true` 的待审核异常数据 | 撤销不允许，claim 保留并返回 `INVALID_STATE` |

### 2.3 输入和版本

- 缺少 `version`、版本为零、负数或无法解析时返回 `400 VALIDATION_FAILED`
- 旧 version 返回 `409 VERSION_CONFLICT`
- 撤销请求不接受客户端状态、金额、tenant、用户或订单列表字段作为事实来源
- body 中多余字段不能改变服务端状态判断
- 申请不存在时返回统一资源错误，不返回付款或用户信息

## 3. 路由和控制器测试

目标接口为 `POST /api/invoices/:public_id/withdraw`。

| 检查项 | 验收要求 |
| --- | --- |
| 路由注册 | 用户路由存在，管理员路由不存在 |
| 认证 | 未登录请求返回认证错误 |
| 主站范围 | 非主站 tenant 返回拒绝，不能进入 service mutation |
| schema gate | 发票 schema 未准备好时按现有能力错误返回 |
| 请求体大小 | 超过现有 JSON 限制返回 `413 BODY_TOO_LARGE` |
| JSON 解析 | 尾随 JSON、无效 JSON 返回 `400 VALIDATION_FAILED` |
| 速率限制 | 使用 `invoice-withdraw` critical rate limit key |
| 幂等 header | 缺失或重复请求遵守现有 operation 处理规则 |
| 返回 DTO | 成功响应使用统一 `success/data` 包装，并返回完整申请详情 |
| 错误映射 | service business error 保留 HTTP 状态和错误码，存储错误不泄露 SQL 或密钥 |

## 4. 并发和幂等矩阵

所有并发场景都要使用同一申请、同一付款 claim 和不同的 operation key，检查最终状态、claim 数量、事件数量和 operation ledger。

| 并发组合 | 允许的成功结果 | 失败方预期 | 不变量 |
| --- | --- | --- | --- |
| 用户 withdraw vs 管理员 approve | 二者只能成功一个 | 另一方 `VERSION_CONFLICT` 或 `INVALID_STATE` | 不能出现 approved 和 withdrawn 双成功 |
| 用户 withdraw vs 管理员 reject | 二者只能成功一个 | 另一方冲突 | claim 最多释放一次 |
| 用户 withdraw vs 管理员 hold | 二者只能成功一个 | 另一方冲突 | 申请状态和 hold 记录一致 |
| 同一用户同一 key 重试 | 第一次结果可重放 | 无业务错误 | 只有一个 withdraw 事件和一个 operation 结果 |
| 同一 key 不同 version/payload | 无第二次写入 | `409 IDEMPOTENCY_CONFLICT` | 原结果和状态不改变 |
| 不同 key 同版本重复 withdraw | 只有第一个成功 | 第二个 `VERSION_CONFLICT` | 不重复事件、不重复通知 |
| withdraw 后立即 create | withdraw 先提交 | create 在 claim 释放后成功 | 不出现重复 claim |
| create 与 withdraw 交错 | 按付款锁和 claim 唯一约束串行 | 冲突方返回业务冲突 | 付款最多属于一个活动申请 |

验证要求包括 SQLite 写事务、MySQL `FOR UPDATE` 和 PostgreSQL `FOR UPDATE` 下的最终结果。测试不能只断言 HTTP 返回，还要读取数据库确认状态。

## 5. 付款异常回调测试

- 撤销完成后，迟到的充值异常、会员额外实收异常或退款关联检查不能把 `withdrawn` 改成 `finance_hold`
- 撤销后 claim 已释放时，异常回调不能通过旧 request 再次锁定或重建 claim
- 付款异常回调重放不增加事件、不修改 withdrawn version
- 已撤销申请的订单重新创建新申请后，新的付款异常应只影响新申请
- 回调与撤销并发时，使用统一付款锁顺序，不能出现一方提交状态、另一方提交相反状态的双成功结果

## 6. 前端行为测试

### 6.1 详情操作

| 场景 | 预期行为 |
| --- | --- |
| `allowed_actions` 包含 `withdraw` | 显示“撤销申请”按钮 |
| 其他状态或无 `withdraw` | 不显示撤销按钮 |
| 点击撤销 | 打开确认弹窗，说明会释放关联付款 |
| 弹窗确认 | 调用 `/withdraw`，payload 只包含当前 `version` |
| 请求进行中 | 禁止重复提交和关闭弹窗 |
| 成功 | 关闭弹窗、刷新当前详情和记录列表，显示 `withdrawn` |
| 版本冲突 | 显示最新版本提示，保留安全页面状态 |
| 撤销状态 | 显示撤销状态、时间线和重新申请入口，不显示确认、异议或上传操作 |

### 6.2 列表、筛选和国际化

- `withdrawn` 能出现在状态筛选和已完成分组
- 管理员全部记录可查看撤销申请，待审核队列不出现撤销申请
- 状态 badge、事件时间线、重新申请入口和撤销弹窗在六种 locale 以及繁体中文中都有翻译
- 语言切换后不出现原始 key、英文回退或布局溢出
- 无障碍检查覆盖按钮名称、弹窗标题、焦点顺序和键盘确认/取消

## 7. 数据库兼容性

当前 `invoice_requests.status` 是 `VARCHAR(32)`，方案不新增字段、表或状态约束。验收仍需覆盖所有支持数据库。

### SQLite

- 使用写事务保证申请状态、claim 删除、事件和 outbox 原子提交
- 不生成 SQLite 不支持的 `FOR UPDATE`
- 事务失败时状态和 claim 一并回滚

### MySQL

- 使用 `InvoiceLockForUpdate` 获取付款和申请锁
- 验证重复读下不会读取旧 claim 快照
- 验证唯一 claim 约束和 request version CAS 同时生效
- 兼容 MySQL 5.7.8 及以上版本

### PostgreSQL

- 使用 `InvoiceLockForUpdate` 获取付款和申请锁
- 验证事务回滚不会留下孤立事件或 outbox
- 验证参数绑定和状态筛选不依赖 MySQL 专有语法
- 兼容 PostgreSQL 9.6 及以上版本

## 8. 定向验证命令

以下命令是本功能完成前的最小验证集合。执行目录为仓库根目录，前端命令进入 `web/default`。

```bash
go test ./service -run 'TestInvoiceWithdrawal' -count=1 -timeout=90s
go test ./router -run 'TestInvoiceFinancialRoutesRequireAuthentication' -count=1 -timeout=90s
git diff --check
```

```bash
cd web/default
bunx --no-install vitest run src/features/invoice/invoice-flow.test.tsx
bunx --no-install oxlint -c .oxlintrc.json src/features/invoice/components/detail.tsx src/features/invoice/components/shared.tsx src/features/invoice/components/user-actions.tsx src/features/invoice/invoice-flow.test.tsx src/features/invoice/lib/format.ts src/features/invoice/types.ts
bun check -b
bun run i18n:check
bun run i18n:sync
```

如果新增独立测试文件或调整测试名称，命令中的测试 glob 必须同步更新。全量测试、全量 lint 和生产构建不属于本方案的定向门槛，除非发布流程另行要求。

## 9. 验收门槛

只有以下条件全部满足，方案才能从“待实现”改为“已实现”：

1. 后端服务、路由认证、前端交互和国际化测试全部通过
2. 正常撤销能释放全部 claim，并能重新创建申请
3. 所有非法状态、越权、旧版本和重复请求测试通过
4. 并发矩阵没有双成功、重复事件或孤立 claim
5. SQLite、MySQL 和 PostgreSQL 的事务及锁语义验证通过
6. `git diff --check`、涉及文件 lint 和前端类型检查通过
7. 撤销状态能在用户记录、管理员全部记录、详情和通知时间线中一致展示
8. 失败事务、网络重试和刷新不会产生无法恢复的中间状态
9. 回滚验证确认已撤销记录仍可读，且不会重新占用或误释放其他申请的付款

## 10. 回滚验证

回滚演练必须保留一条已撤销申请、一条待审核申请和一条已审批申请。

- 先隐藏前端撤销入口，确认旧前端不再发起新的 withdraw 请求
- 已撤销申请仍可读取，历史事件和申请资料不丢失
- 已释放付款不会因前端回滚自动恢复 claim
- 待审核和已审批申请仍按原有 approve、reject、hold 流程工作
- 回滚后的服务端不会把 `withdrawn` 申请重新纳入处理中或再次交付
- 回滚后重新上线新版本时，旧 withdrawn 记录仍能正确显示，并且不能再次撤销
- 任何需要恢复付款 claim 的数据修复必须单独核对事件、订单和 claim，不能通过通用回滚脚本批量恢复

## 11. 文档状态更新

完成实现后，执行以下文档同步：

- 将本文状态改为已实现，并记录实际测试命令和结果
- 更新撤销方案中的实现状态和任何与实际代码不同的细节
- 更新 PRD、开发文档和 UI 规格中的状态机、接口、分组和验收描述
- 在实施记录中加入撤销功能的代码落点、测试证据和回滚演练结论

## 本地验证记录

2026-11-22 在本地完成以下定向验证。

| 命令 | 结果 |
| --- | --- |
| `go test ./service ./model ./router ./controller ./middleware -run '^TestInvoice' -count=1 -timeout=90s` | 5 个包通过。覆盖多付款释放与重提、旧请求重放、越权、版本及状态校验、通知入队失败的事务回滚、迟到异常回执，以及发票相关路由与中间件。 |
| `KKAI_INVOICE_TEST_MYSQL_PORT=60173 KKAI_INVOICE_TEST_POSTGRES_PORT=60174 go test ./service -run '^TestInvoiceWithdrawalRace$' -count=1 -v -timeout=90s` | SQLite、MySQL 5.7、PostgreSQL 9.6 三个子测试均执行并通过。同版本撤销与审批只有一个成功，测试核对最终状态、付款占用、事件及操作记录。 |
| `bun run test -- src/features/invoice/invoice-flow.test.tsx src/features/invoice/invoice-ui-review.test.tsx src/features/invoice/invoice-i18n.test.ts` | 3 个文件共 43 项通过。撤销只提交 version，覆盖取消、网络错误后同键重试、详情刷新、时间线及七种语言。 |
| `bun check -b`、`bunx --no-install oxlint -c .oxlintrc.json src/features/invoice src/i18n/static-keys.ts`、`bun run i18n:check` | 类型、lint 和翻译键检查通过。 |

前端命令在 `web/default` 运行。上述两个 TCP 端口属于当次临时容器。复验时给两个环境变量传入新的本地映射端口，每个数据库使用空库 `kkai_test_invoice` 和专用测试账号 `invoice_test`（同名测试密码）。测试拒绝已有表的数据库。SQLite 测试使用单连接内存库，MySQL 和 PostgreSQL 使用各自的多连接事务及行锁。

事务回滚在 SQLite 中注入 outbox 写入失败后核对，再使用原幂等键成功提交。跨数据库并发实测针对撤销与审批。浏览器端到端、独立的拒绝/暂停/异常回调并发组合及版本回滚演练未纳入本次实测。全量套件未运行。

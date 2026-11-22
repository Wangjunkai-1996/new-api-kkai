# KKAI 发票申请撤销后端开发方案

- **版本**：v0.1
- **日期**：2026-11-22
- **状态**：已在本地实现并通过后端定向测试
- **适用范围**：Go 后端、发票路由、发票服务、付款异常协同及后端测试
- **关联方案**：[发票申请撤销功能实施方案](./invoice-center-withdrawal-plan.md)

本文只定义后端开发契约。完成代码、定向测试和验收前，不能把撤销能力视为已实现。

## 1. 目标和边界

用户可以撤销本人处于 `pending_review` 的发票申请。撤销后申请进入独立终态 `withdrawn`，关联的付款 claim 在同一事务中全部释放，申请本身、申请明细、资料快照和事件时间线继续保留。

以下状态不允许用户撤销：

- `approved_for_issue`
- `delivered`
- `confirmed`
- `disputed`
- `awaiting_reconfirmation`
- `finance_hold`
- `rejected`
- `withdrawn`

不提供部分订单撤销、删除申请、恢复撤销申请或管理员代替用户调用撤销接口。管理员继续使用现有 `reject` 动作处理审核拒绝。

## 2. 状态常量和查询契约

在 `model/invoice.go` 增加：

```go
InvoiceWithdrawn = "withdrawn"
```

`InvoiceAllowedActions` 的用户分支仅在以下条件同时满足时返回 `withdraw`：

```text
status == pending_review
ever_delivered == false
issued_evidence == false
```

管理员分支不返回 `withdraw`。`withdrawn` 的 `allowed_actions` 必须为空。

列表和统计需要同步以下集合：

- 合法 `status` 筛选加入 `withdrawn`
- 用户 `completed` 分组加入 `withdrawn`
- `processing`、`awaiting_confirmation` 不包含 `withdrawn`
- 管理员待审核、待开票、异议队列不包含 `withdrawn`
- `summary.counts` 保留 `withdrawn` 计数
- `invoiced_totals` 仍只按 `ever_delivered` 汇总，因此撤销申请不计入已开票金额

## 3. HTTP 路由和 Controller

新增用户路由：

```text
POST /api/invoices/:public_id/withdraw
```

路由要求：

- 复用 `middleware.UserAuth()`、`middleware.DisableCache()` 和 `InvoiceSchemaGate`
- 使用 `middleware.UserCriticalRateLimit("invoice-withdraw")`
- 复用 `controller.InvoiceAction(false, "withdraw")`
- 继续限制主站 tenant、dashboard session 和资源 owner
- 使用现有 `invoiceDecode`，拒绝超大或带尾随内容的 JSON
- 要求 `Idempotency-Key`

请求体：

```json
{
  "version": 3
}
```

Controller 不新增业务分支。它只读取 `public_id`、`dispute_id`、Idempotency-Key 和 JSON body，交给 `InvoiceService.Mutate`，错误继续经 `invoiceHTTPError` 转换。

## 4. Service 实现

### 4.1 Mutate 动作

在 `service/invoice_core_mutations.go` 的动作分支加入 `withdraw`。

执行前必须经过现有流程：

1. 校验 scope
2. 查询本人可见的申请
3. 按 action、申请 ID 和 dispute ID 生成 operation identity
4. 先重放已完成 operation
5. 校验当前 request version
6. 校验 `InvoiceAllowedActions`

`withdraw` 不调用政策版本检查和 quote 重算，因为它不会交付发票，只释放该申请已经持有的 claim。

动作成功时更新：

```text
status = withdrawn
version = version + 1
updated_at = now
```

`reason_cipher`、`hold_from_status`、交付时间、确认时间和已开具证据保持原值。正常待审核申请没有这些值，更新时仍按现有 CAS 更新映射写回完整状态字段。

### 4.2 事件和通知

使用现有 `s.event` 写入一条事件：

```text
kind = withdraw
status = withdrawn
actor_admin = false
description = empty
```

事件必须和状态、claim 删除、operation result 在同一事务中提交，并经现有 `invoice:event:<public_id>` outbox 机制发送。重复请求只重放 operation result，不能重复创建事件或通知。

## 5. 事务、锁顺序和 Claim 释放

撤销使用现有 `InvoiceService.Mutate` 事务，不另起独立删除接口。

锁顺序必须保持发票模块的统一顺序：

1. 锁定 operation 唯一键
2. 读取申请明细并按 `canonical_payment_key ASC` 锁定所有付款事实
3. 按申请内部 ID 锁定 `invoice_requests`
4. 在申请行锁下删除该 request 的全部 `invoice_order_claims`
5. 写事件和 outbox

付款事实锁和申请锁使用现有 `model.InvoiceLockForUpdate`、`lockForUpdate` 及 `lockQuotePayments`。不得先锁申请再回头锁付款，不得使用旧的 `gorm:query_option`。

Claim 删除条件必须同时包含 tenant 和 request ID：

```text
tenant_id = row.TenantID AND request_id = row.ID
```

不得按用户、订单号、金额或 public ID 模糊删除。删除结果为零也可以成功，因为 operation/CAS 已保证该申请仍处于单次撤销事务中，重放不会再次写事件。

事务提交后的效果必须满足以下不变量：

- `withdrawn` 申请没有活动 claim
- 申请明细仍然存在
- 候选查询可以重新返回原付款
- 事务失败时状态和 claim 同时回滚
- 不出现付款已释放但申请仍为 `pending_review` 的中间提交状态

## 6. 幂等和并发

继续使用 `invoiceOperation` 和 `invoiceFinishOperation`。

- 同一 actor、同一 operation、同一 Idempotency-Key 和同一 payload 返回原结果
- 同一 key 携带不同 payload 返回 `IDEMPOTENCY_CONFLICT`
- 网络超时重试必须复用原 key
- operation replay 发生在 stale version 判断之前
- 用户撤销和管理员审批并发时，只有持有有效版本并成功 CAS 的事务可以提交
- 另一个事务收到 `VERSION_CONFLICT` 或 `INVALID_STATE`，不得删除 claim
- 撤销成功后，使用新 key 和撤销后的版本再次撤销返回 `INVALID_STATE`。使用新 key 和撤销前版本返回 `VERSION_CONFLICT`

`InvoiceActionInput` 可复用现有 `Version` 字段，不新增撤销专用请求 DTO。

## 7. 付款异常终态

在 `model/invoice_payment_safety.go` 的付款异常处理入口，将 `InvoiceWithdrawn` 与 `InvoiceRejected` 视为终态。

迟到的退款、异常回执或会员实收异常回调在发现已撤销申请时：

- 不再次转入 `finance_hold`
- 不返回非法状态错误
- 不重建 claim
- 不覆盖 `withdrawn` 状态

正常情况下 claim 已经释放，回调不会通过活动 claim 找到撤销申请。终态分支仍需保留，用于处理历史 claim、竞态读取或已提交回调。

## 8. 错误码和响应

沿用现有发票错误协议：

| 场景 | HTTP | 错误码 |
| --- | --- | --- |
| 申请不存在或不属于当前用户 | 404 | `NOT_FOUND` |
| request version 过期 | 409 | `VERSION_CONFLICT` |
| 状态不是允许撤销的待审核状态 | 409 | `INVALID_STATE` |
| 同一 Idempotency-Key 对应不同 payload | 409 | `IDEMPOTENCY_CONFLICT` |
| body、version 或 Idempotency-Key 无效 | 400 | `VALIDATION_FAILED` 或 `IDEMPOTENCY_KEY_REQUIRED` |
| schema 未准备好 | 503 | `SCHEMA_NOT_READY` |

成功返回现有 `InvoiceDTO`，其中：

```text
status = withdrawn
allowed_actions = []
```

不新增专用成功响应结构。

## 9. 数据库兼容性

当前 v17 的 `invoice_requests.status` 是 `VARCHAR(32)`，没有数据库枚举或 CHECK 约束。增加 `withdrawn` 不新增字段、表、索引或迁移版本。

实现必须继续兼容 SQLite、MySQL 5.7.8 及以上和 PostgreSQL 9.6 及以上：

- 使用 GORM 查询和删除
- 使用 `InvoiceLockForUpdate` 统一行锁
- 不使用 PostgreSQL partial index
- 不使用 MySQL 专用函数
- 不依赖 SQLite 不支持的锁语法
- 保留 tenant、request、claim 的现有索引使用方式

如果后续 schema contract 为 status 增加枚举约束，必须单独增加跨数据库迁移，并更新迁移测试和 contract 版本。本功能当前不授权执行生产迁移。

## 10. 后端测试矩阵

撤销的主要回归测试位于 `service/invoice_withdrawal_test.go`，跨数据库测试见其中的 `TestInvoiceWithdrawalRace`。

### Service

- 待审核本人可以撤销，结果为 `withdrawn`
- 撤销释放全部 claim，原付款重新出现在 eligible candidates
- request items、profile snapshot 和 submitted/withdraw 事件仍保留
- 撤销事件 actor 为用户且不重复写入
- 撤销后 `allowed_actions` 为空
- 审批、交付、确认、异议、暂停、拒绝和已撤销状态均返回 `INVALID_STATE`
- 管理员 scope 调用 withdraw 返回 `INVALID_STATE`
- `ever_delivered` 或 `issued_evidence` 异常为真时不可撤销
- 旧 version 返回 `VERSION_CONFLICT`
- 同 key replay 返回同一 DTO，不新增事件或 claim 操作
- 同 key 不同 payload 返回 `IDEMPOTENCY_CONFLICT`
- 撤销后可以重新创建一份新申请
- completed 列表包含 withdrawn，processing 和 invoiced totals 不包含 withdrawn

### Payment safety

- 已撤销申请遭遇迟到付款异常时保持 `withdrawn`
- 付款异常不会重新创建 claim
- 已撤销申请不会被错误转入 `finance_hold`

### Router and Controller

- 未认证请求被拒绝
- 非主站 tenant 被拒绝
- 用户路由不能访问管理员撤销语义
- body 超限、尾随 JSON 和缺失 Idempotency-Key 保持现有错误
- 新路由通过 `router/invoice_test.go` 的认证矩阵

## 11. 完成标准

后端实现只有在以下条件全部满足后才可标记完成：

1. `withdrawn` 常量、查询集合和 completed 分组均已同步
2. 用户撤销路由经过认证、tenant、schema gate、限流和幂等校验
3. 撤销状态、claim 释放、事件、outbox 和 operation result 同事务提交
4. 付款锁、申请锁和 claim 删除遵守统一顺序
5. 管理员审批竞态不会产生双成功或错误释放
6. 付款异常终态不会覆盖撤销结果
7. Service、payment safety、router/controller 定向测试通过
8. `gofmt`、`go vet` 或项目约定的 Go 静态检查通过
9. 文档、代码和错误码保持一致

实际验证范围见[本地验证记录](./invoice-center-withdrawal-qa.md#本地验证记录)。

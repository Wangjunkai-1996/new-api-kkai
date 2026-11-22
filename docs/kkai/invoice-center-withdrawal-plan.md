# KKAI 发票申请撤销功能实施方案

- **版本**：v0.1 方案稿
- **日期**：2026-11-22
- **状态**：已在本地实现，定向验证结果见[验收记录](./invoice-center-withdrawal-qa.md#本地验证记录)
- **基线**：[发票中心开发技术文档](./invoice-center-development.md)、[PRD](./invoice-center-prd.md)、[UI规格](./invoice-center-ui-spec.md)

## 1. 目标与范围

一期发票中心只支持用户查看待审核进度。本方案增加用户自助撤销能力，保持申请记录、事件时间线和幂等账迹，并释放该申请占用的付款，使用户可以重新创建申请。

本期只允许用户撤销 `pending_review` 状态的申请。管理员已经审批、暂停、上传或交付处理后，申请不再允许用户撤销。这样可以避免管理员已开始人工开票后，用户端释放付款占用造成业务事实不一致。

撤销使用独立终态 `withdrawn`。不复用 `rejected`，因为拒绝代表管理员确认未开票并作出审核结论，撤销代表用户主动放弃当前申请。

本方案不包含以下能力：

- 部分订单撤销或从申请中移除单笔付款
- 已审批申请撤销
- 已交付或已实际开具票据的撤票、红冲或退款
- 恢复已撤销申请
- 管理员代替用户执行撤销

## 2. 状态和付款占用

### 2.1 状态变化

| 当前状态 | 操作者 | 动作 | 目标状态 | 付款占用 |
| --- | --- | --- | --- | --- |
| `pending_review` | 用户 | `withdraw` | `withdrawn` | 在同一事务中释放全部 claim |
| `pending_review` | 管理员 | `reject` | `rejected` | 按现有规则释放全部 claim |
| `pending_review` | 管理员 | `approve` | `approved_for_issue` | 保持占用 |
| `pending_review` | 管理员 | `hold` | `finance_hold` | 保持占用 |
| `withdrawn` | 任意操作者 | 任意写操作 | 拒绝 | 终态 |

`withdrawn` 申请保留原申请、订单明细、资料快照和事件记录。释放的是 `invoice_order_claims`，不是删除申请或订单明细。释放提交后，候选接口可以重新返回这些付款，用户可以用它们创建新申请。

撤销必须满足以下条件：

- 请求身份是申请本人，且使用主站用户 session
- 申请属于当前 tenant，`status = pending_review`
- `ever_delivered = false`
- `issued_evidence = false`
- 请求携带当前 `version`
- 同一申请中的所有付款 claim 一次性释放

### 2.2 列表、分组和统计

- `withdrawn` 加入用户和管理员的合法状态筛选集合
- 用户 `completed` 分组包含 `confirmed`、`rejected`、`withdrawn`
- `withdrawn` 不计入 `processing`、`awaiting_confirmation` 或 `invoiced_totals`
- 管理员不把 `withdrawn` 放入待审核、待开票或异议队列，但可以在全部记录中查看
- `summary.counts` 返回 `withdrawn` 数量，现有核心摘要字段不新增撤销专用计数
- 申请的金额仍保留在历史记录中，不计入已开票金额

## 3. HTTP 接口

新增用户接口如下。

```text
POST /api/invoices/:public_id/withdraw
```

请求体只需要并发版本。

```json
{
  "version": 3
}
```

请求要求使用 `Idempotency-Key`。客户端同一次重试必须复用原 key。

成功响应返回更新后的完整 `InvoiceRequest` DTO，状态为 `withdrawn`，`allowed_actions` 为空。错误沿用现有发票错误协议。

| 情况 | HTTP | 错误码 |
| --- | --- | --- |
| 申请不存在或不属于本人 | 404 | `NOT_FOUND` |
| 版本已变化 | 409 | `VERSION_CONFLICT` |
| 申请不是 `pending_review` | 409 | `INVALID_STATE` |
| 幂等 key 与历史请求 payload 不同 | 409 | `IDEMPOTENCY_CONFLICT` |
| 请求体或版本无效 | 400 | `VALIDATION_FAILED` |

不新增管理员撤销接口。管理员仍使用现有 `reject`，拒绝理由和“未实际开具”确认保持不变。

## 4. 后端实施设计

### 4.1 模型和状态常量

在 `model/invoice.go` 增加 `InvoiceWithdrawn = "withdrawn"`。当前 `invoice_requests.status` 是 `VARCHAR(32)`，没有状态枚举约束，因此按现有 v17 DDL 不新增字段、表或 schema migration。若后续 DDL 增加状态约束，必须追加独立迁移并更新 schema contract。

### 4.2 路由和控制器

- 在 `router/invoice.go` 用户发票路由中新增 `POST /:public_id/withdraw`
- 复用 `middleware.UserCriticalRateLimit("invoice-withdraw")`
- 复用 `controller.InvoiceAction(false, "withdraw")`
- 保持 session、主站 tenant、schema gate、body 限制和 idempotency key 校验
- 在路由认证测试中加入撤销接口

### 4.3 状态动作和事务

在 `service.InvoiceAllowedActions` 中，仅为非管理员且 `pending_review` 返回 `withdraw`。

在 `InvoiceService.Mutate` 中增加 `withdraw` 分支，事务顺序沿用现有申请写入约定：

1. 先按 operation 唯一键处理幂等重放
2. 读取并锁定申请关联的付款事实
3. 按 tenant、public ID 和 user ID 锁定申请
4. 校验 `version` 和 `InvoiceAllowedActions`
5. 将状态改为 `withdrawn`，递增 request version，更新 `updated_at`
6. 按 request ID 删除全部 `invoice_order_claims`
7. 写入 `withdraw` 事件，事件状态为 `withdrawn`
8. 写入 outbox 通知事件
9. 在同一事务中完成 operation result
10. 提交后返回新的详情 DTO

删除 claim 必须在状态更新事务内完成。任何一步失败都回滚状态、claim、事件和 operation result，不能出现已经撤销但付款仍被占用，或付款已释放但申请仍显示处理中。

撤销不需要调用当前政策校验，也不需要重新 quote。它只释放已经由该申请持有的 claim，不修改付款事实、订单金额、申请金额或资料快照。

### 4.4 付款异常协同

`model/invoice_payment_safety.go` 的付款异常处理要把 `InvoiceWithdrawn` 视为终态。撤销后如果迟到的退款、异常回执或会员实收异常回调再次发现该申请，不应将其转入 `finance_hold`，也不应返回非法状态错误。由于 claim 已释放，正常付款异常流程不应再通过该申请找到关联付款。

### 4.5 查询和 DTO

- `InvoiceAllowedActions` 在 `withdrawn` 返回空列表
- 详情、列表和事件返回 `status = withdrawn`
- `invoiceListQuery` 的合法状态集合和 completed 分组加入 `withdrawn`
- 统计逻辑将 `withdrawn` 归入 completed 语义，但不计入已开票金额
- 候选查询继续以 claim 是否存在为准，不为 withdrawn 增加特殊候选分支

## 5. 前端实施设计

### 5.1 用户详情

在 `web/default/src/features/invoice/components/user-actions.tsx` 增加撤销操作。

- 仅当 `allowed_actions` 包含 `withdraw` 时显示
- 按钮文案为“撤销申请”，使用 destructive 或低强调 destructive 样式
- 点击后打开确认弹窗
- 弹窗说明申请撤销后会释放关联付款，之后需要重新提交申请
- 提交 payload 只带 `version`
- 复用 `useInvoiceOperation`，保持 busy、错误、过期版本和幂等 key 行为
- 成功后关闭弹窗、刷新申请列表和当前详情，保留在当前详情页
- 撤销详情显示“重新申请”入口，沿用现有新建申请路由

撤销确认弹窗不得允许输入部分订单、修改金额或选择新的申请资料。撤销完成后不能在前端乐观地把订单重新加入候选，必须等待候选接口重新读取服务端结果。

### 5.2 状态和记录页

- `InvoiceStatus` 增加 `withdrawn`
- 状态 badge、筛选项、completed 分组和事件时间线增加对应文案
- 撤销记录显示在用户历史中，管理员全部记录可见
- 撤销申请不显示确认、异议、管理员处理或上传相关按钮
- `withdrawn` 与 `rejected` 使用不同标签和说明

### 5.3 国际化

新增中文、英文、法文、日文、俄文、越南文及繁体中文翻译，至少覆盖：

- Withdraw request
- Withdraw this invoice request?
- Withdrawing releases the associated payments. Submit a new request if needed.
- Invoice request withdrawn
- Withdrawn
- This request was updated. Review the latest version before continuing.（复用已有版本变化提示）

按照现有 `i18n:sync` 流程同步 locale，不在组件中硬编码其他语言文案。

## 6. 测试和验收

### 6.1 后端测试

在 `service/invoice_core_test.go` 或独立撤销测试中覆盖以下行为：

- 用户撤销待审核申请后状态为 `withdrawn`
- 所有关联 claim 被释放，原付款重新出现在候选列表
- 申请明细、资料快照、原提交事件仍保留
- 撤销后 `allowed_actions` 为空
- 审批、暂停、已交付、已确认、异议、财务暂停和已拒绝状态均不能撤销
- 管理员调用用户撤销动作被拒绝
- 旧版本撤销返回 `VERSION_CONFLICT`
- 同一 idempotency key 重放返回同一结果，不重复删 claim、不重复写业务事件
- 同一 idempotency key 携带不同 payload 返回 `IDEMPOTENCY_CONFLICT`
- 撤销与管理员审批并发时只允许一个状态转换成功，不能重复释放或重新占用
- 撤销后迟到的付款异常回调不会把申请改成 `finance_hold`

在 `controller/invoice_test.go` 和 `router/invoice_test.go` 覆盖接口认证、主站 tenant、schema gate 和撤销路径。

### 6.2 前端测试

在 `web/default/src/features/invoice/invoice-flow.test.tsx` 覆盖：

- 待审核申请显示撤销按钮
- 其他状态不显示撤销按钮
- 确认弹窗显示释放付款提示
- 提交使用当前 `version` 和撤销路径
- 成功后关闭弹窗并刷新查询
- 版本冲突显示最新申请提示并保留安全状态
- 撤销状态显示重新申请入口

### 6.3 验收标准

只有以下条件全部满足，功能才可标记完成：

1. 用户在待审核详情可以撤销一次申请
2. 撤销成功后申请记录可追溯，状态为 `withdrawn`
3. 关联付款立即重新进入可申请候选，且金额只出现一次
4. 管理员审批与用户撤销不存在双成功结果
5. 已审批或已开票申请无法通过该接口释放付款
6. 重试、刷新和重复点击不会产生重复申请、重复事件或重复通知
7. 六种前端语言和繁体中文均有完整文案
8. 既有发票创建、审批、交付、确认、异议和付款异常回归保持通过

## 7. 实施顺序和工作量

### 第一阶段

完成状态常量、服务动作、claim 释放、查询统计和路由控制器。预计半天到一天。

### 第二阶段

完成用户详情按钮、确认弹窗、状态展示、重新申请入口和六语言文案。预计半天。

### 第三阶段

完成后端并发与幂等测试、前端流程测试、路由认证测试及定向 lint/typecheck。预计半天到一天。

整体预计 1～2 个工作日。当前方案不涉及数据库结构迁移、生产数据修复或支付渠道改造。

## 8. 发布和回滚

撤销功能属于前后端接口联合变更。发布顺序应为先部署包含后端接口和状态读取能力的版本，再启用前端按钮。旧前端没有撤销入口，不影响旧申请处理。

如果发布后发现撤销事务、付款候选或统计异常，应立即隐藏前端撤销入口，并停止新的撤销请求。已成功撤销的申请不能直接改回 `pending_review`，需要依据事件和 claim 审计结果执行单独的数据修复方案。正常回滚不删除 `withdrawn` 记录，也不恢复已释放的 claim。

## 9. 实施记录

PRD、开发文档和 UI 规格已同步待审核撤销、完成分组和重新申请规则。本地测试及发布状态见[实施记录](./invoice-center-implementation.md#开票申请撤销)。

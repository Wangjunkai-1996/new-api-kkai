# 发票申请撤销功能前端开发文档

- **版本**：v0.1
- **日期**：2026-11-22
- **状态**：已在本地实现并通过组件测试、翻译检查、类型检查和定向 lint
- **关联方案**：[发票申请撤销功能实施方案](./invoice-center-withdrawal-plan.md)
- **适用范围**：`web/default/src/features/invoice/`

本文只定义前端落地契约。后端接口、状态转换和付款 claim 释放以关联方案及后端实现为准。前端完成后必须以服务端返回的 `allowed_actions` 和 `status` 驱动界面，不自行推断申请是否可撤销。

## 1. 功能边界

用户只可撤销处于 `pending_review` 的本人申请。撤销成功后申请进入独立终态 `withdrawn`，付款重新进入候选列表，原申请仍保留在记录和时间线中。

以下状态不显示撤销操作：

- `approved_for_issue`
- `delivered`
- `confirmed`
- `disputed`
- `awaiting_reconfirmation`
- `finance_hold`
- `rejected`
- `withdrawn`

撤销操作不支持部分订单选择、修改金额、编辑抬头、恢复申请或管理员代撤销。

## 2. 类型和 API

### 2.1 状态类型

在 `features/invoice/types.ts` 的 `InvoiceStatus` 增加 `withdrawn`。

状态文案和筛选集合在 `features/invoice/lib/format.ts` 增加对应项：

| 值 | 文案 key | 记录分组 |
| --- | --- | --- |
| `withdrawn` | `Withdrawn` | `completed` |

`withdrawn` 申请的 `allowed_actions` 为空。前端不得因为状态值是 `pending_review` 而忽略服务端缺少 `withdraw` 的情况。

### 2.2 API 请求

新增操作复用 `invoiceAction`，请求路径如下。

```text
POST /api/invoices/:public_id/withdraw
```

请求体只传当前申请版本。

```json
{
  "version": 3
}
```

调用必须通过 `useInvoiceOperation` 生成和复用 `Idempotency-Key`。网络超时、页面重渲染或用户再次点击重试时，针对同一 payload 继续使用原 key。成功后让现有 `['invoices']` 查询失效，以刷新记录、详情和候选付款。

错误处理沿用 `InvoiceError`：

| 错误码 | 前端行为 |
| --- | --- |
| `VERSION_CONFLICT` | 保留详情页，显示“申请已更新，请查看最新状态”，撤销弹窗不可提交 |
| `INVALID_STATE` | 使发票查询失效，提示当前状态已变化 |
| `NOT_FOUND` | 显示记录不存在提示，不把它当成可撤销成功 |
| `IDEMPOTENCY_CONFLICT` | 保留错误和当前表单状态，禁止生成新 key 重复提交 |

## 3. 用户详情交互

### 3.1 操作按钮

在 `components/user-actions.tsx` 中增加撤销按钮。

- 仅当 `invoice.allowed_actions.includes('withdraw')` 时渲染
- 文案使用 `Withdraw request`
- 采用 destructive 样式并保持键盘可操作
- 与确认、异议、补充说明按钮共用当前操作区域
- `op.busy` 时禁用所有弹窗提交按钮
- 撤销成功后关闭弹窗，清理错误状态和临时输入，并等待查询失效完成

按钮显示不应依赖前端自行判断 `invoice.status === 'pending_review'`。服务端动作集合是唯一授权依据。

### 3.2 确认弹窗

点击撤销按钮后打开确认弹窗。弹窗不包含文本框、订单选择器、金额编辑器或附件上传器。

| 元素 | 文案 key | 行为 |
| --- | --- | --- |
| 标题 | `Withdraw this invoice request?` | 说明这是当前申请的撤销确认 |
| 描述 | `Withdrawing releases the associated payments. Submit a new request if needed.` | 明确付款会释放，可重新提交申请 |
| 次按钮 | `Cancel` | 关闭弹窗，不发请求 |
| 主按钮 | `Withdraw request` | 使用当前 version 发起撤销 |

弹窗打开时记录 `invoice.version`。详情在弹窗打开后发生更新时，沿用现有 `stale` 逻辑禁用主按钮，并显示最新状态提示。关闭弹窗不会清除服务端申请。

### 3.3 成功后的详情

成功响应返回新的详情 DTO。详情页显示：

- 状态 badge `Withdrawn`
- 原关联订单和原申请金额
- 原提交事件及撤销事件
- `allowed_actions` 为空，因此不显示确认、异议、补充说明或再次撤销按钮
- “Create a new invoice request” 入口，跳转 `/invoices/new`

重新申请入口创建全新的申请。前端不复制旧申请的资料、订单选择或幂等 key，也不对候选列表做乐观修改，候选结果必须来自服务端刷新。

## 4. 记录、筛选和时间线

### 4.1 用户记录

- `withdrawn` 纳入状态筛选项
- `completed` 分组包含 `confirmed`、`rejected` 和 `withdrawn`
- 撤销申请从 `processing` 分组移出
- 撤销申请不计入待确认数量或已开票金额
- 记录行使用独立状态 badge，不能复用 `Rejected` 文案

### 4.2 管理记录

- 管理员全部记录可以查看 `withdrawn`
- `withdrawn` 不出现在待审核、待开票或异议队列数量中
- 管理端详情只读，不显示用户撤销按钮或管理员撤销按钮

### 4.3 时间线和通知

事件 `kind = withdraw` 时显示 `Invoice request withdrawn`。撤销事件由用户发起，用户自己的通知列表不应把该事件当作管理员新通知，管理员侧可看到该事件并按现有通知权限处理。

时间线保留 `submitted` 和 `withdraw` 两个事件。前端不得删除、合并或覆盖原提交事件。

## 5. 国际化 key 清单

所有 key 必须通过 `t('...')` 使用，并通过 `scripts/add-missing-keys.mjs` 写入六种基础语言，再运行 `bun run i18n:sync`。不得直接编辑 locale JSON。

| Key | 英文基准含义 |
| --- | --- |
| `Withdraw request` | 撤销申请按钮和提交按钮 |
| `Withdraw this invoice request?` | 撤销确认标题 |
| `Withdrawing releases the associated payments. Submit a new request if needed.` | 撤销确认说明 |
| `Invoice request withdrawn` | 时间线事件 |
| `Withdrawn` | 状态 badge、筛选项 |

翻译需覆盖 `en`、`zh`、`fr`、`ja`、`ru`、`vi`，并同步项目现有 `zh-TW` 资源。临时添加脚本完成后应删除，不把脚本作为产品代码提交。

## 6. 无障碍要求

- 撤销按钮使用真实 `button` 元素，支持键盘聚焦和 Enter/Space 操作
- destructive 样式保持可读对比度，不依赖颜色表达撤销含义
- 弹窗使用现有 Dialog 标题和描述语义，打开后焦点进入弹窗，关闭后返回撤销按钮
- 错误使用 `role='alert'`，版本冲突和状态变化对读屏用户可感知
- busy 状态禁用重复提交，并保留可读的 `Processing...` 文案
- 状态 badge 和时间线事件同时提供文字，不用图标或颜色替代文本

## 7. 前端测试矩阵

测试文件建议使用 `invoice-flow.test.tsx`，断言用户行为和 API 契约。

| 场景 | 关键断言 |
| --- | --- |
| 待审核且允许撤销 | 显示 `Withdraw request` 按钮 |
| 服务端未返回 `withdraw` | 不显示撤销按钮 |
| 其他状态 | 不显示撤销按钮 |
| 打开撤销弹窗 | 显示标题、付款释放说明、取消和确认按钮 |
| 点击取消 | 不调用 `invoiceAction`，弹窗关闭 |
| 确认撤销 | 调用 `invoiceAction(id, 'withdraw', { version }, key)` |
| 成功响应 | 查询失效，弹窗关闭，详情进入 withdrawn 流程 |
| 版本变化 | 主按钮禁用并显示最新状态提示 |
| 请求失败 | 错误可见，输入状态安全保留 |
| 重复点击或网络重试 | 同一 payload 复用同一 idempotency key |
| withdrawn 详情 | 显示状态、时间线和重新申请入口 |
| completed 筛选 | withdrawn 出现在完成记录中 |

定向验证命令如下。

```bash
bun run test -- src/features/invoice/invoice-flow.test.tsx --run
bun check -b
bunx --no-install oxlint -c .oxlintrc.json src/features/invoice
bun run i18n:check
```

## 8. 完成标准

前端撤销功能满足以下条件后才可标记完成：

1. 待审核申请只根据服务端 `allowed_actions` 显示撤销按钮
2. 撤销请求带当前 version，并沿用统一幂等操作流程
3. 弹窗清楚说明付款释放和重新申请路径
4. 版本冲突、非法状态、未找到和幂等冲突均有安全反馈
5. withdrawn 状态、筛选、时间线、重新申请入口和管理员只读视图完整
6. 所有新增文案已同步六种基础语言和繁体中文
7. 定向组件测试、类型检查、lint 和 i18n 检查通过
8. 详情、记录和时间线通过组件回归，实际执行范围见[本地验证记录](./invoice-center-withdrawal-qa.md#本地验证记录)

# 钱包充值生命周期后端开发规格

**2026-10-24 交互修订：** 主动确认结束成功后，在同一次用户操作内串行精确 acknowledge，成功直接回充值页，无需再点返回；失败保留 ended 与返回重试。自动超时仍保留超时提示和“返回充值”按钮。自动确认只作用于原身份、flow/revision 与原订单均仍一致的 ended 结果，实际收款、身份变化、另一当前流程和迟到旧响应不能自动清理。以下条款按此区分主动结束与自动超时。

**2026-10-23 生效修订：** [本站超时与废单规范](./wallet-payment-local-timeout-spec.md)已获用户确认，统一规定本站 30 分钟期限、本地结束／超时结果及“返回充值”、废单禁止继续支付／人工补单、晚到款继续处理。本文下方与修订冲突的“仅本地超时不能结束、未知永久锁定、30 分钟仅期望、废单仍可人工核实”条款停止适用；其余资金、权限和互斥布局要求保留。

日期：2026-10-23。状态：已确认的开发基线；当前本地实现、验证与渠道限制见 [实施记录](./wallet-payment-implementation.md)。下文“本轮只编写文档”等内容保留为设计阶段记录。

依据：[产品 PRD](./wallet-payment-lifecycle-prd.md)、[生命周期方案](./wallet-payment-lifecycle-plan.md)、[UI 规格](./wallet-payment-ui-spec.md)。本文件是本轮新增 API、DTO、持久化职责和后端行为的唯一契约来源；下文拟新增符号不是现有能力。接口实现不得反向改变已确认页面：存在当前流程时只显示订单详情；无已确认收款时，本地结束／超时均先提供精确确认结果，不等待渠道关闭；主动 end 成功由前端在原操作内串行精确 acknowledge，直接恢复表单，超时仍由用户点返回。确认成功才清流程，失败保留结果与重试。

## 1. 实施边界与不变量

- 范围为余额充值。保留会员、订阅、兑换码、租户资金批次、发票和返佣的既有契约，不将它们统一重写为新支付平台。
- 一单一个可付款会话。恢复支付只返回原会话，不创建新单、不重算价格、不延长期限；新充值必须发生在旧当前流程经服务端确认结束之后。
- 同一用户、同一租户最多一个本次当前充值流程。尚未结束且未到期的 `creating`、结果未知、支付处理中、取消中，以及已确认收款的到账核实均锁定购买；本地结束／超时和可靠终态保留结果，直到精确 acknowledge；主动 end 成功可由前端同次串行确认，超时及其他结果仍点击返回。
- 已有历史待核实单不自动变成本次流程；本次未知单不能由前端擅自归档解锁；无已确认收款时可经服务端本地结束／到期及精确确认返回。
- 验证付款、保存付款事实、发放额度和结束界面流程是不同职责。关闭支付网页、回跳参数、本地超时、申请取消均不是付款或关单证据。
- 可信迟到付款不得因本地 `expired/cancelled/failed` 被丢弃；同一订单只发放一次额度，同一渠道交易不能资助另一笔订单。
- 遵循 Ponytail full：复用现有账务、HTTP/SDK、任务和审计机制，只补必要的订单字段、当前流程和通用收款证据；不增加消息队列、通用工作流引擎、多会话重试平台或第二套钱包。

## 2. 已核实的源码地图

以下基于当前本地源码，不能证明生产安装或服务商能力。

| 入口／文件 | 当前职责与缺口 | 实施改动 |
| --- | --- | --- |
| `router/api-router.go` 的 `/api/user/pay`、`/stripe/pay`、`/creem/pay`、`/waffo/pay`、`/waffo-pancake/pay` | 5 个渠道创建入口，只有通用限流，无持久创建幂等键或当前流程约束 | 所有余额充值创建路径进入同一流程／幂等守卫；新统一 API 复用渠道实现 |
| `POST /api/user/topup` → `controller/user.go:TopUp` | **兑换码充值**，不是第三方支付创建 | 本期不改为订单创建，不因外部支付 pending 无差别封锁兑换码；前端订单阶段隐藏充值模块不等于重新定义兑换码 API |
| `controller/topup.go:RequestEpay` | SDK `Purchase` 生成支付地址和签名表单；保存 `TopUp` 后响应 `url/data`；日志包含完整参数 | 持久原表单与商户引用；表单生成与远端创建语义区别对待；清理支付凭据日志 |
| `controller/topup_stripe.go:RequestPay/genStripeLink` | 先远端 `session.New`，后建本地单；只保留 URL 返回，丢弃 session ID／期限 | 先持久本地单和 claim；固定创建幂等键；保存真实 session 和期限后才交付入口 |
| `controller/topup_creem.go:RequestPay/genCreemLink` | 先本地单，远端失败仍 pending；响应 ID 未保存；成功回调对非 pending 直接 200 | 保存 checkout／产品快照，区分未创建与结果未知；回调进入统一收据入口 |
| `controller/topup_waffo.go:RequestWaffoPay/handleWaffoPayment` | 创建异常统一 failed；非 `PAY_SUCCESS` 通知统一写 failed；未保存 checkout | 按已核实协议区分未知／处理中／可靠失败；消除读取后整行保存 |
| `controller/topup_waffo_pancake.go`、`service/waffo_pancake.go` | 已有 session ID、URL、token、期限返回；请求 45 分钟；未持久这些数据；钱包通知仅处理 completed | 保留既有 buyer identity 校验与环境分流，保存原 session；token 期限与订单期限分别处理 |
| `controller/topup.go:EpayNotify` | 验签后只处理成功；`RechargeEpayVerified` 传准确金额和 provider receipt ID | 增加原商户／环境关联和通用收据；非成功事件按真实协议归类 |
| `controller/topup_stripe.go:sessionAsyncPaymentFailed/sessionExpired/fulfillOrder` | 失败路径是进程锁＋旧对象 `Save`；关闭事件数据库失败不能反馈 webhook；充值未消费付款金额币种作通用验证 | 所有结果进入统一收据／条件转换；失败或关闭不能覆盖成功；确实未保存的事件应让渠道重试 |
| `model/topup.go`、`model/topup_finalize.go` | 订单、跨实例行锁、额度增量、租户资金批次、返佣 outbox；仅允许 pending/success 完成 | 保留事务，增加仅可信收据可完成已关闭单的受控入口，不能普遍放开旧 `FinalizeTopUp` |
| `model/topup_manual_completion.go`、`AdminCompleteTopUp` | 管理员可无收款凭据按 provider 计算并完成 pending | 旧快捷补单入口收敛到“核实支付”；无凭据调整不得调用可信支付入口 |
| `model/invoice_payment_receipt.go` | 可选发票 schema，仅平台易支付；现有金额检查部分依赖 pending | 保留发票异常 hold；通用钱包收据不能依赖发票表存在；所有订单状态都验证金额 |
| `service/invoice_payment_reconcile.go` | 已有 zpayz `api.php?act=order` 商户认证查询，校验订单／商户／币种／实收／退款信号，10 秒超时、禁止跳转、64 KiB 上限 | 提取可复用的传输和证据解析；目前仅用于已 success 的发票补证，**不等于 pending 查询、关单或到期能力已接入** |
| `model/topup.go:GetUserTopUps/SearchUserTopUps` | 本人普通列表和搜索均限近 30 天；无状态筛选；管理员不限时间 | 补明确范围的筛选聚合、未完成列表与不受窗口限制的本人精确查询；列表不顺带跑批 |
| `service/system_task.go`、`model/system_task.go` | 持久任务、每类型活动任务去重、租约心跳、进度和调度 | 新单对账与历史处理分别注册任务类型；订单级 claim 防 API／任务竞争 |
| `middleware/access_token_routes.go`、`middleware/tenant_features.go` | 路由精确授权与租户白名单，新路由默认不可用 | 显式增加已审接口，保持 `wallet:read/write`、`billing:read/write` 和平台／租户边界 |
| `pkg/kkaimigrate/catalog.go/runtime_contract_*.go`、`cmd/kkai-migrate` | 版本化 schema、精确运行契约、独立扩展流程 | 新扩展独立登记，不能修改已发布迁移，也不能由启动 `AutoMigrate` 安装 |

`controller` 只做认证上下文、输入绑定、HTTP 响应和渠道 webhook 验证；`service` 统一编排与投影状态；`model` 负责有约束的持久写入和账务事务。建议新增 `dto/wallet_payment.go`、`controller/topup_lifecycle.go`、`service/topup_lifecycle.go`、`model/topup_lifecycle.go`；渠道具体 HTTP 逻辑保留在现有对应文件或其直接配套文件，不为一个调用点拆机械 helper。

## 3. 持久化规格

### 3.1 复用字段与真实缺口

现有 `TopUp` 只有 ID、租户、用户、`PaidMinor`、`Currency`、`Amount`、`Money`、trade_no、method/provider、创建／完成时间和 status。

- `PaidMinor/Currency` 在易支付创建时已是**预期应付快照**，不能当作已收款证明。新渠道也写入准确的预期值；DTO 改称 `expected_payment`。历史空币种或零值表示未知，不能将 `Money` 乘 100 伪造证据。
- `Amount` 在不同渠道含义不同：Creem 为额度；易支付、Waffo、Pancake 为原归一化数量；Stripe 还用 `Money` 计算到账额度。保留旧字段兼容，新增准确 `credited_quota` 和额度展示快照；新单入账不再读取当时的全局 `QuotaPerUnit` 或当前商品配置重算。
- 余额链路现有 `int64` 上限、`topUpQuotaFromDecimal` 和 `ValidateTopUpQuotaCapacity` 与请求计费 int32 clamp 不同。充值沿用正值／int64 溢出检查，不将合法大额钱包强制截为 int32，也不引入裸 float→int 转换。

建议把下列最少业务信息加到 `top_ups`；名称是实施目标，DDL 的确切长度、索引名和迁移版本需在实现 PR 固化：

| 信息组 | 字段／约束 |
| --- | --- |
| 冻结计价 | 既有 `paid_minor/currency` 为原基础金额；新增 `currency_exponent`、`credited_quota`、`credit_display_amount/unit`、`pricing_mode`、`pricing_snapshot`（TEXT JSON，产品／价目／合法优惠和税费规则）；原快照不可用配置覆盖。渠道认证确认最终应付另存 `payable_minor/currency/exponent` 可空及证据引用/确认时间，不能覆盖基础金额或冒充实收 |
| 幂等提交 | `request_key_hash`、`request_hash`；`(tenant_id,user_id,request_key_hash)` 唯一，旧单 key 为空用 SQL NULL，不能用空字符串导致旧数据冲突；同键异请求拒绝 |
| 原渠道身份 | `provider_account_ref`、`provider_environment`、配置版本／商户标识；保存非秘密身份和可解析的凭据版本引用，不将原商户误换成当前配置 |
| 会话创建 | `create_state`＝creating/ready/unknown/not_created、`create_claim_token/until`、`provider_session_id`、`provider_order_id`；会话标识只能从 unknown/empty 向确认值填充，不能替换为第二会话 |
| 恢复入口 | 私有 `checkout_payload` TEXT，保存 URL 或 POST 表单及必要 token；设置 `json:"-"`，独立 DTO 白名单输出；token 的存储保护和密钥版本按第 7 节落实 |
| 时间与关闭 | `provider_expires_at`、`pay_deadline_at`、`deadline_source`；独立本地事实 `local_ended_at/local_end_reason`（user_ended/timeout）；渠道关闭意图及证据 `close_requested_at/reason`、`closed_at`、`close_evidence_ref`。本地结束不能填写渠道关闭证据，晚付款保留全部历史 |
| 对账／处理 | `provider_payment_state`（未付／处理中／已付／未知等规范化事实）、`last_checked_at`、`next_check_at`、`check_attempts`、`check_claim_token/until`、`reason_code`、`review_owner`、`review_started_at` |
| 并发版本 | `version` bigint；状态、资金／会话事实或可用动作变化后单调递增。更新时间不是并发令牌 |

业务 `status` 扩展 cancelled、review_required、credit_review；创建和处理进度由明确字段派生，不再保存另一套与其重复的 UI 状态机。存量未知原始状态投影 unavailable；只有建立实际核实责任后才能写 review_required。

### 3.2 当前流程表 `wallet_topup_flows`

一行一个 `(tenant_id,user_id)`，跨数据库唯一约束；字段至少 `flow_id`（本次流程不透明 ID）、`revision`、`top_up_id` 可空、`last_acknowledged_flow_id`、`last_acknowledged_trade_no`、`updated_at`。idle 时也保留作用域行与 revision，不能清零版本。

- 创建／恢复绑定时生成新 flow_id；结束时递增 revision 并清除当前引用。旧 ack 重放仅在仍为空且最后确认身份一致时幂等成功；已有新 flow 时返回冲突及最新状态，绝不能清除它。
- phase 根据绑定订单推导，不单独写入一套可能与订单失配的 phase。回调只更新订单和账务；不结束当前流程，不需要反向锁 flow。
- 无当前时 `restore` 只可绑定快照完整、原会话已确认且尚未本地结束／到期的历史对象；纯历史 review、仅账龄旧 pending、订阅／会员镜像单不自动绑定。
- 当前流程可通过 end 结束本站操作，或按本站期限进入 timed_out；保留 result 直到精确 acknowledge。创建结果未知不允许重建同单会话，结束后原单仍在后台核实且不再自动恢复为当前。
- 参考全局锁顺序：流程动作 `flow → topup → receipt（若涉及）→ user`；账务 `topup → receipt → user`，不再反锁 flow。首建 flow 竞争依唯一约束重读。SQLite 使用既有 busy 有限重试与条件更新，不能把无 FOR UPDATE 当作已锁定。

### 3.3 通用收款与订单事件

新增 `wallet_payment_receipts`：规范化 provider、原商户／环境、provider transaction ID、TopUp 关联（待匹配时可空）、金额最小单位、币种与 exponent、实际付款时间、接收时间、证据来源与摘要、验证状态、processing_status、异常原因／负责人、next_retry_at。唯一 `receipt_key` 使用规范化身份元组的 SHA-256；命中后仍比较原字段，不以哈希相等替代内容校验。

唯一元组固定为 `(provider, provider_account_ref, provider_environment, provider_transaction_id)`，不用 user 或订单号拆开同一收款。同一已验证交易只能关联一单；一单可保留多笔真实收款但额度只发一次。渠道 event ID 用于去重通知，**不自动等同资金交易 ID**；需要在渠道验证中确认收款唯一标识。无法匹配本地订单的可信付款也先持久化为待匹配异常，不因查无订单直接 200 丢弃。

新增小型 `wallet_payment_events`（或在实现时证明等价的既有持久业务审计表中保存）：order、事件种类、actor、来源、渠道证据引用、前后业务结果、原因、时间、去重键。用于保留取消申请、可靠关闭、迟到款、人工核实及责任转移。普通应用日志／任务结果会轮转，不替代付款和关闭审计。事件内容白名单脱敏，无完整 checkout、签名、密钥和买家隐私。

通用收据先在短事务中可靠保存，再尝试账务事务；账务失败不能回滚前面已保存的收款事实。处理状态、订单成功、额度、租户资金批次或返佣 outbox 在同一账务事务提交；失败后收据保留 pending/exception，后台继续处理。发票收据在满足既有 schema 和资格时由已验证付款关联，保留其异常 hold 和唯一性，不能把通用收据存在当作解除发票异常的理由。

## 4. API 契约

### 4.1 公共规则

新接口成功使用 `{success:true,message:"",data:...}`；失败使用 `{success:false,code,message,data?}`，code 稳定且供前端 i18n 映射，message 为安全摘要。202 代表已持久接收但未确定，不代表付款／取消成功。旧 API 保留旧 response 外壳，只通过适配器进入同一服务，不重新实现业务。

所有 int64 金额、额度、订单版本和 flow revision 序列化为十进制字符串，避免 JS 精度损失。时间为 UTC Unix 秒，未知为 null；已知创建时间按本站 30 分钟规则计算截止，创建时间未知不伪造期限，渠道期限仍保留原事实。服务端控制币种 exponent，不能假定所有币种都是两位小数。

用户 scope 仅从 `UserAuth`、TenantContext 和数据库归属确定，不接收正文 `user_id/tenant_id` 覆盖。本人接口即使管理员调用也只看本人；全平台接口独立 AdminAuth。跨租户／他人订单统一 404。未知 host、租户禁用渠道和 scoped access token 未授权均拒绝。所有新精确路由补 access-token 规则与租户白名单；租户仍只开放现有允许的易支付／CNY，不借统一 API 扩大渠道。

POST 使用既有 session origin/CSRF 链、UserCriticalRateLimit 和敏感操作限流；支付入口响应 `Cache-Control: no-store`、`Referrer-Policy: no-referrer`。GET 只读，不按列表请求触发整批核实。callback 保留 anonymous body limit 和渠道验签，禁用新购买不能同时停止已有订单回执处理。

### 4.2 DTO

```ts
type MoneyDTO = { amount_minor: string; currency: string; exponent: number }
type DisplayStatus = 'creating' | 'pending' | 'payment_processing' | 'cancel_processing'
  | 'expiry_processing' | 'review_required' | 'credit_review' | 'success'
  | 'cancelled' | 'expired' | 'ended' | 'timed_out' | 'failed' | 'unavailable'
type StatusGroup = 'all' | 'pending' | 'processing' | 'review' | 'completed' | 'closed'
type OrderDTO = {
  trade_no: string
  version: string
  display_status: DisplayStatus
  status_group: StatusGroup
  allowed_actions: ('resume' | 'cancel' | 'end' | 'reconcile' | 'contact_support' | 'acknowledge')[]
  expected_payment: MoneyDTO | null
  pricing_mode: 'fixed' | 'channel_adjusted' | 'unknown'
  payable_payment: MoneyDTO | null
  pricing_notice_code: string | null
  credited_quota: string | null
  credit_display: { amount: string; unit: string } | null
  payment_provider: string
  payment_method: string
  created_at: number
  deadline_at: number | null
  deadline_source: 'provider' | 'local_policy' | 'unknown'
  close_reason: 'user_cancel' | 'timeout' | null
  closed_at: number | null
  local_ended_at: number | null
  local_end_reason: 'user_ended' | 'timeout' | null
  completed_at: number | null
  last_checked_at: number | null
  reason_code: string | null
  received_payment: MoneyDTO | null
  review: { reason_code: string; owner_label: string; last_checked_at: number | null } | null
  legacy_display: { amount: string | null; money: string | null } | null
}
type WalletState = {
  flow: {
    id: string | null
    revision: string
    phase: 'idle' | 'creating' | 'order' | 'processing' | 'review' | 'result'
    order: OrderDTO | null
  }
  can_create: boolean
  create_block_reason_code: string | null
  other_unfinished_count: number
  server_time: number
}
type CheckoutDTO =
  | { kind: 'url'; url: string }
  | { kind: 'form'; method: 'POST'; action: string; fields: Record<string, string> }
```

`creating` 归 processing，表示创建进行中或短暂核实创建结果，不能冒充待付款或已建立责任的待核实。`unavailable` 归 all，不能被当 review。`received_payment` 只来源于已验证的单笔正常入账依据；第二笔收款异常不得把二笔合计伪装成原单正常实收。管理员在受权详情看脱敏收据列表。`legacy_display` 只用于标注原展示信息，绝不参加支付或账务计算。

订单详情增加 `timeline:[{event_type,occurred_at,summary_code}]`；管理员详情可额外提供 `user_id`、证据可信性、脱敏核实结果、责任人与异常记录。会话 ID、数据库错误、checkout payload 不出现在普通 DTO。`allowed_actions` 不含另一个“购买”或“返回表单”绕过入口；acknowledge 只赋给当前流程的可靠终态或本地已结束／超时结果，不把本地结束当作渠道关闭。

### 4.3 请求表

| 方法与路径 | 请求 | 响应与约束 |
| --- | --- | --- |
| `POST /api/user/topup/current/restore` | `{trade_no?:string}` | WalletState。页面初次恢复／跨设备／明确恢复历史单调用；幂等。已有 flow 一律恢复它，不被参数覆盖；如果指定其他单则 409 CURRENT_FLOW_ACTIVE。无 flow 才原子绑定仍有效可靠候选，本地结束／超时废单和纯历史 review 均不绑定；无候选返回 idle |
| `GET /api/user/topup/current` | 无 | WalletState，只读。GET 不自动创建绑定；初次挂载先 restore，轮询只 GET。错误不能伪装 `order:null/can_create:true` |
| `POST /api/user/topup/quote` | `{payment_provider,payment_method,amount?:string,product_id?:string,waffo_pay_method_type?:string,waffo_pay_method_name?:string}` | `{quote_token,expires_at,expected_payment,pricing_mode,payable_payment,pricing_notice_code,credited_quota,credit_display,payment_provider,payment_method,server_time}`。amount 为有界正整数字符串，与 product_id 互斥；Creem 用产品 ID，Waffo 方式必须来自当前允许目录 |
| `POST /api/user/topup/orders` | Header `Idempotency-Key`；正文 `{quote_token,expected_flow_revision}` | `{wallet:WalletState,order:OrderDTO}`。201 新建并就绪，200 同键重放，202 已持久但创建待确认。不返回 checkout；ready 后调用 resume。先查同键，再检查 quote 期限／flow；order 定位原幂等操作，wallet 始终返回最新当前流程，不建第二笔、不重绑已结束原单 |
| `GET /api/user/topup/orders/:trade_no` | 无 | `{order:OrderDTO+timeline,wallet:WalletState}`。本人精确查询不受 30 天限制；查询历史不绑定／替换当前 |
| `POST /api/user/topup/orders/:trade_no/resume` | `{flow_id,flow_revision}` | `{wallet:WalletState,checkout:CheckoutDTO|null}`。只允许匹配当前且状态／能力允许的原单；状态已变时返回 409 与最新 wallet，checkout null；不创建会话或绑定历史 |
| `POST /api/user/topup/orders/:trade_no/cancel` | `{expected_order_version:string}` | `{wallet:WalletState,order:OrderDTO}`。202 申请已保存；重复请求返回现状；当前之外的本人历史单也可按真实能力取消，不替换 current。最新状态变更先返回冲突刷新，不盲写旧版本 |
| `POST /api/user/topup/orders/:trade_no/reconcile` | `{}` | `{wallet:WalletState,order:OrderDTO}`；202 已排队、200 现有结果。被核实的历史单不替换 wallet.current。限流、合并同单请求，不由每次点击直接开一个无界渠道调用 |
| `POST /api/user/topup/orders/:trade_no/end` | `{expected_order_version:string}` | `{wallet,order}`。本人结束本站操作，历史单不替换 current；已超时废单拒绝。结果保留直到精确确认；当前原单 ended 可由前端在同次操作内串行 acknowledge |
| `POST /api/user/topup/current/acknowledge` | `{flow_id,flow_revision,trade_no}` | WalletState。成功、可靠取消／过期、确认无可付会话的失败及本地 ended/timed_out 允许；原子校验当前身份及实际最新事实，再结束本流程，返回 idle；不创建新单，不删除旧记录 |
| `GET /api/user/topup/self` | 现有 `p/page_size/keyword`；新增 `status_group`、`exact_trade_no`、`scope=wallet\|recent\|unfinished` | 保留旧 PageInfo、今日统计；items 增量带 OrderDTO 信息，新增 `counts`、`query_scope`、`server_time`。六筛选固定 wallet 并集范围；exact_trade_no 与 keyword 互斥且跨全部账龄 |
| `GET /api/user/topup` | 同类筛选，管理员范围 | 全平台聚合；不混入本人 current。旧调用无新参数保持全龄，query_scope=legacy_admin；新六筛选显式 scope=wallet。新增详情 `GET /api/user/topup/orders/:trade_no/admin` |
| `POST /api/user/topup/orders/:trade_no/admin-reconcile` | `{reason:string}` | `{wallet:WalletState,order:AdminOrderDTO}`，wallet 仍是管理员本人状态；202 核实受理，order 为受权目标。AdminAuth + billing:write + 审计，不接受客户端 `paid=true/amount` 作为付款事实 |

本地结束／到期且无已确认收款的废单，resume/cancel/end/reconcile/admin-reconcile、旧 `/api/user/topup/complete` 及人工历史批次均由后端拒绝，不仅隐藏按钮；未知创建时间无实际收款证据时同样无这些人工动作。可信回调与自动收款核实使用独立路径，不受人工门禁阻断。

restore、current、acknowledge 的 data 为 WalletState；orders/cancel/end/reconcile 返回 wallet 与被操作订单，管理员 order 增加授权诊断字段。返回的 wallet 从不变成被管理用户的 current。resume 和 detail 使用各自表内响应。错误附带流程状态时统一 `data:{wallet:WalletState}`，必要时可附受权 order 定位原操作，不得将 checkout 放进错误正文。创建重放例：A 已确认返回且 B 已创建，再用 A 的 key 重试，只返回 `{order:A,wallet:当前B}`；若当前空闲则 wallet idle。禁止返 A 的旧 WalletState、将 A 重绑 current 或返回 A checkout；前端仅在 order 与当前绑定一致且允许 resume 时获取入口。

quote token 使用服务端签名的不可篡改快照，绑定用户／租户、渠道、准确计价／额度、原输入、报价版本和短有效期；不需要为一次报价另建持久订单。创建校验有效性和当下允许购买，配置已变但不能兑现原报价时返回 QUOTE_STALE 重报，不静默改价。Idempotency-Key 为 16—128 位 URL-safe 随机字符串；request_hash 取规范化的业务输入与报价事实，不含 token 签发时间／签名等随机封装，同键异金额／渠道／产品／计价事实必须冲突。重放仍验 token 签名和作用域，但已落库同键不受原 quote 到期影响。

报价和订单的 `pricing_mode` 使用同一语义：fixed 时 `expected_payment=payable_payment`，显示“应付金额”；channel_adjusted 时 expected_payment 是冻结基础订单金额，payable_payment 在渠道认证确认最终应付前为 null，显示“订单金额”与 `pricing_notice_code=CHANNEL_ADJUSTMENTS_APPLY` 对应“最终金额以支付页合法优惠、税费为准”。取得可信最终应付后填写 payable_payment 并显示“应付金额”，不覆盖原基础快照；实收仍由 received_payment 单独表示。unknown 仅旧单缺证据，不允许创建新单。合法变量规则在 pricing_snapshot 冻结，不能接受渠道回报任意较小或较大金额；无法验证变量规则的渠道须先补齐规则再启用，不默认撤销已有优惠功能。金额／额度仍保持设计中的相同信息层次，前端不得按 provider 猜标签。

历史响应为白名单 DTO 合并必要旧字段，禁止直接序列化扩展后的 `TopUp`。**六筛选统一 `scope=wallet`：集合为最近 30 天所有记录 ∪ 全部账龄尚无成功或可靠渠道终态的历史记录**，包括未完成／未定性记录和本地结束／超时废单；废单显示 ended/timed_out 并归 closed，仍可查询。UI 明示范围含保留历史，不能把废单称为未完成。`counts` 键固定 all/pending/processing/review/completed/closed，全部统计同一集合及 keyword 条件；选中组 total 为本组总量，不是当前页条数。scope=unfinished 仅“查看其他未完成”入口使用；recent 保留旧本人接口兼容，新六 tab 不逐个切换 scope。**仅 self** 未显式 scope 且无 status_group 的旧调用仍 recent；旧 admin 无新参数保留全龄 query_scope=legacy_admin，不能缩为 30 天。两者有 status_group 未带 scope 默认 wallet。exact_trade_no 覆盖 scope 时间条件、走全龄唯一索引并返回 query_scope=exact，不能被旧窗口截断。分页最大 100、稳定 `id desc`；带索引的并集/聚合查询，禁止把所有旧单读入内存派生分组，也不能无界 LIKE 扫描。非当前订单和管理员查看他人的详情不返回 acknowledge，即使该记录已经可靠终结；历史“重新充值”应读取本人最新 current，通过既有阶段规则决定下一步。

完整分组固定：creating/payment_processing/cancel_processing/expiry_processing → processing；pending → pending；review_required/credit_review → review；success → completed；cancelled/expired/failed/ended/timed_out → closed；unavailable 仅在 all。success、实际收款或 credit_review 优先于本地结束／期限；未知创建时间且无其他已确认事实为 unavailable。历史原始 failed/expired 缺渠道关闭证据时不能伪造可靠关闭；可按已知本站期限投影 timed_out，否则保留 unavailable／真实 review。

### 4.4 错误语义

| HTTP / code | 含义及客户端行为 |
| --- | --- |
| 400 VALIDATION_FAILED | 格式、额度、互斥字段错误；不能建立远端会话 |
| 401／403 | 认证、scope、origin、租户或渠道权限不足；保留既有安全错误规范 |
| 404 ORDER_NOT_FOUND | 不存在或不属本人／当前租户，避免暴露存在性 |
| 409 CURRENT_FLOW_ACTIVE | 已有流程或需恢复的可靠未完成对象，返回最新 wallet；旧端创建也不能绕过 |
| 409 FLOW_CHANGED / ORDER_CHANGED | 版本或身份已改变；恢复最新状态，不重试旧动作覆盖新流程 |
| 409 IDEMPOTENCY_CONFLICT | 同键请求语义不同，不复用或创建 |
| 409 CLIENT_UPGRADE_REQUIRED | 旧渠道创建请求缺 Idempotency-Key；不发渠道请求，提示升级并返回现有 wallet（若可读取） |
| 409 CREATE_DISABLED / QUOTA_CAPACITY_EXCEEDED | 当前可合法 idle，但在线购买关闭或账户额度容量不足；提供安全禁用原因，不变成有 current 或全钱包故障 |
| 409 QUOTE_EXPIRED / QUOTE_STALE | 尚未创建时重新报价；已有同键创建优先返回原结果 |
| 409 PAYMENT_NOT_RESUMABLE / CANCEL_UNSUPPORTED / RESULT_NOT_FINAL | 状态或能力不允许；返回可靠状态和 reason_code，不伪造渠道取消／解锁 |
| 409 PAYMENT_ATTEMPT_ENDED | 本地结束／到期废单或无法确定有效期限，不接受续付、取消、结束或人工核实／补单；刷新最新状态，当前结果按精确 acknowledge 返回 |
| 429 PAYMENT_CHECK_RATE_LIMITED | `Retry-After`；返回已知结果，不再次请求渠道 |
| 503 PAYMENT_PROVIDER_UNAVAILABLE / WALLET_STATE_UNAVAILABLE | 服务暂不可用；如果已经持久创建意图则返回 202 unknown/current 而非暗示未创建；数据库不可确认时保留原幂等键核实 |

对写请求使用同一幂等键重试与“主动重新购买”必须分开；所有 guard 在服务端，隐藏按钮只是展示规则。

## 5. 流程与并发算法

### 5.1 创建与恢复

1. 验证 scope、幂等键长度／字符、规范化请求摘要；命中同键先返回原单，异请求 409。过期 quote 不影响已提交订单的幂等恢复。
2. 短事务锁定用户作用域 flow；检查当前流程和可靠历史恢复候选、报价、余额容量。创建 TopUp 冻结快照、绑定 flow、写 create claim 和审计，一并提交。任何外部请求必须在此之后。
3. 在事务外调用已验证渠道创建，使用固定 merchant order ID／渠道幂等键。超时、连接中断、响应解析失败都可能已创建：记 unknown、禁止第二次非幂等创建，按原身份查询。渠道支持可靠幂等创建时也只在本站操作仍有效且未结束时重放同一请求；结束或到期的 unknown/creating 不再重放创建，即使后来进入 credit_review。
4. 返回后重新锁订单并核验 claim、版本、关闭意图和会话身份，保存原 session 与 checkout；已被支付、渠道关闭、本地结束或到期时不再交付活跃入口。远端成功、本地保存失败按固定身份核实；有效当前流程内不能另下新单，本地结束／到期并精确确认后方可新建，旧单不再重放创建。
5. create 响应为 `{wallet,order}`，不包含 checkout。浏览器先采用最新 wallet，仅当原操作 order 与当前绑定相符且仍允许 resume 时获取入口；打开新标签失败不改变数据库。原表单重放是否产生同一远端单须渠道验证，尤其易支付商户订单重复提交语义。
6. resume 不读当前价格；校验已保存快照、原商户环境、关闭意图、deadline、token 有效期和 current。可按需要受限查询原 session，不能借“刷新 token”创建第二个 checkout；仅在协议证明换 token 仍指向同一会话时允许重签。

同一请求提交结果未知且客户端不知道 trade_no 时，重试 `orders` 的原幂等键或 `current/restore`。远端可能存在的流程必已先有本地 TopUp；若数据库连本地提交是否成功都不可确认，返回可重试错误而非 idle，客户端不换 key。所有已落库订单都有可查 trade_no。

### 5.2 取消和自动到期

取消短事务保存关闭意图、禁用平台续付并安排 next_check；不在锁内调用渠道。重复取消幂等，`success` 不进入 cancel。返回 202 cancel_processing 后由同一核实服务执行：认证查询 → 已付款走收据；处理中继续确认；已确认未付且支持关单才请求关闭 → 验证目标原 session 和可靠关闭证据 → 条件写 cancelled/expired。

本站截止为创建后 30 分钟，若已知渠道截止更早则取更早值。到期即停止所有平台续付／补单动作，display_status=timed_out、phase=result 并提供 acknowledge，显示“本次充值已超时”与“返回充值”；主动结束为 ended。渠道确认关闭仍独立保留 expired/cancelled 和关闭证据。后台不自动清 flow；本地结束不停止可信回调、原单入账或有界自动核实。

查无订单默认 unknown；只有经验证协议能够证明从未创建才视为无会话。关单请求超时、已受理未完成不能算关单成功。到期后渠道异步付款仍可在后台有界确认，但仅 processing 不能延长本站操作期限；已确认收款／credit_review 不被 deadline 强制覆盖。

核实回包写入前检查原 session、claim 和最新版本。关闭／失败条件更新排除 success、已验证收款待入账和更新的支付事实；迟到成功允许进入可信账务处理。关闭证据已保存后收到第二事件也保留旧时间线。

### 5.3 统一核实与后台任务

- 新单及时核实使用独立 `wallet_topup_reconcile` ScheduledSystemTaskHandler；历史回溯使用 `wallet_topup_history_reconcile`。复用注册、租约、context 取消、进度报告、FinishSystemTask；服从 `CanRunWriteBackgroundJobs` 和现有 leader／角色门禁。
- `EnqueueSystemTask` 每类型只返回一个活动任务，不能把每个点击的订单号放进新任务而假设都会排队。单订单待处理状态先持久在 TopUp 的 next_check/claim 中；现有活动任务和下一周期扫描都可发现它，API 仅唤醒调度。
- 按 `(next_check_at,id)` 或到期索引有限游标批次，领取订单级短租约；渠道请求有 context、timeout、响应体上限和商户速率限制。失去 claim／租约的旧 worker 不能写回结果。
- 初始可采用每批 100、同商户串行／小并发、指数退避 5 秒至 5 分钟、连续 8 次失败按实际状态保留责任并退避；本地已结束／到期且无可信收款的订单停止常规调度，不转成人工补单／核实入口。这些是待验证初值，须适配渠道限额，不能当支付期限；已保存可信收据的补偿独立继续。
- 新收据处理队列从持久 pending receipts 扫描；即使用户关页、callback 已 ACK、任务崩溃或发票功能未启用也有后续执行入口。历史任务使用独立预算，不能饿死新订单。
- 历史任务暂停／续跑按第 8 节专用 run 契约执行；进度游标只在批次结果已持久化后推进。暂停不删除 receipt、不把订单标失败，不停止新单核实和已收款入账。

### 5.4 可信收款与账务

验签／认证查询成功只证明证据来源。入账还必须核验原渠道、商户／环境、order/session、交易唯一性、币种 exponent、准确实收、合法优惠／税费规则和用户／租户归属。Stripe `checkout.completed` 未 paid 只是处理中；合法 promotion code 不等于可接受任意较小金额。Creem paid 类型、Pancake buyer identity、环境和 store/order 绑定均保留。

可信付款收据先持久化；随后由仅服务端验证路径调用的 `FinalizeVerifiedTopUp`（拟新增，接收持久 receipt ID）完成以下单一事务：锁订单与 receipt；验证匹配和未使用；检查信用纪元；锁用户并验证准确正额度／int64 容量；必要发票证据与 hold；订单 success、额度增量、tenant lot 或平台 rebate outbox、receipt credited、审计一并提交。复用现有 FinalizeTopUp 内部结算，不复制其账务算式。

旧手工 `Prepare` 回调不能构造“verified=true”绕过这一入口。普通 FinalizeTopUp 不普遍接受已关闭状态。已成功同单同交易重放返回幂等；同交易跨单不能再入账；同单第二笔付款保存异常而不重复额度、不自动退款。对账异常、额度超限、legacy credit epoch 或用户归属问题均保留已验证收款和处理责任。

ACK 条件：已入账，或已可靠保存证据及待处理责任可由后台继续处理；数据库不可保存时按渠道约定 NACK/5xx 促重试。不得把所有异常吞掉返回 200，也不得因业务异常回滚收据后 ACK。未知事件、验签失败和环境错误分开处理，测试流量不可向生产用户发放额度；测试模式跳过签名不能被钱包可信路径当作生产收款。

易支付已登录 [会员开发文档](https://member.z-pay.cn/member/doc.html) 于 2026-10-23 核对：通知须在 5 秒内返回纯字符串 `success`，重试不保证最终送达。回调应先保存已验签的付款事实，缺少可信付款时间时保留挂账责任，交由既有有界对账任务查询原商户；不得在通知响应中等待可能超出时限的远程查询，或以通知时间伪造付款时间。保存失败仍须非成功响应。

必须同时修：Creem 非 pending 提前 ACK、Waffo 非成功全写 failed、Stripe 失败旧对象 Save 与事件保存失败仍 ACK、各渠道创建不确定被记 failed、Epay 记录完整表单／回调日志、Stripe/Creem/Pancake 明文签名与 body 日志。日志只留 trace、trade_no、渠道、事件种类和脱敏结果。

## 6. 展示状态与当前流程派生

| 已确认事实 | Order display_status / flow phase | 动作 |
| --- | --- | --- |
| 本地已建单，创建尚未确认且仍在期限内 | creating / creating | 按能力 reconcile/end、必要 contact_support；正在提交禁重复动作，无购买／续付／ack |
| 原 session ready、可付款、未截止且未结束／无关闭意图 | pending / order | resume、end、reconcile；渠道可取消时 cancel |
| 渠道支付处理中，未结束且未到期 | payment_processing / processing | reconcile、按能力 end；不续付，不 ack |
| 已申请取消，尚未确认且未本地结束／到期 | cancel_processing / processing | reconcile、按能力 end；不续付，不 ack |
| 本站期限已到，无已确认收款 | timed_out / result | 当前 acknowledge，历史只读／客服；不续付、不补单、不人工 reconcile |
| 用户主动结束本站操作 | ended / result | 当前 acknowledge，历史只读／客服；不续付、不补单、不人工 reconcile |
| 已建立核实责任但无法确认是否收款，仍在期限内且未结束 | review_required / review | reconcile、按能力 end、contact_support；未结束当前仍锁定 |
| 收款可信但账务尚未完成／异常 | credit_review / review | reconcile、contact_support；当前仍锁定 |
| 已完整入账 | success / result | 当前 acknowledge；保留详情、记录 |
| 已证实未付款且原 session 因用户请求关闭 | cancelled / result | 当前 acknowledge |
| 到期已证实未付款且原 session 关闭／到期 | expired / result | 当前 acknowledge，保留已验证的渠道到期／关闭事实；不与本地超时混同 |
| 创建确定未发生或可靠支付失败且无可付 session | failed / result | 当前 acknowledge |
| 存储状态无法解释／后端协议不兼容 | unavailable / review | 安全刷新／客服，不能编造已建立核实责任或解锁 |

创建进行中使用 `reason_code=CREATE_IN_PROGRESS`；创建不确定使用 `CREATE_RESULT_UNKNOWN`，短时核实仍是 creating。超出有限核实预算并建立责任后可转 review_required / review；无已确认收款的本地结束／到期仍优先投影 ended/timed_out，不能以 review 延长期限或复用 Pending。`flow.phase` 是页面阶段，`display_status` 是订单展示事实，两者不可混用；例如 expired / result 仍不能直接购买。

`phase=result` 不等于 can_create。`can_create=true` 必须是 idle、没有活动流程和待恢复的可靠可处理对象，并通过服务端购买规则；反向不成立，合法 idle 也可能 can_create=false。`create_block_reason_code` 固定为 null、CURRENT_FLOW_ACTIVE、RECOVERABLE_ORDER_EXISTS、PAYMENT_DISABLED、CHANNEL_UNAVAILABLE 或 QUOTA_CAPACITY_EXCEEDED，由后端给出最直接原因；idle 禁用在线购买仍可读取余额与记录，不冒充恢复失败，不改变兑换码／订阅等独立能力。DB／认证失败是 HTTP 错误，不伪造 idle。`other_unfinished_count` 按用户＋租户统计其他 pending、processing、review、credit_review 和尚未定性的记录，不能叫“可付款数量”，不含成功、可靠关闭及无已确认收款的本地结束／到期废单，不取全平台或原始 pending 总数。

`acknowledge` 在同事务中锁 flow 和对应 TopUp，再检查实时可结束事实。主动 end 的前端串行确认仍使用同一精确契约；超时、已到账、credit_review、身份变化或另一当前 flow 不自动确认。迟到关闭响应或旧客户端 ack 与新创建竞争，由 flow id/revision 和条件更新保证旧响应不能释放新流程。用户只是看历史或关闭记录不调用 ack。

## 7. 渠道验证清单与未决边界

| 渠道 | 当前可复用 | 开放前必须取得的证据 |
| --- | --- | --- |
| 易支付（微信／支付宝） | go-epay v0.0.5 表单签名／验签；发票侧 zpayz 认证查询传输 | 实际网关是否同协议；pending/处理中/关闭响应的语义；查无单何时可靠；同商户订单重复表单是否幂等；关单接口／认证方式；原页面是否失效；实际期限；原商户凭据版本 |
| Stripe | stripe-go v81.4.0 checkout SDK、webhook 验签、异步成功入口 | session retrieve/expire 的账号范围与失败语义；price/quantity/currency/税费／promotion 规则快照；payment intent/charge 唯一收款标识；异步处理中不能提前 expire；创建幂等键保留范围及 actual expires_at |
| Waffo | waffo-go v1.3.2 Create、验签；merchantOrderID/paymentRequestID | Inquiry/Cancel 所需标识、签名、完整中间／终态表、结算金额／税费和交易标识、真实截止及重复创建语义。源码方法存在不证明商户已可用 |
| Pancake | v0.3.1 Authenticated checkout、session/order/token/期限、buyer identity、prod/test 路由 | 原 session query、可靠关单；BuyerSession 取消所需 buyer token 与 order ID，不得拿 checkout token 充当商户关单凭据；token 更新是否同会话；USD/税费快照；实际 45 分钟行为和 completed 金额语义 |
| Creem | HTTP checkout、HMAC、request_id、产品数据 | checkout 查询／关闭／有效期、request_id 是否幂等、原产品价税与实收规则、transaction 唯一标识、测试／生产身份；不能从产品配置 Price 推断真实已收款 |

需留受控脱敏的 request/response fixture、官方协议版本、测试账号／环境、可重复结果及限制；没有证据的能力返回 false，不能先展示取消或自动关单。本站操作期限统一为 30 分钟，可独立于服务商期限生效；Pancake 等渠道期限仍保存原事实，不把本站超时伪装成渠道关闭。

原商户身份已可保存，但**凭据历史版本解析尚未在现有钱包建立**。最低交付是可稳定解析原账号的版本引用、轮换保留窗口与服务端受限读取；禁止轮换后自动使用新商户查旧单。密钥缺失则 review，不凭空断言订单不存在。checkout/token 需服务端加密存储或证明等价的现有秘密存储措施；可用标准库 AES-GCM 与带版本密钥环，AAD 绑定 tenant/user/trade/account，不能复用发票私有 key 名或引入其可选 schema 依赖。密钥丢失／轮换和恢复必须有针对性测试；不把秘密发到日志、列表或 localStorage。

## 8. 存量、兼容和迁移

先只读 dry-run：按渠道、商户／环境可识别性、账龄、快照／会话可恢复性、credit epoch 分组，报告可自动核实、需补证、不可安全推断的数量。快照缺失保持 null，不用现配置填旧数据；创建时间旧不是渠道关闭证据，但已知创建时间的旧 pending 按本站期限即时投影 timed_out；未知创建时间保持 unavailable，不补造期限。

历史人工处理须单独授权并限定范围／批量／速率；报告、选单及执行前重读均跳过本地结束／超时废单，不能借批次恢复人工补单／核实入口。非废单复用同一认证查询和收据入账；已付款未入账进入原账务规则，无法入账保存异常；可靠未付关闭才写终态；有效范围内的未知写实际 review 及负责人，不以此延长本站期限；可信晚款和持久收据补偿独立继续。不删除订单、不自动退款、不清理迟到回执入口。精确本人查询和未处理入口不限 30 天。

历史批次采用专用窄 API，不扩通用任务控制框架。新增 `wallet_topup_reconcile_runs` 保存 run_id、只读报告引用、不可变 scope/digest、max_order_id 边界、cursor、控制 version、desired_state、执行状态、owner、授权引用、累计结果与时间；SystemTask 负责每段有限执行。暂停／恢复是业务 run 状态，通用 task 仍仅 pending/running/succeeded/failed；一段任务完成不等于整个 run 完成。报告最多覆盖显式 provider/account/env/创建区间/tenant 范围，自动冻结 max_order_id，scope 不能用空对象代表无界全部。

| 历史批次专用接口 | 输入与结果 |
| --- | --- |
| `POST /api/user/topup/reconcile-runs/report` | `{scope:{payment_provider,provider_account_ref,provider_environment,tenant_id,created_before,created_after?},batch_size,max_queries_per_second}`；只读业务数据，可保存报告元数据；返回 `{report_id,scope_digest,max_order_id,counts,capability_gaps,expires_at}` |
| `POST /api/user/topup/reconcile-runs` | `{report_id,scope_digest,authorization_ref,reason}` 加 Idempotency-Key；冻结原报告范围，返回 RunDTO 和已排队 task_id；报告失效／digest 不符不启动 |
| `GET /api/user/topup/reconcile-runs/:run_id` | RunDTO：`{run_id,scope_digest,control_version,status,desired_state,cursor,counts,last_task_id,owner_id,updated_at}`；control_version 为十进制字符串；counts.reviewed 为已审总数，credited/closed/review_required/retry_failed 是互斥归类，四项之和等于 reviewed |
| `POST /api/user/topup/reconcile-runs/:run_id/pause` | `{expected_control_version,reason}`；持久 desired_state=paused、version递增，返回 pausing 或 paused；重复同意图幂等，版本冲突 409 RUN_CHANGED |
| `POST /api/user/topup/reconcile-runs/:run_id/resume` | `{expected_control_version,scope_digest,authorization_ref}`；仅原冻结范围和原游标继续，返回 queued；scope 改变必须重新 report＋授权，不能扩大旧 run |

report/start 的数量、时间区间、batch_size 和 max_queries_per_second 由服务端校验并施加有界上限；不得直接信任客户端数值，本规格不预定未经压测的吞吐值。

以上仅平台 RootAuth、交互式 session、origin 检查与审计，scoped token 默认拒绝；普通用户／租户主／普通管理员单笔核实权限不包含批量。start/resume 必须引用真实独立的数据处理授权记录，不得由程序伪造 authorization_ref；设计／部署授权不是历史处理授权。若授权记录系统未接入，运行册由获授权操作者人工核对引用及范围，技术系统记录操作者而不声称引用字符串自身证明批准。

每次远端请求前后检查 run control_version/desired_state 与订单 claim；暂停后在途请求可保存可信收款事实但不得启动下一次查询或推进旧 run 游标。事务中以 run control_version CAS 更新 checkpoint，旧 worker 失去控制权即停止；新的 pause 控制段在在途释放后显示 paused。receipt 处理与正常新单任务不依赖历史 run 是否暂停，恢复时重查游标前未确认批次也必须幂等。RunDTO 的 business status 明确区分 report_only/queued/running/pausing/paused/completed/failed；不要把 SystemTask succeeded 当 completed。控制接口及业务报告在现有管理员任务区最小接入，功能不得以“框架没有暂停”而省略。

DDL 使用 bigint、varchar、TEXT、可移植唯一约束，兼容 SQLite、MySQL ≥5.7.8、PostgreSQL ≥9.6；无 JSONB、部分唯一索引、SKIP LOCKED 或数据库特有布尔 default 假设。索引至少覆盖：scope 当前流程、scope 幂等键、provider account/session（可靠存在时唯一）、receipt key、scope 状态/创建时间、next_check/id、receipt processing/next_retry。迁移前对已有重复数据只读报告，不在 DDL 中隐式销毁冲突。

新增扩展登记到 `pkg/kkaimigrate`，显式更新当前及回退 runtime contract。当前本地 feature contract 支持范围涉及可选发票扩展；不能据此预定下一个版本号或断言生产已有该 schema。精确版本、digest、扩展 receipts 和运行镜像契约在另行获授权实施／迁移时确定；不改已发布 v17 或其他历史迁移，不由应用启动安装。

兼容交付顺序：

1. 旧 frontend 增量理解 failed／unknown／新状态；后端先具备可信收据、迟到款安全和新字段读取兼容，取消／过期仍不启用。
2. 旧 5 个创建接口接同一 guard。生命周期启用后，**缺 Idempotency-Key 统一 409 CLIENT_UPGRADE_REQUIRED，不创建会话**；不是按同金额找单。带 key 的旧路径对原有输入规范化为同一内部创建请求并生成服务端报价，先查原幂等记录再报价；复用 key 但内容不同仍 409。旧 response 外壳仅保持兼容，不承诺未升级旧端完整恢复 UI，先升级对应前端再启用强制门禁。旧 `/topup/complete` 统一改为有审计原因的认证核实适配；缺 reason 用固定“旧管理入口请求核实”审计，不再直接 ManualCompleteTopUp，无凭据不得完成支付。
3. 新后端／前端契约、当前流程、原 session 恢复验收通过，先启用本站期限与本地结束，再逐已验证渠道启用可靠取消／关单；本站超时不以服务商关单能力为前提。回退基线同样能处理已经产生的新状态与迟到款；关功能开关并不能让旧代码安全。
4. 最后执行另行授权的历史批次。应用发布、schema 迁移、历史业务数据处理是三项独立操作，本开发文档不授权执行。

会员／订阅共用回调分流和部分 TopUp 镜像必须保留顺序。统一钱包查询／恢复排除非余额充值镜像，不让 `MEMBER-` 或订阅支付通过钱包的新 flow 重新执行。实施者须读 `membership_payment.go` 与各 subscription callback 的直接调用点并添加边界回归，不扩展其取消／会话功能。

## 9. 最小实施顺序与验收

| 顺序 | 交付 | 必过检查 |
| --- | --- | --- |
| B0 | 实际渠道能力证据；DTO、schema 扩展及凭据版本方案固化 | 每渠道可查／可关／可恢复／期限明确，未证实能力关闭 |
| B1 | 收据持久化、可信完成入口、回调分流与并发修复、旧补单边界 | 重复、迟到、跨单凭据、收据保存失败／账务失败和发票／租户／返佣回归 |
| B2 | 当前流程、quote、幂等创建、原 session 持久化与 resume、所有旧创建 guard | 重复创建只有一 session；跨设备恢复；有效未知流程不绕过，本地结束／到期后精确返回；旧端绕过被拒 |
| B3 | end、本站到期、cancel/reconcile 与有界自动任务；acknowledge | 本地结束／到期即 result，渠道关闭独立；精确 ack 才回 idle；废单无人工入口，晚款只更新原单 |
| B4 | 历史筛选／详情／权限／待核实管理，UI 全旅程联调 | A01—A32 相关 API 和当前流程状态完整；不偏离 UI |
| B5 | 三数据库与回退兼容验证；历史 dry-run 工具 | 精确 schema／runtime 可读，受控批次可续跑，未授权不写历史 |

测试使用现有 testify require/assert、显式 fixture 和可注入 fake provider／时钟。测试业务不变量，不以固定列名、SQL 文本或大量随机循环冒充覆盖。网络 timeout 用确定故障 fixture，不靠 sleeps 模拟并发；竞争用屏障协调真实数据库事务。

必须新增的最小定向用例：

- 创建同键同输入／同键异输入；两设备不同 key 同时购买；本地保存失败前后远端请求次数；创建超时后查询找到原单；恢复不会调用 create；原会话 token 更新不换 session。
- flow restore 不被历史 query 覆盖；纯旧 review 不误锁；已确认收款的当前 review 不释放，未收款可本地结束／到期；结果刷新／重登仍 result；ack 双击／旧 revision／新流程竞争；所有旧 pay 入口 guard 一致。
- 取消与成功、过期与成功、两个 worker、callback 与人工核实跨实例竞争；成功不被覆盖、额度／lot／outbox 一次；只到本站截止投影 timed_out/result，不伪造渠道 expired；废单无续付／取消／人工核实／补单，unknown 创建不再重放；A 晚款不能清 B。
- Epay 签名和商户绑定、Stripe unpaid/异步成功/合法优惠、Waffo 中间态、Creem late paid、Pancake 环境/buyer/session；金额币种／商户 mismatch、event replay、交易跨单复用、同单第二付款。
- 凭据先保存后账务失败仍可重试；额度溢出、credit epoch、发票 partial schema/hold、租户 lot/outbox 故障全部不丢收据；未写成功时 webhook 不 ACK。
- 本人和管理员范围、其他租户／user IDOR、access-token scopes、tenant allowlist、Origin；checkout 不出列表／日志；未知字段不回 pending；30 天外本人精确查单。
- 任务租约丢失、重启、暂停续跑、同类型任务已活动但新订单仍被扫描、历史积压与新单隔离；订单与收据 claim 被旧 worker 覆盖必须失败。
- 三数据库实际唯一约束／并发行为、空旧 key 与扩展兼容、当前和回退 reader；已有会员／订阅 callback dispatch 不被钱包改变。

现有直接回归资产：`model/topup_finalize_test.go`、`model/payment_method_guard_test.go`、`model/reseller_topup_test.go`、`model/invoice_payment_receipt_test.go`、`controller/topup_quota_limit_test.go`、`controller/topup_stripe_reliability_test.go`、`controller/topup_epay_signature_test.go`、`controller/payment_webhook_availability_test.go`、`service/invoice_payment_reconcile_test.go`、`service/waffo_pancake_resolution_test.go`。原测试中“非 pending 一律不能入账”只保留在旧无凭据入口，新增可信迟到款场景不能靠删除保护来通过。

实施后定向命令模板（**本轮未执行**，新测试名须与下列前缀一致或在 PR 更新准确表达式）：

```sh
go test ./model -run 'Test(WalletTopUp|WalletPaymentReceipt|FinalizeTopUp|RechargeEpay|ResellerTopUp|InvoiceTopUp)' -count=1
go test ./controller -run 'Test(WalletTopUp|StripeSession|TopUpQuota|ValidateTopUpQuota|ValidateCreditedQuota|RequestAmountRejects|Epay|.*WebhookEnabled)' -count=1
go test ./service -run 'Test(WalletTopUp|InvoiceEpayMerchantQuery|InvoicePaymentReconciliation|ResolveWaffoPancakeTradeNo)' -count=1
go test ./middleware -run 'Test(WalletTopUp|TenantFeatureGate|TenantDashboardOwner|AccessToken)' -count=1
```

三数据库迁移及并发命令应复用仓库受影响的 `pkg/kkaimigrate`、model 和数据库集成脚本，PR 必须列出精确 fixture／DSN 环境和运行命令，不把 SQLite 单元测试当作跨数据库证明。涉及运行时间未知、预计超过 5 分钟或全套命令时按 AGENTS 先说明并取得批准；定向通过后不自动跑全仓 build/test。

## 10. 本文档验证记录

本轮只读审阅上述源码与三份设计文档；未调用渠道、查询生产、修改业务代码、切换分支、执行迁移或处理订单。

文档检查命令：

```sh
git diff --no-index --check /dev/null docs/kkai/wallet-payment-backend-dev-spec.md
```

结果：无空白错误输出；新文件相对于 `/dev/null` 存在差异，故退出码为 1。另以 Python 检查结尾换行、行尾空白、代码围栏配对及三份相对设计链接存在，均通过。

本轮未运行应用测试、数据库集成或全量测试。真实网关协议、原商户凭据版本、schema 版本／生产契约与跨数据库并发仍是实施验收输入，不因本规格或原型通过而视为已验证。

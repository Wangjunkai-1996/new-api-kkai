# KK 商城后端与支付开发规格

版本：D2；日期：2026-10-28。状态：Go 模型、服务、控制器、路由、后台恢复任务及 schema 19 已在工作区实现。未接触生产、启用渠道或执行生产迁移；精确验证范围见[实施状态](./kk-shop-implementation-status.md)。D1 源码核对基线 `4a8631002` 保留为历史。

上位文档：[PRD](./kk-shop-prd.md)、[业务规则](./kk-shop-business-rules.md)、[实施方案](./kk-shop-implementation-plan.md)。接口与错误码以 [API 契约](./kk-shop-api-contract.md) 为唯一来源；本文规定持久化、事务、恢复和接入方法。V2 HTML 中的场景名、演示订单号、金额和日期均不得直接成为后端默认值。

## 1. 实施边界与已经核实的源码

在现有 Go/Gin/GORM 分层中新增商城模块，同库同进程，不引入第二套登录、消息系统、钱包、通用工作流或仓储接口框架。每商品一个规格、每单一件、一个待核实的首发渠道和币种；自动与人工交付、售后、人工原路退款登记均属于首版。

| 现有源码 | 已有事实 | 商城接入决定 |
| --- | --- | --- |
| `router/wallet_payment.go` | 当前生命周期接口为 `/user/topup`，调用钱包控制器 | 新增独立 `/api/shop`、`/api/admin/shop` 路由，不把商城订单送进充值接口 |
| `service/wallet_payment_gateway.go` | `CreateWalletGatewaySession` 接收 `*model.TopUp`；Stripe 幂等前缀为 `wallet-topup-`；易支付/Waffo 使用 `Wallet recharge`；Pancake 使用 `saas` 税类 | 选定首发渠道后，只提取该渠道需要的中性调用参数和返回结果；不能伪造 TopUp 复用入口，也不能照搬充值商品、税类、促销设置 |
| `service/wallet_payment_webhook.go` | `WalletGatewayForOrder` 查 TopUp，解密原商户快照；易支付按 `WT` 定位，再验签和核对原单 | 可复用 SDK、原凭据快照和身份核验方式；商城拥有独立尝试、验真入口、回执与核销器 |
| `model/wallet_payment.go` | `CreateWalletTopUp` 要求有效 `CreditedQuota` 等充值参数；有创建租约、未知结果与版本 | 借鉴可靠性语义，不给商品款设置充值额度 |
| `model/wallet_payment_receipt.go` | 先保存可信实收、以渠道/商户/环境/交易号摘要去重，再单独结算；冲突不覆盖原事实 | 商城采用相同两段提交原则，独立表与恢复责任 |
| `model/topup_finalize.go` | `completeLockedTopUpTx` 更新 `user.quota`，可能创建分销资金和充值返佣 outbox | 商城绝不调用该核销器、`FinalizeVerifiedWalletTopUp` 或相关余额/返佣入账函数 |
| `service/kkai_outbox.go`、`model/task_accounting_outbox.go` | 已有事务 outbox、唯一事件键、重试/死信；model 可直接写 `KKAIOutboxEvent` | 复用持久队列；model 不反向 import service，事务内按现有 model 模式写事件 |
| `background_jobs.go`、`service/background_job_leader.go` | 写任务经统一注册、leader lease 管理；钱包收款另有 `wallet_payment_receipt_retry` SystemTask | 接入现有注册/leader，不另起无限 goroutine 或进程内定时器承担资金恢复 |
| `service/wallet_payment_secrets.go` | AES-GCM、AAD、密钥版本及旧密钥读取已有实现 | 复用算法和封装方式，商城使用独立用途和密钥域，不直接把凭据塞进钱包 checkout payload |
| `middleware/auth.go` | `UserAuth`/`AdminAuth` 可能接受 PAT；`GetSessionAuthIdentity` 可辨真实会话 | 买家与管理员均额外要求真实 session；不接受 PAT/API Key |
| `middleware/wallet_payment.go` | `WalletPaymentMutationGuard` 允许 PAT 绕过 session Origin 检查 | 不直接复用该 guard；商城写操作使用真实 session 与同源校验 |
| `middleware/secure_verification.go` | `SecureVerificationRequired` 绑定 channel ID；`RequireSecurityProof` 支持细分 operation | 新增商城 operation scope/context；先经过 session 限制，再用通用 proof 能力，不能挂渠道专用中间件 |
| `model/locking.go` | `lockForUpdate` 对 MySQL/PostgreSQL 加行锁，对 SQLite 跳过 | 所有商城行锁调用该 helper，并补充真实写栅栏、CAS 和唯一约束 |
| `model/user.go`、`model/tenant_consumption.go` | 软删/硬删都调用 `checkTenantUserDeletionTx`，但该函数对主站直接放行 | 修改共同删除边界，新增商城责任检查并与下单序列化 |
| `pkg/kkaimigrate/*`、`service/wallet_payment_schema.go` | 迁移有 canonical ledger、checksum、runtime 兼容区间和功能门禁 | 商城采用受控扩展，不能由启动 AutoMigrate 安装；不能只检测表存在 |

源码存在 Stripe、易支付、Creem、Waffo、Waffo Pancake 分支不等于商户已启用。首发渠道要先提交能力记录：允许的商品、商户和环境、金额精度、可信查询及原单映射、创建幂等、未知结果恢复、关闭/过期语义、人工退款核实方式。缺失则只做本地或隔离测试，不收真实款。

## 2. 代码布局与接口边界

建议文件按稳定业务职责拆分，不要求一次创建所有空文件：

| 层 | 建议落点 | 责任 |
| --- | --- | --- |
| Router / middleware | `router/shop.go`、`middleware/shop.go` | 主站、session、Origin、限流、缓存策略；注册 API 契约中的路径 |
| Controller | `controller/shop_catalog.go`、`shop_order.go`、`shop_admin.go`、`shop_payment.go` | 解码、结构化校验、请求身份、DTO 映射；不执行支付/库存 SQL |
| Service | `service/shop_payment.go`、`shop_secrets.go`、`shop_jobs.go`、`shop_schema.go` | 渠道网络调用、加解密、任务调度、schema 门禁；网络调用在业务事务之外 |
| Model | `model/shop.go`、`shop_order.go`、`shop_payment.go`、`shop_fulfillment.go`、`shop_aftersale.go` | 表结构、跨表原子命令、锁与条件更新、审计及事务 outbox |
| DTO | `dto/shop.go` 或按买家/管理分成两个文件 | 请求与响应白名单；数据库密文字段永不自动序列化给客户端 |
| Schema | `pkg/kkaimigrate/schema_shop.go` 及对应测试；`cmd/kkai-migrate/shop_migration.go` 受控入口 | schema 19、16 张表、三方言校验、canonical receipt、就绪检查；普通 target19 只读，生产 apply 依专用 root proof 与独立授权 |

只提取会被充值和商城共同使用的首发渠道函数，输入包括本地交易号、描述、金额币种、原商户凭据快照、回调/返回地址和业务归属。若仅一个渠道需要适配，使用具体函数和显式分支；不提前造插件注册器、通用支付基类或为所有渠道同步改造。

所有 JSON 使用 `common.Marshal/Unmarshal/DecodeJson`。交易金额使用 `int64` 最小货币单位，拒绝非法/越界输入；不使用 float64、quota 换算或饱和裁剪后继续收款。公开金额为 `{amount_minor: "3900", currency: "CNY", exponent: 2}` 这类结构（仅格式示例），公开 ID、行版本为字符串，时间为 UTC RFC3339。数据库绝对时间沿用 int64 UTC Unix 秒；商品时区与自然/工作时间口径独立保存，不能把本地显示字符串作为时间事实。

## 3. 数据模型：字段、索引与不可变边界

以下为拟新增模型，尚无对应表。统一内部主键 `id int64`，GORM 负责生成；公开 ID 用 `crypto/rand` 生成足够随机的固定长度字符串，不暴露递增主键。公开 ID 唯一，所有归属通过服务端确定的 `tenant_id` 和 `user_id` 查询，首版仅允许 `model.PlatformTenantID`。

可修改行带 `version int64 NOT NULL`，初始化为 1；成功业务修改原子 `version = version + 1`。创建/更新时间为 UTC 秒。JSON 快照、受限文字和加密封包存 `TEXT`；不用数据库 JSON 查询、原生 ENUM、部分唯一索引或 MySQL 8 专有能力。业务布尔默认由构造/命令设置，不加 `default:true`。外键若安装应使用 RESTRICT/NO ACTION，禁止交易引用级联删除；没有外键时仍在模型命令中校验关系。

### 3.1 `shop_settings` 与 `shop_user_states`

`shop_settings` 仅一个已知配置行：`id`、`sales_enabled`、`version`、`provider`、`provider_account_ref`、`environment`、`currency`、`currency_exponent`、`min_amount_minor`、`max_amount_minor`、`reservation_seconds`、`max_unpaid_orders`、`policy_version`、`reminder_policy_json`、`retention_policy_json`、`updated_at`。后两项为 API 中提醒阈值/资料保留政策的固定有版本结构，不接受任意执行表达式。不存商户秘密；未确定的经营字段不产生可用默认。关闭 sales 只停止新订单/新支付尝试，存量核实、交付与售后继续运行。

配置结构实施时固定为有类型 DTO：提醒包含付款未知、交付异常、退款待核实三个正数秒阈值；低库存阈值是商品字段。保留包含 `secret_after_delivery_seconds`、`buyer_input_after_closure_seconds`、`evidence_after_settlement_seconds`、`audit_after_settlement_seconds`、`idempotency_after_closure_seconds`，实际数值上线前确定且不能自动缩短已成交查看窗口。交易身份/金额与去重 tombstone 不混入秘密清理；交易留存另由批准的政策处理，首版无交易物理删除 API。

`shop_user_states`：`user_id` 唯一、`tenant_id`、`version`、`retired_at`、`updated_at`。它是下单/创建新责任/删除账户的稳定写栅栏，不保存余额，不复制全部账户资料。删除用户后保留这条非登录身份的 tombstone，历史交易和晚到资金证据仍能定位原用户；禁止复用该 user ID 给新账户。

### 3.2 `shop_products`

- 字段：`public_id`、`tenant_id`、`status`（`draft/published/unpublished/archived`）、`name`、`category_id`、`sort_order`、`cover_ref`、`summary`、`specification`、`product_kind`、`delivery_type`、`price_minor`、`currency`、`currency_exponent`、`terms_version`、`terms_json`、`inventory_identity_domain`、`identity_normalization_version`、`manual_capacity`、`manual_used`、`low_stock_threshold`、`version`、时间。
- `terms_json` 使用 API 第 4.1 节的 `delivery_policy/view_policy/warranty_policy/refund_policy/entitlement_policy` 固定结构，另含交付字段模板/说明、使用/恢复/改绑限制及至多 3 个固定资料字段。合同性字段变化递增 `terms_version`；所有修改递增商品 `version`。公开商品内容按数据展示，禁止执行任意 HTML/脚本。
- 唯一：`public_id`；索引：`(tenant_id,status,sort_order,id)`。分类引用下述单层分类表，不建多级分类系统。名称搜索使用有界关键词和分页，禁止无界全表返回。
- `manual_used` 是有限容量计数，不是已售数量；只能由订单占用/释放事务修改。商品统计的自动库存从库存状态聚合，不存可被运营覆盖的“可售数量”。
- 已有关联库存/订单后不得切换交付类型或身份规范化域；改造商品应新建。历史关联商品不物理删除。`unpublished` 表示暂时下架，`archived` 表示运营归档；两者都停止新购，均不影响历史履约。

`shop_categories` 是支持 API 中单层分类增改的最小表：`id,public_id,tenant_id,name,sort_order,version,created_at,updated_at`；public_id 唯一，索引 `(tenant_id,sort_order,id)`，有关联商品时不物理删除，不增加父级、动态规则或独立分类权限。

### 3.3 `shop_orders`

- 身份/快照：`public_id`、`tenant_id`、`user_id`、`product_id`、`product_version`、`snapshot_version`、`snapshot_json`、`snapshot_hash`、`terms_version`、`policy_algorithm_version`、`quantity=1`、`amount_minor`、`currency`、`currency_exponent`、`terms_accepted_at`。
- 独立事实：`purchase_state`、`payment_state`、`delivery_state`、`effective_receipt_id NULL`、`current_delivery_id NULL`、`active_aftersale_id NULL`、`active_attempt_id NULL`、`reservation_expires_at`、`reserved_stock_id NULL`、`manual_capacity_held`、`paid_at NULL`、`closed_at NULL`、`closed_reason`。
- 人工交付：`buyer_input_ciphertext`、`buyer_input_key_version`、`input_complete_at NULL`（首次齐全，不反复覆盖）、`sla_started_at NULL`、`original_delivery_due_at NULL`、`delivery_due_at NULL`、`sla_pause_started_at NULL`、`sla_paused_seconds`、`processing_owner_id NULL`、`exception_code`、`next_action`。无必填资料的商品在下单时即记资料齐全，不能因 NULL 而永远不起算。
- 风险：`risk_hold`、`risk_reason_code`、`risk_public_note`、`risk_evidence_ref`、`risk_actor_id`、`risk_changed_at`。每次变更另写不可变事件，内部原因不能混入公开说明。
- 唯一：`public_id`、`effective_receipt_id`（可空）；索引：`(tenant_id,user_id,created_at,id)`、`(tenant_id,delivery_state,delivery_due_at,id)`、`(purchase_state,reservation_expires_at,id)`、`(product_id,manual_capacity_held)`。
- 名称、价格及条款快照创建后不可改；补发和退款不重写。`closed` 是本地购买生命周期，后来收到匹配实收可同时为 `closed + paid`，前台主状态优先显示资金/交付事实。

### 3.4 `shop_stock_units`

- 字段：`public_id`、`tenant_id`、`product_id`、`state`、`secret_ciphertext`、`secret_key_version`、`fingerprint_key_version`、`identity_fingerprint`、`identity_domain_hash`、`import_batch_public_id`、`normalization_version`、`masked_label`、`reserved_order_id NULL`、`reservation_expires_at NULL`、`sold_order_id NULL`、`revealed_at NULL`、`quarantine_reason`、`secret_purged_at NULL`、`version`、时间。
- 唯一：`public_id`、`(identity_domain_hash,fingerprint_key_version,identity_fingerprint)`、`reserved_order_id`（可空，首版每单仅一份当前预留）。指纹与域摘要 `VARCHAR(64)`、指纹 key version `VARCHAR(32)`，这一复合键按 utf8mb4 最多 640 字节，避免越过 MySQL 5.7 的较小索引字节限制。三数据库均以 NULL 表示没有归属，不能用空串/0 假装可空唯一值。
- 索引：`(tenant_id,product_id,state,id)`、`(state,reservation_expires_at,id)`、`sold_order_id`、`import_batch_public_id`。长外部标识不直接拼入复合索引；摘要均固定 64 个十六进制字符，实施时计算 MySQL 5.7 utf8mb4 字节预算并验证实际索引。
- `sold_order_id` 一旦确定不改；售出后仅可隔离、保留或按批准政策销毁密文。转 sold、释放或隔离均在同一事务清空 `reserved_order_id/reservation_expires_at` 及订单仍指向本行的 `reserved_stock_id`；历史预留写事件，售出归属保留 `sold_order_id`。否则旧唯一预留会阻止本单获准补发预留新库存。到期释放只更新仍属于同一订单的 `reserved` 行。退款、查看到期和补发都不能把旧库存改回 `available`。

### 3.5 `shop_payment_attempts`

- 字段：`public_id`、`order_id`、`tenant_id`、`user_id`、`attempt_no`、`trade_no`、`provider`、`provider_account_ref`、`environment`、`expected_amount_minor`、`currency`、`currency_exponent`、`business_kind="shop"`、`request_hash`、`provider_snapshot_ciphertext`、`credential_key_version`。
- 网络过程：`create_state`、`provider_session_id`、`provider_order_id`、`checkout_ciphertext`、`provider_expires_at NULL`、`close_state`、`close_evidence_ref`、`claim_token`、`claim_until`、`next_check_at`、`last_checked_at`、`check_attempts`、`last_reason_code`、`review_owner_id NULL`、`version`、时间。
- 唯一：`public_id`、`trade_no`、`(order_id,attempt_no)`；索引：`(create_state,next_check_at,id)`、`(provider,provider_account_ref,environment)`。交易号固定商城命名空间，满足首发渠道长度限制；前缀本身不是真实性或业务归属凭证。
- 金额、商户、环境、本地交易号、原配置快照创建后不可改；更新配置只影响新尝试。旧密钥仍可读才可关闭旧配置。原入口和渠道 session 要保留/加密，响应仅返回当前用户可用且通过 URL 白名单的必要入口。

### 3.6 `shop_payment_receipts`

- 不可变实收：`public_id`、`receipt_key`、`provider`、`provider_account_ref`、`environment`、`provider_transaction_id`、`original_trade_ref`、`amount_minor`、`currency`、`currency_exponent`、`provider_paid_at NULL`、`received_at`、`verification_source`、`evidence_ref`、`evidence_hash`。
- 可恢复处理：`attempt_id NULL`、`order_id NULL`、`candidate_order_ref`、`processing_state`、`exception_code`、`review_owner_id NULL`、`next_action`、`next_retry_at`、`last_processed_at`、`version`。候选映射不等于已认定归属；保存渠道提供的原始引用，与恢复后的映射分别留痕。
- 唯一：`public_id`、`receipt_key`。`receipt_key=SHA256(common.Marshal([provider,merchant,environment,provider_transaction_id]))`，采用固定 tuple 编码；规范化仅限渠道定义的字段，不能随意把区分大小写的交易号小写。
- 索引：`(processing_state,next_retry_at,id)`、`order_id`、`attempt_id`。按身份查询先算摘要再精确比对四元组；重复 key 必须比对全部已接收金额/币种/身份，冲突只追加证据事件，不能覆盖原收款。
- 一笔实收只绑定一个原支付尝试与订单；绑定后不可跨用户改绑。零/负数金额拒绝进入“实收”；首版不处理充值式免单促销。可信回调缺渠道支付时间时可以先存收款，保持待核实，由原渠道查询补齐；不以服务器接收时间伪造支付时间。

### 3.7 `shop_deliveries`

- 字段：`public_id`、`order_id`、`version_no`、`aftersale_id NULL`、`stock_unit_id NULL`、`manual_ciphertext`、`secret_key_version`、`delivered_at`、`view_expires_at`、`reveal_revoked_at NULL`、`reveal_revoked_reason`、`creator_id`、`first_revealed_at NULL`、`secret_purged_at NULL`。
- 唯一：`public_id`、`(order_id,version_no)`、`stock_unit_id`（可空）、`aftersale_id`（可空，首版一次售后最多一次补发）。首次交付固定版本 1；库存和人工密文二选一，不双存秘密。API 的交付 instructions 应作为不可变交付字段保存；仅非秘密说明可进入详情白名单。
- 交付后归属、正文和截止时间不可覆盖；重加密允许只更换密文封包/密钥版本，明文与 AAD 身份不变且有维护审计。订单 `current_delivery_id` 指向当前版本，旧版本停止揭示但历史记录继续存在。

### 3.8 `shop_aftersales` 与 `shop_events`

售后字段：`public_id`、`tenant_id`、`user_id`、`order_id`、`state`、`issue_type`、`requested_resolution`、`description`、`assigned_admin_id NULL`、`resolution_code`、`resolution_note`、`closed_at NULL`、`version`、时间。唯一 `public_id`；索引 `(order_id,created_at,id)`、`(state,updated_at,id)`。无活动/历史案件时 API 投影为 `none`，表内不插入 none 行。以订单 `active_aftersale_id` 在订单锁下保证一个活动申请；不要用 PostgreSQL 专有部分唯一索引实现。

`shop_events` 保存订单时间线、售后消息、风险、退款证据关联、敏感读取审计：`public_id`、`tenant_id`、`order_id NULL`、`receipt_id NULL`、`aftersale_id NULL`、`refund_case_id NULL`、`kind`、`actor_id NULL`、`actor_kind`、`visibility`（public/internal）、`reason_code`、`note`、`evidence_ref`、`evidence_hash`、`operation_key`、`created_at`。索引按 `order_id/receipt_id/aftersale_id` 与时间；`operation_key` 非空时唯一。买家查询必须显式过滤 `visibility=public`，不可查询全量后依赖前端隐藏。正文追加不覆盖；不写完整秘密、请求体、支付签名/密钥或解密错误原文。

人工资料请求、SLA 暂停/恢复、管理员接单均写事件；如果商品条款允许暂停 SLA，事件记录起止、依据及秒数，订单保存当前暂停与累计值。只允许按快照定义调整，不能用“补资料”重置最初 `sla_started_at`。

### 3.9 `shop_refund_cases`、`shop_refund_attempts`、`shop_payment_adjustments`

| 表 | 核心字段 | 唯一与索引 |
| --- | --- | --- |
| `shop_refund_cases` | `public_id,receipt_id,order_id NULL,aftersale_id NULL,kind(goods/extra/mismatch),amount_minor,currency,exponent,state,reason,approved_by,approved_at,cancelled_by,cancelled_at,cancel_reason,review_owner_id,next_action,version,created_at,updated_at` | `public_id`、`receipt_id` 唯一；`(state,updated_at,id)`、`order_id` 索引 |
| `shop_refund_attempts` | `public_id,refund_case_id,attempt_no,amount_minor,currency,exponent,state,provider_refund_ref NULL,adjustment_id NULL,registered_by,registered_at,operated_at NULL,verified_by NULL,verified_at NULL,evidence_ref,evidence_hash,failure_code,next_action,version` | `public_id`、`(refund_case_id,attempt_no)`、非空 `adjustment_id` 唯一；`(state,updated_at,id)` 索引，需保存 `updated_at` |
| `shop_payment_adjustments` | `public_id,receipt_id,adjustment_key,kind(external_refund/chargeback),provider_adjustment_ref,amount_minor,currency,exponent,state,source,evidence_ref,evidence_hash,verified_by,verified_at,version,created_at,updated_at` | `public_id`、`adjustment_key` 唯一；`(receipt_id,state,id)` 索引 |

退款 case 表示持续责任，每个原收款最多一个 case，失败重试和批准后撤销再批准都继续该 case 并追加事件，不重新制造第二项责任。外部 adjustment 是真实资金后续事实；同一个渠道退款既从人工登记又从查单发现时，必须按渠道身份合并为一个 adjustment，再关联本地 attempt，不能计两次已退金额。

adjustment的external_refund指渠道实际发生的退款，包含本地退款尝试对应的外部资金事实；是否由商城流程发起通过source/attempt关联区分，不再新增一个refund近义kind。其公开枚举与API保持一致。

`adjustment_key` 对渠道、商户、环境、调整类型、渠道调整引用取规范 tuple 摘要；没有可核验渠道引用时保持待核实，不随意生成可结算的“外部成功”。拒付和退款分别记录，拒付不是商家退款成功。需要记录矛盾证据时追加 `shop_events`，已确认资金金额/币种/原交易不可编辑。

### 3.10 `shop_payment_evidence`

为 API 的证据登记/读取提供不可变表：`id,public_id,receipt_id,source,provider,provider_account_ref,environment,provider_transaction_id,provider_adjustment_ref NULL,amount_minor,currency,exponent,observed_status,observed_at,reference,summary,correction_of_id NULL,created_by,created_at`。public_id 唯一；索引 `(receipt_id,created_at,id)`、`correction_of_id`。不存任意上传文件或公网代理 URL。渠道引用、说明长度沿用 API 上限。

浏览器只能提交 `source=merchant_console`；商户/环境从所选 receipt 的受控事实加载。`provider_query/provider_webhook` 只由服务端验证器写入。更正另建记录并关联原证据，不 UPDATE 旧观察。创建 evidence 不改变资金真相；只有随后核实命令满足身份、金额、来源追溯和无冲突条件后才更新投影。

上表中的 evidence_ref/evidence_hash 表示首条主证据及摘要；需要多条证据的命令在 `shop_events` 另存有界 `evidence_ids_json`，逐条核对均属于同一 receipt/授权对象。不能接受任意另单 evidence ID，不能用 free-text reason 取代结构化证据。首版不建通用附件系统；人工材料的软件一致性检查不等于渠道的密码学证明。

### 3.11 `shop_idempotency_keys`

字段：`identity_hash`（唯一）、`tenant_id`、`actor_user_id`、`scope`、`key_hash`、`request_hash`、`request_hash_key_version NULL`、`resource_type`、`resource_public_id`、`result_metadata_json`、`created_at`、`expires_at NULL`。identity 摘要绑定主站、actor、操作 scope、目标对象和客户端 key；哈希采用规范结构，不能直接拼可歧义字符串。含 secret_fields/库存 rows 等秘密的 request_hash 使用独立 HMAC，避免低熵秘密通过普通 SHA-256 被离线猜测；按保存的 key version 比较重放，相关旧键保留到记录保留期结束。result_metadata_json 仅可含批次数量等脱敏白名单数据。

业务写入与幂等记录在同一事务提交。重复同 key 同内容返回既有资源；不同内容返回冲突，不能覆盖旧请求。只保存资源引用，不缓存包含凭据/支付入口/个人资料的完整响应。失败事务不留“成功键”；并发唯一冲突回滚后重新读取既有键判定。

API 根版本归属固定：订单子动作更新订单 version；售后消息/决定更新售后 version，补发同时校验并更新订单 version；退款 attempts/record/verify 更新退款 case version；收款 evidence/adjustments/review 更新 receipt 处理 version。子表有 version 仅供内部 CAS，不能误要求前端用 attempt version 替代 case expected_version。同事务如影响其他根的可操作状态也递增对应根，回包包含契约约定的最新脱敏资源。幂等命中优先于旧版本冲突，但不早于当前身份/权限校验。

`GET /api/shop/order-requests/:key` 与 `GET /api/admin/shop/inventory/import-requests/:key?product_id=...` 只在原 actor、tenant、固定操作作用域中查此表，返回 recorded/not_observed 及原订单号或批号/数量，无需新表。not_observed 仅表示查询时尚无已提交结果，不能证明并发原请求失败；后续仍只能携相同 key 重放，不建议改 key 再买或再次导入。

现有 `ConsumeOperationProof` 在业务动作前独立提交 proof 消费，失败不能恢复证明。敏感写操作先做已提交幂等结果查询并重新鉴权：命中仅返回脱敏既有结果；未命中才验证新 proof 并进入事务，事务仍复核幂等及版本。业务事务失败后保持原业务 key，要求重新强验证，不尝试复用已消费 proof；不会因此创建第二份资金责任。

涉及库存/交付/退款的幂等记录至少保留到对象责任终结并满足确认的重放保留期；不能以通用 24 小时清理打穿永久首次交付和资金唯一性。表级业务唯一约束仍是最后防线。reveal 不要求业务幂等键、不读取幂等响应缓存；每次响应重新验证会话、归属、退款/风险与期限并追加读取审计，已到期重放仍拒绝。

## 4. 状态映射与派生规则

业务规则原文具有优先级。下列英文值是与 API 契约对齐的持久化映射，不能再创建另一套近义状态；新增状态须先更新双方文档。

| 维度 | 英文值与对应含义 |
| --- | --- |
| 本地订单生命周期 | `open` / `closed`；关闭不删除实收和待核实责任 |
| 付款 | `unpaid` 未付款、`verifying` 付款核实中、`paid` 已付款；退款后仍有曾经已付事实 |
| 交付 | `not_ready` 未就绪、`pending` 待交付、`processing` 处理中、`awaiting_input` 待补资料、`delivered` 已交付、`exception` 交付异常 |
| 库存 | `available/reserved/sold/quarantined`；“已查看”不是可释放库存状态 |
| 售后 | `pending/processing/replaced/resolved/rejected/closed`；退款进度来自关联 refund case，不复制成可独立改写的售后金额 |
| 退款责任 | `approved/processing/unknown/failed/succeeded/cancelled`；failed 仍是未结责任；unknown 不得转成“确认失败”代替查证 |
| 退款尝试 | `registered/processing/unknown/failed/succeeded`；注册不证明已在渠道操作，取消只在 case 尚无外部执行时按规则办理 |
| 支付创建过程（内部） | `prepared/creating/ready/unknown/failed`；failed 必须是能证明未创建/不可支付的确定结论；超时为 unknown |
| 收款处理（内部） | `pending/matched/extra/review`；matched 表示有效商品款，extra 表示第二笔真实款，review 保留待核实或差错退款责任 |

`risk_hold`、未决 dispute、查看截止是独立条件。主状态按业务规则第 4 节派生：有效款已全退 → 有效款退款责任处理中（含未知/失败的具体提示）→ 活跃售后 → 已付款下的交付 → 付款核实 → 本地关闭/待付款。额外收款未结提示始终单独返回。服务端同时给出当前允许动作，写接口仍逐次校验，不能信客户端 allowed_actions。

支付 attempt API 的 `creating/ready/unknown/closed/failed/paid` 是创建、关闭和已收款事实的合并投影：prepared 尚未发出也显示 creating；closed 需要可信不可付款证据；paid 优先于先前 closed。不能把内部 create_state 直接序列化。资金 adjustment 内部 kind `refund` 对应 API 的 `external_refund` 登记输入，来源另记本地退款尝试/商户后台；state 统一 `pending/succeeded/failed/disputed`。

## 5. 事务与锁顺序

### 5.1 唯一的商城加锁顺序

有后台 fence 时先锁 leader/event，再依次：**用户 → shop_user_state → shop_settings（仅涉及启停/新建）→ 商品 → 订单 → 支付尝试 → 收款 → 售后/退款 case/attempt/adjustment → 库存 → 交付**；同类多行按 ID 升序。最后插入审计、幂等记录和新 outbox。不持有库存锁后再回头锁商品/订单。

从库存、退款或回执定位原订单时，先无锁读取不可变引用，再从序列起点重新查询并锁定，事务内核对映射没有改变。不在“已锁收款”事务中再查锁订单。尚未匹配的回执入库是独立短事务，只写收款及其事件/outbox，不反向拿用户锁；之后另起事务恢复映射。

商品管理员编辑/导入只持有商品及后续库存，不拿用户锁；若库存已关联订单，隔离动作转入有用户/商品/订单上下文的完整命令。删除商品采用归档，不通过级联触及订单。

`shop_user_state` 在锁用户后 `INSERT ... ON CONFLICT DO NOTHING`（GORM clause），再执行真实 `version = version + 1` 写入。对 SQLite，这会竞争数据库写权限；获取写权限后重新读取并验证用户可交易状态。删除路径也参与同一栅栏。不要增加用于抢锁的 auth_version 更新，否则每次下单会撤销会话。

### 5.2 SQLite 与 CAS

每个库存/订单/管理员状态变更使用 `WHERE id=? AND version=? AND <预期状态/归属>`，执行后检查 `RowsAffected==1`。未命中先读取最新对象区分幂等已完成或冲突；禁止强制覆盖。

示例语义：`available → reserved` 必须同时符合商品 ID、版本和 available；释放必须符合 reserved、同一 order ID、已到期和最新订单无有效款；`reserved → sold` 必须符合相同归属并与交付创建同事务。条件失败则整笔业务事务回滚，不保留只有订单没有库存的半成品。

沿用仓库 `walletTransaction`/`membershipTransaction` 的有界思想，商城事务初始最多重试 3 次；只对识别到的 SQLite busy/locked 和可重试写冲突，从事务开头重读。等待要受 context 控制，不能复用失败事务或在 retry 内调用支付商/发送通知。CAS 竞争缺货可以在限额内重新挑库存，达到限额返回明确可重试冲突；不能无限循环。用户删除整体事务也需该保护，不能只重试责任查询而跳过删除/鉴权部分。

MySQL/PostgreSQL 继续使用 `lockForUpdate(tx)`，并保留相同 CAS 与唯一约束。MySQL 5.7 不支持 `SKIP LOCKED`，因此首版用有序挑选及短事务即可。冲突错误用类型化领域错误映射至 API 契约，不能把 SQL/密文/秘密作为客户端 message。

### 5.3 下单与容量

1. 在 session/主站边界后做请求形状/技术上限校验，得到规范请求摘要。客户端不传最终成交价或可变数量，服务端数量恒为 1。
2. 同一事务先锁用户与栅栏，验证当前身份、enabled、未删除及归属；随后查询 actor+scope+key。已有同摘要返回原订单，同 key 异摘要拒绝。此回放不受当前停售、改价、库存或原商品 version 变化影响，不会创建新支付。
3. 仅没有既有幂等结果的新单，继续锁设置和商品，核验 sales enabled、已发布、业务参数/支付能力、条款确认及当前资料要求；在用户栅栏下计算未付款/未知责任上限。product_version 或 terms_version 不一致返回重新确认，不能悄悄按新价下单。
4. 创建不可变订单快照；自动商品预留一份具体库存，人工商品执行 `manual_used < manual_capacity` 的版本条件更新并将订单 `manual_capacity_held=true`。同事务建立到期处理事件、幂等记录和审计。
5. 提交后才允许创建支付尝试或调用渠道。事务失败不产生可付款入口。

人工容量释放必须持有商品与订单锁，且仅 `held=true → false` 成功一次后 `manual_used-1`；完成首次交付或已安全结束占用时释放。迟到收款先认钱，再重新申请库存/容量，失败进入 paid+exception。定期只读核对 `manual_used` 与订单 held 数，发现差异报警，不在读请求里猜测修正。

## 6. 支付尝试、回调与收款恢复

### 6.1 外部调用前后

- 先在短事务持有订单锁，确认没有 ready/creating/unknown 的活动尝试，保存不可变金额、商户/环境及凭据快照，创建唯一 trade_no 与 claim token；提交后调用渠道。运行中的 claim 过期不能证明渠道未创建。
- 渠道请求使用同一 trade_no/原渠道幂等键。返回后以 `attempt_id + claim_token + version` CAS 写回；旧请求迟到不能覆盖新处理结果。若更晚的成功响应与“未知”并存，保留可信信息并核对原身份。
- 发出请求后的网络中断统一 unknown，保留原尝试查单。仅可信证据证明原创建未发生或已不可支付，订单仍可支付且预留重新确认时，才在原订单下增加 attempt_no。无查单能力时保留人工核实责任。
- 本地取消/到期与渠道关闭分列字段。查询/关单在事务外；到期释放可按政策执行，但不得消除回调/核实责任。恢复已有支付入口与新建渠道交易分开；sales 关闭时不得新建。

### 6.2 回调两个提交边界

**阶段 A：保全可信实收。** 有界读取原始 body；按原渠道凭据验签，核对已知商户与环境，必要时主动查询。解析出的真实金额/币种/交易身份即使不匹配订单，也保存独立 receipt。原始签名包不直接写普通日志；证据记录为受限引用和摘要。receipt 与 `shop.payment.receipt.v1` outbox 在一个短事务提交，唯一冲突读取并比对原事实。

只有阶段 A 持久化成功后才按渠道协议 ACK；失败返回允许重试的响应。业务尚未履约、映射异常或后续事务暂时失败不应否认已经持久化的可信收款。未经验证的请求不能制造“可信实收”。

**阶段 B：匹配与业务处理。** 重新取得用户/商品/订单/尝试/receipt 锁；核对渠道、商户、环境、原交易引用、provider session/order、金额/币种/精度、付款状态及商品目录/数量证明。全部匹配时：

1. `effective_receipt_id` 为空则将这笔设为有效商品款并固定 `paid_at`；同一 receipt 重放直接返回已有结果；已有另一笔有效 receipt 时本笔标 extra，持久化差错责任，不产生第二件交付。
2. 实收认可不与库存充足绑定。订单置 paid 后，在同事务登记待交付或异常；插入首次履约事件。库存/容量获取失败产生明确异常并提交已付事实，不把已付退回 verifying。
3. 若身份或金额不全匹配，receipt 留 review，保存原因、责任人与待办，不更新有效商品款。恢复映射仅允许可信证据重建原引用，并从本阶段重新全部校验。

匹配和首次交付可以分成两笔短事务：付款事务只确认有效款并安排履约；履约事务再检查最新退款/风险并原子卖出库存与创建交付。这样缺字段/解密故障只影响交付，不回滚实收。两者都有持久恢复入口。

### 6.3 充值隔离与未知业务归属

共用回调路径时，先按持久化的原 trade/session 业务归属分派；不能“商城查不到就尝试充值核销”或仅相信 URL/前缀/客户端 metadata。每个核销器再次核验 provider 的可信原交易引用和自身 immutable attempt。跨入口投递不能改变业务归属。

同一商户还可能承接充值/订阅等业务。未能证明属于商城的收款只能进入未匹配 review，不能自动给商品或自动退钱；先排除已被钱包/其他业务接收的原交易。不能因为两张 receipt 表各自唯一，就声称已经保证跨业务交易唯一；保障来自可信原单绑定、分派与双向回归。若首发渠道无法提供可证明的原单/业务身份，不能作为首发渠道。

商城路径禁止写 `top_ups`、user quota、充值赠额、分销支付 lot、返佣 outbox。既有充值描述、幂等键和金额语义保持原契约。自动测试同时断言商城真实处理不产生这些副作用，不能只测商城订单变成 paid。

## 7. 履约、领取与 SLA

**首次自动交付事务：** 取得订单最新事实，已有版本 1 则返回；没有有效款、风险暂停、未决争议或有效款退款责任 approved/processing/unknown/failed/succeeded 时拒绝新交付。选择仍归属本单的有效预留；已释放则原子重新申请可售库存。确认密文可解且字段模板完整后，同事务 stock→sold、插入 delivery version 1、设置 current delivery、delivery_state=delivered、释放人工容量（如适用）、写审计和通知 outbox。解密/字段异常必须记录 exception 并停止，不能返回半份内容。

解密只使用本机密钥，不得在事务中请求远端 KMS 或网络。可事务前读取/预解密以缩短锁时间，但提交必须验证密文行版本未变，必要时在锁内重做本地解密。临时明文不进入错误/数据库 trace。

**人工首次交付：** 管理员提交所选订单 version、交付内容和操作幂等键；先加密固定 DTO，再使用同一首次交付命令。两管理员竞争只有一个 `(order_id,1)` 成功，冲突返回已存在事实。处理人标记不等于交付，不能因通知成功而改 delivered。缺资料时保持 awaiting_input，补资料只接受快照中允许的字段。

`sla_started_at=max(可信 paid_at,首次必要资料齐全时间)`。仅当两者已知时计算并持久化绝对 `original_delivery_due_at/delivery_due_at`；已有起点不随请求补充而重置。自然时间按 `duration_minutes` 的受限整数计算。business 使用快照 IANA 时区和每周至多 14 个不重叠窗口，跨午夜预先拆段；累计窗口中真实经过分钟，跳过非工作时间及 DST 不存在的本地时间，重复的本地时段按真实经过时间计算，不含假期引擎。计算须有有效窗口及可达截止的上限，不能因空排班无限循环。该算法和 DST 边界测试完成前禁止发布 business 商品，不用自然小时假装支持。

资料暂停必须是政策显式 `pause_on_missing_input=true` 且请求原已约定字段；首次记录 pause_started_at，补齐后只结算一次这段暂停、追加依据事件并更新 due_at，保留 original_due_at。按所选 SLA clock 排除暂停期间本会计入的自然/工作时间，不能不分口径直接加全部墙钟差；多个请求不能重复暂停或双重加时。

查看政策首版只允许 delivered_at 起算，expired_action 固定 deny_reveal。质保支持 API 的 none/duration/fixed_end：duration 仅从 paid_at 或首次 delivered_at 起算并一次性固化 `warranty_started_at/warranty_ends_at`，fixed_end 为原约定 UTC 截止；none 表示没有额外商业质保，售后入口仍保留。商品权益独立按 entitlement_policy 固化适用期限；补发不重算质保/权益。上述固定字段补入订单事实，不靠查询时重读现商品计算。

**领取：** 专门 POST 每次查询当前 session、主站、用户 enabled、订单归属、有效实收、当前 delivery 归属、非退款限制/风险/争议、未撤销且 `now < view_expires_at`；不可按前端按钮决定资格。普通订单 GET、列表、通知只返回脱敏摘要。读取审计持久化后才返回明文，数据库审计失败时拒绝交付响应；审计表达“接口返回/授权读取”，不声称用户实际使用成功。

返回 `Cache-Control: no-store, private`，避免缓存及响应 body 日志；完整凭据不入 CDN、URL、outbox、浏览器持久化。并发领取与退款以订单锁串行校验：退款先提交就不返回；领取已获准后退款不能撤回已经发出的内容。复制使用同一授权领取结果/接口，不建可绕过期限的复制专用接口。

**补发：** 在活动售后和订单锁下检查无退款/风险/争议，并以唯一 `aftersale_id` 保证一次补发；分配新 stock 或保存新人工密文，建立 version+1，旧版本设 reveal_revoked，订单指向新版本。新 `view_expires_at=本版本 delivered_at+原快照查看时长`。不重写旧期限，不延长商品使用权/质保，不改变已收款金额。

## 8. 退款责任、尝试及拒付

1. **批准：** 按订单→receipt 锁序读取原款。普通款必须是订单 effective receipt；额外/差错款必须是选择的独立 receipt。核对全额尚可退责任和已有 adjustment，创建/继续唯一 case；普通款在同事务关闭新的交付和揭示资格，保留是否已经交付的证据。额外款 case 不影响有效商品款的资格。
2. **登记尝试：** 渠道实际操作前先创建 registered attempt，记录本地 ID、金额/币种与操作者，同时 case 进入 processing 待核实。registered 已占用这笔退款操作责任，不能因为没有 channel_ref 就允许第二次登记；管理员离开/返回继续同一尝试。它不证明渠道已退款；首版不会调用自动退款 API。
3. **核实：** 操作后关联渠道退款引用及可核验证据。成功必须匹配原商户/环境/交易/退款身份/金额/币种；同一调整写入/关联唯一 adjustment，标 attempt succeeded，再计算 case 是否已履行。未知保持 unknown；可信失败标 failed，并保存失败原因与下一步。
4. **失败重试：** 仍在原 case 下；重新核实旧 attempt 没有成功/在途结果、没有拒付冲突、金额确可退，保存证据后才新增 attempt_no。失败不释放普通款的交付限制；unknown 永远不触发自动重退。
5. **撤销批准：** 仅在无渠道执行或已可信证明未发生外部退款、无任何未知/在途 adjustment 时有理由撤销。取消 case 不代表恢复领取：仍重新检查全部资格。已成功退款不可撤销成未退。
6. **外部退款/拒付：** 独立记录、按渠道 adjustment 身份去重。未决 dispute 停止新增退款和受影响订单的履约/揭示，保留责任。处理款额按 receipt 分开计算，不跨单拼数。部分外部调整进入人工核实，首版不自动补退，也不借此开放商品款部分退款。

计算可退责任时，只对同一 receipt、同币种/精度的已接受事实做整数相减；每步先验证非负、单项不超过 receipt、累计不会溢出。分别计算已确认返还/扣回、已登记在途与争议，无法排除重复引用或总额矛盾时阻止操作。普通或差错新退款要求对应整笔尚未被外部调整，且金额等于全额责任；有部分外部调整时交人工处理，不自动制造“剩余部分”商家退款。

拒付完整扣回是资金事实，不能改名为 case succeeded 的商家退款；应通过独立 adjustment/dispute 结果和可公开说明体现。有效款已确认全额外部退款则禁止领取并反映退款事实，额外款全额外部退款只解除该额外款待办。case 是否结案需要确认其责任已由哪笔 adjustment 履行并保存依据，不删除 failed 历史。

## 9. 加密、导入去重与保留

- 独立商城加密 keyring，明确 active key ID、旧 key ID；不使用随机 session secret 兜底。使用标准库 AES-GCM 和随机 nonce。AAD 为固定版本 tuple，如用途、tenant、stock public ID 或 order+delivery public ID、字段模板版本，防止搬移密文串货；支付快照、库存、人工交付和买家资料使用不同用途域。
- 生产部署采用 `SHOP_SECRET_KEYS_FILE` 和 `SHOP_FINGERPRINT_KEYS_FILE` 挂载两份独立 JSON keyring（最大 8192 字节），活动版本分别由 `SHOP_SECRET_ACTIVE_KEY_ID` / `SHOP_FINGERPRINT_ACTIVE_KEY_ID` 指定。文件配置优先于 `SHOP_*_KEYS`；指定文件缺失或非法时 fail closed，不能回退环境值。独立安装仍允许内联变量。infra 的 `newapi_shop_configured` 只控制凭据挂载，不打开销售；有历史订单/保留秘密时不能移除该配置或旧键。两槽和 writer 必须使用相同保留键集合，按受控发布重建后才能采用更换的挂载文件。
- key 不入数据库/仓库；解密可读旧版本，缺密钥时 fail closed 并形成异常，不能使用空密码或重发另一库存掩盖问题。独立验证数据库+密钥备份恢复。轮换通过有界维护批次重加密，业务身份与明文不变，原 key 在所有历史/未结数据迁完前保留。
- 库存身份指纹采用独立 HMAC-SHA256 密钥，不把密码拼入账号身份。账号按明确服务域+登录标识规范化；只做供应方确认可接受的 trim/case 处理，不能默认所有账号名小写。卡密按其确切格式规范化。相同账号不同密码或不同商品期限不能绕过同一身份域去重。
- 指纹密钥与加密密钥独立，常规密文轮换不改变指纹。首版指纹密钥轮换采用暂停导入、保留旧键、分批解密重算、核对全量唯一后切换的维护步骤；未完成不可新导入。已销毁密文且只剩旧指纹的 tombstone 无法重新计算时必须保留该旧键，在导入时同时检查历史版本并写当前版本；不因“轮换完成”丢掉去重能力。该维护流程上线前测试，不能只切环境变量。
- 导入只支持固定账号/卡密模板，限制请求字节数、行数、每字段长度；预检返回行号/脱敏原因。首版确认时重新提交内容并重验，预检不是库存锁。整批存在任何错误/重复则不入库；最终插入仍由唯一约束防预检后竞争，冲突整批回滚。单批技术上限与 API 同为 200 条 / 1 MiB；preview 返回带密钥规范内容摘要，commit 重传 rows 和 preview_digest，服务器重算比对但仍做完整校验。commit 为该批创建随机 import_batch_public_id 写入每行；幂等记录指向批号并保存非敏感成功数，可由库存批号查询重建响应，无需导入草稿/临时文件表。先完成解析/指纹/加密再开短事务，避免持锁解析大文件。
- 禁止批量明文导出；管理员单条读取、补发、退款/外部资金事实确认等使用商城专属强验证 scope。context 只绑定动作、对象 public ID、根 version、幂等 key（reveal 除外）及金额/原因/模板版本等非秘密规范字段，由现有验证服务在服务端规范化并计算绑定摘要；创建、消费路径使用同一允许列表和 DTO。库存导入可另绑定已有 preview_digest 这个不透明值，commit 仍需重算核对。库存/人工正文和买家资料不进入通用验证 context，全文仅参与业务端 HMAC request_hash 并由同 key 首次成功事务固定；不声称 proof 绑定了人工秘密全文，不要求浏览器构造服务端 HMAC，也不新增秘密 proof 预备接口。校验 identity 的 session 限制在 proof 之前，避免通用 step-up 的 PAT 分支获得商城权限。
- 管理员揭示可售库存前，同事务 `available→quarantined`、写揭示时间和审计，提交后才返回秘密，不能查看完继续卖给未知买家。首版拒绝直接揭示 reserved 库存，要求先走明确的隔离/订单处理；对已售库存走原订单当前交付版本的管理领取边界，同样检查风险、退款/争议、查看期限及保留规则，保留 sold_order_id，绝不解除归属。隔离预留库存时按用户→商品→订单→库存锁序清预留并登记订单异常，单纯隔离不能替代风险暂停。
- `view_expires_at` 仅表示停止对客揭示；不直接触发物理删除。另保存/计算 retention 截止和 legal/责任 hold。存在售后、未交付、未知付款、退款/争议时不得清理所需证据或解密能力；到期任务只清除批准政策允许的密文/个人资料，保留交易 ID、指纹 tombstone、金额和关系/审计。保留期限未确定时阻止上线配置完成，不把“无限保留”作为默认。

具体清理记录：订单保存 `buyer_input_retention_until/retention_policy_version`；delivery/stock 保存 `secret_retention_until`；涉案对象保存可审计的 retention hold 原因及设置/解除事件。交付密文截止不早于 view_expires_at，并按快照政策从本版本 delivered_at 算；无交付的库存不因“无期限值”被误清理。到期维护先筛选候选，再在订单/库存锁内重查所有未结责任和明确保全 hold，满足条件才将密文置空并写 purged_at。清理后再到达的合法问题仍保留交易/售后处理入口，不假称秘密可恢复。密钥删除与数据清理分离，需确认没有任何仍需读取的历史引用后才能移除旧解密键。

## 10. 主站权限、账户禁用与删除

路由先使用受信任 Host 解析所得 tenant context，再要求平台 tenant；缺 context 或未知 Host 一律不回退主站。查询均通过请求身份附加 tenant/user 谓词，订单存在与否对他人采用同一不可见响应。平台管理员才能运营；租户管理员即使拥有相同数字角色也不能访问。

现有 `middleware/audit.go` 不持久化请求正文；它对管理写请求临时缓冲至多 64 KiB 响应以判定成功，落库是路由等元数据。这不是已存在的请求体泄漏。商城仍须显式写事务内业务审计；秘密 reveal 应在 `beginAdminAudit` 包装前选择仅状态元数据的审计模式，避免额外复制秘密响应。仅设置 `ContextKeyAuditLogged` 只能跳过 finish，不能取消已经建立的响应缓冲。不得关闭整个管理员审计或影响其他管理路由。

买家和管理员 mutation 必须真实 session + 现有同源策略；GET 也不能由 PAT 获得私有订单/秘密。支付回调单独走渠道验真，不要求浏览器 session，不把同源校验误套在服务端 webhook 上。公开商品读取仍要求主站，但不要求登录。

软删 `User.delete` 和硬删 `User.HardDelete` 共用责任门禁；在 `checkTenantUserDeletionTx` 对主站直接返回前加入商城检查，或抽一个明确的共同业务删除检查再调用原租户检查。下单、创建售后/新支付尝试、收款匹配等能够建立新责任的命令均先取同一用户与 shop_user_state 栅栏。

删除时在该栅栏内检查：可支付/未知的外部尝试、待处理实收、已付未交付、活动售后、风险/争议、approved/processing/unknown/failed 退款责任。即使本地 order closed，只要渠道仍可能收款或结果未明仍阻止删除；不能仅查 payment_state。查询失败或商城 schema 应存在却不可验证时拒绝删除，不能 fail open。

禁用不阻止管理员/worker保存实收和处理原责任；后台只需要历史归属，不要求买家当前 enabled。买家领取/创建新责任仍遵守认证禁用。允许删除/去标识化后保留商城 user tombstone 和交易引用，绝不级联删除收款。若极晚渠道证据到达已删除账户，先独立保全 receipt 并交管理员处理，不借机恢复登录、不自动创建新交易或丢弃钱款。

删除与下单竞争必须用数据库并发测试证明，不能只加一个事务外 `COUNT`。现有其他业务可能采用不同锁序，不在本次全面重排钱包/会员；新删除检查不能反向锁它们的交易行。商城责任计数在用户栅栏下读取即可，所有能新增该责任的商城命令必须遵循同一栅栏。

## 11. Outbox、补偿与停止开关

建议首版只增加三个持久 topic：`shop.payment.receipt.v1`（处理 receipt）、`shop.fulfillment.v1`（首次/获准补发命令的安全续办）、`shop.notification.v1`（无秘密状态提示）。event key 由业务动作与不可变对象/版本构成，不用随机键掩盖重复。payload 仅 `{schema_version,receipt_id/order_id,delivery_id?}` 等引用，不复制凭据、买家资料、支付入口或全量快照。

使用 `KKAIOutboxProcessor.Register` 和现有 leader job/system task 接入。处理函数必须接受 context、批量有上限；失败分为可重试基础设施错误与需人工的稳定业务异常。库存不足不无限快速重试：保存 exception 与责任，补货/管理员继续时创建有唯一依据的下一处理动作。死信必须出现在后台队列，保留重新驱动的审计；“死信”不表示资金责任终结。

再用一个有界维护任务扫四类真实状态：到期预留、unknown/到期原支付查单、pending receipt、paid 未完成交付。扫描按 `(next_due_at,id)`/游标分页，不扫描“前端最近点过的订单”。任务直接调用同一幂等模型命令，不能只反复 enqueue 已标 delivered 的相同 event key 并期待它再次执行。通知丢失也不能重新跑库存分配。

worker 在提交状态前验证自身执行权和最新业务版本。现有 `model/image_async_guard.go` 提供 leader/event fence 设计例子，但名称和语义绑定图片，不直接调用它处理商城；只提取实际需要的中性 fence 检查或实现商城同等小函数。失去 lease 的旧 worker 不得更新新的任务结果；即使发生重放，数据库唯一约束仍防双交付。

schema、密钥或首发渠道缺配置时，新购买 fail closed；运行中的存量核实不能因 sales=false 停止。密钥丢失/worker 不兼容需要可见告警和责任承接，不允许清空 pending 队列让状态“正常”。

## 12. 受控 schema 集成与兼容回退

本次没有确定下一 schema 版本，也没有查询线上版本。当前源码 `runtime_contract_feature.go` 的 `RuntimeMaxVersion=WalletPaymentSchemaVersion`，钱包 schema 常量为 18；这是本地代码事实，不能据此宣称生产为 18 或商城直接使用 19。

实施必须同时完成：

1. 在实际实施基线 `catalog.go` 分配商城扩展版本和唯一 implementation ID；新增三数据库 DDL/模型形状、列/类型/索引校验、canonical checksum，保持已存在迁移不可变。DDL 仅扩展，不删旧表/列；默认无演示商品/订单/库存，不回填虚假业务事实。
2. 参照 `schema_wallet_payment.go` 的 precheck、objects-present、apply、check 路径实现商城受控扩展；预检能发现部分建表/缺索引/无回执。PostgreSQL/SQLite 使用事务能力，MySQL DDL 可能部分提交，要能识别并按专用恢复流程续办，不能把 IF NOT EXISTS 当形状验证。
3. 增加 `CheckShopRuntimeSchema`，覆盖真实 ledger/digest、必须字段与唯一约束；API 与 worker共用有界检查。不在每次库存事务内执行 Migrator 扫描，不把单纯 `HasTable` 当可用。
4. 扩展 runtime 兼容区间与 schema 校验分派，准备扩展前后都能运行的服务/回退版本。本地钱包 `CheckWalletPaymentSchema` 等旧只读门禁已接受 canonical schema19，并保留物理形状/回执检查；旧迁移 proof 的写入授权区间仍冻结。不能取消上界或接受未知未来版本。生产 A/B 回退版本与精确镜像必须另行构建验收，见同级 infra [runbook 31](../../../kkai-infra/docs/runbooks/31-newapi-shop-schema.md)。
5. 为商城支持的 topic、回调和存量模型准备兼容 leader/回退能力，再启用购买；旧 leader 不认识商城 topic 时不能先让新候选写正式商城单。扩展就绪与销售开关分开，关闭新购不停止旧单责任。
6. 开发数据库可以在明确隔离环境应用该扩展；生产安装是单独授权的 schema 操作，必须进入现有 NewAPI runbook 与精确镜像契约检查。此文不授权迁移/构建/部署，也不需要修改 controller pin。

回退不是删商城表：保留资金和交付数据、密钥与 worker 处理能力，在确认兼容的运行版本上继续清偿存量。若预定旧版本不能读取新 schema 或承接商城回调/任务，则它不是可用回退目标，不能仅关入口后回退。

## 13. 开发切片与可验收输出

| 切片 | 实际交付 | 对应验收 |
| --- | --- | --- |
| B1 schema/模型 | 表与索引、状态常量、金额/快照 DTO、独立门禁、三方言扩展/回退检查 | 唯一性、部分扩展失败、未来版本拒绝、钱包门禁兼容 |
| B2 下单/库存 | 商品发布校验、幂等订单、预留/人工容量、取消到期、用户删除共同栅栏 | BR-01～03、11～13、29～30、38 |
| B3 首发支付 | 最小渠道提取、尝试/未知恢复、独立实收、匹配与额外/异常款队列 | BR-04～10、35、39；不改 API quota/返佣 |
| B4 履约/秘密 | 原子首交付、人工时限、加密导入、逐次领取、轮换与清理 hold | BR-14～18、28、40；两管理员并发和越权直调 |
| B5 售后/资金 | 活动申请唯一、补发、风险暂停、退款 case/attempt、外部调整与拒付 | BR-19～27、31～37；额外款独立、失败不能解锁 |
| B6 恢复验收 | worker、死信处理、进程崩溃、渠道超时、sales关闭继续存量 | 收款落库后崩溃可恢复；旧任务不能覆盖新状态 |

每个切片提交时应附具体影响文件、定向测试和未解决经营输入；B1～B3 仅是开发阶段，不算完成用户要求的首版。

## 14. 代码质量与定向验证

- 模型命令以业务动作命名，例如创建订单、接收可信回执、提交首次交付、批准原款退款。稳定跨模块逻辑可以抽函数，单调用机械搬运不另建层。Controller 不调用 DB，model 不访问网络，service 不绕过 model 直接改资金状态。
- 使用早返回、显式状态分支和带语义的错误；不用一个“更新状态”接口接受任意字符串。不用 `Save` 覆盖并发读出的全行，采用明确字段与 version CAS；禁止忽略 RowsAffected 和唯一冲突。
- 事务函数的所有可重试副作用只发生在数据库内；外部调用、邮件和日志敏感信息不进入重试闭包。金额与持续时间在信任边界检查；生产参数缺失拒绝启用，不从演示稿取兜底值。
- 测试使用 `require` 做 setup/致命断言、`assert` 做值比较；用确定的竞争屏障验证两事务结果，不以 sleep、海量随机循环或日志输出来假装并发测试。SQLite 内存数据库应明确连接/共享行为，不能以单连接串行测试代替多连接竞争。
- 契约测试断言用户可见结果和数据库不变量：一份库存/一份首次交付、两笔实收/一件货、有效款与额外款、账务零副作用、到期直调拒绝、删除与下单一致性。不要测试私有常量名、字段列表顺序或复制实现。

建议新增测试按 `TestShop...` 命名并落在对应 model/service/controller/migration 包。后续实现时选用以下定向命令；**本次没有运行，也不承诺当前文件已存在或耗时已测得**：

```sh
go test ./model -run '^TestShop' -count=1
go test ./service -run '^TestShop' -count=1
go test ./controller -run '^TestShop' -count=1
go test ./middleware ./router -run '^TestShop' -count=1
go test ./pkg/kkaimigrate ./cmd/kkai-migrate -run '^TestShop' -count=1
```

共享支付修改后，从已有 `TestWalletStripeGatewayKeepsOriginalCheckoutAndIdempotency`、`TestWalletStripeQueryRejectsDifferentOriginalPaymentIdentity`（仅当首发/改动涉及 Stripe）、`TestFinalizeTopUpCommitsOrderQuotaAndOutboxAtomically`、`TestFinalizeTopUpReplayDoesNotDoubleCreditOrDuplicateOutbox` 及首发渠道的实际测试选择直接回归；新增同渠道商城/充值交叉投递测试。不得把 Stripe 测试当所有渠道通过。数据库扩展需要隔离 SQLite/MySQL 5.7/PostgreSQL 矩阵，使用项目现有测试连接约定并明确每条命令，不能连生产测试。

任何未知耗时、预计超过五分钟或全套命令，按项目要求先报精确命令、必要性和预计耗时并取得批准；普通“验证”不授权 `go test ./...`。定向通过后停止扩大范围。

本次文档检查仅核对上位规则、源码符号、内部交叉引用及空白格式。全仓测试、lint、类型检查、构建、真实支付、数据库迁移及生产检查全部跳过；本规格不替代实现后的交易测试与发布门禁。

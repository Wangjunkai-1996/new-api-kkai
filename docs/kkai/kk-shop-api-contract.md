# KK 商城 API 开发契约

版本：D2，2026-10-28。状态：商城路由、DTO、错误码及安全验证作用域已有本地实现；未部署，不能按本文直接请求生产。实施及验证边界见[实施状态](./kk-shop-implementation-status.md)。

依据：[PRD](./kk-shop-prd.md)、[业务规则](./kk-shop-business-rules.md)、[开发总纲](./kk-shop-development.md)。交易行为以业务规则为准；本文统一前后端字段、端点及错误语义。数据库内部字段见[后端开发文档](./kk-shop-backend-development.md)，前端不得直接依赖 GORM 模型。

## 1. 共用协议与身份边界

| 项目 | 约定 |
| --- | --- |
| 根路径 | 买家及公开目录 `/api/shop`；管理 `/api/admin/shop`；新增注册函数接入现有 API router |
| 身份 | 公开商品仅主站可浏览；买家使用现有网页登录会话；管理为主站平台管理员会话。所有权由会话和可信 Host 租户上下文确定，不接受请求体中的 user_id/tenant_id |
| 会话要求 | `UserAuth`/`AdminAuth` 后额外检查 `GetSessionAuthIdentity`，拒绝 API Key、PAT、禁用用户及跨站身份。管理员不因知道订单号而走买家领取接口 |
| 写入保护 | 浏览器写操作按现有 `SessionCookieOriginGuard` 同源策略；商城不复用允许 PAT 分支的 `WalletPaymentMutationGuard`。支付商回调使用单独验签链，不经过浏览器 Origin 校验 |
| 强认证 | 管理员查看秘密、人工交付/补发、风险复核恢复、退款审批/执行登记/核实及资金调整登记，新增精确的 shop 操作 scope，调用 `RequireSecurityProof`；不能直接挂专用于渠道密钥的 `SecureVerificationRequired` |
| 成功 | HTTP 200 查询或重放，201 首次创建，202 已受理且结果尚未确定；均使用 `{success:true,message:"",data:...}` |
| 错误 | `{success:false,code,message,data?}`；使用真实 4xx/5xx。`message` 不参与客户端分支；新代码由稳定 code 映射国际化文案；沿用鉴权/安全证明既有错误码 |
| 请求限制 | JSON `Content-Type`；拒绝未知的写字段、重复关键字段、尾随 JSON、越界分页/长度/金额。用项目 `common` JSON 包装；没有新的任意透传参数 |
| 缓存 | 所有身份、订单、支付入口、库存、资料及管理接口 `Cache-Control: no-store`；秘密接口另加 `Referrer-Policy: no-referrer`。首版公开目录也不加业务缓存，先避免上下架陈旧数据 |
| 标识与版本 | 所有 ID、订单号、version 为字符串；内部 bigint 不暴露为 JS number。订单号随机生成且不可预测，但不可预测性不替代授权 |
| 时间 | 对外 UTC RFC3339，例如 `2026-10-27T08:00:00Z`；未发生为 `null`。含截止时间的响应提供 `server_time`；服务端判定是否到期，客户端倒计时仅展示 |
| 分页 | `page` 从 1 开始，`page_size` 默认 20、最大 100；稳定排序再加 ID 次序；响应 `{items,page,page_size,total}`，total 为安全整数。查询范围设上限，不默认返回所有记录 |

与现有源码对齐依据：[router/wallet_payment.go](../../router/wallet_payment.go)、[controller/topup_lifecycle.go](../../controller/topup_lifecycle.go)、[middleware/auth.go](../../middleware/auth.go)、[middleware/secure_verification.go](../../middleware/secure_verification.go)。这些是复用基础模式的来源；商城路由当前已有本地实现，实际测试与部署边界以实施状态为准。

### 1.1 金额与文本

统一 Money：`{amount_minor:"3900",currency:"CNY",exponent:2}`。这是协议示例，不指定首发价格或币种。

- `amount_minor` 为非负规范十进制整数字符串（`0` 或不带前导零的正整数），服务端解析为 int64，进一步限制至配置与渠道共同支持的上限。商品售价必须大于零。超额拒绝，不做饱和后收款。
- `currency` 为已启用的单一 ISO 币种，`exponent` 来自服务端币种规则；前端不得提交或改写指数。正常购买不接受客户端最终金额、币种、数量或 quota。
- 前端按字符串/BigInt 做整数格式化；未经安全范围验证不得 `Number(amount_minor)` 或用浮点计算退款。不同币种不能求和。
- 商品/公开说明为纯文本段落和固定结构。首版不增加任意 HTML 编辑器；链接和图片引用单独校验，渲染不使用未净化的 HTML。
- 建议技术上限：名称 120 字符、简述 300、单份条款 8,000、公开/内部消息各 2,000、必要资料每项 500、每件交付秘密 JSON 32 KiB；UTF-8 字符数与请求字节上限分别校验。限制为技术边界，可经需求调整，不是经营默认。

### 1.2 幂等、并发版本及未知结果

所有有业务副作用的 POST/PUT/PATCH 使用 `Idempotency-Key`（客户端生成的 UUID；服务器按 actor、tenant、端点动作、对象绑定）。请求摘要从规范业务字段计算，不包含安全证明、请求时间等易变字段；含秘密时只存带密钥摘要，不存明文请求。

1. 同 key、相同内容返回同一资源/结果；相同 key、不同内容返回 `SHOP_IDEMPOTENCY_CONFLICT`。
2. 同次超时重试保留 key；请求没返回不等于没创建。不使用一个 key 表示后续另一笔购买或另一份交付。
3. 修改已有资源携带 `expected_version`。后端事务内比较并更新；旧版本返回 `SHOP_VERSION_CONFLICT`，附当前脱敏资源（仅已鉴权对象），前端刷新并要求复核，不自动覆盖。
4. 幂等命中先于“旧版本冲突”判断，但仍先验证当前身份和权限；重放不解密历史秘密。消费过的一次性证明不自动授权新动作；已提交动作可在重新鉴权后返回脱敏结果，未提交的动作按既有验证流程重新获取证明。
5. `reveal` 是逐次授权读取且追加审计，不重放保存的秘密响应；不要求业务幂等键。GET 没有状态转换，GET 订单不主动调用渠道查单。
6. 202 表示动作已持久化，返回对象 ID、当前状态和推荐 `poll_after_seconds`；只有真实限流器知道等待时间时才返回 `Retry-After`，不得猜测渠道状态。
7. 支付创建失败只有在 `service.ShopPaymentNotCommittedError` 证明创建事务内业务拒绝、未提交时，控制器才可返回 `data:{request_outcome:"not_committed"}`。该标记不是普通 400/409/404 的默认含义；事务提交不明、attempt 已提交后的入口保存/读取失败不得附加标记。前端仅在此前没有成功或 unknown/网络不确定记录且收到此标记时，才可释放该付款请求的 key/body 并重新确认；已有 unknown 时即使后来收到标记，也必须保留原请求。

安全证明绑定作用域、目标、expected_version、幂等 key 及规范的非秘密动作字段。由现有验证服务在服务端规范化并生成context绑定，浏览器不生成服务端HMAC。密码/卡密/人工交付正文不进入通用验证context；它们只在业务端参与独立HMAC幂等摘要，首次成功事务固定内容。人工交付证明授权的是本订单/版本的这一次动作，不能声称证明绑定了尚未预检的秘密全文。不增加秘密预授权服务。

本地已实现的证明 scope 与动作如下；scope/context 必须贯通现有验证服务允许列表、requirements、归一化、签发/消费、前端类型及错误处理，不能只拼一个字符串。context统一含action、明确目标ID、对应根版本、业务key（reveal除外）及非秘密details；只接受本行固定字段，不接受任意map扩展。

| scope | action与目标 | 绑定的非秘密details |
| --- | --- | --- |
| `shop.inventory.reveal` | reveal，unit_id和库存version | reason |
| `shop.delivery.create` | first或replacement，order_no和订单version；replacement另含case_id/案件version | reason、交付模板版本；秘密正文不进入context |
| `shop.delivery.reveal` | reveal，order_no/delivery_id和订单version | reason |
| `shop.risk.release` | release，order_no和订单version | reason、evidence_ids |
| `shop.refund.manage` | approve绑定receipt_id/收款version；register_attempt、record_attempt、verify_attempt、cancel绑定refund_id/责任version，适用时附attempt_id | kind、Money、reason或public_note、渠道引用、operated_at、conclusion、evidence_ids；仅允许对应action实际需要的字段，金额由服务端复算 |
| `shop.payment.adjustment.manage` | record或review，receipt_id/收款version，review另含adjustment_id | kind、Money、渠道引用、state、reason、evidence_ids |
| `shop.payment.evidence.create` | create，receipt_id/收款version | source、渠道交易/调整引用、Money、observed_status/at、reference/summary的规范摘要、correction_of |

details中原证据引用/摘要不是秘密正文；有敏感资料的reference/summary不得进入普通日志。proof消费与业务事务提交不是一个事务：已提交幂等动作可在重新鉴权后返回脱敏结果；未提交且proof已消费则用原业务key重新验证后重试。事务内仍再次检查版本、归属和业务资格。

版本归属固定：`/orders/:order_no/...` 使用订单 version；`/aftersales/:case_id/...` 使用案件 version（补发另带 order_version）；`/refunds/:refund_id/...` 包括 attempts/record/verify 均使用退款责任 version；`/receipts/:receipt_id/...` 包括 adjustments/review 均使用收款处理投影 version。任一子动作成功都递增对应根版本并返回它，不能让前后端猜测 expected_version 指哪个对象。库存/商品/分类/设置使用自身 version。首次创建没有既有版本时不虚构 `"0"`，证明绑定创建目标范围、请求摘要与 key。

## 2. 对外业务状态

下列英文值是业务规则中文事实的序列化名称，不新增交易规则；不允许客户端通过 PATCH 设置这些事实。

| 字段 | 值 | 含义 |
| --- | --- | --- |
| `purchase_state` | `open / closed` | 本地购买流程是否仍开放；关闭仍能接收迟到实收 |
| `payment_state` | `unpaid / verifying / paid` | 无有效实收、存在待核实、已有有效商品款；退款不抹掉 paid |
| `delivery_state` | `not_ready / pending / processing / awaiting_input / delivered / exception` | 未就绪、待交付、处理中、待补资料、已交付、交付异常 |
| `aftersale_state` | `none / pending / processing / replaced / resolved / rejected / closed` | 普通售后案件事实；退款进度放独立字段 |
| `refund_state` | `none / approved / processing / unknown / failed / succeeded / cancelled` | 仅有效商品款的退款责任投影；失败和未知不同，额外款不占用此字段 |
| `risk_hold` | `null` 或 `{public_reason,started_at}` | 独立暂停事实；不能凭解除风险暂停绕过退款或到期 |
| `display_status` | `awaiting_payment / payment_verifying / closed / pending_delivery / processing / awaiting_input / delivery_exception / delivered / aftersale / refund_pending / refund_failed / refunded` | 主状态严格按业务规则第 4 节优先级计算，不代替事实字段 |
| `notice_codes` | 字符串数组 | 多付款、已过查看期限、风险暂停、资金争议、超时等并列提示，允许和 refunded 同时出现 |

`refund_failed` 属于尚未结清的商品款退款处理状态；`unknown` 对客仍是待核实。风险/争议提示不重写付款或交付事实。当前交付已过查看期限时仍是 delivered，并返回到期 notice 与不可领取原因。

`allowed_actions` 由服务器按当前身份、事实、期限计算，例如 `pay, reconcile, close, provide_input, reveal, create_aftersale, view_aftersale, repurchase`。前端不根据一个主状态推断所有权限；调用端点时服务器再次校验。

## 3. 买家与公开目录 API

表中省略 `/api/shop`。返回字段定义见第 4 节；买家接口均按本人订单过滤，无权限对象与不存在对象统一 404。

| 方法与路径 | 输入/查询 | 输出与关键语义 | 页面 |
| --- | --- | --- | --- |
| GET `/config` | 无 | `new_orders_enabled,currency,payment_methods,server_time`；只公开实际可用方法，不含商户秘密；关闭仍保留历史订单入口 | PAGE-01、03 |
| GET `/categories` | 无 | 单层分类 `{id,name,sort_order}` 列表 | PAGE-01 |
| GET `/products` | `q,category_id,page,page_size` | ProductSummary 分页；仅上架，售罄可显示；q 最多 100 字符 | PAGE-01 |
| GET `/products/:product_id` | 无 | ProductDetail；未上架/跨站统一 404 | PAGE-02 |
| POST `/orders` | `product_id,product_version,terms_version,accepted_terms:true,buyer_input` | 首次 201 OrderDetail；原子预留/人工容量与快照。版本变化 409，重新确认；幂等重放返回原单 | PAGE-02、03 |
| GET `/order-requests/:key` | 原下单Idempotency-Key | 仅查当前会话用户、主站及create-order作用域的幂等记录，返回 `{status:recorded或not_observed,order_no:null或字符串}`；只读恢复未知下单结果，不含原请求资料 | PAGE-03 |
| GET `/orders` | `q,status_group,page,page_size` | OrderSummary 分页；快捷筛选 `all,unpaid,delivery,delivered,aftersale`；q 仅订单号 | PAGE-04 |
| GET `/orders/:order_no` | 无 | OrderDetail，无明文秘密、完整买家资料或支付入口 | PAGE-03、05 |
| POST `/orders/:order_no/payment-attempts` | `payment_method,expected_version` | CheckoutResult；有效入口返回原入口；创建先落尝试；未知返回 202 原尝试，禁止换号。新的 key 也不能绕过活跃尝试唯一性 | PAGE-03 |
| POST `/orders/:order_no/reconcile` | `expected_version` | 202 当前 OrderDetail + `poll_after_seconds`；只幂等调度原交易核实，受独立查单限流 | PAGE-03、05 |
| POST `/orders/:order_no/close` | `expected_version` | 本地结束购买；不声称渠道已关，不删除未知付款责任 | PAGE-03 |
| GET `/orders/:order_no/buyer-input` | 无 | 本人必要资料与字段定义，独立 no-store；不包含交付秘密 | PAGE-05 |
| PUT `/orders/:order_no/buyer-input` | `expected_version,values` | 更新固定字段；服务端判断齐全、计算/保持原 SLA；不可通过重复提交重新起算 | PAGE-05 |
| POST `/orders/:order_no/reveal` | `delivery_id,intent:view或copy,field_key?` | RevealResult，逐次校验；copy 返回指定字段。到期/风险/退款均拒绝；不再分配库存 | PAGE-05 |
| GET `/orders/:order_no/aftersales` | `page,page_size` | 按时间排序的案件与公开记录；不含内部备注、秘密或原始资金证据 | PAGE-06 |
| POST `/orders/:order_no/aftersales` | `issue_type,description,desired_resolution` | Aftersale；仅一活动案件，已有则返回原案件；同 key 改内容冲突 | PAGE-06 |
| POST `/aftersales/:case_id/messages` | `expected_version,body` | 追加公开消息并返回案件版本；不能改写旧消息 | PAGE-06 |

`issue_type`：`unusable / mismatch / not_delivered / other`；`desired_resolution`：`investigate / replace / refund`。期望结果不授权直接发货或退款。首版文字消息足以完成沟通，不增加附件服务；需附件再评估既有安全上传实现。

`buyer_input` 最多三个固定、稳定 field_key，仅 `account_identifier / contact_email / order_note` 这类必要资料；不接收密码、验证码或任意动态字段。真实字段由商品版本约定。

### 3.1 下单与支付的 UI 顺序

商品详情取得版本与条款 → 未默认勾选条款 → 提交订单并取得 `order_no` → 导航付款页 → 用户明确选择实际可用付款方法 → 创建/恢复支付尝试。

`GET /products/:id` 的价格与版本是用户确认依据，不需要再造通用报价服务。`POST /orders` 在同一事务比较 product_version/terms_version 并取真实价格，变化时 409 返回新商品摘要。已建订单的支付金额只取快照。

返回页只携带随机订单号，不携带秘密或支付成功断言。跳转后读取订单，必要时显式 reconcile；不要自动生成第二单。

支付页保存同次尝试的 key、原 payment_method 和 expected_version。HTTP 冲突本身不能证明可换付款方式：创建 attempt 已提交后也可能在保存入口时得到 409，或在后续授权读取时得到 404。只有第 1.2 节的未提交证明且此前从未不确定，或成功响应明确返回经服务端证据确认的 closed/failed，才能释放相应请求。订单 `allowed_actions` 仅决定当前按钮资格，不是此前请求未提交的证据。

若下单已发出但尚未获得order_no，刷新后用仅包含key/身份绑定的本地恢复标记查询 `/order-requests/:key`，取得订单后正常读取。`not_observed` 仅指当前未观察到已提交记录，**不能证明原请求未运行或最终失败**；保留同key，必要时重新确认/输入原资料并按原key重放，不能自动换key新建。幂等记录和订单在同事务提交，查找不能泄露其他actor是否用过该key。

## 4. 关键 DTO

### 4.1 ProductSummary / ProductDetail

Summary：`id,version,name,summary,category_id,cover,price,delivery_type,availability`。

- `cover`：受允许的同源资源引用、width、height、alt；后端不抓取管理员提交的任意远端 URL。首版可使用审定静态素材路径，不建设独立媒体管理系统。
- `delivery_type`：`automatic / manual`；`availability`：`available / sold_out / capacity_full / unavailable`。公开端不暴露库存标识或精确秘密数量。
- Detail 增加 `product_type,specification,delivery_fields,usage_conditions,delivery_policy,view_policy,warranty_policy,entitlement_policy,refund_policy,buyer_input_schema,terms_version,can_purchase,unavailable_reason_code`。
- SLA、时区、服务时段、权益期限、质保和查看期限均采用结构化字段 + 对客摘要，原样进入订单快照。禁止只保存渲染后的文案，再靠文本解析计算截止时间。

政策 DTO 采用以下受限结构；除明确可选项外，上架时必须完整，草稿可空：

| 政策 | 字段和计算约束 |
| --- | --- |
| `delivery_policy` | `{mode,public_terms,sla:null或{clock,duration_minutes,timezone,weekly_windows,pause_on_missing_input}}`；mode 同 delivery_type。自动交付无固定秒发承诺；人工 clock 为 elapsed 或 business，duration_minutes 为正整数，timezone 为 IANA 名称 |
| `weekly_windows` | business 必填的 `{weekday:1..7,start:"09:00",end:"18:00"}` 数组；每周最多 14 段，同日不重叠，跨午夜拆段，elapsed 时为空。未营业区间不计工作时长；首版无假期日历/复杂排班引擎。实际承诺需要节假日规则时先调整政策/范围，不能静默按自然时长算 |
| SLA 起算 | 取可信付款完成时间与原必要资料首次齐全时间的较晚者；按快照服务日历计算一次绝对 delivery_due_at。工作时长按本地窗口对应的真实时间区间累计，DST 缺失时间跳过、重复时间按真实经过分钟累计，结果存UTC；重复提交不能重新起算 |
| SLA 暂停 | pause_on_missing_input 默认不替运营决定，须显式配置。只有事先约定且后台明确请求原定必要字段时记录暂停起止；单次暂停只能结算一次，保留原deadline与调整事件，不能通过普通备注或风险解除改期 |
| `view_policy` | `{duration_seconds,first_start:"delivered_at",replacement_start:"delivered_at",expired_action:"deny_reveal",public_terms}`；duration_seconds 正整数且计算不溢出；每次交付保存自身绝对截止。D1 采用交付完成起算的首版建议，真实时长仍待运营输入 |
| `warranty_policy` | `{mode,start_event,duration_seconds,fixed_end_at,public_terms}`；mode 为 none（无额外商业质保但保留售后规则）、duration 或 fixed_end；duration 的 start_event 为 paid_at 或 first_delivered_at，fixed_end 只使用明确UTC截止，其余无关字段为null。补发不重算原质保；不同承诺须先修改契约 |
| `refund_policy` | `{goods_refund_mode:"full_only",execution:"manual_original_channel",public_terms}`；公开条款不能为空；不由前端文本决定退款资格 |
| `entitlement_policy` | `{mode:"duration"或"fixed_end"或"described",start_event,duration_seconds,fixed_end_at,public_terms}`；记录实际商品有效期和起算依据；described 只用于服务结果等无法统一成时间的权益，说明必填，不承诺无限有效；补发不自动延长 |

商品字段 schema 版本与政策算法版本随订单快照保存。所有期限/分钟、时区和日历在上架及下单时验证，上限由可表达时间范围与已确认经营配置共同约束；不得产生负数或溢出日期。首次起算以真实事实时间为准，不能用通知发送时间或买家第一次点击替代。

### 4.2 OrderSummary / OrderDetail

Summary：`order_no,version,product_snapshot:{name,cover,specification},quantity:1,total,created_at,purchase_state,payment_state,delivery_state,aftersale_state,refund_state,display_status,notice_codes,allowed_actions`。

Detail 在此基础上增加：

| 字段 | 约定 |
| --- | --- |
| `product_snapshot` | 完整成交规则、价格与版本，不从现商品详情覆盖 |
| `reservation_expires_at,paid_at,delivery_due_at,delivered_at,server_time` | nullable UTC 时间；未开始人工计时不伪造 due_at |
| `payment_summary` | 预期 Money、有效实收 Money、公开核实摘要、上次查询时间；不含商户凭据或未授权原始回执 |
| `current_delivery` | `id,version,delivered_at,view_expires_at,masked_fields,instructions,can_reveal,unavailable_reason_code`；默认没有 value |
| `delivery_history` | 历史版本的脱敏元数据、补发关联和停显原因；历史秘密不随详情返回 |
| `buyer_input_status` | 齐全/缺项 field_key、规则摘要；完整值通过独立端点访问 |
| `risk_hold` | 对客可见原因与开始时间；内部证据不下发 |
| `refund_summary,extra_payment_summary` | 商品款与额外款分开，分别列 Money、状态、公开进度；不把额外款完成当作整单退款 |
| `timeline` | `event_id,event_type,occurred_at,public_summary`；只含已发生事实，分页上限与详情首屏截断可继续读取 |

若时间线超过详情上限，使用 GET `/orders/:order_no/events?page=...&page_size=...`，与相同订单授权规则一致。单次详情最多带最近 20 条，不能无限膨胀。

### 4.3 CheckoutResult

`{order,attempt:{id,state,expires_at,last_checked_at},checkout:null或{kind,url?,qr_payload?,expires_at},poll_after_seconds?}`。

attempt state：`creating / ready / unknown / closed / failed / paid`。`closed/failed` 仅在可信渠道证据确认原交易不可再付款时允许新尝试；历史 closed 后迟到可信实收仍保留。未知不能当失败。

checkout kind 首版按选定渠道仅实现其需要的 `redirect / qr`；只能返回受信渠道 HTTPS 地址或协议约束二维码数据。不渲染支付商提供的任意 HTML，不允许客户端指定 return/callback URL，不返回完整签名配置。支付入口只在结账相关请求返回，禁止日志/埋点采集。

### 4.4 RevealResult

```json
{
  "success": true,
  "message": "",
  "data": {
    "order_no": "demo-order",
    "delivery_id": "demo-delivery",
    "delivery_version": "1",
    "view_expires_at": "2026-11-01T08:00:00Z",
    "server_time": "2026-10-27T08:00:00Z",
    "fields": [{"key":"account_identifier","label":"账号","value":"demo@example.test"}]
  }
}
```

此为无效演示数据。`copy` 也通过此端点重新授权，不设置可绕过到期检查的复制专用静态 URL。前端直接请求并仅留组件内存，不进入 TanStack Query/Mutation Cache、Zustand、持久化或遥测。审核文档中的展示窗口并不承诺能撤回已经送达浏览器的数据。

## 5. 管理 API

以下省略 `/api/admin/shop`。GET 列表使用分页与服务端白名单筛选；所有修改要求版本、幂等 key、适用时的理由和操作证明。返回管理 DTO 必须显式挑字段，不能直接序列化数据库行。

### 5.1 配置、商品与库存

| 方法与路径 | 请求/响应要点 |
| --- | --- |
| GET、PATCH `/settings` | 查询/更新 `new_orders_enabled`、首发渠道/币种选择、预留和未付款单限额、运营提醒阈值及保留政策；支付凭据仍由原支付设置管理。GET 的 `payment_methods` 只读投影原支付配置，PATCH 不接受独立方法列表；修改带 expected_version 和 reason。关闭只停止新增，密钥缺失或参数未确认不得启用 |
| GET `/overview` | `from,to,currency`；默认近30天、已配置单币种，响应标注实际区间。订单新增数及实收按订单创建/渠道付款时间统计，退款按成功资金调整核实时间统计；待办覆盖所有未解决责任，不受区间截断。不混币计算净收益，合计超过金额契约时返回 `SHOP_AMOUNT_LIMIT_EXCEEDED` |
| GET、POST `/categories` | 单层名称/排序；name 必填，POST 返回新 category；无层级分类框架 |
| PATCH `/categories/:category_id` | name、sort_order、expected_version；有关联商品的分类不做物理删除 |
| GET、POST `/products` | 列表筛选含草稿/上架/归档；新建允许经营参数缺失的草稿 |
| GET、PATCH `/products/:product_id` | 详情/编辑商品字段；Money 写入仅限管理端，白名单校验；关联订单或库存后禁止修改交付类型 |
| POST `/products/:product_id/publish` | expected_version；完整经营参数、结构化政策和渠道可用性检查；不通过返回字段错误，不能用演示值兜底 |
| POST `/products/:product_id/unpublish`、`/archive` | expected_version、reason；保留库存、历史订单和履约责任，不隐含风险暂停 |
| GET `/inventory` | product_id、state、batch_id、page；只返回脱敏标识、状态、归属、批次和时间 |
| POST `/inventory/import-preview` | product_id、product_version、template_version、rows；受 body 上限；返回规范内容摘要、脱敏行号错误及 valid/duplicate/invalid 数量，不存明文临时文件 |
| POST `/inventory/import-commit` | 相同规范 rows、preview_digest、product_version、template_version；重新校验全部格式和 DB 去重，整批事务全成或全不成；返回 batch_id 与成功数 |
| GET `/inventory/import-requests/:key` | product_id；仅原管理员/主站/import-commit作用域；返回 `{status:recorded或not_observed,batch_id:null或字符串,inserted_count:null或整数}`；不重放库存正文 |
| POST `/inventory/:unit_id/quarantine` | expected_version、reason；禁止新预留/交付；若已关联订单同时形成交付异常，不靠库存隔离代替订单风险暂停 |
| POST `/inventory/:unit_id/reveal` | expected_version（库存version）、reason + 操作证明；单条、no-store、独立审计；已售秘密按原订单与保留政策检查，不提供批量导出 |

导入单批建议上限 200 条 / 1 MiB（两者先达到者生效），服务端约束并同步 UI。preview 无入库副作用且不需幂等键；commit 必须幂等。两个接口请求体均属秘密，禁入 access log、错误详情和自动管理员请求体审计。无需新增导入草稿表/后台解析任务；预览与确认间保持短期组件内存，离开清空。

`rows` 每项为 `{identity,secret_fields:[{key,value}]}`。`identity` 必须显式提供，规范化版本1只去掉首尾空白、区分大小写，不猜测账号字段；禁止换行/NUL，最长4096字节。它只用于域隔离的 HMAC 去重，不单独存储明文、不随列表或错误返回。预览错误字段为 `errors:[{row,code}]`，行号从1开始。库存DTO将内部sold投影为delivered；管理员单次秘密展示最多60秒，已售项还受原订单查看期限限制。

管理商品状态为 `draft / published / unpublished / archived`；unpublish停止公开销售但保留可编辑项，archive仅作后台归档筛选，不删除历史。公开商品DTO只出现published记录。接口和DTO字段名 `product_type` 对应后端商品类型，`delivery_type` 固定automatic/manual，不能混用二者。

导入commit未知时按原key查询以上结果；`not_observed` 与下单恢复含义相同。需要重传原内容时由管理员重新选择输入，以同key校验摘要，禁止在浏览器持久保存秘密来实现自动恢复。管理员主动查看未售库存前将available原子转为quarantined并留痕；reserved拒绝独立查看，避免破坏买家预留；已售项按关联订单当前交付及风险/退款/查看资格检查，不因管理员角色绕过已禁止的揭示。

### 5.2 订单、履约与售后

| 方法与路径 | 请求/响应要点 |
| --- | --- |
| GET `/orders`、`/orders/:order_no` | 筛选 order_no/user_id/product_id/时间/交付/退款/风险；详情包含快照、脱敏事实、可操作项；不返回全部秘密 |
| GET `/orders/:order_no/buyer-input` | 按履约需要读取必要完整资料、记录访问；列表始终脱敏 |
| POST `/orders/:order_no/claim` | expected_version；登记处理责任人，不改变收款或证明交付完成 |
| POST `/orders/:order_no/request-input` | expected_version、missing_field_keys、public_reason；限原字段，保持原 SLA 起点 |
| POST `/orders/:order_no/deliveries` | expected_version、secret_fields、instructions、reason、证明；仅已付款人工订单首交付；原子完成，重复请求返回脱敏既有交付 |
| POST `/orders/:order_no/retry-delivery` | expected_version、reason；对可恢复自动交付异常调度原任务，不是强制已交付 |
| POST `/orders/:order_no/risk-hold` | expected_version、internal_reason、public_reason、evidence_refs；原子阻止新交付/补发/揭示，保留资金事实 |
| POST `/orders/:order_no/risk-release` | expected_version、reason、evidence_refs、证明；只解除风险事实，重新检查所有限制 |
| POST `/orders/:order_no/notes` | expected_version、visibility:internal或public、body；追加记录；公开/内部严格分离，禁止秘密正文 |
| GET `/aftersales`、`/aftersales/:case_id` | 活动/历史案件、原条款、进度、相关交付和资金责任 |
| POST `/aftersales/:case_id/messages` | expected_version、visibility、body；请求补充或解释，不改历史 |
| POST `/aftersales/:case_id/decisions` | expected_version、decision:accept或resolve或reject或close、public_reason、internal_reason?；关闭不能清除未结资金或交付责任 |
| POST `/aftersales/:case_id/replacements` | expected_version、order_version、reason、证明；自动商品由服务端分配库存；人工商品附 secret_fields/instructions；仅一次有效补发对应此处理，退款/风险/争议冲突时拒绝 |
| POST `/orders/:order_no/deliveries/:delivery_id/reveal` | expected_version（订单version）、reason + 证明绑定delivery_id；管理员单版本查看，按有效范围及保留规则控制；已销毁的秘密不通过审计历史恢复 |

替换交付记录的创建不接受“覆写旧密码”。补发提交失败不能先把旧版本停显后丢掉新版本；成功事务才更新当前版本、旧版停显和库存归属。

### 5.3 可信实收、差错与退款

| 方法与路径 | 请求/响应要点 |
| --- | --- |
| GET `/receipts`、`/receipts/:receipt_id` | 按渠道/商户引用/环境/交易号/处理状态查询；可以没有 order_no；详情给实收、差异、证据摘要、责任人和处理历史 |
| POST `/receipts/:receipt_id/assign` | expected_version、assignee_id、reason；只分配处理责任，不改变款项归属 |
| POST `/receipts/:receipt_id/reconcile` | expected_version、reason；按原持久交易引用核实与重跑校验；不接受任意目标 user_id/order_no 改绑 |
| POST `/receipts/:receipt_id/refunds` | expected_version、kind:goods或extra或mismatch、reason、evidence_refs、证明；金额从原实收和未结调整推导。goods 同时锁订单并暂停履约；同收款已有未结责任返回该责任或冲突 |
| GET `/refunds`、`/refunds/:refund_id` | 商品款、额外款、差错退款责任；关联收款、尝试、证据与公开状态 |
| POST `/refunds/:refund_id/attempts` | expected_version、reason、preflight_evidence_refs、证明；外部操作前登记唯一尝试。第一次/失败后重试均核实无成功、在途、争议冲突；未知不得新建 |
| POST `/refunds/:refund_id/attempts/:attempt_id/record` | expected_version、channel_ref?、operated_at、evidence_refs、public_note、证明；登记已操作/结果未知，不因填写单号直接成功 |
| POST `/refunds/:refund_id/attempts/:attempt_id/verify` | expected_version、conclusion:succeeded或failed或unknown、evidence_refs、证明；可信核实商户/交易/退款引用/金额/币种/结果；证据冲突不能成功 |
| POST `/refunds/:refund_id/cancel` | expected_version、reason、evidence_refs、证明；只在证据确定未发生且无未知/在途/争议时撤销批准 |
| POST `/receipts/:receipt_id/adjustments` | expected_version、kind:external_refund或chargeback、provider_adjustment_ref、money、state:pending或succeeded或failed或disputed、evidence_refs、reason、证明；只登记经过核实的后续事实；相同渠道调整去重 |
| POST `/receipts/:receipt_id/adjustments/:adjustment_id/review` | expected_version、新证据及核实结论、reason、证明；追加核实事件并更新当前投影，不覆盖旧事实；争议冲突冻结相关履约/退款 |
| POST `/evidence` | `receipt_id,receipt_version,source,provider_transaction_id,provider_refund_or_adjustment_id?,money,observed_status,observed_at,reference,summary,correction_of?` + 证明；追加受控渠道核实材料，返回 evidence_id；首版不上传文件、不代抓URL |
| GET `/evidence/:evidence_id` | 限主站平台管理员、no-store；按对象关联授权，只返回必要的受控证据摘要；不接受任意 URL 代理抓取 |

`evidence_refs` 指向受控记录中的渠道身份、原交易/退款引用、金额币种、结果、核实来源/时间/人员等结构化证据；首版通过 POST `/evidence` 在管理端登记这些字段并返回 ID。记录只能追加、更正另建关联证据；不能仅有“已看截图”勾选。无需建设文件上传/公网凭证链接服务。商品款只全额；外部部分调整必须核实剩余责任，不能借接口开通主动部分退款。

人工 evidence 的 source 只允许 `merchant_console`，来源描述/summary 最多2,000字符、各渠道引用最多191字符、reference最多500字符；商户与环境由所选收款的受控配置加载，不由自由文本替换。服务端渠道查单/回调证据使用 `provider_query/provider_webhook`，浏览器不得冒充这些来源。observed_status仅表示材料中的陈述，**创建 evidence 本身不更新实收/退款成功**；verify动作另行核实身份、金额、已确认/在途/争议冲突和证据可追溯性。后台材料需可回到支付商核查，任意文本、自制截图或仅管理员勾选不足以确认；来源无法核实时保持 unknown。首发渠道的人工核验步骤在联调前冻结并保存核实人和时间，软件只能执行权限/一致性约束，不能把人工陈述变成密码学证明。

首版不调用渠道自动退款 API。管理员真正点击支付商退款和回来登记之间存在外部操作窗口：本地先持久化尝试并展示“待核实”，页面丢失后继续同一尝试，未知结果禁止再操作。账面投影不能阻止他人在渠道后台另行操作，故每次重试仍必须核对渠道事实。

退款收款身份、金额/币种不允许被普通 PATCH 修改。正常商品款39元而实收短款19元时，只能按19元实收处理差错；不会批准39元退款。所有记录都必须保留最初可信事实。

## 6. 回调及内部任务契约

- 支付渠道选定前不虚构一个“已可用”的 webhook 路径。默认保留所选渠道现有验签入口，在验签后按持久化 business_kind/本地支付引用路由；如渠道允许独立 URL，则另注册商城入口但仍使用同一验证器。具体路径在渠道能力表冻结时写入。
- 所有通知先验证渠道身份、签名、商户与环境；交易前缀只能定位候选业务。必须和原持久化尝试/业务登记一致，不调用另一业务核销器。无法映射的可信收款进入独立差错，不凭 URL 或前缀补造归属。
- 可信回执持久化并安排恢复后再返回渠道成功 ACK；保存失败按该渠道重试协议返回，不以 HTTP200 假成功。渠道响应格式由现有适配器定义，非通用 JSON envelope。
- 已保存同一收款通知可以 ACK 并重跑未完成处理；签名不可信数据不能创建“可信实收”。禁止把原始 webhook 正文（可能包含敏感字段）直接写普通日志。
- 持久任务只携带内部引用和 schema_version，不含账号密码、卡密、买家资料、支付密钥或证明。事件重放重新读最新订单/退款/风险事实。
- API 请求、回调和 worker 共同遵守同一 model 事务入口；不能分别实现三套“付款成功”代码。未知结果不设无限内存轮询，超阈值仍保留后台责任。

## 7. 商城错误码与客户端处理

| HTTP / code | 条件 | 客户端处理 |
| --- | --- | --- |
| 400 `SHOP_VALIDATION_FAILED` | 字段/长度/金额/格式不合法 | 展示 field_errors，禁止回显秘密原值 |
| 401 既有 AUTH 错误 | 登录失效或禁用 | 清秘密和身份查询缓存，重新登录后回原单，不自动下单 |
| 403 `SHOP_SESSION_REQUIRED` / `SHOP_MAIN_SITE_ONLY` | PAT/API Key 或非主站 | 禁止继续，无越权回退入口 |
| 403 既有 SECURITY 错误 | 证明缺失/过期/对象不符/已消费 | 按明确动作重新验证，不自动重放敏感写操作 |
| 404 `SHOP_NOT_FOUND` | 无权访问/不存在/未上架目录 | 统一提示，不泄露他人对象存在 |
| 409 `SHOP_PRODUCT_CHANGED` | 展示版本与真实价格/条款不同 | 展示新摘要，重新确认，不静默收新价 |
| 409 `SHOP_OUT_OF_STOCK` / `SHOP_CAPACITY_FULL` | 原子预留失败 | 不生成可付款单；已有订单按自身事实展示 |
| 409 `SHOP_NEW_ORDERS_PAUSED` / `SHOP_PENDING_ORDER_LIMIT` | 停新单/待付款单超限 | 提供既有订单入口，不能关掉历史处理 |
| 409 `SHOP_IDEMPOTENCY_CONFLICT` / `SHOP_VERSION_CONFLICT` | 同key异内容/旧版本 | 前者停止自动重试；后者刷新对象后复核。付款创建仍按 request_outcome 和先前不确定记录处理，不因 409 自动换 key |
| 409 `SHOP_REQUEST_UNRESOLVED` | 订单写入已提交或已命中幂等结果，但响应读取失败 | 保留原 key 和恢复标记，查询或按原内容重放；不泄露已失去访问权限的订单，不表示未提交 |
| 409 `SHOP_PAYMENT_UNRESOLVED` | 旧尝试不可确定且请求新交易 | 回到原订单核实；常规恢复可用202返回原尝试 |
| 409 `SHOP_ACTION_NOT_ALLOWED` | 最新事实不允许此动作 | 用返回的脱敏 allowed_actions 刷新，不绕过 |
| 409 `SHOP_REFUND_CONFLICT` / `SHOP_EVIDENCE_CONFLICT` | 退款/在途/争议/证据不一致 | 保留原责任，进入核实；不得提示直接再退 |
| 403 `SHOP_REVEAL_BLOCKED` | 风险/退款/争议禁止领取 | 清当前明文，只显示公开原因 |
| 410 `SHOP_REVEAL_EXPIRED` | 当前版本查看期限已过 | 清明文；保留售后和历史进度入口 |
| 429 `SHOP_RATE_LIMITED` | 下单/查单/领取/导入限流 | 按真实 Retry-After 或有限退避，不改变资金状态 |
| 503 `SHOP_CHANNEL_UNAVAILABLE` | 渠道不可用 | 下单前禁支付；已创建尝试仍继续核实 |
| 503 `SHOP_SECRET_UNAVAILABLE` | 密钥缺失/解密或内容校验失败 | 不回传密文；后台保留异常，不重新分配或收费 |
| 500 `SHOP_INTERNAL_ERROR` | 未预期故障 | 返回相关请求标识，保留key并查询原资源；不将其解释为无交易 |

付款创建的未提交标记只表述本次创建事务的事实，不承诺历史未知尝试已关闭。`service.ShopPaymentNotCommittedError` 是唯一产生该标记的内部类型；裸业务错误、数据库/传输/提交错误、创建后失败均不能推断或补写 `request_outcome`。

错误 data 只允许脱敏当前资源、field_errors、retry 信息，以及上述受限的 `request_outcome:"not_committed"`；禁止 ORM 错误、SQL、堆栈、原始请求、账号密码和其他用户信息。未知枚举/畸形 DTO 在前端作为可恢复协议错误处理，不能默认成“成功”。

## 8. 联调契约验收

1. 对 Money、时间、nullable 字段、枚举、分页、业务错误、202 未决与 version 冲突形成前后端共用脱敏 fixture；测试行为而非快照整份大 JSON。
2. 正常 GET/错误/日志不能出现 secret_fields.value；唯一秘密响应遵守专门 reveal 与 no-store 约束。自动管理员审计对导入/交付请求须先确认只写标识，不能依赖通用正则擦除。
3. 下单幂等、最后一件库存、迟到款、退款批准对交付、copy 到期、跨用户/跨租户/跨业务回调分别做真实边界测试，见[质量验收文档](./kk-shop-quality-and-acceptance.md)。
4. 本地实现按本文 DTO 和 scope 接入；真实商户验收仍需确认首发 method、回调 ACK、查单关闭能力，并核对第 1 节证明链。未经核实的支付方式不出现在生产 config 响应。

2026-10-28 补充证据：本地 TLS 假网关签名回调/查单、TopUp/quota 隔离及付款未提交错误契约的定向回归已通过。真实 Epay 付款、到账、渠道关闭和退款取证由用户接手，代理未验证；商城尚未上线。数据库和浏览器的具体版本、命令及未覆盖项统一见[实施状态](./kk-shop-implementation-status.md)。

`not_committed` 只覆盖付款创建专用路径。普通读取仅将记录不存在/无权访问映射为隐藏资源的 404，数据库故障保留为 500。`shopRespondOrder` 的写入调用均位于提交或幂等命中之后；后置读取失败返回 `409 SHOP_REQUEST_UNRESOLVED`，不附未提交标记，也不泄露订单。已通过确定性故障注入：下单提交后、响应鉴权前停用用户，返回未决错误；普通 GET 仍为 404；恢复用户后同 key 重放返回原订单，订单数与人工名额占用均为 1。前端首次收到该错误也保留 key 与恢复标记；此结论不扩展为所有写操作的穷尽验证。

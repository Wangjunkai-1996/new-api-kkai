# KKAI 发票中心开发技术文档

- **版本**：v1.3 实施对齐，2026-10-23
- **面向**：后端、前端、测试及技术评审人员。
- **配套**：[PRD](./invoice-center-prd.md)、[UI 实现与验收规格](./invoice-center-ui-spec.md)、[冻结设计参考](./invoice-center-design-preview.html)。
- **状态**：业务代码、11表v17 schema、独立迁移应用协议与基础设施控制器已在本地实现，尚未安装控制器或生产启用；具体证据与剩余验收见[实施记录](./invoice-center-implementation.md)。本文本身不授权迁移、发布或生产操作。
- **使用规则**：业务与数据契约以本文及 PRD 为准，布局、控件和响应式以 UI 规格为准。三者发生冲突时先修正文档，不能由前端或后端单方面删改功能。冻结原型用于视觉参照，不是生产数据或账务依据。

## 1. 实施范围与不可省略项

本期支持：充值及直接付款会员/订阅订单、整笔合并申请、审核通过/拒绝、正式票据上传后明确交付、用户确认/异议、管理员回复/更正补发、历史文件、常用抬头、站内通知、补充邮件通知。审批通过不等于税务发票已经开具。

以下是开发统一口径：

1. 常用抬头的新增、编辑、选择、软删除和安全持久化属于一期；不能做成空页签或仅浏览器缓存。
2. 一次最多 50 笔付款；不拆单、不部分开票，不提供提交后撤回、删除、导出、OCR、自动税务开票或自动红冲。
3. 同一笔真实付款即使同时存在充值/旧订阅镜像，也只能形成一份候选及一份有效占用。唯一性基于规范付款身份，不能只锁来源行 ID。
4. 金额只来自核验的真实实收；未知金额显示“待核验”，不得使用标价、额度或浮点数估算。首批仅开启配置完整的 CNY 来源。
5. 首期只有主站用户和主站平台管理员；租户开票能力在卖方主体、税务规则及权限方案完成前关闭。tenant 由 Host scope 决定，不由客户端指定。
6. 订单筛选/翻页保留选中项，表头只“选择本页”；不可申请订单独立解释。桌面保留三步流程及右摘要，移动端保留底部金额操作栏。
7. 管理端保持设计稿的“左侧申请队列、右侧详情工作区”。正式环境没有“用户端/管理端”角色模拟切换。
8. 回复说明是同版本重新确认；更正补发才创建新文件版本。上传草稿不改变当前交付版本，也不隐藏用户已收到的文件。
9. 已确认仍允许反馈问题。确认不代表税务核验或放弃更正权利；异议不自动退款、撤票或释放占用。
10. 财务暂停保存原阶段并保持占用；曾交付或已实际开具的票据，不能通过拒绝释放付款重新申请。

## 2. 仓库落点及可复用能力

### 2.1 现状证据

| 仓库位置 | 已确认事实 / 实施含义 |
| --- | --- |
| `model/topup.go`、`model/invoice_payment_receipt.go` | `PaidMinor/Currency`是订单预期条款；只有Epay真实签名回调写入并匹配的独立`InvoicePaymentReceipt`才能证明实收。手工补单和历史浮点估算不生成核验收据，其他渠道不能仅凭success开放。`GetUserTopUps`使用`topUpQueryCutoff()`，不可代替全历史开票候选查询。 |
| `model/membership.go`、`model/membership_order.go` | `MembershipOrder` 与 `MembershipPaymentReceipt` 可核验金额、币种、回执、一次性付款及异常；不能只读套餐 DTO 或订单 success。 |
| `model/subscription.go` | 旧 `SubscriptionOrder.Money` 是浮点，可能镜像成 TopUp；未建立可信付款映射前不可开票。 |
| `model/request_ticket.go`、`controller/request_ticket.go` | 可借鉴 CAS、owner/tenant 查询、事件/未读模式；现有工单实际限主站，不代表具备租户工单能力。不能借用工单表承载付款占用。 |
| `middleware/auth.go`、`middleware/access_token_routes.go` | 使用 `UserAuth/AdminAuth` 并验证 `GetSessionAuthIdentity`，补充 session-only 路由策略。当前业务 API 使用 session Bearer；不要照搬 refresh/logout 专用的 `SessionCookieOriginGuard`。 |
| `controller/reseller_finance_admin.go` | 沿用 `role >= RoleAdminUser && TenantID == PlatformTenantID` 的主站管理员边界；一期不另造“财务角色”或 RBAC 平台。 |
| `service/video_asset_store.go`、`service/video_assets.go` | 可复用私有对象 Get/Put/Delete 存储适配；新增 invoice key 前缀、业务表和授权下载，不复用视频公开展示策略。 |
| `service/kkai_outbox.go` | 已有 `EnqueueKKAIOutboxEvent(ctx, tx, ...)`、唯一 event key、重试和 worker；增加 invoice topic/handler，不再新建通知任务框架。 |
| `pkg/kkaimigrate/`、`cmd/kkai-migrate/invoice_migration.go` | 新表走版本化migration、三数据库方言、runtime contract/schema gate；生产写入使用独立proof约束的v17控制器协议，普通target17拒绝写入。禁止路由或启动时AutoMigrate补表。 |
| `web/default/components.json` | `base-nova`、Base UI、Hugeicons、Tailwind CSS variables；复用当前 shell 和组件，不直接移植原型 DOM/图标依赖。 |

后端沿用 `router → controller → service → model`；建议 `router/invoice.go`、`controller/invoice*.go`、`service/invoice*.go`、`model/invoice*.go`。controller 负责身份、DTO、错误映射；资格策略、事务与状态机集中在 service/model，列表和写接口共用策略。

前端使用 React 19、TanStack Router/Query、现有 axios 实例、RHF + Zod、Base UI/Hugeicons/Tailwind，业务代码放 `web/default/src/features/invoice/`。不引入新的页面框架、支付台账平台或通用通知中心。

### 2.2 配置与能力门槛

复用现有设置/Option 基础设施保存版本化发票政策，提供只读 policy API；本期不新增一整套财务设置后台。上线前通过受控配置流程录入卖方名称、卖方标识、允许来源/渠道、币种及 exponent、票种、条件必填字段、税目兼容分组、手续费口径、留存规则和可选服务时限。记录政策版本并快照到申请。

`minimum_amount_minor` 是以 CNY 分为单位的十进制整数字符串，默认 `"20000"`（200 元），配置可提高但不得降低到 200 元以下。新申请按所选订单的核验实付合计判断，允许多笔合并；后端在创建事务中强制执行，低于门槛返回 `422 INVOICE_BELOW_MINIMUM`。候选和 quote 保留单笔资格，避免阻止小额订单合并。既有申请的幂等重放、审核、交付和更正不追溯当前最低金额。

已开具或交付的申请永久保留付款占用，确认、异议、暂停和补发不会使订单重新可选。政策版本升级时，旧申请不能直接继续交付；管理员须暂停并通过现有恢复操作进行重新审核，确认卖方/税目兼容、票种及资料满足当前要求后，留痕接纳新政策版本。身份或税目不兼容时继续失败关闭，不能通过拒绝已开具申请释放付款。

缺少卖方、规则、私有存储、专用加密密钥或 schema 时，对**新建/交付**失败关闭并返回明确能力原因；不能猜测税务规则。停止新申请不应阻止已交付文件的合法历史访问。接口和 UI 都读取能力，不能只隐藏侧栏。任何新增来源先完成付款及退款路径审查和针对性验收，再开启对应 capability。

## 3. 付款资格、规范身份和金额

### 3.1 资格矩阵

| 来源 | 可申请条件 | 不得替代的依据 |
| --- | --- | --- |
| `topup` 充值 | 成功、有效付款时间；签名回调或商户认证查单产生的唯一verified `InvoicePaymentReceipt`与订单owner/tenant、渠道、实收最小单位及币种匹配；无已登记退款/拒付/冲正、来源已开启 | `Amount`额度、`Money`浮点、`TopUp.PaidMinor/Currency`应付条款、手工补单或赠送额 |
| `membership` 新会员 | 直接一次性付款；订单成功；唯一已核验主回执；Actual 金额/币种/渠道与订单条款一致；额外回执均已 resolved 且有财务处理记录；无未解决异常、退款/拒付 | 当前套餐价、权益剩余、会员是否到期；额外回执金额不计入开票 |
| `legacy_subscription` 旧订阅 | 当前默认需人工核验；补齐可信持久付款证据、映射和规则后才能开启 | 浮点 `Money`、计划标价或成功状态 |
| 旧订阅镜像 TopUp | 先映射同笔原付款，展示一次；证据不足需核验 | 把两个来源当两笔钱 |
| 钱包购买/兑换/赠送/管理员授予 | 不生成新现金开票项目，给出原因和原充值入口（如能准确定位） | 钱包扣款额、会员额度 |

会员过期、额度用完、升级或降级不改变已经证实的真实付款资格。未付款/关闭/退款来源只在“暂不可开票”中展示允许公开的信息。部分退款一期不重新计算剩余额度，整笔进入财务核查。

### 3.2 Canonical payment identity

资格解析器返回稳定`canonical_payment_key`，由经验证的渠道回执身份规范化得到，并与来源建立确定映射；充值和会员必须对同一回执派生相同key，跨source/owner只能存在一个活动claim，其他owner的占用不返回申请public ID。持久化规范化版本，不能因展示字段或密钥轮换而变化。首期政策强制providers仅`["epay"]`且限主站单一商户，未来多商户必须先加入不可变商户命名空间。源系统没有完整渠道交易号时，只有经审计可证明唯一、无镜像歧义的原始付款锚点才可使用。不能按金额、用户名、日期近似合并，也不能对未知别名直接放行。

- 同一 canonical key 在候选列表只出现一次，保留原始来源标签及必要关联摘要；历史镜像不再作为独立可选付款。
- claim 唯一约束是 `(tenant_id, canonical_payment_key)`；仅 `(source_type, source_order_id)` 不足以防跨模型重复。
- item 保留 canonical key 和原始引用快照；请求同时包含同一引用或同付款别名时返回 `DUPLICATE_PAYMENT`，不得静默重复累计。
- 同一个规范付款锚点是申请、审批、交付及退款/冲正处理的共同锁定边界。接入某来源时必须调整该来源的退款/异常写入流程，在同一事务中核查 claim 并标记财务暂停；不能只在提交前做一次检查。
- 已存在付款别名但无法稳定关联、付款异步冲正无法可靠感知的来源，维持关闭；人工核验不能通过前端改 `eligible` 绕开。

### 3.3 精确金额与合并

数据库用 `int64` 最小货币单位，正数、加法溢出及财务金额上限均由服务端验证；客户端合计只用于显示。API 的所有金额（包括税额）使用十进制整数**字符串**，如 `"119900"`，未知用 `null`。TypeScript 以字符串/BigInt 做精确加总和格式化，不经 `Number` 浮点累计或丢失精度；货币小数位从 policy 读取，CNY exponent 为 2。

单张申请必须属于同一 user、tenant、seller、currency、票种及兼容税务组。后端给出不透明 `merge_group_id` 和允许票种；前端可保留跨组选择，但提示分开申请并禁止下一步，不能自行“换算合并”。首期不支持部分金额、跨币种折算或跨主体合并。

资格必须在候选查询、提交、审批和正式交付时重验。查询结果只是瞬时视图；退款或 claim 竞争时以事务内结果为准。

## 4. 持久模型和事务不变量

本期共11张业务表，实际定义以冻结v17迁移为准。所有业务记录保存tenant，所有public ID不可猜，所有外部关联仍重新授权。

| 记录 | 必需字段与约束 |
| --- | --- |
| `invoice_requests` | 内部 ID、public ID、tenant/user、status、CAS version、policy/seller/tax-rule 快照、profile 引用（可空）及**加密不可变抬头快照**、币种/金额、备注、current_delivery_version、active_dispute_id、hold_from_status/原因、ever_delivered/已实际开具证据、关键时间。历史抬头不能被 profile 编辑影响。 |
| `invoice_request_items` | request/tenant/user、canonical key、来源类型/行 ID/订单号、实收额/币种/付款时间/渠道标签、最小付款校验摘要及税务组快照。不能复制卡号、回执密钥或原始 payload。 |
| `invoice_order_claims` | tenant + canonical key 唯一，request 关联；一笔付款同时只有一个活动 claim。rejected 合法释放后 item/event 历史仍保留；其他状态不释放。 |
| `invoice_attachments` | 不透明 ID、tenant/request、uploader/purpose（`invoice` 或 `dispute_evidence`）、随机私有 key、净化文件名、MIME/大小/hash、验证状态、绑定/过期/创建时间。暂存与正式交付分开，附件不能跨申请或用途复用。 |
| `invoice_deliveries` | request + delivery_version 唯一；一期每版一个正式文件，引用 validated attachment；发票号、开票日期、金额/币种/按需税额、交付人/时间。交付后不可变；新版本不覆盖旧文件。 |
| `invoice_disputes` | request、delivery_version、round、分类/说明、可选一个佐证附件、状态/处理回复/处理人/时间。每申请只允许一个未解决异议轮次，通过request.active_dispute_id及request锁串行保证，不依赖跨DB不兼容的partial index；新补充追加事件，不新建并行未解决异议。 |
| `invoice_events` / `invoice_event_reads` | 追加式事件、状态前后/actor/关联版本及用户可见说明；已读按 event + reader 唯一，支持多管理员各自阅读。本期没有内部备注接口或输入；未来新增时必须采用独立存储和管理员受众，不得复用用户可见说明。 |
| `invoice_profiles` | public ID、tenant/user、kind、加密字段、CAS version、soft-deleted 时间；列表脱敏。软删除不删除申请快照。 |
| `invoice_operations` | tenant/actor/operation/幂等键唯一、规范 payload digest、结果引用/安全响应、完成时间；与业务事务原子提交，用于重放和冲突识别。 |
| `invoice_payment_receipts` | 由核验通过的渠道签名回调或商户认证查单在原付款锁内写入；tenant/user/TopUpID、provider、原交易身份SHA-256、实收最小单位/币种、核验状态及付款时间。TopUpID与回执摘要各自唯一；充值与会员同一回执还共享全局claim。新的不一致回执将状态置为exception并同事务暂停关联申请，不能因原回执重放恢复verified。历史订单不凭success或Money自动回填，可通过受控查单核验商户、原订单、渠道交易号、成功状态及精确金额后补证；缺失的实付快照仅取自平台证据，不重复充值。 |

索引至少覆盖owner/tenant + created_at/id、管理员tenant + status/created_at/id、items的来源引用/订单号查询、claim唯一键、deliveries版本唯一键、事件/已读唯一键、operations唯一键及充值回执的TopUpID/receipt_key唯一键。固定长度规范付款key可使用稳定摘要，必须保留可核验的原身份映射，避免索引超长或截断碰撞；不允许因摘要碰撞合并付款。

跨数据库使用 GORM 和 `model/lockForUpdate(tx)`，不可写失效的 `gorm:query_option`。全局锁顺序固定为：operation唯一键 → canonical付款锚点按key排序 → request按内部ID排序 → attachment按ID排序 → dispute/其他子记录。所有会变更申请状态或占用的流程（包括退款回调）遵守同一顺序；可先无锁读不可变items解析付款身份，不能先锁request再回头锁付款。profile单独更新仅锁operation及profile，不参与付款链。SQLite 用写事务、唯一约束及有限冲突重试；数据库忙返回可重试错误，不能误报成功。不得使用仅 PostgreSQL 可用的 partial index 代替三数据库一致约束。新表和索引通过 `pkg/kkaimigrate` 版本化迁移，字段 JSON 使用 `common.Marshal/Unmarshal`，不直接调用 `encoding/json`。

### 4.1 字段加密

profiles 和申请快照中的税号、邮箱、地址、电话、银行信息均加密保存；标题也按私密财务资料保存；备注、异议说明/回复等可能含私密资料的业务文本同级加密，事件只引用相应记录，不冗余复制完整资料。最小实现用 Go 标准库 AES-GCM，每条独立随机 nonce，AAD 绑定 tenant/user/记录身份与字段类型，密文带 format/key ID。部署注入发票专用 key 和旧 key 读取环；不复用登录 RSA。缺 key、解密失败或认证失败须失败关闭、记录不含资料的错误，不静默落明文。上线前验证备份恢复和旧 key 可读的轮换路径，无需新增 KMS 平台。

本人及主站管理员只有在授权详情/编辑时获得业务需要的完整字段。列表返回脱敏摘要；不能承诺对密文抬头做模糊搜索。前端不得把资料、文件 URL 或订单选择写进 localStorage、持久 Zustand 或埋点。

## 5. 规范状态机与按钮权限

`request.version`（并发）、`delivery_version`（票据文件）、`dispute.round`（反馈轮次）相互独立，不能混用。新申请从 `pending_review` 开始，文件尚不存在。

| 状态 / 标签 | 用户允许动作（另均可看详情） | 管理员允许动作 | 目标状态 |
| --- | --- | --- | --- |
| `pending_review` 待审核 | 看进度 | approve / reject / hold | 待开票 / 已拒绝 / 财务待核查 |
| `approved_for_issue` 待开票 | 看进度 | 暂存上传、deliver / hold | 待确认 / 财务待核查 |
| `delivered` 待确认 | 看/下载当前及历史正式文件；confirm / dispute | 查看、必要时 hold | 已确认 / 异议处理中 / 财务待核查 |
| `confirmed` 已确认 | 下载；次操作“反馈问题” | 查看、必要时 hold | 用户反馈后异议处理中 |
| `disputed` 异议处理中 | 看已交付文件、追加反馈/佐证 | reply / reissue / hold | 待重新确认 / 待确认新版本 / 财务待核查 |
| `awaiting_reconfirmation` 待重新确认 | 同版本 confirm / 继续反馈 | reissue / hold | 已确认 / 异议处理中 / 待确认新版本 / 财务待核查 |
| `finance_hold` 财务待核查 | 看原因及已正式交付文件；无确认动作 | 核验后 resume；符合严格条件才 reject | 原合法状态 / 已拒绝 |
| `rejected` 已拒绝 | 看原因；从已释放付款重新申请 | 查看历史 | 原申请不再修改 |

补充约束：

- `approve` 仅从 pending_review。`reject` 仅 pending_review 或 hold_from_status 为待审核/待开票且**从未交付、没有已实际开具证据、管理员明确确认未开具**时允许；拒绝理由必填。已开具未交付也不能靠拒绝释放，须财务更正。ever_delivered 一旦为真不可清除。
- hold 可从待审核、待开票、待确认、已确认、异议和待重新确认进入，保存原状态。重复暂停只能追加说明，不覆盖原阶段；resume 重新验证付款、文件和异议，冲突时继续暂停，不可任意指定目标 status。
- disputed 的“回复说明”处理当前异议轮次后转 awaiting_reconfirmation，不修改文件版本；用户继续反馈重新打开/开始该版本的下一轮，仍只能有一个未解决轮次。
- disputed / awaiting_reconfirmation 的“更正补发”复用交付事务并创建下一版；旧异议以“已补发”结案，新版 delivered。选文件/上传阶段不跳到待开票、不改变 current pointer、不自动关闭异议。
- confirm 只接受当前正式版本，且仅 delivered / awaiting_reconfirmation；重复同一操作可重放。confirmed 再次点同版本确认为无副作用结果；不同 key 也不得重复发通知。旧版本不能确认。
- 不提供任意 status patch、管理员代确认、下载自动确认、删除或未定义的“待补资料”。管理员请求补充说明通过异议消息完成。

### 5.1 幂等、CAS 与恢复

创建申请不传 request version；更新请求/资料必须带当前 `version`。所有业务写入使用 `Idempotency-Key`（客户端 UUID；同次重试沿用），operation ledger 按 tenant/actor/具体操作及资源区分；规范摘要包含对象引用、资料内容、目标文件版本等，剔除不影响语义的传输字段。

先验证身份、资源范围并查询已完成 operation（先于policy关闭/变化、资格和CAS检查）：同 key/同 digest 返回原结果，**先于 stale version 判定**；同 key/不同 digest 返回 `409 IDEMPOTENCY_CONFLICT`。新操作在同一事务中持有 operation 唯一键、按全局顺序锁业务记录、核验状态、CAS、写结果；未提交事务不留下“成功”或永久处理中。相同 key 并发请求等待/重试有限次数，失败回可重试结果。operations 保留至少覆盖申请生命周期，不以短 TTL 让旧创建键产生第二张申请。

只读的quote查询不使用业务幂等键、不写账务状态。附件上传不改变 request version；以独立 upload operation key + 内容 hash 防止重放重复暂存，仍校验 request/purpose/actor。已读操作天然幂等，不需要业务 CAS。网络超时保留表单和 key，查询/replay 原操作，不能生成新 key 盲目新建。CAS 失败返回当前安全摘要，UI 刷新详情并保留尚未提交的输入。

付款核验以原充值订单锁、TopUpID/渠道回执唯一约束天然幂等，不创建申请或业务operation；客户端重试不会重复充值或重置付款异常。`Idempotency-Key`不替代每次查单和事务内证据核验。

历史商户已更换且原流水不可追溯时，运营方可以明确授权按精确订单清单人工认可历史付款，使用[历史易支付付款补证工具](invoice-historical-reconcile.md)。此例外仅适用于既有成功的旧 Epay 充值，按人工认可的原订单人民币付款金额补齐，采用独立认可身份及持久审计；不使用充值倍率或到账额度，不编造支付平台流水，不改变普通付款核验及管理员补单的资格规则。

## 6. HTTP 合约

### 6.1 通用约定

- 下表路径统一加 `/api`；用户路由 session-only，管理员再加 AdminAuth 和主站 scope。所有查询按 owner/tenant 过滤；跨域对象统一 404，不能以 public ID 替代授权。
- 注册 `middleware/access_token_routes.go` 的 session-only 策略、`middleware/tenant_features.go` 的 host capability，避免 PAT 或租户路径绕过。前端同步侧栏和 `TENANT_CONSOLE_ROUTES`，route guard 只是辅助。
- 使用 `Cache-Control: private, no-store` / DisableCache；审计 actor 来自 session，忽略/拒绝 body 中的 tenant/user/status/付款结论。写入速率限制和上传配额沿用现有中间件，防重复/资源滥用。enabled/policy开关在新操作资格阶段判定，不能在中间件中先挡住已完成操作的合法重放；主站身份/资源授权永远不能绕过。
- 成功沿用 `{ "success": true, "message": "", "data": ... }`。失败 `{ "success": false, "message": "安全提示", "code": "稳定机器码", "field_errors": { "profile.title": "校验码" } }`；前端按 code 本地化，不能解析中文 message 决策。
- ID 均为不透明字符串；金额均为整数字符串或 null；version/页码/数量为安全范围 JSON number；时间 RFC3339 UTC，开票日期 `YYYY-MM-DD`。不直接将 GORM model 当 DTO 输出。
- 分页 `p>=1`、`page_size` 默认20/最大100；返回 `{items,total,p,page_size}`。日期 `from` 包含、`to` 不含，均 RFC3339 UTC；UI 根据 policy timezone 把所选本地日期转成半开区间，不拼 `23:59:59`。
- 候选按 `paid_at DESC, stable_source_id DESC`，不可核验时间的不可用来源按来源创建时间稳定排序；申请按 `created_at DESC,id DESC`。列表最多每页100，详情最多50笔。`q` trim 后不超过128字符，参数绑定且转义 LIKE 通配字符。
- 详情/候选返回 `allowed_actions` / reason codes；前端按它显示操作，后端每次重验。按钮不能只按本地状态枚举决定权限。

### 6.2 用户查询与资料

| Method / Path | 响应与规则 |
| --- | --- |
| `GET /invoices/policy` | enabled/disabled_reason、policy_version、seller 摘要、currency/exponent、invoice_types/required_fields、source capabilities、timezone、limits（50笔、20MiB、长度）、可选 SLA。只输出公开规则，不含存储/密钥配置。 |
| `GET /invoices/summary` | 全历史可申请付款 count/按币种 totals，processing_count、awaiting_confirmation_count、unread_count；不受候选当前页影响，不用旧 TopUp 30天接口。 |
| `GET /invoices/eligible-orders` | `eligibility=eligible|unavailable` 分别分页；q订单号、source_type、from/to按付款时间、currency；快捷预选也可用source_type+order_id精确定位；返回候选 DTO、不可用 reason/advice、merge group。filters 不改变已选内存状态。 |
| `POST /invoices/quote` | 只读核对1–50个付款引用及可选invoice_type/policy_version；返回逐项资格/原因、merge_group_id、精确总额及当前policy。供快捷预选、下一步及提交前核对；不创建claim、不保证以后资格仍有效，不接受客户端金额。 |
| `POST /invoices/payments/verify` | session-only、主站、用户限速；仅接收`{source_type:"topup",order_id}`。服务端向配置商户发起HTTPS查单，严格核验后在原订单锁内补齐历史付款证据，成功返回`{verified:true}`；不重复入账，不释放claim，不清除exception。凭据、原始响应和内部错误不返回前端。 |
| `GET /invoices` | `group=all|processing|awaiting_confirmation|completed`、status、q（申请号/订单号）、from/to按申请时间、currency、source_type、分页。group 与 status 同时指定取交集。 |
| `GET /invoices/:public_id` | 申请 DTO、完整本人抬头快照、订单、current_delivery、历史已交付版本、用户可见事件/异议、allowed_actions；不返回内部备注、key、未交付文件或原支付 payload。 |
| `GET /invoice-profiles` | 本人未软删资料分页、脱敏摘要及 version。 |
| `GET /invoice-profiles/:id` | 本人编辑所需完整资料、version；不允许跨 owner 或 tenant。 |
| `POST /invoice-profiles` | 新建资料、返回 ID/version；同 policy 字段规则。 |
| `PATCH /invoice-profiles/:id` | 完整业务字段 + profile version，原子更新；不改变任何申请快照。 |
| `DELETE /invoice-profiles/:id` | body 带 version、软删除；已被申请引用仍保留快照。 |

summary 中 processing = pending_review + approved_for_issue + disputed + finance_hold；awaiting_confirmation = delivered + awaiting_reconfirmation；completed = confirmed + rejected。候选 totals 仅含当前真正可申请、已核验且未 claim 的付款，多个 currency 分开，不跨币种相加。记录组采用相同定义；未读数与待办数不同。

候选 DTO 示例（示例 ID/金额，不是种子数据契约）：

```json
{
  "source_type": "membership", "order_id": "ord_public_id", "order_no": "M202610220001",
  "paid_at": "2026-10-22T02:00:00Z", "payment_label": "已核验渠道",
  "amount_minor": "69900", "currency": "CNY", "currency_exponent": 2,
  "eligible": true, "reason_codes": [], "merge_group_id": "opaque_group",
  "allowed_invoice_types": ["configured_type"], "allowed_actions": ["select"]
}
```

不可用 reason 至少区分 `PAYMENT_UNVERIFIED`、`PAYMENT_ALIAS_UNRESOLVED`、`ORDER_CLAIMED`、`UNPAID`、`REFUNDED_OR_REVERSED`、`PAYMENT_EXCEPTION`、`WALLET_PURCHASE`、`GRANT_NOT_CASH`、`CURRENCY_UNSUPPORTED`、`SELLER_UNCONFIGURED`、`SOURCE_DISABLED`。未知金额 null；多个原因可同时返回。只有占用属于本人时才返回 `claimed_request_public_id` 的查看入口，不泄露他人申请编号。不可核验来源金额为空时不能被 summary 计为0元可申请。

### 6.3 申请、确认和反馈

| Method / Path | 请求及效果 |
| --- | --- |
| `POST /invoices` | 1–50个不同付款引用、`profile_id` 或 inline `profile` 二选一、invoice_type、policy_version、备注、user_confirmed=true；返回申请详情。客户端金额/状态/tenant 不被采纳。 |
| `POST /invoices/:public_id/confirm` | request version、delivery_version；只确认当前可确认文件，追加事件。 |
| `POST /invoices/:public_id/attachments` | multipart，purpose 固定 dispute_evidence、单文件20MiB；绑定本人申请，返回 validated attachment ID/元数据，不改变申请状态。 |
| `POST /invoices/:public_id/disputes` | version、delivery_version、category、description、可选 evidence_attachment_id；初次反馈或同版本继续反馈。仅 delivered/confirmed/awaiting_reconfirmation 创建或重开一个异议轮次。 |
| `POST /invoices/:public_id/disputes/:dispute_id/messages` | version、description、可选 evidence_attachment_id；用户给当前未解决轮次追加说明，保持 disputed。 |
| `GET /invoices/:public_id/files/:file_id` | 授权代理，正式历史/当前文件及本人合法佐证可访问；`disposition=inline|attachment`，不接受存储 key。 |

提交 inline 资料示例；使用已存资料时**删除 profile，改传 profile_id**，不能同时传两者，也不能都不传：

```json
{
  "items": [
    {"source_type": "topup", "order_id": "opaque_order_a"},
    {"source_type": "membership", "order_id": "opaque_order_b"}
  ],
  "profile": {"kind": "company", "title": "用户填写的抬头", "tax_id": "用户填写的税号", "email": "billing@example.com"},
  "invoice_type": "configured_type", "policy_version": "policy_1",
  "note": "", "user_confirmed": true
}
```

常用资料“保存”是独立、明确的 profile mutation；保存失败不应撤销或重复创建已成功的发票申请。申请页可直接 inline 提交，不强制先保存常用资料。profile_id 在创建事务中核验 owner、有效性并复制快照；已编辑但未保存的表单必须 inline 提交，不能用旧 profile_id 覆盖用户修改。

校验：kind为person/company；title trim 后1–200字；企业 tax_id 必填，格式/长度由票种/地区policy指定；email有效且不超过254字符（不把允许输入等同已验证邮箱）；note最多500字；异议说明/回复/拒绝/暂停原因1–1000字。条件字段只在政策要求时采集；基础上限地址500、电话64、银行名称200、账号128字。服务端限制 UTF-8字符数及请求总字节，渲染按纯文本转义。

申请 DTO 至少包含 public_id/display_no、status、version、policy/seller 摘要、profile快照或脱敏摘要、items_count/source_summary、amount_minor/currency/exponent、关键时间、current_delivery_version、current_delivery、delivered_versions、active_dispute、allowed_actions、notification_delivery_state。详情还含 items/events；列表不复制全详情或未交付附件。确认/反馈/补发返回更新后的同一详情 DTO，避免前后端另算状态。

### 6.4 管理工作台 API

| Method / Path | 契约 |
| --- | --- |
| `GET /admin/invoices/summary` | 主站 queue counts：pending_review、approved_for_issue、disputed、all、finance_hold及未读。counts采用当前 q/date/currency/source 筛选，忽略当前 queue/status/p；不把已读数当待处理数。 |
| `GET /admin/invoices` | `queue=pending_review|approved_for_issue|disputed|all`，status、from/to申请时间、currency、source_type、q、分页。q支持申请号、关联订单号、用户名及**账号邮箱**；不承诺加密抬头/收票邮箱的模糊检索。列表抬头/税号/邮箱脱敏。 |
| `GET /admin/invoices/:public_id` | 当前主站申请、完整开票抬头/税号/收票邮箱、付款核验摘要、历史版本/异议/事件、allowed_actions；不能把“列表脱敏”误实现为开票详情也缺资料。 |
| `POST /admin/invoices/:public_id/approve` | version；重验付款/政策/claim，转 approved_for_issue。 |
| `POST /admin/invoices/:public_id/reject` | version、reason、not_issued_confirmed=true；严格遵守第5节条件，释放 claim、状态/事件同事务。 |
| `POST /admin/invoices/:public_id/hold` | version、reason；保存原阶段，锁住后续动作。 |
| `POST /admin/invoices/:public_id/resume` | version、reason；核验后恢复，不能由请求体指定目标状态。 |
| `POST /admin/invoices/:public_id/attachments` | multipart单文件20MiB，purpose=invoice；只在允许交付/补发状态上传，返回 validated attachment ID、文件名/MIME/大小/hash/到期时间。 |
| `POST /admin/invoices/:public_id/deliveries` | version、attachment_id、invoice_no、issued_on、amount_minor、currency、按政策可选tax_minor；仅 approved_for_issue 首次交付。 |
| `POST /admin/invoices/:public_id/disputes/:dispute_id/reply` | version、用户可见 message；关闭当前异议轮次并请求同版本重新确认。 |
| `POST /admin/invoices/:public_id/disputes/:dispute_id/reissue` | version + 同delivery元数据；只对当前或刚回复的异议轮次补发，创建新 delivery_version；不能只改文件指针或复用旧附件。 |
| `GET /admin/invoices/:public_id/files/:file_id` | 管理员授权代理：同申请正式/历史/validated暂存/佐证，按当前权限和purpose校验。 |

发票号1–128字符、去控制字符；issued_on为有效日期且不可晚于卖方当地今天；金额/币种须精确匹配存储申请，税额在政策有效范围内。管理员预览文件并确认票面与元数据一致，系统不能声称仅凭上传或 OCR 保证发票真实/合法。审批和交付若发现资格失效，进入 finance_hold 并返回可识别冲突；这个暂停是已提交的业务结果，事件与幂等结果同事务，不能抛错后事务整体回滚而丢掉暂停。

### 6.5 站内通知 API

用户 `GET /invoices/notifications`、`POST /invoices/notifications/:event_id/read`、`POST /invoices/notifications/read-all`；管理员对应 `/admin/invoices/notifications...`。列表可 `unread_only`、分页，返回安全事件摘要、申请链接、read_at/total/unread_count。不带开票私密字段或存储 URL。用户在中心、管理员在工作台各有“进度通知”入口及未读badge，打开通知项进入申请并标记该项已读，可一键全部已读；没有新建全站通知平台。已读只影响当前 reader；read-all 只标记调用开始时已存在的可见事件，不吃掉随后到达的新事件。不能把打开列表等同于确认收票。

### 6.6 错误与前端处理

| HTTP / code | 前端行为 |
| --- | --- |
| 400 `VALIDATION_FAILED` / `DUPLICATE_PAYMENT` | 字段或订单定位；保留输入。 |
| 401 / 403 `SESSION_REQUIRED`、`FEATURE_DISABLED`、`FORBIDDEN` | 进入现有登录/无权限流程；能力未开启显示配置原因，不反复请求。 |
| 404 `NOT_FOUND` | 安全空态，不泄漏对象是否属于他人。 |
| 409 `ORDER_ALREADY_CLAIMED` / `PAYMENT_CHANGED` / `POLICY_CHANGED` | 标记失效选项并刷新资格；明确重新核对，不静默删掉选项后继续提交。 |
| 409 `VERSION_CONFLICT` / `INVALID_STATE` / `DELIVERY_VERSION_CHANGED` | 刷新详情/队列、保留输入，禁止旧版确认或覆盖。 |
| 409 `IDEMPOTENCY_CONFLICT` / `ATTACHMENT_ALREADY_BOUND` | 不自动换key重发，提示核查原操作。 |
| 413 / 415 `FILE_TOO_LARGE` / `FILE_TYPE_UNSUPPORTED` | 上传区域原位报错，可重选；未交付不显示成功。 |
| 422 `INVOICE_METADATA_MISMATCH` / `INCOMPATIBLE_GROUP` | 定位字段/分组，不允许浮点容差。 |
| 429 / 503 `RETRY_LATER` / `SCHEMA_NOT_READY` / `STORAGE_UNAVAILABLE` | 退避重试或明确不可用，保持幂等键和草稿；不显示假成功。 |

内部500仅安全消息和request ID；不回税号、密文、原始回执、文件key或签名URL。例外码必须同步前端映射和契约测试，不能随意把所有失败压成200成功。

## 7. 核心服务流程、安全与通知

### 7.1 创建 / 审批 / 退款协同

创建申请：验证session/主站host及资源授权、规范请求digest → 查已完成operation并优先重放 → 对新操作验证enabled/policy并解析唯一付款及合并组 → 占有operation唯一键并固定顺序锁付款锚点 → 重验owner/实收/回执/异常/退款/claim → 服务端合计与profile快照 → 同事务写 request/items/claims/event/operation/outbox → 返回详情。任一付款不合格整单失败，不能自动删掉那笔后提交其余金额。

审批与交付锁同一付款边界，再重验事实；付款异常进入财务暂停。退款/拒付/冲正写入方先按canonical key锁付款，再按ID锁关联request，同事务更新申请为hold，保存原阶段和事件。后续已知退款不能只靠夜间扫描才阻止开票。外部支付事件延迟不可完全消除，交付前需核对可获得的最新可信状态；相关来源未具备可靠同步能力前不开放。正式票据后的更正/红冲由财务线下流程处理，应用保留账迹和占用。

当前实现已接入会员额外实收回调，在原付款事务中自动暂停关联申请；既有财务 dismiss 处理后可重验恢复，仍只按主收据金额开票。主站当前没有通用退款写入接口，系统无法感知未登记的外部/手工退款；启用来源前必须确认财务对账和暂停流程。未来新增退款入口必须在同一付款锁边界接入，不能只写支付状态。

充值的真实签名回调在success重放返回前核对并保存独立receipt；与已保存的金额/币种或回执身份不一致时，在同一事务内标记异常并暂停关联申请。手工完成付款路径不写verified receipt，不能通过补单绕过开票资格。未部署发票schema时保留原支付行为；出现部分发票表但缺少安全表时失败关闭，不静默跳过异常协同。

### 7.2 上传与交付分离

1. 授权后限制请求体与file字节；单文件20MiB，multipart总body上限21MiB、元数据JSON上限64KiB。仅PDF/PNG/JPEG，按内容sniff及基础结构校验；图片解码并限制20百万像素。PDF校验header、xref、EOF及已知主动内容标记，配合服务端CSP和前端iframe sandbox隔离；当前实现不是完整PDF解析器或恶意文件扫描器，不能宣称识别全部损坏或主动内容，真实文件兼容性仍需环境验收。
2. 私有存储随机key、独立invoice前缀，无public ACL。净化原名中的路径/控制字符；计算SHA-256。存储和元数据写入不能原子时，以staged记录及可重入清理处理孤儿；失败不产生可交付ID。
3. validated附件绑定tenant/request/actor/purpose；其他申请、用户或用途不能偷用；未绑定暂存24小时过期，清理前再查绑定状态。正式/佐证绑定文件不能按临时文件清理。暂存上传不推进request状态。
4. 交付事务遵守operation → canonical付款 → request → attachment → dispute的统一锁顺序；核验未过期、validated、同request、正式purpose、未绑定、金额/币种/票种与metadata。创建不可变下一版本，更新current pointer/status/ever_delivered，结案相应异议，写event/operation/outbox。事务失败保留旧current；可重试或清理新暂存。
5. 用户只见**已经正式交付**的当前及历史版本，清楚标记“历史版本，不可确认”；finance_hold也继续可看先前已交付文件。未交付草稿永不向用户泄漏。佐证按作者/申请及管理权限访问，不作为正式票据版本。
6. 代理每次校验身份、owner/admin、tenant和附件关联，正确MIME、`private,no-store`、`nosniff`及安全Content-Disposition。PDF内联用受限独立预览容器/内容策略，不允许票据内容执行同源脚本；历史文件不自动展开。不能把长期URL写入query cache或事件。
7. 留存到期须符合财务/法务保存政策并审计，不能通过profile删除或申请“已完成”顺便删除票据。邮件只发进度和需登录的站内链接，一期不将敏感附件直接发到任意填写邮箱。

### 7.3 通知 / 缓存 / 审计

- 业务事件即站内通知依据，同事务落event及outbox；用户看自己的事件，管理员看授权队列事件，各自read receipt。提交、审核、拒绝、交付、确认、反馈/补充、回复、补发、暂停/恢复均留事件；通知受众按动作区分，避免自操作重复提醒。
- 邮件复用现有KKAI outbox新增invoice topic；payload只放事件/申请引用，发送时授权读取并解密所需信息。稳定event key避免业务重复入队，provider message ID可稳定化；网络重试按至少一次语义，不虚称邮件绝对只送达一次。
- 邮件失败不会回滚交付，API按最近一次管理员/系统进度事件返回`queued/retrying/sent/failed/not_configured/not_requested`安全摘要；当前详情页对重试/失败显示专用提示，其余显示站内安全访问说明。死信显示发送失败，不能继续承诺重试。现有outbox重试/死信可观察；现有工单SMTP是best effort，不可描述为现成持久邮件重试。
- 审计记录actor/动作/对象/结果/request ID；不记录完整body、税号、邮箱、回执密钥、文件内容/签名链接。描述、回复纯文本转义；敏感API避免analytics自动采集。
- React Query keys包含host scope、身份、user/admin、筛选/页码；mutation按影响失效候选、summary、records/detail、admin queue/count及notifications。logout/host变化清空内存草稿/选择并清理敏感query cache；403不能渲染上一个身份缓存。

## 8. 前端页面与交互契约

视觉尺寸、控件清单及截图检查详见[UI规格](./invoice-center-ui-spec.md)，本节定义功能落点，不能取代逐屏对照。

### 8.1 路由、组件和入口

| 路由 | 功能 |
| --- | --- |
| `/invoices` | 用户中心共用标题/摘要/tabs；无tab时replace到`/invoices/new`。`tab=records|profiles`分别进入记录/常用抬头，非敏感筛选可放search schema。 |
| `/invoices/new` | 三步独立申请；支持`source_type/order_id`快捷预选并经后端quote重验。 |
| `/invoices/:public_id` | 用户可刷新/分享站内链接的详情；无owner权限返回安全错误。 |
| `/invoice-admin` | 主站管理员队列/详情双栏，search保存queue和选中申请，不包含PII。 |
| `/invoice-admin/:public_id` | 管理详情深链、小屏独立页；返回保留队列条件。 |

通过TanStack file routes `_authenticated`接入；复用AuthenticatedLayout、AppHeader/AppSidebar、SidebarInset/Main、SectionPageLayout。新增“发票中心”和有权限者的“发票管理”。钱包充值历史与会员订单历史加“申请开票/查看申请”入口；点击不可仅本地判断，读取资格再预选，旧充值路径继续进入钱包。

建议结构：`features/invoice/{api.ts,types.ts,query-keys.ts,components/,hooks/,pages/,lib/}`；按页面拆分，不将整个原型塞进一个巨型组件。复用已有Dialog/AlertDialog、Tabs、Table、Checkbox、Input、Select、Skeleton等Base UI封装及Hugeicons；不混用Radix API、不另造shell。六语言文案遵循英文源字符串key及仓库`i18n-translate` skill，不照搬旧文档中不一致的层级key建议。

### 8.2 三步申请与常用抬头

- 选择页：来源/订单号/日期/币种筛选、分页及独立不可用入口；勾选本页仅eligible行。选中项在短时内存按规范引用保存，跨页仍在；进入资料页及提交前调用quote重核；过期资格显式标记并阻断提交。quote不替代创建事务重验。摘要显示已选数、按币种精确金额、来源及不兼容原因；0笔或>50笔/不兼容时不能下一步。
- 资料页：个人/企业、票种、title/tax/email及条件字段；policy决定可选项。选profile后可编辑，未保存改动inline提交；保存/编辑/删除有成功/失败及独立loading，不影响历史申请。本人无profile时有可直接新增的空态。
- 核对页：完整订单/卖方/票种/资料/总额和确认框，有返回对应步骤入口；确认checkbox不默认勾。回退保持表单，提交锁按钮；未知结果用原key查询/重放。成功展示真实申请号进入详情；不复用固定INV编号。
- 开票说明入口说明真实支付范围、整笔/合并限制、财务配置时限与反馈方式；不硬编码“3个工作日”等承诺。不支持票种用说明而非假选择器。

### 8.3 用户记录、详情及反馈

记录组与summary定义一致；状态/q/日期/分页可组合。列表显示申请号、脱敏抬头、来源/笔数、金额、状态、时间、详情；loading保留布局，空态可去申请，error可重试，权限错误不漏旧缓存。

详情遵循UI规格中的资料/订单/文件/时间线顺序；待确认和待重新确认显示“确认无误/发票有问题”，confirmed显示次操作“反馈问题”，disputed显示当前异议与“补充说明”，hold显示原因并保留已交付附件但无确认。拒绝展示理由及重新申请入口，重提创建新申请，旧记录不覆盖。

反馈分类固定为抬头/税号、金额、附件打不开、信息不全、其他；说明1–1000字、可选一个20MiB佐证，先上传再提交引用。失败保留说明，上传成功不等于反馈已提交。管理员回复同版本可再确认/继续反馈；补发新版本必须重新确认。预览/下载都是真实授权文件，不得用原型生成的水印发票冒充完成。

### 8.4 管理队列和动作

保留窄左队列+宽右详情；不改成另一套“左信息右操作”页面。队列counts/filters/search/pagination来自API；选中态、刷新和小屏返回保留上下文。搜索帮助明确是账号邮箱，不误导为可查全部收票邮箱/加密抬头。

详情授权显示完整开票资料、付款核验摘要、关联订单/版本/时间线，列表脱敏。按钮由allowed_actions控制：审核拒绝/通过、待开票上传交付、异议回复/更正补发、财务暂停/恢复；没有“标记用户已确认”。各危险操作使用二次确认，拒绝/暂停原因必填且用户可见，内部备注不得混发。

上传文件后显示文件名、大小、校验/上传结果，可重选；发票号/日期必填，申请金额/币种锁定且服务端再验。交付弹窗核对抬头、税号尾号、金额、文件名和收票邮箱；提交期间禁用。上传失败停留当前态；交付成功仍显示已交付，邮件重试/发送失败分别提示；补发编辑期间仍显示已交付旧版。管理员正式票据通过内容校验后，在对象上传前记录已开具证据；即使存储失败也不再允许拒绝释放付款，需财务核查。

### 8.5 响应式、无障碍与冲突体验

按UI规格的容器宽度断点安排双栏/单栏。原型1024画布的236px摘要、237px管理队列是视觉基线，不直接按浏览器宽度硬套已有shell；移动端固定底部栏考虑safe-area及内容padding。320px起无横向溢出，375×812及1440×960为必验视口，另验1024中等宽度。

复用产品theme vars支持深色；状态含文字/图标，金额tabular nums，输入控件触控目标至少44px。键盘可完成三步、选择及弹层；label/error用aria关联，dialog焦点循环/恢复、ESC、状态aria-live、reduced-motion，正文对比度AA。预览中10–11px辅助字不原样用于生产交互控件，正文/控件使用现有可读token。

网络失败不清空表单；409刷新服务端状态后提示变化而非静默跳步。缓存刷新时保持选择/滚动，按钮有本地pending防双击但不代替幂等；请求取消/离页要保留已提交结果可查，不将HTTP断开当业务回滚。未保存资料离页给一次明确提醒，PII不写URL或浏览器存储。

## 9. 可追踪验收矩阵

以下ID由测试用例、截图和联调记录共同引用。每一行同时验API和UI结果，避免“接口通过但按钮不存在”或“按钮有效但权限/账务错误”。

| ID | 行为与必须通过的断言 |
| --- | --- |
| PAY-01 | 核验充值与直接付款会员可合并；过期/用尽会员仍可；金额来自精确实收；缺/重复/异常/金额币种不符/非一次性回执不可用。 |
| PAY-02 | 旧订阅与TopUp镜像只显示并占用一次；别名输入不重复合计；钱包/兑换/赠送不产生新现金项目；历史>30天仍可查。 |
| PAY-03 | 未知金额null、未开放来源/币种明确原因；超大金额精度/溢出、50笔边界、跨组阻断、部分退款hold，客户端篡改金额无效。 |
| CON-01 | 两用户/两并发请求争同付款只有一成功；多来源别名同样唯一；双击/网络断开同key重放无重复申请/事件，digest冲突报409，重放先于CAS检查。 |
| CON-02 | 提交/审批/交付与退款并发共用锁边界；失败整单回滚；SQLite忙有限重试；CAS冲突不覆盖新版本。 |
| FLOW-01 | pending审批仅变待开票；无文件不可交付；拒绝需理由/未开具确认且合法释放可重提；实际已开具/曾交付不得reject释放。 |
| FLOW-02 | 交付当前版才能confirm；回复说明→同版待重新确认→确认或继续反馈；已确认还能反馈；一个未解决异议；追加说明不重复开单。 |
| FLOW-03 | 更正上传不隐藏旧版、不提前关异议；正式补发新增版本、旧版可查且不可确认；新版本重新待确认。 |
| FLOW-04 | 所有允许状态可hold并保存原态；保持claim和历史访问；hold无用户confirm；resume重验，已交付永不释放。 |
| SEC-01 | 非owner、跨tenant、非主站管理员、PAT全部被拒；篡改profile/attachment/dispute/file ID不越权；未授权不存在性不泄漏。 |
| SEC-02 | profile及申请快照加密/轮换/无key失败关闭；列表脱敏、授权详情完整；无PII落日志/URL/localStorage；profile删除不改历史。 |
| FILE-01 | PDF/PNG/JPEG通过；20MiB上下界/总body、假MIME/损坏/截断/过量像素等拒绝；暂存无用户可见URL、不能跨请求/用途绑定、孤儿清理不误删正式版。 |
| FILE-02 | 上传/对象存储/DB失败不假交付；金额/币种不符拒绝；用户/管理员下载均授权、正确安全头；hold及历史可看、草稿不能看。 |
| NOT-01 | 事件与业务同事务，通知read/read-all隔离reader；未读不等待办；outbox稳定去重/重试/死信，邮件失败不撤销业务成功。 |
| UI-01 | 三步、筛选/翻页保选、选择本页、混组选阻断、返回保值、超时重放、钱包/会员快捷预选及错误定位均可操作。 |
| UI-02 | profile增改删选、条件字段/邮箱/长度验证；记录筛选/详情/说明入口；管理counts/队列/全文资料/上传/回复/补发；每控件有API和空/载入/错态。 |
| VIS-01 | 按UI规格逐屏截图：1440×960、1024中等宽度、375×812和320px，浅/深主题；双栏、间距、字号/金额、队列选中态、移动底栏/弹层/长文案无溢出。 |
| A11Y-01 | 键盘/屏读完成流程、焦点恢复、字段错误/状态非纯颜色、44px触控和AA对比度；六语言无漏key、截断或错币种格式。 |
| DB-01 | SQLite/MySQL/PostgreSQL migration、唯一约束、金额/时间/加密字段、主要事务及竞态行为一致；未部署schema失败关闭不AutoMigrate。 |

新增后端测试使用testify require/assert，显式fixture用户/租户/付款回执；不写随机循环、sleep或只断言helper内部的覆盖率测试。前端验证真实表单/用户动作和API契约，截图必须用与冻结参考一致的稳定虚构数据和状态；动态时间/编号不用于像素误差判定。视觉差异先核布局/关键交互，不以整体像素百分比掩盖漏按钮。

**实施检查边界**：依据实际改动运行invoice定向Go/Vitest/API测试、改动文件format/lint、i18n检查；迁移/鉴权作为L3制定三DB验证。`bun run typecheck`会检查整个前端，在执行前遵循仓库全量命令审批要求，不把它混作不需确认的定向检查。没有定向检查时报告缺口，不擅自替代为全仓测试。已执行命令、浏览器夹具范围及未覆盖项见[实施记录](./invoice-center-implementation.md)，不得把夹具截图描述成真实后端端到端验收。

## 10. 开发任务顺序与交接完成定义

| 阶段 | 责任与交付 | 前置 / 完成门槛 |
| --- | --- | --- |
| T0 政策与契约冻结 | 产品/财务/技术确认来源、canonical映射、税务分组、密钥/留存；本文DTO与UI控件表 | 不明来源继续关闭；业务状态无歧义。 |
| T1 后端数据与资格 | migration、policy/candidates/summary、付款去重/claim、申请与profile、安全字段 | PAY/CON/DB相关场景；三DB证据，实际迁移另行授权。 |
| T2 状态与文件 | session/host权限、审批/hold/确认/异议、上传/历史/下载、events/outbox | FLOW/SEC/FILE/NOT契约；不使用假文件和演示身份。 |
| T3 用户端 | 原shell、三步、记录详情、profile、快捷入口/通知、响应式 | 每个控件对应真实接口，UI-01/02与用户截图检查。 |
| T4 管理端 | 队列counts/过滤、完整详情、拒绝/审批/上传交付/回复补发/暂停 | 不漏任何allowed_actions，危险操作及错误恢复闭环。 |
| T5 联调与视觉验收 | 统一fixture、逐屏截图、六语言/无障碍、业务竞态及权限证据 | 上表每项标通过/失败/未覆盖；未覆盖阻断核心财务/隐私能力上线。 |

前后端开始前将本文字段固化为Go DTO与TypeScript类型及对应契约fixture，字段变更同步UI规格和测试；不能“实际DTO以实现为准”而让双方各写一版。接口完成按真实状态联调，不只mock happy path。任何原型演示简化（假角色、固定号、假文件、浏览器MIME判断）不得带入生产。

交付只有在付款唯一且金额准确、状态/异议/版本闭环、权限和私有文件受测、资料安全持久化、所有承诺页面和按钮在桌面/移动可完成、截图对照通过、财务配置缺失正确关闭后才可标记完成。此文档是减少遗漏的开发基线，不能代替实现后的测试、代码审查与验收。

## 11. 交付核验记录

2026-10-22 的文档阶段已完成需求/开发/UI规格和冻结演示参考交叉评审，统一分组、管理队列/计数、异议补充接口、锁顺序、幂等重放及通知入口。冻结设计CSS与已批准初稿逐字一致；演示脚本仅针对占用、拒绝释放、唯一编号、交付可见性、回复重核和补发版本做定向检查。

2026-10-23已实现并审查前后端业务、私有附件、通知和v17 schema，完成定向Go、Vitest、lint、范围类型检查、i18n同步及真实页面的夹具浏览器核对。修复项包括MySQL锁后读取付款快照、充值真实回执证据与唯一性、充值/会员异常实收自动暂停、邮件死信状态、抬头并发版本、表单冲突保值、金额精度与移动底栏裁切。

本轮补齐最终11表迁移及独立`invoice-migration-v1`应用协议，基础设施侧已本地实现相应控制器；应用要求固定root-owned只读proof、最长120秒时效、精确数据库/digest/checksum/镜像和active/rollback/writer实际进程身份，在迁移锁内重新授权并验证最终回执。普通`--target 17`（包括offline）仍拒绝写入，v16工单协议没有被放宽。最终11表SQLite、真实隔离PostgreSQL18.4/MySQL5.7的schema以及PostgreSQL原子guard/receipt定向验证通过；MySQL首轮因隔离测试容器OOM中断，恢复后仅复测失败范围并通过。业务层真实MySQL5.7重复读的4个子项也已通过，验证旧快照不能绕过付款、收据异常及全局claim的锁后当前读。

具体命令、页面截图、配置方法和最终验证结果统一维护在[实施记录](./invoice-center-implementation.md)。本记录阶段尚未安装生产控制器、执行生产迁移或部署；真实S3/SMTP与浏览器连接真实后端的全生命周期联调也未完成。全仓测试/构建未运行，上线必须继续满足对应版本迁移及发布runbook的强制门槛。


### 人工开票流程修订（2026-10-23）

本功能管理申请、人工审核和人工开好的票据附件，不执行税务开票。启用策略最小为`{"enabled":true}`；销售方/税目配置不再是人工流程启用门槛。默认`manual`票种、CNY和充值/订阅实付来源，付款核验、资料加密、权限及唯一占用要求不变。空销售方不在页面展示。默认附件后端为共享持久化`/data/invoices-private`私有目录，登录后经授权API下载；独立私有S3为可选配置。无需用户提供税务开票接口或新建云存储账号。最终配置及验收以[实施记录](./invoice-center-implementation.md)为准。

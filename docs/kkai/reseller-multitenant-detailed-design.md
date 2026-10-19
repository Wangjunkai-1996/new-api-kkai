# KKAI 代理品牌站详细落地设计

状态：实现基线 1.0；本地代码与定向测试，未部署
日期：2026-10-19

关联文档：[产品需求](./reseller-multitenant-prd.md)、[技术选型](./reseller-multitenant-technical-selection.md)、[实现与验收记录](./reseller-multitenant-implementation.md)。

## 1. 架构与模块

```text
主站 Host / 代理子域名
  → DNS / TLS / Edge（上线前单独配置）
  → TenantRouting → TenantFeatureGate → 身份鉴权
  → controller → tenant / reseller service → GORM model
  → 共享 PostgreSQL + 可选独立日志库
```

继续复用 `web/default`。平台 Channel/Provider、模型基础价和管理员权限不租户化。代理只拥有品牌、目录、销售倍率、客户只读视图、收益与提现。

| 模块 | 代码入口 | 责任 |
| --- | --- | --- |
| Host/权限 | middleware/tenant.go、tenant_features.go、reseller.go | 入口解析、允许清单、owner membership |
| 租户模型 | model/tenant.go、reseller.go、reseller_management.go | 归属、品牌、申请、目录与版本 |
| 价格与预算 | service/tenant_catalog.go、tenant_billing.go、tenant_budget.go | 保底、报价、完整预扣、最终出站校验 |
| 资金 | model/reseller_finance.go、reseller_settlement.go、tenant_consumption.go | 批次、quote、消费意图、原子结算 |
| 冲正/提现 | model/reseller_payment_reversal.go、reseller_withdrawal.go、tenant_reconciliation.go | 反向流水、debt、人工审核及核对 |
| 后台任务 | service/reseller_pending_worker.go、kkai_runtime_jobs.go | 结算重试、24 小时解冻、既有 leader gate |
| 前端 | features/site-config、features/reseller | Host 品牌、申请、平台审核、受限代理后台 |
| 迁移 | pkg/kkaimigrate/schema_tenant*.go、cmd/kkai-migrate | v12 固定快照、历史约束替换、独立日志库 |

没有新增万能 utils、独立 SPA 或代理 Channel 管理接口。既有计费使用浮点的部分继续受 quota 安全转换保护；新现金账务仅使用整数分和 decimal。

## 2. Host 和身份

1. 仅规范化 `Request.Host`：处理大小写、端口、IDNA，拒绝 URL、IP、非法 hostname。
2. 显式平台 Host 映射为 `tenant_id=0`；其余必须命中活动 tenant/domain。
3. 未知或停用 Host 返回拒绝，不回退主站。关闭租户开关时仍校验平台 Host。
4. `TenantContext` 注入可信 tenant/hostname；不得使用客户端 tenant header/query/body 改写归属。
5. Session/API Token 验证其租户与当前入口一致。owner 只通过所属 membership 进入代理后台。

`TENANT_PLATFORM_HOSTS` 是逗号分隔的 hostname 允许清单；未设置时从既有 ServerAddress 取规范化 host。无有效主站 Host 时启动拒绝，不自动开放任意 Host。IP 探针不能绕过规则，发布前须把健康检查改为携带已允许域名的 Host。

owner 保留主站用户行（tenant=0），通过显式管理登录模式（`owner_login=true`）用原凭据在代理 Host 建立独立 owner 会话。默认登录只查客户身份，不再 fallback；同名或同邮箱客户不能遮蔽 owner，密码错误也不能切换命名空间。客户注册在当前租户内产生用户归属和 customer membership；owner 不会自动转换为客户。`TenantAdminAuth` 不依赖 ROLE.ADMIN。平台 Admin/Root 接口仍只给平台账号及主站入口。

用户唯一键是 `(tenant_id, normalized_username)` 与 `(tenant_id, normalized_email)`。空邮箱保存为空身份值，不阻止其他无邮箱用户。不同租户可同名、同邮箱。登录、重置密码、验证码和邮箱绑定均使用租户范围的查询和验证键。

Cookie 为 host-only、Secure、HttpOnly、SameSite；Origin 按当前站点校验。代理 Host 暂不开放第三方 OAuth/Passkey；数据模型仍保留其租户快照。用户/Token 缓存包含租户归属，限流键、前端持久化及请求去重包含 Host/tenant。

## 3. 数据模型

| 表 | 主要约束与用途 |
| --- | --- |
| tenants | 负责人、状态；主站为固定 tenant=0 |
| tenant_domains | 规范化 hostname 唯一；域名状态不由代理修改 |
| tenant_memberships | tenant/user/role 关系；owner 与 customer 不混用 |
| tenant_profiles | 每租户唯一品牌；Logo 二进制与受控文案 |
| reseller_applications | 申请、审核人、审核原因与时间 |
| tenant_catalog_entries | tenant/group/model 唯一；首期单一默认组白名单 |
| tenant_price_versions | tenant/model/group/version 唯一；历史只追加 |
| pricing_quotes | request_id 唯一；供货/销售倍率、单位、费率及 payload hash |
| tenant_consumptions | request_id 唯一；预扣、实际 quota、状态、channel 与错误 |
| customer_payment_lots | topup 与 operation 唯一；实付、费用、到账/消费/退款/剩余 quota |
| customer_payment_reversals | 支付批次人工退款/拒付凭证和幂等记录 |
| reseller_wallets | 每租户唯一；pending/available/frozen/debt、精确余量与 version |
| reseller_ledger_entries | operation 唯一；有符号金额、各余额 delta、来源、反向关联和 hash |
| reseller_withdrawals | tenant+幂等键唯一；金额、费率快照、支付宝、凭证、审核人和时间线 |

users、tokens、top_ups、user_sessions、auth_flows、external_identity_claims、passkey_credentials、user_oauth_bindings、logs、quota_data、tasks 均保存 tenant 快照。历史数据回填主站。LOG_DB 独立时额外执行租户日志迁移，不能只迁主库。

## 4. 目录与定价

`TenantPricingService` 集中处理目录、当前价、最低价和保存版本，不向全局 ratio_setting 写代理配置。

- 供货清单从现有启用能力及 pricing endpoint 元数据生成，排除媒体、task、plugin 和不明端点。
- 首期只使用一个默认销售组及模型白名单；客户不能切组，auto group、重试及 Token 路由也受相同限制。
- 每次选渠道（含重试）重新检查租户/目录，目录撤销后旧 Token 不能绕过。
- 销售倍率相对基础价；供货组倍率只用于 KKAI 供货成本。
- 保存使用 `expected_version`，过期版本冲突不会覆盖新配置。
- 保存倍率低于成本时拒绝；供货上涨后统一使用 `max(saved, supply/0.984)`，向上保留足够精度。客户模型广场和新请求使用相同有效倍率，历史版本不改。

整数分的预估规则：

```text
sale = ceil(base_sale × retail_multiplier)
fee  = ceil(sale × 0.016)
profit = sale - fee - supply
minimum_sale = ceil(supply × 125 / 123)
```

这适用于整数分报价/最低价展示。实际消费往往小于一分钱，现金分摊采用下一节的批次累计规则，不能逐条把报价向上取整后当现金收入。

报价在第一次预扣时持久化，保存 tenant/customer、原始模型/销售组、价格版本、实际倍率、供货来源快照、现金单位、预算、费率和请求 ID。重试复用原 PriceData/quote；不得读取调价后的全局配置重新结算。

## 5. 完整预算与纯文本边界

租户使用独立 `TenantBudget` 快照，复用已有预算算法；不会进入会员扣费路径。按量模型使用既有 `MembershipCatalog.ModelLimits` 的 `max_input_tokens`、`max_output_tokens` 和 evidence，约束必须是上游保证的完整累计用量（包括推理/缓存等计费类别），不是宣传的上下文窗口。

文本生成请求必须有受支持的最大输出参数。预算覆盖完整输入、输出、输出份数和所有已支持价格系数，向上取整后才预扣。按次定价模型可按固定调用价格预算；embeddings 不要求生成输出。

最终发出 HTTP 请求前再次验证：max output 不得删除或提高，输出份数不得增加，所有层级拒绝重复 JSON key，防止原始透传与解析器取值不一致。

请求 DTO 和最终出站载荷都检查纯文本边界：拒绝 image/audio/video、托管搜索/图像工具、动态工具来源和 extra_body 等额外计费入口。普通 caller function/custom 工具仅在没有平台工具收费时允许。试点目录仍须核实模型实际能力与元数据一致。

渠道按 `constant.TenantTextChannelTypes` 集中允许清单限制到已验证的 HTTP 适配器（首期 13 类：OpenAI、Azure、OpenRouter、Xinference、Anthropic、SiliconFlow、DeepSeek、Mistral、NewAPI、Sub2API、AdvancedCustom、VLLM、SGLang），目录展示与实际选路取同一范围；pin、affinity、缓存和重试都不能落到 SDK/WebSocket 或未知传输。适配器派发前另有硬拒绝，最终 HTTP 仍执行预算/内容检查。同名模型有多个渠道时只能选安全渠道，不能用一个安全渠道的元数据为其他传输背书。

## 6. 现金、消费结算和微额处理

### 6.1 支付批次

只允许 KKAI 已确认的人民币真实支付充值。支付成功时，在同一账务事务内保存实付人民币分、实际到账 quota 和该笔 1.6% 费用；重复回调不重复建批次或加余额。优惠或管理员直接赠额不能伪装成可分润现金。

消费 FIFO 分配到付费批次。已消费现金和费用使用累计差额：

```text
累计现金 = floor(paid_minor × cumulative_consumed_quota / credited_quota)
本次现金 = 本次后的累计现金 - 本次前的累计现金
累计费用 / 本次费用使用同样规则
精确利润 = 本次现金 - 本次费用 - quote_supply_unit × consumed_quota
```

精确 decimal 供货成本和 `ExactProfitCarry` 保存亚分余量；只把已实现整数分计入 pending。短请求出现亚分负余量时不凭空截断或提前发放收益，后续累计分摊会继续抵扣。报价显示值、用户 quota、AffQuota、余额求和、消费日志都不能直接产生可提现现金。

### 6.2 请求状态机

```text
TokenAuth → 目录/内容检查 → 价格及完整预算
  → 原子 reserve（quote + consumption + user/token 预扣）
  → 上游调用
  → settlement_pending / refund_pending 持久意图
  → 原子 quota 差额 + 支付批次分摊 + pending 流水
  → settled / refunded
```

预扣锁定客户，检查账户/Token、债务、付费批次覆盖和所有未结预扣。未知/结算失败请求未解决前拒绝该客户新消费。正常请求可并发，批次和余额通过事务串行扣减。

`model.SettleTenantConsumption` 是统一请求结算入口，资金服务使用 `SettleResellerConsumptionTx`；controller 和各 adapter 不复制利润算法。账本流水与 wallet 汇总在同一事务写入，请求 ID/hash 保障幂等。结算失败保留意图，由 worker 重试，不退成免费请求。

确认未发送/明确零用量失败才退款。已确认部分输出按实际消费结算。OpenAI/Claude/Responses 对 missing usage 与 explicit zero 分开处理；最终用量缺失、流中断、可能已发送的网络故障等置为 unknown，保留预扣并停止自动重试。

### 6.3 用量不明与进程中断

平台专用接口：

`POST /api/admin/resellers/:id/consumptions/:request_id/reconcile`

要求 `idempotency_key`、`reason`、`evidence` 和 `actual_quota`；确认零消费还必须 `confirmed_zero=true`。actor 来自服务端平台账号，tenant/request 来自路径，不能由请求体覆盖。

只处理 reserved 请求：

- `last_error=upstream_final_usage_unknown`：核对上游凭证后可提交。
- `last_error=''` 的 crash 遗留：还必须 `confirmed_execution_stopped=true`，管理员先核实旧实例退出或该请求排空，证据写入 evidence。绝不对仍可能执行的请求作该确认。
- 其他状态/错误拒绝。相同 key/hash 的已处理请求可安全回放，不同 payload 返回冲突。

审计零 delta 流水和待结算意图同事务保存，worker 完成资金处理。内部证据不返回代理收益页。不能仅按请求年龄自动退款，也无需人工修改数据库。

## 7. 解冻、冲正与提现

正常收益 `available_at=settled_at+24h`。既有 leader worker 每分钟扫描到期记录，唯一 operation ID 避免重复解冻。路由开关关闭时仍注册资金任务，writer/leader/candidate 限制沿用项目机制。

人工退款/拒付接口记录现实中已核实的支付事实，不发起支付渠道自动退款：

`POST /api/admin/resellers/:id/payments/:topup_id/reverse`

首期为整支付批次处理；有 reserved/settlement_pending/refund_pending 时返回冲突，必须先核对请求。撤销未消费余额、标记批次不可再使用；混合批次消费先反向原收益，再使用原 quote 和 available_at 恢复其他有效批次对应部分。不得重新消耗批次或把恢复收益再次冻结 24h。

pending/available 各自反向扣减；已提现部分进入 debt，后续收益先抵债。不得把负债截为零。债务阻止提现和新消费。平台还保留按原结算 operation 的受审计冲正接口。

提现最低 10000 分，人工支付宝。请求原子锁 wallet、检查无债及 available、转入 frozen、创建提现与流水；金额与固定手续费快照不可事后重算。

```text
requested → approved → paid
requested → rejected / failed（释放 frozen）
paid 核销 frozen；approved 付款结果未明时保持冻结，先人工查单
```

每笔要求租户范围幂等键、平台审核人、时间线；paid 要有凭证。不提供自动打款或重复打款按钮。

## 8. HTTP API 与缓存策略

实际路由以 `router/reseller.go` 为准。

| 范围 | 接口 | 鉴权/租户来源 | 重放与缓存 |
| --- | --- | --- | --- |
| 公共 | GET /api/site-config、/api/site-logo | 可信 Host | config no-store；Logo 含内容版本且 Host 隔离 |
| 客户价格 | GET /api/pricing、模型列表 | 当前租户/Token 白名单 | no-store；不暴露供货价 |
| 申请 | GET/POST /api/reseller/applications | 主站 UserAuth | 只看自己；状态检查；no-store |
| 代理身份 | GET /api/reseller/me | UserAuth + TenantAdminAuth | membership capability；no-store |
| 品牌 | GET/PATCH /api/reseller/profile；POST /brand/logo | 同上，membership tenant | 受控内容替换；同内容等效；no-store |
| 目录 | GET/PATCH /api/reseller/catalog；GET /supply | 同上 | 事务替换；目录校验；no-store |
| 价格 | GET/PATCH /api/reseller/prices | 同上 | expected_version 防覆盖；no-store |
| 客户/收益 | GET /api/reseller/customers、/earnings | 同上 | 租户范围脱敏；no-store |
| 提现 | GET/POST /api/reseller/withdrawals | 同上，关键操作限流 | tenant+幂等键/hash；no-store |
| 平台 | GET /api/admin/resellers | 主站 AdminAuth | no-store |
| 平台审核/域名 | PATCH /applications/:id、/:id/status；POST /:id/domain | 主站 AdminAuth | 锁/状态约束、唯一域名；no-store |
| 平台提现 | GET /withdrawals；PATCH /withdrawals/:id | 主站 AdminAuth | 状态与凭证约束；no-store |
| 内部财务 | POST /:id/settlements/reverse、/payments/:topup_id/reverse、/consumptions/:request_id/reconcile | 主站 AdminAuth + handler 二次边界 | 唯一 operation/hash、审计；no-store |

平台表格中的相对路径均以 `/api/admin/resellers` 为前缀；Logo 路径以 `/api/reseller` 为前缀。代理接口没有任意 tenant ID 参数。

## 9. 前端与品牌

路由：申请 `/reseller-application`，平台审核 `/reseller-admin`，代理 `/reseller` 和 `/reseller/{brand,catalog,pricing,customers,earnings,withdrawals}`。客户沿用现有首页、登录、模型广场和认证页面。

启动读取 `/api/site-config`，提供站名、Logo、contact、announcement、homepage_content、status、maintenance 和 enabled_modules。客户价格继续用 `/api/pricing`，不在公开 config 返回供货信息。

SystemBrand、认证布局、标题/favicon、首页和公告读取站点上下文。query/localStorage/通知/GET 去重键都包含 Host；认证用户变更时清理身份相关缓存。普通客户不能进代理后台，代理也不能进平台管理页面。

品牌为受限模板和纯文本，不接受 HTML/CSS/脚本。Logo 服务端检测 MIME、大小、尺寸并完整解码，拒绝 SVG、外链和恶意文件，返回 nosniff。New API / QuantumNous 项目归属保留。

收益页展示参考预估、pending、available、frozen、debt、累计收益及逐笔现金/供货/费用/available_at。预估基于剩余付费批次和当前价，仅参考，不写账本。客户列表不返回密码、完整联系方式、支付资料或 API key。新增文案按项目六语 i18n 维护。

## 10. 配置、迁移与回滚

| 配置 | 作用 |
| --- | --- |
| TENANT_ROUTING_ENABLED | 默认 false；只控制代理入口，不放宽未知 Host |
| TENANT_PLATFORM_HOSTS | 逗号分隔平台 hostname；包含探针使用的有效域名 |
| TENANT_ROOT_DOMAIN | 分配一层子域名的根域；禁止保留前缀与平台 Host |
| RESELLER_WITHDRAWAL_FEE_MINOR | 平台固定提现费用，整数人民币分，默认 0 |
| MembershipCatalog.ModelLimits | 已验证的累计输入/输出上限和证据；供租户预算共用 |

v12 是显式 contract 维护，不在普通启动中自动执行。历史 v1–v11 定义和校验和不改，新 v12 使用固定 GORM 快照校验。

准备流程：备份并验证恢复 → 观察现有 schema，按既有维护流程到 v11 → 排空并停止不支持租户身份的旧实例 → 显式 `kkai-migrate --target 12` → 如有独立日志库，对其执行 `--tenant-log-schema apply` → 两库校验 → 部署 v12 兼容应用（开关保持关闭）。DSN 通过环境或 stdin 注入，不放在文档命令或日志中。

现有主站身份先回填 tenant=0，再规范化身份并建立复合唯一键，最后移除旧 username 全局约束。预检若发现规范化后重复则拒绝，不自动合并账号。SQLite 重建保留原字段/索引/trigger；MySQL 非事务 DDL 失败可幂等重试，维护期间保持停写。

两个构建 profile 都要求 `(12,12,12)`。这份代码不能用于旧 v7/v8/v11 数据库；也不能把仅关开关当作回滚到 v11 的许可。正常应用回滚必须使用已经验证的 v12 兼容版本。恢复旧 schema/二进制需要独立停机、数据回退与恢复方案，不能自动逆迁移删除新账务数据。

## 11. 上线验收边界

本次未修改基础设施或生产。后续独立验收：wildcard DNS、DNS-01 证书、TLS SNI/SAN、OpenResty 受控子域名及 HTTP 308、原始 Host 保留、保留 Host、未知/停用域名、同源 CORS/Cookie、reload 和回滚。固定域名 gzip Worker 不承接代理 Host。

创建一家 pilot 前必须核实供货模型元数据/预算证据、支付人民币金额来源、health probe Host、v12 备份恢复及兼容回滚版本。再跑真实双租户注册/支付/API/调价/24h解冻/提现/冲正链路。关闭站点先暂停租户与域名、再撤销 Edge 路由；资金任务保留，禁止 compose down 绕过发布流程。

实际本地验证命令、结果和剩余验收见[实现与验收记录](./reseller-multitenant-implementation.md)。

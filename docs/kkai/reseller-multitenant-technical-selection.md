# KKAI 代理品牌站技术选型与实现方案

状态：技术选型冻结 1.0；本地实现，未部署
日期：2026-10-19

关联文档：[产品需求](./reseller-multitenant-prd.md)、[详细设计](./reseller-multitenant-detailed-design.md)、[实现与验收记录](./reseller-multitenant-implementation.md)。

## 1. 采用的方案

在现有 NewAPI 内增加逻辑多租户和受限代理后台，复用 Go/Gin/GORM、React/TypeScript/Rsbuild、Base UI/Tailwind。生产目标仍为 PostgreSQL，同时保持 SQLite、MySQL 5.7 和 PostgreSQL 9.6 的代码及迁移兼容。

只运行一套应用、一套主数据库、一套前端构建产物。Redis 只承担缓存和限流，不承担现金账本。全局 Channel、Provider 和模型基础价仍由 KKAI 管理，代理拥有目录选择、销售倍率、品牌和收益权限。

不引入第二套代理平台、每代理容器、消息队列、独立前端或新的财务基础设施。价格服务、资金服务和既有 leader job registry 覆盖首期需求。

## 2. 关键选型与理由

| 领域 | 选择 | 解决的问题 |
| --- | --- | --- |
| 租户入口 | 规范化 Request.Host + 数据库域名映射 | 未知域名拒绝，品牌和认证入口一致 |
| 身份 | users.tenant_id + 主站 owner membership | 允许跨站同名/同邮箱，owner 保留原账号 |
| 权限 | TenantAdminAuth + 租户路由允许清单 | 代理不能获得平台 Admin/Root/Channel 能力 |
| 价格 | TenantPricingService + 不可变 quote | 不修改全局倍率，历史请求和重试价格不漂移 |
| 请求预算 | 复用既有预算服务的独立租户快照 | 输出用量在调用前有资金覆盖，覆盖出站 override |
| 现金 | 实付批次 + decimal 精确分摊 + int64 分账本 | quota 与现金分开，微额请求不虚增利润 |
| 并发 | GORM 事务、lockForUpdate(tx)、唯一业务键/hash | 预扣、结算、冲正和提现保持原子与幂等 |
| 补偿 | 持久化消费状态 + 既有 leader worker | 结算失败可重试，未知用量保留预扣 |
| 品牌 | Host 隔离的 site-config 与同源 Logo | 一套 SPA 服务所有品牌，不接受任意网页代码 |
| 域名 | 受控一层子域名 + wildcard DNS/TLS + OpenResty | 沿用现有部署，Edge 配置单独验收 |

## 3. 数据边界

新增的实际表为：

- `tenants`、`tenant_domains`、`tenant_memberships`、`tenant_profiles`、`reseller_applications`。
- `tenant_catalog_entries`、`tenant_price_versions`、`pricing_quotes`、`tenant_consumptions`。
- `customer_payment_lots`、`customer_payment_reversals`。
- `reseller_wallets`、`reseller_ledger_entries`、`reseller_withdrawals`。

既有 users、Token、TopUp、Session、AuthFlow、外部身份、OAuth/Passkey、Log、QuotaData、Task 增加租户快照。主站历史归属为 `tenant_id=0`。用户唯一性采用租户内规范化 username/email；空邮箱允许多个账号。

owner 是主站用户在代理站上的 membership，管理登录显式选择主站凭据命名空间；默认登录只查本站客户，避免同名客户遮蔽管理账号。客户用户行归属代理租户。两种身份不能互相代用。`User.Group` 和 `InviterId` 不承担租户身份。

## 4. 价格和现金口径

代理销售倍率相对基础价独立设定，不是乘在供货倍率之上。保存时拒绝低于成本的配置；供货涨价后，新请求和客户价格展示统一采用保底后的有效倍率，旧版本记录保持原样。

报价锁定模型/分组、实际倍率、供货价来源快照、现金换算单位、预算、手续费规则和请求 ID。结算不能重新读取当前倍率或当前 quota 换算价。

整数分的底价满足 `sale - ceil(sale × 0.016) >= supply`，即 `ceil(supply × 125 / 123)`。实际收益从实付批次消费部分分摊，费用不按每条微额请求重复向上取整；精确供货成本与亚分余量累计，只有已实现的整数分进入可提现账本。报价展示的预估整数金额不能作为现金来源。

## 5. 失败处理和人工操作

消费状态保存预扣和最终用量意图。确认未消费才退款；已确认的部分消费仍结算。网络结果、最终 usage 不明时保留预扣、停止自动重试并阻止该客户新消费，平台核对后走受审计接口。

退款/拒付只追加冲正；已有在途请求时先核对，拒绝直接撤销支付批次。已提现部分形成真实 debt，后续收益先抵债。提现仍为人工支付宝，平台配置固定分手续费，不自动打款。

worker 按现有 writer/leader gate 每分钟处理待结算和到期收益。关闭租户入口不会丢弃历史资金处理任务。

## 6. 安全和首期支持范围

- Cookie 为 host-only、Secure、HttpOnly、SameSite；跨租户 Token、Session 和 Origin 均拒绝。
- 代理 Host 只开放经过审核的文本、兼容 API 和必要客户/代理接口，新平台接口默认不可用。
- 客户端及最终出站内容都拒绝媒体、平台托管工具和额外计费载荷；普通按量模型需要已验证的完整输入/输出上限及最大输出参数。
- Logo 仅 JPEG/PNG/WebP、2MB、1024×1024；文案为纯文本，保留 New API / QuantumNous 归属。
- 关闭媒体、异步任务、订阅、兑换码、赠额、邀请奖励、动态 OAuth/Passkey 和自助退款。
- React Query、本地持久化、通知和 in-flight 请求去重使用 Host 范围，私有业务接口禁缓存。

## 7. 迁移与交付边界

v12 将全局用户名唯一性改为租户内唯一性，属于 contract 迁移。两个构建 profile 均要求 `(12,12,12)`，不能套用旧 v7/v8 发布判断。普通启动/普通 Apply 不会隐式迁移 v12；必须使用显式维护目标，独立 LOG_DB 另有迁移和校验。

应用 feature flag 默认关闭，但 Host 允许清单一直生效。上线必须先验证探针 Host、旧实例排空、历史身份冲突、备份恢复、v12 兼容回滚版本，再按现有手工发布 runbook 执行。迁移完成后关闭 feature flag 不足以允许旧二进制回滚。

本次只交付本地代码、文档和定向测试；wildcard DNS/TLS/Edge、真实支付和 pilot 全链路属于上线验收，未在本次执行。

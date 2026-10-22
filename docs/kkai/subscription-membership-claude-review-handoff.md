# KKAI 订阅会员：Claude Code 独立审查交接

整理日期：2026-10-19（北京时间）。交接基线：`a58fb93c2e1dc1322dcfa75ac7883c6317b38aae`。

> **2026-10-22 当前规则变更：** 后台发布折扣后，所有有效新老会员的后续新请求统一采用
> 当前档位最新发布值，取消购买版本及新增组首发系数锁定。已预扣请求仍按完整原快照结算；
> 订单实付金额、额度、有效期和历史数据不改写。第 3 节现行规则及第 9 节审查方向已同步。
> 第 4、6–8、11 节的实现、测试、发布及复核记录保留历史语境；其中依赖旧锁系数规则的
> 结论不再作为当前验收依据，尤其第 11 节 P1-2 的当时复核。本次文档更新不证明已经发布。

这份文档覆盖本轮整套订阅会员功能，包括需求确认、会员计费、订单支付、升级、异常核账、管理端、用户页面及上线收尾。请基于代码和可复现证据独立找问题，不把本文的实现描述或已有测试通过当作正确性证明。

## 1. 项目地址与打开方式

| 项目 | 地址 |
| --- | --- |
| 当前应用仓库（审查主目录） | `/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai` |
| GitHub 仓库 | <https://github.com/Wangjunkai-1996/new-api-kkai> |
| 应用生产分支 | `production/kkrich`，upstream 为 `origin/production/kkrich` |
| 会员实现审查基线 | `a58fb93c2e1dc1322dcfa75ac7883c6317b38aae`，不含后续控制器 pin 与文档归档提交 |
| 在线站点 | <https://api.kkrich.ltd> |
| 用户会员中心 | <https://api.kkrich.ltd/membership> |
| 用户会员倍率说明页 | <https://api.kkrich.ltd/membership/ratios> |
| 管理端入口 | <https://api.kkrich.ltd/subscriptions> → 会员管理；配置、异常订单、待核对调用等位于此功能内 |
| 配套基础设施仓库（需要查发布实现时再读） | `/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/kkai-infra` |
| 生产发布入口手册 | `/Users/tokk/Documents/Codex/runbooks/newapi-upgrade-and-deployment.md` |

建议从当前应用目录开始：

```sh
cd /Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai
git status --short --branch
git rev-parse HEAD
```

会员功能发布结束时源码已经提交、推送并上线，精确历史证据见第 8 节。交接文档在上线后生成，与原始审查报告及第 11 节复核结论一并归档；文档归档不改变运行时代码。请先读根目录 `AGENTS.md`；审查前端再读 `web/default/AGENTS.md`。不用回到历史 worktree，也不需要为了审查先 fetch、pull、换分支或重建环境。

## 2. 希望你审什么、交付什么

请做一次完整的独立代码审查，目标是发现真实的业务错误、资金风险、安全问题、并发与恢复漏洞、使用障碍和缺失的验收覆盖。不要只看最后两次倍率页提交，也不要只审样式或测试覆盖率。

按以下顺序检查：会员账本和资金来源 → 付款和升级 → 在途请求及异步恢复 → 目录/分组/折扣版本 → 接口权限及兼容性 → 后台配置与用户页面。必要时按不同专长并行审查，最后交叉核对同一条调用链。

最终报告请使用中文，先列发现，再列验证缺口。每个问题写明：

1. 严重性（P0/P1/P2/P3）、准确文件和行号。
2. 触发条件、实际行为、违反的业务约定、影响范围。
3. 最小复现或直接代码证据；不能复现的推测应明确标注。
4. 最小修复建议、需要补的回归用例。

另给出“启用售卖前必须解决 / 可以后续处理 / 需要经营者配置或确认”的结论。没有发现问题时直接说明审查范围、证据和未验证项，不为凑数提出重构，也不要把有意约束当作缺陷。

本次交接请求是让你审查并出报告，未要求直接修复、提交、推送、迁移、打开会员开关或重新发布。线上开启会员、发真实收费请求、下单付款和修改业务数据都不属于本次审查。优先本地代码与隔离测试；执行测试遵守仓库的定向验证和全量检查审批规则。

## 3. 最终确认的业务规则

详细定稿见 [subscription-membership-requirements.md](subscription-membership-requirements.md)。以下是不可遗漏的关键约束。

### 3.1 产品、购买与有效期

- 目标：让常用用户预付额度，提高月度复购与收入；优惠来自调用倍率，不是额外送额度。
- 档位与计费组均为后台动态配置；不在业务代码中写死 VIP/SVIP、三档数量或显示名称。首发经营方案为黄金、钻石、王者。
- 计划售价分别为 CNY ¥600 / ¥2000 / ¥5000，额度显示值对应 600 / 2000 / 5000，按现有充值的额度显示单位 1:1；不要误解为必须做 CNY↔USD 汇兑。
- 在默认 `QuotaPerUnit=500000` 下，内部套餐额度分别为 `300000000`、`1000000000`、`2500000000`。套餐总额与单次预扣上限不同，不能用 int32 截断套餐总额。
- 折扣系数由管理员配置，不能假设已经配置完全部生产档位、折扣及模型上限。
- 外部单次付款确认后生效，不使用钱包买会员，不自动续费；不做自助退款、自助取消、提前续费排队或按日补差。
- 会员从支付成功开通起有效 `30×24` 小时；不按自然月，不每日/每周/月重置额度。
- 一个用户只有一个有效会员等级。有效且未耗尽时只能升级，不同档并行、同档重复购买、降档都不允许。
- 到期或实际结算耗尽任一发生就结束权益，可立即重新购买任意档。到期剩余额度作废，不转钱包；原有钱包余额不清零。
- 预扣暂占全部可用额度不等于实际耗尽，失败释放后不能丢掉未过期权益。

### 3.2 升级

- 支付目标档完整售价，目标套餐完整额度与旧剩余额度原数合并，不折算旧余额、不退差价。
- 升级成功后统一重新计 30 天，新请求采用目标档最新已发布折扣，后续发布与该档其他有效会员同步生效。
- 不继承旧会员更便宜的历史折扣，不做跨版本升级保价；同一版目录里高档不能比低档更贵。
- 升级后不会在高档到期时恢复旧等级。
- 代码通过同一会员账户累计总额、保留已用与在途占用实现升级；审查重点是经济效果与上述规则一致、旧请求仍按原快照结算。到期/耗尽后重购是新账户，不能把旧请求退回新账户。

### 3.3 动态分组与定价

```text
B(g)：计费组当前基础倍率
U(u,g)：用户按原有用户组规则获得的非会员倍率
D(m,g)：新请求授权时，该会员当前档位最新已发布的逐组相对折扣系数

正式额外优惠 D < 1：订阅倍率 = min(U, B × D)
未配置/无额外优惠 D = 1：订阅倍率 = U
钱包倍率始终 = U
```

- KK 座上宾等用户原有优惠保留，与会员候选价取低，不折上折。`D=1` 不能意外把原有较高倍率降到基础倍率。
- 各组的系数可以不同；后台更新折扣或基础价影响所有有效新老会员后续新请求，已预扣请求的完整价格快照冻结。
- 每组分别满足高档系数 ≤ 低档系数 ≤ 1；未配置视为 1，相等允许，前后端都要校验并定位冲突。
- 用稳定标识关联档位与分组，不依赖显示名称；改名不丢权益、停用不删历史，同名重建不能串用旧身份。
- 会员不解锁用户本来无权使用的组/模型/渠道。用户有权使用的组即使没有额外折扣，也能使用订阅额度。
- 已发布组及新增组首次、后续调价均适用于所有有效新老会员的新请求，不按购买版本或首次发布版本锁系数。未发布时按 1；目录版本保留审计，不重写订单和已预扣请求。

### 3.4 资金来源、预算与异常

- 订阅优先且钱包自动接续；单请求不混扣。
- 订阅仍有余额但不足本次预扣时，保留剩余会员额度，整笔按钱包价格重新授权；后续小请求仍可消费订阅。
- 钱包仍不足，在请求发往上游前拒绝；不透支、不先发再失败、不在结算时跨扣钱包。
- 预扣时冻结本次来源与完整计价快照。处理中到期、升级、调价不改变它。
- 正常请求预留可验证最大费用，完成后释放多余预留；没有可信上界的调用应明确拒绝，而不是伪造安全估算。
- 上游异常超过授权上限时，最多扣已授权金额，平台承担超额，并记录应计额/实扣额/差额和告警。
- 失败释放回原来源；到期不会因返还延长，重购不会收到旧账户退款；扣费、释放、回调、统计均需防重复。
- 已发送且结果不明的请求不能直接当零费用退款或盲重试，应保留预留并进入受约束的核账/恢复流程。
- Image Studio 另保留“安全归档且可交付部分才收费”规则；未交付成本由平台承担，不能与普通 Relay 结果未知的处理混为一谈。

### 3.5 支付、异常单与充值优惠

- 下单冻结售价、币种、额度、等级和有效期，保留目录版本用于审计；客户端不能篡改，回调不能用后来的金额或额度替换订单条款。付款后的新请求采用该档最新折扣，不承诺长期锁定订单版本系数。
- 首发实际使用既有易支付，付款方式跟随现有支付配置，不能把支付宝写死为唯一代码分支；不新增周期扣款。
- 重复通知幂等，不重复发权益。超时未付款可关闭；关闭后确认到账、金额/方式不符或收款后无法开通的订单要保留付款事实，进入异常待处理，不能直接丢单。
- 已收款异常由客户联系后人工核实；运营建议为 1 小时内首次响应、24 小时内处理，不是自动退款定时器。订单付款有效期当前实现为 45 分钟，与运营 SLA、上游请求截止时间是三件事。
- 取消新充值折价/赠额，保留旧已承诺订单、历史钱包余额、原用户组倍率优惠、邀请返佣与兑换码。核查切换何时生效及是否可逆，不能默认为只改一个 UI 提示。

## 4. 本轮具体完成的实现

### 4.1 后端与数据

- 沿用 Go/Gin/GORM 及既有支付、Relay、任务体系，没有另建支付服务、消息队列或新应用依赖。
- 增加 schema v11 会员扩展，保留原 schema v10 Image Async 及旧订阅历史；会员运行开关开启时检查实际 schema。
- 增加 8 张伴随表：`kkai_membership_catalog`、`kkai_membership_releases`、`kkai_membership_groups`、`kkai_membership_users`、`kkai_membership_accounts`、`kkai_membership_orders`、`kkai_membership_payments`、`kkai_membership_billing`。
- 目录整批发布、不可变版本、用户当前账户和待付订单互斥、独立收款事实、请求持久账本、行锁/条件更新与幂等保护。
- 会员与旧有效订阅/待付订单兼容和互斥；提供冻结条款的购买、升级、继续支付及异常处理。
- 最大预算覆盖 token、多模态/音频、工具、表达式、任务上限；出站时再次核对已授权参数。
- 集成普通 HTTP/SSE、Responses WebSocket、Realtime 逐轮、图片、Image Studio、Midjourney、原生 Task/Suno 等路径的会员资金和冻结计价。
- 持久化 dispatch/截止时间和未知结果；按作用域提供核账、恢复、原来源释放、封顶结算及审计。
- 实际展示复用 `ResolveMembershipRatio` 定价规则，返回用户真实钱包价、会员价和节省比例，只展示其可访问组。

### 4.2 管理端与用户端

- 默认前端 `web/default` 新增会员中心、当前会员与剩余/可用/暂占额度、动态套餐卡片、全价升级确认、订单继续支付及钱包入口。
- 原 `/subscriptions` 保留旧订阅页签，新增会员管理：档位、售价/额度、分组折扣、模型授权上限、异常订单、待核对调用。
- 简化操作：新套餐内部标识自动生成，按支付渠道条件显示产品字段；复用易支付时不要求填写无关第三方产品 ID。
- 修复订阅管理长页无法下滑、页签尺寸与保存入口可达性；分组默认发布、系数默认 1，管理员只修改需要优惠的组；修复空 `model_limits` 被错误拦截。
- 新增 `/membership/ratios`：通栏对照布局、动态档位/分组、普通/钱包/会员实价、实际节省、搜索、只看优惠、价格解释、各档方案与本人当前权益展示。
- 倍率页桌面对照表，内容宽度不足/档位过多时切换卡片；校验明暗主题、320/390/768/1024/1440 宽度及无横向溢出。
- 提供导航、会员中心/钱包/模型广场关联入口；原 `group_ratio` 仍保持钱包价语义，不在前端重复乘会员系数。
- 文案覆盖现有 7 个 locale（zh、zh-TW、en、fr、ja、ru、vi）。会员主体与后续倍率页分别增加翻译键。
- 最后补充混合发布/回退兼容：`membership_ratio_quotes` 不作为整个控制台的全局必需能力；倍率页独立拒绝缺失/畸形报价，不展示误导性零价或零优惠，其他控制台保持兼容。

## 5. 审查代码入口

以下为入口地图，不代表只审这些文件；沿调用者和被调用者继续追踪。

| 领域 | 主要文件（相对应用根目录） |
| --- | --- |
| 路由和权限 | `router/membership.go`、`router/api-router.go` |
| 目录、会员、订单 API | `controller/membership.go`、`controller/membership_config_test.go`、`controller/membership_presentation_test.go` |
| 支付与充值优惠 | `controller/membership_payment.go`、`controller/membership_topup.go`、既有 `controller/subscription_payment_*.go` |
| 会员数据/配置/兼容 | `model/membership.go`、`model/membership_catalog.go`、`model/membership_compatibility.go` |
| 付款/开通/升级 | `model/membership_order.go` |
| 预留/结算/核账 | `model/membership_billing.go`、`model/membership_relay_billing.go`、`model/membership_billing_review.go`、`controller/membership_billing_review.go` |
| 预算/冻结/dispatch | `service/membership_budget.go`、`service/membership_billing.go`、`service/membership_dispatch.go`、`types/membership_pricing.go` |
| 共享资金和统计路径 | `service/billing.go`、`service/quota.go`、`model/task_accounting.go`、`common/quota_math.go` |
| 展示报价 | `service/membership_presentation.go`、`controller/pricing.go` |
| 表达式预算 | `pkg/billingexpr/maximum.go`；先读 `pkg/billingexpr/expr.md` |
| Realtime/Responses | `service/realtime_membership_billing.go`、`relay/channel/openai/relay_realtime_membership.go`、`relay/responses_websocket.go` 及相关 handler/测试 |
| Task/MJ/Image Studio | `service/task_membership_billing.go`、`model/task_membership_billing.go`、`model/midjourney_membership.go`、`service/task_billing_recovery.go`，以及对应 controller、worker、polling |
| schema 与启动 | `pkg/kkaimigrate/schema_membership.go`、`pkg/kkaimigrate/membership_database_test.go`、`main.go` |
| 前端会员全集 | `web/default/src/features/membership/`（页面、admin、API、form、hooks、types、components、测试） |
| 管理端集成 | `web/default/src/features/subscriptions/index.tsx` |
| 用户路由 | `web/default/src/routes/_authenticated/membership/`、`web/default/src/routeTree.gen.ts` |
| 模型广场/钱包/导航 | `web/default/src/features/pricing/`、`web/default/src/features/wallet/`、`web/default/src/hooks/use-sidebar-*.ts` 及 header 导航 |
| 前端发布兼容 | `common/console_contract.json`、`web/default/console-contract.json`、`web/default/src/lib/console-contract*`、`web/default/src/hooks/use-console-contract*` |

API 范围：用户 `UserAuth` 下的 `/api/membership/{catalog,self,orders,orders/:trade_no}` 和创建订单/继续支付；管理员 `RootAuth` 下 `/api/membership/admin/{config,publish,orders,billing-reviews}` 及 resolve；易支付 `/api/membership/epay/notify` 使用签名验真而非登录态。检查详细记录的用户归属、鉴权、越权、脱敏、缓存和限流，不要仅确认路由挂了中间件。

## 6. 提交范围与阅读顺序

会员核心首提交为 `9376de6b7`，其父为 `71283181467d84c113386dc6b0ff4325eccbfcf3`。核心提交涉及 174 文件、约 1.8 万新增行，后面仍有多项修复，因此不能只看最近一条 diff。

| 提交 | 内容 |
| --- | --- |
| `9376de6b7` | 整套会员计费、支付、数据、任务集成、前端和测试 |
| `76da2a42d` | 运行关闭且会员目录尚未准备时的管理端兼容 |
| `02a8763b2` | 客户端取消时仍结算 WebSocket 上游 usage，需结合会员账务审 |
| `838ef1560` / `4815676e6` | v10→v11 bridge 准备及已验证的 v11 feature 契约 |
| `2eb4746e0` / `b6d127d86` | 目录编辑时状态与完整快照类型修复 |
| `5804ac221` | 简化档位标识与按渠道显示支付字段 |
| `904e11119` / `a42f446ab` | 订阅管理滚动和布局修复 |
| `9fe32e2d0` | 分组默认发布、默认系数 1 |
| `ed2aefb81` | 空模型上限配置兼容 |
| `55744a4ed` | 会员倍率对照页、实际报价及测试 |
| `a58fb93c2` | 倍率页缺字段防误显和后端回退兼容 |

可先用本地对象浏览：

```sh
git show --stat 9376de6b7
git log --oneline 712831814..a58fb93c2
git diff --stat 712831814..a58fb93c2
git show 9376de6b7 -- model service controller relay pkg web/default
git show 55744a4ed
git show a58fb93c2
```

这个区间混有其他并行的图片、视频等修改，不把整个区间误称为会员单一改动。会员与共享路径的交叉点要审；独立不相关改动只作为基线背景。

## 7. 测试证据与还没证明的事情

### 7.1 历史实现阶段（2026-10-18）

原记录见 [实现验证](subscription-membership-validation.md)、[前端验证](subscription-membership-frontend-validation.md)、[联调步骤](subscription-membership-integration.md)。它们保留了隔离 worktree、旧基线、“未提交/未上线”等当时状态，不能直接覆盖第 8 节的最终交付记录。测试结果也是分阶段记录，不等于最终 HEAD 上全部重新运行。

- 本地真实应用进程 HTTP 27 项：初始化、迁移、加密登录、目录配置、普通用户访问、401/403 隔离等通过。
- 易支付专项 84 项：真实后端进程执行下单、固定金额签名表单、协议签名回调、三档升级、异常/人工处理、幂等；回调由本机测试生成，没有外部转账。
- SQLite 与本机 MySQL 5.7 / PostgreSQL 9.6 的真实 DDL、多连接行锁和事务测试：迁移/历史保留、并发收款只开一次、预留不超支、封顶、统计幂等和失败回滚等通过。PostgreSQL 首次 DSN 格式错误后只重跑相应范围通过。
- 普通/流式/Responses WS/Realtime/Image Studio/MJ/Task/Suno 的定向回归；本地模拟上游、图片存储、真实 worker 或局部处理入口，不等于真实供应商完整链路。
- Stripe 仅用 SDK 本地签名经 HTTP 验证，不等于商户沙箱 checkout/webhook。
- 历史前端易支付阶段为会员 21 项 + API 2 项；定价 46 项、组合测试等是不同阶段，不累计成一次“全通过”数字。

### 7.2 最后倍率页与发布阶段（2026-10-19）

应用根目录已通过的定向命令：

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./model ./service ./controller -run 'Test(MembershipCatalogQuotesUseActualWalletPricesAndLatestTerms|MembershipGroupQuotes|MembershipPresentationAPIsExposePricesOnlyForAccessibleGroups|MembershipFirstGroupPublicationAndOrderTermsAreImmutable|MembershipCatalogConflictsAndDisabledSalesPreserveEntitlements)$' -count=1 -timeout=120s
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembershipGroupQuotes' -count=1 -timeout=120s
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembershipCatalogQuotesUseActualWalletPricesAndLatestTerms$' -count=1 -timeout=120s
go test ./common -run '^TestConsoleContract$' -count=1 -timeout=60s
```

`web/default` 下已通过：

```sh
bun run test src/features/membership/ratios-page.test.tsx src/features/membership/membership.test.tsx
bun run test src/lib/console-contract.test.ts src/hooks/use-console-contract.test.tsx
bunx tsgo -p /tmp/kkai-membership-tsconfig.json --noEmit
./node_modules/.bin/oxlint -c .oxlintrc.json src/features/membership/ratios-page.tsx src/hooks/use-console-contract.test.tsx src/lib/console-contract.test.ts
```

会员/倍率页 38 项、兼容契约 16 项，共 54 项通过；局部类型及 lint 通过。翻译同步和定向格式检查在对应修改阶段通过。`/tmp` 类型配置属于历史本机证据，重跑前先确认是否存在及路径是否正确。

后端 external/feature 镜像与最终 default 前端正式构建已通过，精确构建命令保存在第 8 节发布记录内。本次交接仅写文档，不需要为它重建或重发。

### 7.3 必须如实保留的验证缺口

- 全仓测试、全量 lint、全应用 TypeScript 没有完整运行；没有可确认的本会员专项 Go `-race` 记录，真实数据库并发测试不能冒充 race detector。
- 历史较大 scoped TypeScript 曾触发同基线可复现的 PageTransition 路由上下文诊断；不能把局部检查通过写成全量类型通过。
- 目前未完成本会员系统真实支付商付款及真实模型全链路/断线联调。既有充值易支付正常，不自动证明新增会员订单回调链路已经真实商户验收。
- 最新线上验收在会员运行关闭时进行，没有开启真实会员报价/购买/消费验证；启用态由本地测试和 fixture 覆盖。
- 上游模型累计费用上界需要经营者据供应商真实强制限制填写，目录字段本身不能证明这些外部承诺成立。
- Realtime 当前要求手动、单响应、关闭独立转写；自动 VAD、托管工具、图片输入等会被明确拒绝，需要审查是否清楚提示/是否满足实际客户用法，不能谎称全部兼容。
- 普通消费展示日志与资金事务沿用独立日志写入，崩溃窗口可能缺一条公共展示日志；会员持久账本才是资金凭据。不声称所有任务终态、资金和跨库日志完全原子。
- 易支付 checkout claim 后进程崩溃的历史说明是依赖 45 分钟订单过期恢复，值得复核当前实现与用户体验。

## 8. 已完成的上线与证据位置

以下是 **2026-10-19 本次发布结束时的观察记录**，不是未来实时状态。此交接整理未重新连接生产。若之后确需查询当前生产，先按发布手册与权限范围操作，不能复用本文的颜色/版本当作现场事实。

| 项目 | 本次发布记录 |
| --- | --- |
| 后端正式版本 | `kkai-prod-20261019.1792407452-55744a4ed`，source `55744a4edb2059974c60b0ed731c36c45bd9765f` |
| 前端正式版本 | `kkai-frontend-20261019.1792407847-a58fb93c2`，source 为最终提交，format 2 |
| 已安装控制器 pin | `bef062cc1a6cfbd7acfaeeebe1cb05e9d5631290`，`router-v3-staged` |
| schema 证据 | 独立只读观察 v11，最后本次应用发布使用 feature；本次最后发布未做 schema 迁移 |
| 后端发布 | stage → 私有候选验收 → 10%/50%/100%各至少120秒 → handoff → promote/finalize 完成 |
| 健康与回退 | 即时及五分钟后均目标版本 HEALTHY、rollback ready；前端回退计划为 `backend-only` |
| 前端发布 | 后端事务完成后 prepare/activate；public HTML、版本化 JS/CSS、no-store 入口、首页 SHA 与所选树一致 |
| 页面验收 | 已登录浏览器打开 `/membership/ratios`，导航、标题、套餐入口正常，显示“会员功能暂未开放” |
| 会员开关 | `KKAI_MEMBERSHIP_ENABLED` 运行能力保持关闭；不等于销售开关，也不代表生产商业目录已验收完毕 |
| 本次清理 | 候选隧道/socket、上传包、本地大型归档已清理，小型 metadata 与证据保留；回退/备份资产未删 |

最后前端修复没有后端代码变更，因此前端/后端 source SHA 不同是 format 2 的正常独立交付，不能把它直接报成版本错配。

本机发布证据根目录：

```text
/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/.local-releases/membership-ratios-release-20261019.csrbrc/
```

其中 `release-complete.md` 是最终上线索引，`release-checks.md` 含精确检查命令与结果；另有 `promote.log`、`status-immediate.log`、`status-delayed.log`、`frontend-activate.log`、`public-frontend-acceptance.json`、`rollback-plan.log`、`error-review.md`、`membership-ratios-online.png` 等。`.local-releases` 是本机证据，不一定在远程仓库；异地审查时应把不可用证据明确列出，不能假设拿到了。

模型错误聚合：基线 892 事件/57 错误（6.3901%）；首发布窗口 894/71（7.9418%）；紧接窗口 903/55（6.0908%）。既有 502/503 仍存在，首窗上升后回落；原始窗口保留。执行者结合健康、变更范围和复测接受发布，没有把这些错误直接归因上游，也没有仅因既有 503 自动回滚。请勿把这写成业务零错误或启用态会员模型调用已通过。

控制器曾提示保留 v10 schema snapshot 时延后自动 release/image 清理，这是现存备份保护规则；未强删镜像或回退资产。若要审基础设施行为，遵守 infra 自身规范，不混入本次应用只读审查。

## 9. 最值得交叉复核的风险点

这些是审查方向，尚不是已证实的 bug；请给证据后再定级。

1. **大额与累计统计边界**：王者总额度 `2500000000` 超过 int32，会员表为 BIGINT，但单次预扣、token、用户/渠道累计统计及转换 helper 仍有 int32 边界。沿完整资金链核对，尤其累计多笔后不能溢出、卡死、负扣款或静默少记账。
2. **并发和锁顺序**：下单/回调/升级/返还/到期/目录发布并发；重复 receipt、request ID、人工 operation ID；死锁重试是否会重做外部调用，SQLite/MySQL/PostgreSQL 是否保持同一经济效果。
3. **跨升级和重购的在途请求**：升级合并时暂占、已用和可用是否正确，旧请求晚到返还是否进入正确账户，revision 与固定价格是否一致，不能复活旧等级或多发额度。
4. **选源和超额**：会员不足后钱包应按钱包价全额重算；token限额、免费模型/倍率0/系数1、无折扣组、异常超额及退款全部走原来源，不能绕过授权。
5. **订单与外部付款**：签名歧义、字段重复、错金额/币种/渠道、迟到付款、checkout未知/崩溃、人工处理重放、已付并发升级冲突，收款事实与权益原子边界。
6. **预算的实际可信性**：隐藏上下文、缓存、音频/工具循环、表达式分支、任务时长/份数、参数覆盖/渠道重试后的真实出站值，声明上限是否被真正执行；不能让正常请求长期由平台补贴。
7. **未知结果和恢复**：HTTP/SSE/WS发送后断线、usage=0与usage未知、重复轮询/outbox、人工核账与晚到成功竞争；Image Studio安全交付与普通Relay未知结果的不同处理是否正确。
8. **运行/销售开关和旧系统**：关闭销售不应撤销已有权益；运行能力关闭是否安全处理旧账本、回调和恢复；原订阅、旧未付充值单与充值优惠切换是否兼容。
9. **动态目录**：已有组及新增组多次发布后，有效新老会员新请求统一采用最新折扣，已预扣请求不改价；组改名/停用/恢复/同名重建、并发版本冲突、单档/三档/更多档、档位隐藏/删除和订单快照完整性。稳定身份规则在现有分组管理入口是否真能执行。
10. **后台可用性**：滚动到表格底部和保存按钮、手机/小屏、模型上限留空与已配上限、内部ID自动生成及支付字段条件显示、错误定位；不要只测孤立组件。
11. **用户价格展示**：实际用户组权限、不折上折、本人当前档与在售档位采用同一最新目录、折扣及基础价更新、失效/耗尽、缺字段/畸形字段、旧后台回退、目录隐藏与缓存失效；新请求报价与服务端授权一致，历史请求仍展示原快照。
12. **权限、审计、维护性**：用户不可读写他人订单/账本；Root管理限制；不泄露支付checkout信息、核账私有原因、密钥或正文；冻结快照存储大小/查询索引/目录发布成本/锁持有时长；已有组件和业务是否被不必要复制。

## 10. 其他参考资料及阅读优先级

1. 本交接 + [业务定稿](subscription-membership-requirements.md)：先理解最终约束。
2. [技术实现说明](subscription-membership-technical-plan.md)：读架构与限制；其中隔离分支、未提交/未部署状态是历史，按本交接第 8 节更新认识。
3. [实现验证](subscription-membership-validation.md)、[前端验证](subscription-membership-frontend-validation.md)：核对真实覆盖、失败后修正和未验证项，不汇总虚高测试数字。
4. [联调步骤](subscription-membership-integration.md)：仅参考隔离测试方法；历史端口、容器、临时脚本、worktree路径先确认，不照抄连接生产。
5. 本机 `.local-releases/.../release-complete.md` 及原始证据：核对最终发布；生产能力是否已安装以当时记录或新授权现场检查为准。

交付期待：指出真正可能损害资金、权益或正常使用的问题，给出小而可靠的修复方向；把经营配置、暂不支持的调用契约、历史文档过时与真实实现 bug 分清楚。

## 11. Claude 报告的独立复核记录（2026-10-19）

本节为当时业务规则下的历史复核，保留原结论及命令。P1-2 与下方发布提示段落讨论的
购买快照/首次发布锁定规则已于 2026-10-22 废弃；当前应验证发布后新老会员的新请求
同步更新，不能再据本节要求保留旧系数。已有授权按原快照结算的资金规则继续有效。

原始报告见 [kkai-membership-code-review-20261019.md](../../kkai-membership-code-review-20261019.md)，保留原文，不能将其中建议直接视为已确认缺陷或已实施修复。复核采用三个角色分别检查账本、目录规则、支付与测试覆盖；审查基线之后的三个应用提交仅更新控制器 pin，会员源码未变。本次没有修改会员逻辑，也没有访问生产。

| 报告发现 | 复核结论及证据 |
| --- | --- |
| P1-1：升级后旧请求返还归属错误 | 按报告场景不成立。升级保留同一账户 ID，追加额度并保留已用、暂占；旧请求释放原暂占并按原快照记实际消费。revision 只阻止新请求采用过时报价，不应阻止已有授权结算。加入报告建议的 revision 相等检查会使升级前的请求无法结算或释放。`model/membership_test.go` 的 `TestMembershipSnapshotUpgradeAndOriginalReservation` 直接覆盖；到期重购的账户隔离另有测试。 |
| P1-2：首次发布为 1 后不追补优惠 | 报告混淆了正式发布的 1 与尚未发布时的临时 1。[业务定稿 §7](subscription-membership-requirements.md#7-折扣版本与新增分组)明确正式发布的 1 按购买快照锁定；仅未发布组在首发时补有效老会员。报告场景购买时已经发布，不经过 `FirstReleaseID` 回退，其建议补丁也不能改变该场景。 |
| P2-1：易支付 checkout 失败后卡住 | 保存失败已有精确释放和立即重试，`TestMembershipEpayLocalCheckoutFailureCanRetry` 覆盖持久化失败及迟到清理不擦除 ready。进程在 claim 后突然退出、或数据库不可用导致无法释放，确有滞留至订单过期的恢复体验缺口。不能通过去掉清理条件解决进程崩溃。 |
| P2-2：排序冲突提示未给出比较对象 | 后端已有冲突分组和当前档位，前端还会定位具体输入。可补比较档位和系数，属于低优先级提示优化，不是已证实的售卖阻塞问题。 |

后台按已确认需求默认发布全部分组、缺省系数为 1，因此保存时可能将尚未配折扣的新组正式发布为无优惠，后续调价不会追改已锁权益。建议补充发布影响提示或变更预览，避免运营误解；不能擅自把“正式无折扣”重新解释成“尚未配置”。

报告列出的验证缺口应按实际覆盖收窄：已有升级与原预留结算、易支付金额与签名校验、Realtime 第二个响应拒绝、Image Studio 会员部分交付、充值优惠开关测试。尚可补充三请求并发升级、多组发布中途故障整体回滚、同档会员连续请求间基础倍率变化、MJ 21/22 受理码、旧未付充值订单跨优惠切换回调等专项；未完成真实商户付款与真实模型完整链路的事实仍保留。

本次运行以下定向命令，均退出 0，分别选择 3 个和 4 个顶层测试；没有重跑全仓测试或全量前端检查：

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./model -run '^(TestMembershipSnapshotUpgradeAndOriginalReservation|TestMembershipPreauthorizationIsNotExhaustionAndOldRefundIsIsolated|TestMembershipFirstGroupPublicationAndOrderTermsAreImmutable)$' -count=1 -timeout=120s
KKAI_MEMBERSHIP_ENABLED=true go test ./controller -run '^(TestMembershipEpayLocalCheckoutFailureCanRetry|TestMembershipEpayVerifiedMoneyAndDuplicateCallbacks|TestMembershipEpayCallbackRejectsForeignMerchantAndTampering|TestMembershipLaunchRemovesOnlyNewTopUpPromotions)$' -count=1 -timeout=120s
```

这些结果支持上述具体结论，不代表全部并发组合、外部支付、真实供应商链路或当前生产状态已经验收。

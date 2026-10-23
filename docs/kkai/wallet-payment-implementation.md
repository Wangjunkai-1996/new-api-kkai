# 钱包充值生命周期实施记录

日期：2026-10-23。本文件记录实际实现、验证和未完成的外部依赖；产品行为与接口仍分别以 [PRD](./wallet-payment-lifecycle-prd.md)、[UI 规格](./wallet-payment-ui-spec.md) 和 [后端规格](./wallet-payment-backend-dev-spec.md) 为准。

最新用户确认已实施：[本站超时与废单修订规范](./wallet-payment-local-timeout-spec.md)。本站 30 分钟超时和主动结束允许结果确认后返回充值；这独立于渠道关单，取代此前“未确认渠道关闭则一直锁定当前流程”的规则。

## 本地交付范围

本次在现有工作目录实施，不创建分支或 worktree。没有执行生产构建、部署、真实数据库迁移、历史订单处理、代码提交或推送。

- 钱包先恢复服务端流程；空闲、创建、当前订单、处理中和结果互斥。订单阶段隐藏独立余额卡、充值、兑换、Creem 和订阅购买模块。
- 支付在新标签页打开；原订单及原结账入口持久保存。关闭标签页不会取消订单，也不会清除当前流程。
- 按服务端事实显示继续支付、结束本次充值、渠道取消或核实；本站结束／超时和可靠渠道结果均保留，点击“返回充值”且确认对应流程后才恢复表单。本站废单无继续支付、取消、人工核实或补单入口。
- 五个旧支付创建入口统一使用幂等和当前流程门禁。`POST /api/user/topup` 继续是兑换码入口。生命周期安装后，缺少 `Idempotency-Key` 的旧支付客户端返回升级要求；classic 未移植完整钱包 UI。
- 本人和管理员历史支持同一范围内的六组状态计数、全龄未完成入口和不受 30 天限制的本人精确查号。历史详情不替换当前流程。
- 历史核实采用只读报告、冻结范围和订单上界、独立授权引用、版本控制、租约及检查点；暂停和续跑只允许平台超级管理员交互式会话。开发和部署授权不包含实际执行历史处理。
- 运行时 canonical schema 验证共享 30 秒正负缓存、3 秒检查超时和并发合并；更换 DB 对象立即失效，到期重新验证漂移。维护检查保持即时验证，模型物理列检查不缓存。报价输入防抖 300ms，输入变化立即隐藏旧报价并禁止用旧金额确认。

## 渠道能力边界

| 渠道 | 原支付入口恢复 | 认证付款核实 | 主动取消／可靠超时关闭 |
| --- | --- | --- | --- |
| Stripe | 原 Checkout Session | 原账号 SDK 查询及验证后的回调 | SDK expire 后重查 unpaid + expired，才进入可靠终态 |
| 易支付 | 原签名 POST 表单、原订单号 | 原商户认证成功查询及验签回调 | 当前公开文档及已登录的会员文档均未提供商户关单及可靠到期状态，暂不启用取消／自动关闭；仍需服务商确认能力 |
| Creem | 原 checkout | 验证后的付款回调；缺可信付款时间或完整定价证据时保存回执并挂账复核 | 未证实的关单／到期能力关闭 |
| Waffo | 原订单／支付入口 | 验证后的付款回调 | 未证实的关单／到期能力关闭 |
| Waffo Pancake | 原 session／入口，独立检查 token 期限 | 验证后的付款回调 | 仅有返回期限不足以证明渠道已关闭，未证实能力关闭 |

2026-10-23 只读核对 [公开文档](https://zpayz.cn/doc.html)，并在用户已登录的 Chrome 标签页完整核对 [会员开发文档](https://member.z-pay.cn/member/doc.html)：已登录文档包含页面跳转支付 `submit.php`、API 支付 `mapi.php`、单笔查询 `api.php?act=order`、退款 `api.php?act=refund`、支付通知及 MD5 签名规则。单笔查询只定义 `status=1` 已付和 `status=0` 未付；创建参数没有有效期，未提供取消、商户关单或权威过期状态。退款接口是已支付资金的退款，不能作为未支付订单的关单接口。此结论限定于已核对文档，不推断服务商绝对没有其他私有能力。

原支付接口说明可以直接进入收银台，API 支付可返回 `payurl`／`qrcode`／`img`，但没有明确保证同一 `out_trade_no` 重复提交的行为或旧收银台的失效条件；恢复原入口仍需真实渠道验收。页面中的用户取消回跳不能作为关单证据。该缺口不影响保存原入口和付款核实；不能据账龄或查询未付伪造“已自动取消”。剩余应向服务商确认商户关单接口、创建有效期、过期后的原链接可付性及重复提交原订单号的语义。

会员文档还要求支付通知在 5 秒内返回纯字符串 `success`，否则会按 `0/15/15/30/180/1800/1800/1800/1800/3600` 秒策略重试，且不保证最终通知成功。因此可靠性依赖已验签付款事实持久保存及独立补偿，不能仅依赖回调持续重试。没有调用真实查询、退款或支付接口。

据此修正了本次易支付 v1 回调：去掉响应前最长 10 秒的商户 HTTP 等待；验签和金额检查后，先持久保存收款事实，缺可信付款时间则挂账并立即安排现有后台核实。回调不再发外部请求，后台使用订单保存的原商户凭据查询并补齐付款时间后才入账；不新增每回调 goroutine。存储失败返回 `fail`，重复通知不重复收据或额度。此变更消除了远程查询导致超出 5 秒的路径，尚未以真实环境证明所有数据库负载下均在 5 秒内完成。

没有使用真实商户资金进行测试；本地模拟协议测试不证明线上渠道配置、回调可达性或实际关单能力。

可信回调缺少付款时间或定价证据时，保留收据及处理责任后挂账，不以通知接收时间代替实际付款时间。能明确当前商户和环境、但本地找不到订单的已验签收款进入独立 unmatched 记录；旧商户密钥无法解密、签名未通过或商户身份不明确时拒绝确认，要求服务商重试。并非每个身份／币种／session 不匹配分支都已落为独立收据，部分分支仍拒绝并让服务商重试；完整异常队列和真实渠道证据仍属于上线验收缺口。

## 安装和兼容边界

新增扩展为独立 v18，旧 v17 和既有迁移校验和保持不变。新接口要求完整物理 schema 及 canonical 迁移记录；手工添加几张表不能启用资金流程。启动不负责安装钱包扩展。

本版本在未安装 v18 时暂停所有在线充值新建，五个旧创建入口同样返回 `503 WALLET_STATE_UNAVAILABLE`；兑换码和旧订单回调仍保留各自流程。启动及旧数据读写兼容不表示旧购入口继续可用，不能作为无需安装扩展的升级直接发布。安装后，旧客户端缺少幂等键返回 `409 CLIENT_UPGRADE_REQUIRED`。CLI target18 当前仅支持只读 precheck／dry-run，通用 apply 拒绝安装；没有交付可直接操作生产的 v18 控制器。

在受控安装扩展之前，需要另行确认实际数据库版本、摘要、精确运行镜像与回退镜像，以及安装控制器的支持情况。当前只交付应用内扩展源码和本地验证，不能据此声称生产已经支持 v18 或可以直接迁移。

持久结账数据使用带作用域 AAD 的 AES-GCM。必须设置至少 32 字符的稳定 `WALLET_PAYMENT_SECRET`，或明确设置的 `CRYPTO_SECRET`；不会用随机会话默认值保存支付凭据。轮换时，旧密钥保存在有界 JSON 数组 `WALLET_PAYMENT_PREVIOUS_SECRETS`，直到相关订单和收据处理完毕。原渠道凭据与原商户环境一起加密保存，不能以新配置代替。Creem 需要明确的 `WALLET_CREEM_ACCOUNT_REF`。密钥值和完整支付入口不进入列表、普通详情、日志或浏览器存储。

## 验证记录

完整前端类型检查已获得用户明确授权；其余检查保持在受影响包和文件范围。没有运行全仓测试、全仓 lint、生产构建或真实渠道扣款。

### 后端定向检查

以下命令均在应用仓库根目录执行，结果为 PASS。Go 检查均设置了至多 240 秒的外部超时；根代理的新命令另外带 `-timeout=120s`。

```sh
go test ./middleware ./router -run '^TestWalletPayment|^TestWalletPaymentRoutes' -count=1 -timeout=120s
# middleware PASS 0.071s；router 初次因并行开发中的 imports 失败，修复后仅重跑 router：
go test ./router -run '^TestWalletPaymentRoutes' -count=1 -timeout=120s
# PASS 0.071s

go test ./model -run '^TestWallet|^TestFinalizeTopUp|^TestValidateMainSchemaPrerequisites' -count=1
# PASS 0.784s
go test ./model -run '^TestWalletPaidReceiptSurvivesAccountingFailureAndCanRetry$' -count=1
# PASS 0.063s（回执异常负责人调整后）
go test ./model -run '^TestWalletHistoryRunsRotateByLastProgress$' -count=1
# PASS 0.061s
go test ./model -run '^TestWalletHistoryPaidAmount' -count=1 -timeout=120s
# PASS 0.068s

go test ./service -run '^TestWalletHistory' -count=1
# PASS 0.047s
go test ./service -run '^TestWalletHistoryLostLeaseCannotAdvanceOrStartAnotherQuery$' -count=1
# PASS 0.045s（失租暂停边界调整后）
go test ./service -run '^TestWalletStripeGateway|^TestWalletStripeQuery' -count=1 -timeout=120s
# PASS 0.071s
go test ./service -run '^TestWalletReceiptRecovery' -count=1 -timeout=120s
# PASS 0.887s
go test ./service -run '^TestWalletStripeClosure' -count=1 -timeout=120s
# PASS 3.318s
go test ./service -run '^TestWalletPaymentSchemaGate|^TestWalletReceiptRecoveryAfterCrashBeforeCredit$|^TestWalletHistory' -count=1
# PASS 1.802s，运行时门禁缓存和漂移检查调整后
go test ./controller -run '^TestWallet|^TestLegacyWallet|^TestPayment' -count=1
# PASS 0.074s
KKAI_MEMBERSHIP_ENABLED=true go test ./controller ./service -run 'TestWallet|TestStripeWebhook|TestCreemWebhook|TestWaffoWebhook|TestWaffoPancakeWebhook|TestEpay|TestCreemTestMode|TestMembershipStripeWebhook|TestInvoiceEpayMerchantQuery' -count=1
# 最终 PASS：controller 0.944s、service 5.941s；原商户验签、会员回调、未付查询和最新相关实现
go test ./controller -run TestWalletEpayUnknownOrderCallback -count=1
# PASS 0.850s；真实回调入口验证未知订单仅保存认证付款事实，错误签名和缺失 canonical 记录不确认付款
go test ./controller ./service -run 'TestWalletEpayUnknownOrderCallback|TestCreemTestMode|TestWalletStripeQuery|TestResolveWaffoPancakeTradeNo' -count=1
# PASS：controller 0.847s、service 0.071s；最终渠道归属与非成功响应收紧后
go test ./controller -run 'TestWalletEpayPaidCallback' -count=1
# PASS 0.950s；按已登录文档调整回调后：重复通知零 HTTP、不提前入账、后台补齐付款时间后入账、存储失败不 ACK

go test ./pkg/kkaimigrate -run '^TestWalletPayment|^TestInvoiceExpandPreservesCanonicalHistoryAndRequiresReceipt$|^TestContractForDialectRequiresTenantSchema$|^TestMigrationCatalog' -count=1
# PASS 1.747s
go test ./cmd/kkai-migrate -run '^TestWalletPayment|^TestDescribeContractJSONRequiresTenantSchema$|^TestInvoiceProof|^TestInvoiceInspection|^TestRequestTicketProof|^TestRequestTicketInspection|^TestResellerPrefixProof|^TestResellerPrefixInspection' -count=1
# PASS 3.496s
git diff --check
# PASS
```

实际覆盖的资金边界包括单流程幂等、精确确认终态、租户隔离、重复／冲突／迟到回执、入账失败保留证据及独立恢复、返利 outbox、取消与付款竞争、关单响应后重查、历史暂停和失租检查点。收据恢复与 Stripe 关单集成测试通过公开维护 API 构建 SQLite canonical v18，不能以手工建表绕过门禁。

### 前端定向检查

从 `web/default` 执行：

```sh
bun run test -- src/features/wallet/payment-flow-api.test.ts src/features/wallet/lib/payment-flow.test.ts src/features/wallet/hooks/use-wallet-payment-flow.test.tsx src/features/wallet/hooks/use-billing-history.test.tsx src/features/wallet/components/current-topup-order.test.tsx src/features/wallet/tenant-wallet.test.tsx src/features/wallet/wallet-payment-i18n.test.ts src/features/system-info/__tests__/wallet-topup-reconcile-run.test.tsx src/features/system-info/__tests__/system-tasks-panel.test.tsx
# PASS：9 文件／49 案例，4.55s
bun run test -- src/features/wallet/payment-flow-api.test.ts src/features/wallet/lib/payment-flow.test.ts src/features/wallet/components/current-topup-order.test.tsx src/features/wallet/tenant-wallet.test.tsx src/features/wallet/wallet-payment-i18n.test.ts src/features/system-info/__tests__/wallet-topup-reconcile-run.test.tsx
# PASS：6 文件／36 案例，4.33s，凭证和金额格式修改后仅复验改动范围
bun run test -- src/features/wallet/tenant-wallet.test.tsx
# PASS：5 案例，2.20s
bun run test -- src/features/wallet/wallet-payment-i18n.test.ts src/features/system-info/__tests__/wallet-topup-reconcile-run.test.tsx
# PASS：10 案例，1.25s
bun run typecheck
# PASS：3.82s，报价防抖最终改动后
bun run i18n:sync
# PASS；七语言每语新增124键，所有原有键值保持
```

报价防抖最终改动后再次执行 `bun run test -- src/features/wallet/tenant-wallet.test.tsx`，6 案例 PASS（1.19s）；`./node_modules/.bin/oxlint -c .oxlintrc.json src/features/wallet/index.tsx src/features/wallet/tenant-wallet.test.tsx` 通过。以上增量检查合计覆盖 9 个相关文件中的 53 个独立测试案例；没有把重复运行的数量累加。27 个本次改动的 TS／TSX 文件定向 lint，0 errors／warnings；最后路由变更后单独执行 `./node_modules/.bin/oxlint -c .oxlintrc.json src/routes/_authenticated/wallet/index.tsx` 通过。格式化保留原版权头。

27 文件的精确 lint 命令：

```sh
./node_modules/.bin/oxlint -c .oxlintrc.json src/features/system-info/__tests__/wallet-topup-reconcile-run.test.tsx src/features/system-info/components/system-tasks-panel.tsx src/features/system-info/components/wallet-topup-reconcile-run.tsx src/features/system-info/wallet-topup-reconcile-api.ts src/features/wallet/components/creem-products-section.tsx src/features/wallet/components/current-topup-order.test.tsx src/features/wallet/components/current-topup-order.tsx src/features/wallet/components/dialogs/billing-history-dialog.tsx src/features/wallet/components/payment-amounts.tsx src/features/wallet/components/payment-evidence.tsx src/features/wallet/components/recharge-form-card.tsx src/features/wallet/hooks/use-billing-history.test.tsx src/features/wallet/hooks/use-billing-history.ts src/features/wallet/hooks/use-wallet-payment-flow.test.tsx src/features/wallet/hooks/use-wallet-payment-flow.ts src/features/wallet/index.tsx src/features/wallet/lib/payment-flow.test.ts src/features/wallet/lib/payment-flow.ts src/features/wallet/payment-flow-api.test.ts src/features/wallet/payment-flow-api.ts src/features/wallet/payment-flow-fixtures.ts src/features/wallet/payment-flow-types.ts src/features/wallet/tenant-wallet.test.tsx src/features/wallet/wallet-payment-i18n.test.ts src/hooks/use-sidebar-data.ts src/i18n/static-keys.ts src/routes/_authenticated/wallet/index.tsx
```

### 实际组件视觉检查

使用真实 `CurrentTopupOrder`、项目样式和国际化，配合固定订单 fixture，在本地临时 Rsbuild 页面中检查。1440px 桌面、320px 窄屏、深色和法语长文本均无横向溢出；“返回充值”可聚焦并由 Enter 触发确认回调。检查中发现并修复 `Intl.NumberFormat('zhCN')` 导致白屏的问题，复用现有语言映射且保留 BigInt 精度。临时页面和服务已清理。

截图位于本会话可视化目录：

- [桌面超时结果](/Users/tokk/.codex/visualizations/2026/10/23/01a16810-aff1-7911-8ea3-630944f7ae7d/wallet-implementation-desktop.png)
- [手机超时结果](/Users/tokk/.codex/visualizations/2026/10/23/01a16810-aff1-7911-8ea3-630944f7ae7d/wallet-implementation-mobile.png)
- [深色超时结果](/Users/tokk/.codex/visualizations/2026/10/23/01a16810-aff1-7911-8ea3-630944f7ae7d/wallet-implementation-dark.png)
- [待支付且不支持取消](/Users/tokk/.codex/visualizations/2026/10/23/01a16810-aff1-7911-8ea3-630944f7ae7d/wallet-implementation-pending.png)
- [法语窄屏](/Users/tokk/.codex/visualizations/2026/10/23/01a16810-aff1-7911-8ea3-630944f7ae7d/wallet-implementation-fr-mobile.png)

这些是组件视觉证据；未将 fixture 中的关闭结果描述为易支付实测能力，也未把按钮回调模拟视为完整后端 E2E。Safari 外部表单真实提交仍需验收。

仍需要真实渠道验收、MySQL／PostgreSQL 实库验证及生产迁移／回退验收。全部 A01—A32 的浏览器、渠道与故障证据收齐前，不将本地单元测试通过描述为生产验收完成。

## 本站超时与废单修订实施记录

- 服务端统一创建后 30 分钟作为本站期限，取更早渠道截止；UI、resume、end、cancel、人工核实及旧补单入口均实时校验，不能依赖客户端或等待扫表才失效。创建时间未知保持 unavailable，不编造具体超时时刻。
- 新增独立 LocalEndedAt／LocalEndReason，展示 ended／timed_out；不改变渠道关闭证据。本人当前或历史有效订单可结束，结果保留到精确 acknowledge；历史结束不替换当前另一单。旧结果确认不能清掉后来创建的流程。
- 老 pending 仍保留全龄可查，分组为 closed／已结束、显示本站超时，排除其他未完成计数和恢复候选。旧历史 response 只读投影 expired 使 classic 的旧补单按钮消失，不改数据库原收款事实，不影响会员／订阅镜像。
- 本人／管理员人工核实、新旧补单 API 及公共 ManualCompleteTopUp 拒绝废单；人工历史批次排除废单并在执行时复核。后台可信回调、收据恢复和有限自动查单继续运行；已结束且创建未知时不再重放创建。达到有限重试上限停止主动查询，之后可信回调仍可重新安排核实。
- 已到账及可信已收款的 credit_review 优先于本地结束标记和账龄，不能把已收款显示成未付款废单。旧 A 晚到账只更新 A、不清当前 B；重复通知只入账一次，A／B 都实际付款则各走原账务事务。
- 后续未付／未知或可靠关闭查询不能覆盖已有 paid、credit_review 或已落库可信回执；保留原收款事实、金额和核实责任。仅因回执匹配失败而待核实的订单仍保持原付款标记，不伪造已付款；原可信回执补证和结算仍可重试。
- 后台核实任务按最终持久化的订单状态分类，模型保留的到账核实不会因请求中的未付／关闭结果被误计为 closed 或 retry_failed；账务及渠道事实不受任务计数修订影响。
- 未安装的 v18 扩展内新增两个可空字段；没有改写旧迁移校验和或运行真实迁移。此前安装／发布边界继续有效。

### 本次新增检查

仓库根目录执行：

```sh
go test ./model -run '^TestWallet|^TestManualCompleteTopUp|^TestInvoiceTopUpManualCompletion|^TestFinalizeTopUp|^TestValidateMainSchemaPrerequisites' -count=1
# PASS 1.078s
go test ./pkg/kkaimigrate -run '^TestWalletPayment|^TestInvoiceExpandPreservesCanonicalHistoryAndRequiresReceipt$|^TestContractForDialectRequiresTenantSchema$|^TestMigrationCatalog' -count=1
# PASS 1.661s
go test ./cmd/kkai-migrate -run '^TestWalletPayment|^TestDescribeContractJSONRequiresTenantSchema$' -count=1
# PASS 0.933s
go test ./controller ./service -run 'TestWalletTopUpDTO|TestWalletOrderActions|TestWalletLocalEnd|TestWalletExpiredAttempts|TestWalletEndReturns|TestWalletEnded|TestWalletStripeClosure|TestWalletHistory' -count=1
# PASS：controller 1.980s、service 5.660s
go test ./controller -run 'TestWalletLocalEnd|TestWalletEndReturns|TestWalletLegacyHistory' -count=1
# PASS 1.979s（最后历史镜像隔离及历史 end 返回 current 契约新增后）
go test ./controller ./service -run 'TestWalletExpiredAttempts|TestWalletEnded' -count=1
# PASS：controller 0.983s、service 1.728s（结束／到期后即使进入到账核实，也不重放创建未知的支付会话）
go test ./middleware ./router -run '^TestWalletPayment' -count=1 -timeout=120s
# PASS：middleware 0.074s、router 0.072s
go test ./model -run '^TestWalletNegativeChecksCannotErasePaidFactsOrReceiptReview$|^TestWalletIncompletePaidProofIsDurableAndCanBeEnriched$|^TestWalletPaidReceiptSurvivesAccountingFailureAndCanRetry$|^TestWalletFlowRetainsResultAndOriginalIdempotentOrder$' -count=1
# PASS 0.204s（最后收款状态防降级、已持久化回执和补证／入账回归）
go test ./service -run '^TestWalletReconcileReportsPersistedReceiptReview$|^TestWalletStripeClosureRequiresAuthenticatedRecheckAndPaymentWins$' -count=1
# PASS 6.465s（核实任务按持久状态统计、收据已提交未更新订单的崩溃窗口及原关单竞争）
```

从 web/default 执行：

```sh
bun run test -- src/features/wallet/payment-flow-api.test.ts src/features/wallet/lib/payment-flow.test.ts src/features/wallet/hooks/use-wallet-payment-flow.test.tsx src/features/wallet/components/current-topup-order.test.tsx src/features/wallet/components/dialogs/billing-history-dialog.test.tsx src/features/wallet/tenant-wallet.test.tsx src/features/wallet/wallet-payment-i18n.test.ts
# 首轮 6 文件／43 案例通过；新 Hook 用例断言未等待通知，修正测试后只重跑 Hook：
bun run test -- src/features/wallet/hooks/use-wallet-payment-flow.test.tsx
# PASS 7 案例，3.11s；上述 7 文件当时的 50 案例均通过，不把首轮整体写成通过
bun run test -- src/features/wallet/components/current-topup-order.test.tsx src/features/wallet/components/dialogs/billing-history-dialog.test.tsx
# PASS 12 案例，5.18s（文案位置调整后）
bun run test -- src/features/wallet/wallet-payment-i18n.test.ts
# PASS 7 案例，1.94s
bun run test -- src/features/wallet/hooks/use-wallet-payment-flow.test.tsx src/features/wallet/components/current-topup-order.test.tsx src/features/wallet/components/dialogs/billing-history-dialog.test.tsx
# PASS 22 案例，5.42s（最终资金核实优先及未知日期只读边界调整后）
bun run typecheck
# PASS 6.22s（用户已授权的完整前端类型检查）
bun run i18n:sync
# PASS；本次增补 14 键 × 7 语言，临时写入脚本已删除
```

前述 27 文件 lint 命令另加 `src/features/wallet/components/dialogs/billing-history-dialog.test.tsx` 的 28 文件检查通过；最后资金优先调整后对对应六文件再次 scoped lint 通过。未运行全量测试、全仓 lint 或生产构建；真实渠道及 MySQL／PostgreSQL 实库验证缺口不变。

独立只读终审核对本站到期的状态、分组、计数、新旧手工入口、批次执行前重读、结束后创建未知不重放、精确流程确认及原订单晚到款结算，未发现新增 P0／P1／P2 问题。终审提出的已知收款状态防降级问题已由资金安全角色修复，并通过上述最终定向回归；终审没有执行真实订单或生产操作。

PRD、UI、前后端开发、总开发计划及验收正文已统一新规则，旧原型证据明确标为历史参考。根代理最终执行 `git diff --check` 通过；内联 `python3` 检查 9 份钱包文档、75 个本地链接、PRD／验收各自 A01—A32 完整有序、行尾与代码围栏及临时 QA 文件清理，均通过。文档调整后未重复运行输入未变的业务测试。

### 本次实际组件检查

使用真实组件、项目样式和 i18n，固定 fixture 验证本站超时／主动结束结果及结束确认；默认桌面、320px 窄屏和法语深色窄屏均无横向溢出。“返回充值”由键盘 Enter 触发回调。该检查是组件验证，不能替代真实后端 E2E 或渠道关单实测。临时三个 QA 文件和服务已清理、浏览器尺寸已恢复。

- [本站超时桌面结果](/Users/tokk/.codex/visualizations/2026/10/23/01a16810-aff1-7911-8ea3-630944f7ae7d/wallet-local-timeout-desktop.png)
- [本站超时手机结果](/Users/tokk/.codex/visualizations/2026/10/23/01a16810-aff1-7911-8ea3-630944f7ae7d/wallet-local-timeout-mobile.png)
- [结束本次充值确认](/Users/tokk/.codex/visualizations/2026/10/23/01a16810-aff1-7911-8ea3-630944f7ae7d/wallet-local-end-confirm.png)
- [本站主动结束结果](/Users/tokk/.codex/visualizations/2026/10/23/01a16810-aff1-7911-8ea3-630944f7ae7d/wallet-local-ended-desktop.png)
- [法语深色窄屏结果](/Users/tokk/.codex/visualizations/2026/10/23/01a16810-aff1-7911-8ea3-630944f7ae7d/wallet-local-timeout-fr-dark-mobile.png)

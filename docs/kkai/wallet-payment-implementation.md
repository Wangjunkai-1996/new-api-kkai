# 钱包充值生命周期实施记录

日期：2026-10-23；发布准备增量更新：2026-10-24。本文件记录实际实现、验证和未完成的外部依赖；产品行为与接口仍分别以 [PRD](./wallet-payment-lifecycle-prd.md)、[UI 规格](./wallet-payment-ui-spec.md) 和 [后端规格](./wallet-payment-backend-dev-spec.md) 为准。

最新用户确认已实施：[本站超时与废单修订规范](./wallet-payment-local-timeout-spec.md)。本站 30 分钟超时和主动结束允许结果确认后返回充值；这独立于渠道关单，取代此前“未确认渠道关闭则一直锁定当前流程”的规则。

## 本地交付范围

2026-10-23 本地实现阶段在现有工作目录实施，未创建分支或 worktree，也未执行生产构建、部署、生产数据库迁移、历史订单处理、代码提交或推送。2026-10-24 用户已明确授权安装、提交推送和完整蓝绿上线；下方发布准备记录区分已验证事实与待执行步骤，不能将此前本地阶段结论当作本次交付最终状态。

- 钱包先恢复服务端流程；空闲、创建、当前订单、处理中和结果互斥。订单阶段隐藏独立余额卡、充值、兑换、Creem 和订阅购买模块。
- 支付在新标签页打开；原订单及原结账入口持久保存。关闭标签页不会取消订单，也不会清除当前流程。
- 按服务端事实显示继续支付、结束本次充值、渠道取消或核实；本站结束／超时和可靠渠道结果均保留，点击“返回充值”且确认对应流程后才恢复表单。本站废单无继续支付、取消、人工核实或补单入口。
- 五个旧支付创建入口统一使用幂等和当前流程门禁。`POST /api/user/topup` 继续是兑换码入口。生命周期安装后，缺少 `Idempotency-Key` 的旧支付客户端返回升级要求；classic 未移植完整钱包 UI。
- 本人和管理员历史支持同一范围内的六组状态计数、全龄未完成入口和不受 30 天限制的本人精确查号。历史详情不替换当前流程。
- 历史核实采用只读报告、冻结范围和订单上界、独立授权引用、版本控制、租约及检查点；暂停和续跑只允许平台超级管理员交互式会话。开发和部署授权不包含实际执行历史处理。
- 运行时 canonical schema 验证共享并发合并；2026-10-24 已对齐 invoice 的取消隔离：完整检查使用独立 15 秒预算，成功缓存 30 秒，失败只退避 5 秒，关闭一个浏览器请求不会污染其他用户的检查结果。更换 DB 对象立即失效，到期重新验证漂移；只记录固定错误类别和耗时，不记录 driver 细节。维护检查保持即时验证，legacy 最终插单 canonical17 校验与模型物理列检查不缓存。报价输入防抖 300ms，输入变化立即隐藏旧报价并禁止用旧金额确认。

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

2026-10-24 兼容增量已取代最初“未安装 v18 就暂停全部新充值”的实现：只有 canonical17 且完全没有 v18 对象时，五个旧支付入口继续原流程；canonical18 使用新流程。未知记录、残缺 DDL、完整记录与实际 schema 不符或数据库错误均返回 `503 WALLET_STATE_UNAVAILABLE`，不能回退到旧创建。`GET /api/user/topup/info` 的 `wallet_lifecycle_ready=false` 仅表示前述可接受的 canonical17 状态；完整 canonical18 返回 true。前端据此选择旧钱包或新生命周期钱包，错误则保持不可用／重试状态。生命周期安装后，缺少幂等键的旧支付客户端仍返回 `409 CLIENT_UPGRADE_REQUIRED`，不增加不可靠的服务端幂等适配。

旧创建在 provider 请求完成后的最终插单处重新验证 canonical17 和无 v18 对象。短数据库事务先读取 `top_ups` 持有表访问锁，再校验并插单；PostgreSQL 的 ALTER 必须等待事务结束，或先完成后令插单复核拒绝。事务不跨外部 provider 网络等待，且保留原租户与精确 CNY 现金校验。新回调、收据和 schema18 业务不因恢复旧分支而回退。

钱包迁移使用独立 `wallet-payment-migration-v1` 生产协议；通用 target18 apply 仍拒绝安装。实际安装能力及 A/B 发布顺序以下节和已确认基础设施 checkout 的 runbook 30 为准，不能从本地源码推断已经安装。

在受控安装扩展之前，必须确认实际数据库版本、摘要、精确运行镜像与回退镜像，以及安装控制器的支持情况。本地应用／控制器实现和隔离验证不能证明生产已经支持 v18 或可以直接迁移；安装、实际运行身份和完整发布证据分别记录。

持久结账数据使用带作用域 AAD 的 AES-GCM。必须设置至少 32 字符的稳定 `WALLET_PAYMENT_SECRET`，或明确设置的 `CRYPTO_SECRET`；不会用随机会话默认值保存支付凭据。轮换时，旧密钥保存在有界 JSON 数组 `WALLET_PAYMENT_PREVIOUS_SECRETS`，直到相关订单和收据处理完毕。原渠道凭据与原商户环境一起加密保存，不能以新配置代替。Creem 需要明确的 `WALLET_CREEM_ACCOUNT_REF`。密钥值和完整支付入口不进入列表、普通详情、日志或浏览器存储。

## 2026-10-24 发布准备与 A/B 顺序

用户的“安装、提交推送代码、蓝绿上线、不要停”已授权正常交付从准备到 promotion 的持续推进；不再重复请求同一授权。控制器行为变更仍必须完成既有强制验证，并先建立独占空闲控制面安装窗口：没有 candidate、rollout、deploy、架构维护或待恢复事务。安装器替换共享 helper 和 Python 环境，不能用随后 schema 命令才获取的锁代替这个窗口。infra `make policy` 已由执行代理确认 exit 0；正在协调另一轮已存在的应用发布完整结束后释放安装窗口。未据此声明控制器已安装或生产迁移已完成。

两个必要的应用版本按下表交付，每个精确提交只构建一个不可变版本，分别完成标准候选验收和 `10% → 50% → 100%`、每档至少 120 秒的灰度及发布后门禁：

| 版本 | 包含的实际能力 | schema 迁移权限 |
| --- | --- | --- |
| A | 完整 v17/v18 钱包业务、readiness、新旧页面选择、legacy 最终插单事务锁、新版回调／持久收据 | 专用 CLI 仅 `plan`／`status`；显式拒绝 `apply` |
| B | 保留 A 全部兼容与资金安全行为 | 新增固定 root proof 验证、锁内复核及原子 `apply`，只授权冻结的 v17→v18 钱包计划 |

B 是真实可执行迁移能力变更，不能以空提交、纯 metadata 或重建未变源码取代。两版均声明并实际支持 max18；不删除已知必要的插单锁来制造第二版。A 不执行 DDL，但可在 additive18 上运行完整新 API、资金回调和收据，即使后续正常回滚重建 A writer 也无需重新迁移。

执行顺序是：强制验证及隔离演练通过 → 独占空闲窗口内安装精确干净 infra 提交并 self-test → 应用 pin 对齐实际安装 SHA → A 完整发布 → B 完整发布 → 验证实际 active／rollback／writer 进程均支持 exact canonical18 → 新鲜可恢复备份与只读 plan → active B 镜像执行独立 apply → schema18、角色关闭、业务和回滚验收。前端实际 format 从现场 manifest 取得，format1 精确配对，format2 遵守已安装的独立兼容协议。不能以 console contract 文档自身的 `format_version` 推断前端 artifact 格式。

基础设施 runbook 30 的最终接口如下，以下是待执行操作说明，不是完成证据：

```sh
# 精确 root-owned infra checkout，按既有手工安装流程传输及保留 Git 对象。
sudo /EXACT/ROOT_OWNED/INFRA_CHECKOUT/scripts/production/install-newapi-manual-deployer.sh \
  --source-dir /EXACT/ROOT_OWNED/INFRA_CHECKOUT
sudo /usr/local/sbin/kkai-newapi-manual-deploy self-test --installation-only
sudo /usr/local/sbin/kkai-newapi-schema-migrate wallet-payment --help

# A/B 两次完整发布通过后，走正常私有 sys1 SSH 路径调用。
sudo /usr/local/sbin/kkai-newapi-schema-migrate wallet-payment plan
sudo /usr/local/sbin/kkai-newapi-schema-migrate wallet-payment apply \
  --confirm-plan-sha256 EXACT_FRESH_PLAN_CONFIRMATION
sudo /usr/local/sbin/kkai-newapi-schema-migrate wallet-payment status
```

`EXACT_FRESH_PLAN_CONFIRMATION` 使用最新 plan 返回的 `confirmation_sha256`，不能使用演练或旧版本值。安装后及 apply 后通过已确认 infra checkout 的 `make newapi-status` 取得实际健康与 rollback readiness；该命令不能单独证明 schema 兼容或 writer 拓扑。apply 由 active B image 执行，proof 绑定数据库、frozen plan、安装 SHA 和三个实际运行进程，root 0400／只读挂载且有效期至多 120 秒；Go 在 schema 锁内再验。PostgreSQL DDL、索引和 canonical18 receipt 同事务提交，失败不自动重试，旧迁移协议不扩权。

完成判据保留标准即时／延迟 status、公开精确版本、等长脱敏 relay 错误窗口人工归因和兼容 rollback pair。schema 操作另外要求 full18 shape／canonical receipt、迁移角色 NOLOGIN／零会话及进程／routing 未漂移。新鲜 readiness=true 后验收新钱包，不为激活 UI 再造第三个后端版本。真实渠道支付、关闭 tab 后恢复、渠道能力允许的取消、超时保留结果与“返回充值”、老废单无补单入口需要分别记录结果；历史业务数据处理仍不属于 DDL。

### 本轮兼容与协议验证

以下已执行的最小检查通过，未运行全仓测试：

```sh
go test ./controller -run 'TestWallet(Legacy|Compatibility)' -count=1
# PASS 5.856s：canonical17 五入口、真实 Epay 签名表单及 CNY snapshot、最终插单复核、异常 schema、18旧客户端与 readiness。
go test ./controller -run '^TestWalletCompatibilityUnavailableDatabaseCannotSelectLegacy$' -count=1
# PASS 0.070s：数据库不可用不选择旧钱包。
go test ./service ./controller -run 'TestWallet(PaymentSchema|Legacy|Compatibility)' -count=1
# PASS service 1.071s／controller 5.001s：取消前拒绝、检查中取消不污染共享结果、独立完整检查预算、失败短退避、18完整gate复用与17 fresh分类、插单最终复核。
TEST_RESELLER_POSTGRES_DSN='postgres://postgres@127.0.0.1:58104/kkai_test_online_acceptance?sslmode=disable' \
  go test ./model -run '^TestTopUpLegacyPostgresInsertSerializesWithWalletDDL$' -count=1
# PASS 0.569s：受限本地 PostgreSQL18.4 独立 schema／双 session，旧插单事务阻止真实 ALTER，提交后 ALTER 成功，后续旧插单拒绝。
```

隔离 PostgreSQL 已验证迁移 DDL 和 receipt 原子提交／失败回滚。真实 root controller→proof→Docker→Go→PostgreSQL 演练已验证过期、错误 target digest／数据库／runtime contract、非 0400、非 root owner、可写挂载、缺失 proof、non-UID0 和旧 plan confirmation 均被拒绝；各次拒绝后数据库仍为17、无 v18 列，迁移角色 NOLOGIN／零会话。成功 apply 和只读 replay 已通过，最终 schema18 canonical receipt 完整，legacy pending 未被改写，迁移角色保持 NOLOGIN／零会话。证据位于本机 `/tmp/kkai-wallet-root-rehearsal-YBOVmlrj`，其中 `application-source.sha256` 绑定被演练的 Go 文件；最终 B 文件恢复一致时可以复用，后续若改动该协议逻辑则验证受影响范围。临时容器、独立数据库和角色已清理，未构建正式发布镜像。

演练使用 native ARM64 CLI 和真实 `/new-api` fixture 进程身份；环境／HTTP／topology 由 fixture adapter 提供，只证明相应协议边界，不是完整生产验收。A/B 均携带 `/kkai-migrate`，A 的只读 `status` 可用于租户业务 readiness；external Docker target 无条件编译并复制该二进制。前端检查结果及两次正式发布坐标待执行代理追加。

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

# 订阅会员实现验证记录

## 当前规则变更与定向验证（2026-10-22）

后台发布折扣后，所有有效新老会员的后续新请求统一采用当前档位最新发布值；不再按
购买版本或新增组首次发布版本锁定系数。已预扣请求仍按完整原快照结算；订单实付金额、
额度、有效期和历史账本不重写。

本次在 `/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai` 完成本地代码修改和
定向验证；未提交、推送、迁移或部署，未连接生产。验证覆盖三档已有/新购会员折扣
提高、降低及恢复为 1，新增组后续发布、待付订单付款后使用最新折扣、已预扣请求跨发布
重放和结算、过期报价拒绝，以及目录删除档位后的迟到收款人工开通保护。

后端命令：

```sh
KKAI_MEMBERSHIP_ENABLED=true go test -json ./model ./service ./controller -run Membership -count=1 -timeout=120s
KKAI_MEMBERSHIP_ENABLED=true go test ./controller -run '^TestMembershipMidjourneyBillingCheckFailsClosed$' -count=1
```

组合执行中 model 53、service 72 个测试/子测试通过；controller 63 个通过，
`TestMembershipMidjourneyBillingCheckFailsClosed` 首次失败。新共用夹具先发布目录，导致
schema 检查已有缓存，原删表注入不再触发首次检查；仅将该测试改用同连接池的新 GORM
handle，定向重跑通过，生产代码未为此更改。未重复运行已通过且输入未改变的范围。
原始结果：`/tmp/kkai-membership-final-results.jsonl`。

MySQL 5.7 / PostgreSQL 9.6 使用本机已有镜像、独立 tmpfs 容器及随机隔离数据库，实际执行：

```sh
KKAI_MEMBERSHIP_ENABLED=true KKAI_IMAGE_ASYNC_MYSQL_TEST_DSN='root@tcp(127.0.0.1:49948)/?parseTime=true' KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN='postgres://postgres@127.0.0.1:49947/postgres?sslmode=disable' go test -json ./pkg/kkaimigrate -run '^TestMembershipRealDatabaseTransactions$' -count=1 -timeout=120s
KKAI_MEMBERSHIP_ENABLED=true KKAI_IMAGE_ASYNC_MYSQL_TEST_DSN='root@tcp(127.0.0.1:49948)/?parseTime=true' go test -json ./pkg/kkaimigrate -run '^TestMembershipRealDatabaseTransactions/mysql$' -count=1 -timeout=120s
```

PostgreSQL 首次通过；MySQL 首次在基础应用模型 AutoMigrate 阶段因驱动将 `uniqueIndex`
字符串映射成 LONGTEXT，无法创建 username 唯一索引。仅对本夹具设置 MySQL
`DefaultStringSize=191` 后，MySQL 定向重跑通过；会员表仍完全使用正式迁移 DDL。
两种数据库均实际验证并发付款幂等、并发预扣、账本失败回滚、旧账户返还和钱包接续，
并新增“事务先建立旧读视图，再发布新版本，旧报价预扣必须拒绝”的确定性回归。
日志为 `/tmp/kkai-membership-real-db-results.jsonl`、
`/tmp/kkai-membership-mysql-final-results.jsonl`；两个任务容器均已删除。

前端 3 文件、40 项测试及会员范围 TypeScript、lint、格式、i18n 检查通过，精确命令见
[本次前端记录](subscription-membership-frontend-validation.md)。所有本次 Go 文件
`gofmt -l` 无输出，变更范围 `git diff --check` 通过；两个新增测试文件另查空白通过。
全仓测试、全量 lint/类型检查、生产构建均跳过。尚未执行本次真实支付商、收费模型上游、
浏览器端到端或生产发布验证。

以下 2026-10-18 结果保留为历史；其中“折扣发布与冻结”按当时规则验证，不替代上述新规则验证。

## 历史验证记录（2026-10-18）

日期：2026-10-18。仅代表本地隔离工作区验证，不代表生产可直接启用。

- 工作区：`/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai`
- 分支：`codex/subscription-membership`；会员原起点 `7be0aa7be15d209b65def2b626dcdaf20873e7e0`，
  当前基线 `71283181467d84c113386dc6b0ff4325eccbfcf3` 已整合会员工作。
- 所有 Go 命令均从此工作区根目录执行；前端命令从 `web/default` 执行。
- 会员变更未提交、推送或部署；原 `production/kkrich` 未被本任务修改。未连接生产或运行真实
  支付及模型收费请求；以下新增数据库测试在本机隔离容器中执行。
- 全仓测试、全量 lint / TypeScript、生产构建均跳过；使用受影响范围的检查。

## 易支付固定金额专项（2026-10-18）

用户确认继续复用当前正常的易支付在线支付。会员金额直接读取订单锁定的整数分，
不经过充值汇率/折扣计算，也不需要周期扣款或商品 ID。应用级联调验证了实际登录、
管理员配置、下单、签名表单、回调、三档升级和异常处理；回调由本地测试按易支付协议
签名，没有调用外部支付商或发生真实转账。

专项评审修复了三处边界：

- 套餐名称含 `&` 时现有 SDK 拒绝生成签名；仅在支付商品描述中替换为全角 `＆`。
  易支付纯本地表单生成/保存失败会释放本次 claim，可修正配置后继续原订单；其他
  渠道可能已在外部创建 checkout 的不确定状态仍保持保护。
- 异常款管理前端原来将数据库数字 ID 传给要求 `trade_no` 的接口，现统一使用并编码订单号。
- 会员表单明确在当前页 POST，避免异步请求结束后打开新窗口被移动浏览器拦截；
  钱包共享 helper 的默认导航行为保留。

最终真实后端进程专项：84 项全部通过。

```sh
python3 /tmp/kkai-membership-epay-h58crurp/run_epay_http_integration.py
```

结果：`/tmp/kkai-membership-epay-h58crurp/run-hz56upo3/result.json`。
确认客户端篡改金额/额度不生效、订单金额及额度不随目录改价改变、MD5 表单校验、重复
回调幂等、黄金→钻石→王者同账户额度 100000→300000→600000，revision 1→2→3。
两名用户均经实际管理配置获得正钱包 12345，开通/升级/异常/人工处理后保持原值。
错签名/商户拒绝；金额/方式/过期收款保存异常凭证且不授予权益；按 `trade_no` 人工
处理、重放处理和晚到重复回调均幂等。最终 1 个账户、3 成功订单、3 异常订单、6 唯一收据。
联调值仍是测试夹具，不替代正式目录。正式目录应配置为 CNY ¥600/¥2000/¥5000，额度显示值
600/2000/5000；在默认 `QuotaPerUnit=500000` 下对应内部 quota 300000000/1000000000/
2500000000。进程、数据库和临时公钥已清理；保留脚本与脱敏结果。

后端回归通过（分别 0.109s / 0.098s）：

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./controller -run '^TestMembershipEpay' -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=true go test ./controller -run '^(TestMembershipEpay|TestMembershipUncertainCheckoutCannotCreateAnotherProviderSession|TestMembershipStripeCheckoutUsesFrozenOneTimePrice|TestEpayCallbacksRejectAmbiguousSignaturesBeforeSettlement|TestMembershipLaunchRemovesOnlyNewTopUpPromotions)$' -count=1 -timeout=90s
gofmt -l controller/membership_payment.go controller/membership_epay_test.go model/membership_order.go
git diff --check
```

第一条覆盖 4 个会员易支付顶层测试；第二条仅覆盖其他完整匹配的兼容测试，正则中的
`TestMembershipEpay` 完整匹配分支没有匹配长测试名，不重复计入覆盖。格式及空白检查无输出。
进程在易支付 claim 后直接崩溃的恢复未扩展，仍依赖 45 分钟订单到期后重新下单。

前端会员 21 项及实际 API 2 项通过，类型检查覆盖本轮源码、测试及直接依赖；完整命令见
[前端验证记录](subscription-membership-frontend-validation.md)。未运行全仓测试或构建。

## 本地整合与真实数据库

隔离分支快进后应用会员工作，整合前 stash
`edc3afcbaa68490894101adc4f636225ee2b8cea` 保留。19 个冲突文件已无标记，Git 无未合并
条目，差异空白检查通过。Image Async 保留 schema 10，会员迁移改为 11，运行契约
当前生产发布使用已安装控制器支持的 bridge `(9,10,9)` 或 feature `(10,10,10)`；会员 schema 11
代码保留但本次不启用运行开关，不修改旧迁移含义或自动启用可选扩展。
本次先整合 `82947b10c` 的 Image Async，再快进到 `712831814`，保留主工作区并行新增的
两个控制台兼容性修复；后者与会员文件没有重叠，快进没有产生新冲突。

本机 Docker 使用 `desktop-linux` 的 Unix socket；MySQL 5.7 与 PostgreSQL 9.6 均为
独立测试容器。fixture 每次创建随机数据库/ schema，结束后清理；不改已有应用表。
会员测试使用正式迁移 DDL、真实行锁和多个连接，没有用单连接池串行化并发。

首次完整命令：

```sh
KKAI_MEMBERSHIP_ENABLED=true KKAI_IMAGE_ASYNC_MYSQL_TEST_DSN='root@tcp(127.0.0.1:51652)/?parseTime=true' KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN='host=127.0.0.1 port=51691 user=postgres dbname=postgres sslmode=disable' go test ./pkg/kkaimigrate -run '^TestMembershipRealDatabaseTransactions$' -count=1 -timeout=120s
```

MySQL 子项全部通过；整体 4.335s，仅 PostgreSQL 因该 fixture 要求 URL DSN 而失败。
改为 URL 后只重跑 PostgreSQL，通过（4.465s）：

```sh
KKAI_MEMBERSHIP_ENABLED=true KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN='postgres://postgres@127.0.0.1:51691/postgres?sslmode=disable' go test ./pkg/kkaimigrate -run '^TestMembershipRealDatabaseTransactions/postgres$' -count=1 -timeout=120s
```

两库覆盖 9→10→11 扩展与历史保留、重复迁移、并发付款只发放一次、并发预留不超支、
实扣封顶与统计幂等、账本写失败后额度/token 一起回滚、重购返还原账户、整笔钱包
回退及返还。付款事实由测试夹具提供，没有调用外部支付商。

Image Async 两库契约命令通过（25.845s）：

```sh
KKAI_IMAGE_ASYNC_MYSQL_TEST_DSN='root@tcp(127.0.0.1:51652)/?parseTime=true' KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN='postgres://postgres@127.0.0.1:51691/postgres?sslmode=disable' go test ./pkg/kkaimigrate -run '^TestImageAsyncRealDatabaseContract$' -count=1 -timeout=120s
```

覆盖 9→10 迁移及旧历史保留、受理事务回滚、真实锁冲突错误及 fence 失效。图片异步
与会员资金/部分交付的组合回归另记于下一节，不将迁移测试等同于调用入口验证。
上述端口仅是本轮本机测试记录；复用步骤与外部联调边界见
[联调步骤](subscription-membership-integration.md)。

两库检查结束后已删除本任务创建的两个临时容器及其 tmpfs 数据；未清理其他容器。

## 整合后的共享事务与应用编译

兼容性查询复用调用者事务后，定向回归通过（0.233s）；覆盖 SQLite 并发付款及选源、
旧订阅互斥、旧待付订单与迟到付款。应用入口及其依赖编译通过（0.088s；未运行入口测试）。

```sh
go test ./model -run '^(TestMembershipConcurrentCallbackAndFundingSelection|TestMembershipLegacyContractsAndReverseCreationGuard|TestMembershipLegacyPendingCheckoutAndLateLegacyPayment)$' -count=1 -timeout=90s
go test -tags external_frontend . -run '^$' -count=1 -timeout=60s
git diff --check
git diff --name-only --diff-filter=U
```

差异空白检查通过，未合并路径为空；125 个变更 Go 文件的 `gofmt -l` 无输出。

## 实际应用进程与 HTTP 权限联调

在全新 `/tmp` SQLite 上运行实际应用，完成 27 项检查：基础表初始化、9→10→11 迁移、
真实 setup、通过公钥端点完成 root/普通用户 RSA-OAEP 加密登录、管理员发布三档目录、
普通用户 catalog/self、无支付配置不可购买、普通用户管理读写 403、匿名访问 401、
拒绝写入不改变发布版本。最终确认没有订单、收据或会员账户，并停止应用、关闭端口。

```sh
python3 /tmp/kkai-membership-http-b61s7ki7/run_http_integration.py
```

结果为 `success: true`，27 项全部通过。精确子命令及检查记录保留在
`/tmp/kkai-membership-http-b61s7ki7/run-fnrmzo0j/result.json`；说明位于同目录上一级
`README.md`。测试脚本用最小环境白名单及临时 cwd，避免加载仓库 `.env`；凭据只在
内存中，SQLite 数据及临时公钥已删除。脚本位于 `/tmp`，不是新增应用依赖或生产脚本。

初次启动仅因测试环境漏设 `FRONTEND_MODE=external` 失败，补齐后通过，无源码修改。
这是实际后端 HTTP 联调，未运行浏览器、外部支付商或真实模型上游。

## 图片异步与会员组合

以下命令均通过；依次为 0.290s、0.218s、0.143s、0.154s：

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./controller -run '^TestMembershipImageAsyncWorkerEditFundingAndPartialDelivery$' -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=false go test ./controller -run '^TestImageAsync' -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=true go test ./model -run '^TestMembershipImage' -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=false go test ./service -run '^(TestImageAsync|TestImageStudioQuote|TestImageStudioBatchQuote|TestImageStudioEditQuote|TestImageStudioDeliveryBilling|TestReconcileStaleImageGeneration)' -count=1 -timeout=90s
```

使用本地 HTTP 图片上游和 R2 模拟服务，执行真实业务 worker；未访问真实外部供应商。
覆盖零钱包的会员请求两张仅交付一张、编辑请求整笔回退钱包、排队后改价/会员过期不
追改冻结报价、重复投递只执行/记账一次、未发生违规费用时释放其预留。

此前 `^TestMembershipImageAsync` 范围中的新增受理/恢复三项通过，唯一集成断言把违规
费用预留误当成最终费用。修正夹具期望后仅重跑失败集成项，即上面首条，通过；没有
通过放宽实际扣费规则让测试通过。

## 本地支付回调 HTTP

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./controller -run '^TestMembershipStripeWebhookVerifiesSignatureAndGrantsOnce$' -count=1 -timeout=90s
```

通过（0.090s）。本地使用 Stripe SDK 生成测试签名，经 HTTP 回调处理：坏签名返回 400
且不改账；有效签名返回 200；重复事件只产生一个会员账户/收款记录、1000 测试额度，
钱包 123 保持不变。没有访问 Stripe 沙箱，不等同于真实商户 checkout/webhook 联调。

## 账务、配置、订单及迁移（整合前阶段性回归）

以下命令在会员实现阶段均已通过。测试使用本地 SQLite 夹具；新增的真实
MySQL/PostgreSQL 结果单列于上一节，不将这些旧结果冒充整合后重跑。

```sh
go test ./model ./pkg/kkaimigrate ./controller -run '^(TestMembership|TestContractForDialect)' -count=1 -timeout=120s
go test ./model ./controller -run '^(TestMembership|TestCompleteSubscriptionOrder)' -count=1 -timeout=120s
go test -tags kkai_bridge ./pkg/kkaimigrate ./cmd/kkai-migrate -run '^(TestMembership|TestContractForDialect|TestDescribeContractJSON|TestApplyMembershipMigrationTarget)' -count=1 -timeout=120s
go test ./model -run '^(TestMembershipRankChangeCannotRepurchaseSameActiveTier|TestMembershipSnapshotUpgradeAndOriginalReservation|TestMembershipPaidExceptionsCommitAndCanBeResolvedOnce)$' -count=1 -timeout=120s
go test ./pkg/billingexpr -run '^TestMembership' -count=1 -timeout=60s
go test ./model -run '^TestMembership(Relay|Image)' -count=1 -timeout=90s
```

覆盖真实业务约束：升级与重购隔离、原来源退回、并发预留与幂等、最终耗尽与暂占区分、
折扣发布与冻结、迟到/额外/重复付款、同稳定档位不能借修改 rank 再买、旧订阅互斥、
人工核账权限和原子资金/统计。表达式预算测试覆盖上下界及拒绝无界表达式。

## 普通、图片、任务及实时请求

以下为已通过的阶段性定向回归。最后收尾修改另记于下一节，未因输入不变重复跑全套。

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembership' -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=true go test ./service ./relay ./relay/channel/openai -run '^(TestMembership|TestResponsesUsage)' -count=1 -timeout=120s
go test ./relay/channel -run '^(TestMembershipTransport|TestDoRequest|TestUpstreamPolicyResponseBody)' -count=1 -timeout=120s
go test ./relay ./relay/channel ./controller ./service -run '^(TestResponsesWS|TestBuildResponsesWS|TestNormalizeResponsesWS|TestUpstreamPolicy|TestDoRequest|TestShouldRetry|TestRealtimeRetry|TestImageStudio)' -count=1 -timeout=120s
KKAI_MEMBERSHIP_ENABLED=true go test ./relay/channel/openai ./service -run '^(TestMembershipRealtime|TestRealtimePolicy|TestOpenaiRealtimeHandler|TestStopRealtimeReaders)' -count=1 -timeout=90s
go test ./controller -run '^TestImageStudioRatioBatchQuoteAndSettlementMultiplyCountOnce$' -count=1 -timeout=90s
go test ./controller ./service ./model -run '^Test(ImageStudioMaximumPreconsume|ImageStudioDeliveryBilling|ImageStudioBillingGuard|MembershipTask|TaskMembership|PreConsumeTaskBilling|TaskBilling|TaskAccounting)' -count=1 -timeout=90s
go test ./relay/channel/openai -run '^TestMembershipRealtimeLeavesDownstreamWritableForPublicError$' -count=1 -timeout=90s
```

覆盖最大费用授权、会员不足整笔按钱包重新报价、异常封顶及审计、冻结工具/音频/表达式
价格、出站参数校验、未知结果保留预留、真实 HTTP 拒绝与模糊失败区分、图片交付折算、
任务终态及应计金额原子保存后由原生结算/outbox 恢复推进、Realtime 逐轮预留及客户端
错误可写性。原生 Task 普通完成不宣称状态与资金一次提交；人工结案才将任务状态、
资金、统计、审计及 outbox 纳入同一事务。

## 最终审查与收尾回归

独立复查修复了 HTTP/WS 模糊错误被误判成未执行、HTTP/SSE 失败终态丢失真实 usage、
显式零 usage 被当作未知、WS 截止时间等待数据库收尾才能断开、音频重复结算及失败后
仍记消费日志等问题。Realtime 表达式改为复用现有 token 分类，避免音频/cache 重复计费。

已通过：

```sh
go test ./controller -run '^(TestMembershipUnknownOutcomeStopsOtherwiseRetryableChannelFailure|TestShouldRetryStopsAfterOutputOrCancellation|TestRealtimeRetryAfterClientHandshake)$' -count=1 -timeout=90s
go test ./service ./relay ./relay/channel ./relay/channel/openai -run '^(TestMembershipFailedTerminal|TestMembershipResponses|TestMembershipTransport|TestOaiResponsesStreamHandler|TestResponsesToolBilling)' -count=1 -timeout=120s
go test ./relay -run '^(TestMembershipResponsesWS|TestResponsesWSShutdown|TestResponsesWSErrorAttribution)' -count=1 -timeout=120s
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembership(AudioSettlementKeepsFundingAndLogsConsistent|RealtimeExpressionDoesNotDoubleCountAudioAndCache|RealtimeFixedPriceAndZeroUsageSettleFrozenAuthorization)$' -count=1 -timeout=90s
go test ./service -run '^TestFixedPriceBillingDatabaseMatrix/sqlite' -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=true go test ./relay/channel/openai -run '^TestMembershipRealtimeAuthorizesEveryTurnAndFallsBackAtWalletPrice$' -count=1 -timeout=90s
```

## Midjourney 与共享人工核账

Midjourney 已完整接入会员预留、冻结价结算、原来源返还和共享人工核账。
以下真实处理入口使用本地 HTTP 上游及显式请求上下文，
不涉及真实模型收费，也不等同于完整认证与分发中间件联调。最终命令均通过：

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./controller -run '^TestMembershipMidjourney' -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=false go test ./controller -run '^TestMembershipMidjourney' -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=false go test ./service -run '^(TestPrepareMidjourney|TestSettleMidjourney|TestMidjourneyRefund|TestRefundMidjourney)' -count=1 -timeout=90s
```

controller 分别通过（0.148s / 0.085s）；旧路径 service 回归通过（0.045s）。覆盖普通
imagine、模式前缀路由及 SwapFace：资金不足不出站、不留任务；受理后保留预留；会员
到期或改价不改变原冻结金额；无会员及会员不足时整笔用钱包；终态失败退回原来源；
重复轮询幂等；旧订阅及会员关闭兼容；缺会员表时历史任务可读、新计费请求明确失败；
模糊响应只提交一次，502 返回本地任务 ID 并保留预留进入核账。

交叉审查同时修复同一上游 ID 对应多条本地任务时，旧任务超时污染会员任务状态的问题；
非终态的进度 100 不再导致停止轮询，会员表检查的数据库错误不再被误判成旧钱包路径。
用例确认这些边界。入口测试初跑两项因夹具缺少 JSON Content-Type 失败，补齐后定向
复跑及上述最终两环境回归均通过。

MJ 模型事务验证实际执行：

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./model -run '^TestMidjourneyMembership' -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=true go test ./model -run '^TestMidjourneyMembershipTerminalAccountingRollsBackTogether$' -count=1 -timeout=90s
```

首条前两项通过，第三项仅 `int` / `int64` 断言类型不一致；改为 `EqualValues` 后仅复跑
失败项，通过（0.074s）。验证授权失败不遗留任务、升级后仍返还原授权、终态账务失败
回滚任务与资金；不将首条初跑描述为全通过。

共享 MJ 核账与原来源隔离回归已通过（0.573s）：

```sh
go test ./model -run '^(TestMembershipMidjourneyReview|TestMembershipBillingReview|TestMembershipPreauthorizationIsNotExhaustionAndOldRefundIsIsolated)' -count=1 -timeout=120s
```

覆盖重购后的会员/钱包原来源、人工费用封顶、晚到失败不重开、已受理任务达到核查
门槛后才可处理，以及既有人工核账回归。此命令未重复验证后续原生 Task 状态标记及
轮询修复；对应结果单独记录。

共享人工核账现覆盖普通 relay / Responses WebSocket、Realtime 响应轮、MJ 及原生
Task。Image Studio 保持自己的交付收费与恢复契约。原生 Task 人工核账模型实际执行：

```sh
go test ./model -run '^TestMembershipTask' -count=1 -timeout=90s
go test ./model -run '^TestMembershipTaskReviewFinalizesOriginalFundingAndRejectsLateResults$' -count=1 -timeout=90s
```

首条仅新增用例的四个子例因 `TaskStatus` / `string` 断言类型不同失败，其余 Task 回归
及故障回滚通过；改为显式 `TaskStatus` 后仅复跑失败用例，通过（0.188s）。覆盖会员/
钱包 × 结算/释放、重购后的原账户隔离、异常封顶、统计一次、私有原因不泄漏、旧/新
轮询及晚到受理/费用重算不能重开已结案任务、outbox 唯一；统计写失败会回滚资金、
账本与任务。

原生 Task / Suno 服务轮询回归已通过（0.331s；外层 Python 超时为 120s）：

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembership(TaskPolling|SunoPolling)' -count=1 -timeout=90s
gofmt -l service/task_membership_polling_test.go
```

7 个子例覆盖会员/钱包超时后保留原来源预留、未知任务人工可见且继续凭真实上游 ID
轮询、受理后缺 ID 停止盲目查询但不退款、仍在提交且缺 ID 的任务不提前开放核账、
超时/缺 ID 状态写失败整体回滚、Suno 非失败状态的说明文字不终止任务及后续成功按
冻结价结算。`gofmt -l` 无输出，未为文档重复测试。

随后复查补齐原生 Task 统计阶段对未结算预留的守卫，恢复 outbox 遇到 `ErrNotReady`
会延后且不耗尽重试，人工释放后的 `Refunded + UNKNOWN` 结束恢复。模型最终定向
回归通过（0.336s），会员服务恢复/会计回归通过（0.300s），两个修复文件的空白检查
通过：

```sh
go test ./model -run '^(TestMembershipTask|TestUpdateWithStatusPreservingBilling|TestMarkTaskSubmissionAmbiguous|TestPersistTaskSubmissionAcceptance)' -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembership(SunoPolling|TaskRecovery)' -count=1 -timeout=90s
git diff --check -- model/task_accounting.go service/task_billing_recovery.go
```

服务回归覆盖成功后真实会计 handler 重复投递只记一次日志、请求数和用户/渠道统计；
确认失败并释放后重复会计不记消费；未知提交且缺目标额度时恢复与会计均延后并保留
预留；人工释放后同一恢复事件幂等结束，不虚构上游结果或消费。

旧任务恢复单独以关闭开关重跑，通过（0.076s）：

```sh
KKAI_MEMBERSHIP_ENABLED=false go test ./service -run '^TestTaskBillingRecovery' -count=1 -timeout=90s
```

此前混合范围的命令如下，只有五项旧夹具因未迁移会员 schema 在预扣费被拒绝，其他
用例通过。随后仅按旧路径配置重跑上述失败范围，没有放宽生产 schema 检查：

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^(TestTaskBillingRecovery|TestSweepTimedOutTasksLeavesAcceptedUnknownPollable|TestFailPollingTaskDoesNotOverwriteConcurrentSuccessOrRefund|TestMembershipTaskCompletionPersistsActualForCrashRecovery)' -count=1 -timeout=90s
```

## 编译与前端

```sh
go test -tags external_frontend . -run '^$' -count=1 -timeout=60s
```

通过。这是后端编译检查，不是生产镜像或前端构建。此前
`go test . ./router -run '^$' -count=1 -timeout=60s` 中 router 通过，根包因干净 worktree
没有 `web/default/dist` 嵌入资产失败；使用仓库现有 `external_frontend` 模式完成编译，
没有伪造 dist，也没有用未授权的全量前端构建替代。

```sh
bun x vitest run src/features/membership/membership.test.tsx
bun x vitest run src/features/pricing/__tests__/use-pricing-data.test.ts src/features/pricing/__tests__/model-cards.test.tsx src/features/pricing/__tests__/task-price-display.test.tsx
```

会员 16 项、定价 46 项通过；33 文件 lint/格式、局部 TypeScript、翻译同步和桌面/手机
明暗主题浏览器检查通过。精确 lint 文件清单、类型配置、视觉命令及截图见
[前端验证记录](subscription-membership-frontend-validation.md)。截图售价、额度与折扣是
fixture，不是正式套餐配置。

扩大 TypeScript 子集时，出现原 checkout 同样可复现的 PageTransition 路由类型诊断；
未改动该无关文件，不能声称全应用类型检查通过。

本次 locale 整合保留每种语言 15 个 Image Async 新键及 147 个会员新键，共 7820 键。
`bun run i18n:sync` 后 7 个语言的 missing/extras 均为 0，逐键对比冲突两侧确认没有丢失
新增值；指定这 7 个 JSON 的 `bun x oxfmt --check` 和差异空白检查通过。

整合后会员页、图片编辑器及提交恢复的组合回归通过：3 个文件、29 项测试，10.17s。

```sh
bun x vitest run src/features/membership/membership.test.tsx src/features/image-studio/components/image-composer.test.tsx src/features/image-studio/components/image-submission-recovery.test.tsx
```

随后同步主工作区两个控制台兼容性修复，相关边界、contract hook 与会员页面组合通过：
3 个文件、25 项测试，4.31s。未重复执行输入未变化的图片用例。

```sh
bun x vitest run src/components/console-compatibility-boundary.test.tsx src/hooks/use-console-contract.test.tsx src/features/membership/membership.test.tsx
```

## 尚未验证及集成约束

1. 真正支付商 checkout/webhook、真实模型及流式断线联调尚未执行；最大费用模型限额
   必须根据覆盖所有路由上游的强制累计上界配置，目录不会替管理员验证外部承诺。
2. Realtime 当前要求手动响应、单响应、关闭独立转写；自动 VAD、托管工具、图片输入等
   现有客户端行为会被明确拒绝，不能宣称这些调用方式已兼容。
3. 普通消费展示日志沿用既有独立日志写入；资金与统计事务成功后进程中断可能缺一条
   展示日志。持久化会员账本才是资金依据，不宣称跨数据库日志原子性。
4. 当前隔离分支已整合至 `712831814`，Image Async 10 / 会员 11 的迁移编号冲突已解决。
   原 checkout 与生产分支未被本任务修改；本地通过不证明生产 schema 或控制器已兼容。
5. `KKAI_MEMBERSHIP_ENABLED` 默认关闭，正式目录仍需由管理员发布上述价格/额度并填写逐组
   折扣。启用前还需独立发布及迁移验收；关闭销售与关闭运行能力不是同一操作，不能丢失
   既有账务。异常付款建议按“1 小时内首次响应、24 小时内完成处理”执行，客户未联系前不
   自动退款、取消或发放权益。

Image Studio 的原有交付保证继续保留：`TestImageStudioUndeliverableSuccessPayloadIsRefunded`
及部分归档测试要求未安全交付部分不向用户收费；即使上游已有用量，该部分成本也由平台
承担。这与普通 relay 未知用量保留预留的规则不同。图片恢复会先隔离/丢弃不可交付资产，
再按原来源释放，且不重发供应商请求。

# 会员请求暂占优化：实施与验证记录

日期：2026-11-09（含第二轮本地迁移与数据库验收）。依据：[开发文档](membership-reservation-development-plan-20261109.md)。

本记录是本地代码交付，不是生产发布回执。已实现 P0/P1 的本地异常恢复、公平核账和 v21 专用迁移入口，完整性能验收及事故暂停通道仍待补齐；仅在本机隔离数据库演练迁移，未修改生产数据库、构建生产镜像或启用线上开关。后续代码提交与推送按用户授权独立执行，不代表生产能力已启用。工作区其他任务的改动不属于本记录。

## 1. 已实现行为

| 场景 | 现在的实现 |
| --- | --- |
| 正常预占、派发、成功结算 | 继续使用原账本和原资金事务，不读取恢复表或额外保存证据；开启恢复模式不会增加同步 SQL 次数 |
| 最终应计额已知，但结算失败 | 只在异常路径读取原账本，绑定身份、RequestHash 与原冻结快照摘要；独立事务保存应计额与本地费用事实，后台可在 72h 之前重试 |
| 已确认零费用，但释放失败 | 保存 confirmed_zero 依据，按原会员账户或钱包、原令牌恢复；不向新套餐转账 |
| 断线、截断、用量未知 | pending + unknown，不把缺失用量当作零费用；退款接口也检查未知状态与本地费用 |
| 两份凭证矛盾、跨请求复用收据、证据损坏 | 保留原证据及有界冲突摘要，停止自动资金调整；同凭证重复采集不重复扣费 |
| 满 72h 的无证据暂占 | 独立 aged 通道，原资格与年龄守卫不变；成功查询日志且无已存可信证据/冲突时，才按既有政策全额补偿 |
| 补偿后才到证据 | 仅记录迟到证据，不向用户追扣 |
| 资金提交后缓存未刷新 | cache_pending，仅重试用户/令牌缓存失效；令牌已删除可结束，数据库查询失败必须重试 |
| 积压或冲突队首 | 固定扫描上界、持久游标、回绕、独立通道预算；最后实际处理项才推进游标 |
| leader 丢失 | 后台事务持有租约行锁并用数据库时间校验 holder/fence，提交前再检查，失败回滚 |

资金仍由现有 `settleMembershipRelayBillingTx`、`ReleaseMembershipBillingTx` 完成；新增模型不复制计算或扣款公式。恢复证据事务和资金事务相互独立，资金回滚不丢失已保存的证据。

额外异常保存使用独立的 2 秒上下文，不使用已被客户端取消的上下文。数据库完全不可用或进程在正常结算前崩溃时，仍可能无法保存内存用量；后续通过孤儿扫描和原 72h 兜底处理，不承诺所有用量都可追溯。

## 2. 代码落点

| 责任 | 文件 |
| --- | --- |
| 请求异常接入 | `service/membership_billing.go`、`service/membership_billing_evidence.go`、`service/billing.go`、`relay/request_billing.go`、`relay/channel/openai/relay_realtime_membership.go` |
| 证据、两段事务、终态与审计 | `model/membership_billing_recovery.go`、`model/membership_billing_recovery_types.go` |
| 扫描游标、退避、数据库 fence | `model/membership_billing_recovery_scan.go` |
| 作业模式、预算、日志取证、缓存恢复 | `service/membership_billing_recovery.go`、`service/kkai_runtime_jobs.go` |
| 可取消的既有标记与缓存失效 | `model/membership_billing_review.go`、`model/membership_billing.go`、`model/user_cache.go`、`model/token_cache.go` |
| v21 显式迁移与 runtime 合约 | `pkg/kkaimigrate/schema_membership_billing_recovery.go`、迁移目录/契约检查、`cmd/kkai-migrate/` |

没有新增运行时依赖、消息队列、正常请求前置网络调用或逐 token 工作。

## 3. 开关与后台预算

环境变量 `KKAI_MEMBERSHIP_BILLING_RECOVERY_MODE` 在作业注册时读取，默认 `off`；未知值启动报错。当前没有动态管理 API；切换模式需要重启对应进程，不能当作已经实现的即时停款接口。

- `off`：不新增前台旁表证据；已有 young ready 暂存，满 72h 仍优先尊重已有证据，继续原补偿政策。
- `observe`：独立影子游标和分类，不生成可执行 ready；满 72h 的兼容核账仍可调整资金。
- `apply`：捕获异常、处理可信终态、发现孤儿并执行有界核账。
- `paused`：本会员核账作业整轮停用并记录告警，当前实现也暂停缓存补偿；前台正常计费不受模式控制。

新表存在但回执/形状/索引不完整时失败关闭，不回退到不识别旁表的旧补偿程序。两表都缺失且模式为 off/observe 时，保留原 72h 核账入口。生产应先验证 schema 及兼容回退版本，再启用 apply。

每分钟运行，发现阶段两个孤儿通道各最多 100 行，冲突延期最多 10 行，aged 最多 100 行/25 笔调整，ready/cache/pending 合计最多 100 行/25 笔资金调整。整体最多 410 行、50 笔调整；aged 和 ready 不互抢预留预算。扫描阶段在 1 秒/6 秒子预算中预留 200ms 保存已完成进度。

20 秒是合作取消预算：数据库和 Redis 调用传递 context，底层连接、网络超时及驱动取消仍需隔离验收。未引入脱离上下文继续执行的超时 goroutine，也未修改全局连接池或 Redis 超时配置。不能把该预算描述为任何故障下均精确 20 秒硬停止。

## 4. Schema 与上线前提

v21 只新增 `kkai_membership_billing_recovery` 和 `kkai_membership_billing_scan_progress` 两张表及恢复调度/收据唯一索引，不修改已有资金、不自动制造历史证据，也没有 P2 派发身份列。

feature 合约最大兼容版本为 21，最低/默认迁移目标仍为 14；bridge 合约保持原范围。已有 v1–v20 迁移回执与摘要保留。旧 v15–v19 控制器继续按各自冻结合约校验；当前 v21 二进制不能借 v20 凭证执行 v20 生产迁移。历史凭证篡改测试通过固定 v20 合约保留，新生产入口拒绝 v21 冒用旧授权。

已提供 canonical/表列/主键/索引检查、离线扩展 API，以及独立 `--membership-billing-recovery-migration plan|status|apply` 入口。generic `--target 21` 仍只允许预检/dry-run，真实 apply 必须携带专用 root 只读凭证。控制器已在同级 infra 实现并通过完整本地 policy；Linux CLI + PG18 真实迁移演练已通过，但未安装到生产、未更改应用 controller pin。正式前提和兼容 A/B 版本顺序见 [infra runbook 32](../../../kkai-infra/docs/runbooks/32-newapi-membership-billing-recovery-schema.md)。

## 5. 首批已执行验证

本节是第一批历史验证，以下均在本地临时 SQLite 或现有隔离测试夹具执行。第二轮真实数据库/性能结果另见本节之后。带会员行为的命令显式启用会员开关，避免因默认关闭而跳过。所有命令退出码均为 0；未执行全仓库测试、全量构建、生产写入或端到端负载压测。

```sh
# 初始账务、核账与原有收尾回归：model/service PASS
KKAI_MEMBERSHIP_ENABLED=true go test ./model ./service -run '^Test(Membership(Recovery|Evidence|RelayReconcile|BillingReview|RelayStatistics|TokenRefund|UnlimitedToken|FiniteToken|Failed|Accepted|Truncated|Responses|Dispatch)|BackgroundJobRegistry|GenerateTextOtherInfo)' -count=1 -timeout=120s

# 退款守卫、请求收尾、Realtime：三个包 PASS
KKAI_MEMBERSHIP_ENABLED=true go test ./service ./relay/channel/openai ./relay -run '^Test(Membership(Evidence|Recovery|Failed|Accepted|Truncated|Responses|Realtime|Transport)|PrepareWalletRequestBillingWithoutMembershipAccount)' -count=1 -timeout=120s

# v21 迁移和历史数据/契约保留：PASS
go test ./pkg/kkaimigrate -run 'TestMembershipBillingRecovery|TestContractForDialectRequiresTenantSchema|TestMembershipPaymentExpandPreservesPriorContractsAndPayments' -count=1 -timeout=90s

# CLI 与各历史迁移授权门禁：PASS
go test ./cmd/kkai-migrate -run 'TestMembershipBillingRecovery|TestDescribeContractJSONRequiresTenantSchema|Test(ResellerPrefix|RequestTicket|Invoice|WalletPayment|Shop|MembershipPayment)' -count=1 -timeout=90s

# 恢复历史 v20 篡改用例后复测：PASS
go test ./cmd/kkai-migrate -run '^TestMembershipPayment' -count=1 -timeout=90s

# 上下文接口调整后的令牌缓存回归：PASS
KKAI_MEMBERSHIP_ENABLED=true go test ./model -run '^Test(TokenCache|TaskBillingCacheReconcile)' -count=1 -timeout=90s

# 传输未知结果与已有首字转发逻辑：两个包 PASS
KKAI_MEMBERSHIP_ENABLED=true go test ./relay/channel ./relay/helper -run '^Test(MembershipTransport|DoRequestNegotiatesSub2TTFT|StreamScannerHandler_Sub2TTFT)' -count=1 -timeout=90s

# bridge 的既有兼容边界：两个包 PASS
go test -tags kkai_bridge ./pkg/kkaimigrate ./cmd/kkai-migrate -run '^Test(MembershipBillingRecovery|DescribeContractJSON|ContractForDialect)' -count=1 -timeout=90s

# 修正证据损坏守卫与后台 checkpoint 后最终复测：model/service PASS
KKAI_MEMBERSHIP_ENABLED=true go test ./model ./service -run '^TestMembership(Recovery|Evidence)' -count=1 -timeout=120s

# 格式与空白检查：PASS
git diff --check
```

新增行为覆盖：证据提交后资金事务失败、冻结应计额与违规费保留、unknown 不变成 zero、收据复用/内容冲突、损坏证据禁止补偿、早期结算/72h 守卫、原账户/钱包/令牌归属、重复与迟到凭证、缓存查询失败和令牌删除区分、leader 丢失、跨页/中途断点/500 行后仍可推进、模式矩阵与部分 schema 拒绝旧入口。

`TestMembershipEvidenceSuccessAddsNoSynchronousSQL` 对相同的预占→派发→成功结算流程比较 off/apply，SQL 总数相同，恢复 schema 检查次数为零，恢复表无新增行。它证明正常流程未新增数据库步骤；它不是真实客户端首内容延迟或吞吐测试。

## 5.1 第二轮本地验收与性能修复

本轮真实数据库全部为 Docker 本机回环端口、独立临时命名空间及合成数据。测试使用 PostgreSQL 18.4、PostgreSQL 9.6 和 MySQL 5.7；MySQL 镜像在 Apple Silicon 上运行 AMD64 仿真，不将其耗时外推到生产。

- v21 runtime shape、收据唯一性及未知收据允许 NULL：SQLite、PG18、PG9.6、MySQL5.7 通过。
- 前台结算与后台恢复竞争：资金、令牌、用户统计只生效一次；真实用户行锁等待后取消、旧 leader 拒绝资金操作通过。
- Linux ARM64 CLI：9 项实际 root-proof/PG18 演练通过，包含只读 plan、过期/可写挂载/错误 owner/mode/旧 rollback 合约拒绝、DDL 中途失败原子回滚、有效 apply、只读身份重放。每次写操作后确认迁移角色 NOLOGIN 且零会话，独占数据库/角色/凭证 volume 已清理。它是本地开发二进制，运行身份为合成数据，不能替代正式 Linux AMD64 镜像或生产控制器验收。
- 迁移控制器基础版本完整本地 `make policy` 通过，279.6 秒；随后新增生产模式配置需要另行 `make check`，不能沿用该通过记录覆盖新配置。

HTTP 试验经过真实 `PrepareMembershipBudget → PreConsumeBilling → TextHelper → OpenAI adapter → StreamScanner → PostTextConsumeQuota`，固定 4 并发、连接池 8、4 次预热。客户端验证 role-only 事件后才出现实际 `hello world` 内容。SQL 总量包含并行 worker；后台只执行一轮，记录实际 worker 时长，不能称持续高压。

实际发现并修复了两处争用：

1. 孤儿扫描重复选取已有 pending/ready/conflict 记录，再次申请用户账本锁。现在以 `NOT EXISTS recovery` 只发现未入列账本，aged 仍读取既有证据，冲突保持自身退避。
2. 纯重试调度曾走 `User → Ledger → recovery`；现在保持 `Lease → Ledger → recovery`，不锁用户/账户/令牌、不改资金或证据。PG18/MySQL5.7 的回归在用户行锁未释放时执行延期，并逐字段核对金额及原证据不变。

初次 501 冲突试验中 apply 的后台阶段超时，40/40 请求仍成功；修复后超时消失。随后 16 格 PG18 矩阵（每格 200 次，总计 3,200 次）均正确完成和结算，但多用户积压重复试验仍可见吞吐下降，因此将冲突延期预算由每轮 100 降至 10，不改变 aged/ready 的资金处理配额。相同场景后台时间从约 684–738ms 降至 161–189ms。修订后会员场景不再重现原来的同向吞吐下降，钱包尾延迟仍有波动；这些结果不构成完整性能验收通过。

MySQL HTTP 矩阵初次执行在 180 秒上限结束，且发现测试夹具把 `logs.other` 建成 VARCHAR(191)，以及恢复 off 时已有的多用户预占死锁。该批数据已明确标为无效，不参与性能判断。夹具已恢复 TEXT 日志列；InnoDB 证明预占对不存在 request_id 的锁定查询导致主键 gap 锁互相阻塞。

MySQL 预占已改为在原外层事务内先 INSERT 占住唯一 request_id，再按原流程预占资金并 UPDATE 冻结账本；正常账本 SQL 仍为两条。只有确切的 MySQL 1062 重复键错误进入锁定读取和身份/指纹幂等检查，失败时草稿和资金一起回滚。MySQL 重放使用 current-read 读取余额，避免 Image 等外层旧 RR 快照返回陈旧额度；PG/SQLite 保留原查询路径。真实 MySQL 的 4 项回归全部通过：四用户并发占键、旧 RR 快照幂等与跨用户冲突整体回滚、钱包和会员令牌不足整体回滚。历史的 MySQL 会员零额度预占 RowsAffected 行为未在本轮修改。

修复后的短验收为 8/8 成功；随后功能矩阵的前七格共 280 次请求全部成功、SQL 错误为零、账本全部结算。最后一格预热时隔离 MySQL 容器因 1GiB 内存上限触发 OOM（退出 137），整次命令失败。恢复同一测试容器、重新发现动态端口后，仅复测最后一格，40/40 成功、SQL 错误和未结算账本均为零；保留 1GiB 上限，峰值约 592MiB，无新 OOM。八种功能场景已覆盖，结果是“七格成功 + 失败格复测通过”，不是一次完整矩阵命令通过。此轮与 infra 检查并行，只作功能验收。

原始逐请求样本（gzip）、全部汇总及 Linux 演练结果保存在 [本地验收证据](membership-recovery-evidence-20261109/measurements.json) 与 [Linux root-proof 回执](membership-recovery-evidence-20261109/linux-root-proof.json)。失败样本和修复前样本均保留，未事后删除或改写。

执行命令如下。DSN 仅由本机权限 0600 的临时文件注入环境，未存入仓库；下列命令需配置对应的 `KKAI_SHOP_*_TEST_DSN` 或 `KKAI_MEMBERSHIP_RECOVERY_*_DSN`。

```sh
# v20/v21 迁移与 CLI；PG18/MySQL fixture 启用：PASS
go test ./cmd/kkai-migrate ./pkg/kkaimigrate -run 'TestMembership(BillingRecovery|Payment)' -count=1 -timeout=120s

# PG9.6 新表 gate：PASS
go test ./pkg/kkaimigrate -run '^TestMembershipBillingRecoveryRuntimeSchemaAndReceiptOwnership$/postgres' -v -count=1 -timeout=90s

# PG18/PG9.6/MySQL5.7 账务验收：分方言执行；MySQL夹具/观测修正后失败项重跑 PASS
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembershipRecoveryRealDatabaseAcceptance$' -v -count=1 -timeout=120s
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembershipRecoveryRealDatabaseAcceptance$/postgres' -v -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembershipRecoveryRealDatabaseAcceptance$/mysql' -v -count=1 -timeout=90s
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembershipRecoveryRealDatabaseAcceptance/postgres/deferred_recovery_does_not_lock_user$' -count=1 -timeout=45s -v
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembershipRecoveryRealDatabaseAcceptance/mysql/(deferred_recovery_does_not_lock_user|real_row_lock_cancellation)$' -count=1 -timeout=45s -v

# 最终调度修复及正常路径无新增SQL：model/service PASS
KKAI_MEMBERSHIP_ENABLED=true go test ./model ./service -run '^TestMembership(Recovery|Evidence)' -count=1 -timeout=120s

# MySQL预占改动的直接调用方与账务回归：model/relay/service PASS
KKAI_MEMBERSHIP_ENABLED=true go test ./model ./relay ./service -run '^Test(Membership(Snapshot|WalletFallback|Preauthorization|Billing|ConcurrentCallback|TokenRefund|UnlimitedToken|FiniteToken|NoDiscount|LegacyContracts|OwningTransaction|Task|Image|RelayStatistics|Recovery|Evidence)|ImageBilling|MidjourneyMembership|PrepareWalletRequestBillingWithoutMembershipAccount)' -count=1 -timeout=120s
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^(TestImageAsyncFundingUsesAcceptanceTransaction|TestImageMembershipSignedQuoteRequiresCurrentPublishedDiscount|TestMembership(Reservation|ResponsesRejectsInsufficientAPIKeyQuotaWithoutChargingMember|InsufficientReservationUsesWalletPrice))' -count=1 -timeout=90s

# MySQL唯一键占用和RR幂等：4项PASS，1.34秒
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembershipReservationMySQLAcceptance$' -count=1 -timeout=60s -v

# 修复后MySQL功能矩阵：七格/280请求PASS，最后一格预热遇本地容器OOM，命令FAIL
KKAI_MEMBERSHIP_ENABLED=true KKAI_MEMBERSHIP_RECOVERY_MEASURE=1 KKAI_MEMBERSHIP_RECOVERY_SAMPLES=40 KKAI_MEMBERSHIP_RECOVERY_CONCURRENCY=4 go test -tags membership_acceptance ./service -run '^TestMembershipRecoveryHTTPMeasurement$/mysql/(membership|wallet)/users-4/conflicts-(0|501)/(off|apply)$' -count=1 -timeout=150s
# 恢复本机MySQL后仅复测失败格：40/40、SQL0、未结算0，PASS
KKAI_MEMBERSHIP_ENABLED=true KKAI_MEMBERSHIP_RECOVERY_MEASURE=1 KKAI_MEMBERSHIP_RECOVERY_SAMPLES=40 KKAI_MEMBERSHIP_RECOVERY_CONCURRENCY=4 go test -tags membership_acceptance ./service -run '^TestMembershipRecoveryHTTPMeasurement/mysql/wallet/users-4/conflicts-501/off$' -count=1 -timeout=90s -v

# 显式 opt-in：PG18 16格/200次，与4用户501冲突重复对照；功能PASS，性能不自动判PASS
KKAI_MEMBERSHIP_ENABLED=true KKAI_MEMBERSHIP_RECOVERY_MEASURE=1 KKAI_MEMBERSHIP_RECOVERY_SAMPLES=200 KKAI_MEMBERSHIP_RECOVERY_CONCURRENCY=4 go test -tags membership_acceptance ./service -run '^TestMembershipRecoveryHTTPMeasurement$/postgres' -count=1 -timeout=180s
KKAI_MEMBERSHIP_ENABLED=true KKAI_MEMBERSHIP_RECOVERY_MEASURE=1 KKAI_MEMBERSHIP_RECOVERY_SAMPLES=200 KKAI_MEMBERSHIP_RECOVERY_CONCURRENCY=4 go test -tags membership_acceptance ./service -run '^TestMembershipRecoveryHTTPMeasurement$/postgres/(membership|wallet)/users-4/conflicts-501' -count=3 -timeout=180s

# Linux CLI独立演练：9项PASS，67.549秒；TEMP是本机独占临时目录
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOPROXY=off GOTOOLCHAIN=local go build -ldflags='-X github.com/QuantumNous/new-api/common.SchemaManagementMode=external' -o TEMP/kkai-migrate ./cmd/kkai-migrate
python3 scripts/kkai/membership-recovery-linux-acceptance.py --container kkai-membership-v21-pg18-adc938 --binary TEMP/kkai-migrate --output TEMP --password-file TEMP/password

# 基础迁移控制器：完整本地policy PASS（不覆盖随后模式配置）
make -C /Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/kkai-infra policy

# 最终配置、控制器、真实stage/status协议：完整本地check PASS，320.702秒
make -C /Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/kkai-infra check
```

应用发布 wrapper 的 `bash scripts/kkai/deploy-manual-release_test.sh`、`bash -n scripts/kkai/deploy-manual-release.sh scripts/kkai/deploy-manual-release_test.sh` 均通过。新增生产模式配置的 40 个定向 Python 用例、相关发布/回退 Shell 回归、实际 Compose 渲染和本机 `ansible-playbook ... --check --diff` 通过；精确命令见 [infra 配置验收记录](../../../kkai-infra/docs/evidence/2026-11-09-membership-recovery-config.json)。

完整 `make check` 已按用户授权执行。首轮因旧 console contract 测试夹具缺少每槽恢复模式字段而失败；仅补齐夹具，未放宽生产校验，3 个相关用例及下一轮完整检查通过。最终互审又发现真实 stage 成功输出缺少恢复模式回执，导致应用 wrapper 误判结果；已改为从冻结 manifest 读取、核对并唯一输出模式，补充真实 stage 函数与 manifest reader 回归。随后对最终全部可执行输入再次运行完整检查：**PASS，320.702 秒**。三次完整检查分别记账，精确输入哈希与日志见 [最终 infra 门禁证据](../../../kkai-infra/docs/evidence/2026-11-09-membership-recovery-check.json)。这些是本地检查，不是生产主机 dry-run。

应用全仓测试/全量构建未运行；本轮无生产访问。性能验收文件使用 `membership_acceptance` build tag，避免普通 service 测试仅因这些 opt-in 测试而编译所有 Relay provider。三个本轮独占的数据库容器、临时密码/env 文件及 Linux 测试二进制已清理，保留脱敏日志与原始样本；详见 [本地资源清理回执](membership-recovery-evidence-20261109/cleanup.json)。

## 6. 尚未验收或实施

1. **数据库覆盖边界**：本机 PostgreSQL 18.4/9.6、MySQL 5.7 已执行新表唯一性与 runtime gate、并发结算、真实行锁取消和 leader 保护。独立 Linux CLI root-proof 的 v20→v21 apply/失败回滚/重放仅在 PG18 验证；其 runtime 身份为合成数据，不证明生产控制器的现场进程采集。生产规模执行计划、迁移影响和精确正式镜像仍待验收。
2. **首字与后台压力**：已建立真实 TextHelper/adapter/StreamScanner 的本地 HTTP 测量，解析首个非空 content，单独保留首 SSE。已测 off/apply、会员/钱包、单/多用户、0/501 冲突；结果见第二轮记录。尚不覆盖完整鉴权/选渠道/输入分词、非流式、无限令牌、历史基线提交或持续恢复负载；不能宣称全部性能门槛通过。
3. **生产迁移/发布**：专用控制器的本地实现及演练已完成；生产安装、迁移批准、真实兼容回退版本、exact image 合约与完整灰度尚未执行。恢复模式的 manifest/候选/回退冻结链路已在本地实现并通过定向检查；应用 wrapper 支持显式模式并校验 preflight/stage 回执，新增配置完整 `make check` 已授权执行，最终结果见上节。没有仅模式热切换或立即 paused 的独立入口，不能用手工改容器启用。实现与剩余门禁已写入 infra runbook 32。HTTP 灰度比例不等于后台核账用户比例；现 worker 没有用户白名单。
4. **P2 Sub2API**：只读核对登记仓库 `/Users/tokk/Documents/Codex/2026-08-09/li/work/sub2api`。`backend/internal/server/middleware/newapi_attribution.go` 验签后只向上下文保留归属，nonce 没有进入最终账单；`backend/internal/pkg/kkaiattribution/verifier.go` 的 nonce 用于短期防重；`backend/ent/schema/usage_log.go` 没有 NewAPI attempt 字段；`backend/internal/repository/usage_billing_repo.go` 的 `(request_id, api_key_id)` 不能当作该 attempt 的最终收据。未发现对应查询专用接口，所以未虚构 URL、未增加 HTTP 查账，也未修改 Sub2API。
5. **其它分期能力**：未自动提前释放本地费用事实不全的未派发孤儿；其证据仍 unknown，走既有 72h 政策。未调整预占公式、输出上限、会员权益时长或收费规则；P3 的估算分析另行实施。

下一交付顺序：补齐性能覆盖与事故暂停通道 → 验收并安装精确控制器、准备兼容 A/B 发布 → 单独授权迁移 → 按正式发布流程观察和启用；P2 在双方最终收据协议落地后独立交付。

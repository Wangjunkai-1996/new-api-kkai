# 会员请求暂占优化：首批实施与验证记录

日期：2026-11-09。依据：[开发文档](membership-reservation-development-plan-20261109.md)。

本记录是本地代码交付，不是生产发布回执。首批实现 P0/P1 的本地异常恢复与公平核账，未修改生产数据库、构建生产镜像、执行迁移或启用线上开关。后续代码提交与推送按用户授权独立执行，不代表生产能力已启用。工作区其他任务的改动不属于本记录。

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

每分钟运行，发现阶段三个通道各最多 100 行，aged 最多 100 行/25 笔调整，ready/cache/pending 合计最多 100 行/25 笔资金调整。整体最多 500 行、50 笔调整；aged 和 ready 不互抢预留预算。扫描阶段在 1 秒/6 秒子预算中预留 200ms 保存已完成进度。

20 秒是合作取消预算：数据库和 Redis 调用传递 context，底层连接、网络超时及驱动取消仍需隔离验收。未引入脱离上下文继续执行的超时 goroutine，也未修改全局连接池或 Redis 超时配置。不能把该预算描述为任何故障下均精确 20 秒硬停止。

## 4. Schema 与上线前提

v21 只新增 `kkai_membership_billing_recovery` 和 `kkai_membership_billing_scan_progress` 两张表及恢复调度/收据唯一索引，不修改已有资金、不自动制造历史证据，也没有 P2 派发身份列。

feature 合约最大兼容版本为 21，最低/默认迁移目标仍为 14；bridge 合约保持原范围。已有 v1–v20 迁移回执与摘要保留。旧 v15–v19 控制器继续按各自冻结合约校验；当前 v21 二进制不能借 v20 凭证执行 v20 生产迁移。历史凭证篡改测试通过固定 v20 合约保留，新生产入口拒绝 v21 冒用旧授权。

已提供 canonical/表列/主键/索引检查、离线扩展 API，以及 CLI 的 `--target 21` 预检/dry-run。**v21 专用生产迁移控制器尚未实现；CLI 真实 apply 明确拒绝。** 不能直接拿本批代码启用生产恢复。后续先完成控制器与可回退版本，单独取得迁移授权，按版本 runbook 验证后才能迁移、灰度及放量。

## 5. 已执行验证

以下均在本地临时 SQLite 或现有隔离测试夹具执行。带会员行为的命令显式启用会员开关，避免因默认关闭而跳过。所有命令退出码均为 0；未执行全仓库测试、全量构建、生产写入或端到端负载压测。

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

## 6. 尚未验收或实施

1. **真实数据库**：`KKAI_SHOP_MYSQL_TEST_DSN` 与 `KKAI_SHOP_POSTGRES_TEST_DSN` 均未配置；对应真实数据库子测试跳过。已验证 SQLite 与三方言 DDL 的静态兼容规则，未证明 MySQL 5.7.8/PostgreSQL 9.6 的实际锁、超时、执行计划及迁移性能。
2. **首字与后台压力**：未做真实客户端首个内容事件的 P50/P95/P99、吞吐、连接池/锁等待、积压扫描开关对照；未以 first_sse/role-only 事件代替首字。该项和生产同方言并发验收是启用门槛。
3. **生产迁移/发布**：v21 专用控制器、迁移批准、兼容回退版本、exact image 合约验证与完整灰度均未完成；未使用历史对话推断当前生产状态。
4. **P2 Sub2API**：只读核对登记仓库 `/Users/tokk/Documents/Codex/2026-08-09/li/work/sub2api`。`backend/internal/server/middleware/newapi_attribution.go` 验签后只向上下文保留归属，nonce 没有进入最终账单；`backend/internal/pkg/kkaiattribution/verifier.go` 的 nonce 用于短期防重；`backend/ent/schema/usage_log.go` 没有 NewAPI attempt 字段；`backend/internal/repository/usage_billing_repo.go` 的 `(request_id, api_key_id)` 不能当作该 attempt 的最终收据。未发现对应查询专用接口，所以未虚构 URL、未增加 HTTP 查账，也未修改 Sub2API。
5. **其它分期能力**：未自动提前释放本地费用事实不全的未派发孤儿；其证据仍 unknown，走既有 72h 政策。未调整预占公式、输出上限、会员权益时长或收费规则；P3 的估算分析另行实施。

下一交付顺序：完成隔离数据库及性能验收 → 实现并审查 v21 生产迁移控制器与兼容回退版本 → 单独授权迁移 → 按正常发布流程部署并 observe/apply；P2 在双方最终收据协议落地后独立交付。

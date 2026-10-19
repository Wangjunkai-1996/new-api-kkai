# 代理站在线兼容改造与验收

日期：2026-10-20。状态：本地实现与隔离验收，不是生产发布指令。

本记录取代此前将代理站上线绑定到全站停写的发布建议。产品仍采用一套 NewAPI、一套主数据库和受限代理后台；本次只调整数据库与旧程序共存的交付路径。历史 v12/v13 定义、checksum 和原发布/恢复证据保留，不能将它们的验收结果算作 v14 的验收结果。

截至应用发布前，已安装精确控制器 `820d8714760f9dbb7f22bade729f9bd335ff118c`；安装自检、全部 Python CLI 安装布局检查及安装后主站 status 通过。对应 `make policy` 完整通过。尚未执行生产维护、数据库迁移、路由切换或正式发布构建。`appdata` 固定 16 GiB，严禁扩容。生产状态必须另行通过已确认 infra 的 `make newapi-status` 现场观察，本文不以历史结果声明当前线上状态。

## 1. 数据库与程序的过渡顺序

| 状态 | 主迁移账本 | 可运行程序 | 代理业务 | 回退边界 |
| --- | --- | --- | --- | --- |
| 原始库 | 1–11 | 原生产版本 | 关闭 | 原版本 |
| 扩展中 | 1–11 + 独立步骤凭据 | 原生产版本 | 关闭 | 保留已完成的增量结构，不逆向删列 |
| 扩展完成 | 1–11 + 完整校验的步骤凭据 | 原版本与新 bridge | 关闭 | contract 前可回到原版本 |
| 最终 contract | 1–11、14 + 完成凭据 | 新 bridge 或新 feature | 启动时验证成功后才能开放 | 只允许租户感知版本 |

新 bridge 的版本范围为 11–14，但仅声明 11、14 两个兼容前缀。启动时拒绝原始或部分扩展的 11，也拒绝历史 12/13。11 必须完整验证新增结构、步骤凭据、旧用户名约束以及没有提前写入租户业务数据。feature 只接受完整 14。

最终 contract 前，必须先证明 active、rollback 和所有后台角色均已换为租户感知版本，且代理写入入口关闭。这个证明需要部署控制器提供实际运行身份；本轮没有用注释、版本区间或人工猜测代替它。

## 2. 已实现的增量结构操作

`pkg/kkaimigrate/schema_tenant_online*.go` 提供独立的计划、扩展、检查和最终 contract 库函数：

- 扩展期间不改动 1–11 账本，不删除旧全局用户名唯一约束，不回填规范化身份或现金。
- 新列有冻结的类型、长度、默认值及 NULL 规则。旧程序省略这些列时，主站租户默认为 0，历史充值现金快照保持未知，不伪造人民币来源。
- 每一步保存计划摘要、来源前缀和 started/done 凭据；仅恢复有凭据且结构完全匹配的操作。无主的部分结构、篡改凭据和无效索引均拒绝自动采纳或删除。
- PostgreSQL ≥12 使用固定连接和不等待的 advisory lock；已有表新增列及最终用户约束操作先取 `ACCESS EXCLUSIVE NOWAIT`，不会排队等待业务事务。普通语句上限 5 秒，并发索引语句上限 60 秒、锁预算 100 毫秒。获取不到锁就退出，完成的步骤保留以便核验后续跑。
- 在事务外创建 PostgreSQL 并发索引，检查 valid/ready、完整列序、唯一性及 predicate/expression。SQLite 同样拒绝部分索引冒充完整唯一约束。
- 最终 contract 的业务数据检查在删除约束事务前完成；事务内仅删除旧全局键、核验元数据并记录 14，不扫描业务表。它仍可能短暂阻塞，不承诺零锁等待。
- MySQL 5.7 与旧 PostgreSQL 使用明确的 `Options{Offline:true}` 阻塞兼容路径，不称为在线方案。MySQL 隐式提交 DDL 使用 owned contract 凭据恢复；不能因为旧键已部分删除便无法继续，也不能把未知结构当作已完成。

SQLite 是本地兼容回退，不具有 PostgreSQL 并发 DDL 的语义。生产只读采样确认 PostgreSQL 18.4、schema 11，logs 约 163 万行、quota_data 约 73 万行，采样时没有超过 5 秒的事务；这些观测不承诺迁移执行时没有新的锁竞争。

## 3. 业务与缓存的关闭边界

- `TENANT_ROUTING_ENABLED=true` 额外要求最终 schema 14，不能在 expanded 11 开启域名业务。
- 代理申请、平台审核及代理后台均使用启动时确定的结构检查结果；不影响主站状态接口。旧 bridge 进程不会因数据库随后变成 14 就自行开启代理入口。
- 消费结算和收益解冻 worker 使用相同的启动语义。最终 schema 下关闭域名路由仍保留历史资金恢复；过渡进程需在 contract 验收后替换。
- 用户 Redis hash 继续使用旧程序认识的 schema 2。新 reader 仍要求 `TenantID` 存在，否则回源；保留旧 Lua 的额度预扣、到账、退款与鉴权版本 fence 兼容。不能以提高 hash 版本导致旧程序静默跳过充值或退款更新。

`kkai-migrate --tenant-expand plan|status|check --json` 只读；`apply|finalize` 仅允许 PostgreSQL 在线路径，并要求固定 `/run/tenant-online/proof.json` 的 Linux root/0400 独立只读挂载。凭据必须绑定精确数据库、编译计划、来源、三运行角色及不超过 120 秒的期限，并在每次写入前复验。普通 Apply 和 `--target 14` 不能跨过最终边界。

`GET /api/internal/tenant-runtime` 只接受实际 socket 对端为 loopback 的请求，转发头不能授权。它返回初始化时冻结的 schema、代理业务开关、角色、版本和计划摘要。控制器分别核对容器镜像、真实运行进程和这个 HTTP 快照，不能执行第二个二进制就声称原进程已经初始化成功。

正式 bridge 产物经 `build-manual-release.sh --prepare-online --schema-contract bridge --frontend-mode external --planned-infra-sha <exact-sha> --planned-deployment-protocol tenant-online-v1` 生成不可变 `.online.json`。它只能经 `deploy-manual-release.sh --stage-online <metadata>` 进入既有 stage/canary/promote；stage 的服务端只读门禁必须证明扩展已完整。不能改 metadata 或提前启动 bridge 来绕过原始 schema 11 的拒绝。

冻结协议：plan `dbf0ffa60103902d51e41482d98b106643570b3060625251ade3473cd8a95fc4`；PostgreSQL 14 前缀 `sha256:b831116798762d19219db564cac02fd293352f911d8b6b9d25e8c8db0001eccb`。历史 11/12/13 摘要未改变。仅新 14 的 wallet carry 和支付反向聚合字段使用 TEXT（MySQL LONGTEXT），避免不同价格版本的分母累积超过单笔字段长度；单报价和单笔流水继续有长度边界。

## 4. 隔离验收与覆盖范围

以下第一轮定向检查全部通过。实库验收仅使用本地临时容器，按独立 schema/database 隔离。第一轮三个临时数据库容器已删除；下列端口是该轮执行记录，不是常驻测试环境。后续真实 HTTP 和 Linux/规模演练使用各自受限证据目录及独立本地容器，结果分别记录，不能混同生产环境。全仓库套件未运行。

已完成的检查包括：

- SQLite 的展开、contract、断点恢复、陌生部分结构拒绝、凭据篡改与唯一性。
- feature/bridge 运行契约、只读 CLI、代理 HTTP 关闭边界与后台任务启动边界。
- PostgreSQL 的短锁失败后恢复、共享 advisory lock、会话设置恢复、并发索引失败产物保留和结构漂移拒绝。
- PostgreSQL 每个扩展步骤提交后，经独立连接执行旧 GORM 用户、充值、Token 写入，再用当前模型核对默认值和余额。连接与原生产程序一致，使用 `PreferSimpleProtocol=true`；该项是模型兼容检查，后续另行运行真实旧/新二进制 HTTP 验收。
- 用户/session 缓存、旧/新额度 Lua、充值退款与鉴权撤销 fence 的兼容回归。
- PostgreSQL 模型层充值成功与重复回调、预扣/实际结算/重复结算、24 小时边界、提现/费用快照、已提现后拒付形成 debt、逐笔流水与 wallet 汇总一致。
- PostgreSQL 独立会话争用，通过锁等待图确认并发提现、重复幂等键与重复结算确实互相等待，而非单连接串行模拟。

第一轮财务测试在持久化结果边界注入用量。后续真实 HTTP 验收使用实际旧/bridge/feature 二进制、PostgreSQL、Redis、SMTP stub 和 OpenAI stub，涵盖真实 Epay 签名回调、充值幂等、模型返回及资金闭环；不会调用真实支付渠道或收费上游。第二租户倍率为 3 的演练发现中间除法导致少记 1 分，已将中间除法统一改为标准库精确有理数，并完成下面的最终重验。浏览器和真实域名验收另行记录。

最终新 14 验收（2026-10-20）：

- `tenant-online-http-acceptance.py` 最终 run11：195 项通过，旧站扩展期间 154 次、bridge 收口期间 150 次真实鉴权请求，均无失败。倍率 2/3 的实际供货成本均 20000 分，手续费 640/960 分，收益 19360/39040 分。四次 18 位小数调价产生 148 字符 carry 和 151 字符反向聚合，继续消费、提现保护、拒付和幂等反向均通过；最终钱包与流水对平。
- `tenant-online-linux-acceptance.py`：真实 Linux root/0400/独立只读证明允许执行，其余 rw mount、错误 owner/mode、过期及非 root 均拒绝，7 案通过。CLI SHA-256 为 `dc56eca6c5ea9ea84ff23811fde9ff93a8303fff26525432b3d13af260f0cac3`。
- 新鲜 schema11 合成库上 1630001 条 logs、730000 条 quota，主表 heap 2779324416 bytes；扩展 7.50 秒、收口 7.89 秒，171 次旧 SQL 事务成功、0 失败。16 个已记录的 AX 返回至 commit 窗口最大 5ms。演练使用本地 tmpfs，原索引合计 470704128 bytes 小于生产旧索引，50ms 采样可遗漏短锁，不能称为生产延迟保证。
- 最终 SQLite、真实 PostgreSQL 18、MySQL 5.7、default/bridge CLI 定向检查通过；MySQL 保留字身份查询也有实际回归。新两列真实 PG 类型为 NOT NULL TEXT，无默认值；旧迁移定义未变。

最终 HTTP 命令如下；仅旧 binary 复用，当前源码变化后 bridge、feature 和 helper 均重新编译：

```sh
python3 scripts/kkai/tenant-online-http-acceptance.py \
  --fixture /Users/tokk/.codex/tmp/kkai-reseller-release-20261019-xfxouy63/fixture/source11-main-identity-collisions.dump \
  --output /Users/tokk/.codex/tmp/kkai-online-http-20261020-run11 \
  --reuse-binaries /Users/tokk/.codex/tmp/kkai-online-http-20261020-run10
```

四枚二进制身份和清理记录见该目录的 `result.json`；业务源码指纹为 `28ab2dce04fdbf42d16da2acc6b503b1a0d107c47ca4a0d158d88da6df88ea6e`。Linux/规模的确切命令与边界在 `/Users/tokk/.codex/tmp/tenant-online-linux-scale-20261020-exacttext/README.md`、`summary.json`。以上均为隔离测试，不是生产上线证据。

### 确切命令与结果

1. 新迁移定向集合：PostgreSQL 18.4、SQLite、MySQL 5.7，通过，34.103 秒。旧 PostgreSQL 专用用例在这一轮按版本跳过，随后单独执行。

```sh
KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN='postgres://postgres@127.0.0.1:52954/kkai_test_online_acceptance?sslmode=disable' KKAI_IMAGE_ASYNC_MYSQL_TEST_DSN='root@tcp(127.0.0.1:51903)/?charset=utf8mb4&parseTime=True&loc=Local' go test ./pkg/kkaimigrate -run '^TestTenantOnline' -count=1 -timeout=120s
```

2. PostgreSQL 9.6 普通模式拒绝且没有写凭据，再显式离线展开并完成 14，通过，9.439 秒。

```sh
KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN='postgres://postgres@127.0.0.1:53224/kkai_test_online_acceptance?sslmode=disable' go test ./pkg/kkaimigrate -run '^TestTenantOnlineLegacyPostgresExplicitOfflineMigration$' -count=1 -timeout=120s
```

3. MySQL 故障注入增强后的最近范围，通过，7.347 秒。真实 Finalize 删除旧键后，注入 ledger 写入失败，验证账本仍 11、旧键已删除，再次 Finalize 恢复 14。

```sh
KKAI_IMAGE_ASYNC_MYSQL_TEST_DSN='root@tcp(127.0.0.1:51903)/?charset=utf8mb4&parseTime=True&loc=Local' go test ./pkg/kkaimigrate -run '^TestTenantOnline(MySQLExplicitOfflineMigration|RuntimeRequiresCompleteExpansion|LedgerRejectsHistoricalBranches)$' -count=1 -timeout=120s
```

4. 既有历史前缀、checksum、CLI 和 feature 运行契约回归，通过，两个包分别 2.931 / 0.575 秒。共享历史迁移检查另外通过，0.595 秒。

```sh
go test ./pkg/kkaimigrate ./cmd/kkai-migrate -run '^Test(TenantCompatibility(MigrationPreservesLegacyAccounts|Precheck|PreservesSQLite|RejectsUnnamed|Failure|RequiresPristine|Ledger|Runtime)|TenantSchema(MigrationContract|Rejects|Immutable)|MigrationCatalog|RuntimePlans|ContractForDialect|RuntimeRejectsV11|DescribeContractJSONRequiresTenantSchema|ApplyTenantMigrationTarget|TenantOnlineInspection|TenantOnlinePlan)' -count=1 -timeout=120s
go test ./pkg/kkaimigrate -run '^Test(MaintenanceTargetsExpandSequentiallyThroughV11|PlanHasImmutableChecksums|MigrationContractChecksum|CheckRejects|ApplyRejectsNonContiguousAppliedPrefix|ApplyResumesFromValidAppliedPrefix|CompatVersion)' -count=1 -timeout=120s
```

5. Bridge 运行契约和 CLI，通过，两个包分别 0.389 / 0.033 秒。bridge 专属的扩展完整性和历史分支互斥另有定向检查通过。

```sh
go test -tags kkai_bridge ./pkg/kkaimigrate ./cmd/kkai-migrate -run '^Test(TenantOnlineInspection|TenantOnlinePlan|ContractForDialectRequiresTenantSchema|RuntimeRejectsV11UntilExplicitTenantMigration|DescribeContractJSONRequiresTenantSchema|RuntimePlansUseReviewedTenantCompatibilityPath)$' -count=1 -timeout=120s
```

6. HTTP、Host 和 worker 启动边界，通过，三个包分别 0.089 / 0.074 / 0.489 秒。应用后台任务的直接依赖回归也通过，0.074 秒。

```sh
go test ./middleware ./router ./service -run '^Test(TenantSchema|ResellerFinanceAndApplicationReviewRoutes|TenantContext|TenantRoutingDisabled|TenantSession|ResellerFinanceWorkersFreezeUntilFinalSchemaAtStartup|ResellerPendingWorkerContinuesAfterAnotherTenantFails)' -count=1 -timeout=120s
go test . -run '^(TestApplicationBackgroundJobsDeclareLeaderWriteBoundary|TestServingBackgroundShutdownPersistsPerformanceSamples)$' -count=1 -timeout=120s -v
```

7. 缓存与额度兼容，通过，1.137 秒；PostgreSQL 财务与真实会话争用，通过，0.559 秒。没有使用真实支付渠道或真实模型服务。

```sh
go test ./model -run '^(TestTenantTransition|TestTryReserveQuota|TestRedisBatchReserve|TestReserveFallsBack|TestBatchReserve|TestSynchronousReserve|TestTokenCacheInit|TestTokenCacheTenantBackfill)' -count=1 -timeout=120s
TEST_RESELLER_POSTGRES_DSN='host=127.0.0.1 port=52954 user=postgres dbname=kkai_test_online_acceptance sslmode=disable' go test ./model -run '^TestResellerPostgres' -count=1 -timeout=120s -v
git diff --check
```

只读复审提出的 Currency 默认值、SQLite 部分唯一索引和 MySQL 新 schema 可达性问题均已修正。迁移/控制器独立复审未发现新的可复现 P1/P2；该结论不覆盖真实资金演练随后发现的精度缺陷，也不替代实际发布与业务验收。

## 5. 接下来必须完成的发布准备

1. 控制器在不停主站的前提下执行扩展，并明确记录 exact plan、每步结果、停止条件和恢复方式；不能进入全站维护来替代在线兼容设计。
2. 控制器证明 active、rollback、后台 writer/leader/worker 的实际二进制、schema 能力与代理关闭状态。contract 后禁止选回原 schema 11 二进制；回滚指针必须已经指向新 bridge。
3. 在生产等规模副本上验证耗时、长事务和锁预算；任何不满足条件的检查应退出并保持原站服务，而非自动切为维护。独立 `LOG_DB` 若启用，必须补专用在线扩展与验证，不能沿用旧的普通建索引入口冒充在线迁移。
4. 用精确构建的旧/新产物完成 HTTP、支付、模型、Redis 与后台并存演练，并验证原用户余额、Token 和 session；本轮的 GORM/模型测试不能替代它。
5. 用户已要求自主继续，但主站不得停机。先完成两次独立 bridge 的完整蓝绿验收，使 active/rollback/writer 都有兼容版本，再做 contract；最后通过正式发布启动最终 schema 进程并仅开放一个代理子域试点。DNS/TLS/Edge 及停租户行为单独验收，不把历史 reject-only 接入当作业务开放。

原维护控制器的恢复顺序已在本地修复并有独立回归：内部 router 在辅助服务探针前可用，公网恢复在所有必要服务之后。它随精确控制器版本验证安装，不是本次进入维护的授权；本次交付不执行维护路径。

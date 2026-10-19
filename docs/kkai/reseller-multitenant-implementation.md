# KKAI 代理品牌站实现与验收记录

日期：2026-10-19
工作区：`/Users/tokk/.codex/worktrees/tenant-prd-architecture/new-api-kkai`
交付范围：本地代码、文档和定向测试；未提交、未部署、未迁移生产数据库。

## 1. 已实现

- 主站 tenant=0、Host 拒绝回退、独立 owner/customer、租户身份/Token/Session/日志/缓存/限流隔离。
- 代理申请、平台审核/域名/状态、受限品牌、目录/供货成本、销售倍率、客户只读概览。
- 一套 React 前端的 Host 品牌、模型广场、申请/审核/代理后台及六语言文案。
- 当前供货价保底、不可变 quote、完整预算、统一出站检查、目录撤销后的重试复验。
- 人民币付费批次、FIFO 累计现金分摊、亚分 carry、原子消费结算及持久补偿。
- 24 小时 pending 解冻、人工支付宝提现、费用快照、冻结/核销/拒绝和债务限制。
- 平台整支付批次退款/拒付记录、混合批次恢复、反向流水与人工 usage 核对。
- 显式 v12 schema contract 及独立 LOG_DB 迁移；SQLite/MySQL/PostgreSQL 兼容。

详细接口、数据/锁边界、操作条件见[详细设计](./reseller-multitenant-detailed-design.md)。

## 2. 已执行的后端验证

以下均为定向命令，不是全仓库套件。数据库 DSN 仅从本任务临时容器配置传给子进程，未写入文档或打印凭据。

### 2.1 迁移

```sh
go test ./pkg/kkaimigrate ./cmd/kkai-migrate -count=1
go test -tags kkai_bridge ./pkg/kkaimigrate ./cmd/kkai-migrate -run 'Tenant|ContractForDialect|RuntimeRejectsV11|DescribeContract|Historical|MaintenanceTargets|PlanHasImmutable' -count=1
go test ./pkg/kkaimigrate -run '^TestTenant(SchemaMigrationRealDatabases|LogSchemaRealDatabases)$' -count=1
```

结果：通过。覆盖 SQLite 约束替换与原字段/索引/trigger 保留、PostgreSQL 9.6 与 MySQL 5.7 真正 UNIQUE constraint、主库和独立日志库幂等/checksum、v11 runtime 拒绝、显式 target 和固定快照一致性。实库临时 namespace 自动清理。

v12 checksum：`4a2edf08e0e75fa7c6827288edbd4a7bb83d0084b0b93204568043214c751519`。历史 v1–v11 checksum 未改。

### 2.2 租户与财务集成

```sh
go test ./model ./service ./middleware ./controller ./router ./relay ./relay/helper ./relay/channel ./relay/channel/openai ./relay/channel/claude -run 'Test(Tenant|Reseller|TokenCacheTenant|ResolveModelRequestRateLimit|AuthLogout|ValidateAndFill|EnsureEmailAvailable|InsertRejectsDuplicateEmail|InsertKeepsBlank|UserUpdate|Membership(FailedUsage|FailedTerminal|AcceptedResponse|TruncatedStream|ResponsesAccounting|ResponsesFailure|Transport)|Responses.*(Usage|Fail|Incomplete))' -count=1 -timeout=120s
```

结果：最后集成复验 10 个包全部通过（包括 owner 登录命名空间与安全渠道限制的最终代码）。覆盖跨租户身份、目录/模型列表、最低价和调价快照、现金分摊、重复消费/退款、解冻/提现、Host/Origin/缓存及未知用量处理。

```sh
go test ./model -run '^TestTenantConsumptionAtomicSettlementDatabaseMatrix$' -count=1 -timeout=120s
```

结果：SQLite、PostgreSQL 9.6、MySQL 5.7 全部通过，未跳过实库。覆盖原子预扣/差额结算、幂等、余额、Token 和 pending 账本一致性。真实库使用 `TEST_TENANT_POSTGRES_DSN` 和 `TEST_TENANT_MYSQL_DSN`。

```sh
go test ./model -run 'TenantConsumptionReconciliation|ResellerPayment|ResellerConcurrentPayment|ResellerMixedLot' -count=1
go test ./controller ./router -run 'ResellerConsumptionReconciliation|ResellerFinanceAndApplicationReviewRoutes' -count=1
```

结果：通过。人工核对/支付冲正新增分支的事务注入回滚、混合批次连续退款、跨租户和 actor 输入拒绝使用 SQLite 定向测试；这些新增分支没有另外声称已做三库实测。

### 2.3 预算、输出用量与重试

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^Test(Tenant(Budget|Outbound)|Membership(Budget|Outbound))' -count=1 -timeout=120s
go test ./service ./controller -run '^TestTenant(Catalog|Billing|Pricing)' -count=1 -timeout=120s
go test ./relay/channel/openai ./relay/channel/claude -run '^TestTenant(Chat|Claude)UsageDistinguishesMissingFromExplicitZero$' -count=1 -timeout=120s
go test ./controller -run '^Test(TenantRetry|GetChannel|RelayExhaustedPool|ShouldRetry|MembershipUnknownOutcome)' -count=1 -timeout=120s
```

结果：通过。缺失、空对象、部分 usage 与明确零消费区分；确认部分用量结算，未知结果保留预扣。重试在停租户、撤目录或更换供货组后拒绝，并保留原报价组。

Responses 空/部分 usage 的补充回归：

```sh
go test ./relay/channel/openai ./service -run '^Test(TenantResponses|MembershipResponses|Responses.*(Usage|Fail|Incomplete)|Membership(FailedUsage|FailedTerminal|AcceptedResponse|TruncatedStream|ResponsesAccounting))' -count=1 -timeout=120s
```

结果：通过。不仅缺字段，`usage={}`、单边计数和 null 也不能冒充 explicit zero。

补充 embeddings 的协议计数（不要求不存在的 completion_tokens）：

```sh
go test ./relay/channel/openai -run '^Test(Tenant(Chat|Embedding|Responses)|MembershipResponses|OpenaiHandlerMarksLocally)' -count=1 -timeout=120s
```

结果：通过。embeddings 的 prompt_tokens=0 是有效显式零用量，缺失/空 usage/只有 total_tokens 继续保留预扣。

安全传输与既有筛选回归：

```sh
go test ./service ./relay -run '^TestTenant(Catalog|ChannelSelection|AdaptorDispatch|Outbound)' -count=1 -timeout=120s
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^Test(Tenant|Membership(Budget|Outbound))' -count=1 -timeout=120s
go test ./model -run '^TestChannel(SelectionFiltersBeforePriorityAcrossCacheModes|WebSocketFilterRequiresNativeResponses)$' -count=1 -timeout=120s
```

结果：通过。包含同名模型的 SDK/HTTP 混合供货、SDK 高优先级过滤、pin 拒绝、数据库与内存缓存两种选路，以及适配器调用前拒绝。

管理登录命名空间与 2FA：

```sh
go test ./model ./controller -run 'TenantLoginScopes|TenantOwnerLogin|TenantRegistrationCreates' -count=1
go test ./model -run '^TestValidateAndFill' -count=1
```

结果：通过。即使客户与 owner 用户名、邮箱及密码完全相同，显式模式仍选择唯一身份；拒绝跨 Host、撤销 membership、主站 owner 模式和错误输入，2FA challenge 绑定原 owner 与当前 Host。

### 2.4 直接回归和编译

```sh
go test ./controller ./router ./service -run 'Test(KKAIBalanceAdjustment|LeaderLease|EmbeddedFrontend|LegacyFrontend|ExternalFrontend|SetRouter|ParseFrontend|IsAPIStyle)' -count=1 -timeout=120s
go test ./controller -run '^TestKKAIBalanceAdjustment' -count=1 -timeout=120s
go test ./service -run '^TestKKAIOutboxMySQL57' -count=1 -timeout=120s
go test . -run '^$' -timeout=120s
```

结果：router/service 通过；balance fixture 首轮缺当前钱包列，补齐测试 schema 后 controller 定向重跑通过。MySQL 测试 DSN 首次因不满足专用库前缀被安全拒绝，改用本任务 `kkai_test_*` 临时库后通过并清理。main 包编译通过，不执行 main 的测试。

## 3. 前端验证

在 `web/default` 执行：

```sh
bun run typecheck
bun run build
bun run i18n:check
```

结果：通过。首轮代理/品牌/定价相关 7 个测试文件共 47 测试通过，受影响文件 oxlint 通过。构建约 4.72 秒。i18n 六个要求语言有真实译文；其余现有 locale 沿用项目 sync fallback。未运行全站 Vitest 或全仓 lint。

最后 owner 登录入口变更后追加定向验证（没有重复全站 build）：

```sh
bun run test src/features/auth/sign-in/owner-login-api.test.ts src/features/auth/sign-in/owner-login.test.tsx src/features/auth/sign-in/owner-login-routes.test.ts src/features/auth/otp/__tests__/login-verification.test.tsx
./node_modules/.bin/tsgo --project node_modules/.tmp/tsconfig.owner-login.json --noEmit
bun run i18n:sync
bun run i18n:check
```

结果：4 文件、24 测试通过，受影响文件 oxlint 通过，定向 tsgo 通过。临时 tsconfig 只用于本地检查，包含修改文件及必要 ambient/route tree/main 声明；类型依赖自然带入其余路由源文件，不新增持久构建配置。

最终 `git diff --check` 与全部修改 Go 文件的 `gofmt -l` 检查通过。仅本任务创建的 PostgreSQL/MySQL 测试容器已停止并移除，其余容器未操作。

## 4. 域名预接入与尚未执行的上线验收

`kkai.win` 的 wildcard DNS、独立 DNS-01 证书、自动续期及 HTTP 308 / HTTPS 404 拒绝入口已完成，详见[域名接入记录](./reseller-domain-kkai-win.md)。目前尚未将代理 Host 接到应用；未知子域仍不能访问主站。该域名专项交付与代理应用上线是两件事，不将拒绝入口验收当成代理业务验收。

- 业务 Edge 的真实差异、受控应用及回滚；现有授权仅覆盖已执行的拒绝入口。
- 真实双 Host 浏览器 Cookie/CORS、真实支付回调、首家代理完整注册→支付→API→调价→解冻→人工提现链路。
- 生产数据身份冲突预检、备份恢复、旧实例排空、v12 迁移及兼容回滚版本验证。
- 核实 pilot 模型的文本能力元数据及累计 input/output bounds 证据，确认每个已开放渠道走受支持出站校验。

测试覆盖不能替代这些上线步骤。代理应用未执行生产 build、stage 或 promote，生产数据库未迁移。前述三库实测属于本地隔离测试，全仓库测试未运行。

## 5. 合并生产基线后的补充验收（2026-10-19）

开发 worktree 已从 `d8768f78ff9bfcbb29163b44362219fec0b1736c` 对齐本地 `production/kkrich` 的 `83b06bdc2351f5e682f27cd45cf9e076b10b6254`。这是开发目录的基线对齐，没有更新生产分支或发布应用。原 269 个工作文件全部恢复；只有导航、路由、七个 locale 及相应菜单测试因生产变动合并。原文件归档和 stash 保留在受限目录，未丢弃。

七个 locale 按键三方合并，分别保留两边对旧基线的全部修改；每个文件 7,961 键。同一键没有实质译文冲突。当前会员倍率、图片定价及订阅布局更新保留，83 个生产路由全部保留并加入 5 个代理路由。前后端 console contract 同时声明 `reseller_sites`，继续保留 `membership_ratio_quotes`；旧后端会被新前端兼容性边界拒绝。

本轮修复：

- 邮箱绑定在事务内从用户记录取得 TenantID，并原子更新 email 与 normalized_email；同邮箱允许跨租户，禁止在本租户重复。
- 代理申请审核锁定 owner 后校验已有 membership，包含已停用站点；同申请重试幂等，同一 owner 不能并发创建多个站。
- 倍率保存携带各模型 expected_version 并刷新版本，连续保存不再因旧版本固定返回 409。
- 代理 Host 不挂载订阅、邀请、图片/视频或平台管理相关请求；支持实际法律页面 `/user-agreement`。
- owner/customer 登录跳转及 401 恢复保留登录模式；身份变化清除私有查询和 mutation 缓存，公共站点配置保留。

合并后的后端回归再次通过第 2.2 节首条 10 包定向命令。只读迁移预检和构建协议另通过：

```sh
go test ./pkg/kkaimigrate ./cmd/kkai-migrate -run 'Test(TenantSchemaPrecheck|TenantPrecheckCLI|AuthenticationPrecheckCLI|DescribeContractJSON|TenantSchemaImmutableChecksum|TenantSchemaMigrationContract)' -count=1 -timeout=120s
python3 scripts/kkai/prepare-maintenance-release_test.py
bash scripts/kkai/build-manual-release_test.sh
go test ./common -run '^TestConsoleContract$' -count=1
go test -tags kkai_bridge ./common -run '^TestConsoleContract$' -count=1
```

前端在 `web/default` 执行以下定向验证：

```sh
bun run test src/lib/console-contract.test.ts src/hooks/use-console-contract.test.tsx src/components/console-compatibility-boundary.test.tsx src/hooks/__tests__/sidebar-config.test.tsx src/features/auth/sign-in/owner-login-routes.test.ts src/features/auth/sign-in/owner-login.test.tsx src/lib/auth-query-cache.test.ts src/lib/http-client-auth-redirect.test.ts
bun run test src/hooks/__tests__/sidebar-config.test.tsx
./node_modules/.bin/tsgo --project node_modules/.tmp/tsconfig.tenant-acceptance.json --noEmit
bun run i18n:check
```

结果：首轮 58 项中只有菜单旧预期失败；修正预期以保留生产新增菜单后，相关 14 项全部通过，其余 44 项通过。定向类型检查、5,931 个源文案键、相关文件 oxlint 与 diff 检查通过。未重复全站测试或构建，也未做真实浏览器验收。

## 6. 生产交付准备与剩余门禁

- v12 新增只读 `--precheck --target 12 --json`，使用与回填相同的身份规范化，输出聚合冲突数。SQLite query_only 测试验证成功和失败路径都不写库；本轮没有重复此前已通过的 PostgreSQL/MySQL 迁移矩阵。
- 维护构建仅接受两个精确协议：原 `rc41-maintenance-v1` 的 7→9，新增 `tenant-maintenance-v1` 的 11→12。目标租户镜像是 external 12/12/12，不是假定兼容旧 schema 的 bridge。v12 累计 PostgreSQL prefix 为 `sha256:ab50914ae132abdb57d3604e7aa0936f83b96299f2b70501a8615242ddeec1dd`，与第 2.1 节单条迁移 checksum 含义不同。
- infra 本地维护链路已绑定原 v11、目标 v12、精确产物、关闭路由、全部停写、零连接、新鲜备份及隔离恢复证据；迁移失败保留恢复意图。进入 writer/public 不可逆边界后，不能再恢复旧库丢弃新增业务数据。独立 LOG_SQL_DSN 非空时自动维护拒绝，需另行评审；应用支持独立日志库不等于此维护控制器已支持它。
- 正常 stage 的只读契约门禁拒绝在 live11 上部署 exact12；live12 则要求候选、当前与回滚/writer 全部支持12。不能拿普通部署代替迁移。
- Compose 和各探针显式携带受管平台 Host；默认租户路由仍关闭。平台列表只含精确平台域名及内部 `newapi-active`，不能加入 wildcard、kkai.win 或代理域名。
- 业务 Edge 只有本地候选渲染与真实 OpenResty 隔离验证；未实现或执行生产业务激活事务。候选将静态页/资源也经过当前 Host 的 site-config 验证，不让未知或停用域名拿到站点外壳。后续必须基于真实现场制作独立差异与恢复计划。
- 以上次已验收拒绝入口快照生成的业务差异仅供本机审阅：`/Users/tokk/.codex/tmp/kkai-tenant-business-preview-20261019-2l9uei8f/config.diff`。候选 SHA-256 为 `4c179ee4ad6ca8d306cdc603301da492a88dedc0a49cfb05f03905f2027f441a`；未刷新现场、不是生产 apply plan，也没有复用旧拒绝入口授权。
- 相关 infra Python、shell、Compose 及 9 个 Host/path 的隔离 nginx 验证已通过；完整 `make check` 后续已获本轮单独授权并执行，具体结果见第 7 节。之前域名专项例外不延伸到本次应用/schema/Ansible 变更。门禁和具体命令见已确认 infra 的 `docs/runbooks/25-newapi-tenant-schema-v12.md`。
- 尚未安装本轮控制器，应用 pin 保持实际已安装版本；尚未提交、构建维护镜像、执行生产预检/备份/迁移或创建 pilot tenant。appdata 固定 16 GiB，本轮不修改任何磁盘配置。

在已确认的 infra checkout 中，本轮其他定向验证命令如下；均通过，不代替完整门禁：

```sh
bash roles/newapi/tests/test-compose.sh
python3 -m unittest discover -s scripts/tests -p test_newapi_health.py -v
/opt/homebrew/Cellar/ansible/14.1.0/libexec/bin/python -m unittest discover -s scripts/tests -p test_newapi_status_remote.py -v
python3 -m unittest discover -s scripts/tests -p test_newapi_rollout_rollback_recovery.py -v
/opt/homebrew/Cellar/ansible/14.1.0/libexec/bin/python -m unittest discover -s scripts/tests -p test_frontend_independent_release.py -v
/opt/homebrew/Cellar/ansible/14.1.0/libexec/bin/python -m unittest discover -s scripts/tests -p test_newapi_architecture_maintenance.py -v
ruby roles/newapi/tests/test-staged-candidate-policy.rb
bash roles/newapi/tests/test-adopt-router-policy.sh
bash roles/newapi/tests/test-simple-deploy-policy.sh
python3 -m unittest scripts.tests.test_newapi_tenant_preflight.TenantPreflightTests scripts.tests.test_newapi_membership_preflight.MembershipPreflightTests scripts.tests.test_newapi_schema_v10.ImageAsyncPreflightTests
bash scripts/tests/test_newapi_tenant_schema_contract.sh
python3 -m unittest scripts.tests.test_tenant_business_config.TenantBusinessConfigTests
python3 -m unittest scripts.tests.test_tenant_business_nginx.TenantBusinessNginxTests
python3 scripts/check_source_size.py
git diff --check
```

系统 Python 缺 PyYAML 的 remote-status 首次失败后改用已有 Ansible venv 通过；simple policy 捕获的 Jinja 插值已改为位置参数并通过。源码行数策略通过，未扩大 baseline。原有 Worker 的无关修改摘要与操作前一致，未混入本轮改动。

## 7. 本轮完整 infra 门禁结果（2026-10-19）

用户明确授权后，在已确认的 infra checkout 执行 `make check`，UTC `15:10:22` 至 `15:18:18`，
约 7 分 56 秒。完整 syntax、policy 和现场存储校验通过；最后的
`ansible-playbook site.yml --check --diff` 在旧 SSH 断言失败，整体退出 2。

失败点是 `roles/ssh/tasks/main.yml:48`：当前规范 Tailscale 入口为 `sys1`，首次锁定保护却
无条件要求 ansible_host 等于 WireGuard 字面 IP。SSH/Host 文件本轮无 diff，没有绕过或修改
该保护。后续 NewAPI/Edge 主机角色尚未执行到，不能将完整门禁标为通过。

现场 `ok=70 changed=3 unreachable=0 failed=1 skipped=52`；3 项 changed 是 check mode
预计的目录/apt 索引变化。appdata 的固定 16 GiB existing-only 合同与最终 VG 预留断言通过，
没有扩容。尚未安装控制器、构建维护镜像、迁移生产数据库或开放代理业务。

证据目录：`/Users/tokk/.codex/tmp/kkai-reseller-make-check-20261019-v0stlxhg`，含完整受限
日志及结构化结果。后续需单独处理旧全量检查与当前拓扑差异；本轮检查授权不等于修改 SSH
保护或安装/发布的授权。已通过且相关输入未变的检查不重复执行。应用全仓测试本轮仍未运行。

用户随后授权清理过时阻断。infra 的 `make check` 已改为本地 syntax/policy/模板渲染综合检查，
原 `site.yml --check --diff` 独立保留为 `make site-check`，仅用于整机基线变更。旧命令失败
记录不改写为成功；本地检查不等于生产验收，独立 Edge 的覆盖保护继续有效。日常 SSH/
firewall 角色改为接受当前受管 Tailscale/`sys1` 或 WireGuard 私网入口，并保留确认、个人
管理员及公网/未知入口拒绝；首次 lockdown 专用的握手、自动恢复、重新连接流程未改。

修正后的本地完整 `make check` 在 UTC `15:25:33` 至 `15:29:58` 运行，退出 0。最终地址
修正后追加 29 个真实 Ansible guard 场景及 host/lockdown/rollback-access 语法检查，通过；
独立复审、diff 和源码大小策略通过。未重复整机 site-check，也未将本地结果宣称为生产验收。
新证据目录为 `/Users/tokk/.codex/tmp/kkai-local-check-20261019-9pudfm74`。准确命令和范围
见 infra runbook 25 的“日常检查入口修正”。应用全仓套件仍未运行，本轮没有生产操作。

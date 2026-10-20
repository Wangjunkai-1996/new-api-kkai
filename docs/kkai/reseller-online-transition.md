# 代理站在线兼容改造与验收

日期：2026-10-20。状态：平台后端、最终 schema 和前端已发布验收；首个代理账号、前缀和域名业务开放待分配。本文是执行记录，不是可直接重放的生产发布指令。

本记录取代此前将代理站上线绑定到全站停写的发布建议。产品仍采用一套 NewAPI、一套主数据库和受限代理后台；本次只调整数据库与旧程序共存的交付路径。历史 v12/v13 定义、checksum 和原发布/恢复证据保留，不能将它们的验收结果算作 v14 的验收结果。

已安装控制器和应用 pin 均为 `3bde016bd98c1231a361b3367617edfe66bbd04d`。在线扩展、两次重新验收的 bridge 发布、contract14、最终 feature 发布及独立前端激活均已完成。下面保留第一次 A4 回滚和诊断的历史，最终坐标与验收见「平台发布完成记录」。本轮在线交付未进入维护或停写，未扩容 `appdata`；其固定 16 GiB 合同保持不变。生产状态仍必须通过已确认 infra 的 `make newapi-status` 现场观察，本文不代替实时状态。

2026-10-20 首次发布阻断历史（本段窗口均为 2026-10-19 UTC）：原版 baseline `23:17:50–23:22:50` 为 124 个 Relay 事件、0 错误；A4 `23:40:59–23:45:59` 为 161 个事件、19 个 502，分布于五分钟中的四分钟。官方 rollback 返回 `rolled-back`，恢复原版 `kkai-prod-20261019.1792407452-55744a4ed`；即时和后续 status 均为 HEALTHY、rollback ready，两公网入口返回精确原版本。回滚后 `23:51:00–23:56:00` 为 157 个事件、1 个 404、0 个 502。19 是含重试的错误事件数，不能当作用户失败数，时间关联也不能替代根因。当时暂停后续资金 bridge、contract14、新前端激活及业务 Edge，保留已构建产物与原窗口；这个暂停状态已被后续重新验收取代。

后续上下文也保留独立窗口：10% 灰度的 120 秒为 29 个事件、1 个 502 和 1 个 404，50% 为 48 个事件、1 个 502，100% 为 62 个事件、1 个 413；混流聚合不能分辨产生事件的版本。2026-10-20 UTC `01:40:00–01:45:00`，旧版仍被选中时出现 1057 个事件、66 个 502，亦为 `openai_error` / 未知代码。因此不能直接宣布 A4 引入或未引入回归，也不能把状态页健康当作模型业务无故障；必须继续限定范围调查。

本地诊断采集器独立增加固定 `error_code` / `error_type` 白名单聚合，未知值在 SQL 边界内归为 `unknown`，不输出任意错误值、内容或身份；真实隔离 PostgreSQL 18.4 验证、9 项定向测试、独立审查及完整 `make policy` 通过。该本地工具不属于已安装控制器，无需重装、修改应用 pin 或重建应用。A4 的 19 个错误均为 `openai_error`，其中 18 个为字面代码 `unknown_error`、1 个代码不在白名单；旧版后续 66 个中分别为 19 和 47 个。`unknown_error` 既可能是本地缺码归一化，也可能来自供货方同名字面值；这些分类尚未证明故障来源。证据保存在 `/Users/tokk/.codex/tmp/kkai-reseller-online-release-20261020/`。

精确签名诊断对照已完成：仍使用原 A4 五分钟窗口与旧版 `2026-10-20T01:40:00Z–01:45:00Z` 窗口。A4 的 19 个错误中，18 个在旧版窗口存在相同 `(channel, model, request_path, raw error_type, raw error_code, status)`，1 个未匹配，0 个不可比较；旧版对应为 13 个重现、53 个未匹配。重复事件使用 `EXISTS`，不会相互连接放大；签名仅在数据库内比较，只输出固定分类和计数。这里采集器的 baseline/candidate 名称仅表示诊断时间先后，分别对应 A4/旧版，不能替换原发布 baseline。该结果既不能证明同一根因，也不能排除发生频率回归。证据为 `error-signature-A4-vs-old-diagnostic.txt`。

后续独立操作者复核决定解除“仅凭上述 502 继续阻断”的判断，复用现有产物重新执行完整候选验收和 10/50/100 灰度，不能把此前 A4 自动算作已接受。零错误不是发布条件；原版也有的同类错误须保留记录和观察。版本/健康/路由/契约/回滚身份失败、主站鉴权或计费等可复现回归，以及有候选关联证据的持续恶化，才触发停止或正式回滚。单个未匹配签名不单独证明回归。不得为此修改渠道或停止主站。

本次新增诊断验证命令及结果：

- `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v scripts.tests.test_newapi_error_window`：11 项通过。
- `PYTHONDONTWRITEBYTECODE=1 python3 /Users/tokk/.codex/tmp/kkai-reseller-online-release-20261020/check-error-signature-local-pg.py`：本地隔离 PostgreSQL 18.4 的 27 行夹具通过，临时数据库和容器均已清理。
- infra `make policy`：完整通过，记录为 `policy-error-signature-diagnostics.log`；独立审查无 P1/P2。
- `git diff --check`：通过；该诊断修改未运行应用全仓库测试，也未要求重建或发布应用。

## 2026-10-20 平台发布完成记录

用户选择「先完成平台上线，代理账号和前缀以后再定」。本次没有创建虚构 owner、租户或子域名，没有转换现有客户。`kkai.win` 的业务 Edge 未 apply，保留既有 reject-only 接入；后续明确 owner 和前缀后，再登记域名并完成真实代理试点验收。

| 已验收对象 | 精确版本或状态 |
| --- | --- |
| 第一次重新验收的 bridge A4 | `kkai-prod-20261019.1792452025-4a8165d34`，rollout `841f049e38cb40c08502d08cbd94fecf` |
| 第二次 bridge B | `kkai-prod-20261019.1792452394-7da8ea7d9`，rollout `6aa3bb10e8944be781f0b7b3d27b99ea` |
| 最终数据库 | canonical 1–11、14，`finalized`，44/44；contract 和独立只读 status 均 verified |
| 最终后端 C | `kkai-prod-20261020.1792466895-dad8a4792`，feature/external，rollout `619fce2400bb48e483801d4d9bf3b284` |
| 最终回滚 | B，源码 `7da8ea7d91c4a473840a9a569343226036abf6a3`，保留精确有理数资金修复 |
| 最终前端 | `kkai-frontend-20261019.1792452856-d9d82eccf`，format2/API2 |
| 前端 previous | `kkai-frontend-20261019.1792407847-a58fb93c2`；官方 rollback-plan 为 `backend-only` |

A4/B 均复用原不可变产物，重新完成私有候选检查、10%/50%/100% 每档至少 120 秒、promotion、即时和五分钟后 status，以及等长错误窗复核。A4 发布后为 38/1355 个错误事件，基线 201/1400；B 发布后为 39/1545，基线 43/1396。它们是真实的两次已接受 bridge 发布，不将首次回滚或指针修复算作成功发布。

contract 通过新鲜 plan 确认值 `3b7fd5f6b7f07b5f0f03049b651d1c35620be04b276824acad9b0b28c818fd64` 执行，独立 `status --operation contract` 确认 version14、finalized、44/44。随后只构建一次正式 feature/external 后端；C 相对 B 的 Go、迁移和发布脚本没有变化，仅前端金额显示及文档变化，因此没有重跑已通过的 195 项隔离业务演练。候选实际 HTTP 启动快照为 finalized、ready=true、serving、routing=true、后台任务关闭；15 项主站 Host、鉴权、私有接口及未知域名检查通过。

C 正式晋级后，即时和实际五分钟后的 `make newapi-status` 均为精确目标版本、HEALTHY、snapshot stable、rollback ready。writer 的真实 HTTP 快照为 finalized、ready=true、routing=true、leader、后台任务开启。最终 manifest 核对 active C、rollback B，以及无 candidate/switch 事务；未手动重启进程。后端发布保留原前端，事务结束后才 prepare/activate 新前端。

C 的错误窗口均为 2026-10-20 UTC，含重试的日志事件比例不代表客户失败率：

| 窗口 | 总事件 / 错误事件 | 状态 |
| --- | --- | --- |
| 固定 120 秒基线 | 592 / 17 | 17×502 |
| C 10% | 512 / 12 | 10×502、400/404 各一条 |
| C 50% | 673 / 5 | 4×502、一条 500/claude_error |
| C 100% | 576 / 7 | 7×502 |
| 发布前 `03:24:04–03:29:04` | 1557 / 44 | 42×502、2×404 |
| 发布后 `03:44:48–03:49:48` | 1391 / 3 | 1×502、2×未知路由 404 |

50% 的孤立 Claude 500 在 100% 和五分钟发布后窗口未复现，仍保留为未归因事件。剩余 502 和未知路由 404 同样不宣称已定位根因；未发现与候选相关的新故障证据。操作者与独立复核接受发布，不以零错误为条件，也不宣称全体线上请求零失败。

前端实测入口、登录/注册、模型广场和代理路径返回精确新资源；JS/CSS 为版本化 immutable 地址，metadata 不公开。完整同名 `Cache-Control` 响应头包含 no-store；`expires -1` 另生成的第一条 no-cache 不是缓存策略回归。浏览器显示 KKAI 首页、登录和注册表单，未登录进入代理后台跳至 403；API 权限拒绝通过。没有注册生产测试账号或调用真实收费模型/支付来凑验收。

首页原始 SHA 校验保留一个明确的限定例外：`/assets/kikai-home.html` 公网 200/no-store，但 Cloudflare 自动注入一段固定统计脚本并调整结束标签缩进。原始公网 SHA 为 `655b1ffd2402c8d839a6431626fc94e11f0825e2827b775e030a57038d09ae8d`，产物 SHA 为 `ab442b86decb6c129ac25c5b2ce8824ee37498ac6f6f15ff34de390751a7cfc8`，不能标记原始 SHA 一致。仅替换那段完整固定字节、要求恰好一次后，其余内容与当前文件逐字节一致，归一后 SHA 等于产物；操作者在用户自主继续、不得影响主站的授权内限定接受该已识别转换。未改 Cloudflare 统计或 Edge，也未使用任意 script/空白过滤。固定比较段从行首两空格的 `<script type="module" src="https://static.cloudflareinsights.com/beacon.min.js/…"` 开始，包含完整 SRI、data-cf-beacon 和下一行 `</body>`，不含末尾换行，SHA 为 `d7c31959ec265a7ca72cd1bf7ce50f86363ebc7c1c0fea93dbcb357242cb43a0`。

执行命令与结果（SSH 均使用 sys1 的既定私网选项，以下省略重复选项；这些是执行记录）：

```sh
sudo -n /usr/local/sbin/kkai-newapi-schema-migrate tenant-online contract --artifact-receipt /srv/kkai/newapi-tenant-online/artifacts/kkai-prod-20261019.1792452394-7da8ea7d9/receipt.json --confirm-plan-sha256 3b7fd5f6b7f07b5f0f03049b651d1c35620be04b276824acad9b0b28c818fd64
sudo -n /usr/local/sbin/kkai-newapi-schema-migrate tenant-online status --operation contract --artifact-receipt /srv/kkai/newapi-tenant-online/artifacts/kkai-prod-20261019.1792452394-7da8ea7d9/receipt.json
scripts/kkai/build-manual-release.sh --schema-contract feature --frontend-mode external
scripts/kkai/deploy-manual-release.sh --stage /Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/.local-releases/kkai-prod-20261020.1792466895-dad8a4792.json --tenant-routing-enabled true
python3 /Users/tokk/.codex/tmp/kkai-reseller-online-release-20261020/check-candidate.py --version kkai-prod-20261020.1792466895-dad8a4792 --routing true
make newapi-status
BASELINE_START=2026-10-20T03:24:04Z BASELINE_END=2026-10-20T03:29:04Z CANDIDATE_START=2026-10-20T03:44:48Z CANDIDATE_END=2026-10-20T03:49:48Z make newapi-error-window
sudo -n /usr/local/sbin/kkai-frontend-manual-deploy prepare --metadata /tmp/kkai-frontend-20261019.1792452856-d9d82eccf.json --backend-manifest /srv/kkai/newapi-releases/kkai-prod-20261020.1792466895-dad8a4792/release-manifest.yml
sudo -n /usr/local/sbin/kkai-frontend-manual-deploy activate --release-id kkai-frontend-20261019.1792452856-d9d82eccf --expected-current-release kkai-frontend-20261019.1792407847-a58fb93c2 --backend-manifest /srv/kkai/newapi-releases/kkai-prod-20261020.1792466895-dad8a4792/release-manifest.yml
sudo -n /usr/local/sbin/kkai-frontend-manual-deploy rollback-plan --backend-manifest /srv/kkai/newapi-rollback/release-manifest.yml
```

以上命令均退出 0；完整 stage、三档 rollout、handoff/promote、前端和只读 HTTP 证据保存在 `/Users/tokk/.codex/tmp/kkai-reseller-online-release-20261020/resume/`。`C-status-pre-delay.txt` 是提前取得的附加状态，不冒充五分钟延迟证据；正确延迟证据为 `C-status-delayed.txt`。旧 v10 schema snapshot 使自动 release/image 清理延期，warning 已记录，不是 promotion 失败；保留生产回滚资产、schema receipt/archive。全仓库测试未运行；此前的定向财务、跨数据库和真实 HTTP 检查没有因纯文档或本次重复确认而重跑。

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

生产只读计划暴露了 PostgreSQL 的权限差异：仅有 SELECT 的 standby 看不到 `information_schema.table_constraints` 的主键，而真实 `pg_catalog` 主键存在。新在线检查通过 `pg_catalog` 核对完整有序主键及 valid/ready/unique/immediate 状态，保留类型、默认值和非空检查，不增加数据库权限。真实 SELECT-only、非 owner、默认只读角色的 expanded/finalized 检查及错误主键拒绝均有回归；此修复不改变冻结的结构或迁移摘要。

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

## 5. 发布约束与后续代理分配

1. 控制器在不停主站的前提下执行扩展，并明确记录 exact plan、每步结果、停止条件和恢复方式；不能进入全站维护来替代在线兼容设计。
2. 控制器证明 active、rollback、后台 writer/leader/worker 的实际二进制、schema 能力与代理关闭状态。contract 后禁止选回原 schema 11 二进制；回滚指针必须已经指向新 bridge。
3. 在生产等规模副本上验证耗时、长事务和锁预算；任何不满足条件的检查应退出并保持原站服务，而非自动切为维护。独立 `LOG_DB` 若启用，必须补专用在线扩展与验证，不能沿用旧的普通建索引入口冒充在线迁移。
4. 用精确构建的旧/新产物完成 HTTP、支付、模型、Redis 与后台并存演练，并验证原用户余额、Token 和 session；GORM/模型测试不能替代真实 HTTP。本次两种层面的验收均已完成，证据见前文。
5. 本次两次 bridge、contract 和最终功能发布已完成。按用户最后选择，首个代理 owner 和前缀以后确定；明确分配后再登记 tenant/domain、审查增量业务 Edge，并完成真实 DNS/TLS/Host、代理及停租户验收。不能把平台路由开关或历史 reject-only 接入当作代理域名已开放，也不能隐式将现有客户转成 owner。

原维护控制器的恢复顺序已在本地修复并有独立回归：内部 router 在辅助服务探针前可用，公网恢复在所有必要服务之后。它随精确控制器版本验证安装，不是本次进入维护的授权；本次交付不执行维护路径。

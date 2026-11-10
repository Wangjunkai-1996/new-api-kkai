# rc.49 Redis 专项处理方案

评审日期：2026-11-10。状态：Redis MAC 本地实现及定向 PostgreSQL / Redis 验证已完成，尚未提交或发布。完整双版本混跑、性能预算与上线门禁仍未完成。实现和验证范围见 [本地实施记录](rc49-redis-implementation.md)。

## 推荐决策

**保留现有 Redis 8.6.3 standalone、DB 0、全部 key 格式、签名秘密及额度协议；增量加入用户、Token、Session 鉴权元数据 MAC。**

这次不迁移 Redis，不改 Cluster，不引入双 namespace 或双写桥，不清库。默认不增加兼容准备版、资金停机窗口或会员恢复暂停操作；通过真实相邻版本混跑验证后，按原有应用蓝绿流程发布。验证发现实际不兼容时再处理具体问题，不预先建设一套迁移机制。

该决策由缓存协议、账务与租户安全、发布与回退验证三个独立角色，加上负责实时运维取证的主代理完成两轮交叉评审。数据库范围遵循用户明确决定：仅 PostgreSQL。

## 本次线上事实

通过 SSH 别名 `sys1`（`100.124.157.30`，用户 `tokk`）检查；未修改服务器配置、容器、业务数据或 Redis key。

| 项目 | 2026-11-10 本次观测 |
| --- | --- |
| 官方状态入口 | 同级 `kkai-infra` 执行 `make newapi-status`：HEALTHY、snapshot stable、rollback ready |
| 当前应用 | blue serving + 独立 writer：`kkai-prod-20261109.1794239898-a552996db` |
| 回退应用 | green standby-readonly：`kkai-prod-20261109.1794233284-fc46400f3` |
| Redis | `8.6.3`，`redis_mode=standalone`、`cluster_enabled=0`、master、0 replicas |
| 连接关系 | 上述三个应用进程共同连接 `newapi-redis:6379`、DB 0 |
| 缓存与账务配置 | `SYNC_FREQUENCY=60`；三个进程 `BATCH_UPDATE_ENABLED=false` |
| 秘密一致性 | 三个应用挂载的 CryptoSecret 相同；SessionSecret 也相同；仅输出布尔比较结果，无秘密或摘要 |
| 内存与持久化 | 使用约 4.93 MiB，历史峰值约 17.19 MiB，上限 2 GiB；`noeviction`、AOF everysec；AOF/RDB 状态 ok |
| 计数快照 | 14 connections；evicted_keys=0、rejected_connections=0；不据此推断所有业务均无错误 |
| 风险 Stream 元数据 | 09:44:17 北京时间：长度 0、pending 0、lag 0；只读元数据，没有读取事件内容 |

Redis/容器主要快照时间为北京时间 09:39:19（UTC 01:39:19）。这些结论来自本次 status、容器允许字段和 Redis INFO，不来自旧文档。角色/版本来自容器身份和状态检查，不将 `make newapi-status` 夸称为完整 router/writer/leader 拓扑证明。

当前分析应用 HEAD 为 `5eb8a92581df793be4ca108aaa27391bdef68442`；实际生产源码为上述 a552996db。对 `common/redis.go`、`model/token_cache.go`、`model/user_cache.go`、`model/user_auth_cache.go`、`model/user_session.go`、`model/quota_reserve.go` 比较，生产 current→分析 HEAD、rollback→current 均无差异。未来实施仍需重新冻结精确相邻版本。

infra 当前 checkout 为 `29e9a8e927ea35df939e912baa381e663591f4a2`，工作区干净。本文没有执行 controller self-test，也不以本地 SHA 推断已安装能力；没有查询数据库 schema、有效会员恢复控制状态或所有后台 job。容器环境里的 recovery `paused` 不是持久化控制状态的证明。

原始脱敏证据：[Redis/容器快照](../../.local-releases/rc49-review/redis-live-observation.json)、[Stream 元数据](../../.local-releases/rc49-review/redis-stream-observation.json)。本机忽略目录里的证据不是生产配置。

## 官方两项变更分别处置

| 官方变更 | 对 KKAI 的决定 | 理由 |
| --- | --- | --- |
| #8200，`706144769e5bed5a47b78721e3bb11da3cf4c0b9`，鉴权缓存完整性 | 适配采纳 | 校验缓存身份、权限和绝对有效期，必须保留本地租户及账务机制 |
| #8218，`2de51a19e35a602a0041d380b29be7c9bcd1e0e5`，Cluster hash tags | 当前拓扑不适用 | 单机没有跨槽要求；改 fence/version key 反而割裂旧新节点的撤权边界 |

两项是独立提交，保留原 key 不等于放弃鉴权安全修复。上游和本地使用的都是普通 `redis.Client`，本地已采用 go-redis/v9，保留现有客户端，不降回上游 v8，也不声明新增 Cluster 支持。官方多节点同步升级警告针对其协议变化，不能直接变成 KKAI 必须停机或迁 Redis 的结论。

保留 `user:<id>`、`auth:user:fence:<id>`、`auth:user:version:<id>`、`token:<HMAC>`、`token:fence:<HMAC>`、`token:generation:<HMAC>` 及 Session 原键格式。当前 user/session cache schema 均保留本地值，不能让官方 session schema 1 覆盖本地 2。

## 最小实现范围

1. 用户、Token、Session 一起加入 MAC/绝对有效期，避免只保护一条鉴权路径。复用现有 CryptoSecret，使用用途域分离并绑定完整 key、规范化授权字段和到期时间；不新增秘密、不在同次发布轮换秘密。
2. 明确签入 TenantID、用户/Token 身份、授权状态与版本、分组及 AutoGroups 等安全字段。UserBase/Token 的 TenantID 为 `json:"-"`，不能照搬整个结构序列化而漏签。
3. Quota、RemainQuota、UsedQuota、AccessedTime 等可变计数继续走既有原子额度路径，不纳入鉴权元数据 MAC。该保护不等于整个 Redis 或金额计数器防篡改，SQL 条件扣减与持久化账务仍负责资金正确性。
4. 新节点对缺签、坏签、过期签名不使用该缓存鉴权，回 PostgreSQL 读取可信快照。只签数据库或服务端创建的可信状态，不能给任意 Redis 内容补签；DB 读取失败按现有失败处理，不能回头接受无效缓存。
5. 修复在同一 Lua/CAS 内保持 token generation、mutation fence 和提交后二次失效；用户保留 AuthVersion 下界；Session 保留 tombstone、version 和原观测 deadline。比较旧 CacheMAC 本身不足以识别旧 writer 的修改，不能替代这些门禁。
6. 修复只替换授权 metadata，保留合法实时额度。不能删除整 hash 或用旧 DB 余额覆盖正在预扣/退款的计数。非法、越界或缺少必要额度字段时走明确失败/既有对账路径，不凭“像整数”的正则判合法，也不默默补零、释放额度或重放交易。
7. 审查所有 metadata 写入口：`updateUserSettingCache`、`updateUserNameCache`、`RefreshUserGroupCache` 等局部写入改用可信完整 metadata 更新/重签，保持版本检查与重读收敛。额度专用更新继续独立。
8. 签名内 `CacheExpiresAt` 是绝对边界。旧 writer 的 EXPIRE 不能延长它；修复失败也不能无限刷新无效快照 TTL。Session 迟到快照不得重新获得完整观测窗口。
9. 新缓存字段使用 `gorm:"-"`，本项不需要数据库迁移。`AllowUserQuotaQuery` 属于独立的令牌功能/schema 更新，不能强行捆绑为 Redis MAC 的前置迁移。

修复还有三个必须落实的边界：

- **同一版本的字段变化也要识别。** 改组、用户名或设置可能不提升 AuthVersion，旧 writer 也可能不改 CacheMAC。修复不能仅凭 MAC、generation 或 AuthVersion 相等就覆盖这些字段；应核对观察到的相关 metadata 或沿用可信重读收敛。竞争只做有界重读，仍冲突就停止回填、不续租无效缓存。本次请求只有在重新确认数据库状态并通过现有撤权门禁后才可继续，不能无限重试。
- **保留额度要先核对身份。** 缓存的用户、Token、所属用户及 TenantID 必须与可信数据库身份一致，并按各字段实际整数范围与业务规则校验计数器。合法负余额、负 RemainQuota 或 unlimited 语义不能统一改成非负；身份错配、缺字段、越界或非法值不能补零、复制旧余额或重签洗白。走安全的现有回退/对账路径，无法安全完成则拒绝该请求。
- **缓存校验失败不等于用户退出。** 对活动会话的缺签、坏签、签名过期应回源，不能直接转成已撤销或清除客户端登录。继续保留既有 revoking/revoked 墓碑的拒绝和回填阻断语义，不能为提高可用性绕过。数据库或撤权状态暂时无法确认时，返回临时失败并安全拒绝；只有可信证据确认会话失效才按失效处理。有效会话在正常升级中不得因格式变化被踢出。

涉及的现有入口：[用户缓存](../../model/user_cache.go)、[用户授权下界](../../model/user_auth_cache.go)、[Token 缓存](../../model/token_cache.go)、[Session](../../model/user_session.go)、[额度预留](../../model/quota_reserve.go)。实施时顺着全部调用链检查，而不是仅替换这几份官方文件。

## 新旧共存和回退如何工作

旧版写无签缓存，新版回 SQL 读取并重签；新版写带签缓存，旧版忽略新增字段。双方继续使用同一套失效键、AuthVersion 下界、generation 和额度计数，所以不需要双写 namespace。旧版更新 metadata 可能让签名失效，新版回源是可预期的过渡成本，需要观察查询量与延迟。

本地实现已完成，并验证了部分旧协议交互；完整精确相邻版本混跑仍未执行，不能声称发布混跑门禁已通过。完整性防护只覆盖执行新校验的进程，混跑期间旧节点仍不检查 MAC；不能宣称全系统已经获得新防护。旧 standby 如果仍可执行相关读取，也应纳入剩余旧执行路径说明，不要求为升级标记好看而额外发布一次。

混跑从 stage 的私有 serving candidate 就开始，经过至少六分钟灰度，且跨越旧 writer 排空/替换；现行手册允许 image worker 排空最多 24 分钟。回滚时旧读写路径重新启用。因此等待 60 秒 TTL 不能代替双版本验证。

应用回退保留同一 Redis、DB、key、CryptoSecret/SessionSecret 和缓存 schema；不迁回 key、不恢复旧 Redis dump、不删除撤权记录。回退到旧无 MAC 应用会失去新增完整性防护，应作为安全能力回退记录。

## 为什么不能清库

Redis 不承担现金总账，但也不是全是可丢缓存。它还有用户 AuthVersion 下界、Token generation/fence、Session 撤权墓碑、限流/cooldown、风险事件 Stream/pending/dead-letter。清库会抹掉撤权与风控边界，可能丢失尚未落 SQL 的安全事件。

当前 Stream pending/lag 为 0 不构成以后可删的理由。本方案保持 Redis 实例及这些键原样，因此没有风险事件迁移或停生产者的需要。充值、退款、会员恢复记录与缓存补偿 outbox 以 PostgreSQL 持久化状态为准；不因应用缓存升级重放金融事件，也不额外改变会员恢复模式。

## 实施验收与发布条件

先执行改动相关的本地回归，再用**隔离 PostgreSQL + Redis 8.6.3 standalone**运行精确旧版与新版的行为测试，不对生产注入坏缓存或测试付款。以下隔离验收必须全部在生产 stage 之前完成：私有 candidate 与旧应用共享 Redis，尚未公开也会改变旧版读到的缓存，不能把它当作无用户影响的测试沙箱。

| 场景 | 通过标准 |
| --- | --- |
| 双向读写/撤权 | 旧写→新读、新写→旧读，user/token/session 禁用、删除、改组/授权、退出后不复活 |
| 既有登录连续性 | 升级前签发的 Cookie/JWT、refresh token、API token 跨越切换与回退仍按原有效期使用，无升级引起的强制退出或新增 401；撤销记录继续生效 |
| 修复与资金并发 | 缺签/坏签修复同时预扣、充值、退款、修改限额，不覆盖实时余额、不重复结算 |
| 过期与迟到 | 旧 writer 反复延长 TTL 后，签名仍按绝对时间失效；迟到 DB 快照不能越过 generation/floor/tombstone |
| 篡改与绑定 | 跨 key、TenantID、身份、权限、到期时间篡改拒绝；非法/越界额度不被静默接受或重置 |
| 错误回退 | Redis/DB/修复 CAS 失败遵循现有安全语义；无效缓存不因失败无限续租 |
| 在途工作连续性 | 切换前开始的 SSE 能正常结束，不因本次改动截断、重发上游或重复扣费；受影响异步任务经过 writer 交接到达正确终态，原结算/退款规则保持 |
| 相邻版本回退 | 旧版持续服务→candidate 写入/修复→abort→仅旧版继续运行，全程不清缓存仍能正确鉴权和计费；promote 后回退也保持撤权与账务连续性 |
| 回源性能 | 冷缓存、旧 writer 持续改 metadata、集中签名过期三种场景下，PostgreSQL 查询量、连接占用及请求延迟保持在发布前记录的预算内，无新增连接池超时/误鉴权，修复后回源收敛 |

混跑测试覆盖已知 active/writer `a552996db` 和 rollback `fc46400f3` 的相关路径；执行前如生产版本推进，更新对应精确版本。以上版本当前关键缓存文件相同可以减少重复源码审查，但不能把文件相同当作所有集成场景通过。

不运行 Cluster 测试，不设计多数据库路径；本项没有 Redis 服务器版本升级、ACL 重做、容量扩展或实例切换需求。生产普通发布沿既有 Mac 构建、stage、候选验收、10/50/100、writer handoff、promote、即时/延迟状态、错误窗口、回退及清理流程。MAC 改动本身默认不要求停机或额外准备版，真实混跑失败或新增风险才重新决策。

上线观察额外关注新节点缓存回源/数据库查询及延迟、鉴权失败、token 已提交但失效失败、cache_pending 补偿积压、充值后余额可用性。只采集已有聚合或定向脱敏证据，不添加庞大监控系统，不用 INFO 累计错误计数推断本次失败率。

## 最终执行顺序与停止条件

1. **先完成本地实现和独立审查。** 冻结来源、改动路径、签名字段及全部 metadata 写入口；把本方案适配为一组可独立验证的应用改动。Redis MAC 本身不捆绑数据库迁移、金额算法变化或密钥轮换。作为 rc.49 整合的一部分验证，不默认增加一次生产发布。
2. **隔离环境通过上述矩阵，再允许生产 stage。** 使用合成业务数据和精确相邻版本验证共享缓存、回退残留、会话与资金行为。先记录旧版在相同请求组合下的延迟、数据库查询/连接占用及现有超时上限，再为本次明确可接受预算；不能用 Redis 内存充足推断 PostgreSQL 容量，也不能在结果出来后放宽标准。预算和必要证据未建立前，不进入生产 stage。
3. **后续获准发布时重新取得现场证据。** 确认版本、Redis/秘密来源、controller 与回退资产没有漂移；若本次整合同时含 schema 或 TokenKit 改动，仍分别完成它们的门禁，不能由 Redis 方案推导整版已可上线。
4. **从 stage 起观察旧版服务。** 候选只做必要的最小业务验收；不向生产缓存注入坏数据，不做生产压测。除非用户明确接手业务验收，代理按既有发布规则完成验收；身份、账务、混跑和回退安全门禁仍必须满足。
5. **通过正常灰度与排空，再结束发布。** 各档至少 120 秒，保留原 writer/流式请求排空机制；不能为提速强制截断在途工作。执行即时/五分钟延迟状态、日志保护和脱敏错误窗口、回退及清理验收。

阶段决策按可归因的证据执行，不以统一错误率判定：

| 发现的问题 | 处理 |
| --- | --- |
| 隔离验收失败、预算缺失或回退不成立 | 留在本地修复，不 stage |
| 已确认由新缓存实现造成的登录误失效、授权复活、串租户、重复/错扣、流中断或持续回源超预算 | 立即停止晋级，按当前 controller 状态使用官方 abort/rollout rollback/rollback 路径，不临时清库或关闭 MAC 绕过 |
| 身份、版本或秘密来源发生漂移 | 停止，重新核对精确目标 |
| 错误可能相关但证据不足 | 保持当前阶段，做一次聚焦复核；不能报告完成 |
| 独立证据证明属于无关外部或既有故障，且无候选回归 | 按原手册归因记录，不增加无依据等待或重复测试 |

回退是止损手段，不会自动抹去已发生的资金错误。若发现资金异常，停止相关发布推进，保留证据，交由现有账务核对/恢复流程处理；不能重放收费/退款或恢复旧 Redis 快照冒充无损恢复。

本方案的最终承诺是**以登录、请求、账务连续性为可验证的放行条件**。当前已具备本地实现和定向回归证据，尚不能承诺运行时零风险或已经验证线上用户零影响。未通过放行条件时，继续维持现有生产版本。

## 方案评审阶段的验证与边界（实施前历史记录）

- `make newapi-status`：通过，HEALTHY、rollback ready。
- 本机运行的 SSH 只读探针：INFO/容器允许字段/秘密相等布尔比较通过；Stream XLEN/XINFO GROUPS 元数据通过。探针保存在本机忽略目录，没有安装为服务器工具。
- `git diff --name-only a552996db2adb65d9e2074dbfc2e97962d0ef9bd 5eb8a92581df793be4ca108aaa27391bdef68442 -- common/redis.go model/token_cache.go model/user_cache.go model/user_auth_cache.go model/user_session.go model/quota_reserve.go`：无差异；rollback→current 同范围也无差异。
- 本轮只修改方案文档和本机分析产物，未实现 MAC、未运行业务测试、未构建部署；**全套测试跳过**。
- 剩余缺口是实现后的真实双版本验证及未来发布时的新鲜状态。当前容量快照、单次健康或 Stream 无积压不能代替这些证据。
- 最终方案复核仅更新文档、检查本地源码，没有重复查询生产；上一节状态是带时间的历史取证，不作为后续发布的实时快照。

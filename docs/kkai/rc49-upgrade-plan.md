# NewAPI rc.49 升级合并方案

状态：本地分域整合及定向验证已实施。精确目标仍为 rc.49；没有提交、构建生产镜像、部署或生产迁移。最终行为、878 路径/215 提交处置与验证限制见 [实施记录](rc49-integration.md)。以下设计保留决策背景；涉及生产状态的旧观察不作为当前证据。

## 决策

采用 **以官方 rc.41 为内容基线、以当前 KKAI 实现为本地侧、以官方 rc.49 为目标侧的分域三方整合**。完整核对上游增量；按模块形成可审查、可验证的改动。保留 KKAI 的账务、租户、Studio、商城、发票和手动发布契约。

本轮不重写历史，不补造 `merge -s ours` 的祖先关系，不自动新建分支或 worktree。实现仍在确认的应用 checkout 中开展；实施、提交与生产操作分别按后续授权推进。上游目标固定到精确提交，实施中不自动追逐新发布。

“完成升级”指上游增量全部有证据处置，适用功能进入真实链路且通过相应验收。仅更新版本号、能编译、补几个公告功能，都不满足完成标准。KKAI 有意保留的差异单独记录，不能称与官方逐字一致。

## 已确认的基线与差异范围

| 坐标 | 本轮证据 |
| --- | --- |
| 本地实施起点 | `5eb8a92581df793be4ca108aaa27391bdef68442`，`production/kkrich`；最初分析提交为 `a552996db2adb65d9e2074dbfc2e97962d0ef9bd` |
| 官方内容基线 | rc.41：`2035a82aeb5414253a728bd937d4b8f97aa99b9b` |
| 官方目标 | rc.49：`48a6c87f9e7757fdc58e5be9bcb4f8281ad0dfd0` |
| 实际 Git 共同祖先 | `2d8e50bf36e94200b809dfb39e73624ec48b1e23` |
| 上游增量 | 215 个提交；878 个内容变化路径：757 修改、118 新增、3 删除 |
| 初步映射分类 | 487 个路径已有本地分叉、需要语义协调；391 个路径本地仍与旧基线相同或尚不存在，可先试套 |
| 前端源码子集 | 官方 `web/src` 共 300 个变化路径：141 个需协调、159 个仍为基线或尚未新增 |

以上路径计数采用 blob 内容比较，不检测重命名、不计 mode-only 变化，不是实际 Git 冲突数，也不是缺失功能数量。391 个候选仍需依赖和行为审查，不能自动判定安全。

rc.41 的[完整接入记录](../rc41-full-integration.md)和单父整合提交 `581c8c448` 说明已做过语义移植，但没有把官方 rc.41 接入 Git 祖先链。直接 merge/rebase 会重新处理大量已移植历史。

[upstream-baseline.env](upstream-baseline.env) 中的 rc.21 是历史质量比较基线，并承载 Bun 版本 pin；它不是当前功能版本。不能直接改成 rc.49 来掩盖祖先检查、目录布局或质量债务差异。后续独立记录“已整合上游提交、路径映射、保留差异、验收证据”，完成后用 rc.49 作为下一轮内容比较起点。

分析开始时会员恢复文件有未提交改动，随后由其他任务提交为上述本地 SHA。本方案不把它们计为本次改动。实施前再次核对 HEAD 与工作区；若发生变化，只补审新增差异。共享 checkout 的重叠文件不能由多个执行者同时修改。

## 角色意见与取舍

| 角色 | 核心意见 | 采纳方式 |
| --- | --- | --- |
| 上游合并架构 | 需要完整增量台账；本次修复 Git 谱系会扩大到更早历史 | 分域三方整合，保留单父来源记录；不额外做谱系修复 |
| KKAI 后端与数据 | schema、Redis 协议、金额与租户边界决定是否能安全发布 | 三项兼容设计前置，不能到发布时才发现 |
| 前端与 API | 官方 `web/src` 不能覆盖本地 workspace；新导出和定价有真实业务冲突 | 源码映射到 default，前后端按功能成套整合 |
| 发布与验证 | 源码能力不能证明线上可用；本次为 L3 改动 | 分域验证后再申请必要的整体检查，生产准备独立进行 |

不采用“整目录替换后修编译错误”；不采用只挑几项补丁却标记整版完成。普通 cherry-pick 只适用于台账中依赖闭合、路径一致、尚未等效接入的个别提交，不作为主路线。

## 必须先定下的三项兼容设计

### 1. 数据库扩展与旧版回退

rc.41→rc.49 源码新增了本地尚未实现的持久化结构：

| 结构 | 必须确定的语义 |
| --- | --- |
| `tokens.allow_user_quota_query` | 旧令牌关闭；新建令牌由业务代码显式开启；编辑省略时保留，缺字段响应按关闭 |
| `quota_data.cache_read_tokens`、`cache_creation_tokens` | int64；历史缺失数据不能伪称实际已统计；初始化、累加与展示语义一致 |
| `audit_logs` 的 `idx_audit_time_event(created_at,event_id)` | 核对 PostgreSQL 主库/独立日志库归属、索引顺序和执行锁影响 |

这是源码层面需要扩展设计的证据，不是线上缺列的证明。未来执行前必须取得 PostgreSQL 新鲜、只读的数据库结构证据。本次 KKAI 分叉明确采用 PostgreSQL-only，不设计或实现 SQLite/MySQL 兼容路径。

保留 [model/main.go](../../model/main.go) 的 external 模式启动不执行 DDL。新增结构已接入 `pkg/kkaimigrate` 的 PostgreSQL canonical21 + 独立 rc49 回执在线扩展；没有打开 AutoMigrate，也没有改变既有迁移摘要。生产控制器/扩展流程及兼容回退版仍是独立发布准备门禁。`CacheMAC`、`CacheExpiresAt` 是缓存字段，不创建数据库列。

最终两个 profile 均要求 canonical21 与 `rc49-online-v1` 回执；字段和日志索引实际形状都需验证。账本不推进22，保留旧 max21 应用回退；执行授权已取得，仍需精确双版本混跑、扩展与发布验收。本轮尚未执行生产 DDL。

### 2. Redis 新旧节点协议

2026-11-10 的四角色专项评审和只读现场检查确认：生产使用 Redis 8.6.3 standalone，`cluster_enabled=0`，三个应用进程共用 DB 0。最佳方案是保留现有实例、key 格式及 KKAI 失效/额度协议，只适配鉴权元数据 MAC。完整依据与实施门禁见 [Redis 专项方案](rc49-redis-plan.md)。

上游 #8200（鉴权缓存 MAC）适配采纳；#8218（Cluster hash-tag 键变更）对当前拓扑不适用，不迁键、不双写、不增加 Cluster 验证。保留本地 generation、提交前后 fence、用户 AuthVersion 下界与 Session 撤权墓碑。使用 Redis 8.6.3 standalone 验证相邻旧/新二进制双向读写、撤权、预扣和退款。

本地 `TenantID` 标记为 `json:"-"`，照搬官方序列化签名会漏签租户身份；租户及其他本地授权字段应显式纳入完整性校验。可变额度继续沿用原子更新通道，防止缓存回填恢复已撤销权限或覆盖已结算余额。

缺签、坏签或绝对签名过期时，新节点回 PostgreSQL 取可信快照；修复只更新授权元数据，保留合法的实时额度，继续原子校验 generation/fence。旧节点延长 Redis TTL 不能延长签名有效期。默认沿原蓝绿流程，不增加准备版或停机窗口；只有实际混跑验证失败才调整方案。新节点校验不等于仍运行的旧节点也具备 MAC 防护，回退到旧二进制会失去新增防护。

最终放行条件已固定在专项方案：隔离新旧版本混跑、candidate abort 后缓存残留、登录会话与在途请求连续性、计费正确性、回源性能预算必须在生产 stage 前验收。私有候选同样会写共享 Redis，不能当作无影响沙箱。没有这些证据，维持现有生产版本；Redis 方案通过也不代替整版 schema 与 TokenKit 验收。

### 3. TokenKit 与收费精度

上游新增独立 `tokenkit` 模块并删除旧 `service/tokenizer.go`。上游长字符段按 1024 字节分块、测试允许一定误差；本地 [精确计数实现](../../tokenkit/tokenizer.go) 与 [BPE 实现](../../third_party/tokenizer/codec/codec.go) 保留精确计数和有界缓存。差异会影响预留额度以及缺失上游 usage 时的回退收费。

已接入 TokenKit API、新模型及图片规则，并将本地精确 OpenAI 计数和缓存收敛进同一实现。保留必要的 tokenizer fork，只有证明等价后才删除；不长期维持两条独立计数链。独立模块的依赖替换必须在主模块和 `GOWORK=off` 场景都生效。

台账将此项标为“适配并保留计费精度”，不能用官方容差用例代替金额契约。真实 usage 优先、显式零、缺失值和未知终态仍须区分。

## 分批实施与交付物

| 批次 | 内容 | 退出条件 |
| --- | --- | --- |
| 0：冻结与对账 | 精确三方来源；878 路径、215 提交按功能关联；补核 rename/mode、已有移植、路径例外 | 每项有负责人；schema/cache/计数设计明确；相关 rc.41 历史缺口可追踪 |
| 1：工具链与模块 | Go 1.26.10 安全更新；Bun 1.4.4；依赖、安全补丁、TokenKit 模块接线 | 生产 Dockerfile、镜像 digest、CI pin、模块依赖与发布版本门禁一致 |
| 2：协议与适配 | RelayKit、DeepSeek、Claude/Gemini、gpt-image-2.6、Vertex、渠道状态、插件新入口 | 显式零、限量、终态、工具历史、错误脱敏、模型与渠道身份保持；新图片格式的处理链有验收 |
| 3：持久化与安全 | 受控扩展源码、新旧缓存、鉴权、令牌新权限、日志导出权限 | external 无隐式 DDL；新旧兼容和隔离测试有证据；生产扩展尚未执行 |
| 4：账务集成 | 缓存计费、重试预扣、任务结算、会员/租户、Studio 固定报价 | 原资金来源、int64 钱包、USD epoch、幂等、退款、恢复 fence 不退化 |
| 5：默认前端联动 | 日志导出、缓存统计、价格确认/删除撤销、渠道/插件/通知 UI、语言资源 | 对应 API/权限真实串联；七种现有语言保留；default 构建契约清晰 |
| 6：整合收口 | 覆盖台账关闭、定向与经批准的整体检查、回退组合验证 | 无 pending；每项适配/例外有理由和证据；形成可发布的精确提交 |

批次按依赖串行收口；互不重叠的领域可并行分析或实施。只在获得提交授权后形成分域提交；每个提交绑定台账条目和实际验证结果。

实际生产入口 [build/kkai-image/Dockerfile](../../build/kkai-image/Dockerfile) 及本地构建脚本已同步 Go 1.26.10、Bun 1.4.4；未仅修改普通 Dockerfile 或 go.mod。新增 tokenkit 后，两条 backend 构建路径都需包含模块清单。保留 KKAI 镜像、FFmpeg、external frontend、安全策略及 schema 工具链。

前端普通源码映射为 `web/src → web/default/src`。依赖合入 `web/default/package.json` 和 `web/package.json` catalog，重算 `web/bun.lock`；不能拿官方单包锁文件覆盖 workspace。官方 `bun check`/Vitest 等工具升级与发布 pin 一并验证，插件脚本相对路径重新检查。classic 不是生产同步目标，不默认扩展为 classic 全量移植或构建。

## 人工整合必须保护的业务边界

| 边界 | 具体要求 |
| --- | --- |
| 钱包、会员、租户 | 保留 int64/BIGINT、预留→结算→退款持久化链、原支付来源、未知用量状态、额度饱和审计和恢复 leader fence；不能照搬官方 int 钱包 |
| Studio 与任务重试 | 冻结单价/数量/分组倍率；每次重试实际提交前预留；图片部分交付、视频异步终态与退款完整对应 |
| 插件顶层 `/v1` | 内层 Gin dispatch 必须传递或重建可信 Host 租户上下文，再鉴权；保留默认拒绝路由、任务所有权、插件身份和渠道能力约束 |
| CSV 导出 | 复用列表的 `billing_source`、credit epoch 归一、租户结算投影和隐私字段隐藏；先按真实 log ID 归一，再转展示 ID；权限/时间范围/并发上限由后端实施 |
| 定价编辑 | 保留 `display_billing_expr`、`image_pricing_policy`、按秒/按次单位、会员分组与 USD 语义；新旧价格确认纳入实际提交数据及原子保存边界 |
| 共享前端 | 保留 Host 站点先决检查、租户所有者路由、Host+SID 请求隔离、换号缓存清理、credit epoch、一次性 proof 不重放 |
| 商城与发票 | 即使文件没有文本冲突，也验证共享用户/付款/钱包链，防止重复付款回调、资金来源和发票占用规则回退 |
| API 契约 | 保留本地 API 3 前端及后端 `[2,3]`、KKAI capabilities；版本号相同不证明新增接口与回退后端兼容 |

关键入口包括 `service/billing.go`、`service/billing_session.go`、`service/quota.go`、`model/user.go`、`model/token.go`、`relay/common/relay_info.go`、`web/default/src/lib/http-client.ts` 与 `web/default/src/routes/__root.tsx`。这些文件不能批量选一侧解决冲突。

## 验证方案与成本边界

整合属于 L3：工具链、依赖、schema、鉴权、全局缓存和账务都有变化。先按领域跑最小有效验证，失败仅重跑失败范围；不为安心重复已通过且输入未变的命令。

| 范围 | 最小有意义的验收 |
| --- | --- |
| RelayKit/TokenKit | 协议终态/显式零/工具/图像；1024 边界、长串、多语言、工具 JSON、增长历史、并发；上游 usage 有/无时预扣与结算 |
| Redis | 真实 Redis 8.6.3 standalone 相邻版本混跑、双向撤销、授权字段篡改、绝对签名过期、充值/退款/限额变更、修复不覆盖预留 |
| schema | PostgreSQL 扩展、重复执行、失败恢复、旧版可运行、external 启动无 DDL |
| 账务 | 钱包/会员/租户成功、失败、重试、未知结果及恢复；Studio 部分交付；商城重复回调与发票占用 |
| 权限/控制台 | 平台管理员、主站用户、租户所有者、租户客户；跨租户插件/导出拒绝、换号、定价保存、支付回跳 |
| 前后端组合 | 旧前端+新后端、新前端+新后端、新前端+回退后端；缺失新能力时通过 capability 阻断或明确先回退前端 |

以下保留原计划候选范围，不代表执行记录；实际精确命令、结果与失败后的缩小重跑见 [验证记录](rc49-verification.json)：

```sh
go test ./model -run 'Test(TokenCache|TaskBillingCacheReconcile|ExternalSchemaStartup)' -count=1 -timeout=90s
go test ./service -run 'Test(TokenCount|ResponsesUsageAccumulator|ApplyResponsesUsage|WalletTrustThreshold)' -count=1 -timeout=90s
go test ./middleware -run 'Test(TenantFeatureGate|PrepareTaskPlugin|TaskPluginEndpoint)' -count=1 -timeout=90s
go test ./common -run '^TestConsoleContract$' -count=1 -timeout=90s
# 工作目录：web/default
bun run test src/lib/auth-query-cache.test.ts src/lib/http-client-credit-epoch.test.ts src/lib/http-client-auth-redirect.test.ts src/routes/__tests__/-root-tenant-bootstrap.test.ts
```

上述用例不覆盖新增 schema、真实 standalone Redis 双版本混跑、新导出、所有计费变化或完整依赖图，不能将其全部通过视为整版验收。新增回归保护业务/安全合同，避免只检验内部实现。

整体前端 typecheck/test/build、主仓库/独立模块的广泛测试及任何耗时未知或预计超过五分钟的命令，在执行前必须给出**精确命令、必要性、预计耗时**并取得明确批准。完整上线后的强制发布门禁仍适用，不因定向测试而减免。

特别注意：`scripts/kkai/check-fork-quality.sh` 即使不带 `--full`，也会创建 worktree、跑全前端和 `go vet ./...`。它不适合作为本轮快捷验证；未经用户明确要求创建 worktree，也不能直接调用。

## 生产交付的条件路径

初次整合讨论未访问生产；后续 Redis 专项已取得时间绑定的只读状态与 Redis 拓扑证据，见专项方案。该快照不代替未来发布前的现场门禁，也不证明 schema 或前端格式兼容。

后续获准发布时，按[全局发布入口](/Users/tokk/Documents/Codex/runbooks/newapi-upgrade-and-deployment.md)从当前确认的同级 infra 执行 `make newapi-status`，取得独立 schema 证据，核对 installed controller pin 与真实前端格式。

若新增 schema 需要扩展，按专用流程单独授权与执行，先具备新 schema 下可运行的回退应用。若缓存协议仍不兼容新旧节点，停止普通灰度，按已评审兼容准备版或维护路径处理。两项都通过后才走常规发布。

正常联合更新优先先发布兼容后端：Mac 本地不可变构建 → stage 精确 metadata → 候选最小业务验收 → 10%/50%/100% 各至少 120 秒 → promote → 即时与五分钟延迟 status → 60 秒日志保护后等长脱敏错误窗口复核。候选使用生产 writer 数据库身份，真实验收也可能写业务数据。

format 2 前端 prepare/activate 在后端事务外进行；新增接口和回退范围均满足后再激活新版前端。兼容后端发布复用现有前端；format 1 则保持精确配对。源代码里的能力声明不等于生产已安装。

发布完成还需回退组合、错误归因和本机/线上受控清理证据。源码合并、生产迁移、应用发布是不同结论，不能用其中一个代替其他步骤。

## 实施结果与剩余发布门禁

- [实施记录](rc49-integration.md)、[逐路径/提交台账](rc49-integration-ledger.json)记录全部 878 个净变化路径和 215 个提交来源；适配、保留和测试布局例外分别写明。
- [后端说明](rc49-backend-integration.md)和[Redis 实施](rc49-redis-implementation.md)记录持久化与缓存边界；[验证记录](rc49-verification.json)保存精确命令与分域结果。
- 本地定向回归、真实 PostgreSQL/Redis 边界验证及前端受影响依赖类型检查已完成；后续整体验证和真实混跑进展见[发布准备记录](rc49-release-readiness.md)，不得把 fixture 浏览器结果当作生产验收。
- 本轮没有新建分支/worktree、提交、推送或操作生产。完成状态是本地实施，不能称已上线或已满足发布门禁。
- 发布前仍需 canonical21 + rc49 扩展的新鲜兼容证据、可运行回退组合、实际日志库证据、精确新旧二进制混跑/性能验证及正式手工发布验收。源码台账不替代这些证据。

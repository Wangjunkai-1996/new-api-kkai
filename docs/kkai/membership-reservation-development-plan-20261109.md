# 会员请求暂占优化开发文档（2026-11-09）

依据：[会员请求暂占优化方案](membership-reservation-optimization-plan-20261109.md)。
适用读者：计费后端、Relay/Sub2API 开发、数据库迁移与发布负责人、测试人员。
设计协作：计费账本、Relay/性能、生产对账三个技术角色分别核对代码后合并。
设计基线：`922db66baa46c07b5f36cdce8191a52d9af519db`；本轮交付源码：`fc46400f3fed2647fafadd974ea1fc8d005d6015`。
状态：**P0/P1 已上线，canonical21 已确认，apply 已启用；相关性能、正常业务和全 scope 账务核对通过。** 本窗口无自然自动退款终态样本，实际证据和限制见[生产交付记录](membership-reservation-production-20261109.md)。 应用发布与业务恢复分别验收，本文不证明线上已返还额度。实现与验证命令见[实施记录](membership-reservation-implementation-20261109.md)及[性能证据](membership-recovery-evidence-20261109/performance-B/README.md)。下文保留完整目标设计；P2/P3 尚未交付，“拟新增”不代表这些阶段已经实现。

## 1. 已确定的实现边界

1. 保留请求期间的预占、冻结报价、资金来源及派发前标记；优先修复结束后的收尾与异常恢复。
2. **正常请求到首个内容输出前不新增 SQL 往返、跨服务查账、同步日志或监控上报，不增加逐 token 工作。** 正常成功结算不查询异常表，不增加 evidence/outbox INSERT，也不增加结算 SQL 往返数。
3. 正常成功已经持久保存的 `ActualQuota / ChargedQuota / ExcessQuota / State / SettledAt / Snapshot` 即为正常账务结果。原方案中的“终态信息随结算落库”落实为复用这些字段，不要求给每笔正常请求存一份新的原始收据。
4. 新增异常恢复旁表和少量后台扫描进度。仅 unknown、结算/释放失败、后台发现的孤儿请求产生恢复记录；大凭证不放进主账本的 `SELECT *` 路径。复用现有后台任务、事务和锁，不引入消息中间件或通用工作流框架。
5. 提前处理可信凭证与 72 小时无证据补偿使用不同的内部入口。无证据补偿保留年龄限制；冲突凭证不能变成“无证据”释放。补偿提交后不向用户追扣。
6. 首期 P2 跨服务自动取证仅开放已验证的 HTTP 同步 Relay 路径。Responses WebSocket/Realtime 的每轮请求与握手是不同身份，需独立的逐轮关联协议；本期保留它们原有结算及兜底行为。原生 Task、Image Studio、Midjourney 保持各自任务恢复流程。
7. P0/P1 可先交付，不依赖 Sub2API 查账接口。P2 未验证协议或性能前保持关闭。P3 仅产出预占金额分析与具体公式修改建议，不同时改变客户端输出上限或收费规则。

“SQL 往返不增加”不等于已经证明没有 CPU、行宽、锁或后台资源争用成本。性能门槛见第 8 节；有可重复回退即停止放量。

## 2. 现有入口与开发落点

以下为相对仓库根目录的现有文件；“拟新增”名称尚无代码，实施时可按最终领域命名微调。

| 落点 | 现有事实 | 开发动作 |
| --- | --- | --- |
| `service/membership_budget.go` | 请求估算；缺省 1024 token 不约束上游 | P0 统计估算/实扣；本期不调低预占、不改变默认输出行为 |
| `service/membership_billing.go` | `membershipBillingSession.Settle/Refund` 驱动结算、释放 | 成功路径原样；失败路径保留本次可信应计额/零费依据并写恢复记录 |
| `service/billing.go`、`relay/request_billing.go` | 区分用量、未知结果及失败退款 | 原 unknown 标记和恢复记录原子保存，不能结果未知后重发 |
| `model/membership_billing.go`、`model/membership_relay_billing.go` | 原来源、令牌、终态与统计事务 | 复用 `settleMembershipRelayBillingTx`、`ReleaseMembershipBillingTx`；不复制资金计算 |
| `model/membership_billing_review.go` | 派发/未知标记、人工审核、system 72h 守卫 | 保留人工权限与 72h 守卫；新增可信证据入口；后台入口传递取消上下文 |
| `model/membership_billing_reconcile.go` | 每轮从头扫描 500，结清 50，仅满 72h | 持久游标、分通道发现异常、读取证据、保留无证据补偿 |
| `service/kkai_runtime_jobs.go` | 每分钟、writer/leader 作业 | 复用 `membership-relay-reconcile`；网络查询编排放 service，model 只负责存取和事务 |
| `service/membership_dispatch.go`、`relay/channel/api_request.go` | 派发前持久标记；上游 ID 进入上下文 | P2 将已签名 attempt 的两个标量字段并入原派发 UPDATE；收到响应头仅更新内存 |
| `relay/channel/internal_attribution.go`、`pkg/kkaiattribution/` | 来源白名单、HMAC、每次签名 nonce | 复用既有可信归属；提供经过本服务签名流程确认的派发元数据，不信客户端自报头 |
| `model/log.go` | 可关闭、可独立部署/清理的消费日志 | 保留为补充证据，不作为唯一可靠队列或零费证明 |
| 拟新增 `model/membership_billing_recovery.go` | 无 | 异常证据、后台游标、可信终态/补偿事务 |
| 拟新增 `service/membership_billing_recovery.go` | 无 | 取证、冻结报价重算、退避及有界作业编排 |
| `pkg/kkaimigrate/`、`cmd/kkai-migrate/` | 显式迁移与运行契约 | 新增扩展版本及校验，不改已应用的历史迁移 |

## 3. 数据设计与迁移

### 3.1 P0/P1 异常恢复表（拟新增）

表名：`kkai_membership_billing_recovery`，与主账本位于同一主库。

| 字段 | 类型/约束 | 含义 |
| --- | --- | --- |
| `request_id` | VARCHAR(128)，主键 | 原账本 ID，一笔异常最多一行 |
| `state` | VARCHAR(16) | `pending / ready / conflict / cache_pending / resolved`，不能代替资金终态 |
| `evidence_kind` | VARCHAR(24) | `unknown / final_usage / confirmed_zero` |
| `evidence_payload` | TEXT | 版本化、规范化最小证据，应用限制 16 KiB；不存提示词、响应正文或密钥 |
| `evidence_hash` | VARCHAR(64) | 规范化证据摘要，相同重放幂等 |
| `receipt_key` | VARCHAR(191)，可空、唯一 | 受信来源命名空间与最终收据 ID 的稳定摘要，防止同一收据跨请求重复使用；unknown 为 NULL |
| `next_attempt_at`、`attempt_count` | BIGINT、整数 | 调度与退避；不改变账本 `review_after` 的派发截止含义 |
| `last_checked_at`、`last_error_code` | BIGINT、VARCHAR(64) | 低基数错误类别，不保存原始网关错误体 |
| `revision` | BIGINT | 条件更新版本，阻止迟到查询结果覆盖新证据或人工结案 |
| `created_at / updated_at / resolved_at` | BIGINT | 生命周期；终态记录保留审计，不自动删除 |

调度索引：`(state, next_attempt_at, request_id)`。凭证中的身份须完整匹配原账本，不能仅凭 request ID 或 hash 相同信任。`receipt_key` 的上游唯一性范围包含冻结的来源身份；不用 request ID 掩盖同一上游收据被绑定两次的问题。数据库实现遵循项目的三种方言规则，使用 TEXT JSON 和 `common.Marshal/Unmarshal`。

证据 payload v1 的逻辑字段：

```text
version, origin(local_terminal/local_consume_log/sub2api), kind,
request_id, user_id, token_id, channel_id,
source, account_id, snapshot_hash,
dispatch_attempt_id?, receipt_source?, upstream_request_id?, receipt_id?,
finalized_at, observed_at, usage_schema?, normalized_usage?,
assessed_quota, billable_usage, zero_cost_basis?
```

`assessed_quota` 来自已冻结的 NewAPI 计价结果；对于外部收据须先完成同口径重算，不能直接填 Sub2API 扣款金额。零额的 `final_usage` 仍可能有真实调用并计入请求数，不能只凭金额为零改成 `confirmed_zero`。未知证据不携带伪造的零额度，不能覆盖可信终态；同一凭证重放不重复写钱，不同终态凭证冲突时保留第一份证据与有界冲突摘要、停止自动结案，不覆盖原证据。原始多份大响应不塞进该表。

`evidence_hash` 只覆盖稳定账务语义：来源、request/attempt、收据 ID、冻结快照、终态、规范用量/应计额、billable_usage 与零费依据；排除 observed_at、last_checked_at、查询耗时等采集信息。规范化使用固定字段顺序与数值表示，不能对原始 JSON 字节直接哈希。同一收据在不同时间查询仍应幂等，同一收据 ID 的账务内容变化才是冲突。

hash 不同不自动等于账务冲突：local_terminal、消费日志与 Sub2API 可能是同一终态的不同证明。须按请求/attempt、冻结报价、完整用量、应计额及本地费用依据比较语义；一致时补充有界证据引用，不二次结算，不覆盖原证据。只有金额相同而用量或归属不同也不能自动视为一致。

`confirmed_zero` 必须证明**按冻结 NewAPI 报价及本地费用规则的总应计额为零**。Sub2API 未收费只证明上游事实；本站可能仍有按次费或违规费，见 `service/violation_fee.go`。存在这类费用时走最终应计额结算；缺少触发事实时保持 unknown，或不开放该定价模式的 P2，不能直接全额退款。

### 3.2 P0/P1 后台进度表（拟新增）

表名：`kkai_membership_billing_scan_progress`，仅少量 lane 行。

`lane VARCHAR(32) PRIMARY KEY`；`cursor_at BIGINT`、`cursor_request_id VARCHAR(128)`；
`sweep_upper_at BIGINT`、`sweep_upper_request_id VARCHAR(128)`；`revision BIGINT`、`updated_at BIGINT`。

时间戳为 0、空 ID 仅表示尚未开始。每次 sweep 冻结候选上界，以 `(时间, request_id)` keyset 推进；扫描完成回绕并刷新上界。用 revision CAS 保存进度；失败允许重扫，不允许跳过未处理记录。

### 3.3 P2 派发身份扩展（拟新增，独立交付）

只给主账本增加两个小标量，不增加前置查询、证据正文或新索引：

- `dispatch_attempt_id VARCHAR(32)`：实际发送的 `X-NewAPI-Attribution-Nonce`，默认空。
- `dispatch_receipt_source VARCHAR(64)`：启动配置中受信收据来源的稳定逻辑 ID，默认空。

两者随 `MarkMembershipBillingDispatched` **已有 UPDATE** 保存；仅已通过本服务签名路径且支持取证的 HTTP 请求填写。来源映射在启动时验证并保存在内存，不能在派发路径临时联网/查库解析。来源 ID 不得在配置更新后改指另一个收据命名空间；旧映射及凭据轮换的查询能力保留至相关暂占和审计窗口结束。配置失效只关闭该来源的新增提前取证，不能阻断普通 Relay。

清理“已确认无费用”的失败尝试时，在既有清理 UPDATE 中同时清空这两个字段；下一次尝试写新 nonce。unknown 禁止清理并重发。旧记录、未签名来源和不支持协议的路径字段为空，不反填猜测值，不能自动套用 P2。

### 3.4 扩展与回退要求

两张旁表和 P2 两列均属于显式 schema 扩展；P0/P1 也不是零迁移。版本号在开发时从实际迁移目录分配，不在本文预定或复用历史版本。P2 两列的空默认值需保证旧兼容写入正常；新模型不能在字段未安装时隐式读写它们。

运行时启动/worker 能力检查须核对 canonical 回执、表、列及索引；现有 `CheckMembershipSchema` 只检查表存在，不足以证明新能力。检查结果按 schema 生命周期复用，不把额外 schema 检查放到每次请求。仅访问旁表的 D1/D2 代码可通过能力门禁在未安装时保留旧路径；包含 P2 主模型持久字段的二进制必须在其 schema 合约满足后才能运行，不能靠关开关掩盖缺列。禁止运行时 AutoMigrate。

现有显式索引为 `(state, created_at)`、`(billing_scope, state, review_after)`，依据 `pkg/kkaimigrate/schema_membership.go`，不能只看 GORM 标签。先验证执行计划，确有必要才添加包含 request ID 的覆盖索引；新增索引也会增加普通写入成本，必须纳入性能对照。

## 4. 账务状态与事务边界

### 4.1 分类与动作

| 事实 | 资金动作 | 恢复处理 |
| --- | --- | --- |
| 正常完成，可信最终用量 | 现有事务按冻结报价结算，返还差额 | 成功不建恢复行 |
| 确认未派发或协议确认零费用，且本地总应计额为零 | 原来源释放 | 成功不建恢复行；失败持久化零费及本地费用依据 |
| 可信部分用量且已符合现有终态判断 | 依现有规则结算 | 不能把截断流中的估算当最终用量 |
| 已发送，断线/缺 usage/结果未知 | 保留预占 | `pending + unknown`；不得自动重发 |
| 有可信结果但结算失败 | 保留或重读已有终态 | 持久化 `ready` 证据后后台重试 |
| 满 72h，符合既有资格，既无冲突也无可归属证据 | 全额平台补偿 | 原因明确标 `no_evidence_compensation`，不称“确认零费用” |
| 凭证身份或金额冲突 | 保留待审 | `conflict`，不挡住其他请求 |
| 已 settled/released 后再到凭证 | 不再调整资金 | 同结果幂等；不一致/迟到仅审计和平台成本归集 |

### 4.2 最小内部函数契约（拟新增）

```go
RecordMembershipRelayEvidence(ctx context.Context, input MembershipRelayEvidenceInput) error
ResolveMembershipRelayEvidence(ctx context.Context, requestID string) (*MembershipBillingRecord, error)
CompensateExpiredMembershipRelay(ctx context.Context, requestID string) (*MembershipBillingRecord, error)
```

这些是内部 model/service 边界，不新增公开路由，不允许客户端提交 `verified=true` 或自由指定应扣金额。证据输入只由可信本地收尾或经过认证/校验的取证适配器构造；model 事务内仍重验账本身份、快照摘要、attempt 和金额边界。

`ResolveMembershipRelayEvidence` 在锁内强制 `Scope == relay`，只消费已持久化且符合终结依据的可信终态，允许不足 72h；它不能仅凭 ready 状态或空证据释放。已派发证据须匹配当前 attempt；P1 本地证据依据现有终结与禁止未知重试约束校验，P2 强制 nonce 匹配。未派发孤儿须同时满足 1h 门槛和禁止再派发证明。`CompensateExpiredMembershipRelay` 强制 relay scope、72h 年龄、现有复核资格以及无冲突条件。现有 `resolveMembershipBillingReview(..., true)` 的 72h 守卫保留；早期入口不得改成 `system=false`、伪造管理员或传自由 Action 来绕过。已有人工入口继续原认证授权。

### 4.3 异常证据与资金使用两个短事务

```text
请求异常或后台取到凭证
  → A：重新核对原账本，保存证据/恢复状态，提交
  → B：重读资金终态与已保存证据，结算/释放/补偿，提交
  → 提交后刷新缓存；失败只重试缓存，不重做资金调整
```

- 前台异常 A 与 B 的业务锁序为 `User → Ledger → recovery`，使用 `lockForUpdate` 和 `MembershipTransaction(ctx, ...)`。后台事务先取得 leader 租约行锁或项目既有等价事务 fence，再进入相同业务锁序；以数据库时间验证 holder/fence/有效期，并持有 ownership 保护至提交，不能仅在事务外检查一次。SQLite 使用项目可验证的条件事务等价实现。短事务期限须小于剩余租约，提交前再次验证；正常前台不增加租约锁。既有账户/令牌子锁保持原顺序。checkpoint 独立短事务同样保护 ownership，再 CAS 进度，不让正常请求等待后台游标锁。仅修改重试调度的事务使用 `Lease → Ledger → recovery`，不取得用户、账户或令牌锁；它不得转入资金处理，仍在锁内检查账本终态与 recovery revision。
- 必须把 A/B 分开：若证据与首次失败的资金事务同进同退，结算失败仍会丢失凭证。A 只记录事实，不扣款；B 内资金、统计、账本审计和 recovery `cache_pending` 原子提交；缓存确认后才标 recovery resolved。主账本的 settled/released 始终是资金终态。
- B 复用现有 `settleMembershipRelayBillingTx` / `ReleaseMembershipBillingTx`。`charged=min(assessed,reserved)`、平台差额、冻结价格、原会员账户/钱包/令牌归属均保持。
- unknown 标记与恢复行在现有异常事务内保存。即使先前已标记 unknown，也应幂等补建缺失的恢复行，不能被现有提前 return 漏掉；异常保存使用有界、脱离已取消客户端请求但受进程停止控制的上下文。
- 正常结算与后台同时到达时锁内重读主账本。数据库返回提交结果未知时也先重读，不凭调用方错误重复处理。已经完成的结果不被旧 worker 或人工重放覆盖。
- 72h 补偿也必须在锁内查看 recovery：可信终态优先结算，冲突保留；只有无证据才补偿。日志库与主库没有分布式原子快照，按主库终态提交先后裁决。补偿先提交，迟到日志/收据不追扣。
- 原会员已到期或已重购仍调整原账户，不转入新套餐或延长有效期；这是原路账务恢复，不等于给已到期权益新增可用时间。原令牌删除或管理员重置后不阻塞账户返还，继续记录现有未应用令牌返还字段。

缓存恢复仅作用于异常行：B 提交后执行既有用户/令牌缓存失效，成功则把 recovery 标 resolved；失败沿用 next_attempt_at 重试，或进程在提交后崩溃也由 `cache_pending` 继续。该分支只重读主账本并刷新缓存，绝不再调用资金结算；令牌确已删除视为无需刷新，数据库查询错误不能当删除。现有 `MembershipBillingAfterCommit` 不返回错误，实现时以最小内部结果接口复用其失效逻辑，保留正常成功路径的 SQL/缓存调用数量，不给正常请求建恢复行。后台观察到正常收尾已抢先完成时，也只做缓存确认后结束异常行。

### 4.4 明确无法消除的窗口

正常路径不新增证据写入，因此“上游给出最终用量 → 现有结算提交前进程崩溃”，或主库完全不可用时，内存用量仍可能丢失。恢复依据已持久派发标记、上游收据或已有日志；仍无证据则按 72h 政策补偿。不能承诺所有原始用量永不丢失，也不能用 goroutine/内存队列充当持久保障。

## 5. 有界后台核账

### 5.1 候选通道

| lane | 候选与排序 | 作用 |
| --- | --- | --- |
| `aged` | relay、reserved、`created_at<=now-72h`，保留现有复核资格；按 created_at/request_id | 独立执行既有补偿兜底，不受 Sub2API 查询排队影响 |
| `dispatched_orphan` | relay、reserved、`review_after>0 && review_after<=now`；按 review_after/request_id | 找到进程崩溃/异常标记失败的派发记录，不遍历未到期长请求 |
| `undispatched_orphan` | relay、reserved、dispatched_at=0、创建已满现有 1h 门槛；按 created_at/request_id | 锁内确认尚未派发；与派发入口禁止老授权发送的规则共同保证安全 |
| recovery 到期行 | pending/ready/cache_pending/conflict 分别按 next_attempt_at/request_id 取小批 | 实际已停止的传输直接入列，早于派发 deadline 也可取证；cache_pending 仅刷新缓存，conflict 单独预算 |

孤儿扫描只负责发现尚无 recovery 记录的账本并持久入列；已有记录由到期队列按退避处理，不能重新入列绕过冲突退避。aged 通道仍读取已有证据。`undispatched_orphan` 只有在锁内重读 dispatched_at=0、确认达到既有禁止再派发年龄，并确认本地费用规则也不收费后，才可形成本地零费证据；本地费用事实不全则仍未知。“派发已标记但查不到上游记录”仍是 unknown。Realtime/WS 没有逐轮远端身份时不进入 P2 查账，孤儿发现本身不授予额外退款权限。

### 5.2 扫描算法与公平性

1. 每个 lane 在 sweep 开始时保存固定的候选上界；使用现有索引分页，不使用 OFFSET。
2. 页内每项必须已处理、已确认终态，或已持久入列/延期，才能推进到该项。**不得把刚抓取的一整页末尾直接保存为进度**：达到 50 笔结清上限时，页内剩余项还没处理。
3. 保存进度采用 revision CAS，并验证本轮 leader ownership；进程在处理后、保存游标前崩溃只造成重扫。上界以内无更多候选时回绕。新增/更新到已扫区间的记录在下一 sweep 发现，不永久漏扫。
4. 每轮限制扫描量与耗时，各 lane 轮转，给 aged 保留预算；不会为扫清历史而无界循环。冲突先持久保存分类和退避，再继续后续项。
5. 拟初始退避 1、2、4、8、15 分钟后封顶；遇到新可信证据立即 ready，不等待旧退避。72h aged 通道独立检查，不被取证 backoff 延后；冲突不是自动补偿对象。

### 5.3 初始执行预算与取消

以下是开发初值，需隔离负载验收后确定，不能当成现网吞吐承诺：

- 继续每分钟执行，单执行器；每页至多 100、单轮总扫描至多 500、资金终态调整至多 50、整轮上限 20 秒。
- 先为已派发孤儿、未派发孤儿各执行一次至多 100 行/1 秒的发现，冲突队列仅对已终结主账本收尾至多 10 行/1 秒，未解决冲突不反复写延期；三个步骤轮换起始顺序，不在此阶段动钱。随后 aged 最多 100 行/25 笔调整/6 秒，再处理 ready/cache_pending/pending 队列合计最多 100 行/25 笔资金调整/6 秒，三类各保留轮转机会。每个阶段有独立子上下文，前一阶段不能耗尽下一阶段预算。
- 两个资金阶段都获得一次执行机会后，空闲的笔数/时间预算才可互借，仍守总上限；持续 ready 压力不能借走尚未执行的 aged 份额。查询失败或租约失效停止相应写入，不为保证处理数绕过限制。
- 最后才进行远端取证，并发 1、每轮至多 2 次、单次超时 2 秒，总远端预算最多 4 秒且不超过整轮剩余时间；从已经选出的 pending 中取，不另扫一批。取到证据先持久保存，下轮资金阶段处理；远端慢查不能占用前述份额。
- 资金事务逐条进行，取证网络调用全部在事务外；查询/锁等待设置已验证的方言适配超时。不得通过无限增大连接池或 goroutine 数量追赶积压。
- 单 job 当前不会并发重入；但 leader 切换/重复执行仍靠数据库终态保证幂等。后台入口用传入 ctx，不用现有 `membershipTransaction` 的 `context.Background()` 包装。
- 采用已有 leader holder/fence 语义：后台写事务及 checkpoint 提交重验 ownership，租约丢失立即取消。参考 `service/shop_jobs.go` 的边界，不把商城业务对象挪作会员数据；新检查不进入前台事务。
- DB 超时、连接池/锁压力高时减少本轮工作并退避，告警积压；前台请求不等后台腾出工作槽。异步不代表无资源开销，必须做第 8 节后台开/关/积压对照。

日志查询失败不能当作空结果：保留现行故障时不做无证据补偿的行为并告警；有已保存可信证据的独立处理不应被日志库故障阻断。新增 Sub2API 不可用则退避，不能额外阻塞原先可执行的 72h 补偿。依赖持续不可用时如需扩大补偿政策，另行提出，不能静默改变规则。

## 6. Sub2API 取证契约与 Relay 关联

此节为**待双方实现与验证的协议**。实施时已只读检查本机登记的 Sub2API 源码：存在签名归属验签，但 nonce 未关联到最终计费记录，也未找到按 attempt 查询最终收据的专用接口；未核对线上接口。P2 仍关闭，不假定已经存在某个 URL，详细证据见实施记录。

### 6.1 派发身份

现有签名归属头包含 NewAPI request ID、user/token/channel/multi-key/model/source 和签名 nonce。nonce 可以标识一次 HTTP 派发，但不代表上游支持幂等执行。

P2 在签名完成后将 attempt 与受信来源以内存元数据传到 `PrepareMembershipDispatch`，与既有 deadline 一起提交；在 `relayClient.Do` 前完成。`doRequest` 不能只读取用户可影响的同名 header 就认定可信。签名入口必须明确返回/保存“本次由本服务签名且来源匹配”的结果；未经过该入口的 `DoRequest` 调用仍正常转发，但没有远端自动核账资格。

最小实现复用 `Signer.ApplyRequest` 已有的 applied 返回值：`applyInternalAttributionHeaders` 仅在 `applied && err == nil` 时，创建 channel 包私有的 request-context 标记，保存 nonce、签名请求/渠道/目标与来源 ID。`doRequest` 核对标记和实际请求一致后才传派发信息；不增加签名往返，不信任裸 header，也不让同一个 RelayInfo 上一次尝试的标记流入下一次请求。

同 request ID 可以经历多次明确零费的重试，因此 request ID + channel ID 不足以区分同渠道两次尝试。新 attempt 只能在旧尝试确认零费并完成清理后替换；上游返回 ID 只是辅助信息。HTTP2 透明重试、签名 nonce 重放及上游内部多次尝试必须在协议验收中明确，不能猜测累加多条账单。

### 6.2 查询与应答

查询键：`receipt_source + request_id + dispatch_attempt_id`。接口使用服务认证和受信来源路由；响应绑定该身份，可审计验证，不使用客户端提供的 URL，不跟随跨源重定向。请求超时 2 秒，响应体开发上限 64 KiB，解析后规范化证据至多 16 KiB；不输出秘密或原请求内容。

| 字段/状态 | 契约 |
| --- | --- |
| 归属 | 原签名来源、request/attempt/user/token/channel/model；与冻结账本及签名语义匹配。若某用量归属需要 multi-key，须在协议中证明该维度或增加同 UPDATE 的小字段，不能猜测 |
| `pending` | 上游仍未终结，保留暂占 |
| `not_found` | 查不到，不证明没有收费 |
| `final_usage` | 唯一最终收据、不可再变化的完整用量及 usage schema；可以按原 NewAPI 报价重算 |
| `confirmed_zero` | 上游明确最终未收费及机器可判定依据，不是仅返回金额 0 或 HTTP 4xx/5xx；映射到本地释放前还须确认 NewAPI 本地费用也为零 |
| 最终收据 | receipt ID、finalized_at、可选上游 ID；与上游最终计费事实持久一致，重查稳定 |
| 内部多次尝试 | Sub2API 给出该 ingress attempt 的权威最终结果；多个冲突/未终结收费记录不由 NewAPI 擅自合并 |

价格快照中的会员折扣、缓存、工具、音频、表达式、违规费用和单位必须得到相同口径的处理。P2 按已经验证的 `usage_schema + 定价模式` 白名单逐类启用；缺字段、未知版本或不能复算的模式保持 unknown，不能用上游美元金额代替用户应扣额度。

冻结报价计算优先复用现有纯计算函数；不得调用带 Gin/写日志/扣款副作用的完整 `PostTextConsumeQuota` 来后台重放。若确需抽出纯计算函数，必须同时让现有路径和恢复路径共用，并单独验证计费与性能等价；涉及表达式先读 `pkg/billingexpr/expr.md`。本地已可信计算且保存的应计额不依赖当前全局价格重新计算。

### 6.3 P2 上线前必须补齐的证据

- Sub2API 的精确接口与认证约定、来源映射、按 nonce 查询能力，以及最终计费与收据持久化的一致性。
- 无响应头、同渠道重试、内部尝试及重启后查询的行为；WS/Realtime 逐轮协议另列，不能宣称 HTTP 方案覆盖。
- 终态凭证覆盖率、延迟分布、保留期；保留期至少覆盖 72h 补偿和最长恢复/审计需求，具体天数形成配置与运维约定后再启用。
- 取证响应与 NewAPI 冻结报价的精确对照样例，包括零费用和真实用量为零两种情况。

以上依赖只阻止 P2 自动处理，不阻止 P0/P1 开发或现有正常请求。

## 7. 分工与可验收交付

| 工作包 | 技术负责人 | 交付物与前置条件 | 完成判据 |
| --- | --- | --- | --- |
| D0 基线 | SRE + 计费 | 只读统计定义、当前链路 SQL/首内容基线；不足 7 天或缺日志明确样本覆盖 | 正常/异常、资金来源、scope 分开；零实扣样本不参与除法，单列 |
| D1 schema | 计费 + 数据库 | recovery/checkpoint 扩展、运行能力检查、迁移及回退兼容；不改历史 migration | 三方言隔离验证、正常路径 SQL 数保持、旁表尚未启用也可运行 |
| D2 本地恢复 | 计费 + Relay | 异常持久化、证据入口、保留 72h 补偿、原路退款 | 证据提交后结算失败可恢复；正常成功不访问旁表 |
| D3 调度 | SRE + 后端 | 公平扫描、游标、退避、ctx/fence、限额与指标 | 500 个冲突不阻塞第 501 笔；超时/换 leader 可恢复 |
| D4 跨服务 | Relay + Sub2API | 单独身份列扩展、可信签名元数据、认证查询契约、冻结价重算白名单 | 第 6 节验收完成后按来源启用；不支持的路径保持原行为 |
| D5 金额优化 | 计费 | 7 天有效样本分析和逐模型公式建议 | 明确节省占用与平台超额变化；单独审查实现，不捆绑 D2/D3 |
| D6 交付验证 | 测试 + SRE | 账务回归、首字对照、迁移兼容、灰度/回退记录 | 第 8/9 节通过，不能以“异步了”代替性能证据 |

代码按 D1 → D2/D3 → D4 分批集成；D0 与设计准备可并行。文档协作角色不等于已经分配真实团队人员或承诺工期。新增配置保持最少：拟定 recovery 模式 `off/observe/apply/paused` 与受信 receipt source 白名单，默认 off；精确定义见第 9 节。初始固定预算用有名常量，确有现场调节需要才增加配置。

## 8. 测试与性能门槛

### 8.1 账务及恢复用例

| 用例 | 结果 |
| --- | --- |
| 不足 72h + 可信最终费用/明确零费 | 可提前处理；不足 72h + 无证据绝不补偿 |
| 证据 A 提交，资金 B 回滚/进程退出 | 重启从 ready 恢复；资金、统计只生效一次 |
| B 已提交，缓存失效报错或进程立即退出 | cache_pending 只重试缓存；余额和统计不再次调整 |
| unknown 标记成功、旧版本没有 recovery 行 | 新扫描幂等补建；同一恢复不能覆盖已有终态 |
| 两 worker、正常收尾、人工操作竞争 | 锁内重读；重复不动钱，不串资金来源 |
| 证据与补偿两种提交顺序 | 证据先提交则按证据；补偿先提交则迟到审计，不追扣 |
| 不同用户/令牌/attempt/来源或复用 receipt | 拒绝自动结案；身份冲突可追踪 |
| 同收据重查但 observed_at 改变；同 ID 账务内容变化 | 前者幂等，后者冲突；采集时间不参与证据 hash |
| 不同证据来源确认同一账务结果；上游零费但本站有违规费 | 前者语义核对后幂等；后者按冻结本地费用结算，不全额释放 |
| 原会员到期/重购、令牌删除或额度重置 | 原账户处理；不转新账户，令牌异常不阻塞资金返还 |
| 最老 500 条冲突，第 501 条可处理 | 跨轮继续推进；冲突退避且不会永久饥饿 |
| ready 持续满载，aged/孤儿/冲突队列同时有积压 | 保留各阶段执行机会，aged 在持续压力下仍推进 |
| 同 timestamp、页中到 50 条上限、重启、持续新增 | 游标不跳过未处理项，固定上界正确回绕 |
| lease 失效或游标 revision 冲突 | 停止旧执行者写入；重扫不重复终态 |
| 日志查询报错 vs 成功空结果 | 前者故障，后者才适用既有无证据政策 |
| Sub2API 慢/不可用/不支持、超大或畸形用量 | 有界退避；不阻断正常请求或已有可执行的 72h 通道 |
| 未到 deadline 的长请求、非 relay scope | 不按年龄提前释放 |
| 同渠道重试、无响应头、WS 多轮 | HTTP attempt 归属准确；未覆盖的逐轮协议拒绝 P2 自动匹配 |

测试采用显式夹具和可控时间/依赖，不用随机循环、长 sleep 或伪性能单测。新增 Go 测试遵循 require/assert；沿用既有测试文件，只有新的稳定边界才新增文件。

已存在的主要回归文件：`model/membership_billing_reconcile_test.go`、`model/membership_billing_review_test.go`、`model/membership_relay_billing_test.go`、`model/membership_additional_test.go`、`service/membership_lifecycle_test.go`、`service/membership_zero_usage_test.go`、`relay/channel/api_request_membership_test.go`、`relay/channel/internal_attribution_test.go`。按改动补充 migration 与 worker 测试。

以下命令是原设计中的定向入口，不作为已执行回执；实际执行的命令与结果见实施记录及第 8.4 节证据。本次文档收尾未重跑测试。后续使用前先核对测试名称和编译范围；冷编译时间未知或预计超过 5 分钟须依仓库规则先说明并获准，不自动改跑全仓。

```sh
go test ./model -run '^TestMembership(RelayReconcile|BillingReview|RelayStatistics|TokenRefund|UnlimitedToken|FiniteToken)' -count=1 -timeout=90s
go test ./service -run '^Test(Membership(Failed|Accepted|Truncated|Responses|Dispatch)|BackgroundJobRegistry|GenerateTextOtherInfo)' -count=1 -timeout=90s
go test ./relay/channel -run '^Test(MembershipTransport|DoRequestNegotiatesSub2TTFT)' -count=1 -timeout=90s
go test ./relay/helper -run '^TestStreamScannerHandler_Sub2TTFT' -count=1 -timeout=90s
go test ./pkg/kkaiattribution -count=1 -timeout=90s
```

新增 recovery/迁移测试的精确命令在实现后补入验证记录，不能只跑旧正则而漏掉新行为；SQLite 单测不代表 MySQL 5.7/PostgreSQL 真实锁及迁移已验证。迁移必须补三方言定向测试和实际查询执行计划。计费估算未改时不重复无关全套。

### 8.2 首字指标定义

| 指标 | 定义与用途 |
| --- | --- |
| 客户端首内容时间（主门槛） | 客户端发起请求至首个实际可见内容事件，不把 ping、role-only、usage-only、response.created 当首字 |
| 派发前耗时 | 入口到实际 relayClient.Do，用于排除新增预处理等待 |
| 派发后首内容 | 发起上游调用至首个实际内容；辅助定位上游与转发耗时 |
| 首字节/首 SSE | 各自单列，不能替代首内容；工具参数和 reasoning 首输出按协议另列 |
| 非流式 | 首字节与完整响应耗时，不伪称流式 token TTFT |
| 结算与争用 | SQL 往返数、事务/锁等待、连接池等待、CPU/内存/GC、吞吐及总耗时 |

现有 `stream_scanner.go` 在首个非空 data 事件记录 FirstResponseTime，`first_sse_ms` 可能早于真实内容；`frt` 优先 Sub2 sideband，否则取响应头。因此本次验收由测试客户端按协议解析实际内容，保留原指标定义，不为新测量给生产流新增逐 token 处理。

### 8.3 对照与放量判据

1. 固定基线/候选提交、模拟上游时序/输出、数据规模、连接池、机器资源、请求体和并发。预热后交替运行基线与候选；覆盖会员/钱包、有限/无限令牌、流式/非流式、同账户竞争及多账户负载。
2. 分别在后台关闭、正常核账、500+ 冲突积压、上游查账超时、数据库争用场景比较。测试客户端验证首个内容晚于首 SSE 的夹具确实被正确测量。
3. 每档输出样本数、成功/错误数、P50/P95/P99、吞吐和资源数据；缺样本不报 P99 通过。用基线重复运行建立噪声范围，固定测试方法后再评候选，不能事后扩大噪声范围放过回退。
4. 同分支正常路径 SQL/外部调用数不得增加，原输出/flush 顺序不得改变；任何超出基线波动且可重复的首内容、完成耗时或吞吐回退阻止放量。预算参数不是可以接受固定 1%/5% 退化的授权。
5. 性能工具先使用已有可用工具/最小隔离测试客户端；不要给全仓加泛化性能框架。长时间压测先给出精确命令、环境、预计时长并按仓库规则执行。生产不注入故障或创建付费压测流量来补验收。

### 8.4 本轮性能结论与交付状态

完整原始样本、精确命令、源码/测试二进制哈希及失败边界见 [B 性能证据](membership-recovery-evidence-20261109/performance-B/README.md)。冻结基线未因候选结果而放宽，三次真实源码修订分别记账：

| 源码 | 实际覆盖 | 结论 |
| --- | --- | --- |
| `cc99ff134` | 32 场景两次，共 64 格、12,800 次候选请求 | 账务全部正确；保留一个重复主指标和两个辅助 P99 越界，不作无条件性能通过。 |
| `c6fc071f8` | 去除多余租约写入后，4 格压力加 8 格生产节拍对照，共 2,400 次请求 | 功能通过；同窗 501 积压仍有完成 P95 约 +15%、吞吐约 −9%，继续修复，未以无冻结 flag 代替判断。 |
| `fc46400f3` | 同样 12 格、2,400 次相关跟进，其中候选 1,600 次、历史对照 800 次 | SQL 错误、未结算、连接池等待为零；积压额外 SQL 203→4，原明显争用消除，支持受控业务验收。 |

最终修复停止对未变化 reserved 冲突反复延期，aged 页的恢复状态一次批量读取；真实资金操作继续在事务内重验，25+25 笔资金预算不变。501 生产节拍单轮 worker 从 150–197ms 降至 44–47ms。最终生产同窗首内容 P50 中位约 +0.092ms、完成 P95 约 +0.435ms、吞吐约 −1.87%，差值如实保留；不能承诺数学意义的零开销。

每格 200 次请求、并发 4、连接池 8。压力模式每 100ms 尝试启动真实 worker；生产节拍对照为启动时执行一轮、后续每 60 秒一轮，B 的真实控制 poll 均独立每秒运行。短请求窗口覆盖启动轮，不能把此窗口的差异外推为每分钟持续损失。P99 样本有限，仅作辅助。真实鉴权、渠道分发、输入分词、Redis 及生产规模不在该隔离测量范围；最终 SHA 未重跑无关完整矩阵。

本地验收不证明线上额度已返还。本次文档提交时，v21 迁移、恢复模式启用和自然业务对账仍须由生产交付记录确认；P2/P3 保持独立待办。此次收尾只审查文档和证据哈希，不运行全仓测试或追加性能测试。

## 9. 观察、灰度与回退

低基数指标：各 lane 扫描/入列/结算/零费释放/补偿/冲突笔数及额度，队列深度与最老年龄，完整 sweep 时长，最后成功轮次，checkpoint CAS/fence 失败，凭证覆盖率/延迟/迟到额度，平台超额与补偿成本。请求 ID 只进受控审计，不作为指标标签。

初始告警：连续 3 个调度周期未成功；超过 72h 的可处理记录连续 2 轮不推进；任何重复扣款、负额度、错误来源或补偿后追扣立即停用新增处理。冲突与依赖故障单列，不能把它们排除后声称“超期暂占为零”。“凭证可用后约一轮处理”只是在依赖健康且无积压下的目标，超预算时优先保护正常请求并告警。

交付步骤：扩展兼容性准备 → 单独授权的 schema 迁移 → 安装兼容应用但新能力关闭 → observe 核对分类 → 小范围 apply → 按基线门槛放量。P2 另按受信来源与用量白名单启用。模式只控制本方案的后台恢复及异常扩展，不改变正常前台结算。

| 模式 | 异常事实与观察 | 远端查账 | 不足 72h 的后台结案 | 已满 72h 的兼容处理 |
| --- | --- | --- | --- | --- |
| `off`（默认） | 前台保留原 unknown 标记，不新建扩展证据；保留已有旁表。已安装 schema 的 aged 兼容处理仍可写其审计/缓存进度 | 关闭 | 关闭，已有 ready 暂存 | 继续，须读已有旁表：可信证据优先、冲突待审、无证据才补偿 |
| `observe` | 用独立影子游标分类和审计，不推进 apply 游标，不生成可被资金消费者执行的影子 ready | 仅对已配置来源作有界观察，结果只作影子统计 | 关闭 | 同 off，原通道仍运行，不能称整个进程不写资金 |
| `apply` | 持久保存真实异常/证据并按阶段推进 | 来源与用量白名单内启用 | 只处理合格可信终态 | 继续，证据优先、既有政策兜底 |
| `paused`（事故暂停） | 保留已有事实，可记录诊断；不自动把冲突转 ready | 关闭 | 全部关闭 | 本会员核账作业的资金调整也暂停并告警，防止问题继续扩大 |

`off` 不是事故停款开关；资金不变量失败使用 `paused` 并走受控修复/回退。后台资金事务在执行前检查模式，切换暂停后取消并等待在途轮次结束，记录已经提交的操作；不能承诺撤回已提交事务。恢复必须检查故障原因和积压，禁止清空审计。observe 产生的观测不能被另一模式的资金任务当成真实已验证证据直接消费。

已提交资金对应的 cache_pending 在所有模式下均可限量刷新缓存，不能因此重新结算。未安装新 schema 时只能运行旧兼容路径；模式矩阵不授予访问缺失表/列的能力。

回退必须保留恢复表、已保存证据和资金终态。**旧核账程序若不知道旁表中的可信证据，可能按“无日志”误补偿，因此不能仅凭新增表是 additive 就声称任意旧版本可回退。** 首次启用前准备懂得优先读取旁表证据的兼容回退版本，或在受控回退流程暂停旧补偿作业并有明确恢复安排；不能依赖删除新表或清空证据回退。

后端发布遵循 [NewAPI 本地交付入口](/Users/tokk/Documents/Codex/runbooks/newapi-upgrade-and-deployment.md) 的精确 schema 合约、external、stage、10%/50%/100% 灰度、promote 及发布后证据/清理。文档不指定当前颜色、生产版本或 schema profile；业务补偿与部署分开。首批本地开发未执行生产访问或构建。

## 10. 开发启动检查表

- [ ] D0 基线定义、有效样本覆盖和 SQL/首内容量测入口确定。
- [ ] D1 迁移版本、两个旁表、三方言索引与兼容回退设计确认。
- [ ] D2 证据类型、零费用依据、两事务与 72h 独立守卫实现并回归。
- [ ] D3 游标分页、租约取消、查询预算、故障和积压验收通过。
- [ ] D4 由 Sub2API 核实契约后再实现；未覆盖模式明确禁用提前取证。
- [ ] 账务与正常请求性能对照记录齐全，剩余验证缺口明确列出。
- [ ] 需要生产交付时单独完成迁移授权与相应发布门禁；未以文档完成冒充开发上线完成。

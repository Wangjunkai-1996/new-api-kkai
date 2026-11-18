# 工单 #28：Invitations Rust 后端开发规格

依据：[产品需求文档](../invitation-rebate-visibility-prd.md) §3–6；协作入口：[角色分工](./README.md)、[QA 验收矩阵](./qa.md)。源码核对基线为独立仓 `/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src` 的 `ddad789`。以下为待实现规格，不是已上线接口或测试报告。

**唯一业务改动：** 在登录用户的 `my-code` 成功响应中增加「如果现在发生一笔符合条件的好友充值，适用多少比例」。不新增路由，不改 Go、数据库、结算或历史明细接口；无需该用户已有邀请或订单。

## 1. 响应契约

`src/main.rs::application_router` 已注册 `GET {BASE_PATH}/api/invitations/my-code`，页面使用 `GET /invitations/api/invitations/my-code`。保持 `src/routes/user.rs::my_code` 的入参及 `ok(InvitationStats)` 外层不变。以下是**合成联调样例**：

```json
{
  "success": true,
  "message": "",
  "data": {
    "invitationCode": "QA-A", "isSales": false, "hasSalesAccount": false,
    "invitedCount": 0, "totalRebate": 0, "completedRebate": 0,
    "pendingRebate": 0, "confirmingRebate": 0,
    "topupRebatePolicy": { "state": "available", "ratio": 0.075 }
  }
}
```

| 判定条件（已通过访问控制且原摘要成功） | `data.topupRebatePolicy` 的完整 JSON | 含义 |
| --- | --- | --- |
| 开关开，命中且比例合法 | `{"state":"available","ratio":0.075}`；命中零为 `{"state":"available","ratio":0}` | `ratio` 必为 `[0,1]` 有限 JSON 数值；零是结果，不再回退。 |
| 开关开，选规查询成功但未返回行 | `{"state":"not_configured"}` | 当前无适用规则；不承诺补发。 |
| 已确认订单返利开关关 | `{"state":"disabled"}` | 不查个人分组或规则，注册固定奖励仍依原开关。 |
| 仅个人分组、选规或比例转换失败，或超时 | `{"state":"unavailable"}` | 只降级比例；仍返回 HTTP 200、原邀请码及真实汇总。 |

Rust 建议在 `InvitationStats` 上增加 `topup_rebate_policy: TopupRebatePolicy`，沿用外层 `camelCase`。枚举使用 `#[derive(Serialize)]`、`#[serde(tag = "state", rename_all = "snake_case")]`，变体为 `Available { ratio: f64 }`、`NotConfigured`、`Disabled`、`Unavailable`。后三态**省略** `ratio`，不是 `null` 或 `0`。字段对跨版本调用方可选；本版基础成功必给四态之一，旧版才可缺整个字段。旧前端忽略它；新版遇缺字段、未知状态或非法数字按不可用处理，已明确关闭的开关优先。

## 2. 实现顺序

1. **保留原授权与必需数据。** `SessionUser` 由 `src/middleware.rs` 验证，只用 `user.id`。`ensure_invitation_access` 对普通用户检查 `user_invitation_rebate_enabled`；已登记销售（含停用）沿用可读权限。`hasSalesAccount` 来自 `sales_accounts` 是否有行，`isSales` 是当前 `active`，都不是选规依据。`ensure_claim_access` 另管申请，停用销售仍不可申请。拒绝客户端指定邀请人、分组、身份或判定时间覆盖会话；不向公共 `status`、钱包或管理端增设查价入口，不建跨账号缓存。
2. **先取完整摘要，再取可降级字段。** 原 `get_rebate_record_source_filter`、本人 `aff_code`、邀请人数及账本汇总保留原错误路径；复用已读的 `source_filter.order_rebate_enabled`。只有成功读到 `false` 才给 `disabled`；开关缺失、无效或原汇总失败不能造出成功响应。`invitation_signup_reward_enabled`、`rebate_to_balance_enabled`、`settlement_mode` 不是充值费率开关。保留原有 401／403、基础用户不存在的 404 及数据库或认证服务故障响应。
3. **开关开时独立读邀请人的实际分组。** 从只读 `state.newapi_pool` 参数绑定 `user.id`：`SELECT "group" FROM users WHERE id = $1 AND deleted_at IS NULL`。与 NewAPI `model/topup_finalize.go::rebateInviterIdentity` 取邀请人而非付款人的口径一致。不取 `SessionUser.group`，不从最近订单猜测。不要把新列拼进原 `SELECT aff_code`：新列缺失或无读取权限时，邀请码查询必须仍能成功。读取已有行的 SQL `NULL` 视作空组；查无该行、SQL 错误、解码错误均为 `unavailable`，不能当空组。
4. **与充值事件共用分组规范化。** 提取 `src/rebate_source_events.rs::validate_event` 现有的纯分组逻辑供两处用，保持事件原有错误、载荷及散列不变：Rust `str::trim()` 去两端空白；空串取 `default`；非空保持大小写，按 UTF-8 **字节**计 `len() <= 64`。超限返回 `unavailable`，不截断、不改查通用规则。`default` 是具体分组名，不是所有分组的兜底规则。
5. **用同一个选择器和时刻。** 仅在服务端取一次 `as_of = Utc::now().timestamp()`（Unix 秒），让内部只读函数显式接收该秒，便于测试。调用 `current_rebate_rate(&state.pool, group, TOPUP_ORDER_TYPE, user.id, as_of)`：此函数已委托 `src/rebate_lifecycle.rs::selected_rebate_rule`，并把 `rebate_rate::TEXT` 解析为 `Result<Option<Decimal>>`。真实充值用事件的 `inviter_group` 和 `completed_at`；首页替换的是时点与当前分组，不是结算逻辑。不复制 SQL、不调用管理端列表、不加锁或跨库事务，不保证未来订单锁价。
6. **在序列化边界转换一次。** `Ok(None)` 才是 `not_configured`；`Ok(Some(rate))` 先按 `Decimal::ZERO..=Decimal::ONE` 校验，再用现有 `ToPrimitive::to_f64` 并检查有限值。规则列为 `DECIMAL(5,4)`；`0.0001`、`0.0750`、`1.0000` 输出数值 `0.0001`、`0.075`、`1`。不先四舍五入、不乘 100、不发字符串或百分号。匹配行的值空、不可解析、非有限、负数或大于 1 都是 `unavailable`，不向下重选。勿复用历史展示的 `text_to_f64`（解析失败会默认成零）。

### 选择器的三个分支

所有分支先限定 `rule_type = 'topup'`。以下是对**现有 SQL** 的解释，不是要实现的第二套匹配逻辑：

| 顺序 | 谓词 | 易错边界 |
| --- | --- | --- |
| 1. 销售专属 `__sales__` | `effective_time < as_of`，且该邀请人 `created_at < as_of` 的最后一条 `sales_account_events` 为 `active = TRUE`；按 `created_at DESC, id DESC` 取一条 | 两个比较都是 `<`。在变更秒 `T` 仍看前态，`T+1` 才纳入变更；同秒多事件取最大 `id`。无旧事件或最后一条停用则不命中。即便资料已停用，同秒仍可能显示旧比例，但申请立即按停用状态拒绝。 |
| 2. 邀请人分组 | `user_group = group AND user_group <> '__sales__'` | 仅匹配规范化后的实际组；普通用户的组即使名为 `__sales__` 也不能获得专属比例。此分支**没有** `effective_time` 条件。 |
| 3. 通用 `__all__` | 同一订单类型的 `user_group = '__all__'` | 也没有 `effective_time` 条件。非 `default` 组未命中时直接查通用，不额外尝试 `default`。三层均无行才是无规则。 |

### 局部失败与日志

只对步骤 3–6 的新增读取设置一次有界超时（含两个连接池的等待，例如合计 500 ms，不重试）；授权及原摘要不得进入该降级分支。捕获新增读取的 `Err`，**不**在 `my_code` 顶层直接 `await?`，不使用 `.ok().flatten()` 或沿用旧比例。新增逻辑在开关开启时至多多两次 `SELECT`，次数与订单数无关。

现状：`src/error.rs::AppError` 在构造整请求错误响应时才记录数据库／内部错误，成功的 `ok(...)` 不会经过它。局部失败在捕获点记一次结构化 `tracing::warn!`：事件名 `my_code_topup_policy_unavailable`，`reason` 仅取 `group_read`、`invalid_group`、`rule_read`、`invalid_ratio`、`timeout`。不记录令牌、Cookie、邀请码、用户／组名、规则 ID、费率、原始 SQL 或驱动错误；不把诊断放进客户端 `message`。正常关闭、无匹配和明确零值不告警。

## 3. 金额、历史与零写入边界

- **充值基数是实际到账额度。** `src/rebate_source_events.rs` 的 v2 事件只接受正整数 `credited_quota`，结算用 `credited_quota × Decimal 比例`，中点远离零舍入为整数 quota。v1 才从正整数分基数和 `quota_per_unit` 换算。本接口不算金额、不模拟入账。尤其不能照搬 `load_rebate_rule` 的快照缺省值：它以 `rule_id=None, rate=ZERO` 表示无匹配，直接取 `rate` 会把无规则谎报成 0%。
- **历史以记录为准，不以快照替代记录。** `GET /invitations/api/invitations/rebate-records` 维持原分页、过滤、`orderType`、`status`、`displayStatus`；`fetch_rebate_records → rebate_record_from_row` 从 `rebate_records.rebate_rate` 投影 `rebateRatio: number | null`，保留明确零和合法人工调整。`orderAmount` 为原订单金额折分，`rebateAmount` 是 `quota_to_cents` 后的展示值，沿用 `creditCurrency`；两数不是反算比例的依据。确认中也可能已有比例；缺比例时保留原状态，由前端区分待核定与历史缺失。
- **不同来源不套用首页值。** `src/routes/system.rs` 的订阅和独立兑换同步分别读下单人／兑换人的 `users."group"`；规范充值事件路径中的兑换则可按 `topup` 入账。`invite_inviter`、`invite_invitee` 是固定注册奖励，优先按类型识别，不将空比例解释为零。
- **只读针对本次 `my-code` 增量。** 不调用 ingest、finalize、reprice、解锁、申请、审批、发放或同步，不写业务表、快照、收据、outbox 或余额。尤其不要调用看似只读的 `rebate-records` 来算首页：它已有 `unlock_paid_invitation_signup_rewards` 写路径；`available-rebates` 也可能触发解锁。不改这些路由原行为，不新增逐笔查询或迁移。

## 4. 定向测试与签收

以下命令均在上述 **Rust 仓根目录**执行，`--offline --locked` 需要已缓存锁定依赖。本次只编写规格，所有命令均未运行。

**已存在的回归基线。** 前两条无需数据库，仅覆盖旧摘要字段和 v2 基数；后两条为 `src/db.rs::tests` 已有的 `#[ignore]` PostgreSQL 例，需先确认 `TEST_DATABASE_URL` 指向本机专用测试库，许可创建／清理测试 schema。它们覆盖销售时点、冻结快照及历史可见性，但不能证明新接口正确。

```sh
cargo test --offline --locked --bin invitations routes::user::tests::serializes_confirming_rebate_in_invitation_stats -- --exact
cargo test --offline --locked --bin invitations rebate_source_events::tests::schema_v2_accepts_only_credited_quota_as_the_rebate_base -- --exact
cargo test --offline --locked --bin invitations db::tests::sales_topup_rate_uses_order_time_identity_and_preserves_snapshots -- --ignored --exact
cargo test --offline --locked --bin invitations db::tests::user_rebate_records_include_initializing_and_always_hide_closed -- --ignored --exact
```

**拟新增，尚未编写。** 每个测试函数名都以 `my_code_topup_policy_` 开头。用 [QA 夹具](./qa.md) 的三种不同费率（销售 0.15、`qa-main` 组 0.075、通用 0.03），不要用相同数值验证优先级。

| 位置／类型 | 需要独立断言的行为 |
| --- | --- |
| `src/routes/user.rs::tests`，无数据库 | 四态 JSON 键集、非 `available` 确无 `ratio`、零与精度边界、空／非法／越界不得变零；删除新增键后原响应完全一致。给共享规范化用例覆盖 SQL NULL 对应输入、全空白、带空白有效组及 64／65 个 UTF-8 字节。 |
| `src/db.rs::tests`，隔离 PostgreSQL：选规（A01–A04、A11） | 复用 `TestDatabase` 的 `external_pool` 和 `pool`，分别建用户和规则夹具。无订单也有比例；会话组与数据库组不同时用后者；验证销售、组、通用、无行、组为零且通用非零、保留组名不能越权。规则与身份分别在 `T-1/T/T+1` 测启用、停用、同秒多事件；普通／通用规则不受销售时间谓词约束。固定 `as_of`，同时断言预设数值与同秒真实选择器结果，不用 sleep。 |
| `src/db.rs::tests`，隔离 PostgreSQL：权限与失败（A04、A08、A09） | 普通账号无入口仍 403；无会话须经真实路由提取器验证拒绝；停用销售可读但不可申请。只开注册奖励返回 `disabled`；仅关闭转入或发放维护不改比例。A/B 交替读取不串码、不串比例。仅让测试库缺 `"group"` 列、规则查询报错或超时：原字段不变、局部 `unavailable`，恢复后重试可用；同一故障下关开关应得 `disabled`。原开关读取／基础摘要失败则仍为整请求错误。 |
| `src/db.rs::tests`，隔离 PostgreSQL：历史及无写入（A05–A07） | 老单 5%，将普通组改为 7.5% 后反复读摘要：与夹具预期对照，首页更新、旧记录的比例／金额／状态不变；核对旧零、NULL、人工调整、两类固定奖励和不同订单来源。对照读取前后的相关账本、快照、申请、发放、解锁与余额值及行数；历史投影可用 `fetch_rebate_records` 取证，不把明细 GET 的既有解锁计入摘要。 |

无数据库开发可先运行下列第一条（默认跳过标记 `#[ignore]` 的 SQL 例）；**正式联调只需第二条**，与 QA 文档同一入口，一并执行纯逻辑和隔离 SQL 用例。不要为前缀加 `--exact`，不要用未设库时静默 `return` 来伪造通过。

```sh
cargo test --offline --locked my_code_topup_policy
TEST_DATABASE_URL="${TEST_DATABASE_URL:?请先指定本机隔离测试库}" cargo test --offline --locked my_code_topup_policy -- --include-ignored
```

**签收交付物：** 后端提交 Rust 变更版本、四态与整请求拒绝的脱敏样例、旧字段对照、同秒预期／实测表、历史及只读断言、定向命令和实际命中数。评审核对权限先于查询、复用结算选择器、失败没有伪装成零、读摘要没有资金副作用；QA 在 A01–A11 矩阵签收，A10 的页面展示归前端。无数据库、零用例命中或 SQL 例被跳过须列为未验证。回退服务仅撤掉新增字段，不回滚业务数据。

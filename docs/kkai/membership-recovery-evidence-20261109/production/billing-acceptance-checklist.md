# B + canonical21 的最小生产验收

仅供执行者使用。本文件及配套脚本是本地准备材料，不是生产验证结果。配套
`billing-acceptance.py` 只读 PostgreSQL；不创建样本、不回放请求、不修改账务。
必须先通过运行手册规定的当前部署身份、canonical21、控制器和回退版本门禁。

## 先明确模式

- `paused` 才停止整个自动恢复轮次。暂停后正常请求仍可预占、结算、释放。
- `observe` 会真实执行超过 72 小时的兼容核账及 cache_pending 重试，包括真实扣费和退款。
  它不是资金 dry-run；合格旧记录已有证据则结算，无证据则按既定政策补偿。
- `apply` 另外处理 ready/pending 和异常路径证据。冲突记录保留待核实，不能猜费用。
- 暂停或回退不撤销已提交账务。回退 A 应固定 paused，不能回退到 max20。

## 执行顺序

1. 获取新鲜 `make newapi-status`、专用迁移 status、`membership-recovery status`。
   确认 active/writer 是已验收的精确 B，rollback 是固定 paused、支持 21 的精确 A；
   canonical21 的版本/摘要/收据完整，migrator NOLOGIN 且无会话；无并行 candidate/rollout。
2. `sudo kkai-newapi-manual-deploy membership-recovery set-mode paused`。
   保留返回 generation、writer/process identity、有效 paused 与 inflight=false。
   选定观察起点 T0（主机 UTC epoch 秒），执行 baseline 汇总；以下各阶段固定同一 T0。
3. `sudo kkai-newapi-manual-deploy membership-recovery set-mode observe --expected-generation G0`。
   status 必须确认同一 writer 上的对应 mode/generation。等待自然完成至少两轮，检查汇总
   worker 日志和实际失败；不要反复切换模式来强迫额外执行。每轮写入/时间预算有限，不能
   宣称一分钟清空任意积压。observe 期间允许真实旧账结算/补偿和缓存刷新。
4. 再次 `set-mode paused`，确认新的 G1、paused 和 inflight=false，取得 paused 汇总。
   比较同一快照的会计约束、实际恢复终态原因/额度及缓存状态；核实新增差异后才继续。
   有不明会计差异、错误增长或无法确认暂停时保持暂停并查证。
5. `sudo kkai-newapi-manual-deploy membership-recovery set-mode apply --expected-generation G1`。
   status 验证后观察至少两轮。取得首次 apply 汇总；从已完成快照选择固定终态上界 T1，
   用同一 T0/T1 再取一份作为重复轮次基线；再经过自然轮次取第二份。保持相同的 T0/T1。
   固定终态样本的数量/金额/指纹应一致，其 cache_states 可以从 cache_pending 收敛到 resolved。
   样本为 0 时明确记录未观察到；超过 1000 时明确这是最多 1000 条的样本。
6. 完成当前、延迟的部署/控制 status、错误窗口和回退就绪检查。报告 schema-ready、
   recovery-enabled、自然业务样本验收各自的实际结论；自然样本不足不等于业务验收全通过。

## 配套只读命令

将脚本放在执行者选定的主机 root-only 目录；仅记录脱敏 JSON：

```sh
sudo python3 /ROOT_ONLY_DIRECTORY/billing-acceptance.py baseline --since T0 --terminal-cutoff T0
sudo python3 /ROOT_ONLY_DIRECTORY/billing-acceptance.py after-observe --since T0 --terminal-cutoff T1
sudo python3 /ROOT_ONLY_DIRECTORY/billing-acceptance.py apply-repeat-baseline --since T0 --terminal-cutoff T1
sudo python3 /ROOT_ONLY_DIRECTORY/billing-acceptance.py apply-repeat-check --since T0 --terminal-cutoff T1
```

T0/T1 是整数 epoch 占位符，不可原样执行。后两次 T0/T1 必须完全相同，且 T1 来自
先前 snapshot_epoch。脚本一条汇总查询在一个 REPEATABLE READ READ ONLY 事务中执行，
`SET LOCAL ROLE newapi_standby`，statement_timeout=5s、lock_timeout=100ms、进程超时15s。
如果超时/缺表/权限失败，停止并诊断；不要扩大预算或把缺失结果当作 0。该脚本只适用
已验证的 v21 表结构，本身不替代 canonical digest/receipt 的迁移检查。

## 精确对账口径

设单笔预占 R、实际计费 A、最终扣费 C，必须满足：

- settled：C=min(A,R)，excess=A−C，返还=R−C；所有金额非负。
- released：A=C=excess=0，返还=R。
- 每个会员账户 reserved=该账户所有 billing_scope 的 reserved 账本 SUM(R)。
- 每个会员账户 used=该账户所有 billing_scope 的 settled 账本 SUM(C)。
- 账户 total/used/reserved 非负且 used+reserved≤total；账本 user 与原 account 的 owner 一致。
  不能只取 Relay 账本对所有会员账户，也不能把旧会员周期返还计入当前新账户。
- 后台完成的真实窗口记录以 review_reason 为 trusted_terminal_evidence 或
  no_evidence_compensation 且 review_resolved_at≥T0 统计。输出按资金来源、终态、证据和
  缓存状态分组。补偿记录必须 released 且完成时年龄≥72h。
- cache_pending/resolved 必须关联存在的 Relay 终态账本。resolved_at 必须有效。
  pending/ready 关联已完成账本可能是前台获胜后的正常待清理状态，不可直接判为重复扣费。
- token_refund_unapplied/token_used_refund_unapplied 单列检查：管理员调额或重置使用量可能
  导致它们非零；不能把这种受限 Token 退款直接说成原付款账户未退款。
- worker 日志 adjusted_quota 是 C，不是返还额；退款只用 SUM(R−C)。会员退款增加原账户可用额，
  钱包退款增加用户钱包余额；reservation_closed 是解除的完整预占，不能等同于退款。

## 证据边界

账户对账、终态金额公式和固定样本指纹是必要观测，不单独证明并发 exactly-once。固定指纹
只证明该终态样本的资金字段在重复观察间没有改变；不会证明所有历史终态都被再次处理过。
并发/租约过期/重试/崩溃边界仍依赖已有隔离数据库回归证据，生产不注入故障或造账。

自然流量可能没有有费恢复、明确零费用恢复、72h 补偿或 cache_pending 失败重试样本。
每一种未发生分支都应标注“本窗口无自然样本”，不能以队列为空推断全部行为已在线验证。
固定样本 cache_pending→resolved 能证明相同账本已完成运行时缓存失效调用；如果从首次采样
起就 resolved，只能证明成功完成状态，不能证明观察到了失败重试。resolved 也不证明
某个用户浏览器已重新取数。

全局钱包、Token 或 user.used_quota 的前后差会混入其他请求、充值和管理员操作；不能用它们
推导某笔退款已准确到账。本脚本不导出标识或敏感 payload，因此不做单用户余额归因。
需要该证据时由执行者对已自然产生的明确账本在主机内部作最小范围关联检查；不制造交易。
已有账差必须与 baseline 比较后查明来源，不能通过改数“做平”。

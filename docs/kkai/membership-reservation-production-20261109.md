# 会员暂占恢复生产交付记录

2026-11-09，北京时间 23:30 完成应用、schema21 和自动恢复启用验收。源码同步与生产交付独立记录；本页后的文档提交不改变线上二进制。

## 已上线结果

| 项目 | 实际结果 |
| --- | --- |
| 应用 B | `fc46400f3fed2647fafadd974ea1fc8d005d6015`，`kkai-prod-20261109.1794233284-fc46400f3` |
| Active / writer | green / 独立 writer，均为 B；最终 `make newapi-status` 为 HEALTHY，rollback ready |
| 回退 A | `2648161aba4755a4cc7f75a10098deb3cbbd1d4a`，`kkai-prod-20261109.1794225812-2648161ab`，固定 paused，兼容21 |
| 已安装控制器与应用 pin | `29e9a8e927ea35df939e912baa381e663591f4a2` |
| 数据库 | canonical21，恢复两表、列、索引及迁移凭证完整；migrator NOLOGIN、零会话 |
| 恢复开关 | `apply`，generation `2f3a0b21dfa04d44925482f117c3ea81`；服务与 writer 确认，回退 A 仍固定暂停 |
| 前端 | format2 `kkai-frontend-20261108.1794152490-b57024a2a`，current/previous 与 manifest hash 均未变化 |

A、B 分别完成候选最小业务、10%/50%/100% 每档至少120秒、独立 promotion、即时及五分钟延迟状态、额外60秒日志保护及等长错误窗口审阅。没有重复构建同一提交、没有在生产构建、没有跳过灰度。B 于15:07:32 UTC 完成 promotion，15:26:59 UTC 启用 apply。

## 性能与实际业务

正常预占、成功请求及结算不增加同步恢复表 SQL，不增加首内容之前的外部调用。恢复控制轮询和有界恢复在后台运行。隔离 PostgreSQL 的最终对照中，生产501条积压场景首内容 P50 中位变化 +0.092ms（+0.78%），完成 P95 中位 +0.435ms（+1.91%）；未发现持续同向退化，但不能解释成数学上的零开销或生产所有流量的延迟保证。原失败数据与限制完整保留在 [B 性能证据](membership-recovery-evidence-20261109/performance-B/README.md)。

候选、公开切换后、apply 启用后均验证真实会员和普通钱包的 SSE：收到内容、正常结束，会员账本唯一结算且资金来源/公式正确，钱包消费日志存在且扣费非负。生产没有制造失败账单或注入故障。

observe 自然完成3轮；apply 在最终账务快照前自然完成5轮，每轮133条扫描/100条排队，全部 failures=0。累计500条 unknown 暂占进入 pending，扫描游标持续前进。全 scope 的5个独立只读快照均满足：

- 会员账户 reserved 等于对应账本 reserved 总额，used 等于 settled 扣费总额；差异账户为0。
- 负金额、余额越界、用户/账户归属错误、终态金额公式错误、Token退款未应用量均为0。
- sidecar 缺失账本、作用域错误、虚假终态等异常均为0。

**本次观察期实际自动结算/退款终态为0，不能称“全部已返还”。** 最后快照15:30:36 UTC 的会员暂占849笔（其中848笔待核查）、钱包569笔；两者满72小时的笔数都为0。会员原始暂占值793,205,843 quota，钱包206,542,426 quota，包含随正常业务变化的预占，不能当作全部应退款金额。没有可信终态证据的记录按既定72小时规则处理；冲突记录不能猜费用。对已有可信终态的异常才提前恢复。

固定 T0=1794237649 / T1=1794238071 的两次终态样本均为空，空指纹相等**不能证明生产重复扣费边界**。本窗口没有有费恢复、confirmed_zero释放、72小时补偿、cache_pending失败重试的自然样本；这些分支依靠已归档隔离数据库回归，未宣称全部在线验证。

## 错误窗口与迁移异常

B 发布前五分钟727个模型事件/5错误，发布后963/1；启用 apply 前两分钟292/5，启用后343/2（400、404）。它们是模型事件率，不是最终用户失败率。B100%灰度848次完成请求中有3次 relay_chat HTTP5xx；旧 active 先前同窗也有该类错误。独立代码范围复核、业务流程和恢复轮次未发现本次特有回归；未定位这些既有生产错误的精确原因，不能称全站零错误或确定上游故障。

迁移前备份 `20261109T150933Z`：全schema及除logs外的数据、6个角色，已在 racknerd-104 ingest/verify；校验等级仅 `encrypted_integrity_only`，不冒称本次重新做了恢复演练。dump SHA256 `a162a7c1c31889a976e254b5d22871d681d89e7a8da71b8a3bc9c88197291955`，roles SHA256 `09b2f9af4d2b3397870cd64fc81d4b8f94f96e970151f02003ec6c12404ea354`。

首次受控 apply 返回失败并留下 started/failed 审计。后续独立只读 status 已证明 canonical21、两表/索引/receipt 完整，三台运行时身份未变，migrator NOLOGIN/零会话。按 runbook32 的 target21 completed replay 取得 verified；该分支不启用迁移账号、不重做DDL。首次失败具体阶段未确定，不能声称首次命令成功或确定超时。已保留失败记录；后续基础设施维护应补细分脱敏错误阶段。未扩大100ms锁/5s语句/15s命令预算。

本地验收脚本首次按行解析 PostgreSQL 多行 JSON 失败，修为解析完整stdout后通过；该错误是只读结果解析，无数据库写入。早期候选验证脚本、错误远端路径调用的失败日志亦保留，不计作PASS。

## 验证命令与范围

最终完整基础设施检查已获用户授权，`make -C ../kkai-infra check` PASS（275.931秒；936项非文档输入无漂移），包含policy；未在输入未变时重跑。应用未运行全仓测试；采用受影响 model/service/relay/迁移CLI、真实PostgreSQL/MySQL、暂停并发屏障及真实性能验证，原始命令见 [实施记录](membership-reservation-implementation-20261109.md) 和性能证据。

最终优化对应定向命令（PASS）：

```sh
KKAI_MEMBERSHIP_ENABLED=true GOMAXPROCS=2 go test -p 1 ./service \
  -run '^TestMembershipRecovery(ClosedConflictsAreNotBlockedByUnresolvedBacklog|WorkerModesKeepEarlyEvidenceAndAgedFallbackSeparate|WorkerRefusesPartialSchemaInsteadOfLegacyFallback)$' \
  -count=1 -timeout=90s
git diff --check
```

生产正式命令 `make newapi-status`、`make newapi-slot-http-window`、`make newapi-error-window`、专用迁移 `plan/apply/status`、`membership-recovery set-mode/status` 的参数、时间和返回码随 [生产证据目录](membership-recovery-evidence-20261109/production/README.md) 保存。只读账务脚本使用RR事务、standby角色、5s语句/100ms锁/15s进程预算。

## 清理与回退

本机移除3个已核验发布tar（合计282,781,184字节）及无容器引用的旧ff2a镜像标签；保留metadata/证据。性能专属容器、匿名卷、凭据及约2.73GB临时产物已按精确清单清理，见 [性能清理回执](membership-recovery-evidence-20261109/performance-B/cleanup.json)。

sys1 无上传后端tar、候选状态/容器、已知隧道/socket残留；本次已安装控制器的上传bundle已按SHA/身份核验后删除。171个后端release、94个镜像受v10 schema snapshot冻结保护，明确保留；117个前端目录中2个受指针保护，115个尚在30天窗口，无到期项。未手删线上release/image/frontend，未运行全局prune。清理后正式status及current/rollback/前端hash/进程身份复核通过。

回退必须使用已安装控制器及兼容21的A。A固定paused，正常请求结算继续，自动恢复停止；暂停/回退均不撤销已完成资金事务，不允许回退max20、删除恢复表或覆盖恢复旧数据库。

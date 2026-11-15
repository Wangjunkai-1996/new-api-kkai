# 发布与 SRE 方案审查

审查日期：2026-11-15（Asia/Shanghai）
审查对象：`plan.md` v0.1
审查角色：发布 / SRE
结论：**有条件通过；完成下列发布方案补充后可冻结，不需要重复向用户申请正常发布授权。**

本次仅审查和只读核查。未构建、提交、stage、promote、改动生产状态或执行数据库维护。没有取得本次发布时的实时应用槽位、版本、前端格式或数据库兼容证明，本文不将历史记录视为这些证明。

## 1. 已核对的发布入口

| 对象 | 确认位置 / 身份 | 审查结果 |
| --- | --- | --- |
| NewAPI 应用 | `/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai`，`production/kkrich` | 正确应用入口；只能提交本任务改动，构建前再做干净树及完整 Git 对象门禁 |
| NewAPI infra | `/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/kkai-infra` | 有正式 NewAPI 手工入口；此 checkout 没有 Sub2API 发布 wrapper |
| NewAPI 应用 pin | `ee4e460d1ac77e017ee3ad3a4af97beb3b090634` / `router-v3-staged` | 是本地契约文件的值；正式准备必须核对实际安装身份，不因文档 HEAD 不同而重装控制器 |
| Sub2API 应用 | `/Users/tokk/Documents/Codex/2026-08-09/li/work/sub2api`，`release/v0.2.21-kkai` | 本次已确认应用 checkout；其 sibling `kkai-infra` 不存在 |
| Sub2API infra | `/Users/tokk/Documents/Codex/2026-08-09/li/work/kkai-infra-warmup`，`e7f898f7831a50ed9675885fd92cbdf71e306847` | 由全局手册明确引用定位，具有正式 wrapper；只核对这个明确路径，没有扫描历史 clones |
| Sub2API 已安装控制器 | `/opt/kkai-sub2api-release/releases/e7f898f7831a50ed9675885fd92cbdf71e306847` | 只读 `self-test` 返回 `status=ok`；安装 SHA-256 为 `1daddc38533f6e9eb23fe5660c8d822dd22dafd988446c611d6015c13d0d1a6d` |

Sub2API 实际安装声明 `format_version=1`、`observation_contract=operator-reviewed-v1`，其 baseline controller commit 为 `e459cb1a586f9d64507ec49c09bdc5da913827ed`；能力包括 `operator-reviewed-v1`、`reviewed-plan-sha256`、`single-upload-plan-stage`、`release-timing` 和 `reviewed-image-cleanup`。因此本次可使用正常 v1 计划、灰度以及受审查清理入口，不需要为应用修复升级控制器。正式 stage 前仍须冻结并核对同一安装身份。

只读核查命令：

```sh
ssh -o BatchMode=yes -o ConnectTimeout=12 -o IdentitiesOnly=yes \
  -o ProxyCommand=none -o ProxyJump=none -o KexAlgorithms=curve25519-sha256 \
  -i ~/.ssh/ovh_sys1 sys1 \
  'sudo -n /usr/local/sbin/kkai-sub2api-release self-test'
```

Sub2API `status` 可能 rebaseline 并保存控制状态，不能把它当作本次方案阶段的纯只读替代品；实际发布启动时可在用户已授予的发布授权下执行。

## 2. 冻结方案前必须完成的修改

### RP-1：冻结串行顺序和精确 scope

`plan.md` 第 7 节目前建议 Sub2API → NewAPI。这个顺序在协议上可行：现有 NewAPI 已认识 marker，Sub2API 新 marker 不需要同步切换消费者。NewAPI → Sub2API 也可行，且本次更适合采用：

1. 先完成 NewAPI 后端的安全诊断落盘与权限保护，再完成管理员前端展示；旧 Sub2API 的普通错误继续走原有状态路径。
2. NewAPI 发布完成后，再发布 Sub2API 的生产者终态证据修复，使新协议生效时诊断能力已经可用，便于观察切换后的明确失败和未知路径。
3. 任一端回退都不要求另一端同时回退：NewAPI 回退后旧消费者仍能识别 marker；Sub2API 回退后只恢复旧错误输出，NewAPI 继续兼容。

这不是协议硬依赖，但应在 `release-plan.md` 选定一条顺序并给出原因，避免执行时临时切换。建议冻结为 **NewAPI 后端 → NewAPI 前端 → NewAPI 完整收尾 → Sub2API image → Sub2API 完整收尾**。

两端不得并发 stage/canary/promote。Sub2API stage 会记录并保护所有非目标服务的容器生命周期；灰度期间更换 NewAPI 容器会造成保护对象漂移。单纯换一下先后次序不能替代串行约束。

主方案已包含管理员诊断 UI，因此 NewAPI 是后端与前端联合变更；format 2 下仍是两个独立事务，后端 candidate/rollout 完成后才能 prepare/activate 前端。Sub2API 预期是 `image` scope；代码审查必须确认共享文件中的改动被图片路径限制，不能只因文件名或 package 共用就发布 text scope，也不能在实际改变文本行为时继续声称仅 image。

### RP-2：把 schema 和前端能力选择落成可检查门禁

NewAPI 当前目标源码两个 profile 的 `(min,max,target)` 都为 `(21,21,21)`。`contract.go` 还要求 **`rc49-online-v1` 和 `video-media-metadata-v1` 两项 expansion**。仅证明 canonical21 或仅查 rc49 回执均不充分。

`release-plan.md` 必须记录：精确 target SHA、实际选用 profile、canonical21 digest、两项 required expansion 的 digest/完成状态、现场只读证据来源，以及精确 built image 的 `--describe-contract --dialect postgres --json` 对照结果。只读证据通过已安装控制器及适用 schema 入口取得。不得依据 profile 名称、`newapi-status` 或 generic preflight 推断兼容，也不应将这次普通发布变成迁移任务。

若经验证的已安装 stage image-validation 已将精确 archive/image 与 binary contract 和实时 DB 绑定，可复用该成功结果；否则使用精确 image ID 做本机离线 contract 核对。不得为查询契约重建相同提交。

前端须在发布时检查实际 selected artifact / manifest / controller 与 Edge 能力。format 2 兼容后端更新保留 current、previous 和资源校验和，后端事务后才激活本次前端。若现场仍是 format 1，则使用其精确配对路径；不得按本地源码宣称 format 2 已安装，也不得临时改元数据模拟升级。新 UI 必须兼容诊断字段缺失，以支持前后端滚动与独立回退。

### RP-3：明确无账号验收的覆盖范围和不可伪造的证据

用户明确排除了代理登录账号验收，且已授权完整上线；不再索取账号、不创建测试用户/API key、不制造生产业务数据。记录应写明：**“依用户要求，登录和真实账号业务 E2E 未由代理执行（用户接手业务测试，代理未验证）。”** 不能将免登录要求写成测试通过，也不应把这种缺口重新变成用户审批门禁。

NewAPI 手册已有业务测试交接例外；适用的是账号与业务 E2E，不是版本、健康、schema、灰度计时、错误复核或回滚门禁。此次 worker 在 candidate 中没有后台作业执行能力，真正 writer 变更发生在 promote；候选 `/api/status` 正常不能证明 async worker 业务效果。需保留本地 worker 回归证据及发布后可取得的自然终态/诊断证据，并如实记录不存在自然样本的范围。

Sub2API v1 仍要求 baseline 和 candidate 的 `samples` 均为真实正数。由自然图片流量被动采样；无样本时延长当前灰度观察到方案规定的上限，不得把健康探测、usage 行、HTTP 200 或 `image_count` 当成完整图片成功证明。未取得正数错误窗口样本不得伪造 JSON 晋级；到上限仍不足时保留原活动版本并按控制器停止/回退，报告实际限制。若自然完整图片结果无法独立验证，仅说明真实图片业务 E2E 未由代理执行，不宣称其成功。

`release-plan.md` 应在开始前写出被动窗口的来源、UTC 边界、样本归属、最多等待时长和缺样本处置。这无需向用户重复申请登录权限，也不能用 v2 shadow collector 替代生产 v1 门禁。

### RP-4：将两端灰度与错误复核要求写成可执行序列

NewAPI：发布前一次 fresh `make newapi-status`，精确 target/rollback 可用；保留 stage 前 300 秒 UTC 基线。stage 后核对 candidate version、slot、image 和私有入口。`10% → 50% → 100%` 每级至少 120 秒，保存 rollout ID；按 exact version/slot/rollout 做 handoff-ready 后 promote。promote wrapper 已 finalize，不再无条件 finalize 第二次。考虑 async writer drain 最长可能等待 24 分钟，等待不应被误报成超时失败或据此重发 promote。

promote 后必须有即时状态、五分钟延迟状态、有效路由、writer/rollback 身份和 rollback ready。候选五分钟错误窗口结束再等 60 秒写入保护，用 `make newapi-error-window` 采集与基线等长、已批准的脱敏模型错误聚合，执行者逐组归因。命令 exit 0 仅表示采集成功，不表示错误可接受。不可用的目标 HTTP 5xx/延迟聚合如实写 unavailable，不能用全局事件替代目标版本证据。

Sub2API：精确 request/archive → `plan-stage` → 执行者审阅完整 plan 并将 exact `plan_sha256` 输入 stdin → `staged_0` → `10/50/100/complete`。三个观察窗口均至少 120 秒。每次从窗口的真实 entered-at 保留完整错误群与真实正数 denominator，不挑干净窗口覆盖已发现的错误。

v1 正常 error event 只有 `count/status/origin/reason/upstream_status` 五个字段。正常 evaluator 允许的 candidate 外部错误仅是 `external_upstream` 的 `overload|timeout`，两个状态均为 408/502/503/504；其他已独立归因且与发布无关的既有、客户端、外部失败可用现有 v1 operator acceptance，绑定当前 release、source phase、destination、entered-at、router hash 和最终文件字节 SHA。该通道由执行者审阅，不增加用户确认；不能隐去失败、重标 HTTP200 内嵌错误、把未知原因写成外部错误或接受新回归。

### RP-5：明确单端回退与清理边界

回退不改业务数据，也不把已正确退款的任务重新扣费。历史任务 7734 不应在发布过程重开或重放。

- NewAPI 灰度失败先使用对应 rollout 的 rollback；候选拒绝用官方 abort；已完成 promote 的回退使用官方后端 rollback，并先确认仍选中前端与回退后端兼容。format 2 若 rollback-plan 要求先切前端，则先选择已安装兼容前端再回退后端。
- Sub2API 使用 exact release ID 的官方 rollback。不能改 router 内容、删除 state、重新 stage 同一 artifact 来猜测中断结果；保持当前 router inode 和原活动版作为回退对。
- 任一端动作中断先按 recorded identities 核对实际结果，只有原事务允许的重试才能继续。不要重复成功构建或成功 promote。
- NewAPI 清理保留 active/rollback 至少 24 小时及控制器 pin，前端保留 current/previous/rollback-plan 目标与至少 30 天资源支持窗口。受控保留状态决定实际可删项，不能凭构建日期删除。关闭本任务创建的候选 SSH socket。
- Sub2API 已安装 `reviewed-image-cleanup`，可执行 `cleanup-plan` 后审阅再 `cleanup-apply`，绑定 exact release、固定至少 24 小时前的 `before-epoch` 和 plan SHA。保护所有在用容器、current/previous manifest、state 引用；禁止 raw prune 或手工 rmi 绕过计划。
- 本地只删除已证明不再需要且有校验/远端部署证据对应的本任务 archive/临时资源。控制器不支持或保留期未到的资源写出明确 deferred disposition；清理 warning 不否定已完成发布，但不应消失在最终记录中。

## 3. 无需扩大范围的结论

本问题不需要 schema 变更、控制器重装、Edge/ACME 首次接管、额外分支/worktree、Git hosting 同步、生产构建或日志驱动修改。现有控制器和发布协议提供了所需门禁。两端应用制品及回滚保持独立；本次不新增跨服务的原子发布机制。

生产语义改变发生在 Sub2API 证明并输出终态 marker 时，故业务正确性主要由来源完整性、unknown 粘滞、批次成功优先及 NewAPI 幂等退款的定向回归证明。蓝绿观测是防止运行回归与身份错误的补充，不能替代这些测试。

## 4. 冻结与执行条件

主代理将 RP-1 至 RP-5 的选择和命令落实到 `plan.md` / `release-plan.md`，再将本审查处置记入总 review。后续开发文档、测试与代码独立审查应一致；若实现改变 release scope、frontend/API 合约或迁移边界，需重新核对对应条目。全部条目处置后，本 SRE 审查同意按用户现有授权执行正常发布，不增加等价审批。

审查验证仅包括文档、脚本/契约源码检查和上述已安装 Sub2API `self-test`（通过）。未运行应用测试或全仓套件；未验证发布时生产状态、数据库和前端实际格式。这些是正式发布准备阶段的明确待执行门禁，不是当前已通过结果。

## 5. v1.0 再审（2026-11-15）

已复核主方案v1.0及落地的 `release-plan.md`：RP-1至RP-5均已处置。冻结了NewAPI后端/前端完整收尾后才发布Sub2 image；schema两扩展、实际format2事务隔离、账号E2E交接、真实正样本与120秒/30分钟边界、exact identity回退及受控清理均已明确。**发布/SRE方案审查通过，可进入定向开发验证与最终代码审查，随后按现有授权执行正常发布。**

另已完成只读准备，见 `release-preparation.md`：fresh NewAPI HEALTHY/rollback ready，canonical21及两个必需扩展通过，安装pin文件相符，实际format2/api_contract4、pair gate与backend-only回退计划通过。选择feature有现场tenant routing及安装门禁依据。尚未构建或变更生产；最终制品、灰度、错误窗口和发布后验收仍是执行门禁，不在本结论中预先视为通过。

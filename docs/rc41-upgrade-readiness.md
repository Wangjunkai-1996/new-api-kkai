# rc.41 架构升级：切流前准备记录

记录日期：2026-10-05。本文记录本轮证据，不代替下一次生产操作前的实时状态检查。

后续统筹更新：用户已要求将本任务、美元额度迁移、sys1磁盘缩容与备份卷分配安排到同一个维护窗口。后续准备和执行统一引用 [磁盘、rc.41与美元额度联合维护计划](kkai/rc41-usd-joint-maintenance-plan.md)。本记录保留架构任务已有证据；它们不代表新增存储/币制阶段已实现或三项联合验收已通过。

## 目标与边界

将官方 `2035a82aeb5414253a728bd937d4b8f97aa99b9b` 的 rc.41 架构合入本地
`production/kkrich`，保留 KKAI 的 `web/default`、Studio 和计费能力。
JWT/refresh session、scoped PAT、安全验证及任务插件采用新版实现；不恢复额外的旧
Gin/Gorilla session 兼容层。官方自身的有限旧 PAT 退休机制保留。

用户本轮授权推进到公开切流之前。10% 灰度也是公开切流；前端 activate、维护页面接管、
生产停写和数据库迁移均未执行。应用合并已完成，不能据此宣称候选验收完成。

## 本轮只读现场证据

- 基础设施 checkout：`/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/kkai-infra`。
- `make newapi-status`：HEALTHY；green；版本
  `kkai-prod-20260924.1790226680-fa1fecb81`；snapshot stable、rollback ready。
- 实际安装 controller 与应用 pin 相同：
  `f8f3a0afa5ccbe533e5ebd34fc343643ae05748e`。
- 当前后端镜像的 schema contract 是 `(7,8,7)`。
- 使用 standby 只读身份，在 `BEGIN READ ONLY` 中读取 migration ledger 和字段目录：
  已记录版本为 **v7**，完整前缀摘要为
  `sha256:d0779962929f5f47a608fa83adec278bcadf4458507d6dc36bb4503878cde15e`。
  没有读取业务行；运行配置未设置独立 `LOG_SQL_DSN` / `LOG_SQL_DSN_FILE`。
- 当前选中的前端为 **format 1 / API contract 1**：
  `kkai-frontend-20260924.1790227810-fa1fecb81`。历史 format 2 验收记录不能代替这一事实。
- 旧镜像的完整 `--observe --current` 检查失败：缺少 `midjourneys.token_id`。
  进一步字段目录比对确认还缺 `midjourneys.billing_channel_id`；这两列已经纳入本次
  尚未发布的 v9 forward migration。不能把“账本 v7”描述成完整 schema 检查通过。

脱敏结构证据保存在本机 `.local-releases/rc41-preparation/`。

## 已完成的本地修复

- 修复 Argon2id 新密码无法通过旧 bcrypt-only 校验的问题；保留已有 bcrypt 密码登录。
- 任务列表不再读取和返回内部任务 `data` 快照。
- Image Studio 固定分辨率计价优先级与实际 Relay 结算一致。
- 视频 Data URL 复用统一的大小限制、HEAD、流式读取与缓存安全实现。
- 用户设置按实际传入字段更新，保留省略项；通知地址更新经过校验。
- `--observe --current` 按历史 schema 版本校验；新版应用启动仍严格要求 v9。
- 迁移回归覆盖真实逐版本升级、数据保留和重复执行；补齐旧测试的必要基础表。
- 默认 UI 修复本次变更涉及的 React 状态、ref 与表单读取问题；保留用户既有修改和版权头。

## 当前不能 stage 的原因

新版两个构建 profile 的实际契约均为 `(9,9,9)`，新版控制台要求 API contract 2。
`bridge` 这个名称不能使代码运行于 v7/v8。安装的 controller 只接受到 v8，schema CLI 与
snapshot 状态机也没有 v9 流程。

正常候选虽然不接公开流量，却使用生产 writer 数据库。对 v7 直接 stage 新版无法启动。
新旧前端 API 契约又没有交集，当前 release controller 不能原子切换前端、后端和数据库。
不能通过更改 JSON 声明、放宽门禁或提前激活新版前端处理这些问题。

因此本轮没有上传、stage、canary、promote 或执行任何生产迁移。专用维护预构建入口现已
实现并通过定向测试；它只准备目标 v9 产物，不宣称线上已是 v9，也不允许普通 stage 使用。

## 推荐后续方案：维护窗口内联合升级

这属于基础设施/schema 升级。现有正常蓝绿入口不能直接执行下面的流程。维护事务已在
本地实现并完成定向验收；只有完成全部产物、控制器和演练门禁后才能成为可执行的生产方案。

1. 在当前 infra 分支补齐 v9 的精确 contract、digest、snapshot、observe 与恢复校验。
   迁移器必须绑定审核过的不可变新版镜像，不能继续强制取旧 current 镜像，也不能仅在
   enum 中加入 9。保留旧版精确前后端组合与数据库恢复证据。
2. 实现持久化维护事务，明确维护路由、请求排空、唯一 writer 停写、旧/新前端选择和
   新/旧后端坐标。失败后继续保持维护状态，不向公众开放不匹配组合，不绕过常规入口。
3. 在隔离 PostgreSQL 环境演练与现场形状相同的 v7→v8→v9、重复执行和中断恢复。
   验证旧业务记录保留、新版登录/权限/计费/任务插件及 Studio 的最小流程；执行 infra
   `make syntax`、`make policy`，如改变 Ansible 行为再执行 `make check`。
4. 形成精确提交、镜像摘要、前端metadata、联合操作计划与恢复记录后，按用户明确安排并授权
   的维护窗口执行数据库迁移、controller安装及开放。不得在仅有准备授权时先迁移生产再长期等待。
5. 经确认进入维护窗口，全体受影响业务排空停写，先完成卷外B0、磁盘缩容/挂回验收、
   新备份卷与B1恢复验证；随后按逐版本门禁迁移和观察，私网验收
   新前后端serving与schema/币制契约；writer和真实辅助写入保持停止，直到联合open阶段。
   保持维护入口直到组合验收全部通过，金额迁移必须在start-target之前完成。
6. 在用户完整窗口执行授权内、联合验收通过后开放新组合，不逐阶段重复确认。
   不能使用契约不兼容的新旧控制台混跑10%/50%/100%；
   维护发布必须有明确获准的专用路径。随后完成即时/延迟状态、脱敏 Relay 错误窗口及恢复
   资产检查。普通应用回滚不能直接承诺恢复 v7；维护方案必须覆盖数据库恢复与停写边界。

上述生产授权要求来自全局 NewAPI 入口的“Schema 契约门禁”、infra runbook 19 的独立
schema apply 授权与 runbook 21 的不兼容契约维护边界，并与本轮“切流之前”的要求一致。

## 验证范围

本轮采用受影响包与关键流程验证。具体命令、最终结果和独立前端产物记录保存在本机
`.local-releases/rc41-preparation/verification.md`，供交付时审阅。

未运行 `go test ./...` 或整个 workspace 测试。前端全目录 lint/format 的首轮检查存在
未改动文件的既有错误；随后仅修复并检查本次改动范围，没有修改全站历史代码或关闭规则。
生产候选验收、真实业务数据副本恢复演练、维护事务的完整集成验收与生产公开切换均尚未完成。

## 维护窗口前的进一步准备

2026-10-05 的独立只读检查仍显示原版本 `HEALTHY`。本次取得的 schema-only dump 不含
业务行、owner 或 ACL，SHA256 为
`bd50712cfd527044be4be55e68c8b90f5a3610c35f26ef51a395ba066f0187ac`。
生产 NewAPI 专库约 7.9 GiB；此数值只用于容量准备，不是恢复时间承诺。

使用完整生产 DDL 和合成业务数据的 PostgreSQL 18.4 演练已通过：v7→v8→v9、重复迁移、
存量原列指纹、密钥/PAT/退休时间幂等，以及 `pg_dump` / `pg_restore` 恢复到 v7。
测试本体约 6 秒，仅证明合成数据上的行为，不能用来估计生产停机时间。
证据在 `.local-releases/rc41-preparation/maintenance-exact-schema-rehearsal/`。

维护准备构建使用现有 Mac builder，完整后端镜像内的 `/new-api` 与 `/kkai-migrate`
共享同一个不可变 image ID：

```bash
scripts/kkai/build-manual-release.sh \
  --prepare-maintenance --schema-contract feature --frontend-mode external \
  --planned-infra-sha REVIEWED_LOCAL_INFRA_COMMIT \
  --planned-deployment-protocol rc41-maintenance-v1
```

仅在生产 checkout 门禁满足后执行。输出 `.maintenance.json` 绑定源码提交/tree、镜像 ID、
archive/metadata SHA256、精确 `(9,9,9)` 与 PostgreSQL digest、console API 2，以及计划
安装的控制器。`planned-infra-sha` 不代表已经安装，不修改现行 application contract pin。
生成的 metadata 含 `release_purpose: maintenance-preparation`，普通 deploy wrapper
在任何 SSH/SCP 前拒绝它。

独立前端可复用已验证产物
`kkai-frontend-20261004.1791150534-1d46a87cf`，format 2 / API 2，archive SHA256
`a5f50ae72dea8466e6efe98e28da260dcb14a49678f92a88cc614d3db9e5e157`。
改变后端发布工具而未改变前端代码不要求重建它。

同一新版镜像的两槽及两份对称 manifests 已通过实际模板/Compose/现有对称校验演练。
这提供 v9 内的槽位故障切换，不能回退 rc.41 的代码缺陷。公开前恢复旧版需要完整旧
前后端与专库备份；一旦启动新版 writer 或接入公开流量，就不能再把旧备份恢复称为无损回退。

上述架构准备完成时，应用和infra中的美元额度迁移未跟踪文件原地保留，未纳入架构提交。
用户现已要求合并维护排期，后续应收敛联合源码、账务兼容与控制器变更，统一完成干净构建和
infra source-size门禁，不能隐式stage、ignore、删除或更改测试baseline掩盖。
本记录所述正式镜像仍未构建、维护工具仍未安装；生产动作待用户通知维护窗口。

联合顺序为：全体受影响业务一次停流/冻结、卷外B0、appdata缩容与数据验收、从释放VG
空间创建独立备份卷、B1多数据源备份/隔离恢复、v7→v8→v9、保持应用停止时迁移钱包/邀请
权益及定价、跨库对账和缓存处理、受控私网验收、统一开放。新备份卷不替代缩容前B0。

架构备份目前位于待缩容appdata内，主库大小×3+2 GiB的空闲门禁按历史数据需约25.7 GiB，
与16 GiB缩容目标冲突。必须提前实现备份/scratch/恢复全过程的新目录与挂载身份合同，
并核对受影响服务清单、现有路由操作顺序、币制一致性与辅助目标版本。
2026-10-06用户已明确取消额外断电/宿主机重启/实际缩容演练及通用共享维护框架开发；
这些额外项目不再是本次准备门禁，不以它们为由自行推迟维护时间。
现有架构`open`不能直接作为联合开放入口；Sub2API/CPA等更早恢复写入也会使整卷恢复失效。
健康16 GiB卷可继续承载旧业务恢复，不因应用失败自动扩回144 GiB或删除新备份卷。

原30–60分钟仅为架构排期建议，联合窗口按实际磁盘检查、B0/B1备份、迁移、全体服务验收
所需时间安排，不将取消的额外演练计入。本段原记录仅为文档阶段的历史状态；后续已进行
只读生产清点，但尚未缩容、创建备份卷、修改账务或发布。旧问题充值单保持hold，禁止补款。

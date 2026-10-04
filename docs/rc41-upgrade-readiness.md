# rc.41 架构升级：切流前准备记录

记录日期：2026-10-05。本文记录本轮证据，不代替下一次生产操作前的实时状态检查。

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

因此本轮没有上传、stage、canary、promote 或执行任何生产迁移。后端正式镜像构建也保留到
专用迁移流程和目标 schema 证据满足门禁后；前端独立产物可以在本地准备。

## 推荐后续方案：维护窗口内联合升级

这属于基础设施/schema 升级。现有正常蓝绿入口不能直接执行下面的流程；需要先实现并
验收专用维护事务。以下是待实现/待批准计划，不是已经具备的生产能力。

1. 在当前 infra 分支补齐 v9 的精确 contract、digest、snapshot、observe 与恢复校验。
   迁移器必须绑定审核过的不可变新版镜像，不能继续强制取旧 current 镜像，也不能仅在
   enum 中加入 9。保留旧版精确前后端组合与数据库恢复证据。
2. 实现持久化维护事务，明确维护路由、请求排空、唯一 writer 停写、旧/新前端选择和
   新/旧后端坐标。失败后继续保持维护状态，不向公众开放不匹配组合，不绕过常规入口。
3. 在隔离 PostgreSQL 环境演练与现场形状相同的 v7→v8→v9、重复执行和中断恢复。
   验证旧业务记录保留、新版登录/权限/计费/任务插件及 Studio 的最小流程；执行 infra
   `make syntax`、`make policy`，如改变 Ansible 行为再执行 `make check`。
4. 形成精确提交、镜像摘要、前端 metadata、操作计划与恢复记录后，单独确认生产维护窗口、
   数据库迁移与 controller 安装。不得在尚未确认公开切换时先迁移生产再长期等待。
5. 经确认进入维护窗口，排空并停写，创建和验证备份；按逐版本门禁迁移和观察，私网验收
   新前后端、writer 与 schema。保持维护入口直到组合验收全部通过。
6. 经用户明确确认后开放新组合。不能使用契约不兼容的新旧控制台混跑 10%/50%/100%；
   维护发布必须有明确获准的专用路径。随后完成即时/延迟状态、脱敏 Relay 错误窗口及恢复
   资产检查。普通应用回滚不能直接承诺恢复 v7；维护方案必须覆盖数据库恢复与停写边界。

上述生产授权要求来自全局 NewAPI 入口的“Schema 契约门禁”、infra runbook 19 的独立
schema apply 授权与 runbook 21 的不兼容契约维护边界，并与本轮“切流之前”的要求一致。

## 验证范围

本轮采用受影响包与关键流程验证。具体命令、最终结果和独立前端产物记录保存在本机
`.local-releases/rc41-preparation/verification.md`，供交付时审阅。

未运行 `go test ./...` 或整个 workspace 测试。前端全目录 lint/format 的首轮检查存在
未改动文件的既有错误；随后仅修复并检查本次改动范围，没有修改全站历史代码或关闭规则。
生产候选验收、真实业务数据副本恢复演练、维护事务的实现/故障恢复与生产公开切换均尚未完成。

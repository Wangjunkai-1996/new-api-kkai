# KKAI 发布安全与提速方案

评审日期：2026-10-21。范围：本地应用构建脚本、基础设施发布实现与操作手册。
后续实施已加入本地计时、只读恢复、资源/SSH实验开关和独立脱敏 HTTP 采集器。
没有访问生产、执行真实发布构建、修改已安装控制器或切换流量；本文不代表实时生产状态。
操作入口仍是 `/Users/tokk/Documents/Codex/runbooks/newapi-upgrade-and-deployment.md`。

## 推荐结论

保留现有生产门禁，优先省去不需要的整个构建/发布阶段，复用精确产物和有效验收证据，
再根据分段计时调整构建资源。不要先提高全局并发或缩短观察窗口。

发布工程、SRE、AI 网关业务可靠性三个独立角色完成评估并交叉讨论后，形成以下优先级：

| 顺序 | 动作 | 收益与条件 | 当前状态 |
| --- | --- | --- | --- |
| 1 | 按变更范围选择交付路径 | 纯前端不构建/灰度后端；format 2 兼容后端更新不构建或切换前端 | 源码已有能力；发布时确认实际安装能力 |
| 2 | 保留缓存，失败复用精确产物 | 避免重复编译、导出和重复生成发布版本 | 既有能力与操作规则；不改变产物校验 |
| 3 | 在既有等待窗口完成检查，复用本次有效业务证据 | 减少串行空等和重复收费请求 | 本次完善验收记录要求；仍需执行者按窗口落实 |
| 4 | 增加轻量分段计时和本地恢复记录 | 找到真实瓶颈，减少人工操作及中断后的重复劳动 | 已实现构建/stage sidecar、人工阶段计时、只读恢复入口 |
| 5 | 一次受控构建试用 4 CPU / 4GiB | 有可能降低 CPU 限制造成的编译耗时；必须实测 | 已实现 balanced 显式档位，默认不变；真实实验待授权构建 |
| 6 | 依计时结果选择 SSH 复用、传输压缩或缓存层调整 | 只优化已确认的瓶颈 | SSH 两项独立开关已实现且默认关闭；缓存层调整仍等待数据 |
| 7 | 增加批准的按槽脱敏 HTTP 聚合 | 改善灰度质量与异常判断 | 独立本地 reader 已实现，定向与完整 policy 通过；现场验收未执行 |

当前没有真实发布分段计时，不能承诺具体分钟数或加速比例。

## 本次已完成

### 构建输入与来源一致

- 后端从记录的精确 commit 通过 `git archive` 导出普通临时目录；Dockerfile、主 context、
  named context、console contract 和维护准备 helper 都来自同一快照。
- 前端的源码、锁文件、法律声明、console contract 和打包 helper 来自同一快照。
  生产构建不读取工作目录的 `.env` 或现有 `node_modules`。
- 前端用临时 Git index 检测 install/build hook 对跟踪文件的修改或非忽略新文件，
  不修改原 checkout 的 index；原 checkout 后续编辑或提交不污染快照。
- 生产构建仍要求开始时位于干净的 `production/kkrich`；不创建分支或 Git worktree。
  临时目录在成功或失败时清理。

### 工具链与操作规则一致

- 生产前端实际 Bun 版本必须等于目标提交的 `KKAI_BUN_VERSION`，metadata 记录真实版本。
  保留 Bun 全局缓存，并以 frozen lockfile 为快照重新安装依赖。
- 普通前端文档以 format 2 为主，将 legacy format 1 精确配对单列，避免混用流程。
- 删除普通入口中过期的 schema 固定版本选择规则。profile 含义由目标提交决定；
  必须匹配新鲜只读数据库版本、canonical digest 和所需扩展回执。
  min/max 区间不代表区间内每个版本都兼容，缺证据不能默认 `bridge`。
- 精确已安装控制器若已在改动 idle slot 前完成绑定本次 archive/image 的二进制契约及
  live-schema 校验，直接复用该证据，不再在 Mac 重复 load/run；先确认实际安装版本。
- 基础设施手册补充最小脱敏验收记录，区分版本身份、完整业务结果、样本量和统计证据。
  SSE 必须完整结束，异步任务必须看到终态及受影响结算；健康接口和 HTTP 202 不足以验收。

没有改动应用 controller pin、基础设施控制器逻辑、schema 或生产配置。
全局入口与应用/infra 规则已同步修正；此次修改仍为本地未提交内容。

## 最短安全交付路径

| 变更 | 构建/交付 | 必要验收 |
| --- | --- | --- |
| 文档、无运行时影响的说明 | diff 与命令接口核对 | 不访问生产，不构建 |
| 仅 default 前端 | 确认已安装 format 2 能力后构建一次，prepare/activate | 当前后端兼容；入口、版本化资源、缓存和受影响操作 |
| 后端，保持 format 2 前端兼容 | external 后端镜像一次；stage、候选、完整灰度、promote | 精确 image/schema/console contract，受影响后端流程，发布后观察与回滚 |
| 前后端联合 | 各构建必要产物；按兼容关系选择顺序，状态事务串行 | 相邻版本兼容；需要时明确过渡与回退顺序 |
| schema、Edge、控制器 | 单独进入对应版本手册 | 不混入普通应用发布 |

若实际仍为 legacy format 1，保留其精确配对协议，不能直接套用 format 2 快速路径。
兼容性、版本、槽位、健康和回滚状态必须在实际发布时从确认的 infra checkout 获取，
不沿用本文或旧对话记录。

后端观察等待下限为 `3 × 120 + 300 + 60 = 720 秒`，约 12 分钟：
10%/50%/100% 各至少 120 秒，晋级后观察 5 分钟，再保留 60 秒日志写入保护。
这不包含构建、上传、stage、候选验收、promote 和 drain；纯前端路径不套用后端灰度。
发布前 baseline 使用此前已发生的 300 秒日志窗口，不额外空等五分钟。

候选进入灰度前，仍须对精确版本完成最小受影响业务流程验收。灰度期间每档检查实际路由和异常、
更新验收记录；发布后五分钟内完成适合当时阶段的公网、静态资源和只读状态检查。不要先等完
观察时间，再开始所有可以在窗口内完成的检查。错误率、错误数或 `review_required` 本身不阻断
晋级，也不增加观察时间；归因与已有窗口并行。若灰度期间的错误有独立证据确认属于本次发布以外的
原因（如精确变更范围排除该失败路径及其共享组件、发布前已存在的同类故障、外部提供商或共享服务事故），
且证据能解释观测错误、没有候选特有
回归，则候选身份和健康门禁通过后，到达每档 120 秒下限即进入下一档。即使该原因令候选上的流程
暂时失败，也不重复采样或再次征求审批。不跳过 10%/50%/100% 任一级别。若变更路径重叠，检查
获批的脱敏错误证据和候选/未变更 active 槽同窗聚合；active 槽无可用样本不单独阻断，但剩余证据
仍须足以排除本次发布为原因。样本不足且因果仍不清楚时才 `inconclusive/hold`。

完整业务验收绑定本次 commit、image、schema/console contract 和相关策略。
身份与输入未变时可沿用本次有效结果，每档更新路由/错误证据，无须每档重复生成收费图像或视频。
重建、证据 TTL 过期、相关策略变化、回滚重启或未归因异常时，重新验证受影响部分。
候选 tunnel 结果不能证明公网灰度命中；固定凭据可能在 10%/50% 时始终命中旧 cohort。
低流量下记录统计样本不足；最少受控完整结果仅证明功能，不宣称统计成功率。

## 提速工具实施与后续验证

脚本用法见 [发布计时与安全恢复](../../scripts/kkai/release-record.md)。工具已经落地，
以下资源与网络收益仍待真实计时；按槽采集器的 infra 强制 policy 已通过，正式发布时
仍须使用精确干净版本并完成现场验收。

### 1. 分段计时和恢复记录

已给现有脚本补充本地计时，不另建发布平台。记录：source SHA、infra pin、version、
artifact checksum、字节数、阶段起止 UTC、耗时、结果。当前不猜测缓存命中概况；
BuildKit 编译/导出暂作为一个整体阶段，确有需要时再从受控日志细分。

构建/stage 自动拆分源码导出、依赖、BuildKit/前端 bundle、摘要/打包、preflight、上传、
load/stage；候选验收、三档灰度、promote/drain、状态和错误窗口用薄包装器计时。
先区分 CPU、网络和安全等待的占比。

恢复记录仅作索引，原子写入、权限 0600，不记录凭据、请求正文或客户信息。
恢复时先读官方 candidate/rollout 状态，再按精确 version、slot、rollout ID 继续；
`release-resume-status.sh` 只读官方 candidate/rollout 状态并核对显式坐标；
不复制远端状态机，不盲目重放 stage/promote，不额外增加用户审批。
记录初始化严格；之后的计时写入故障只告警并保留操作原结果，避免诱发重复操作。
完整发布授权继续覆盖正常验收与晋级，异常门禁仍须停止。

### 2. 单次资源实验

本机只读结果：Mac 为 10 逻辑 CPU / 16GiB，Docker 可用约 7.75GiB；
现有构建默认 quota 为 2 CPU、内存 3GiB，Go 并行度 4、媒体并行度 2。
在下一次已授权、不同精确提交的正常发布中，可试用以下参数：

```text
--resource-profile balanced
```

保持 Go `-p4`、media `-p2` 和重型步骤串行，不同时运行多个重型构建。
记录总耗时、缓存冷热、OOM/交换、桌面响应与产物验收结果；这些参数并非全部进程的总资源上限。
同既有、相近正常发布比较并注明缓存差异，不为跑分重建同一不可变 release。
只有在可比数据表明有收益且无资源回归后，才考虑修改默认值；不合适则恢复现有参数。

### 3. 按真实瓶颈追加优化

- 上传为主：分别验证 SSH 连接复用与 `scp -C`，保持控制器收到的原始 `.tar` 和 SHA 校验。
  socket 按发布隔离，候选 tunnel 独立管理并精确关闭；压缩收益必须抵过本机 CPU 成本。
- 编译为主：保留 `kkai-mirror-builder`、Go/Bun/FFmpeg 缓存，确认 cache miss 原因。
  当前 external backend 已跳过前端，Go/工具已采用原生构建平台交叉编译，不重复改造。
- 只有数据证明值得时，才调整 Docker `COPY . .` 或工具层。源码变更会使 RUN 重执行，
  但 Go 编译缓存仍可复用，不能当作每次全量编译；缩小输入必须覆盖 embed、工具与法律文件。
- 不复用来源不明的 live `node_modules`。未来如缓存已物化依赖树，需按 Bun、OS/架构、
  lockfile、workspace manifests 隔离并防止 hooks 污染，否则继续使用包缓存和 frozen install。

### 4. 独立增强灰度观测

当前 router 模板已有请求耗时字段。新增 `make newapi-slot-http-window` 使用固定只读程序
在服务器端聚合，只返回窗口、slot、容器/镜像/版本身份、完成请求数、5xx、延迟及样本量，
不返回原始行、动态路径、凭据或用户身份；校验身份前后稳定、日志前界、超时/大小和格式。
它不改已安装控制器，无需安装或变更 pin；infra 强制 policy 已完成，后续在正式发布
时通过精确干净本地版本与现场证据验收，不能将当前未提交代码当作已验收能力。

HTTP 聚合按槽提供整体数据，并增加 `relay_chat`、`relay_responses`、`relay_media`、
`relay_other`、`api`、`static`、`other` 七种固定粗类别的请求数和 5xx 数；不输出具体路由、
查询参数或业务流，也无法识别所有已经返回 200 的流式失败；
全局 Relay error-window 同样不绑定目标槽位。高错误率不是自动停止条件，也不能单独证明因果。
若独立证据确认错误来自发布前已存在的问题、外部故障、变更范围外且无共享影响的路径，或其他本次发布以外的原因，
并能解释观测错误，
就记为 `unrelated`。灰度期间即使失败路径与改动重叠、候选流程暂时不可用，也在每档 120 秒下限及
身份/健康门禁满足后进入下一档；晋级后则按原定五分钟观察、60 秒日志保护和状态检查完成验收，
不增加等待。路径重叠时，两槽同窗同类 5xx 是有用佐证，但 active 槽没有可用样本不单独否定足以
解释错误的独立证据；粗类别也不能单独归因。候选特有回归则按官方流程回滚。若变更可能影响该路径
且现有证据不能排除发布为因，才记为 `inconclusive/hold`，只在既有窗口内做一次聚焦复核，不延长
观察、不反复择窗。缺少指标如实记为 unavailable，不推断错误率为零。

## 始终保留的保护

镜像与来源身份、schema/console contract、产物摘要、frozen install、受影响业务终态、
三档灰度、快速错误归因、错误窗口、writer drain、回滚准备都继续保留。晋级后的五分钟观察、
60 秒日志写入保护及状态检查仍按原要求完成；外因确认只免除额外诊断等待，不免除既有安全门禁。
worker drain 可等待最多 24 分钟，旧槽还需连接排空；先记录原因，不强杀或提前发信号提速。

可并行的是资源允许的独立本地相关检查、只读核对和证据整理。
backend stage/rollout/promote/rollback、frontend prepare/activate、schema、Edge、
控制器安装和清理等共享生产状态事务继续串行，不能将跨切换身份的快照拼为成功证据。
不默认引入 registry/Kubernetes、全局 prune、`--no-cache`、新 archive 协议或更宽泛的重试。

## 本次验证与边界

以下定向检查已通过，回归脚本使用 mock Docker/Bun；前端另使用小型真实临时 Git 仓库：

```bash
bash -n scripts/kkai/build-manual-release.sh scripts/kkai/build-manual-release_test.sh
shellcheck -x scripts/kkai/build-manual-release.sh scripts/kkai/build-manual-release_test.sh
scripts/kkai/build-manual-release_test.sh
bash -n scripts/kkai/frontend-build-release.sh scripts/kkai/frontend-build-release-package.sh scripts/kkai/frontend-build-release_test.sh
shellcheck -x scripts/kkai/frontend-build-release.sh scripts/kkai/frontend-build-release-package.sh scripts/kkai/frontend-build-release_test.sh
scripts/kkai/frontend-build-release_test.sh
bash -n scripts/kkai/deploy-manual-release.sh scripts/kkai/deploy-manual-release_test.sh scripts/kkai/release-record.sh scripts/kkai/release-resume-status.sh
shellcheck -x scripts/kkai/deploy-manual-release.sh scripts/kkai/deploy-manual-release_test.sh scripts/kkai/release-record.sh scripts/kkai/release-resume-status.sh
scripts/kkai/deploy-manual-release_test.sh
python3 scripts/kkai/release-record_test.py
python3 scripts/kkai/release-resume-status_test.py
```

覆盖来源快照隔离、构建中原目录变化、清理、前端 hook 污染拒绝、Bun 版本拒绝和本地 opt-out。
还覆盖资源优先级、SSH/SCP 开关/独立 socket 清理、上传失败原退出码、计时写入故障不改变
操作结果、TERM 转发、只读恢复精确坐标和不确定 stage 不重试。
infra 的 `PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/test_newapi_slot_http_window.py -v`
通过 11 项 fixture/mock 回归，三文件内存编译通过。
经用户明确批准，以下 infra 完整本地策略回归通过（退出码 0），包含本地 Docker 演练、
新增采集器测试及全目标 ShellCheck；演练不访问生产：

```bash
make -C /Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/kkai-infra policy
```

最终交叉复核补齐了正常发布入口与工具文档的链接、按槽采集所需官方 JSON 状态命令及
身份字段映射。恢复工具在 metadata-only 模式下只接受无 rollout 状态；存在当前或历史
状态时要求明确的 rollout ID、版本和槽位，避免误认其他发布，相关定向回归已通过。
采集器的非法参数统一返回固定脱敏错误，回归覆盖环境变量、命令行值、未知参数和
不触发 SSH，并保留正常 `--help`。最终修正后按原授权重跑上述 `make policy`，
退出码 0；本地 Docker 演练的容器与网络已自动清理并只读确认。

两个仓库的 `git diff --check` 通过。应用全套测试未运行；未做真实依赖安装、前端 bundle、
Docker 发布构建、上传或线上验收，因此真实构建兼容性、耗时与加速效果仍需下一次授权发布验证。

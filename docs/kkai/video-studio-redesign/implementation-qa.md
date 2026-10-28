# 视频工作室重构 · 实施与验收记录

2026-10-28。状态：已在独立 worktree 整合商城发布基线，完成合并后的定向验证和审查修复。实现、播放器修复、脚本和证据均已形成可审查的本地提交，当前按用户完整上线授权进入 `production/kkrich` 整合与正式发布流程。真实服务与实机验证尚有未覆盖项；最终发布结果另以发布记录为准。

发布授权更新：用户随后明确“我自己测，你上线就行了”。用户接手业务测试，代理未验证真实上传、生成终态、签名续期和账单闭环；不再以缺少测试账号阻断正常发布。代码已完成的验证结论沿用，发布阶段由代理执行精确版本、入口/资源、缓存、健康、契约、错误证据和回退门禁。本轮源码及证据将提交合入本地 `production/kkrich` 后构建，最终发布状态另以发布记录为准。

回退准备：在 V2 之前发布保留旧界面的过渡前端，在旧 POST 发送边界识别新版未决存储，未决、未知版本或损坏数据均禁止生成。V2 激活后必须使用该安全过渡前端作为回退目标，不能直接选择原 `e44ae8d98` 产物；不得清空用户未决记录。V2 自身仍使用输入指纹和持久操作身份保护，过渡版全局阻止未决提交的保护只用于旧界面。

本记录是当前实现证据。[开发总纲](development.md)、[开发质量与验收方案](quality-and-acceptance.md)及[技术审查记录](development-review.md)保留编写时的计划与判断，其中“本轮仅文档”“未来实施”描述不代表当前交付状态。设计原型检查、代码检查、真实 React 页面检查、真实服务验收是不同证据，不互相替代。

## 1. 范围与基线

- 用户已授权按设计实施，并要求隔离另一个对话的上线任务。实现位于独立工作区 `/Users/tokk/.codex/worktrees/video-studio-v2/new-api-kkai`（detached HEAD）。原基线为 `1ead6eb1fccbdc766b009aab755237d6f41d220e`，实现提交为 `64ac1d76d`；商城发布后的本地生产基线 `e44ae8d98` 已合并为 `ccbeea3b5`，补齐两份测试的提交为 `f6b284f85`。原主仓库保持干净。
- 商城对话 `01a17c9e-dd5d-73f2-a7fc-cfd4f5b69ccc` 的最终交付记录与用户确认满足计划启动条件；这只证明可以继续本任务，不替代视频工作室的真实业务验收或发布授权。商城生产状态不从本文推断。
- 七份 locale 按键整合，保留商城原有值和视频新增 74 键；商城导航、生成路由与后端接口保持合并基线行为。最终范围仍为 default 前端及文档，没有新增后端差异。
- 需求依据为 [PRD v1.2](prd.md) 的 VS-01～VS-20；视觉依据为 [UI 规范](ui-spec.md)及暗色 V2 [设计稿](design.html?theme=dark)。正式页面沿用应用主题，暗色与浅色均有实现，不强制覆盖全站主题。
- 改动涉及 Video Studio 创作、作品库、任务页、相关路由、提交恢复与私有媒体边界。共享媒体授权修正同步覆盖图片工作室直接消费者。
- “视频作品”用于参考现有组件、媒体生命周期和通用基础设施；没有将 Studio 数据强转为 `TaskLog`，没有合并 `/usage-logs/task`，没有对视频作品模块进行扩展性重构。
- 没有新增运行时依赖，没有修改后端接口、数据库、计费公式或生产配置；本轮没有生产业务写入或付费生成。

## 2. 已实现内容

| 开发批次 | 实际落地 | 当前证据与边界 |
| --- | --- | --- |
| P0 契约与基线 | 固定产品、接口与设计依据；隔离工作区，明确各模块编辑所有权 | 文档、代码核对及独立技术审查；不以历史原型截图证明功能完成 |
| P1 提交与恢复 | 运行时校验报价/回执；规范化输入指纹；按用户保存带版本的未决操作；POST 前持久化；未知结果只读恢复；最终发送前身份/输入门控 | 提交协议、store、hook 和真实 HTTP adapter 边界的定向测试通过；精确对账与跨标签页原子互斥仍受第 7 节限制 |
| P2 创作台与路由 | 左侧单编辑器、右侧当前成果；模型/模式、画幅、常用与高级参数；独立费用区；近期选择；创建后留页；已知目标与仅任务号定位 | 页面、参数、路由、查询/目标选择测试；浏览器确认小屏仅一个编辑器 |
| P3 媒体与公共边界 | Studio 播放器、详情与状态组件；`contain`；稳定媒体身份与签名地址分离；一次自动恢复及手动重试；互斥与释放；拒绝证据立即隐藏旧媒体 | 播放器/共享签名及图片消费者回归；本地 MP4 可解码并播放，未验证真实存储或上游媒体服务 |
| P4 作品库与任务页 | 服务端全部/ready 筛选、已加载记录日期分组、新结果提示与主动接纳、分页；详情；终态删除与回焦；任务状态、复制任务号与定向入口 | 查询、列表顺序、删除及目标测试；真实 React 页面截图。服务端删除并发边界未新增能力 |
| P5 视觉与可访问性 | 暗色 V2 分层表面、控件与局部动效；电影器材插画；窄屏布局；减少动态效果；七语言新文案 | 浏览器核对设计尺寸、颜色、320/390px、减少动态效果和浅色主题；完整实机、软键盘与可访问性矩阵尚未全部完成 |
| P6 集成审查 | 分角色审查提交、查询/删除、媒体、表单/视觉及共享消费者；修复下表问题；汇总本地检查与剩余限制 | 受影响测试、前端类型检查、局部 lint/格式、i18n 和浏览器验证通过；不宣称全部发布门槛已完成 |

创作区装饰使用 [cinema-tools-still-life.webp](../../../web/default/src/assets/video-studio/cinema-tools-still-life.webp)。城市骑行画面仍为设计与浏览器 fixture 的示例封面，不能填补缺失的用户作品媒体。真实作品卡片不挂载隐藏播放器，只在选中成果或打开详情时挂载所需播放器。

## 3. VS-01～VS-20 实现追踪

下表中的“测试”指第 5 节已执行定向集合内的覆盖；“浏览器”指本地 API fixtures 驱动的真实应用。未覆盖的真实环境条件在第 7 节统一列出。

| 需求 | 实现与检查入口 | 已有证据 / 尚未证明的部分 |
| --- | --- | --- |
| VS-01 权限与准备 | `use-video-token-gate`、`video-studio-entry-gate`、`video-create-page` | 权限准备不闪现错误空态，旧模型快照刷新失败仍保留工作区；token gate / create-page 测试通过。真实权限后端未联调 |
| VS-02 模型/模式 | `video-composer`、`video-domain`、参数与素材组件 | 按真实 profile 暴露模式并校验兼容输入；模型/模式及参数测试通过；首尾帧浏览器截图 |
| VS-03 提示词/参数 | `video-domain`、`video-submission-protocol`、`video-parameter-fields` | 8000 UTF-8 字节上限，保留显式 false/0；空数值不退回上次可付费值；无效高级项展开；协议/表单/参数测试通过 |
| VS-04 素材角色/上传 | `video-asset-uploader`、既有 upload lifecycle/protocol/reference hydration | 保留 reference、reference_video、first_frame、last_frame 角色和既有恢复协议；相关定向测试通过。未执行真实文件上传与分片续传 |
| VS-05 草稿保护 | 既有用户草稿 store、`video-composer`、`video-create-page` | 套用样片/不兼容切换有确认，切换成果不替换编辑器或草稿；页面与表单测试覆盖。真实软键盘与上传中跨断点尚待实机 |
| VS-06 报价区 | `video-composer`、`use-video-submission`、`video-submission-protocol` | 独立费用呈现；报价与冻结输入对应；过期禁提交且可重试，不无限自动刷新；迟到/无效报价测试通过。真实金额与后端账单未验证 |
| VS-07 提交 | `use-video-submission`、`api`、`queries` | 仅明确点击触发；POST 不自动重试或认证刷新重放；接收后留页、保留草稿并选择新目标；hook、API 和页面测试通过 |
| VS-08 未知结果 | `video-studio-submission-store`、提交协议/hook、`video-submission-notice` | 先持久化后发送；损坏/写入失败关闭发送；改稿再改回及 24h 后仍保留等价未决保护；迟到响应只更新原操作；有效回执或同任务归档/ready 才结束未知。仅 queued/processing/failed 不足以消除未知 |
| VS-09 任务定位 | `queries`、`use-video-generation-target` | task 字符串与数字 generation 分开；30 秒发现窗口与手动重启；已知 generation 走详情观察，不伪造 ID。目标测试通过 |
| VS-10 选择/多任务 | `use-video-generation-target`、`queries-target`、create/library 页面 | 明确目标与已选目标优先；后台新项不抢选；活跃目标移出 head 仍由同一 owner 观察；历史失败不阻断已知目标；目标/查询/页面测试通过 |
| VS-11 真实阶段 | `video-generation-status`、创作成果区和任务页 | queued、processing、archiving、ready、failed 分开，归档不提前播放；不编造百分比/预计时间；生成/归档真实 React 截图。真实供应商阶段更新未联调 |
| VS-12 媒体呈现 | `video-generation-player`、`video-generation-details`、样片/成果媒体所有权 | 横竖方 `contain`；切换/关闭/离页释放；主成果、样片、详情互斥；播放器测试及本地 MP4 浏览器检查通过。Safari 与真实流媒体传输未验证 |
| VS-13 媒体恢复 | 播放器、`studio-media`、页面 refresh 回调 | 正常签名更新保留媒体身份；一次自动恢复后手动重试；refetch 有 error 时不复用保留的旧 data 伪装成功；拒绝授权立即撤下旧来源；播放器与共享媒体测试通过 |
| VS-14 灵感 | `video-sample-gallery`、`video-composer` | 保留模型/能力支持的分类、分页和预览；套用保护；折叠后关闭 resize 观察、虚拟化和自动加载，保留筛选；样片测试通过 |
| VS-15 作品库 | `video-library-page`、`video-library-order`、`queries` | `status=ready` 在服务端分页前过滤；仅已加载数据分组；新 head 不抢顺序，主动刷新接纳；分页追加不混入未接纳新项；列表顺序与查询测试通过 |
| VS-16 删除 | library 确认层、delete mutation | ready/failed 才有入口，确认时重查最新状态；失败保留；成功取消/移除各筛选缓存、清选择/媒体、记录 tombstone 并回相邻项；删除测试通过。后端仍无终态强制约束 |
| VS-17 任务页 | `video-tasks-page`、目标路由 | 状态、公开原因、时间与任务号；复制任务号；ready 前往库，其他阶段前往创作定位；保留刷新失败快照，无全历史统计；浏览器截图与相关查询/路由测试。任务页全部交互未逐项实机验证 |
| VS-18 响应式/导航 | create/library 路由 schema、单编辑器、局部 CSS | `sample` 兼容；task/generation 严格校验；拒绝/删除/暂未定位区分；路由 11 项测试通过；320/390px 无横向溢出且一个编辑器。所有设计宽度与实机键盘矩阵未全部覆盖 |
| VS-19 用户/安全 | 页面的 owner/sid 边界、提交 epoch、私有 query key、共享签名拒绝证据 | 账号 A→B→A 不恢复旧操作发送资格；异步完成检查身份；401 和逐路径拒绝不展示旧媒体，包括图片消费者本地快照；相关测试通过。无跨设备恢复保证 |
| VS-20 无障碍/i18n | Base UI 组件、语义标签/焦点、`video-studio.css`、七语言 locale | 语义/删除回焦相关测试；减少动态效果 0s 浏览器证据；74 个新键覆盖七语言且占位一致。完整键盘、读屏、对比度、200% 文字与所有语言长文案审查未全部完成 |

上述入口均位于 [Video Studio 源码](../../../web/default/src/features/video-studio)、[共享媒体工具](../../../web/default/src/lib/studio-media.ts)、[未决 store](../../../web/default/src/stores/video-studio-submission-store.ts)、[路由](../../../web/default/src/routes/_authenticated/video-studio)及[样式](../../../web/default/src/styles/video-studio.css)。

## 4. 技术审查修复记录

| 审查问题 | 已落地修复 | 回归依据 |
| --- | --- | --- |
| 数值清空仍可能沿用旧值；画幅键存在别名 | 空输入保持无效态并阻止报价/提交；`ratio` 与 `aspect_ratio` 均显示画幅控件，传输保留原键 | parameter-fields / composer 测试 |
| 隐藏灵感仍观察尺寸、虚拟化和分页 | 可见性作为显式边界，折叠时停用相关工作，保留用户筛选 | sample-gallery 测试及实现复核 |
| 深链接活跃目标移出最新页后失去更新；后台新项抢选择 | 同一页面 query owner 观察明确 generation；初始/回退选择锁定，用户显式操作才更换 | queries-target / target hook / create-page 测试 |
| 删除确认依据陈旧对象；迟到列表可能复活已删除项 | 确认读取最新状态；取消列表/详情请求，清所有筛选缓存并保存本页 tombstone | library-page / queries 测试 |
| 授权撤销后旧签名、外部地址或图片过渡快照仍可显示 | 共享 hook 输出身份/逐路径拒绝证据；消费者立即撤下来源和本地快照，不能用原 URL 降级绕过拒绝 | studio-media 与图片 creation-results/library 测试 |
| refetch 失败仍带旧 data，可能误报续签成功 | 先检查 `result.error` 与拒绝证据，再接受返回 data | 媒体生命周期测试及页面回调复核 |
| 异步拦截器期间身份/输入改变；认证刷新重放付费 POST | 最终同步 guard 放在 HTTP adapter 实际发送前；该 POST 使用 `skipAuthRefresh`，mutation 明确 `retry:false` | api-submission 的真实 transport 边界测试 |
| 报价过期触发循环；未知结果被临时状态误解锁 | 过期显式重试；未知仅由有效回执或同任务 archiving/ready 证据结束，保留长期等价输入保护 | use-video-submission / protocol / store 测试 |
| ready 视频首次签名尚未返回却播报预览故障 | 创作页与作品库透传首次签名加载状态；等待时显示 status 并隐藏播放入口；失败/拒权仍保留不可用态；健康来源不因续期重载 | player 新增回归、真实 React 初始签名延迟断言 |
| 作品库异步签名回执只依赖卸载清理 | 下载、详情续期及刷新回执后显式复核当前账号；与现有账号/会话 key 卸载保护共同生效 | 页面定向回归及独立差异审查 |
| 作品库截图在海报异步解码前拍摄 | 脚本等待三张海报加载并 decode 后再截图 | 浏览器零错误通过及截图人工复核 |

复核还检查了单编辑器跨断点、素材角色、失败不误称空数据、不可用目标不回退冒充其他成果、局部动效和公共组件复用边界。审查结论不扩大已执行测试或浏览器证据的覆盖范围。

## 5. 已执行验证

以下是实施负责人确认的实际结果，不是待执行清单。前端命令工作目录为 `web/default/`，浏览器脚本与 `git diff --check` 在工作区根目录执行。

```sh
bun run test src/features/video-studio src/lib/studio-media.test.tsx src/stores/video-studio-submission-store.test.ts src/features/image-studio/components/image-creation-results.test.tsx src/features/image-studio/image-library-page.test.tsx
bun run typecheck
bun run test src/features/video-studio/video-target-route.test.ts
```

- 商城基线整合后定向测试：**28 个文件，251 项通过**。路由 schema 的 11 项包含在此集合内，不额外累加。本轮加载态修复后只重跑下方受影响三文件，**28 项通过**（包含新增回归），未为凑总数重跑已通过的其他文件。
- 完整前端类型检查：`bun run typecheck` 通过；已获得用户授权，不能称为文件级类型检查。
- 局部 lint：由 Python 合并 `git diff --name-only` 与 `git ls-files --others` 的本任务文件集合，向 `bunx --no-install oxlint -c .oxlintrc.json` 传入 **50 个 TS/TSX 文件**，通过。
- 局部格式：对 **51 个 TS/TSX/CSS 文件**执行 `bunx --no-install oxfmt --check`，通过；受保护版权头保持原样。没有将仓库 wrapper 冒充局部检查。
- i18n：`python3 /tmp/video-studio-i18n-check.py` 通过；合并后检查 207 个 Studio 静态键及动态键、新增 **74 键 × 7 语言**（en、zh、zh-TW、fr、ja、ru、vi）、商城旧值不变和插值占位一致。本次加载文案复用已有 `Loading...`，七语言均存在；没有新增 locale 写入。该检查脚本是本次本地临时审计工具，不是仓库长期工具。
- `git diff --check` 通过。
- **未运行全测试套件、全仓库格式检查、生产构建或后端测试。** 一次文件选择脚本路径错误导致 oxlint 意外扫描全仓并报告既有问题；修正选择路径后上述 50 文件定向 lint 通过，未改动无关文件。完整前端 typecheck 已获用户授权并通过。本地通过不能代替正式发布所需的构建与验收。

本轮修复后实际执行的命令（工作目录 `web/default/`），均退出 0：

```sh
bun run test src/features/video-studio/components/video-generation-player.test.tsx src/features/video-studio/video-create-page.test.tsx src/features/video-studio/video-library-page.test.tsx
bun run typecheck
bunx --no-install oxlint -c .oxlintrc.json src/features/video-studio/components/video-generation-player.tsx src/features/video-studio/components/video-generation-player.test.tsx src/features/video-studio/components/video-generation-details.tsx src/features/video-studio/video-create-page.tsx src/features/video-studio/video-library-page.tsx
bunx --no-install oxfmt --check src/features/video-studio/components/video-generation-player.tsx src/features/video-studio/components/video-generation-player.test.tsx src/features/video-studio/components/video-generation-details.tsx src/features/video-studio/video-create-page.tsx src/features/video-studio/video-library-page.tsx
```

路由类型泛型修正后单独重跑上述路由测试，11 项通过。局部 lint/格式的文件选择方式如下（工作区根目录）；这保证新增但尚未提交的文件也在检查范围内：

```python
from pathlib import Path
import subprocess

tracked = subprocess.check_output(
    ['git', 'diff', '--name-only', '--', 'web/default/src'], text=True
).splitlines()
untracked = subprocess.check_output(
    ['git', 'ls-files', '--others', '--exclude-standard', 'web/default/src'], text=True
).splitlines()
files = sorted(set(tracked + untracked))
typescript = [str(Path(p).relative_to('web/default')) for p in files
              if Path(p).suffix in ('.ts', '.tsx')]
formatted = [str(Path(p).relative_to('web/default')) for p in files
             if Path(p).suffix in ('.ts', '.tsx', '.css')]
subprocess.run(['bunx', '--no-install', 'oxlint', '-c', '.oxlintrc.json', *typescript],
               cwd='web/default', check=True)
subprocess.run(['bunx', '--no-install', 'oxfmt', '--check', *formatted],
               cwd='web/default', check=True)
```

初次格式检查发现 CSS 换行需调整，执行 `bunx --no-install oxfmt --write src/styles/video-studio.css` 后，`bunx --no-install oxfmt --check src/styles/video-studio.css` 通过；其余 50 个已通过文件未因该操作改变。

## 6. 真实 React 视觉与媒体证据

本轮使用 [visual-qa.mjs](visual-qa.mjs)检查真实 React 路由，拦截本地 API 并提供显式 fixtures；独立的静态 [design.html](design.html?theme=dark)仅是设计参照。开发服务器与浏览器脚本实际命令：

```sh
# 工作目录：web/default/
VITE_REACT_APP_SERVER_URL=http://127.0.0.1:9 bun run dev --port 4875

# 工作目录：工作区根目录
ffmpeg -hide_banner -loglevel error -f lavfi -i testsrc2=size=640x360:rate=24 -t 3 -an -c:v libx264 -pix_fmt yuv420p -movflags +faststart -y /tmp/video-studio-qa.mp4
QA_NODE_PACKAGE=/Users/tokk/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/package.json node docs/kkai/video-studio-redesign/visual-qa.mjs
```

浏览器脚本使用本地合成的 3 秒 MP4 验证媒体解码，最终运行退出码为 0，共记录 10 组证据。服务地址、运行时路径与媒体 fixture 为该机器的检查环境，不是产品配置或真实后端。验收完成后停止 4875 开发服务，原 4874 设计服务未改动；保留 17 张正式实现截图。

浏览器脚本检查及 [browser-evidence.json](screenshots/implementation/browser-evidence.json)记录本次 Chromium 156 的结果：

| 检查项 | 已确认结果 |
| --- | --- |
| 暗色基调与布局 | 页面 `rgb(17, 19, 21)`，强调色 `#b1bcff`；编辑器 400px，列间距 22px |
| 当前视频 | `readyState=4`，时长 3 秒，原生 controls 可用，`object-fit: contain` |
| 1024/768/390/320px | 无横向溢出；仅一个 composer；编辑器和成果分别留有截图 |
| 初次签名等待 | 阻塞首次签名回执时显示 status，无错误 alert、无播放入口；回执返回后恢复播放入口 |
| 作品库海报 | 三张图片完成 decode 后截图，人工复核可见 |
| 详情媒体互斥 | 打开详情时主成果播放器为 0，避免两个活动播放来源 |
| 减少动态效果 | 实际 transition 为 `0s` |
| 浅色主题 | 页面 `rgb(246, 247, 250)`，强调色 `#4c63da` |
| 错误与付费调用 | 无 console/page errors；`generationPosts=0` |

主要截图：[桌面创作](screenshots/implementation/dark-create-desktop.png)、[390px 编辑](screenshots/implementation/dark-create-390.png)、[390px 成果](screenshots/implementation/dark-result-390.png)、[320px 编辑](screenshots/implementation/dark-create-320.png)、[320px 成果](screenshots/implementation/dark-result-320.png)、[作品库](screenshots/implementation/dark-library-desktop.png)、[详情](screenshots/implementation/dark-details-desktop.png)、[任务](screenshots/implementation/dark-tasks-desktop.png)、[首次进入](screenshots/implementation/dark-empty-desktop.png)、[生成中](screenshots/implementation/dark-processing-desktop.png)、[归档中](screenshots/implementation/dark-archiving-desktop.png)、[首尾帧](screenshots/implementation/dark-first-last-frames.png)、[浅色首次进入](screenshots/implementation/light-empty-desktop.png)。

截图用于核对设计落实，不表示严格像素差分通过，也不表示原型的每一个模拟场景都已完成真实服务验收。fixture 测试验证布局、渲染和本地媒体行为，不能证明上游任务、真实上传或实际结算。

## 7. 未覆盖项、能力限制与交付边界

1. **真实服务**：尚未联调真实后端权限、报价、供应商任务、真实上传/分片恢复、签名过期/存储故障或实际账单。本地无隔离后端监听/容器，未发现可用测试账号。`GET /api/video-studio/token` 对 legacy token 可能写库修复，创作页缺 token 时会自动 POST 创建；不能直接打开生产页面并称其严格只读。需要独立库、预置账号/token/作品后，允许模型/分页/详情查询、无计费报价及媒体签名，阻止生成、上传、删除与 token 创建。没有进行付费真实生成；不将 fixture 的零错误解读为生产链路通过。
2. **实机与可访问性**：未覆盖 Safari、真实设备软键盘、全部设计宽度/侧栏组合、完整键盘和读屏流程、200% 文字、所有语言长文案以及完整对比度/触控尺寸审计。已有 jsdom 语义测试与 Chromium 布局证据只证明对应检查点。
3. **未知提交能力**：现有 API 无法精确定位所有无 task_id 的未知操作；无证据时持续待核实，不自动重发，也不在 24 小时后换新身份。未决本地存储不是永久账本；用户清理浏览器数据或跨设备时没有可靠恢复保证。
4. **并发范围**：不同标签页可非原子读改写同账号 localStorage，生成不同 operationId，甚至后写覆盖前一笔未决记录；服务端按幂等键去重，无法合并不同键的同内容请求。该既有能力边界须纳入上线说明，不能宣称跨标签页原子去重或全局防重复扣费。
5. **删除服务端边界**：前端确认时重新校验终态，但后端当前没有终态强制检查。确认与 DELETE 之间的服务端竞态不是前端可以完全消除的保证。
6. **明确不扩展的能力**：没有完整历史输入再生成、全历史搜索/状态统计、取消上游任务或退款能力，不用页面表现暗示这些服务已存在。
7. **回退**：新未决记录包含版本。回退到不识别这些记录的旧前端可能重新开放重复提交；存在未决记录时需保留可识别保护提示的安全版本或前向修复，不能清空 store 作为修复方案。
8. **交付状态**：本地实现和商城基线已在独立 worktree 固化，本轮修复及证据尚未提交；没有合入生产分支、推送、构建生产镜像或发布视频工作室。发布需另按维护中的 NewAPI 手册、当时后端 manifest、安装能力及实际发布范围执行，本文不声明当前生产版本、槽位、健康或回滚就绪。存在新版未决记录时禁止直接回退到 `e44ae8d98` 旧前端，必须保留识别该记录的安全版本或前向修复。

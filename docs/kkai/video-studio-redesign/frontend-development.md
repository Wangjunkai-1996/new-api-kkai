# 视频工作室前端开发设计

版本：1.0 · 2026-10-28。状态：开发方案，尚未实施。行为以 [PRD](./prd.md) 为准，视觉以 [ui-spec v1.2](./ui-spec.md) 和暗色 V2 原型为准；代理审查不代表用户已经批准设计。本文按当前源码核对，不把原型演示当作已存在功能。

实施范围是 `web/default` 用户侧的创作、作品库、任务三个页面。保留模型管理、样片管理、上传协议与计费接口。优先直接使用现有模块；只有被两个实际场景共用的行为才抽取，不建立跨图片/视频/日志的“统一工作室框架”。

## 1. 源码现状与必须补齐的差异

以下源码链接均从本文件定位；文件内符号是定位依据，不依赖容易漂移的行号。

| 位置 / 符号 | 当前实现 | 本轮改造与验收重点 |
| --- | --- | --- |
| [video-create-page.tsx](../../../web/default/src/features/video-studio/video-create-page.tsx) `VideoCreatePage` | 样片墙为主；桌面右侧编辑器；1180px 视口断点切 Drawer/Sheet；回执跳转 library | 改左编辑、右成果；单棵编辑器树；提交成功/未知均留在 create；权限准备局限于编辑区域 |
| [video-composer.tsx](../../../web/default/src/features/video-studio/components/video-composer.tsx) `VideoComposer` | RHF/Zod、300ms 报价防抖、参考素材恢复、组件内幂等 ref 和 submissionLock；切样片直接 reset | 保留字段/上传能力；拆清提交生命周期；覆盖确认；费用独立；未决保护不能随输入或样片变化清空 |
| [queries.ts](../../../web/default/src/features/video-studio/queries.ts) `useVideoGenerations` | 分页历史、头页轮询、已观察活跃任务详情补查、删除缓存清理 | 每页一个查询所有者，子区接收结果；保留已离开头页任务观察；补充库内显示顺序稳定，不另起一套轮询 |
| [video-library-page.tsx](../../../web/default/src/features/video-studio/video-library-page.tsx) `VideoLibraryPage` | 回执任务号 30 秒发现窗口、卡内单播放器、终态删除确认 | 增加全部/可播放筛选和日期分组；只给当前详情挂载播放器；复用发现语义和删除清理，不丢失目标提示 |
| [video-generation-card.tsx](../../../web/default/src/features/video-studio/components/video-generation-card.tsx) `VideoGenerationCard` | 播放器与卡片耦合，`object-cover`；重试只重新加载同一来源 | 卡片只负责缩略图/状态/选择；播放器移为 Studio 内共用组件，主视频 contain，并接真实签名刷新 |
| [video-tasks-page.tsx](../../../web/default/src/features/video-studio/video-tasks-page.tsx) `VideoTasksPage` | 已有状态列表及媒体签名读取 | 改为任务信息列表；ready 提供定位作品入口；不为每条任务签发不用的 video URL |
| [video-sample-gallery.tsx](../../../web/default/src/features/video-studio/components/video-sample-gallery.tsx) `VideoSampleGallery` | 模型/能力控制的分类筛选、虚拟列表、最多两个 warmed preview、单活动样片 | 保留筛选/分页/预览能力；接入页面媒体互斥；收起不丢筛选；明确新容器高度和虚拟列表滚动所有权 |
| [video-studio-draft-store.ts](../../../web/default/src/stores/video-studio-draft-store.ts) | 用户隔离的草稿与续传记录，写入失败为 best effort | 草稿照用；付费前未决记录必须使用能报告失败的独立持久化路径，不能直接调用吞错 setter |
| [studio-media.ts](../../../web/default/src/lib/studio-media.ts) `useStudioMediaUrls` | 按用户/路径去重，100 条分批签名、到期前更新 | 继续使用；补齐主动失败续签、播放不被后台签名打断、账号切换/删除清理验收；不得视作已全部满足 |

当前 Studio 没有作品详情弹层：库内播放在 `VideoGenerationCard`。设计中的详情应复用公共 `Dialog` 基座，新增只接收 `VideoGeneration` 的详情内容；不能宣称已有 `VideoWorkPreview` 可原样移入。

## 2. “视频作品”的参考与复用决策

参考对象是 [`features/usage-logs/video-works`](../../../web/default/src/features/usage-logs/video-works/video-works-page.tsx)。它处理 `TaskLog`，包含公共 API 视频任务、管理视角、计费状态和多产物；Studio 处理当前用户的 `VideoGeneration`。二者能复用 UI 基础设施与真实成熟做法，但不是同一个业务数据源。

| 分类 | 已核对来源 | 本轮决定 | 边界与理由 |
| --- | --- | --- | --- |
| 直接复用 | [Dialog](../../../web/default/src/components/dialog.tsx)、[Button](../../../web/default/src/components/ui/button.tsx)、Tooltip、AlertDialog、Empty、Skeleton、StatusBadge | 使用现成 Base UI 组合，不写第二套弹层/焦点陷阱/按钮体系 | 通过 Studio class/token 实现设计；默认 Button 尺寸不足 44px，显式设定本页命中区；Dialog 自带 100ms 动效需按本页规范覆盖 |
| 直接复用 | [use-copy-to-clipboard.ts](../../../web/default/src/hooks/use-copy-to-clipboard.ts)、[copy-button.tsx](../../../web/default/src/components/copy-button.tsx) | 复制完整任务号、成功/失败反馈沿用公共实现；需要 Hugeicons 外观时组合现有 hook + Button/Tooltip | 不复制 `VideoWorkCopy` 的日志 CSS 与 `icon-xs` 尺寸，不为这几十行引入跨 feature 依赖 |
| 直接复用 | [format.ts](../../../web/default/src/lib/format.ts)、[dayjs.ts](../../../web/default/src/lib/dayjs.ts) | 日期、相对时间、quota 兜底格式用项目工具 | 报价优先 `display_amount`；不套用日志账单的 `amount_available` 或臆造实扣金额 |
| 直接复用的素材 | [视频作品素材说明](../video-works/assets/README.md)、`src/assets/video-works/{empty,processing,failed}.webp` | 真空/进行中/明确失败可使用既有透明插画，按 V2 尺寸低调呈现，不重复生成或复制文件 | `expired.webp` 仅后端明确过期时可用；封面丢失不能用示例汽车、海景或城市图代替 |
| 小范围抽取，限 Studio | [video-generation-card.tsx](../../../web/default/src/features/video-studio/components/video-generation-card.tsx) 播放生命周期；卡片/任务页重复状态映射 | 形成 `VideoGenerationPlayer` 供创作成果和库详情两个实际调用方；状态展示组件供三页使用 | 不先改日志播放器为通用适配器；不让展示组件自行查模型、签名或轮询 |
| 行为参考 | [video-work-preview.tsx](../../../web/default/src/features/usage-logs/video-works/video-work-preview.tsx) 原生 controls、playsInline、metadata、状态局部降级 | 使用原生可达播放器，局部错误保留任务信息；通过公共 Dialog 展示详情 | 不照搬 `key={source:revision:...}`；签名变化会重建播放器/丢失播放位置，不满足本轮 VS-13 |
| 行为参考 | [use-video-work-artifacts.ts](../../../web/default/src/features/usage-logs/video-works/use-video-work-artifacts.ts) `renewMedia` | 借鉴一次自动续签、合并并发恢复、手动重试、401/403 隐藏媒体 | Studio 仍调用 `useStudioMediaUrls`，不引入 `getTaskArtifacts`、hosted/legacy fallback 或 artifacts 查询键 |
| 行为参考 | [use-selected-video-work.ts](../../../web/default/src/features/usage-logs/video-works/use-selected-video-work.ts) | 选中记录离开当前列表后独立读取；权限/删除证据优先于旧快照 | 使用 Studio 数字 generation ID 详情接口；日志按 task_id 搜索的接口不能替代 Studio 任务发现 |
| 行为参考 | [video-work-item.tsx](../../../web/default/src/features/usage-logs/video-works/video-work-item.tsx)、[video-works.css](../../../web/default/src/features/usage-logs/video-works/video-works.css) | 封面入口、状态插画、信息密度、显式移动动作、container query 和 contain 舞台 | 重新按 V2 token/层级写局部样式，不整份复制 1000 余行历史叠加 CSS |
| 不跨域复用 | [use-video-works.ts](../../../web/default/src/features/usage-logs/video-works/use-video-works.ts)、[use-video-works-identity.ts](../../../web/default/src/features/usage-logs/video-works/use-video-works-identity.ts) | 保留各自 queries、identity、路由和缓存 | 日志有 host/tenant/session/admin view、页码分页、统计、搜索；Studio 现为用户隔离、游标分页、有限 status，不互相冒充能力 |
| 不跨域复用 | `VideoWorkCost`、`VideoWorkBillingSheet`、`VideoWorkFailure`、`useVideoWorkDownload`、`prepareVideoPosters` | 不导入 Studio | 依赖日志账单/安全投影/产物集合或专属准备接口；不能把 `SUCCESS` 直接等同于 Studio `ready`，也不能用生成 failed 推导退款 |

共享优先级：已经公共的组件/工具 → Studio 现有能力 → 两个 Studio 调用方的小抽取 → 参考视频作品实现新需求。若未来确有第三个同契约消费者再考虑公共播放器；本期不顺手重构视频作品。

## 3. 文件级实施边界

下列“拟新增”是具体实施位置，当前文件尚不存在；可在实现时合并短小展示片段，不要求为了目录完整度逐个造空壳。

| 文件 | 动作 | 责任 |
| --- | --- | --- |
| `features/video-studio/video-create-page.tsx` | 改造 | 唯一页面编排：访问准备、当前目标、结果查询、未决提醒、媒体活动所有者；删除 Drawer/Sheet 双挂载分支 |
| `features/video-studio/components/video-composer.tsx` | 改造 | 唯一 RHF 实例；保留上传/参数校验；受理回调不导航；样片/模型/模式覆盖确认；费用拆出独立显示；必填高级字段展开 |
| `features/video-studio/components/video-parameter-fields.tsx` | 改造 | spec 驱动的常用/高级分组、视觉比例选项；原参数键与显式 false/0 不丢失；未知规格采用现有控件兜底 |
| `features/video-studio/components/video-generation-player.tsx` | 拟新增，来自既有卡内播放器 | 原生媒体生命周期、稳定媒体身份、加载/失败/恢复；两个调用方为主成果和详情；不直接请求/计费 |
| `features/video-studio/components/video-generation-result.tsx` | 拟新增 | 当前作品、定位中/超时、阶段/失败/空态的展示，接收已解析 view state 和动作 |
| `features/video-studio/components/video-generation-details.tsx` | 拟新增 | 基于公共 Dialog 的只读详情；复用播放器/状态；公开字段、任务号、下载、终态删除；无完整“再次生成” |
| `features/video-studio/components/video-generation-status.tsx` | 拟新增，小抽取 | 合并现有卡片/任务页重复状态映射；保留五态，文本和图标，进度未知时不画 0% |
| `features/video-studio/hooks/use-video-generation-target.ts` | 拟新增，小抽取 | create/library 两处目标解析和 30 秒发现窗口；已有 ID 走详情；只在明确证据后反馈不存在/不可用 |
| `features/video-studio/video-domain.ts` | 定向扩展 | 完整请求规范化、稳定指纹输入、真实进度识别；不把签名 hash 当指纹，不混入 UI 展示开关 |
| `features/video-studio/api.ts`、`schemas.ts` | 定向扩展 | 报价/回执最小运行时校验，统一 task ID 校验，prompt 按规范化内容的 8000 UTF-8 字节校验；错误证据以接口契约白名单处理 |
| `stores/video-studio-submission-store.ts` | 拟新增 | 用户隔离、带版本、可报告失败的未决记录；与草稿完全独立；写失败阻止 POST，不静默降级 |
| `features/video-studio/components/video-submission-notice.tsx` | 拟新增 | 展示该用户全部未决操作的紧凑提醒；查状态只读；不因选作品/编辑/无模型而隐藏 |
| `features/video-studio/queries.ts` | 定向扩展 | 复用列表、头部、详情观察机制；提供列表状态判定/稳定显示所需信息；mutation 捕获发起用户，迟到回执不更新另一账号页面 |
| `lib/studio-media.ts` | 小范围共享修正 | 查询键改用既有 `privateUserQueryKey`；signal 传到批次签名和逐条 fallback；保留 401/单路径 403/404 证据，客户端结果扩展 `deniedPaths`；后端协议不变，定向验收图片工作室消费者 |
| `features/video-studio/video-library-page.tsx` | 改造 | 全部/ready 真筛选、日期分组、稳定浏览顺序、新内容提示、当前详情和终态删除 |
| `features/video-studio/video-tasks-page.tsx` | 改造 | 单一任务列表；复制/查看任务/ready 定位，不重复播放器与画廊 |
| `features/video-studio/components/video-generation-card.tsx` | 改造 | 缩略封面、摘要、选择/详情、终态动作；移出播放器，不保留隐藏 video |
| `features/video-studio/components/video-sample-gallery.tsx`、`video-sample-card.tsx` | 定向扩展 | 局部嵌入布局、折叠/展开、与主媒体双向互斥、触屏显式预览；已有分页恢复保持 |
| `routes/_authenticated/video-studio/{create,library}.tsx` | 改造 | 保留旧参数，增加已知目标参数，类型化导航，见第 5 节 |
| `styles/video-studio.css` | 拟新增 | Studio 语义 token、容器响应式、动效与 reduced-motion；由 Studio 路由/入口加载 |
| `src/i18n/locales/{en,zh,zh-TW,fr,ja,ru,vi}.json` | 实施时定向修改 | 只新增/调整本需求文案，先读取 i18n skill，以当前实际支持的七种语言为准，使用英文源文键；不要因旧模块命名差异全量迁移 `videoStudio.*` |

不改 `video-upload-protocol.ts` 的上传协议、分片算法和服务端计费。只有页面生命周期改造暴露出真实回归时才修对应边界并补定向回归。

## 4. 状态所有权与提交数据流

| 状态 | 唯一所有者 | 不允许的副本/隐式副作用 |
| --- | --- | --- |
| 字段、dirty、校验错误 | `VideoComposer` 的 RHF | 选历史作品不 `form.reset`；CSS 断点不换 key；不要用全局 store 替代表单 |
| 恢复草稿、上传 resume | 既有用户隔离 draft store | 不放签名 URL/File/Blob；不承担提交前持久化可靠性 |
| 服务器 generation / profile / sample | React Query | 不复制整份响应到 Zustand；纯派生值在 render 中计算，不用 effect 同步副本 |
| 当前目标/主动选择 | 页面 + 经校验的 route search | 后台轮询不改选择；目标未定位时不借其他作品填充 |
| 尚未决议的提交 | 新 submission store | 不放 composer ref，不绑定当前草稿，不因 24h 到期自动删除 |
| 表单发送防重 | 提交事件中的同步门闩 + mutation 状态 | 不能只依赖下一次 render 才生效的 disabled；同次点击/键盘提交走同一入口 |
| 媒体活动所有权 | 当前页面一个 `activeMedia` | 样片/主作品不能各自宣布 active；详情打开先撤销其他所有者 |
| 签名 URL | `useStudioMediaUrls` 内存 query | 不进草稿、路由、未决记录；不使用上一个用户的缓存值 |
| 高级设置/灵感折叠、弹层 | 相关页面局部 UI state | 用户主动展开不被刷新复位；不改变业务请求指纹 |

提交顺序固定：字段/profile/ready 素材验证 → 获取当前有效报价并核对与当前请求相符 → 检查等价未决请求 → 分配操作身份 → **持久化提交中记录成功** → POST → 明确回执/明确拒绝/待核实三分支。请求指纹覆盖用户、原 token、有效 group、model、mode、prompt、parameters、素材 ID 及角色、sample；报价刷新不改变操作身份。完整序列与错误分类遵循 PRD 第 5 节及接口开发文档。

同账号不同输入可明确新建操作；多个未决项不能被一个单值 `submissionLock` 覆盖。记录只存最小恢复信息，不保存完整请求来提供“自动重放”。已有 task_id 只读定位；没有 task_id 不用 prompt/时间相似性猜对应记录。关闭提示、变更 token、变更模型、切作品均不解除等价未决保护。

提交快照同时捕获 `userId`、既有 auth `session.sid` 和本次页面/提交协调者的活动世代。Studio 私有内容以用户/会话身份设边界；身份切换或卸载即失效旧世代，异步完成处理先核对仍为同一活动世代再 toast、选目标、导航或 invalidate 当前缓存。仅比较 userId 不足以防止 A→B→A 的旧回执污染；sid 缺省也不能退化为只比较 userId，仍须检查边界世代。可对原 owner/operation 保存迟到证据，但不得应用到新页面。此逻辑限定在 Studio 协调层/mutation 上下文，不改全局认证协议；恢复本地记录的持久化分区仍按用户，不把会话退出误当作解除未知保护。

Web Crypto 摘要是异步步骤。在摘要/持久化等待结束后、POST 发出之前，必须再次核对 owner/会话世代仍有效、原 token/group 仍相符、冻结输入仍为本次确认输入、绑定报价仍有效。任一条件变化即不发送，并对尚未发送记录持久化为取消发送/安全移除；不能使用旧快照在新会话下付款，也不能默默换令牌或重新报价后继续 POST。

模型/模式/样片更改先计算会丢失的非兼容参数、素材及当前编辑内容；有损才确认。确认前不改变表单；取消保持原状；确认后一次应用。初始 sample 深链接只应用一次，query refetch 不能再次覆盖用户已编辑内容。切模型时可兼容提示词/参数继续保留；失效素材不发送，恢复能力仍可达。

## 5. 路由、目标、近期作品和列表稳定性

### 路由方案

兼容现有 [`create.tsx`](../../../web/default/src/routes/_authenticated/video-studio/create.tsx) 的 `sample` 和 [`library.tsx`](../../../web/default/src/routes/_authenticated/video-studio/library.tsx) 的 `task`。拟为 create 增加 `task`，为 create/library 增加 `generation`：前者复用当前 `task_` 格式与 128 字符校验，后者仅接受正安全整数。无效参数不发请求；保留旧 URL 可访问。

1. 回执只有 task_id：create 用 `replace` 更新 `task` 并移除旧 generation，保持表单与当前页面；沿用 30 秒有限列表发现。任务未出现只表示暂未定位。
2. 已知 generation ID：用 `useVideoGeneration(id)` 读取，不必扫描历史；若同时带 task，返回记录必须匹配。403/404 后清理其播放器/媒体并显示目标不可用，禁止回落到别的作品冒充此目标。
3. 用户主动选择近期作品/打开库详情时将已知 `generation/task` 写到 URL；后台完成、head 刷新不写 URL。刷新、返回时可读到同一个已知目标。由页面事件明确决定 replace，避免每次选择堆积历史；跨页导航仍保留正常浏览器返回。
4. 当前目标的暂时网络失败保留已授权可见快照并标陈旧；权限拒绝/删除证据优先于快照。账号边界重置整个私有页面选择和媒体状态。

`useVideoGenerationTarget` 统一处理上述两种身份、发现定时器清理和手动刷新；不接受把字符串 task_id 转成数值详情 ID。给目标 hook 传页面已有列表，不能在 hook 内再调用一遍 `useVideoGenerations`。

当前 `useVideoGeneration(id)` 本身没有轮询。已知目标第一次读到的记录必须交给 `useVideoGenerations` 扩展后的目标观察入口，并入既有 observations/detailTargets；目标即使不在已加载历史中也能继续追踪。头页已有目标时沿用头页更新，离开头页时由同一详情 query key 补查，使用现有 `getVideoTaskPollInterval`，终态/权限拒绝停止。不要只加一次 GET 就把“深链接活跃任务持续更新”标为完成，也不要另加页面 `setInterval`。

### 选择和顺序

成果初始选择：明确 URL/新回执目标 → 仍有效的主动选择 → 首次成功列表的最新条目。只有成功空列表显示首次引导；历史读取失败不阻止可用编辑器。近期组件仅接收页面 query 数据，避免一页为结果/近期/任务提醒建立三份观察器。

库内 `useVideoGenerations` 当前会合并新 head 条目，不能原样渲染后声称“浏览位置不变”。实施使用当前筛选/用户范围内的已接受显示 ID 顺序：首次成功建立顺序；已显示 ID 原位更新字段；分页成功在尾部追加；新 head ID 暂存为“有新作品”；用户点击刷新时接受并置顶；删除只移除对应项。不冻结任务状态、不复制服务器对象。筛选改变重建顺序；日期分组根据已接受条目计算，明确仅为已加载记录。

`全部/可播放` 只能映射 `status: ''/undefined` 与 `status: 'ready'`；请求发生在分页前。不做前端截取当全量过滤，不引入视频作品的四状态 tabs、全局统计或全文搜索。

## 6. 媒体、签名、删除与身份边界

`VideoGenerationPlayer` 接收稳定 generation/asset 身份、已授权 source/poster、是否允许播放、恢复回调与活动事件。数据获取在页面或 Studio 专用 hook 完成，player 不知道 TaskLog、token 或费用。主舞台/详情 `contain`，卡片 poster 可 `cover`；只对当前播放目标挂载 video。原生 `controls playsInline preload='metadata'`，不引入新播放器包；默认不有声自动播放，用户明确点击播放可以播放。

签名续期有两类动作：后台正常续签仅更新下次加载可用 URL，不以 URL 为 React key，也不更换健康播放中的 src；当前媒体发生真实加载错误时，按媒体身份合并一次自动恢复，重新取签名后重载，失败后给手动重试。401/403 禁止继续显示旧私有媒体。自动恢复不循环、不调用生成接口，不以所有播放错误推断“文件过期”。恢复必要重载时保留可恢复的 currentTime/暂停状态，账号切换/撤权/删除不恢复播放。

活动媒体可用页面级判别联合表示 `none | generation(id) | sample(id) | details(id)`；start 事件先撤销旧所有者。`VideoSampleGallery` 现有 preview registry 仍管理 warmed 缓存，外部活动所有权控制能否播放；收起、模态打开、离页和隐藏页面时 pause，关闭弹层不擅自重新播放之前的视频。不要全局扫描所有 DOM video，也不要把媒体 ref 持久化到 store。

复用 [private-query-cache.ts](../../../web/default/src/lib/private-query-cache.ts) 现有用户边界；当前 `studio-media-urls` 是单独的查询根，且签名请求未消费取消 signal，不能假定 `private-user` 清理已覆盖它。最小共享修正为使用 `privateUserQueryKey(userId, 'studio-media-urls', ...paths)`，由已有账号边界清理；queryFn 的 signal 传到批次签名及逐条 fallback，取消后不得继续展开 fallback 请求。不要添加第二个全局账号订阅或清理框架。删除成功时清理包含该结果路径的签名查询，清除局部实际 src，阻止迟到回调复活。此项属于共享模块变动，必须同时定向覆盖图片工作室媒体消费者，不能将对视频的修复带成图片回归。

当前签名 hook 的批次/逐条 `catch` 会吞掉权限错误，单改 query key 不足以支持撤权。客户端最小有类型扩展保留 `urls/expires_at` 并返回 `deniedPaths`：401 作为整体认证失败立即传播，撤销该用户全部已挂载媒体；批次 403/404 可沿用逐条 fallback，逐条 403/404 明确记入 deniedPaths；普通网络失败可保留已授权旧快照，缺少 URL 本身不等于撤权；abort 直接结束，绝不 fallback。`signedStudioMediaUrl` 和有媒体快照的消费者都必须拒绝 deniedPaths；player 当前原始路径命中拒绝证据时立即 pause、移除 src，不受“健康播放暂不换签”规则保护。以上是客户端适配，不要求后端增加响应字段。

| 共享签名的直接消费者 | 实施/回归约束 |
| --- | --- |
| [image-creation-results.tsx](../../../web/default/src/features/image-studio/components/image-creation-results.tsx) | 当前有 `presentedImage` 留帧；撤权须清其实际图片/旧帧及下载 URL，不能只清 query 后继续显示 |
| [image-library-page.tsx](../../../web/default/src/features/image-studio/image-library-page.tsx) | `wallSnapshot`、详情及已映射签名 URL 一并处理拒绝路径；普通网络失败仍保留合法旧内容 |
| [image-sample-gallery.tsx](../../../web/default/src/features/image-studio/components/image-sample-gallery.tsx)、[image-sample-admin.tsx](../../../web/default/src/features/image-studio/admin/image-sample-admin.tsx) | 缩略图与素材详情不得恢复拒绝路径；保持其他授权资源可用，不因单资源失败清空整页 |
| [video-asset-uploader.tsx](../../../web/default/src/features/video-studio/components/video-asset-uploader.tsx)、样片库、作品库和现任务页 | 素材/海报/视频/下载均消费同一拒绝证据，不能偷偷回落为未签名私有路径；任务页改版后可移除不需要的媒体签发 |

这些图片文件仅因共享媒体安全语义而做必要兼容，视觉/业务流程保持原范围；测试不可只 mock 掉签名 hook，还需真实 QueryClient/网络边界验证拒绝、取消与迟到结果。

删除使用既有 `useDeleteVideoGeneration` 和 `forgetObservedGeneration`/`pruneVideoGenerationQueryCaches`。仅 ready/failed 提供动作，确认期间禁止重复点击；成功才从列表/选择/目标/签名缓存移除，失败保留记录和明确恢复入口。删除不是取消生成或退款。关闭与成功删除后的焦点分别回触发项、相邻项/列表，不能留在已卸载按钮。

## 7. 暗色 V2、响应式、动效与素材

主题遵循 [ThemeProvider](../../../web/default/src/context/theme-provider.tsx) 的 light/dark/system；原型默认 dark 只表示本轮审查视图，不能在业务页强制写 `documentElement.classList` 或新增独立主题存储。新增 `.video-studio` 局部语义变量，对齐公共 token 能表达的部分；精确 V2 表面和控件变量放本页样式，弹层 Portal 内容也挂同一命名空间。不要修改全站主色来实现一个工作室。

视觉验收使用 ui-spec v1.2 的中性墨色页面、12px 卡片、400/380px 编辑栏、22px 栏距、桌面 28px 留白；不继续照抄 PRD 初稿的 16px 卡片尺寸。暗色主色实心按钮使用深色 `on-accent` 字，不用白字；必要控件轮廓区别于装饰分隔线。

布局在工作室内容根设置 container query；可用宽度不足 850px 自然单栏。DOM 顺序保持编辑→成果→近期→灵感，CSS 只变列，不将 composer 搬到另一个 portal 或设置断点 key。主操作在文档流；手机锚点按用户动作滚动，不抢输入焦点。保留输入/上传实例，320px、200% 文字、软键盘、短视口及侧栏伸缩均验收。

灵感沿用现有虚拟列表，不新增第二个全窗口滚动监听。展开区在桌面给明确可用高度、复用自身 scrollRef；窄屏也要给可见可滚动区域与可操作“加载更多/重试”，不能缩成零高。收起暂停媒体/观察，展开后保留筛选并重新测量；不要让整页双重纵向滚动妨碍编辑与成果定位。

以 CSS transition/现有 Base UI 开合状态实现 160–260ms 模式切换、按钮反馈、结果进入、局部展开。循环动效仅限真实 queued/processing/archiving，不制造百分比/预计完成时间。`prefers-reduced-motion: reduce` 下停 transform、循环与自动预览，立即显示最终状态；覆盖公共 Button/Dialog 自带动效时只影响 Studio 范围。真实服务端进度缺失/非法时展示阶段，不能把 `getVideoProgress` 当前兜底 0 当可靠的 0%。

位图用法：已有透明状态插画优先复用；本轮 [cinema-tools-still-life.webp](./assets/cinema-tools-still-life.webp) 为不透明工具静物，仅可按设计用于空态/灵感装饰且 contain；[city-ride-cinematic.webp](./assets/city-ride-cinematic.webp) 是示例封面，只留设计稿/固定视觉夹具。实施仅将实际使用的生产装饰资源导入构建，不打包 PNG 母版和演示封面，不在生产连接生图服务。

## 8. 实施审查门槛

本方案不把“更换 JSX 和 CSS”当完成。每个相关 PR 必须能回答：它复用了什么现有能力、保留了哪条付费/身份/媒体不变量、哪一个真实行为测试证明了新增风险被覆盖。用普通组件/少量领域函数，避免泛型表单引擎、统一任务状态机、第二套事件总线和新依赖。

定向行为重点：跨断点上传不中断；输入/报价/指纹一致；存储失败零 POST；立即刷新后仍有未决提醒；改稿再改回不能解除保护；晚到回执不写入新用户；新回执留页并定位；不同状态查询失败不冒充空；后台不抢选择/滚动；签名更新不中断健康播放；样片/主成果/详情互斥；终态删除清理所有可见路径。无障碍重点：必要控件 44px、焦点可见且回归正确、弹层安全区/滚动、错误关联、一次性状态播报、七语言长文。

本文件阶段只读代码并落文档，未运行业务测试、整仓类型检查或构建。后续各批次的精确验证命令由[质量验收文档](./quality-and-acceptance.md)定义；共享 `studio-media` 或路由契约变动按受影响模块和直接消费者扩大定向检查，不能用检查文档链接替代实施验收。

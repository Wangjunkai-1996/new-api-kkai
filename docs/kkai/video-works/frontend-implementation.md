# 视频作品：前端开发方案

状态：开发方案及本地实施记录，2026-10-27。下文第 1–8 节保留设计交接基准，第 9 节记录本轮落地。任务相关修改尚未提交或发布。

## 1. 实现基准与范围

- 以 [完整交互稿](design/prototype.fragment.html)、[桌面深色基准](design/desktop-dark.png)、[手机基准](design/mobile.png) 为视觉验收依据；插画与状态切图按 [资产规范](assets/README.md) 使用。
- 入口和页面标题使用“视频作品”。主层级为作品预览、模型、状态、时间、金额；插件、运行节点、长 ID 等进入详情。
- 改造现有 `/usage-logs/task`；保留链接与侧栏权限配置键。普通日志、绘图日志、Video Studio 的素材库/任务页不跟随重做，不把所有任务类型硬改成视频。
- 设计稿的模型、价格、封面、退款、倒计时、状态数量均为样例，不能成为业务默认值。正式数据来自任务接口及其公共投影。
- 高还原度包括桌面行布局、手机卡片、主题、抽屉、工具按钮、全部状态；不能以给旧表格换列名代替页面实现。

## 2. 源码核查与复用边界

以下路径相对仓库根目录，点击可读实际实现：

| 当前代码 | 已有能力与处理要求 |
| --- | --- |
| [usage-logs/index.tsx](../../../web/default/src/features/usage-logs/index.tsx) | 三类日志共用入口与作用域；增加 `task` 专属视图分支，其他分支继续使用原表格。草稿目前把 `drawing` 页也取成视频标题，落地时修正该耦合。 |
| [usage-logs-table.tsx](../../../web/default/src/features/usage-logs/components/usage-logs-table.tsx) | TanStack Query、服务端分页、URL 状态、权限占位数据守卫；保留普通/绘图日志逻辑，不加视频卡片样式穿透选择器。当前任务列表无自动轮询。 |
| [usage-logs-provider.tsx](../../../web/default/src/features/usage-logs/components/usage-logs-provider.tsx) | `useLogsViewScope()` 提供 `self/admin/root`；管理员选“仅我的”必须等同普通用户视角。 |
| [api.ts](../../../web/default/src/features/usage-logs/api.ts) / [types.ts](../../../web/default/src/features/usage-logs/types.ts) | 统一 `api` 实例、管理员/个人任务列表、按任务取制品；扩展已约定的参数及公共 DTO，不另造请求客户端。 |
| [task-artifacts.ts](../../../web/default/src/features/usage-logs/lib/task-artifacts.ts) | 已校验制品类型、通用 capability URL、重复 key、数量及旧视频/音频兼容；新增托管视频独立解析，不为对象存储 URL 放宽原有通用路径校验。 |
| [task-artifacts.tsx](../../../web/default/src/features/usage-logs/components/task-artifacts.tsx) | 草稿已有按打开动作请求制品、媒体失败重试和原生播放器；复用协议与媒体渲染能力，拆出可由页面控制的预览内容。 |
| [task-billing.tsx](../../../web/default/src/features/usage-logs/components/task-billing.tsx) | 未提交草稿已有金额、计费 epoch 守卫、历史规则、动态计费与计费状态展示。复用内容，抽屉替代当前 Dialog 外壳；修正草稿 `completed → Settled` 的过强推断。 |
| [task-details.ts](../../../web/default/src/features/usage-logs/lib/task-details.ts) | 已区分管理员插件信息与 root 运行信息；专属详情继续经过同一权限规则。 |
| [task-logs-columns.tsx](../../../web/default/src/features/usage-logs/components/columns/task-logs-columns.tsx) | 草稿已有模型优先、作品入口和扣费列；可提取业务展示逻辑，不能将旧多列表格当作最终设计完成。 |

基础组件使用现有 `Button`、`Tabs`、`Tooltip`、`Skeleton`、`Empty`、`Sheet`、封装 `Dialog`、`copy-button`、日期范围组件及分页组件。保持 `@base-ui/react` 的 `render` 组合方式；不得套用 Radix `asChild` 示例。

## 3. 文件级实施职责

优先在 `src/features/usage-logs/video-works/` 内收敛专属展示；以下为计划文件，不表示已创建。只在组件职责或共享行为确实需要时拆文件，不为每个图标/字段建组件。

| 计划文件或修改点 | 职责 |
| --- | --- |
| `usage-logs/index.tsx` | `task` 路由渲染 `VideoWorksPage`；复用现有认证、页面壳及作用域 Provider；绘图与普通日志维持各自标题/入口。 |
| `video-works/video-works-page.tsx` | 页头、筛选、状态标签、分页与空态编排；唯一选中任务及弹层状态，不为每行挂一份查询和弹层。 |
| `video-works/video-work-item.tsx` | 同一任务语义结构按容器宽度呈现桌面行/手机卡片；作品入口、模型、真实参数、状态、时间、金额、工具按钮。 |
| `video-works/use-video-works.ts` | 列表查询、查询键、权限作用域、分页、定向刷新及轮询策略；原始任务 DTO 保持可追溯。 |
| `video-works/video-work-preview.tsx` | 预览/错误/处理中/结果未保留；按需获取制品，使用原生 `<video controls>`，详情侧栏按权限展示。 |
| `video-works/video-work-billing-sheet.tsx` | 380px 扣费抽屉，复用 `TaskBillingDetails`；金额、状态、历史规则和缺失数据回退。 |
| `video-works/video-work-state.ts` | 仅保存需要桌面/手机/详情共用并可独立验证的状态与模型映射；不建通用渲染引擎。 |
| `api.ts`、`types.ts`、路由搜索 schema | 对齐后端契约的类型、查询参数和 AbortSignal；任务分支使用 `q/status_group/media_type=video`，不改变其他日志语义。 |
| `src/styles/video-works.css`（确有需要时） | 作品容器查询与少量专属变量；类名前缀隔离，禁止覆盖全站 `table`、`button`、`h1`。 |

搜索文字草稿与当前弹层为局部状态，服务端列表归 Query，已应用筛选归 URL。可直接计算的状态文案、统计或是否可播放不重复存为 React state。无需新增 Zustand store、播放器库或动画依赖。

## 4. 视觉尺寸、主题与素材

原型的全站顶栏/侧栏是展示背景；正式实现复用应用壳，**不复制一套导航**。下面尺寸按作品内容区测量；在实际壳中以相同内容宽度对照截图。

| 区域 | 桌面目标 | 手机目标 |
| --- | --- | --- |
| 主区 | 页边距约 24px；标题卡与列表间距 22px | 375px 下两侧 15px；320px 下 12px |
| 标题卡 | 最小高 104px、圆角 12px、左右 20px；插画 116×88px；标题/插画/作用域三列 | 最小高 122px、内边距 16px；插画 88×82px；作用域在标题下方；极窄屏插画 74px 宽 |
| 列表 | `作品 / 状态 / 生成时间 / 扣费 / 操作`；参考列宽 `minmax(300px,1fr) 72px 75px 87px 78px`，间距 12px | 一列作品卡，卡间距 16px；隐藏桌面表头 |
| 作品行 | 最小高 125px；内边距 20px 15px；列表整体圆角 12px；细分隔线 | 卡圆角 12px；封面铺满顶部；信息 16px 内边距，底部独立操作栏 |
| 媒体入口 | 144×81px、16:9、圆角 8px；中间播放圆钮 31px | 成功作品 16:9；状态图区域约 2.3:1；播放圆钮 43px |
| 字体层级 | 模型 16px/600；金额 18px/600；辅助信息 11–12px | 模型 17px；金额 21px；辅助信息 11–12px |
| 预览 | 最大宽 900px；内容 `minmax(0,1fr) 245px`，播放器 16:9 | 单列，详情在播放器下；弹层自身滚动 |
| 扣费 | 右侧 Sheet 380px；标题及金额固定在正常文档流中，内容区可滚动 | 最大视口宽度；可到达关闭和全部扣费字段，不被软键盘/安全区遮挡 |

原型在 740px 视口切卡片，因为 900px 以下隐藏了模拟侧栏。正式应用使用**内容容器约 704px**作为行/卡片切换参考，避免真实侧栏展开时在 900px 屏幕塞入过窄表格；内容 704–760px 可用 132×75px 封面和更紧列间距。不要直接沿用旧日志 640px 移动断点。验收同时覆盖侧栏展开/折叠。

长模型与法语/俄语文案允许换行、扩高；金额保持完整，任务 ID 可省略并复制完整值。不能通过横向滚动、缩小全页或把文本裁没来“对齐”。交互触达在粗指针设备至少 44×44px，可扩大透明点击区而不放大视觉图标。

- 使用项目主题 CSS 变量及 `.dark`，局部映射原型背景/面板/边线/主蓝/成功/失败色；不把 `light-dark()` 和固定灰色直接覆盖全局主题。金钱用 tabular-nums，正文沿用产品字体。
- 图标统一 `@hugeicons/core-free-icons` + `HugeiconsIcon`，对应播放、下载、复制、详情、状态、关闭、重试；原型 Lucide 仅为设计示意。按钮内使用 `data-icon`，不混入 PNG 小工具图标或额外图标库。
- 页头及空列表/生成中/失败/链接失效使用已审资产，按状态选择，保持透明背景和比例。装饰图 `alt=''`；状态文本必须独立可读。素材本地构建打包，禁止生产运行时调用生图。
- 两张演示封面只用于文档/测试，**不得挂在真实用户任务上**。正常托管成功视频必须显示获授权的真实封面；真实缺失、生成失败或历史记录无封面才使用同尺寸中性占位。不能全页占位宣称高还原完成，也不把视频 URL 填进 `<img>`。
- 低动效偏好关闭缩放/转场；无自动播放、无限呼吸光、无意义闪动。悬停/焦点/按下样式都需落地。

## 5. 查询、筛选与权限

### 查询行为

当前后端接口已有任务 ID、渠道、时间、分页及原始 `status/action/platform` 过滤；前端任务参数类型尚未完整承接。新增能力是 `q` 模型/任务 ID 搜索、`status_group` 状态分组、`media_type=video` 媒体约束和聚合计数，按 [后端契约](backend-contract.md) 实施。联调前不可把本页过滤冒充全量查询，也不可用本页条数充当总数。

1. 延续 `/usage-logs/task` 与现有 `page/pageSize/filter/startTime/endTime/channel` 链接；新增参数由 Zod 白名单校验，输入长度、日期顺序、页码范围与后端一致。`filter` 老参数继续代表任务 ID，不能悄悄改为混合搜索。
2. 新页面始终发送 `media_type=video`；混合搜索使用 `q`，trim 后最多 191 字符，匹配公开任务 ID 或展示模型的字面子串；状态标签使用 `status_group`，不同时发送非空原始 `status`。搜索输入可 300ms 防抖，Enter 立即应用。日期/渠道/状态/作用域变更回到第 1 页；浏览器前进后退恢复筛选。
3. 每次请求只取当前页。建议默认 20 条，不继承旧桌面 100 条导致大批媒体入口；保留合理分页选项及服务端总数。展示结果数与当前筛选一致。
4. 状态标签对应 `all/success/processing/failed`；`processing` 包含 `NOT_START/SUBMITTED/QUEUED/IN_PROGRESS`，`UNKNOWN` 只在 all 中显示并保留“状态未知”文本。读取 `status_counts` 作为相同权限、媒体类型、搜索、日期、渠道条件下的聚合；当前 `status_group` 不改变其余标签计数，分页 `total` 则服从当前状态过滤。
5. 使用列表查询结果中的公开金额/规则快照；不为每行分别请求价格、用户、制品或扣费。初次加载用同布局骨架；刷新保留内容并提示更新，接口失败不得显示“还没有作品”。

### 缓存与作用域

建议查询键：`['video-works', tenantId, userId, viewAccess, normalizedFilters, page, pageSize]`；制品键另含该身份三元组、numeric `TaskLog.id` 与公开 `task_id`。`normalizedFilters` 只含服务端语义的稳定值，不放 `t`、函数、Date 实例或完整路由对象。

- 复用 `useLogsViewScope()`；`self` 只请求 `/api/task/self`，禁止仅在管理员返回结果中筛自己的数据。
- 普通用户、管理员“仅我的”隐藏用户/渠道/管理员扩展信息；root 运行信息继续走现有保护。当前未提交制品 query key 仅带 task ID，落地时需补身份隔离。
- 只有 tenant/user/access 相同的分页请求可保留旧页占位。切身份、切作用域时清选中任务、关闭弹层，不把原作用域数据闪给下一作用域；失败不恢复旧权限数据。
- 统一认证登出缓存清理继续生效；敏感制品签名 URL 不写 localStorage、URL 查询参数、日志或埋点。列表和制品请求传 `AbortSignal`，防止快速切页后的旧响应覆盖当前界面。
- 刷新/重试只失效相应作品查询，不继续使用广泛的 `invalidateQueries(['logs'])` 刷新无关日志。

### 生成中与自动更新

当前 `progress` 字符串可能是真实上游值，也可能是后端阶段占位；目前没有可验证的来源字段。第一版统一呈现“排队中/生成中”等真实阶段，**不显示百分比、不画伪进度、不估算剩余时间**。只有新增并验收可信来源契约后才开放数字进度。

计划通过 Query `refetchInterval` 每 10 秒更新含非终态生成记录或 `media.state=processing` 的可见列表；后端调度周期约 15 秒，页面应称“自动更新”，不承诺实时进度。抽屉打开且任务/托管媒体未终结时，即使当前筛选不再含该任务，也保持对该任务的获授权状态查询。优先复用服务端任务 ID 过滤能力，最多一个进行中请求，避免自己 setInterval 多路并发。

浏览器后台或离线暂停；回到前台可重新请求。全部任务与计费处理均终态后停止；仍待结算的已完成/失败记录每 15 秒更新，避免界面长期停留“预扣”。`completed` 停止计费轮询不等于证明已结算。连续查询失败按 20/40/60 秒退避并提供手动刷新；401/403 立即停止且清除当前敏感展示。刷新只读本地记录，不触发额外上游轮询，不以轮询次数递增进度或推断成功。

## 6. 媒体、详情与扣费

预览弹层由页面唯一状态管理：`{ kind: 'preview' | 'billing', taskRecordId } | null`，其中 `taskRecordId` 是 numeric `TaskLog.id`；公开 `task_id` 从选中记录取出，仅用于既有制品 endpoint。任务对象从当前缓存派生，避免轮询后继续显示旧金额。预览转扣费复用同一选中 ID，不能出现两个抢焦点的遮罩。

- 点击封面/“查看作品”后才请求制品；打开失败/生成中详情无需提前请求视频流。多制品提供可切换列表，不能只静默取第一项并丢其余结果。
- 原生播放器只在可预览状态挂载，`playsInline`、`preload='metadata'`，不自动播放；关闭、切任务或切制品时暂停并卸载媒体。列表只懒加载可见真实封面图片，不预加载所有视频抓首帧。
- 生成状态、结算状态、媒体可用状态分别维护。视频成功但链接失效仍是成功任务；媒体 `onError` 只能说明“暂时无法加载”，不能凭此认定链接过期。Studio 的 `poster_expires_at/expires_at` 只是签名 URL 有效期，可以重新签发，**不是资产删除/永久失效时间**；确认资产不可取回后才显示失效状态。
- 重试重新请求制品投影、重新校验获授权 URL 后再重建播放器；通用合法 capability URL 可以与上一次相同，不要求签名字符串变化。403 不无限重试。托管视频下载直接使用独立 `download_url` 的 attachment 签名；通用制品才按契约加 `download=1`。跨域 `<a download>` 不作为完成证据，不为大视频默认 fetch Blob。
- 首次空列表引导视频工作室需检查当前用户是否有入口权限；无权限则显示中性说明。搜索无结果保留筛选并提供重置；失败、结果未保留、媒体为空分别给出准确说明。
- 计费优先复用 `formatLogQuota`、`LogCostDisplay`、`TaskBillingDetails`，但必须先检查新增的 `billing.amount_available === true`；缺失或 false 显示 `—`，不能把错误转换产生的 0 当成免费。示例中的 `$` 不得覆盖用户币种/积分显示设置；金额不得用 `toFixed(2)` 重新定义后端记账。
- 抽屉显示任务冻结规则与后端公开计费语义；`completed` 仅显示“计费已处理 / 当前扣费”，单独出现不能证明“已结算 / 实际扣费”。缺字段显示“此记录暂无扣费明细”，不拿当前定价补历史。预扣/退回/最终金额三行只有相应会计字段可核实时才显示；不能从 `status === FAILURE` 或 `quota === 0` 推导“已退款”。
- `billing.duration/size/resolution` 是倍率时按倍率展示，不能拿它们冒充视频秒数、画幅或 720p。任务时长/分辨率/封面缺数据时省略该项。

托管媒体采用 [后端契约 §7.1](backend-contract.md#71-托管视频正常路径复用-studio不新增签名-endpoint) 的明确路线：复用已有 Studio 海报流水线与本地对象存储短签名。任务列表按 numeric `Task.ID → generation → output(position=0) → asset` 批量校验所有者和作用域，最多追加三次 DB 查询；不新增签名接口、不下载媒体、不在 GET 中归档/截图。前端不得每行另查 Studio，也不直接调用其所有者签名接口绕过任务权限。

| 计划响应 | 前端处理 |
| --- | --- |
| `TaskLog.media` | 对象可选；存在时 `source:'studio'` 与 `state:'ready'|'processing'|'unavailable'|'restricted'` 必传。`poster_url?/poster_expires_at?` 成对，`width?/height?/duration_seconds?` 仅实际 inspect 正值。ready 且存在合法海报时显示真实图；processing 独立显示“视频处理中”；unavailable/restricted 显示准确中性状态。 |
| 分页 `data.media_warning` | 可选 `media_temporarily_unavailable`；显示媒体暂不可用的非阻塞提示并保留任务及扣费记录，不能当成空作品或未授权后兜底请求。 |
| 既有制品 endpoint 的 `data.hosted_video` | 对象可选；存在时 numeric `task_record_id` 与媒体 state 必传，ready 才包含且必传 `content_url/download_url/expires_at`；其他 state 禁止 URL。核对 `task_record_id === selectedRow.id`，尺寸时长可选。只在打开预览/主动下载后请求并签视频地址；公开 task_id 歧义的 404 显示不可用，不在前端任选记录。 |
| 原 `artifacts/legacy_content_url` | 保留通用 capability 的路径、token、类型、数量校验；不因为新增 Studio 短签名而接受任意上游 URL。已识别为托管的任务不回退到通用上游结果。 |

托管制品缓存只保留内存，默认 staleTime 30 秒且不得跨签名有效期复用；主动重试绕过旧缓存。`poster_expires_at/expires_at` 为 Unix 秒，比较时统一转换，不能当毫秒。封面短签名到期后的首次展示/重试刷新当前列表投影，视频短签名到期后的播放/下载动作重新取制品；同页请求去重，不为每张图建立刷新定时器。重新投影后合法签名文本可能相同，不以字符串是否变化判断重试成功。通用长期 capability 没有 expires_at，不套用 Studio 自动续签假设。

**正常托管 ready 成功视频的真实封面是本期高还原验收要求**；该投影与真实媒体链路未交付即属于阻断项。中性占位只覆盖真实缺失/损坏/处理中/权限受限/历史记录，不能替代主路径交付；目标环境缺少 ready 资产或可用签名配置时记录联调阻断，不自动启用生产开关补齐。

## 7. 代码质量、国际化与可访问性

- TypeScript 严格类型；接口不可信内容按现有解析器验证。避免 `any`、双重断言绕过权限/状态类型、嵌套三元、巨大渲染函数及每行 useEffect 请求。
- 不把 HTML 原型的 `innerHTML`、模板复制、DOM 查询、全局点击事件、手写焦点陷阱、Tweak、`window.openai` 搬进 React。视觉还原用 JSX/CSS，交互用局部状态和 Base UI。
- Query 管服务端数据，现有路由管筛选；少量派生计算无需全量 `memo/useMemo/useCallback`。仅重型详情确有首屏负担时动态加载，不新增预加载框架或虚拟列表系统。
- 完整支持实际七语言 `en/zh/zh-TW/fr/ja/ru/vi`。英文源字符串为 key，组件内 `useTranslation()`，动态状态 key 登记 `src/i18n/static-keys.ts`；按 i18n 技能的 `add-missing-keys.mjs` 写入七语言后 `bun run i18n:sync`，不可手改 locale JSON。
- 模型名、任务 ID 不翻译；时间/时长/货币本地化。检查法语、俄语长文案、错误原因和超长 ID 的换行；不用缩字体掩盖溢出。
- 使用语义化列表/行与按钮；每个图标操作有可访问名称与 Tooltip。封面播放按钮、详情、复制不能形成嵌套按钮。图标与颜色不能作为唯一状态信息。
- Dialog/Sheet 使用标题、描述、焦点锁定、Escape、关闭后回焦；遮罩不吞底层页面滚动。复制操作确认成功再 toast，失败时显示可选中文本。
- 页面状态更新使用温和 `aria-live`；不每次轮询都朗读整个列表。正文对比度至少 4.5:1；控件焦点清晰，触达面积不足原型处以可访问性优先。

## 8. 实施顺序与验证边界

1. 对齐后端列表筛选、状态计数、媒体摘要、计费公开投影；确认缺失字段回退。把当前未提交草稿逐项纳入评审，不默认其已通过最终设计验收。
2. 仅替换 `task` 展示分支，先以明确标识的开发 fixture 做桌面/手机/深浅色视觉还原；fixture 不能随生产列表导入。
3. 接入真实查询、权限、轮询和预览，再接冻结计费；逐项删掉原型演示行为。完成字段缺失、链接失败和权限切换回归。
4. 最后做视觉对照、目标测试、变更文件 lint/类型检查、七语言核对；无证据不声称“完全还原”。

本节保留开发前的验证计划；文档阶段未运行应用测试。后续实际执行命令见第 9 节及 [实现与验证记录](implementation-review/README.md)，候选验证范围仍以 [验收方案](acceptance.md) 为准。

新增行为测试覆盖：权限/身份缓存隔离、服务端筛选分页与 URL 恢复、可见时轮询/终态停轮询、真实封面获授权加载、媒体按需挂载及重试重新请求合法投影、历史扣费缺失回退。调整共享组件时追加其直接依赖测试；不写只断言 CSS 类名、截图文件存在或纯静态行数的测试。重试测试允许重新获取后的合法 URL 保持相同。

视觉验收覆盖 320、375、768、1024、1440px，深浅色、侧栏展开/折叠、成功/生成中/失败/空态/筛选空态/媒体失败/扣费；检查无水平溢出、图片比例、焦点与真实请求数。`bun run i18n:sync` 是修改翻译后的必需整理步骤，另检查本功能新增 key 在七语言均存在。

常规 L2 开发不自动运行整个仓库测试、全站 lint/typecheck/build。实际发布时再按 NewAPI 发布范围与 runbook 执行强制门禁；本设计交接不授权发布。

## 9. 本轮本地落地记录

- `video-works/` 已增加专属页面、行/卡片、筛选栏、页头与分页、单一预览、任务详情及扣费 Sheet；预览详情和下载流程按职责拆分。局部 `video-works.css` 同时给页面和 portal 配置主题，未覆盖全站组件。
- `/usage-logs/task` 使用视频专属页面；普通与绘图分支继续原表格。真实数据来自授权列表/制品投影；正式构建仅打包五张状态插画，没有演示封面。
- 渠道筛选复用管理员 `/api/channel/search`，仅打开下拉后查询，支持名称/ID、每页 30 项及继续加载；响应只留 ID/名称到控件缓存。筛选栏在有结果时也提供重置。
- 托管下载要求匹配的 `hosted_video` 投影，不回退到通用/原始上游 URL。临时 `unavailable` 允许重取授权投影；`restricted` 与结果未保留禁用媒体请求。播放器续签支持返回同一 URL 后重新挂载。
- 视频时长、实际尺寸及生成耗时分别展示；生成耗时只有有效开始/结束时间时出现。列表不显示不可信百分比。扣费复用 `TaskBillingDetails` 的视频 presentation，突出当前金额并保留币种/积分/会员语义。
- 组件目标验证：`bun run test src/features/usage-logs/video-works/video-work-ui.test.tsx`，6 条通过；自有 TS/TSX 使用显式文件列表运行 `bunx --no-install oxlint -c .oxlintrc.json`，通过；组件入口使用临时 `.video-works-ui-tsconfig.json` 运行 `bunx --no-install tsgo --noEmit -p .video-works-ui-tsconfig.json`，通过后删除临时配置。路由级局部类型检查缺少应用 TanStack 注册，未用全站类型检查替代。
- 实际页面由父任务进行浏览器截图与交互验收。没有运行完整测试、全站 lint/typecheck/build，没有构建或发布生产镜像。目标环境真实媒体签名与下载验收仍按总验收方案记录。

组件验证复现（在 `web/default/`，临时配置检查后删除）：

```sh
bun run test src/features/usage-logs/video-works/video-work-ui.test.tsx
bunx --no-install oxlint -c .oxlintrc.json \
  src/features/usage-logs/video-works/video-work-state.ts \
  src/features/usage-logs/video-works/video-work-summary.tsx \
  src/features/usage-logs/video-works/video-work-item.tsx \
  src/features/usage-logs/video-works/video-work-preview.tsx \
  src/features/usage-logs/video-works/video-work-details.tsx \
  src/features/usage-logs/video-works/video-work-billing-sheet.tsx \
  src/features/usage-logs/video-works/video-works-filter-bar.tsx \
  src/features/usage-logs/video-works/video-works-channel-picker.tsx \
  src/features/usage-logs/video-works/video-works-controls.tsx \
  src/features/usage-logs/video-works/video-works-page.tsx \
  src/features/usage-logs/video-works/use-video-work-download.ts \
  src/features/usage-logs/video-works/video-work-ui.test.tsx \
  src/features/usage-logs/index.tsx \
  'src/routes/_authenticated/usage-logs/$section.tsx'
bunx --no-install tsgo --noEmit -p .video-works-ui-tsconfig.json
```

`.video-works-ui-tsconfig.json` 精确内容：

```json
{
  "extends": "./tsconfig.app.json",
  "include": [],
  "files": [
    "src/env.d.ts",
    "src/tanstack-table.d.ts",
    "src/test-setup.ts",
    "src/features/usage-logs/video-works/video-works-channel-picker.tsx",
    "src/features/usage-logs/video-works/video-work-preview.tsx",
    "src/features/usage-logs/video-works/video-work-item.tsx",
    "src/features/usage-logs/video-works/video-work-billing-sheet.tsx",
    "src/features/usage-logs/video-works/use-video-work-download.ts",
    "src/features/usage-logs/video-works/video-work-ui.test.tsx"
  ],
  "compilerOptions": { "incremental": false }
}
```

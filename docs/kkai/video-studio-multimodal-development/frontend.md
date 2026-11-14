# 视频工作室多模态参考：前端开发文档

日期：2026-11-14。状态：本地实现已落地，工作树未提交、未部署。负责人角色：前端；契约负责人：后端/架构；交互与视觉验收：设计、产品、QA。实际定向验证与未验证边界见 [实现与验收记录](implementation-qa.md)。

本文规定新视频创作流程的实现与验收，不表示能力已上线、费用已验证或真实媒体已通过测试。以 [PRD](../video-studio-multimodal-design-20261114.md)、[完整 Web 设计](../video-studio-multimodal-web/index.html)、[后端契约](backend.md) 为依据；API 字段和错误语义以后端文档为唯一源。保留的设计目标与本地实现状态分别说明，真实供应商能力仍需独立验收。设计稿 `design.js` 仅用于交互演示，不得作为上线代码。

## 1. 范围与实施前提

- 交付一个统一创作表单：精确型号 → 参考素材 → 画面描述 → 输出设置 → 费用/生成；无“文生/图生”顶层切换，不把音频设计成可单独提交的模式。
- 首次完整交付包含图片、视频、音频组合、真实上传/播放、报价/提交、恢复、错误处理和视觉还原；不可用“音频按钮已画好”代替音频链路。
- 新页面只保存和恢复新格式草稿、新格式样片引用。旧页面、旧请求、旧草稿自动转换与重放均不做；已有作品和任务仍可查看，已受理任务继续处理、归档与结算。切换不批量清空旧草稿或业务数据。
- 沿用 `/video-studio/create`、`/video-studio/library`、`/video-studio/tasks` 的分工。只重排创作页及其必要的共享依赖，不借此重做全部平台壳、作品库或任务页。
- 前置输入必须完整：精确模型与渠道的已验收能力、各类数量上限、音频与输出声音关系、可用模型/分组、适配器版本及新契约。不设置三类素材合计数量上限；`max_total_count` 仅为已有配置兼容字段，null 或历史数值均不阻止启用、上传、报价或提交。其他未知能力或未验收组合按不可提交处理，不能用名称后缀、静态设计数据或历史值补齐。
- 代码遵循项目 `AGENTS.md`、`web/default/AGENTS.md`、`ponytail`、`shadcn-ui`、`vercel-react-best-practices` 和 `i18n-translate`。复用已有库，不增加上传/状态机/表单/波形依赖；保留安全、可访问性和扣费保护。

## 2. 源码改造地图

以下路径均相对 `web/default/`。本表保留实施前的约束和改造目标，不能当作当前未实现清单；实际组件和完成范围见 [实现与验收记录](implementation-qa.md)。

| 文件/符号 | 实施前约束 | 改造目标 |
| --- | --- | --- |
| `src/features/video-studio/types.ts`：`VideoComposerValues`、`VideoReferenceInput`、`VideoReferenceAssetInput`、`VideoModelSpec`、`VideoSample`、`VideoUploadPurpose` | 草稿只有 `reference_asset_ids`；角色无音频；样片为平行 ID/URL 数组 | 消费新能力和有序绑定；明确本地条目与 wire DTO 的边界；保留真实已有字段名 |
| `schemas.ts`：`getVideoModelPreset`、`videoComposerSchema`、`videoModelSpecSchema`、`validateComposerForProfile` | `_with_video_ref` 被放入 `image_to_video`；最多 2 个参考；只需一个图片或视频 | 新生成不使用旧模式/单参考分支；数量、时长、方式与组合来自能力；`8000` 字节提示词约束继续复用 |
| `schemas.ts`：`createVideoModelProfileFormValues`、`parseVideoModelProfileForm`、样片表单函数 | 管理端重建前端硬编码预设；样片按当前 profile 推断输入 | 管理员消费后端已验证预设，只允许收紧；新样片显式保存角色/顺序/版本，无旧数组转换 |
| `video-domain.ts`：`getVideoReferenceRoles`、`buildVideoQuoteRequest`、`getVideoSubmissionRequestKey`、`restoreVideoComposerDraft`、`getSampleReferenceAssets` | 仅取必填角色；按 ID 排序生成请求键；恢复时 `slice` 截断；猜素材 MIME | 改为新绑定集合规范化；按角色次序与 `position`；不截断、不猜 MIME；不自动排除不兼容条目 |
| `components/video-composer.tsx`：`VideoComposer` | 模型/模式变化会截断引用；图片/视频二选一上传器 | 统一表单，保留全部条目，删除顶层模式 UI；参与/排除状态、模型兼容提示、报价失效和提交门禁 |
| `components/video-asset-uploader.tsx`：`VideoAssetUploader`、`VideoAssetUploadSession` | 一个 `purpose` 和一套 accept；卡片只渲染图片/视频；移除可能直接删资产 | 复用 Uppy 生命周期及协议；逐文件 purpose；混合入口、音频预览、替换事务；草稿移除仅解绑 |
| `video-upload-protocol.ts`：`createVideoUploadProtocol`；`video-upload-selection.ts`：`prepareVideoUploadSelection` | 已有 single/multipart、预留/完成、恢复和重复回调防护 | 增加音频用途与新恢复记录；保留协议校验和中断清理；不另写 PUT/分片协议 |
| `video-upload-resume.ts`：`getVideoUploadMaxBytes`、`sanitizeVideoUploadResumeRecords`、`shouldDeleteVideoAssetOnRemove`；`video-upload-lifecycle.ts` | 视频 fallback 允许 1 GiB；用户参考移除可触发删除 | 新入口按能力与站点限制取最严值；不能将 fallback 当模型接受上限；解绑不走 ready 资产 DELETE |
| `video-reference-hydration.ts`；`queries.ts`：`useVideoReferenceAssetHydration` | 以裸 ID 列表恢复 | 每个绑定恢复真实资产及校验证据，缺失保留占位；不提交剩余子集 |
| `src/stores/video-studio-draft-store.ts`：`sanitizeVideoStudioDraft`、`useVideoStudioDraftStore` | 格式版本 1，旧模式与 ID 数组；读写时主动删除 legacy key | 新独立版本命名空间、单草稿；不读取/转换/批量删除旧 key；继续用户隔离及 storage 异常处理 |
| `queries.ts`：`useVideoModels`、`useVideoQuote`、`useCreateVideoGeneration`、`videoStudioQueryKeys`；`api.ts` | 已有 token-scoped models、报价/生成入口和取消重试策略 | query key 包含新规范化语义/能力版本；客户端只用服务端报价；新增受理查询消费见第 4 节 |
| `hooks/use-video-submission.ts`：`useVideoSubmission`；`video-submission-protocol.ts` | 已有发送锁、身份检查、落盘回执、unknown 保护；禁止同角色多项 | 保留保护；替换重复角色判断与指纹，加入顺序/版本；unknown 只查受理，不自动新建 |
| `src/stores/video-studio-submission-store.ts`；`components/video-submission-notice.tsx` | 持久化 sending/unknown/accepted 操作记录，当前不保存完整请求 | 与创作草稿独立；新流程拟增加本次规范化请求快照与原键的恢复数据，不含签名 URL/Token 密钥；新建草稿/切模型不抹除待查记录；历史受理任务不重放旧输入 |
| `video-create-page.tsx`：`VideoCreateWorkspace` | 现成结果选择、近期 3 项、样片、下载、activeMedia | 保留真实数据流，接入新 UI/样片快照；结果摘要读任务快照，不读正在编辑的草稿 |
| `components/video-generation-player.tsx`、`video-generation-details.tsx`、`video-generation-status.tsx`；`src/lib/studio-media.ts` | 已有签名访问、播放/失败/状态能力 | 复用，扩展媒体互斥到参考音视频；下载真实结果，不下载设计静帧 |
| `admin/video-model-spec-editor.tsx`、`admin/video-model-admin.tsx`、`admin/video-sample-admin.tsx` | 只显示单一图片/视频参考类型 | 展示完整已验收能力；新样片组合校验、引用快照与失效修复；不提供任意字段映射编辑器 |
| `components/video-studio-nav.tsx`、`src/styles/video-studio.css`、现有 layout 组件 | 原创作样式和新设计尺寸存在差异 | 第 7–9 节限定在新创作页的 scoped 样式和壳适配，避免改坏任务/作品库 |

本地实现从 composer 分出 `components/video-reference-collection.tsx::VideoReferenceCollection`，集中处理分类、额度、卡片、排除区与替换，上传协议继续复用原模块。没有另建前序建议的 `VideoReferenceSection` / `VideoReferenceCard` 或通用状态机。计数、兼容项、是否可报价从当前草稿和服务端数据推导，不为每个按钮单建组件或维护第三份同步数组。

现有 UI 组件优先：`Button`、`Form/FormField/FormItem`、`NativeSelect`、`Textarea`、`Switch`、`Dialog`、`AlertDialog`、`DropdownMenu`、`Progress`、`Badge`、`Empty`、`Skeleton`、`Tooltip`。本项目为 Base UI，不套用 Radix 的 `asChild` 假设；先读组件 props，沿用 `render` 组合。现有视频模块使用 `lucide-react`，首版延续该图标体系，不为了全项目 `components.json` 的 hugeicons 配置批量换图标。弹层必须有 Title/Description；禁止用 `dangerouslySetInnerHTML` 移植原型字符串模板。

## 3. 单一草稿及操作规则

### 3.1 本地状态

RHF 中保存一份正在编辑的业务表单，Zustand 只承担该表单的新格式持久化/恢复及上传恢复记录；不能以 RHF `reference_asset_ids`、React `referenceAssets`、store `references` 三份独立可变列表互相补丁。远端资产状态由 React Query 统一持有；表单只存绑定身份和用户意图。新草稿载入一次、用户操作后保存，不用每次查询刷新覆盖正在编辑的字段。

已实现的本地草稿（**不是 API**）保存 `model_profile_id`、`mode`、`prompt`、`parameters` 和 `references`。已绑定条目包含稳定 `client_id`、正整数 `asset_id`、目标角色、组内顺序、`included`（用户明确排除才为 false）；上传/替换过程由上传器和引用区管理，未绑定的候选不写为有效草稿引用。File、Uppy 对象、播放元素、对象 URL 和签名 URL 不写 localStorage。草稿不持久化能力版本，恢复后重新获取当前能力，报价请求使用当前目录的版本。

`mode` 不提供顶层手动切换：无参与素材推导 `text_to_video`，有参与素材推导 `reference_to_video`；仅已验收的高级画面引导显式选择帧控制。上传期间通过 busy 状态阻断报价，不能暂当“无参考”报价。被标记不兼容的参与条目仍保留并阻断，只有用户明确排除才退出请求。

新 key/结构版本与旧存储隔离；不沿用当前 `removeLegacyVideoStudioDraft` 的删除行为处理旧草稿。用户切换/登出必须隔离内存、查询、恢复记录与异步回调，保持现有 `userId/sessionId`/identity epoch 保护。持久化失败不能显示“草稿已保存”；普通草稿可继续编辑并提示本机未保存，提交恢复记录落盘失败则沿用现有停止发送保护。

### 3.2 操作事务

| 操作 | 草稿处理 | 报价与焦点 |
| --- | --- | --- |
| 添加 | 文件逐个预检；合法项保留，非法项明确说明；同 asset ID 或同一批中可确认的相同 File 提示已添加 | 有活动上传即暂停；焦点留在添加入口/新条目。name/size/lastModified 指纹只是恢复线索，不作为内容相同的安全证明 |
| 替换 | 旧绑定保留有效；候选新资产独立上传/校验；新资产 ready 后原位置一次切换；失败保留旧项 | 替换处理中停止报价/提交；成功失效报价；失败恢复旧请求语义后重新确认可报价 |
| 移除 | 仅解绑草稿；已上传 ready 资产不调用 `deleteVideoAsset`；未完成上传取消走现有 abort 协议 | 原报价立即失效；焦点到相邻卡片或添加按钮；可撤销时恢复原顺序/参与标记，并重新校验 |
| 排除/恢复 | 只有点击“本次不使用”才 `included=false`；进入可折叠未使用区；切回模型不自动恢复 | 排除/恢复改变请求语义，失效报价；折叠/展开纯 UI 不失效 |
| 重排 | 图片、视频、音频各组内前移/后移，保持 client ID 与 asset ID；统一重算连续 position | 失效报价；焦点跟随原条目；提示检查描述，不改写“图1”等自然语言 |
| 换模型 | 提示词、全部素材及排除状态保留；能力改变使不支持项待处理；可兼容参数保留 | 不静默切 token/group；无效参数展示具体差异，由用户确认可用新值；确认后重报价 |
| 使用样片/复用新任务设置 | 先确认覆盖；读取新引用快照并逐项恢复/校验；不能截取“能用的前几项” | 缺失项占位阻断；不以当前 profile 猜 MIME；旧记录只有查看结果，不提供隐式旧输入重放 |
| 新建创作 | 用户确认后清空本次编辑草稿；保留结果选择、近期任务和独立 submission records | 取消旧草稿待报价，回到提示词/模型；不取消已受理生成 |

## 4. API 消费合同与报价一致性

### 4.1 新流程消费合同

| 入口/对象 | 保留的现有名字 | 新流程消费要求 |
| --- | --- | --- |
| `GET /api/video-studio/models?token_id=…` | profile `id`、`model`、`display_name`、`specification_version`、`specification`、`default_parameters` | 前端选择项持有 `id`，实际报价 `model` 是字符串；能力扩展字段以 backend.md 为准，不再从 SKU 后缀推断 |
| `POST /pg/videos/quote` | `token_id`、`model`、`mode`、`prompt`、`parameters`、`reference_assets`、可选 `sample_id` | 新增 `specification_version`；`reference_assets` 每项 `asset_id`、`role`、新增 `position`；不发送 `model_id`，不发送本地 `model_profile_id` |
| 引用角色与顺序 | `reference_video`、`first_frame`、`last_frame` 已存在 | 多模态新角色 `reference_image`、`reference_audio`；新生成不保留旧 `reference` 别名；每类 `position` 从 0 连续，服务端固定 image/video/audio 角色次序后按 position 规范化 |
| 模式 | `text_to_video`；已存在帧控制语义按后端合同 | 有参与素材用新增 `reference_to_video`；不继续发送旧 `image_to_video`；能力未开放的帧控制不显示 |
| 报价响应 | `quota`、`request_hash`、`expires_at`、可选 `amount`、`display_amount`、`other_ratios` | 只有服务端成功响应可显示可提交价格；不是 `quote_id`/`price`；不要自行乘音频时长或素材数 |
| `POST /pg/videos` | 报价请求字段 + `max_quota`、`quote_hash`、`quote_expires_at`；请求头 `Idempotency-Key` | `quote_hash` 取报价 `request_hash`；发送同一规范化语义，提交前及 transport dispatch 时再检查身份、能力、草稿、资产 ready 与报价时效 |
| 上传预留/完成/查询 | `filename`、`mime_type`、`size_bytes`、`purpose`、`multipart`；reservation `asset`、`upload_mode`、`part_size`、`expires_at`、`max_size_bytes` | 新增 upload purpose `reference_audio`；图片 purpose 仍是 `reference`，视频仍是 `reference_video`，不要和新的引用 role `reference_image` 混淆 |
| 资产 `VideoAsset` | `id`、`mime_type`、`duration_seconds`、`size_bytes`、`width`、`height`、`codec`、`state`、`failure_reason`、各媒体 URL | 真实内容与时长以服务端为准；探测证据存于后端 `media_metadata`，当前资产响应不公开该字段；音频无需 poster/preview 即可 ready |
| 新样片 | `id`、`model_profile_id`、`model_version`、`mode`、`parameters`、`video_asset_id` 等既有字段 | `reference_assets: [{asset_id,role,position,asset,unavailable}]` 已取代旧平行数组；asset 可为 null；admin 写只发绑定三字段；`sample_id` 仅来源，提交完整实际输入，不由后端补回已排除项 |
| 新任务结果输入摘要 | 结果 `id` 为 generation 数字 ID，`task_id` 为公开任务 ID | 已提供只读 `reference_assets` 和能力版本，引用形状同新样片，由任务绑定快照取得；缺失素材保留位置/不可用状态，不从当前草稿拼摘要，不保证旧任务可以复用输入 |
| 受理回执 | `task_id` 或 `id`、可选 `status/progress/created_at` | 继续 `parseVideoSubmissionReceipt` 的冲突/缺身份拒绝；收到 HTTP 200 不等于得到可信回执 |
| 无 task ID 的 unknown 恢复 | 已落地按幂等键只读 GET | `GET /api/video-studio/submissions/by-key`，`Idempotency-Key` 请求头；响应 `status=accepted/pending/not_found`、可选 `task_id/generation_id`；owner 校验由后端执行 |

能力应明确每类格式、数量、严格字节上限、单项/分类总时长、尺寸/fps 及有限组合规则；前端只消费 backend.md 锁定的结构。图片、视频、音频分别按各自 `max_count` 判断，上传剩余位置、链接添加与排除项恢复均按对应类别计算；不展示或校验三类合计数量上限。视频与音频各自累计时长，不能合并。原型 `9/3/3` 不是前端常量。拒绝/刷新类错误码必须与后端表逐一对应；未识别的发送失败不归类为“确定未受理”。

### 4.2 报价和发送门禁

1. 引用条目均完成恢复、ready、有验证证据，且没有上传、替换、不兼容、缺失、错误组合或无效参数，才构造 request 并启用 `useVideoQuote`。音频可先上传，但只有音频时不报价。
2. 规范化覆盖用户/token/有效分组隔离、精确 model、能力版本、推导 mode、trim 后 prompt、parameters 以及有序 `asset_id/role/position`。前后端使用同一语义规则；不再按 asset ID 排序，不再以 role 唯一拒绝多图。签名 URL、文件名、播放状态、折叠状态不进指纹。
3. 任一有效语义变动立即撤销“报价可提交”状态；旧请求较晚返回不能覆盖新草稿的 quote。React Query key 与提交前重新计算的 request key 必须一致。请求取消是优化，request key 比较才是正确性保障。
4. 每次用户明确点击生成创建一个操作 ID，先持久化操作记录再发 POST；保留现有 `submissionBusyRef`、身份 epoch、dispatch 回调二次检查以及禁止自动 auth retry 的行为。提交时固定输入快照，之后编辑草稿不改已提交任务。
5. `quote_stale` 或能力变化后只重新取能力/报价，成功后仍由用户重新点击，不能自动续发生成。确定拒绝才解除该次发送记录；不明受理保留并阻断重复新建。
6. unknown 有 task ID 时查现有任务；无 ID 时调用已实现的 by-key 查询。`pending` 继续查询，`accepted` 跟随任务，`not_found` 也不能自动换 key 再发；长期未决保留原键与快照，仅继续查询或交人工处理；没有确定未派发证据不得提供再次生成动作。不得把上传重试、校验重查、重新报价、查询受理和再生成统一成一个“重试”。

### 4.3 管理配置

- 旧模型缺少参考策略时，由管理员显式点击“应用参考素材预设”升级；只更新支持模式、参考策略并补齐缺失参数，保留已有默认参数、名称、排序和启用状态。通过原模型保存 API 递增能力版本、冻结旧版快照；仅打开编辑页不升级。
- 图片、视频、音频分别编辑数量、格式、字节数和对应尺寸/时长/帧率限制；已知模型只允许收紧预设边界，不设置三类合计数量字段。首帧与尾帧各最多一个，二者都占图片分类名额。
- 模型管理页的全局多模态开关仅超级管理员可见，复用 `video_studio.multimodal_enabled` 配置 API。编辑前强制刷新 `/api/status`，缺少明确布尔值或读取失败时不可修改。保存后显示服务实际状态，至少等待 60 秒并取得匹配的新状态才结束同步提示，75 秒仍未确认则停止自动轮询、提示手动刷新；成功手动刷新以服务实际值结束过期等待。此状态只证明被访问服务的观测值，不代替 adapter、当前版本和回退版本的发布验收。

## 5. 上传与播放实现

混合入口使用已安装 Uppy 核心与 AWS S3 插件。文件选择/拖入后按单个 File 确定预期媒体用途并预检，传入同一 `createVideoUploadProtocol`；当前 uploader 的单个 `purpose` prop 不能直接放宽 accept 后使用，否则音频会被当图片预留。用户/admin 作用域、上传 session 生命周期要稳定，不能因模型切换或排序重新挂载整个 Uppy 实例。

前端 accept 来自已验收能力并受本站限制：候选 JPEG/PNG/WebP、MP4/MOV、MP3/WAV；不沿用 WebM 作为 Seedance 保证。浏览器 MIME 为空时可作扩展名提示/预检，最终必须等待服务端内容校验。能力合同中的拟定 `max_size_bytes` 是后端已换算严格供应商边界后的**有效最大整数值，包含边界**，前端用 `size <= max_size_bytes`，不要再减 1；例如供应商“小于 50 MB”与“前端接受不超过 49,999,999 字节”一致。显示单位按 backend.md 的十进制 MB 规则，不能把本站 20 MiB 写成供应商 30 MB。时长保留原精度用于校验，显示格式化不能反馈为校验值。视频/音频分类总时长分别计算，不能加成输出时长或费用倍率。

`pending_upload → uploaded/processing → ready/failed` 是现有资产状态，不另造一套同名服务器状态。UI 可细分选择、预留、上传、暂停、网络失败、检查较慢；状态查询失败保留上次资产状态并显示“无法更新”，不能直接变成 ready 或重传。后台校验较慢复用 `getVideoAssetInspectionPollInterval` 等现有策略，避免每卡片新 setInterval。恢复只针对新格式：保留分片元信息，刷新后需要用户重新选择本地 File 时明确说明，不宣称浏览器已持久化文件内容。

参考音视频使用原生 `<audio controls preload="none">` / `<video controls preload="metadata">`；先通过 `useStudioMediaUrls` 得到当前会话可访问 URL。一个页面共享 active media 身份，参考音频、参考视频、结果、样片与详情互斥，切换或关闭弹窗暂停上一个；禁自动播放。音频不生成伪波形；设计中的竖条只可做装饰图标并 `aria-hidden`，不能暗示真实 waveform。对象 URL 卸载/替换后 revoke，签名到期重取访问权限，失败不泄露底层地址。

## 6. UI 状态与真实动作

状态可组合，不能仅有一个全页 `loading`。卡片负责文件错误，素材区负责组合错误，按钮附近重复最关键阻断原因；toast 是补充。以下每行至少有一个行为测试或 QA 用例映射。

| 状态 | 主要显示 | 报价/提交与动作 |
| --- | --- | --- |
| 01 入口准备/权限校验 | 原位置 skeleton/entry gate，保留页面结构 | 禁止报价；复用 token gate，不创建隐式 token/group |
| 02 无可用模型/模型加载失败 | 无模型或重试提示 | 保留草稿；重取模型，不切到隐形 SKU |
| 03 新草稿无参考 | 紧凑统一添加区，能力摘要 | 支持文字才可报价；视频必填型号提示补视频 |
| 04 选择文件预检失败 | 文件名、类型/大小/数量边界 | 合法文件保留；对应失败项不预留上传 |
| 05 上传中/暂停/取消 | 每文件进度与现有可用操作 | 禁止报价/提交；取消不撤销其他成功项 |
| 06 上传失败/离线 | 阶段原因、继续上传 | 重用新格式有效 reservation；不新建生成 |
| 07 上传完成、校验中 | 正在检查素材 | 不能当 ready；仅轮询状态 |
| 08 检查较慢/状态查询失败 | 等待较长/无法更新、检查状态 | 保留素材；不自动重传或误报失败 |
| 09 媒体校验失败 | 损坏、格式、时长等具体原因 | 替换/移除；不以扩展名强制放行 |
| 10 ready | 缩略图/音频标识、文件名、状态、真实时长 | 其余门禁通过后报价；预览/替换/移除/排序 |
| 11 只有音频或删除最后视觉项 | 请再添加图片或视频 | 禁止报价；已上传音频保留 |
| 12 分类数量/分类累计时长超限 | 对应类别的当前值和上限，定位冲突项 | 禁止报价；替换/移除/明确排除，无在线裁剪；不设跨类别总数量限制 |
| 13 参考音频与关闭声音不兼容 | 两种明确修复动作 | 用户开启声音或排除音频；不自动改开关 |
| 14 模型/高级方式不兼容 | 素材待处理，精确可用型号候选 | 保留素材；显式换型号/方式/排除后重新验证 |
| 15 本次未使用 | 折叠区数量与恢复/移除操作 | 不计请求/费用；切模型不自动恢复 |
| 16 替换进行中/失败 | 旧项仍可见，新项阶段和原因 | 处理中阻断；失败保留旧项；成功原位提交新 ID |
| 17 素材无权限/已删/验证证据不足 | 不可用占位、重传/替换 | 禁止提交部分子集；不自动删除草稿记录 |
| 18 报价中/失败/过期/能力变化 | 询价状态、具体重试/刷新原因 | 不能显示过期金额为可提交；重新成功也不自动生成 |
| 19 报价 ready | 实际 model、参与素材摘要、真实预计费用 | 用户点击；无假金额、无前端素材计费公式 |
| 20 发送中 | 提交中及本次固定配置 | 锁重复发送；不因切页面/点新草稿丢回执 |
| 21 受理不明 | 正在确认是否受理 | 查任务或已实现的 by-key；不以新 key 重发 |
| 22 排队/生成中/归档中 | 实际任务状态、允许继续编辑 | 不伪造进度；归档未完成不得开放下载 |
| 23 完成/播放失败/签名刷新 | 可播放结果或明确恢复提示 | 真实播放和下载；结果输入摘要来自提交快照 |
| 24 生成失败/结果已移除/查询失效 | 错误/不可用说明及任务入口 | 不承诺退款已完成，费用状态依现有服务端记录 |
| 25 新样片恢复缺项/旧格式请求被拒 | 逐项修复或刷新重新配置 | 不转换旧格式，不重放旧草稿，不默默丢素材 |

## 7. 视觉基准、tokens 与尺寸

权威基准为 [1440 深色完整图](../video-studio-multimodal-web/desktop-dark.png) 和 [1440 浅色完整图](../video-studio-multimodal-web/desktop-light.png)，均为 **1440 × 1218** 完整页面。静态源码用于确定规则，完整图用于核对最终视觉；读取 `design.css` 必须包含末尾覆盖规则，不能按前面较小字体照抄。真实数据、真实播放器和必要可访问性修正按第 8–10 节记录为受控差异。

将设计 token 映射到新创作页 scoped `--vs-*`，不要写入全局 `:root` 或覆盖其他页面主题。弹窗通过现有 portal 的容器 class/主题变量继承保持同一 theme；不要另建与应用主题不同步的 `data-theme` toggle。

| 语义 | 深色 | 浅色 |
| --- | --- | --- |
| page | `#111315` | `#f5f6fa` |
| shell | `#191b1e` | `#ffffff` |
| panel | `#1c1e21` | `#ffffff` |
| raised / hover | `#25282c` / `#2d3036` | `#f2f4f8` / `#e9edf5` |
| text | `#f1f1ee` | `#242a35` |
| muted / dim | `#a3a7ae` / `#7e848e` | `#677080` / `#788291` |
| border | `#34373c` | `#e0e4ec` |
| accent / on-accent | `#b1bcff` / `#191d2c` | `#4c63da` / `#ffffff` |
| soft | `#303442` | `#edf0ff` |
| success / background | `#97d6bb` / `#263b33` | `#35755b` / `#edf7f1` |
| warning / background | `#efc98b` / `#3a3022` | `#8b5a0b` / `#fff7e8` |
| error / background | `#f5a7ac` / `#3c282b` | `#b33445` / `#fff2f4` |

| 元素 | 1440px 精确基线 |
| --- | --- |
| 顶栏/侧栏 | 顶栏高 56、左右 24；展开侧栏宽 184，侧栏上/左右/下 padding 28/12/18 |
| 主内容 | max-width 1690，padding 24px 28px 20px；侧栏后居中；左边界 x=212，右边界 x=1412 |
| 页面标题/导航 | H1 24px、weight 650、letter-spacing -0.5px；导航上距 16、下距 22，tabs gap 26、底线 2 |
| 工作区 | 左列 410、列间 24、右列 `minmax(0,1fr)`；两 panel 顶部 y≈214；编辑器 x=212，结果 x=646，结果宽 766 |
| 面板 | 1px border、radius 12；编辑器 padding 20；原型没有现有代码的大面板阴影 |
| 模型 | radius 8，padding 10px 11px，图标底 33×33，label 11px、选项 13px |
| 素材 | 默认 3 列、gap 8；缩略图高 76、卡片 radius 7；文件名最终 11px；分类摘要最终 11px |
| 提示词 | 段前距 20；框 radius 8；textarea padding 12、min-height 119、最终 13px/line-height 1.85；工具行左右 10 |
| 输出设置 | 段前距 20；3 列、gap 10；select min-height 34、padding 7px 9px、radius 6；声音行上距 15 |
| 提交区 | 上距 19、分隔后 padding-top 15；价格字号 15；按钮宽 100%、高至少 43、radius 7；注释最终 11px |
| 结果 | header padding 17px 20px 15px；标题 16；画面两侧 margin 12，16:9、radius 7；中央播放按钮 54×54 |
| 结果元信息/引用 | details padding 16px 18px；型号 12、元信息 11；引用区 margin 0 18、padding 10px 0 12px、文字 11 |
| 近期创作 | 标题上/下距 22/12；3 列、gap 11；图像 16:8.5、卡片 radius 8，body padding 10 |
| 弹窗 | 普通最大 `min(580px,100vw - 32px)`、padding 24、radius 14；大预览最大 `min(1080px,100vw - 40px)` |
| 文字/图标/焦点 | body 13px/1.5；辅助文字末尾覆盖后多数 11、字段主标签 12；默认图标 18/stroke 1.7；focus 2px accent、offset 4 |

字体基线栈为 `Inter,"PingFang SC","Microsoft YaHei",system-ui,sans-serif`，原型未引入 Inter 文件。不可为了截图擅自加远程字体依赖；在同一浏览器/OS下使用该 scoped 栈核对，字体 fallback 差异须记入视觉证据。实际应用全局 `--font-body` 不因此改掉。真实媒体按结果比例展示，不将每个竖屏结果裁成 16:9；1440 基准 fixture 使用 16:9，其他比例另验可完整查看。

## 8. 现有样式与平台壳差异的解决办法

实际壳为 `AuthenticatedLayout → SidebarProvider / AppHeader / AppSidebar / SidebarInset → VideoCreatePage`，不存在原型 `.global-header/.app-shell` 的独立生产壳，也不存在可直接复用的 `StudioWorkspaceShell`。`Header` 当前依赖 `--app-header-height:3rem`（48px）；Sidebar 当前展开 `13rem`（208px）、图标 `2.75rem`（44px）。仅替换 composer 会导致整幅图左右/上下偏移。

实施采用**既有壳的创作路由 scoped presentation**：在既有 layout 通过当前 route 派生标记（拟定，例如 `data-workspace="video-create"`），在该作用域调整 CSS 变量和必要 spacing，使展开基准可达 56/184；紧凑壳尺寸按断点表处理。离开创作路由自然移除，不用 body DOM 副作用添加永不清理的 class，不复制导航 DOM，不改变真实菜单权限、动态链接、品牌、搜索、通知、语言、账户和侧栏折叠功能。`new-api` / `QuantumNous` 归属信息完整保留。用户主动折叠的壳单独验收，不强制改其偏好；截图基准使用展开状态。

平台功能比原型多，不能为高还原删功能或把导航改成 toast。原型“设计预览/示例素材”状态在正式应用移除；真实额外 header 操作采用现有紧凑排列。完整页必须提交对照图，明确列出这些受控差异，不能只裁左编辑器宣称完成。若 scoped 壳会影响其他路由，先修作用域；不以本任务为由重写所有平台壳。

样式收敛建议在新创作根节点增加一个明确 class（拟定 `.video-studio-create-multimodal`），以现有 `src/styles/video-studio.css` 为入口改 scoped 规则，而非追加整份 `design.css`。必须处理下列冲突：

- 现有左列 `minmax(380px,400px)`、gap 22、容器 padding 28 → 基准 410/24、padding 24/28/20。
- 现有 `.video-studio-page :is(button,[role=button]) {min-height:44px}` 与参考缩略图按钮/小型工具冲突：限定新页的视觉尺寸，同时扩大独立可点击区域，不把全站 44px 安全尺寸删除。
- 现有资产 grid 在 600 容器宽以下强制单列、近期第三项横跨两列，与新稿不同；仅新创作引用区和近期区替换规则。
- 现有 `@container video-studio (max-width:905px)` 提前堆叠，原型按 viewport 940 堆叠；不能两套规则叠用。保留容器感知以支持真实侧栏变化，按下面明确宽度门槛重定新页，避免旧规则二次触发。
- 现有 14px 控件字体、44px select、阴影及 `--vs-muted/#aaadb3` 等与设计不同，按第 7 节 scoped 调整；错误/焦点/触控对比度优先。需要增强对比度的文字使用更清晰 token 并记录差异，不追求低对比像素。
- Portal 弹窗不在 page DOM 下时显式传入 scoped class/同主题 token；禁止为弹窗另加全局 raw 色值或任意 z-index 覆盖 Base UI 层级。

## 9. 响应式与高还原验收

所有尺寸以 CSS px 和浏览器 100% 缩放说明。主要验收 viewport **320×900 / 390×844 / 1280×900 / 1440×1000 / 1920×1080**，均含浅深主题并保存完整页面截图；1440 原始基准图的 1218 是 full-page 结果高度，不要求把浏览器 viewport 高度固定为 1218。另抽查 768、1024 和 600/940/1200/1600 前后各 1px，不把每个状态与全部尺寸做笛卡尔积。

| 宽度/断点 | 生产实施目标 |
| --- | --- |
| 1440 | 完整深浅基准；184 侧栏，主内容 24/28/20，410 + 24 + 右列；3 列参考和近期 |
| 1280 | 仍为 184 侧栏、410 左列、24 gap；可用结果列约 606；不得因旧 905px container 规则提前单列 |
| 1920（≥1600） | 内容 max 1690 居中，左列 450、gap 24、编辑器 padding 24；缩略图 90、textarea min 134，结果 header 20/22，stage margin 14 |
| 941–1200 | 紧凑侧栏设计 64；内容 padding 22；左列 370、gap 20、编辑器 padding 17；结果 actions 可换行 |
| ≤940 且 >600 | 单列，唯一 composer 在结果前，编辑器 padding 24；参考通常 4 列，恰 3 项可 3 列；高 100；宽度不足时按最小可用卡宽进一步降列 |
| 390（≤600） | 移动壳/抽屉沿现有控制，顶栏目标 52；内容 19/15；编辑器 padding 18；参考 3 列、缩略图 75；近期 2 列、第三项不强制横跨；播放在可展开弹窗中 |
| 320（≤360） | 参考 2 列；必要时长文件/错误卡转 1 列；输出设置 2 列、时长占整行；添加弹窗单列；隐藏非必要装饰副标题，保留错误/费用/提交/操作 |
| 容器不足 | sidebar 用户设置或嵌套容器造成可用宽不足时优先单列，门槛按新页最小列宽计算；不用硬宽导致页面横向滚动；主基准展开壳仍满足上述画面 |
| 200% 浏览器缩放 | 按实际 CSS viewport 重排，如 1440 物理宽约 720 CSS 宽应单列；不使用 CSS transform 整页缩小；文字/按钮/原生播放器完整可操作，无裁字/遮挡 |

触屏控件字体至少 16px 防 iOS 自动放大；操作触达面积至少 44×44px，视觉小图标可通过有间距的外层按钮实现，不能让 hit area 相互覆盖。桌面非触屏小操作至少满足 24×24px/足够间距；原型 20px 删除、hover 隐藏预览和粗指针 32px 控件不能原样照搬。长文件名可截断但需可访问完整名称，卡片错误必须换行，长法语/俄语/越南语不能用无限 nowrap 撑宽页面。

视觉验收必须使用固定 fixture（同模型名、一张图片/一个视频/一段音频、相同描述、16:9 结果、3 个近期项、相同主题/OS/DPR/字体），关闭非必要动画，等待字体和图片完成后全页截图，并记录 viewport、缩放、提交 commit、fixture 与浏览器版本。设计媒体可用于测试 fixture，不得混入真实用户任务列表。主对齐线/列宽/面板 padding 与基准偏差目标 ≤2px，标题/按钮/卡片尺寸偏差 ≤2px；文本换行按同环境基准一致。结果媒体真实 controls、必要错误信息和无障碍 hit area 允许经设计/QA列明的差异，不能以该容差隐藏结构变化。若 QA 另设像素 diff 阈值，以其记录为准，不把文字抗锯齿差异等同功能缺陷。

失败条件：只提供裁剪截图；只有深色通过；1440 左列重新拉宽；1280 无故堆叠；320 横向滚动；200% 看不到生成按钮/关闭弹窗；暗色 token 直接覆盖全平台；错误状态使页面关键结构跳动且无法操作。

## 10. 原型演示行为替换表

| `design.js` / HTML 演示 | 正式行为 |
| --- | --- |
| `models` 的 `sd25video/sd20video/sd25image`、静态 limits | 使用真实 API `model`、用户可见候选及能力；不把演示 ID 发后端，不静态承诺组合 |
| `initialReferences`、`addReference(kind)` 直接插入 ready 示例卡 | 真正 file input/拖入 → 预留 → 上传 → 校验；显式呈现各阶段 |
| `referenceGroups()` 只按演示 model 判断视频待处理 | 使用服务端能力和资产验证结果；所有参与但不兼容项阻断，不从 request 默默过滤 |
| `applySettings()` 载入示例数组与参数、`renderDraft()` 禁用超出当前模型时长的选项 | 覆盖前确认；显示具体参数变化，用户确认后应用；素材不截断、失败保留 |
| `quoteChanged` 布尔值和“生成前询价” | 实际 query key、服务端 quote、过期/失败/版本变化；不显示模拟金额 |
| `submitSimulation()` / 本地 tasks 插入排队卡 | 实际 submit hook、落盘回执、Idempotency-Key、受理查询、服务端任务列表 |
| result 静帧、无 src 的 `<audio>/<video>` | 真实签名媒体、原生控件、互斥播放、刷新与失败状态 |
| download 示例 WebP | 服务端授权 `download_url` 对应视频；未 ready 禁止下载 |
| taskCount=2 / 仅演示 3 个 tasks | 真实分页/统计语义；没有权威总数时不把当前页数量当总任务数 |
| 草稿“已保存在本次预览中”、刷新重置 | 新版本按用户持久化；保存失败明确提示，刷新恢复新格式，旧格式不读取 |
| 原型“本次未使用”仍在大列表呈灰态 | 按 PRD 收入折叠未使用区；不计有效摘要，恢复是显式操作 |
| `data-info` 导航只 toast、手改 DOM theme | TanStack Router 真实导航和现有 theme provider；通知/账户继续真实组件 |
| `innerHTML`、手写 focus/dialog、演示 waveform | React JSX、Base UI focus 管理、原生媒体；不复制模拟 JS，不新增真实波形承诺 |

## 11. 国际化、可访问性与代码质量

六基准语种 `en/zh/fr/ja/ru/vi` 的新增文案全部完成；运行时已经存在的繁体 `zhTW`（locale 文件 `zh-TW.json`）也要完整覆盖，不删除第七语言或让它显示 key。每个组件自行 `useTranslation()`；新增 key 用英文源文案，不再新造 `videoStudio.*` 层级 key；已有键按本任务范围复用，不做全局改名。

数量/秒/字节限额、文件名、模型名和错误值使用参数化翻译，复数按语言处理；`toIntlLocale`/Intl 格式化时长和数值展示，不能拿格式化字符串做校验。稳定错误码映射翻译，服务端补充信息安全显示为文本。区别“参考音频”和“视频声音”，不把“生成音频”翻译为上传声音。

locale 修改只能通过 skill 规定的 `add-missing-keys.mjs` 工作流并最终 `bun run i18n:sync`；已有 skill 脚本六语种列表需在本功能写入时覆盖已存在 `zh-TW.json`，不能直接手改 JSON。当前 `scripts/check-i18n.mjs` 的 `REQUIRED_LOCALES` 只有 en/zh，其通过不证明六/七语种齐全；本功能改动键清单须另做所有运行时语言的定向键/插值校验。动态 label/错误键登记 `src/i18n/static-keys.ts`。不为本功能顺带清理全库既存未翻译项。

键盘流程必须覆盖添加、类型选择、关闭弹窗、替换、移除、前移/后移、排除/恢复、模型候选、报价重试、提交、unknown 查询、预览、下载。拖拽仅增强，hover 不是唯一入口。组使用有标题的 section/fieldset，卡片可为 list/listitem，不将整张含操作按钮的卡片嵌套到 button 内；图标按钮带含文件名的 aria-label；焦点和排序后身份稳定。

错误与控件通过 `aria-describedby` 关联；组合错误提供持久文本；状态区域 `aria-live="polite"`，只播报阶段转换与完成，不播报每个上传百分点。确需立即修复的提交错误用 alert；不在每次 render 重复播报。弹窗 title/description、焦点 trap、Esc、关闭后焦点恢复使用现有组件。文本对比 4.5:1，重要非文本控件/焦点 3:1；状态同时有文字/图标。媒体无自动播放、减少动效偏好生效，200% 下可 reflow。

类型使用明确 discriminated union/`unknown` 边界解析，禁止 `any` 和把 API 响应直接断言为有效能力。事件中的提交/替换副作用留在 handler，不用 Effect 状态观察触发收费请求。订阅 Zustand 最小 selector，查询按用户隔离；不在全表单每次键入时重建 Uppy，长列表也不新增不必要虚拟化。只在实际重复/昂贵计算处 memo；不写仅复制实现的测试、不用 sleep/随机大循环验证正确性。

## 12. 实施顺序与验证入口

以下保留实施顺序和可选验证入口，不代表每条命令都已执行。实际已运行的定向测试、TypeScript、lint 和 i18n 结果见 [实现与验收记录](implementation-qa.md)。实施属于跨模块 L2，涉及上传安全/幂等/全局会话部分按 L3 审查；默认采用最小相关验证，广泛命令及预计超过五分钟/未知时长的检查按根 AGENTS 先说明命令与必要性并获明确许可。

1. 锁定 backend.md 中所有拟定字段和有限规则，制作确定性新格式 fixtures；先改 types/schema/domain/store，复核无旧流程分支或数据删除。
2. 贯通混合上传、真实媒体探测/恢复和有序绑定；再接报价/受理查询，先保护幂等和身份，再增加交互。
3. 组装统一页面及管理端新样片，完成 scoped 样式和实际平台壳适配；两主题/多尺寸对照，不等待全部功能完成才发现布局问题。
4. 完成全部运行时语言、键盘和恢复矩阵；由 QA 执行真实媒体/上游合同验收。前端 mock tests 通过不等于外置适配器或计费验收通过。

已确认 `web/default/package.json` 的 `test` 为 `vitest run`，`vitest.config.ts` 使用 jsdom、`src/test-setup.ts`、`src/**/*.{test,spec}.{ts,tsx}`。优先扩已有测试；按批次仅执行输入变更的 scope，不重复跑无变更的已通过命令。

在 `web/default/` 中的定向命令：

```sh
# 草稿、归一化、报价/幂等规则；均为现存测试文件
bun run test src/features/video-studio/video-domain.test.ts src/features/video-studio/video-submission-protocol.test.ts src/features/video-studio/video-admin-forms.test.ts src/stores/video-studio-draft-store.test.ts

# 上传和恢复；新音频、替换/解绑回归应加在对应实际测试边界
bun run test src/features/video-studio/video-upload-protocol.test.ts src/features/video-studio/video-upload-lifecycle.test.ts src/features/video-studio/video-reference-hydration.test.ts

# 组合交互、quote/submit、真实页面结果衔接
bun run test src/features/video-studio/components/video-composer.test.tsx src/features/video-studio/hooks/use-video-submission.test.tsx src/features/video-studio/api-submission.test.ts src/features/video-studio/video-create-page.test.tsx src/features/video-studio/components/video-generation-player.test.tsx

# 仅实际修改文件；下列为基础改造清单，实施时补入真实新增/修改路径
bunx --no-install oxlint -c .oxlintrc.json src/features/video-studio/types.ts src/features/video-studio/schemas.ts src/features/video-studio/video-domain.ts src/features/video-studio/components/video-composer.tsx src/features/video-studio/components/video-asset-uploader.tsx src/features/video-studio/hooks/use-video-submission.ts src/features/video-studio/video-submission-protocol.ts src/stores/video-studio-draft-store.ts

# 只检查改动 CSS，避免使用全目录 format wrapper
bunx --no-install oxfmt -c .oxfmtrc.json --check src/styles/video-studio.css

# locale 写入后按技能要求归一；它不是应用测试，也不证明翻译质量
bun run i18n:sync
```

测试应明确断言：同组交换 position 改变 request key，但数组物理换序且 position 不变不改变语义；签名 URL 刷新不改变；普通参考重复 asset/非连续 position 被拒；只音频无法报价；替换失败旧素材保留；模型变化不截断；排除只经显式操作；unknown 不重发；新草稿不清待确认记录；用户切换后晚到响应不写错用户；有效最大字节值可用、再加 1 被拒及 15.01 秒不会舍入放行；旧格式不被恢复。新增组件测试文件若实施中确有需要，创建后再加入命令，不能把未创建的测试命令称为已可运行。

**类型检查状态：** 前序文档阶段发现 `bun check -b` 不可用；本地实施阶段已通过 `bunx tsc -b --pretty false --noEmit`，确切结果见实现记录。后续仍按实际改动选择最小范围，不把 lint/测试替代类型检查，也不把历史工具缺口写成当前未完成状态。

**格式检查边界：** `scripts/format-with-protected-headers.mjs` 会遍历整个前端、临时写文件再恢复，即使 `--check` 也不是单文件工具；不将它伪装成 targeted check。TS/TSX 改动用只覆盖实际文件、保留许可证头的检查方式，并逐个核对 diff；不得为了格式通过修改 protected attribution。

视觉/浏览器验证用现有可用浏览器工具启动本地应用并按第 9 节截图、操作，不引用设计 README 中本机 `/tmp/check-video-studio-*.cjs` 作为可复用仓库测试。这些脚本不在仓库，也只测静态设计。生产验收最终记录确切命令、结果、截图路径、未验证项；明确说明全量套件是否跳过。前序文档阶段未运行应用测试；当前本地实现的定向测试已执行，完整验收矩阵仍以实现记录中的覆盖与缺口为准。

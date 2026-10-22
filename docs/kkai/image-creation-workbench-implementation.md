# 图片工作室创作工作台：实现与还原验收方案

日期：2026-10-22。状态：代码已本地实现，定向行为、类型、lint 与浏览器夹具检查完成；真实服务端到端联调与发布未执行。

目标：将已确认的「专注工作台 · 完整功能版」预览落到现有创作页，保留全部现有能力，实现生成后同页查看、再生成、成品续编及细腻动效。功能、视觉、交互三项均为交付门槛，不能以“功能可用”代替设计还原。

## 1. 基线与范围

- 方案评审基线：`production/kkrich`，`1a51eeab93d829cea13290ecdf54af861b6ae3bb`。实际实施基线：`f3329ab8b1eb31b15909009782be00baa4338ebc`；实施开始时工作区干净。
- 视觉基线：[已确认交互预览](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-workbench-complete.html)。SHA-256：`1bccff3e6fea40e651be09f22f1f7cbb8d5391985fff1003328324247c544946`。旧 `creation-workspace-review.html` 不再作为依据。
- 行为基线：[完整功能审计](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-feature-audit.md)，F01–F26 在本文件第 6 节落到验收项。
- 本轮已实现 `web/default` 创作工作台及受鉴权原图读取端点，保留作品库中复用 Composer 的兼容性。未提交、推送或发布；本地只编译了视觉检查入口，没有进行生产构建或访问生产。
- 完整功能落地包含一个小型后端能力：受鉴权的成品原图读取。无数据库结构变更；不能把整项工作标成纯前端发布。
- 预览中的示例模型、固定像素范围、示例价格、模拟任务进度、演示状态切换器均不得进入真实业务。素材沿用实际作品和既有样例，无需为此次开发重新付费生图。

三位并行技术角色已完成源码评审：前端架构负责状态和接口；视觉与动效工程负责控件尺寸、主题、响应式及动效；质量保障负责行为回归、视觉差分和发布阻断条件。本方案汇总其结论并明确最终取舍。

## 2. 架构定案与文件分工

采用 **一个持续挂载的 ImageComposer + 一个独立结果舞台**。保留 RHF/Zod、尺寸解析、报价、Token 门禁、上传和幂等流程，在原引擎上调整结构和外观。

| 文件 / 模块 | 改动与边界 |
| --- | --- |
| `web/default/src/features/image-studio/image-create-page.tsx` | 按用户 ID 挂载工作台；左创作、右结果；单个页面滚动区；管理本次任务 ID、当前查看任务/图片、载入意图；移除提交后跳库；保留样例深链接 |
| `components/image-composer.tsx` | 增加 `presentation: 'workbench' \| 'sheet'`；统一完整载入上下文；提交快照与编辑修订隔离；向页面上报真实活动状态。默认 Sheet 行为保持兼容 |
| 新增 `components/image-creation-results.tsx` | 状态、原比例主图、同批全部结果、本次创作历史、再生成、继续编辑、下载、详情；不复制报价或提交逻辑 |
| 新增 `components/image-resolution-field.tsx` | 预设、自定义宽高、交换、比例提示、真实限制与错误；仍只有一个规范的尺寸参数来源 |
| `components/image-parameter-fields.tsx` | 继续渲染全部动态 select/integer/boolean；分组不漏字段，错误组自动展开；不把参数写死为某个模型 |
| `components/image-reference-field.tsx` | 保留多图、顺序、文件名、上传和重试；增加局部样式及稳定 ID 的排序动效 |
| `hooks/use-image-references.ts` | 增加原子 `replace(files)`，完整校验成功才替换；返回明确结果，保留 scope/revision 过期保护 |
| `queries.ts`、`components/image-regeneration-sheet.tsx` | 提取共享原输入查询和单任务 query options；工作台与旧 Sheet 共用，保留用户隔离、可见性和重试规则 |
| 新增 `image-output-reference.ts`、`api.ts` | 同源读取成品原图，生成 File，取消/超时/错误处理；不把签名 URL 当持久数据，不直接复用 output ID |
| 新增 `web/default/src/styles/image-create.css` | 创作页作用域内的视觉变量、容器断点和动效；不修改全局 Button、Select 等默认外观 |
| `router/kkai_routes.go`、`controller/image_studio_library.go`、`service/image_output_edit_source.go` | 新增第 5 节的受鉴权成品读取端点，复用存储与现有授权规则 |
| `common/console_contract.json`、`web/default/console-contract.json` | 后端声明 `image_output_edit_source`；前端优先使用新接口，旧后端通过已要求的 `studio_media_urls` 授权签名回退，不把新能力列为全局必需 |
| 六语言 locale 与必要静态 key 声明 | 仅补实际新增产品文案，遵循 i18n 技能脚本流程 |

以上 `components/`、`hooks/`、`queries.ts`、`api.ts` 路径均相对 `web/default/src/features/image-studio/`。不为这次改造另建全局状态库、第二套表单或新的组件库。已有 `motion/react`、Base UI、Lucide、React Query 足够。

### 2.1 当前代码中必须处理的问题

| 现状证据（代码基线行号） | 直接套样式的后果 | 实现要求 |
| --- | --- | --- |
| `image-create-page.tsx:62` 无条件跳库 | 成功结果离开创作流程 | 所有提交响应留在当前页，由任务状态驱动结果区 |
| `image-create-page.tsx:99`、`:113` 分别挂载 desktop / Sheet | 跨断点卸载表单，编辑和 File 状态丢失 | 一个 DOM 实例，CSS 改列数；移动端不另挂 Composer |
| `image-composer.tsx:160` 初始化模式/参考图，`:368` 复用 effect 仅 reset RHF | 原作参数与模式、参考图不一致 | 完整上下文原子载入，不能只换 props 或 reset 字段 |
| `image-composer.tsx:736` 同步成功后清草稿 | 后续输入的新稿被旧请求清掉 | 按提交时编辑修订判断，只有仍为同一稿时才清理 |
| `use-image-references.ts:135` select 是追加且返回 void | clear 后校验失败会丢掉旧参考组 | 验证完成前保留旧组，新增原子 replace |
| `controller/image_studio_library.go:119` content/download 返回 302 | img 可显示但跨域 Blob 读取可能失败 | 新增同源受鉴权读取路径，不依赖对象存储 CORS |
| `service/image_async_submission.go:110` 只接受本人 ready reference | output ID 直接用作参考会被拒绝 | 成品转 File 后走既有参考验证/上传 |
| 通用控件默认约 28–32px | 与设计稿 40–46px 控件明显不同 | 精确样式作用到实际 input/select/button；NativeSelect 的外层 className 不等于内部 select 高度 |

## 3. 状态、提交与载入协议

### 3.1 状态归属

| 数据 | 唯一来源 | 生命周期 |
| --- | --- | --- |
| 当前模式、模型、提示词、参数、数量 | Composer / RHF 与既有 domain 逻辑 | 用户编辑；不由结果查询反写 |
| 本地 File、原参考 ID、上传状态 | 既有 reference hooks | 内存；按用户、模型规格、限制作用域隔离 |
| 普通生成草稿 | 既有用户隔离 draft store | 延续 300ms 保存；不存 File、Blob、签名 URL |
| 本次提交快照 | 提交开始时冻结 | 直到该记录离开当前工作台；不读取后续 watch 值 |
| 服务端任务、原输入 | React Query | 既有 privateUserQueryKey；不复制一份可变任务到 Zustand |
| 会话任务顺序、当前任务/图片 ID | 工作台本地状态 | 此页面会话；离开后通过作品库查历史，不承诺永久版本树 |
| 签名媒体地址、当前显示图片 | useStudioMediaUrls 与派生显示状态 | 有效期/用户变化后重取；不存进提交快照 |

外层工作台以 `userId` 为 key。换账号立即卸载表单、结果、载入意图和原图读取；请求取消或以账号/修订校验丢弃迟到结果。现有 Query key 隔离继续生效，不能仅清表单却保留前账号图片。

### 3.2 提交过程

1. 点击生成时，冻结 mode、model、prompt、parameters、count、reference identity、editor revision；按既有门禁、校验和报价指纹检查提交。
2. 可选 `onActivityChange` 只报告真实阶段：参考文件处理、上传（有真实百分比）、服务端校验、提交。没有真实进度的阶段只显示文字与等待动效。
3. `onSubmitted(generation, snapshot?)` 保持旧调用兼容；回调将任务写入同一用户的查询缓存，并加入本次记录，自动选择这次新任务。
4. 成功、部分成功、失败、提交中、结果未知都留在创作页。后续轮询完成只更新对应记录；用户已主动看其他记录时，不强抢选择或焦点。
5. 提交时保留上一张成品，明确标为“上一版”；当前新任务的等待/失败状态不能被旧图伪装成成功。
6. 结果快照不可受下一版编辑影响。旧响应只有在 `currentEditorRevision === submittedRevision` 时才可清除对应持久草稿；编辑过的新稿以及后续轮询均不得触发清理。
7. 同时跟踪本次所有 submitting 任务，不只轮询选中的任务。复用 3/5/10 秒递增轮询、后台页面暂停和焦点刷新；终态停止轮询，unknown 按原恢复规则手动检查，避免整库轮询。

### 3.3 载入意图与草稿保护

给 Composer 一个带单调递增 `revision` 的 `loadRequest`，区分 `sample`、`regenerate`、`edit-output`。参数类型复用现有类型，不为同一实体另造 DTO。

- `sample` 携带完整样例配置；保留 sample_id 与深链接行为。
- `regenerate` 携带 generation 和原输入；默认 1 张、不继承 sample_id。原模式未知须明确选择，原模型失效不自动换模型，参数规格变更须显式复核。
- `edit-output` 携带已验证成品 File 和来源 generation/asset ID；当前选定模型继续作为编辑目标，保留当前提示词和相容设置，切编辑模式、数量 1、移除 sample_id、替换为所选成品参考。若当前模型不可编辑或当前设置不合法，明确阻断并由用户选择，不静默改模型/尺寸。该入口只准备编辑，不自动调用模型。

真正应用前比较完整当前输入：文本、模式、模型、参数、数量、原参考 IDs、本地文件顺序。会覆盖用户改动时使用现有 Dialog 确认，原图读取与验证期间仍可取消，失败不修改旧稿。

应用必须一次更新 RHF 值及 mode、savedReferenceIds、usingSavedReferences、reuseModeConfirmed、settingsReviewed、appliedReuseRef、旧报价失效状态。相同任务再次载入也由 revision 生效，不能以 generation ID 相同而忽略。

首次载入原作前保留普通草稿的内存快照，含本地 File 引用和顺序；复用上下文保持 `persistDraft=false`，提供“恢复创作草稿”。恢复仍经过当前模型/限制校验。跨账号或页面卸载销毁内存快照。反复载入原作不得层层覆盖那份原始普通草稿。

参考替换采用先验证、后提交：对目标 scope 完成类型、文件大小、总量、元数据检查，再一次提交新组。模式切换导致引用 scope 改变时，必须将目标 scope 一并纳入载入事务，不能先 replace 又被 mode effect 清空。异步完成前账号、模型、规格、限制或载入 revision 已变化，整次载入作废，保留用户更新后的输入。新建 Object URL 失败/取消时释放，旧 URL 仅在替换成功后释放。

## 4. 结果舞台与操作语义

| 状态 | 展示 | 可用动作 |
| --- | --- | --- |
| 尚无任务 | 灵感入口、简洁空状态 | 选样例、完整编辑设置 |
| 文件处理 / 上传 / 校验 | 真实阶段；上传才显示真实百分比 | 上传失败重试；原流程允许的取消上传，不伪造取消生成 |
| submitting | 等待、已提交设置、后台更新提示；可查看上一版 | 查看状态、切换已有记录；禁把它当失败新建重试 |
| unknown | 明确“结果待确认”，保留回执 | 检查旧请求；不得新 key 自动再发 |
| succeeded | 所有 ready 图片，实际尺寸、站内结算消耗 | 再生成一版、用所选成品编辑、逐图下载、大图/详情 |
| partial | 显示完成数 / 请求数，保留所有已完成图片及真实错误 | 同成功，重试是新报价的新任务 |
| failed | 受控错误、原参数摘要；旧成果在历史可访问 | 载入设置后修改/再生成，不自动收费重试 |
| archive_failed | 如实说明结果归档异常 | 检查状态和现有恢复说明；不得包装成普通再生成失败 |
| 图片签名/加载失败 | 对应图片的可恢复错误状态 | 重新签名/加载；不填入演示图、不改变任务结算状态 |

ready 资源按 `position` 再 `id` 排序；同批 2–4 张全部可达。主图 `object-fit: contain`，用真实宽高；缩略图可以 cover，主图不得裁切。站内消耗沿用 `final_quota / quotaPerUnit` 与 `formatBillingCurrencyFromUSD`，不能显示上游成本、示例 `$0.02` 或未结算的预估值冒充实扣。

原图成功解码后才切换显示。切图请求带 asset ID/revision，快速 A→B→C 时 A/B 的迟到 load/decode 不能覆盖 C；新图读取期间保留已显示图并标记正在切换。签名续期不重复播放成功揭示。

四类恢复动作严格分开：上传重试；检查旧提交；确认 not_found 且回执有效后用原 key 安全重发；载入设置后重新报价生成新版本。报价变更要求再次确认，unknown/过期回执不能通过“再试一次”绕过防重复计费。

## 5. 成品继续编辑：确定的接口方案

采用新增 **`GET /api/image-studio/assets/:id/edit-source`**。它只负责读取本站本人已完成作品的原始字节，不复制数据库资产，不创建生成任务、不计费。前端通过现有鉴权 API 客户端同源读取 Blob，再走 File 校验与既有上传/同步 multipart 链路。

### 5.1 后端契约

- 路由挂在 `UserAuth + ImageStudioAccess` 分组；不放进可由媒体签名访问的 content/download 分组。
- 复用 `GetAuthorizedImageAsset(..., isAdmin=false, ...)`，并明确要求 user scope、本人 owner、output kind、ready、未删除；关联任务为 settled 且 succeeded/partial。管理员也不能通过此个人操作读取他人原图。
- 只从数据库资产记录的 `ObjectKey` 读取 `ImageAssetStore.Get`；不接受外部 URL、任意 object key 或 thumbnail 参数，不产生重定向。
- 字节上限取当前 `image_studio_setting.Get()` 中输出与引用单文件/总量上限的最小值，并沿用像素限制。检查资产元数据及对象元数据，再限定实际读取字节；不能仅信 Content-Length。引用上限与输出上限不相同的场景必须返回明确错误。
- 复用 `service/image_archive_fetch.go` 的 `HTTPImageArchiveFetcher.Ingest`：临时文件、`LimitReader(max+1)`、MIME 魔数、像素和 SHA256 检查。完整读取后核对数据库字节数、已记录的 SHA256 与对象长度（已知时）；不另写图片解码器或无界 `io.ReadAll`。
- 所有检查通过后才写入 200，从已验证的临时文件发送内容；禁止直接把 R2 body 交给 `DataFromReader`。读取超时初始值为60秒，继承请求 context；取消必须实际中断源读取。所有路径关闭对象流并清理临时文件，发送后也清理。
- 成功返回真实 image MIME、准确长度、安全文件名、`Cache-Control: private, no-store`、`X-Content-Type-Options: nosniff`；不泄露存储 key、临时签名或上游地址。读取错误使用当前受控错误响应风格。
- 无登录/权限、作品删除、未就绪、类型不支持、超过引用限制、存储失败分别映射为可理解错误；复用既有 403/404、413 和图片校验错误，存储/超时使用稳定脱敏错误码。S3 Get 的嵌套对象缺失错误要在服务入口归一化为不存在，不能直接暴露底层错误。读取失败保留原稿和参考图，用户可重试读取。

### 5.2 前端与兼容性

- 优先使用认证 `api` 同源请求新接口、Blob 响应、`disableDuplicate: true`，按账号和载入 revision 绑定 AbortSignal；页面离开、账号切换或新载入意图时取消。整个读取流程共享75秒截止时间；Blob 错误响应解析受控 JSON 错误码，再显示翻译后的提示。
- 仅新接口返回404/405且没有非空业务错误码时，兼容旧回滚后端：用资产 ID 构造固定 `/api/image-studio/assets/:id/content`，经已鉴权 `POST /api/studio/media-urls` 重新校验资产权限并取得短签名原图。401/403、业务404、超时和服务端失败不触发回退；不读取传入的任意 `content_url`。
- 签名原图由原生 `fetch` 读取，不使用会附 Bearer 的全局 API 客户端；设置 `credentials: omit`、`referrerPolicy: no-referrer`，拒绝重定向、非 HTTPS 和含用户信息的 URL。沿用 MIME、精确字节数和引用上限校验，并对回退原图解码检查真实宽高；失败或取消保留草稿。回退依赖对象存储 GET CORS，发布前必须在生产 Origin 以真实浏览器 GET 验证；历史 OPTIONS 记录不能替代本次验证。
- 成品进入编辑前仍通过现有 `getImageReferenceBatchMetadata` 和模型引用限制；output ID 仅记录来源，不作为 reference ID 提交。
- 验证成功才执行第 3.3 节的完整载入；替换参考组默认是 1 张所选成品。保留随后追加、排序、删除参考的原能力。
- 成品原始尺寸不强制覆盖用户当前输出尺寸；上传图尺寸和最终输出尺寸是两个概念。
- 后端实际支持的 profile 继续声明 `image_output_edit_source`；前端将它作为优先路径，`required_capabilities` 仍要求已存在的 `studio_media_urls`，不强制新能力。新旧后端都能保留续编流程，新增能力本身不改变现有 API contract 版本。
- 后续发布按 backend/combined 范围及维护中的 NewAPI runbook 执行，验证当前及回滚后端兼容；format 2 前端激活位于后端 candidate/rollout 事务之外。具体格式、控制器和生产状态必须发布时获取，本文件不宣称当前线上已支持。

## 6. 功能逐项保留与验收

| ID | 落点与必须通过的行为 |
| --- | --- |
| F01 生成/编辑 | 顶部常驻切换；保留模型/提示词，原手动切模式清本地引用语义不变；编辑 1 张；提交时禁切 |
| F02 全部模型 | 两种模式展示全部 enabled 模型；不照预览硬编码三个模型、不暗中过滤 |
| F03 尺寸预设 | 按 specification 真实选项；通过 request_key=size 找字段，不能假定 key 为 size |
| F04 自定义像素 | 仅 custom_size 支持时显示；与预设同区且一级可见；宽/高/交换映射到同一 RHF 参数 |
| F05 完整尺寸验证 | 格式、正安全整数、步长、边长、总像素和比例约束均保留；不兼容值保持可见并阻止提交 |
| F06 提示词识别 | 保留中英文、普通描述、--size、x/×/*、px/像素的真实解析；生效后反馈与重新报价 |
| F07 排除/冲突 | 引用、代码、URL、路径、示例、参考图、否定不误识别；不完整/非法/多尺寸冲突明确阻断 |
| F08 手动优先 | 只有识别尺寸变化才自动应用；润色或删除提示词尺寸不覆盖手动值；恢复草稿不重置手动值 |
| F09 切模型联动 | 保留提示词、跨真实 size key 映射；其他参数采用新默认；不兼容尺寸不静默降级 |
| F10 动态参数 | select/integer/boolean 全保留；质量只在真实配置存在时提升展示；其他全部参数均可达 |
| F11 张数 | 上限 min(参数 max、effective_max_outputs、全局 4)；最大 1 或无配置时隐藏；编辑 1 张 |
| F12 多参考图 | JPEG/PNG/WebP，多选追加、有序、序号/文件名、预览、上移/下移/删除；最多按模型限制且不超 8 |
| F13 引用校验 | 类型、空文件、单文件/总量、数量、元数据，模型/规格/限制作用域变更规则保留 |
| F14 上传恢复 | 原异步直传、进度、验证、失败重试、分片续传保留；刷新不能宣称已恢复本地 File |
| F15 原参考复用 | 有效异步 reference IDs 按原顺序复用、删减或整组替换；失效需重传；删完仍是编辑模式 |
| F16 样例 | 保留完整 gallery 分页、选中、空态、错误重试；应用整套配置；编辑模式也能用 |
| F17 样例链接 | create?sample=id 仍可进入并载入；窄屏滚动到设置，不另起表单实例 |
| F18 用户草稿 | 用户隔离，原 300ms 保存/恢复、空提示词清理；不持久化 File；覆盖确认及恢复入口 |
| F19 原作再生成 | 载入真实模式/模型/参数/参考，默认 1 张、不继承 sample_id、不覆盖普通草稿 |
| F20 旧任务修复 | 未知模式明确选；失效模型不暗换；规格变化、被移除参数需显式重置和审阅 |
| F21 Token 门禁 | 创建专用 Token 并继续；分组/权限/无模型/能力查询失败信息保留，secret 不暴露 |
| F22 精确报价 | 400ms 刷新；旧报价立即失效；指纹匹配才可提交；临期刷新和变价再次确认不变 |
| F23 全部提交路径 | 同步生成、同步 multipart 编辑、异步 asset IDs；异步失败不得隐式降级同步 |
| F24 幂等回执 | 先存回执；pending/found 查旧；有效 not_found 才原 key 重发；过期不盲发，unknown 保留 |
| F25 多图/详情 | 全部 ready 图按序、逐图尺寸与下载、完成数/请求数、站内实扣、参数、时间、请求 ID、受控错误 |
| F26 权限/隔离 | 继续签名媒体；只在可见页轮询、焦点刷新；换账号清结果；删除整批且提交中禁删 |

分辨率控件允许用户短暂输入空宽/空高和不完整字符串，立即失效报价并显示错误，不强行回退预设。提交仍只使用规范的 `parameters.<真实 key>`；不能额外维护一套与提交脱节的“展示尺寸”。比例为宽高的派生值，不新增 16:9 或 4K 到像素的猜测解析。

动态参数分组规则固定：模式/模型/提示词/参考图/分辨率/数量常驻；真实 quality 参数存在时进入主输出区；其余参数 ≤2 个默认展开，>2 个使用“更多模型参数”，标题显示数量与修改/错误状态。有错自动展开并定位首个错误；折叠不 unregister 字段，不丢值、不绕过验证。未知模型无 size/quality 配置时不补虚构控件。

## 7. 视觉还原规格

### 7.1 布局与控件

断点依据扣除应用侧栏后的**工作区可用宽度 W**，不是 window.innerWidth。容器为工作区外层，网格放在内部；调整宽度不改变 Composer 身份。沿用真实 AppHeader、AppSidebar、ImageStudioNav，不再叠加预览的示意顶栏和 62px rail。

| 项目 | 定案值 |
| --- | --- |
| 外边距 | W>850：25px 22px 24px；W≤850：20px 17px；W≤480：20px 11px |
| 双栏 | W≥1180：420px + minmax(0,1fr)，gap 22px；850<W<1180：minmax(355px,42%) + minmax(0,1fr) |
| 窄屏 | W≤850：完整创作在上、结果在下；提供查看结果/回到设置，不隐藏核心字段 |
| 编辑卡片 | border 1px、radius 16px、padding 20px、section gap 19px；W≤480：radius 13px、padding 16px 13px、gap 18px |
| 模式切换 | 外 padding 4px、gap 4px、radius 11px；按钮 h36/r8，W≤480 h40 |
| 模型/预设 | h42、radius 9px、padding 9px 10px；模型图标内距 12px |
| 提示词 | min-height 135px、max-height 360px、padding 13px、radius 11px、line-height 1.8 |
| 宽高 | h40、radius 8px；两侧 minmax(0,1fr)，中间交换视觉宽 28px；W≤480 为 24px |
| 输出行 | 两列等分、gap 18px；W≤480 gap 11px；长文案允许自然换行 |
| 数量 | 容器 h42、radius 9px、padding/gap 3px；选中项 radius 6px |
| 主按钮 | h46、radius 9px、全宽；次按钮 min-height 40px、radius 9px |
| 主舞台 | radius 15px、padding 22px、min-height 320px、aspect 1.19；W≥1180 aspect 1.3、max-height 580px |
| 窄屏舞台 | W≤850 aspect 1.5/min-height 260px；W≤480 min-height 230px/padding 13px |
| 图片 | contain，真实宽高；radius 7px；shadow 0 20px 42px，浅 #1f2b4730 / 深 #00000055 |
| 历史缩略图 | 卡片宽 76px，图高 45px；同批选图宽 58px，图高 33px；可换行不裁切 |
| 边界 | 网格子项、select、舞台、图片按钮均 min-width:0；舞台列 minmax(0,1fr) |

单个工作区承担纵向滚动；主 Composer 不继续套原内部 overflow-y-auto。报价与提交区在自然流中可见，长表单需要吸底时使用有实际占位的 sticky，并计入 safe-area；不让固定浮层覆盖最后一项错误或参考图。手机提交时引导到任务状态区，后台完成不打断正在输入的用户。

### 7.2 色彩、排版与表面

使用创作页作用域的语义 CSS 变量及真实 `.dark` 主题来源。不要依赖全局 preset 的 border、radius、input height 自动碰巧一致。Portal 中的相关 Dialog 内容同步挂局部 token 作用域。

| 语义 | 浅色 | 深色 |
| --- | --- | --- |
| 背景 | #f6f7fa | #101217 |
| 卡片 | #ffffff | #1a1e26 |
| 次级表面 | #f2f4f8 | #242a34 |
| 正文 | #222833 | #edf0f7 |
| 次要文字 | #657182 | #a7b1c2 |
| 线条 | #e2e7f0 | #303744 |
| 强调 | #4c63da | #b0beff |
| 强调底色 | #edf0ff | #2a3351 |
| 强调色上文字 | #ffffff | #18203a |
| 成功 | #38775f | #8ad4b2 |
| 错误 / 错误底 | #b33445 / #fff2f4 | #ff9eac / #38232b |
| 舞台 | #e9edf5 | #13171f |
| 卡片阴影色 | #1b2a4610 | #00000030 |

卡片阴影 `0 4px 18px`；focus ring 使用 3px 强调底色+强调边线；不删除 focus-visible。字号基线：页面标题 21px/500（≤850 为20、≤480 为17）、模块标题14px/500、字段12px、桌面输入13px、金额16px、正文14px/1.5。尺寸/金额采用 tabular-nums；图标延用 Lucide 16px，装饰图标 aria-hidden。

字体遵守应用用户字体偏好。视觉验收夹具固定同一字体，预览系统字体与应用 Public Sans 的差异要明确记录，不能在截图失败后临时换字体规避。触屏输入至少16px；细小说明文字如不能满足可读性可调整到12px，并记入预先列出的适配项。

### 7.3 动效规格

| 动效 | 时长 / 参数 | 触发与限制 |
| --- | --- | --- |
| 控件状态 | 160ms；background/color/shadow | hover、选中、focus；不使用 transition:all |
| 按压 | translateY(1px) | 仅可用按钮 active |
| 尺寸识别反馈 | 700ms；光晕 5px→0 | 新解析尺寸真正写入时一次；普通输入/重新报价不重播 |
| 参数展开、引用加入 | 200ms；opacity 0→1、Y 5px→0 | 明确展开/新资源；参数错误自动展开也可用 |
| 引用排序 | 200ms keyed layout | 既有稳定 file ID；保留键盘上移/下移 |
| 任务状态层 | 240ms 入场 | 任务/真实阶段变化；同任务轮询不重建 key |
| 成品揭示 | 500ms，cubic-bezier(.2,.8,.2,1)，opacity .2→1、Y8→0、scale .985→1、blur3→0 | 图片 load/decode 成功后；每个 asset ID 成功揭示一次 |
| 等待呼吸 | 1700ms ease-in-out；scale 1→1.055→1、opacity .8→1→.8 | 只有真实等待时循环，页面不可见时暂停；SVG 动画放外层 wrapper |
| 减少动态效果 | 关闭位移、缩放、模糊、循环 | 同时覆盖 CSS prefers-reduced-motion 与 motion useReducedMotion；状态直接切换 |

简单状态用 CSS，进入/排序使用已有 motion；不引入动画依赖。只在真实状态转变时播动效，图片签名更新、窗口回焦、报价刷新和提示词输入不得让结果重新入场。大图 blur 仅限上述一次揭示，不能常驻动画和永久 will-change。

## 8. 落地顺序与每步出口

| 阶段 | 交付 | 进入下一步的条件 |
| --- | --- | --- |
| A 冻结与接缝 | 保存基线截图/参数；先完成完整 loadRequest、编辑修订、原子 reference replace、共享查询 | 新旧 Sheet 都保持原语义；同一任务可重复载入；失败不丢稿；已有功能回归无倒退 |
| B 完整工作台外观 | 单实例双栏/堆叠、全部参数布局、自定义宽高、局部 tokens | 使用固定数据对照桌面/手机/浅深主题；核心尺寸正确，F01–F18 入口均在 |
| C 真实同页结果 | 所有任务状态、多图、后台查询、媒体、站内消耗、再生成 | 成功不跳库；旧任务不清新稿；unknown 无新请求；多 pending 和切换账号正确 |
| D 成品续编 | 同源新端点优先、旧后端鉴权签名回退、File 校验及完整编辑载入 | 本人成品成功续编；跨用户、超限、读取失败、快速切换均保留旧输入；同步/异步路径都可用；回退需真实GET CORS验证 |
| E 动效与精修 | 全动效、焦点/触屏、六语言、图片解码和竞态处理 | 录屏与对照图完成，reduced-motion 可用；不留下“后续再补”的关键视觉项 |
| F 联合验收 | 定向测试结果、视觉差分、真实接口验证记录、遗留项列表 | 第 9 节无 P0/P1；再讨论代码提交/发布，不能用计划文档代替已验收结果 |

每阶段提交给复核角色的材料包括：变更文件、功能对应编号、同尺寸截图/录屏、实际验证命令与结果。阶段 B 的几何问题当场修正，不累计到接口完成后再凭感觉补样式。用户后续已授权本轮代码实施和定向验证；该授权不包含发布。

## 9. 验收与验证命令

### 9.1 视觉及交互验收

1. 冻结原 HTML 和 hash，保存独立基线截图，禁止为让测试通过重写已确认原稿。生产适配副本只允许下列明确项目：外壳复用、viewport→container 断点、用户字体、触控/可读性适配、真实动态文案与数据；逐项记录，不能把整块布局划为差异豁免。
2. 同 Chromium 版本、OS、字体、DPR=1、100% 缩放、主题和固定数据；字体加载和图片解码后截图。对齐相同工作区内容宽度与区域，分别提供基线图、实现图、50% 叠加图、差分图。应用 header/sidebar 不计入工作区像素分母，另验与应用融合效果。
3. 核心控件高度、间距、圆角和栏宽偏差 ≤2 CSS px，主要区域边界 ≤4px；稳定同内容工作区差异像素目标 ≤1%。仅允许预先固定的抗锯齿容差，不遮掉控件、图片或错误状态；低于比例也不能放过明显局部偏差。跨字体/动态数据场景改用锚点与可用性，不冒称精确像素通过。
4. 视口覆盖320、375、768、1024、1280、1440；额外测工作区 W=479/480/481、849/850/851、1179/1180/1181，及应用侧栏展开/折叠。主流程均测浅/深色。320px 无水平溢出、按钮裁切；软键盘弹出时字段、错误和提交可达。
5. 固定情景：自定义/仅预设/无尺寸模型、全部参数类型、长提示词、8张参考图、4张成品、尺寸冲突、折叠参数错误、partial、failed、unknown、archive_failed、媒体读取失败、普通草稿和原作反复切换。
6. 录屏单验：尺寸反馈、展开、排序、提交等待、解码后揭示、快速切图；同任务轮询与媒体续签无重复入场；reduced-motion 无位移/模糊/循环。
7. 键盘可完成主流程；Dialog/大图 Esc 关闭、焦点约束和焦点恢复；参考排序不只靠拖动；触摸命中区域至少44px且不互相重叠。必要时小屏操作换行，不为凑原稿窄按钮牺牲可用性。
8. 六语言检查真实长文案，尤其 fr/ru/vi；不缩字硬塞核心按钮。中文输入法组合输入不被尺寸解析打断。字段有 aria-invalid/aria-describedby，状态使用 polite，不逐帧朗读；正文对比度≥4.5:1。

### 9.2 行为测试落点

| 测试 | 关键不变量 |
| --- | --- |
| 新增 `image-create-page.test.tsx` | 所有提交状态留页；任务 A 完成不清草稿 B；多个 pending 正确更新；查看历史不改稿；断点单实例；换账号清理 |
| 新增 `components/image-creation-results.test.tsx` | 全部 ready 图按序、逐图下载/尺寸；partial 完成数；unknown 无新建；相同任务刷新不重播；快速切图竞态 |
| 新增 `image-output-reference.test.ts` | 同源获取、File 校验、无隐式提交；失败/超限/401/403/取消/账号模型变化保留旧输入；不直接提交 output ID |
| 扩展 `components/image-composer.test.tsx` | 重复 revision 载入、全上下文复核、报价失效、提交修订保护、隐藏错误展开；既有尺寸/幂等/同步异步回归 |
| 引用 hook 测试 | replace 原子性、目标 scope 切换不误清、过期结果丢弃、文件顺序及最后一张删除仍编辑 |
| 新增后端读取契约测试 | 仅本人 ready 已结算 output；拒绝 catalog/reference/他人/未完成/已删除；伪造长度、超限、短读、中途失败、取消/超时、MIME/哈希不符；关闭 reader/删除临时文件、无重定向 |
| 路由及 console contract | 新端点必须鉴权/访问门禁；旧版本回退重新执行媒体签名授权；新能力不作前端全局依赖，GET CORS单独实测 |

不新增只测 className 或组件内部变量的测试。视觉由截图验收；真实API接通后使用可控测试环境验证生成/编辑报价→提交→查询→续创，记录实际结果。浏览器 mock 通过不代表真实服务已通过，生产不是本轮测试场。

### 9.3 定向验证范围（实际执行记录见第 10 节）

从 `web/default/` 执行一次受影响文件集；下面前三个是计划新增测试。若拆出独立原子 replace 测试，将其加入同次执行：

```bash
bun run test src/features/image-studio/image-create-page.test.tsx src/features/image-studio/components/image-creation-results.test.tsx src/features/image-studio/image-output-reference.test.ts src/features/image-studio/components/image-composer.test.tsx src/features/image-studio/components/image-generation-card.test.tsx src/features/image-studio/components/image-submission-recovery.test.tsx src/features/image-studio/image-library-page.test.tsx src/features/image-studio/hooks/use-image-edit-references.test.ts src/features/image-studio/image-prompt-size.test.ts src/features/image-studio/image-parameters.test.ts src/stores/image-studio-draft-store.test.ts
```

```bash
bun run oxlint -c .oxlintrc.json src/features/image-studio/image-create-page.tsx src/features/image-studio/components/image-creation-results.tsx src/features/image-studio/components/image-resolution-field.tsx src/features/image-studio/image-output-reference.ts src/features/image-studio/components/image-composer.tsx src/features/image-studio/components/image-parameter-fields.tsx src/features/image-studio/components/image-reference-field.tsx src/features/image-studio/components/image-regeneration-sheet.tsx src/features/image-studio/queries.ts src/features/image-studio/api.ts src/features/image-studio/hooks/use-image-references.ts
```

Lint 列表按实际改动补入测试文件，未改路径删去。定向类型检查使用临时 tsconfig 继承 `tsconfig.app.json`，显式列改动 TS/TSX 与所需全局声明，`include: []`，保持原严格选项和 alias，不关闭错误来缩范围；传递导入同样接受检查：

```bash
bun run tsgo --noEmit --project node_modules/.tmp/tsconfig-image-workbench.json
```

当前 `bun run typecheck` 为全前端 `tsgo -b`，`format:check`/`i18n:check` 同样扫描整个前端，不得伪称定向命令。定向格式检查只处理实际文件的正文并保留版权头；定向 i18n 检查覆盖本次 literal/dynamic keys 的六语言和插值。locale 写入必须经 `add-missing-keys.mjs` 后 `bun run i18n:sync`，不手改 JSON。

根目录计划执行以下后端测试（新测试统一采用 `TestImageOutputEditSource...` 前缀，包含服务、控制器、路由）：

```bash
go test ./service ./controller ./router -run '^(TestImageOutputEditSource|TestSignAuthorizedImageAssetUsesDownloadableFilename|TestGetImageStudioGenerationInputContract|TestImageStudioPhaseOneRoutesAreRegistered|TestImageAsyncRoutesRequireAuthentication)' -count=1
go test ./common -run '^TestConsoleContract$' -count=1
go test -tags kkai_bridge ./common -run '^TestConsoleContract$' -count=1
git diff --check -- web/default/src/features/image-studio web/default/src/styles web/default/src/i18n web/default/console-contract.json service/image_assets.go controller/image_studio_library.go router/kkai_routes.go common/console_contract.json
```

执行前按实际变更确认测试名和 build tag；若预计时长未知或超过5分钟，按成本规则先说明精确命令、原因和时长并取得授权。全量测试/构建不作为此功能例行检查的替代；定向检查通过且没有新增证据时停止扩测。局部类型检查若不能覆盖真实路由/声明约束，明确报告缺口，再按规则申请全量 `bun run typecheck`。未来发布仍需遵守发布 runbook 的强制门槛。

### 9.4 阻断交付条件

- **P0**：unknown 重复收费请求；报价与实际模型/尺寸/张数不一致；新稿或旧参考图被迟到响应清空；跨账号作品泄露；绕过授权/引用所属验证。
- **P1**：任一 F01–F26 回退；仍跳库；遗漏多图；续编失败不可恢复；窄屏裁切；主要操作不能键盘完成；reduced-motion 无效；明显偏离设计且没有明确记录的必要适配；截图/动效未核验。

完工报告必须附实际截图和验证结果，不能承诺“100%”后省略对照。目标是在固定环境下尽可能逐像素还原；真实外壳、动态数据、字体偏好和可访问性适配必须透明记录。

## 10. 实施与核验记录（2026-10-22）

### 10.1 已实现

- 单个 Composer 持续挂载，按工作区容器宽度切换双栏与堆叠；手机可直接查看结果、返回设置。
- 生成/编辑后留在创作页，展示本次全部结果、历史、实际扣费和任务详情；支持下载、再生成、成品续编。
- 保留预设、自定义像素、提示词尺寸识别、手动优先、动态参数、原参考图、有序多图、真实报价、全部同步/异步传输及幂等回执。
- 载入先复核完整设置和引用，再原子替换；完整普通草稿可恢复。只改参数也会保护草稿；账户/模型规格变化及迟到响应不能覆盖当前输入。应用样例不会复活已删除引用或切回旧引用。
- 成品续编通过新受鉴权 `/api/image-studio/assets/:id/edit-source` 端点读取，验证归属、结算状态、MIME、尺寸、字节数和哈希。流和临时文件有关闭/清理；SDK 超时原消息脱敏。首次实现时两个后端 profile 和前端兼容契约均声明 `image_output_edit_source`；发布准备中前端依赖已按第5.2节修正为可选优先路径，验证记录见第10.5节。
- 控件160ms、引用排序200ms、图片解码后500ms揭示、尺寸识别700ms；等待采用2.4s呼吸与2.8s环绕，隐藏页面暂停循环。
- 55条新键覆盖 en/zh/fr/ja/ru/vi 和仓库实际存在的 zh-TW；未改写已有译文。

### 10.2 实际定向命令与结果

以下前端命令工作目录为 `web/default`。仅重跑修改影响或之前失败的最近范围。

```sh
bun run test src/features/image-studio/image-create-page.test.tsx src/features/image-studio/image-output-reference.test.ts src/features/image-studio/components/image-generation-card.test.tsx src/features/image-studio/components/image-submission-recovery.test.tsx src/features/image-studio/image-library-page.test.tsx src/features/image-studio/image-prompt-size.test.ts src/features/image-studio/image-parameters.test.ts src/stores/image-studio-draft-store.test.ts src/features/image-studio/hooks/use-image-edit-references.test.ts src/features/image-studio/api.test.ts
```

首次186通过、1失败：新Blob读取测试的beforeEach错误返回mock函数，被Vitest当成清理函数执行。修复测试夹具后，最近范围通过：

```sh
bun run test src/features/image-studio/image-output-reference.test.ts
```

结果6/6。创作页最终修改后最近范围通过：

```sh
bun run test src/features/image-studio/image-create-page.test.tsx
```

结果9/9，覆盖6种提交状态留页、晚到任务与新稿、原输入失败恢复、账户切换。

```sh
bun run test src/features/image-studio/components/image-creation-results.test.tsx
```

最终11/11，覆盖多图、失败/未知状态、原图操作、切图与媒体解码竞态。

```sh
bun run test src/features/image-studio/components/image-composer.test.tsx src/features/image-studio/components/image-resolution-field.test.tsx src/features/image-studio/hooks/use-image-references.test.ts src/features/image-studio/hooks/use-image-edit-references.test.ts src/features/image-studio/components/image-output-quantity-field.test.tsx
```

该次54/54。之后样例引用与高级参数/识别反馈回归修正，仅执行对应范围：

```sh
bun run test src/features/image-studio/components/image-composer.test.tsx -t sample
bun run test src/features/image-studio/components/image-composer.test.tsx -t 'advanced parameters|prompt dimensions|sample'
```

分别4/4与13/13通过；未重复运行未变的引用与分辨率测试。

```sh
bun run tsgo --noEmit --project node_modules/.tmp/tsconfig-image-workbench.json
```

最终通过。临时配置继承原 `tsconfig.app.json`，显式包含18个变更TS/TSX入口、env/test setup及其传递依赖；没有关闭原严格选项。发现并修正3个不完整ImageSample测试fixture，未用unknown断言绕过。

```sh
bun run oxlint -c .oxlintrc.json src/features/image-studio/api.ts src/features/image-studio/components/image-composer.test.tsx src/features/image-studio/components/image-composer.tsx src/features/image-studio/components/image-creation-results.test.tsx src/features/image-studio/components/image-creation-results.tsx src/features/image-studio/components/image-parameter-fields.tsx src/features/image-studio/components/image-reference-field.tsx src/features/image-studio/components/image-regeneration-sheet.tsx src/features/image-studio/components/image-resolution-field.test.tsx src/features/image-studio/components/image-resolution-field.tsx src/features/image-studio/hooks/use-image-references.test.ts src/features/image-studio/hooks/use-image-references.ts src/features/image-studio/image-create-page.test.tsx src/features/image-studio/image-create-page.tsx src/features/image-studio/image-output-reference.test.ts src/features/image-studio/image-output-reference.ts src/features/image-studio/queries.ts src/i18n/static-keys.ts
```

首次仅创作页及其测试报告括号、button type与非空断言问题；修正后以下最近范围均通过，其余文件首次已通过：

```sh
bun run oxlint -c .oxlintrc.json src/features/image-studio/image-create-page.tsx src/features/image-studio/image-create-page.test.tsx
bun run oxlint -c .oxlintrc.json src/features/image-studio/image-create-page.test.tsx
bun run oxlint -c .oxlintrc.json src/features/image-studio/components/image-composer.tsx
node node_modules/.tmp/check-image-workbench-format.mjs
bun run i18n:sync
node node_modules/.tmp/check-image-workbench-i18n.mjs
```

最终lint通过；19个TS/TSX/CSS文件格式通过，版权头逐字保留；7个实际locale的新键、实际调用和插值一致性通过。`.tmp`中的配置与检查脚本是本机验证辅助文件，不加入产品构建。

以下Go命令工作目录为仓库根：

```sh
go test ./service ./controller ./router -run '^(TestImageOutputEditSource|TestSignAuthorizedImageAssetUsesDownloadableFilename|TestGetImageStudioGenerationInputContract|TestImageStudioPhaseOneRoutesAreRegistered|TestImageAsyncRoutesRequireAuthentication)' -count=1
```

首次service/router通过，controller短读错误状态断言失败；统一短读错误脱敏后最近范围通过。后续授权和超时脱敏修改亦仅重跑对应范围：

```sh
go test ./service ./controller -run '^TestImageOutputEditSource' -count=1
go test ./service -run '^TestImageOutputEditSourceAuthorization$' -count=1
go test ./service ./controller -run '^(TestImageOutputEditSourceNormalizesObjectErrors|TestImageOutputEditSourceHTTPContract)$/^(read_timeout|storage_timeout)$' -count=1
go test ./common -run '^TestConsoleContract$' -count=1
go test -tags kkai_bridge ./common -run '^TestConsoleContract$' -count=1
git diff --check
```

以上最终范围全部通过；新增Go文件完成gofmt。

**没有运行全量测试、全仓lint/typecheck或生产构建。** 没有新增依赖、锁文件、数据库迁移、分支或工作树。

### 10.3 浏览器实测与还原边界

在隔离Chrome里用实际React组件、项目CSS、翻译与React Query运行；API由本地固定夹具响应，未向真实上游发起计费请求。入口使用：

```sh
bun run rsbuild dev --config node_modules/.tmp/image-workbench-rsbuild.mts
```

- 成功留页、4张结果逐张选中、原图Blob续编、覆盖确认、完整普通草稿恢复通过。
- 手动宽度改为2048后润色含1536尺寸的提示词，仍保持2048；原有手动优先语义通过。
- 320px触屏加载8图，排序后的实际multipart请求包含8个文件，顺序为2、1、3…8，与界面一致。每个引用操作按钮实测44×44px。
- 工作区320/375/479/480/481/768/849/850/851/1024/1179/1180/1181/1280/1440均无水平溢出。1180及以上左栏420px，gap22px；850及以下同一表单堆叠。
- en/zh/fr/ja/ru/vi在320px下无横向溢出和按钮文字裁切；zh-TW完成静态键/插值检查。
- 浅深主题截图核验；减少动态效果时实测主图animation为none、transition为0s；无浏览器pageerror。
- 桌面实测模式36px、选择器42px、自定义宽高40px、左栏420px、间距22px。确认稿5行提示词的实际高度144.953px已匹配；保留135–360px限制和手动调整。
- 预先允许的适配：真实导航外壳、用户字体（应用Public Sans、确认稿系统字体）、真实模型参数与完整限制说明、实际价格/状态/任务ID、触屏44px。配置不存在的高级参数不虚构。

基线与实现工作区同宽1360px，已保存未遮罩的50%叠加及差分图。字体、实际文案和模型字段不同，因此**不宣称达到“差异像素≤1%”或100%逐像素验收**。核心尺寸已量测，真实字体与数据下的剩余差异透明保留。完整应用侧栏/认证外壳、真实对象存储与模型服务端到端联调未执行；本地页面夹具通过不代表这些检查通过。

截图、对照与视频位于本机已确认预览目录，文件链接见下方。未改写确认稿HTML。

- [桌面浅色实装](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-implemented-desktop-light.png)
- [桌面深色实装](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-implemented-desktop-dark.png)
- [手机设置](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-mobile-settings-dark.png)
- [手机结果](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-mobile-result-dark.png)
- [320px结果](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-mobile-320-result.png)
- [确认稿工作区](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-baseline-workarea.png)
- [实装工作区](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-implemented-workarea.png)
- [50%叠加](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-workarea-overlay.png)
- [差分图](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-workarea-difference.png)
- [交互录屏](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-workbench-implemented.webm)

### 10.4 报价与生成等待细化（2026-10-22）

- 工作台报价计算时显示固定宽度占位，取代可见“询价中”文案；1.4s单次扫光后保持占位，金额返回160ms淡入。400ms防抖、旧价立即失效、提交禁用和真实报价指纹规则保持有效。
- 已有作品时，生成等待改为底部状态卡，保留上一张图片；首次生成仍在结果区居中等待。静止图标搭配2.4s呼吸光晕和2.8s细轨道环绕，状态卡240ms入场；新成品完成解码后500ms揭示。
- 参考图校验、上传及真实上传百分比继续显示；模型请求处理只显示通用等待状态，不虚构绘制阶段、完成率或剩余时间。
- 本轮新增两条英文文案通过临时补键脚本及 `bun run i18n:sync` 覆盖全部7个实际locale；原有 `Estimated cost` 译文不改写。

本轮前端最近范围检查（工作目录 `web/default`）：

```sh
bun run test src/features/image-studio/components/image-composer.test.tsx -t 'workbench estimate'
bun run test src/features/image-studio/components/image-creation-results.test.tsx
bun run tsgo --noEmit --project node_modules/.tmp/tsconfig-image-workbench.json
bun run oxlint -c .oxlintrc.json src/features/image-studio/components/image-composer.tsx src/features/image-studio/components/image-composer.test.tsx src/features/image-studio/components/image-creation-results.tsx
node node_modules/.tmp/check-image-workbench-format.mjs
```

结果依次为1通过/49跳过、11/11通过、类型检查通过、lint通过、19个文件格式通过。新报价测试首次单数译文断言写错，修正后仅重跑该测试。结果区测试及其scoped lint亦通过。未改Go，不重复运行前次已通过的后端检查；未运行全量或生产构建。

隔离浏览器中使用真实组件及本地API夹具核验：报价计算时旧文案不可见、金额返回、首次生成成功、有旧图时底部等待、320px状态卡无溢出、等待动画暂停/恢复，以及新asset揭示；未出现pageerror。演示没有调用真实上游或计费服务，不代表真实模型端到端验收。

- [报价计算状态](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-price-calculating.png)
- [桌面生成等待](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-generating-with-previous.png)
- [320px生成等待](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-generating-mobile.png)
- [生成等待与成品出现录屏](/Users/tokk/.codex/visualizations/2026/10/20/01a158d7-7b0e-70c2-a5a6-6b153f66b990/creation-generation-flow.mp4)

### 10.5 回滚后端兼容修正（2026-10-22）

发布准备发现旧回滚后端没有新端点。为保留安全回滚和完整续编功能，前端增加第5.2节所述的已鉴权原图签名回退，移除新能力的全局强依赖；后端实现及其能力声明不变。此修正不改写此前夹具或后端验收结果。

最近范围检查（工作目录 `web/default`）：

```sh
bun run test src/features/image-studio/image-output-reference.test.ts
bun run tsgo --noEmit --project node_modules/.tmp/tsconfig-image-workbench.json
bun run oxlint -c .oxlintrc.json src/features/image-studio/image-output-reference.ts src/features/image-studio/image-output-reference.test.ts
```

结果为19/19、类型检查通过、lint通过。测试覆盖旧路由404/405回退、固定资产路径、存储请求不携带应用凭据、业务/权限/服务错误不回退、取消、共享超时、流式字节上限、尺寸不符及不安全URL拒绝。两文件正文经定向格式化，原版权头逐字保留；未运行全量测试，未新增依赖或文案。真实存储 GET CORS 由发布流程单独验收，此处不宣称已通过。

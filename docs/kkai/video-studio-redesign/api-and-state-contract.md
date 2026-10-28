# 视频工作室开发契约：API、状态与提交恢复

版本：1.0，2026-10-28。依据：[产品需求](prd.md)、[视觉规范](ui-spec.md)及本次工作区代码。本文是开发约束，不代表相关增强已经实现或通过业务验收。基线为前端改造，不修改计费、数据库、上游任务流程或服务端接口；后端代码仅用于核实边界。

## 1. 数据域与现有接口

Studio 的历史实体为 `KKAIVideoGeneration`，通过内部数字 `Task.ID` 关联通用任务。用户可见 `generation.task_id` 是任务字符串标识，`generation.id` 是 Studio 记录数字 ID。提交回执中的字符串 `id` 也不是 generation ID。任何情况下都不能 `Number(receipt.id)` 后调用 generation 详情或删除。

“视频作品” `/usage-logs/task` 使用 `TaskLog`，含 Studio 和公共 API 等来源，具有自己的分页、筛选、计费与媒体投影。它可提供组件与交互参考，但不得成为 Studio 历史数据源；不得把两个域的状态、记录 ID、query key、权限模式或媒体 hook 直接替换。代码依据：[Studio types](../../../web/default/src/features/video-studio/types.ts)、[视频作品 API](../../../web/default/src/features/usage-logs/api.ts)、[视频作品查询](../../../web/default/src/features/usage-logs/video-works/use-video-works.ts)。

| 现有接口 | 输入/输出核心 | 本轮约束 |
| --- | --- | --- |
| `GET /api/video-studio/token` | capability：`status`、`required_group`、`token{id,name,group}`、`effective_models`、`can_create` | 沿用 token gate；展示 ID/名称，不读取或保存密钥 |
| `POST /api/video-studio/token` | 返回 capability 与 `created` | 仅沿用现有令牌准备流程；不能作为未知生成的补发步骤 |
| `GET /api/video-studio/models?token_id=` | 当前令牌可用 profile/specification | 实际 profile 决定模式、参数和素材角色 |
| `GET /api/video-studio/samples`、`GET /samples/:id` | `token_id`，列表另有 `cursor/limit/model/category` | 保持分类 capability 门控、分页和指定样片恢复 |
| `POST /pg/videos/quote` | `VideoQuoteRequest` → `quota/display_amount/request_hash/expires_at/other_ratios` | 报价不生成任务；响应需新增最小运行时校验 |
| `POST /pg/videos` | 相同输入＋`max_quota/quote_hash/quote_expires_at`；header `Idempotency-Key` | 唯一付费提交入口；发出前先可靠保存未决记录，详见第 4 节 |
| `GET /api/video-studio/generations` | `cursor/limit/status` → `{items,next_cursor?}` | 默认 20，最大 50；按 generation ID 降序；仅空 status 或 `ready` |
| `GET /api/video-studio/generations/:id` | 正整数 generation ID | 当前用户拥有、未删除的记录；没有按 task_id 查询 Studio generation 的接口 |
| `DELETE /api/video-studio/generations/:id` | generation ID → `{deleted:true}` | UI 仅终态可删；删除不取消任务、不承诺退款 |
| `POST /api/video-studio/uploads` | 上传用途/文件元信息 → reservation、single/multipart、限制、短期上传请求 | 保留现有 Uppy 签名直传与续传实现 |
| `GET /uploads/:id`、`GET /uploads/:id/parts`、`POST /uploads/:id/parts/:part_number`、`POST /uploads/:id/complete`、`DELETE /uploads/:id` | 素材状态、分片列表、签名、完成/中止 | 路径前缀均为 `/api/video-studio`；网络传输完成不等于素材 ready |
| `GET /api/video-studio/assets/:id`、`DELETE /assets/:id` | 素材详情/删除参考素材 | 继续使用现有所有权、引用与状态检查 |
| `POST /api/studio/media-urls` | `{paths:[…]}` → `{urls,expires_at}` | 每批 1–100 个合法 Studio 资源路径；用户会话鉴权与资产授权后返回短期 URL |
| `GET /api/video-studio/assets/:id/content`、`GET /assets/:id/download` | content 支持 `variant=poster/preview` | 媒体认证、Studio 访问判断与资产授权继续由服务端负责 |

接口注册依据：[Studio routes](../../../router/kkai_routes.go)、[relay routes](../../../router/relay-router.go)。具体客户端适配仍集中于 [api.ts](../../../web/default/src/features/video-studio/api.ts)。不能为视觉筛选增加未支持的 status、全文搜索、总数或日期查询参数；当前 TypeScript 的 `VideoGenerationFilters.status` 比服务端宽，实施时收窄为 `'' | 'ready'` 或等效受限类型。

列表分页后客户端按日期分组，只能称“已加载记录”。失败不能变为空列表。详情 404 无法区分他人记录、已删除、无此 ID，向用户展示“无法访问或记录已移除”，不能泄露存在性或伪称已确认删除。

**删除边界：**[DeleteVideoGeneration](../../../service/video_generations.go)当前软删 generation 并安排输出资产删除，未检查 task 是否终态。仅 `ready/failed` 显示删除是现有 UI 和本期产品规则，不能写成服务端保证。服务端终态删除加固需单独变更范围与验证，本期不默默加入；自有删除成功后立即清理选择、播放器、详情与列表缓存，失败保持原项。

## 2. 输入、报价与响应校验

### 2.1 请求输入

`VideoQuoteRequest` 包含 `token_id/model/mode/prompt/parameters/reference_assets/sample_id?`，生成请求另加报价上限和签名。复用 [video-domain.ts](../../../web/default/src/features/video-studio/video-domain.ts) 的 `buildVideoQuoteRequest`、`buildCreateVideoRequest` 及 profile 参数校验，不创建第二套请求构造器。

- 服务端去除 model/mode/prompt 首尾空白；prompt 不得为空，长度为 **8000 UTF-8 字节**。前端计数以实际提交规范化内容的 `TextEncoder` 字节数为准，不以 JS 字符串 length 替代。
- 参数先按当前 mode 过滤、合并 profile 默认值并校验，再形成同一份报价/提交快照；保留 `false`、`0`，拒绝非有限数字。UI 字段 key 和 specification 的 `request_key` 不可混为一谈，上游 payload 映射仍由服务端执行。
- 当前素材角色有 `reference/reference_video/first_frame/last_frame`；服务端要求角色数匹配、每个角色唯一、素材 ID 有效、素材 ready、MIME 匹配、属于当前用户或已发布目录。
- [normalizeVideoReferences](../../../service/video_studio_submission.go)按 specification 的期望角色顺序重建素材，而不是照入参数组顺序发送。首尾帧的 slot→role→asset ID 关系必须保留。当前接口不接受同一 role 多张素材；未来若支持，必须重新定义含 position 的规范，不能继续使用无序集合去重。
- 样片套用后将实际有效值/素材显式放入请求，不依赖空字段触发服务端样片回填来建立本地“等价”判断。generation 没有完整参考素材与角色快照，不能从历史记录实现完整再次生成。

代码依据：[submission normalization](../../../service/video_studio_submission.go)、[parameter validation](../../../service/video_parameter_validation.go)。

### 2.2 三种身份不可混用

| 名称 | 来源/作用 | 生命周期 |
| --- | --- | --- |
| 本地请求指纹 | 规范化有效内容的摘要，识别等价未决请求 | 内容不变即稳定；与报价刷新无关 |
| 操作身份 / `Idempotency-Key` | 一次用户明确提交新建的随机身份，现有 `nanoid` 可继续使用 | 本次操作固定；未知结果不得换 key 补发 |
| `quote.request_hash` | 服务端对用户、规范化请求 hash、过期时间计算的 HMAC 签名 | **5 分钟**，传为 `quote_hash`；不是稳定请求指纹 |

服务端实际还有内部规范化请求 hash 和服务端幂等请求 fingerprint，两者规范不同，前端不应猜测或伪造它们。本地摘要只需可靠判定本地有效输入等价，不需要复现 Go JSON 的字节 hash。代码依据：[video_submission_integrity.go](../../../service/video_submission_integrity.go)。

当前幂等记录按 `user_id + operation + key` 预留，视频默认自首次服务端预留起 **24 小时**；同 key 不同 fingerprint 冲突，未绑定记录返回 in-progress，已绑定记录可返回 task 回执。过期记录允许删除后重新预留。图片专属保留逻辑不适用于视频，且本地 `createdAt` 不能证明服务端有效期。因此即便拿着原 key，过了窗口也不能安全地自动重发。[idempotency.go](../../../service/idempotency.go)是事实依据。

### 2.3 新增最小运行时校验

现状：`getVideoTokenCapability/createVideoToken` 使用 Zod parse，而 quote/receipt 主要依赖泛型强转；TypeScript 类型不能证明网络响应有效。本期只补付费边界的校验，不重写全站 API 层。

| 响应 | 接受条件 | 无效时 |
| --- | --- | --- |
| Quote | 对象；`quota` 为非负安全整数；`request_hash` 为预期 64 位十六进制字符串；`expires_at` 为有效正整数且提交瞬间未过期；`display_amount` 若有必须为字符串 | 不启用生成，显示报价不可用并允许重新获取；不新建付费操作 |
| Receipt | 本次 `/pg/videos` 成功响应；`task_id` 或 `id` 至少一个为合法任务字符串；trim 后长度不超过 128 且匹配 `^task_[0-9A-Za-z_-]+$`；若同时存在必须一致；错误/unknown 信息优先于成功判定；存在的 `status` 必须是受支持的字符串状态 | 2xx 缺 ID、格式不符、冲突 ID、解析失败、显式 `unknown` 或未识别响应形状，均保留 pending，不能 toast“已创建”后解锁 |

回执允许的最小字段与实际正常 handler/模型适配回包做 fixture 对照，不能要求从未承诺必有的 model/progress/created_at。任务 ID 格式由 [GenerateTaskID](../../../model/task.go) 产生，回执与 [library route](../../../web/default/src/routes/_authenticated/video-studio/library.tsx) 共用校验规则，避免“已受理但路由丢掉目标”。已存在状态值 `queued/in_progress/completed/failed/unknown` 来自 [OpenAI video DTO](../../../relaykit/dto/openai_video.go)，与 Studio `processing/archiving/ready` 不同。校验失败只影响本次结果判定，不触发自动重试。报价金额显示优先服务端 `display_amount` 或现有系统额度格式；不照搬原型中的固定人民币符号。`max_quota` 是报价确认上限，不是任意放大以避免 stale 的开关。

## 3. 任务与素材状态

| Studio status | 当前服务端投影 | UI 行为 |
| --- | --- | --- |
| queued | task 为 NOT_START/SUBMITTED/QUEUED | 阶段文字，允许编辑下次输入，不播放/下载 |
| processing | task 为 IN_PROGRESS，或未结束 UNKNOWN | 不把此状态当明确提交成功证据；无可信进度则不用百分比 |
| archiving | task SUCCESS，但输出还未 ready | 上游已成功，结果准备中；不提前播放/下载 |
| ready | task SUCCESS 且输出资产 ready | 允许经授权的媒体播放/下载 |
| failed | task FAILURE、已结束 UNKNOWN，或 SUCCESS 后输出 failed/deleted | 显示公开原因或中性失败描述；不推导退款或可重试归档 |

表依据：[videoGenerationStatus](../../../service/video_generations.go)。媒体 URL 字段存在也不能越过 `status === 'ready'` 的产品门控。failure_code 与公开 failure_reason 可用于文案，原始上游响应、凭据、内部栈不可展示。仅有 failed 无法可靠区分生成失败、最终 unknown 或归档失败；若后端未提供明确分类，使用“暂时无法提供结果，请查看任务状态”。不得从 failure_code/错误文案猜测结算。

素材是独立状态机：`pending_upload → uploaded/processing → ready`，另有 failed/deleting/deleted。`complete` HTTP 成功可能仍需检查；复用现有 uploader/asset polling，不把进度条 100% 当作可提交素材。现有续传记录及草稿 hydration 按用户和上传会话恢复，原型上传演示不能替换协议实现。

## 4. 本期新增：可持久化的未决提交保护

### 4.1 最小记录和规范化

新增独立于草稿、当前选择和媒体的用户级未决存储；推荐沿用现有浏览器存储能力并提供显式读写结果。不能直接依赖会吞存储异常的持久化封装，把“内存已设置”当作持久化成功。

最小记录为：`version/userId/operationId/requestFingerprint/createdAt/updatedAt/state/taskId?/generationId?`。其中 state 至少区分 sending 与 unknown；刷新读取 sending 视为待核实。已确认受理但尚未定位的目标可保存为定位信息，不能混成仍未知的提交。只存摘要，**不存 token secret、签名 URL、原始请求提示词或二进制**，因此恢复记录不能重建原 POST。

本地指纹按如下规则生成并版本化：

1. 从同一份已校验有效提交快照取值，包含用户作用域、token ID、有效 group、model、mode、prompt、parameters、reference roles/IDs、sample ID。group 来自当前 token capability 的 `token.group`；现服务端强制专用 `Seedance 视频` group。若 capability 不完整或身份不一致则阻止提交，不能以空 group 代替。当前不需要新 group API。
2. model/mode/prompt 使用与服务端 `strings.TrimSpace` 对齐的 Unicode 空白边界；不折叠内部空白、不做大小写或 Unicode NFC 改写。不要假定 JS `.trim()` 在所有 Unicode 字符上与 Go 完全一致。
3. 参数 key 使用确定性排序，保留类型；`false/0` 不与缺省字段等同。规范化后的默认参数必须与实际请求一致；不使用 localeCompare 产生依赖浏览器语言的序列。
4. 先根据当前 specification 将槽位映射为独一无二的 role/asset ID，再固定排序生成摘要。合法请求的数组重排不会改变语义，交换首尾帧会改变 role→ID 配对，必须改变指纹。当前 server 幂等 fingerprint 按 role、ID 排序；不要仅排序一串 ID 而丢失角色。
5. sample 缺省统一表示为空；不把 quote hash、quote expiry、max_quota、展示名称或 profile 视觉元信息纳入等价判断。摘要可使用原生 Web Crypto SHA-256，无需引入加密库；计算/存储失败均在发送前阻止生成。

持久化是浏览器范围的尽力保障，无法承诺浏览器被用户清空数据后、跨设备或跨标签页原子互斥。不要因此静默弱化同一客户端内的防重规则。可监听 storage 更新提高跨标签可见性，但本期不增加分布式锁或重放队列。

### 4.2 严格顺序

1. 点击生成后同步占用本次提交锁，捕获 user ID、当前会话世代、token/group、不可变请求和报价快照，再校验素材、参数、最新有效报价。指纹计算期间也应禁重复触发。会话世代必须能识别“同一用户退出后重新登录”，不能只比 user ID。
2. 读取该用户全部未决摘要并检查等价请求；命中则停留查状态，不换新 key。编辑后改回同样会命中。不同内容仅在新一次明确点击后创建另一个 operation，不能覆盖旧未决项。
3. 指纹计算等异步操作结束后重新核对 owner、会话世代、token/group、冻结请求与当前有效输入、报价匹配及有效期；只有全部仍一致才将 sending 记录持久化。**所有 await 和持久化完成后，紧邻 POST 调用前再做同样检查**，通过后只发出一次 POST，不在最终检查与调用间继续 await。身份/输入/报价已改变则不发送；若已预写该操作记录，按本次从未调用 POST 的明确证据移除或标记 not-sent，写失败保守保留并告知。写失败、读取失败或无法解析用户未决记录时 fail closed，解释恢复记录不可用，保留原数据；不静默重置存储后发送。
4. 接收响应时首先核对操作快照、用户 ID 和当前会话世代。换账号后的迟到响应不能写入新用户 UI/cache；如需处理原账号记录，只能按原 owner 和 operation 定位，不能用“当前用户”覆盖归属。
5. 按第 4.3 节证据保存受理/拒绝结果或 unknown。移除/标记完成也必须成功持久化；若写失败，保留保守保护与恢复告知，不能在内存解锁后让刷新重新遗忘证据。
6. pending 提醒持续存在，独立于成果选择、草稿保存、样片套用、模型变化、页面导航和新操作。未决项不可因容量上限、时间清理或草稿清空而淘汰；存储容量不足则阻止新增操作。

### 4.3 证据判定表

`accepted` 在本表指有足够证据结束“提交结果未知”的保护，不表示视频已完成，更不表示结算完成。`rejected` 表示本次新请求明确未被本接口受理，可结束该次 pending；仍不能宣称已退款。

| 获得的证据 | 判定 | 可否结束 pending / 后续动作 |
| --- | --- | --- |
| POST 未调用，前端校验/指纹/持久化失败 | not sent | 不存在已发送操作；不调用网络；保留输入 |
| 本次首次 POST 的正常 2xx 回执，通过最小校验且不带 unknown/error | accepted | 保存 task ID 和定位目标；结束未知保护；继续列表发现；不自动再提交 |
| POST 对应明确 `quote_stale` 409 | rejected | 当前新操作结束；刷新报价，下一次必须用户再次点击 |
| POST 对应明确 `video_quote_required/idempotency_key_required/invalid_video_studio_request` 400 | rejected | 这些当前代码路径均在 RelayTask 前拒绝；显示输入/报价问题 |
| POST 对应明确 `video_token_required` 400，或 `video_token_invalid/video_token_group_invalid/video_token_model_forbidden/video_token_ip_forbidden/video_token_group_unavailable/video_studio_access_denied` 403 | rejected | 仅结束该次有明确响应的新操作；重新检查访问/令牌，不能补发旧 unknown |
| `task_submission_unknown`，包括带 task ID | unknown | 保存 task ID；只读查状态，不重放 |
| `video_studio_conflict` 409 | unknown | 同码覆盖幂等冲突、in-progress 等；不能按英文 message 猜“安全重试” |
| 无响应、断网、超时、请求中止/页面关闭、2xx 畸形/缺 ID/解析失败、无法识别错误、未审计的 4xx/5xx | unknown | 保留保护；HTTP 状态本身不能充当未发送证明 |
| 刷新恢复到 sending，或原报价/本地 24h 已过期 | unknown | 保留记录，显示只读核实入口；时间流逝不解锁 |
| 同 task ID 的 generation 被列表发现，但 queued/processing | unknown 仍在 | 可定位展示真实状态；不能因“有记录”解除原 unknown |
| 同 task ID 的 generation 为 failed | unknown 仍在 | 可能是结束的 UNKNOWN 或归档失败；不据此清除未知提交保护 |
| 同 task ID 的 generation 为 archiving 或 ready | accepted/completed evidence | 当前投影证明 task SUCCESS；可持久化结束未知保护，继续归档/播放展示 |
| 只有相同模型/提示词/时间的历史作品，没有原 task ID | 无关联证据 | 不做模糊匹配、不绑定、不清除 pending |
| 30 秒内未发现、分页中没有、详情 404、被删除、查状态权限失败 | 无否定证据 | 不证明没有任务或未收费，保留待核实 |

明确拒绝白名单必须按 **本次 `/pg/videos` 响应 + HTTP status + 应用 code** 三者实现，并用对应 fixture 验证；不把 message 文本作为协议。不在白名单的错误默认 unknown，即使某些后端内部路径实际上未发送。不要用后来获取 token/quote 的成功或失败反向结束旧 pending。

**为何有 generation 也不能直接解锁：**[SubmitVideoStudioTask](../../../controller/video_studio_relay.go)在 provisional persist hook 内绑定幂等 resource 并创建 generation，而 [submitPreparedTask](../../../relay/relay_task.go)随后才发送上游。UNKNOWN 又被 Studio 投影为 processing/failed，现有列表未暴露可完整区分受理证据的 submission state。这是实际信息边界，不是 UI 文案可弥补的问题。

若只有摘要且没有 task ID，当前前端无法精确恢复关联；unknown 可能长期保留。本期不提供“忽略并重试原请求”来解除保护，不承诺最终可自动消除。后续要做到完整恢复，应独立设计有用户归属校验的只读操作/任务查询契约，届时另审 API、隐私与幂等生命周期。

## 5. 定位、轮询和选择

现有 [useVideoGenerations](../../../web/default/src/features/video-studio/queries.ts)已包含分页历史、head 查询和已观察活跃记录的详情补查。继续复用它，不在创作台新增 setInterval 或套用视频作品的 10/15 秒轮询。

- 回执仅有 task ID 时沿用 [video-library-page.tsx](../../../web/default/src/features/video-studio/video-library-page.tsx)的 **30 秒发现窗口**和手动刷新；只在本次已加载列表/head 内匹配，不扫描全历史。超时是“暂未定位”，不是生成失败。
- 已知且经当前用户验证的 numeric generation ID 可以直接查详情。把 task ID 和 generation ID 作为不同字段保存；未校验的本地 ID 先重新授权查询，不直接呈现旧内容。
- 当前选择优先级为明确目标→仍有效的用户选择→首次成功列表的最新项。后台完成/新 head 不替用户切换；目标未找到时不拿其他作品冒充。
- 原有活跃详情观察保留：移出最新页不能停止观察。页面隐藏暂停活跃任务轮询；网络恢复/显式刷新复用查询层，失败保留可用旧快照并标记状态暂未更新。
- 作品库新增记录提示由 head 对比产生；仅用户主动刷新后把新记录置顶，避免浏览位置跳动。未知提示独立于所有这些选择和布局状态。

## 6. 身份、媒体与公开错误

`UserAuth + VideoStudioAccess` 是服务器边界；前端 gate 只改善反馈。token 还受所有权、状态、专用 group、模型、IP 限制。所有 query key 继续包含用户 ID，models/samples 同时含 token ID；不能从视频作品的 admin/user 模式复制一个通用 identity 后替换 Studio 规则。[queries.ts](../../../web/default/src/features/video-studio/queries.ts)和 [video_studio_token.go](../../../service/video_studio_token.go)提供现成依据。

换账号/退出立即停旧播放器、清旧选择与私有缓存、取消或忽略旧请求回执；旧用户未决记录可按 owner 隔离保存，重新以该用户登录后读取。UI 不展示旧用户缩略图、任务号或错误内容。页面重新鉴权失败时不继续展示私有媒体。

媒体继续复用 [useStudioMediaUrls](../../../web/default/src/lib/studio-media.ts)：现有实现按用户和路径去重签名、每批 100、到期前约 30 秒刷新、部分失败降级查询。签名端重新读取用户状态，校验相对路径及资产授权，返回 no-store 响应，见 [studio_media_urls.go](../../../controller/studio_media_urls.go)。签名 URL 不写 draft/pending/永久缓存；对象 URL 的生命周期与播放器实例关联，卸载后释放。

**本期共享层修正，尚未实现：**当前 `studio-media-urls` query 根未接入 `private-user` 清理，queryFn 未传播取消 signal，且 batch catch/单项 allSettled 会丢失授权拒绝原因。实施改用既有 `privateUserQueryKey(userId, 'studio-media-urls', ...paths)`；将 signal 传至批量和 fallback 请求，abort 直接退出且不展开 fallback，也不能在 allSettled 后作为成功结果返回。保留以下确定的前端结果契约，后端接口不变：

- 任一 401 整体传播为身份错误，对消费者不再暴露该用户可用签名，撤销该用户当前活动媒体；不能因 Query 还保有旧 data 而继续展示。
- batch 的 403/404 按现有单路径 fallback 细分；单路径 403/404 记入与 `urls/expires_at` 同返的 `deniedPaths: Readonly<Record<string, 403 | 404>>`。当前播放/封面路径命中则立即撤销对应 src；其他成功路径仍可用。
- 普通网络错误不记 deniedPaths、不伪装撤权，按局部加载失败处理；保留健康视频 src 的优化只能在没有身份/路径拒绝证据时生效。

仅迁 query key 或取消请求不能单独证明授权撤销已经正确处理。此共享变更必须定向回归图片工作室直接消费者，并以真实 hook/QueryClient 测试证明切账号、取消、混合批次撤权及迟到响应处理；仅 mock 签名 hook 的组件测试不够。

视频作品的 [useVideoWorkArtifacts](../../../web/default/src/features/usage-logs/video-works/use-video-work-artifacts.ts)可参考“单次自动续签、并发合并、身份切换忽略迟到结果”机制，但其 `/api/task/:taskId/artifacts`、TaskLog/hostedVideo 数据协议不能直接用于 Studio。新增播放器只挂当前活动媒体；主结果、样片、库内详情统一互斥。换签不能无条件改正在播放的 src；失败后至多一次自动局部续签/恢复，再提供手动重试；保留播放位置和静音状态需按实际媒体能否 seek 决定。

原型静态封面不是媒体失败 fallback，不能伪装为用户生成结果。失败处理不重发生成、不默认文件已过期、不渲染未经公开错误投影的内部数据。[video_studio_response.go](../../../controller/video_studio_response.go)与 `Task.PublicFailReason` 负责公开错误契约；前端优先映射受支持 code，未知 code 使用中性提示。

## 7. 实施与审查门槛

本契约新增的是前端本地状态与边界校验：pending 存储/规范化/证据判定、quote/receipt parse、媒体互斥与定位展示。其余接口继续复用。以下任一需求会改变范围，应先回到产品与契约评审：按 task_id 精确定位、完整历史再生成、服务端全状态筛选/统计、取消任务、归档重试、服务端删除终态限制、跨设备未决恢复。

实施时的最小验证必须覆盖：等价请求改出改回、quote 刷新不换操作身份、参数 false/0、素材首尾帧交换、相同 role 拒绝、UTF-8 字节、持久化前失败、摘要计算期间切账号/同账号重登/token-group改变/报价过期均零 POST、POST 后立即刷新、网络无响应/畸形 2xx/任务号格式不符、冲突不解锁、24h 不解锁、迟到跨账号回执、processing/failed 不清 unknown、同 task 的 archiving/ready 才闭环、删除/404 不当作受理否定证据、混合签名批次撤权不能保留旧 src。具体测试命令与责任归入质量验收文档；本文编写阶段仅核对源码，未运行业务测试。

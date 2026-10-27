# 视频作品：后端与数据契约

状态：开发方案，未实施。依据 2026-10-27 本地源码及已确认的 `video-works-complete.html`；本文件不代表已部署能力。示例封面、金额、退款过程及过期状态均不能作为真实接口字段。

## 1. 本次实现决策

沿用任务列表、任务制品投影和内容代理，补充视频筛选与安全的展示字段；托管作品复用现有 Video Studio 资产、海报和签名能力，不创建新任务系统、结算系统、数据库表、截图服务或对象存储。查询与媒体签名只读，任何查询、预览、下载、重试均不得触发生成、归档/回填、补扣、退款或修改任务。

| 能力 | 已提交源码 | 本地未提交草稿 | 本次必须实现或明确降级 |
| --- | --- | --- | --- |
| 列表 | 自己/管理员列表，分页、单状态、精确任务 ID、时间、管理员渠道 | 无查询变更 | 视频范围、模型/ID 搜索、状态分组及同范围计数、查询错误透传 |
| 计费 | 当前 `quota`、冻结私有计费上下文 | `TaskDto.billing` 与 `TaskBillingForDisplay` 白名单投影 | 金额有效性标识、严格字段语义、历史数据未知态；修正 `completed` 文案 |
| 作品 | 通用制品代理；Studio 有 output 关联、真实 poster、短签名 | 前端强化入口；任务 DTO 尚无托管摘要 | 列表批量只读投影真实海报；现制品接口增加按需 `hosted_video`，不新建 endpoint |
| 下载 | 通用内容 URL；Studio 已支持 attachment 短签名 | 通用前端仅 `<a download>` | 托管作品复用 attachment=true；通用内容路由补 `download=1` |
| 进度 | `progress` 字符串混合上游进度和阶段占位 | 无来源字段 | 本期一律用阶段状态，不显示百分比 |
| 原预扣/累计退款 | 不在公开任务 DTO 中 | 未补齐 | 明确“暂无完整流水”，不倒推或读取全部日志拼接 |
| 封面流水线 | 通用 TaskArtifactStore disabled；独立 Studio inspect/poster outbox + FFmpeg 已存在 | 尚未桥接任务列表 | 正常托管成功作品必须有真实海报；历史/异常才降级；源码存在不证明生产启用 |

源码入口：[路由](../../../router/api-router.go)、[任务控制器](../../../controller/task.go)、[任务模型](../../../model/task.go)、[计费投影草稿](../../../service/task_billing.go)、[通用存储边界](../../../service/task_artifact_store.go)。

## 2. 接口、认证与权限

| 接口 | 认证与响应 | 本页用途 |
| --- | --- | --- |
| `GET /api/task/self` | `UserAuth`；`{success,data:{page,page_size,total,items}}` | 普通用户固定使用；管理员“仅我的”也使用此接口 |
| `GET /api/task/` | `AdminAuth`；相同分页结构 | 平台管理员“全部作品”；租户站点不能靠前端角色开关绕过鉴权 |
| `GET /api/task/:task_id/artifacts` | `UserAuth`，控制器再次按所有者/管理员判权；`{success,data}` | 打开作品或下载时取得制品；与列表分离 |
| `GET /v1/tasks/:key`、`/:key/artifacts` | relay API `TokenAuth`；无 dashboard 成功包装 | 保持原 API 客户端行为，本页不借用用户 API Key 调用 |
| `GET/HEAD /v1/tasks/:key/artifacts/:artifact_key/content` | relay token 或绑定该 task/key 的 `access` capability | 播放、Range、HEAD、下载；dashboard cookie 本身不能替代此认证 |

普通用户只查自己的记录。管理员全部列表补充用户名/渠道及 `admin_info`；`root_info` 只给 root。管理员切换“仅我的”时同样得到普通用户投影，不将全部列表的特权数据从缓存带入。

`admin_info` 可含请求 ID、请求路径、插件名称/版本/作者；`root_info` 可含上游任务 ID、节点、插件运行代次。Key、Token ID、订阅内部 ID、原始请求、插件状态、`private_data`、上游结果地址、内部计费审计均不得透出。现有自己的列表会省略查询 `channel_id`（DTO 中可能表现为 0），UI 隐藏渠道而不是显示“渠道 0”。

保留 [dashboard 租户鉴权](../../../middleware/auth.go)。本次不把平台 `AdminAuth` 解释为租户管理员权限，也不增加跨用户 `user_id` 参数给自己的接口。名单过滤必须在 DB 查询前构造，不能分页后在浏览器过滤。

列表继续 `Omit("data")`；投影显式清空 `data`、成功结果 URL 和旧版藏在 `fail_reason` 的 URL。制品投影保持独立调用，列表不执行插件 `ListArtifacts`、远程 HEAD 或视频下载。成功任务不等于一定可取回制品，`result_discarded=true` 必须显示“结果未保存”。

## 3. 视频列表查询扩展（待实现）

保留原 endpoint；新页面始终传 `media_type=video`，只在该模式启用以下契约，其他现有任务客户端保持原行为。

| 参数 | 定义 |
| --- | --- |
| `p`、`page_size` | 正整数；默认 1/20，最大 100；视频模式非法值返回 400，不依赖旧分页工具对负数的宽松处理 |
| `media_type` | 本页固定 `video`；按中央任务 action 分类，在数据库分页前过滤 |
| `q` | trim 后最多 191 字符；公开 task ID 或展示模型名称的字面子串搜索；不搜索提示词、原始 JSON 或私有上游 ID |
| `status_group` | `all/success/processing/failed`；`processing` 对应 `NOT_START/SUBMITTED/QUEUED/IN_PROGRESS`；`UNKNOWN` 只包含在 all |
| `start_timestamp/end_timestamp` | Unix 秒，提交时间闭区间；页面按用户时区计算今天/最近 7/30 天；提供时须合法且起点不晚于终点，保持原有历史查询范围，不新增 31 天限制 |
| `channel_id` | 仅管理员全部列表可用；自己列表传入该参数返回 400，前端隐藏控件 |

不同时使用非空旧 `status` 与新 `status_group`；冲突返回 400。既有 `platform/action/task_id` 若保留，与新条件取交集；不能使 `media_type=video` 失效。

视频分类覆盖 canonical `image_to_video/text_to_video/first_tail_to_video/reference_to_video/remix`，以及 [历史别名](../../../constant/task.go) `generate/textGenerate/firstTailGenerate/referenceGenerate/remixGenerate`。未知 action 不因“非 Suno”就被认作视频；新增视频插件需在中央分类补齐。核对旧 remix 预览分支：归一化后值为 `remix`，不得拿 `remixGenerate` 再比较。

复用一个有领域含义的 GORM 视频查询 scope 同时服务列表、总数和状态计数。日期/用户/渠道/action 等使用参数绑定；`q` 中 `%`、`_`、转义字符按字面搜索。模型取 `properties.origin_model_name`，空时回退 `upstream_model_name`，搜索与展示完全一致。模型 JSON 提取使用显式 SQLite/MySQL/PostgreSQL 分支并覆盖最低支持版本；SQLite 对旧无效 JSON 使用有效性保护，不能用整个 JSON 的 `LIKE` 代替字段搜索。

视频模式响应在 `data` 内增加 `status_counts:{all,success,processing,failed,unknown}`。计数使用身份、视频、搜索、时间、渠道等全部筛选，但不受当前 `status_group` 限制；`total` 则是当前页状态过滤后的总数。使用一条按 status 分组聚合查询，无逐行查数；轮询存在瞬时变化时允许相邻读取短暂不一致，不为展示开启长事务。

现有四个 `TaskGetAll*/TaskCountAll*` 会吞 DB 错误。改造相关调用返回 error；失败应返回 5xx 和稳定错误码，不能伪装为空列表或总数 0。本期无 Redis 结果缓存；后端查数据库，用户名可继续使用现有用户缓存。列表与制品 JSON 添加/保持 `Cache-Control: private, no-store`。

## 4. 行数据与时间

| 页面内容 | 真值及缺失规则 |
| --- | --- |
| 模型 | `properties.origin_model_name` → `upstream_model_name` → “未知模型”；不以平台/渠道名冒充模型 |
| 生成类型 | action 中央归一化；不识别的值显示“其他”，不能显示成文生视频 |
| 提交时间 | `submit_time > 0`，否则有效 `created_at`；均缺失显示 `—` |
| 开始/结束时间 | `start_time/finish_time`，0 代表未知；不能渲染 1970 年 |
| 生成耗时 | 仅两端有效且结束不早于开始时使用 `finish_time-start_time`；提交到结束应明确标为“总耗时” |
| 媒体规格 | 只有实际媒体元数据能提供尺寸/时长；`billing.seconds` 是计费时长，不自动等于成片时长 |
| 失败原因 | 服务端公开、脱敏后的 `fail_reason`；作为文本呈现，不执行 HTML、不暴露完整上游响应 |

## 5. 计费字段与金额语义

本地草稿在 [DTO](../../../dto/task.go) 新增 `billing?: object`，只从任务冻结快照投影，不执行结算。正式实现保留显式白名单，使用明确 Go DTO/可选指针和前端类型约束；不要把任意 `map[string]any` 当作无校验前端契约。**本次只在 `media_type=video` 列表模式要求每项必有 `billing` 与 `amount_available`；其他既有 Task DTO 出口保留可选兼容。** 金额有效性与价格明细分开判断：历史记录没有 BillingContext，但额度转换可信时仍返回 `billing:{amount_available:true}`（有则另带 pricing_epoch/state），照常显示历史金额，只将明细标未知；不能因缺少价格快照隐藏可信金额。

| 公开字段 | 冻结来源/解释 |
| --- | --- |
| 顶层 `quota` | `Task.Quota` 经 `HistoricalCreditQuota("tasks", id, quota)` 转换后的当前金额单位；不是原始预扣、退款额或累计消费 |
| `billing.amount_available`（新增必传布尔） | 有效 epoch 边界且金额投影成功才为 true；缺失配置、转换错误、非法金额为 false。false 时金额显示 `—`，不得把失败转换得到的 0 当免费 |
| `state` | `PrivateData.BillingState` 的七种已知值；空/新未知枚举视为未知 |
| `billing_source` | 已识别的 `wallet/subscription/membership`；不推断缺失来源 |
| `pricing_epoch` | 草稿已有 `legacy_075/usd_credit_v1`；旧 epoch 的当前金额可转换，但旧价格明细不直接拼 USD 公式 |
| `model_price/model_ratio/group_ratio` | `BillingContext` 中提交时冻结的价格/倍率；不是当前模型市场报价 |
| `user_group_ratio` | `AccountingContext.HasUserGroupRatio=true` 时的冻结值；显式 0 不得因 truthy 判断丢失 |
| `billing_unit` | 冻结 PerCallBilling + seconds 对应 `second`，否则 `request`；缺失不可默认按秒 |
| `seconds` | 已验证 OtherRatios 中计费秒数；是用量，必须和 duration 倍率区分 |
| `duration/size/resolution` | 已验证 OtherRatios 中的倍率；`size` 数字不是 `1920×1080` |
| `image_count` | 同一公开 DTO 兼容字段；不能拿它作为视频份数 |
| `billing_mode/expr_b64/matched_tier/usage_facts` | 冻结 TieredSnapshot；`matched_tier` 当前来自 EstimatedTier，不能标为“实际命中档位”而无最终记录 |
| `membership_tier_name/membership_tier_style` | 冻结 MembershipPricing 的展示元数据；样式按现有允许值映射 |
| `membership_discount/membership_wallet_ratio/membership_candidate_ratio/membership_savings` | 冻结会员方案；`membership_savings` 是比例，不是退还金额或已节省货币值 |
| `membership_wallet_fallback` | 冻结会员降级到钱包的事实；缺失不补 false 来推断计费来源 |

可选数字用“字段存在”判断。明确记录的有限 0 是真实值；缺失/null 是未知；非有限值或类型错误丢弃该明细并记录服务端诊断。不补 1 倍率、不补 0 元单价、不以当前会员等级补历史折扣。旧私有快照的非指针标量可能把缺失解码为 0，不能仅因草稿投影写出了 `model_price:0` 就声称免费；先按冻结计费模式核实该字段适用及存在证据，无法证实时省略此项。价格明细不完整时保留可信当前金额，公式区显示“该记录的计费明细不完整”。

`usage_facts` 还需嵌套白名单，禁止原样透出任意对象：本期仅接受冻结快照中表达式使用的计费事实和已确认的公共数字/枚举字段，移除请求头、原始参数、嵌套对象、URL/密钥等；无已审计 schema 的事实不展示。当前模型价格 API 只能辅助非权威标签，不得成为历史单价/用量/档位真值。动态表达式按 [计费表达式契约](../../../pkg/billingexpr/expr.md) 复用展示解析器，不 `eval`，不在前端重新结算。

平台客户表达式使用冻结 `DisplayExprString`（有则优先）；租户客户沿用其冻结销售表达式。任何管理员实际表达式、内部账本、渠道成本都不能通过公共 billing 附带输出。金额以服务端 quota 为准，UI 不要求装饰性公式反算结果覆盖实际金额。

### 状态不得与生成状态混为一谈

| 原始 state | 本期展示 | 金额标题 |
| --- | --- | --- |
| `pending` | 待计费 | 当前扣费 |
| `reserved` | 已预扣 | 当前预扣 |
| `dispatching/accepted` | 待结算 | 当前扣费 |
| `ambiguous` | 待核实 | 当前扣费 |
| `refunded` | 已退款 | 当前扣费（通常为 0，仍以服务端值为准） |
| `completed` | 计费已处理 | 当前扣费 |
| 缺失/未知 | 状态未知 | 当前扣费 |

`completed` 不可直接译为“已结算”：[恢复逻辑](../../../service/task_billing_recovery.go) 也会结束提交结果不明或尚未提交的计费恢复。当前草稿 `completed → Settled` 必须修正。要使用设计中的“实际扣费/已结算”，须另有已核实最终结算记录；本期不凭该状态推断。

“生成失败”不意味着已退款；“生成成功”不意味着已结算；`quota=0` 也不证明退款。设计中“预扣 0.80 → 实扣 0.64 → 退回 0.16”仅是示例：现有任务字段没有完整流水，冻结 EstimatedQuota 也不能冒充实际预扣收据。本期展示“当前扣费 + 状态 + 可证实冻结价格”，完整预扣/退款流水明确暂无。若后续必须补资金流水，须另行以账本/审计事实建立按所有者受控的明细接口，不扫描前端日志拼接，更不改写历史任务。

## 6. 进度与刷新

[阶段常量](../../../relay/channel/task/taskcommon/helpers.go) 把 submitted/queued/in_progress 设为 10%/20%/30%；[轮询器](../../../service/task_polling.go) 在上游有 Progress 时又会覆盖，数据库字段没有来源。数字本身不能区分真值和占位，包括 10、20、30 也可能恰好是上游真值。

本期仅展示“待提交/排队中/生成中/成功/失败/状态未知”和非定量加载动画，不显示百分比、进度宽度、预计剩余时间。后续数字进度必须先打通 adaptor→轮询→持久化→DTO 的 `progress_source`，仅 verified upstream 范围 0–100 可展示；不为本次 UI 增加该 schema。

前端复用现有查询框架：页面可见且存在进行中生成/托管媒体处理中记录时每 10 秒查询；仅剩已终结生成但计费待处理时每 15 秒查询；全部终态后停止。最多一个进行中请求，切页/筛选/卸载取消或忽略旧请求，失焦/离线暂停。连续失败使用 20/40/60 秒退避，鉴权失败停止。刷新只读本地任务记录，不额外触发上游轮询。现有 60 秒会员查询不是任务自动刷新。

## 7. 制品、封面、预览与下载

### 7.1 托管视频正常路径：复用 Studio，不新增签名 endpoint

[VideoGeneration](../../../service/video_generations.go) 已按 numeric `Task.ID` 批量关联 generation、output link 与 asset；generation 记录在旧任务尚未回填 `AssetHostedResult` 时仍具权威性。[实体关系](../../../model/video_studio.go) 为 `KKAIVideoGeneration.TaskID → Task.ID`、`KKAIVideoTaskAsset.TaskID/AssetID`、`role=output/position=0` 的主输出，不是字符串 task_id 或模型名关联。`CreateVideoGeneration` 会设 `AssetHostedResult=true`。

现有 [资产流水线](../../../service/video_asset_pipeline.go) 依次归档、inspect、poster；[FFmpeg](../../../service/video_media_processor.go) 在约 0.1 秒取真实画面，JPEG 最多 120 KiB，成功后写 `PosterObjectKey` 并将 asset 设 ready。[一次性归档工具](../../../service/video_task_archive_once.go) 是独立操作路径，不能由任务 GET、预览或重试调用。此次只消费已有资产；不开启 worker/backfill、不申请迁移、不证明生产已启用此流水线。

**列表最小投影：** 在视频模式当前页读取完成后，至多追加 generation、output links、assets 三次批量 DB 查询；复用现有 generation view 的关联逻辑，提取共享“任务输出资产”查询，避免每行查询和再次加载已取得 tasks。包含 deleted generation 以识别删除状态；所有关系先核验：generation.UserID = task.UserId、asset.OwnerUserID = task.UserId、scope=user、kind=output、角色/position匹配。主输出重复/错配不得随便选一个。Studio 记录或 AssetHostedResult 存在时禁止回落至原始上游链接。

任务 DTO 新增 `media`（可选）：`source:'studio'`、`state:'ready'|'processing'|'unavailable'|'restricted'` 为必传字段；`poster_url?:string`、`poster_expires_at?:number`、`width?:number`、`height?:number`、`duration_seconds?:number` 为可选字段，URL 与到期时间成对出现。顶层 `id` 是数字 Task.ID，`task_id` 是公开字符串 ID，二者不得混用。尺寸与时长仅来自已 inspect 的正值，不能由扣费倍率补全。未删除 ready asset 且 PosterObjectKey 非空时，直接调用现有 `VideoAssetStore.PresignDownload(..., attachment=false, TTL)` 生成海报短签名；这是本地签名，不做远程 Head、媒体下载、插件投影或截图。普通历史任务无对应资产时省略 media，沿通用制品路径处理。

状态优先级为 restricted（媒体访问受限）→ unavailable（关系错配、删除、失败、读取/签名异常）→ ready（合法未删除的 ready 输出）→ processing（非终态生成，或已知输出资产尚在处理）。成功任务没有 output 关联不证明归档在运行，标 unavailable；不能从“有 outbox 源码”猜测后台进度。ready 输出缺 poster 是可诊断的异常降级，视频仍可按需取回，但不满足正常封面验收。

权限在批量查询和签名前执行：使用当前请求已授权的任务集合；一次批量用户查询同时核实 viewer 的启用状态/最新角色和本页所有 task owner 状态，不逐行查用户。**owner 不存在或已禁用时，即使 viewer 是管理员也不签名**，沿用通用媒体所有者必须启用的边界；现有签名在 TTL 内可能仍有效。管理员跨用户能力只沿平台任务管理员授权，租户守卫仍生效。资产没有独立 tenant_id，必须依赖已获授权的 Task.TenantID/UserId 与上述所有者链，不能仅凭 asset ID 签名。权限不允许则返回 restricted、无 URL，任务历史仍可看。单资产错配/删除/签名失败只使该行 unavailable、无 URL，其他行照常；整批资产查询失败仅降级本页媒体并返回分页对象内 `data.media_warning:'media_temporarily_unavailable'`，保留可信任务/金额/分页并允许重试。主任务查询失败仍是 5xx。所有降级记服务端诊断，不静默声称无资产。

访问能力与处理开关分开：`video_studio_setting.CanAccess(viewer.Role)` 决定 Studio 访问；store 配置/签名是否可用决定本次读取能否交付 URL。`WorkerEnabled`、`ArchiveEnqueueEnabled`、`BackfillEnabled` 决定后台处理，关闭它们**不能禁读已有 ready 资产**，只读路径不得调用上传侧 `ValidateVideoAssetProcessingAvailable()`。公开页面显示 Enabled、源码默认值或已有 worker 类都不能代替这些具体判断。

**不能直接调用现有 Studio 签名接口绕过权限。** [GetAuthorizedVideoAsset](../../../service/video_assets.go) 即使 `isAdmin=true` 也要求 user-scope 资产属于当前 user；管理员特例只适用于 catalog。[POST /api/studio/media-urls](../../../controller/studio_media_urls.go) 及原 content/download 路由因此无法直接服务管理员查看他人托管任务。此次不放宽这些接口；在任务投影服务内以已核验的 task→generation→output→owner 链授予窄范围媒体读取，复用 store 的签名原语，绝不把请求中的 owner 当授权主体。

**按需预览：** 既有 `GET /api/task/:task_id/artifacts` 在同一权限与关联校验后，为托管任务增加独立 `hosted_video`。其 `task_record_id:number`、`state:'ready'|'processing'|'unavailable'|'restricted'` 必传；仅 state=ready 时必传 `content_url:string`、`download_url:string`、`expires_at:number`，可附 `width/height/duration_seconds`；其他 state 禁止带 URL。数字 task_record_id 必须等于选中行 id；若公开 task_id 在当前授权范围内不唯一，拒绝投影（404），不能任选历史行。content_url 用原对象短签名，download_url 用现有 `attachment=true`；filename、TTL、Content-Disposition 规则沿用 Studio。托管处理中/已删除/受限不进入插件/旧视频兜底。前端给 hosted_video 单独严格类型和 URL 解析，不为对象存储放宽通用 capability URL 路径校验。

两个 expires 字段均为 Unix 秒、仅指**签名 URL 的有效期**，不代表任务或资产过期。本地签名 TTL 使用现有 Video Studio 配置；列表不输出视频/下载短链。签名到期或对象存储拒绝已过期签名时，封面重新获取对应列表投影、预览重新获取制品投影即可续签；只续签一次，后续失败呈现不可用并保留手动重试。换身份立即清除签名缓存；签名本身仍可能在 TTL 内有效，沿用当前 Studio 安全边界，不承诺即时撤销。

正常托管成功作品必须在列表显示该任务的**真实海报**，同时能打开正确视频与下载。前端仅懒加载可见封面图；列表不拉视频抓首帧。海面/汽车演示图只用于设计 fixture。历史未归档、资产正在处理、封面缺失/损坏、权限受限允许同尺寸中性降级，并给出准确原因；不能把全部占位图当作正常验收通过。若目标环境没有 ready 资产/可用签名配置，这是封面闭环的联调门槛，不自动启用生产开关来“补齐”。

### 7.2 非托管视频：现有制品与内容代理

制品接口保留 `artifacts:[{key,type,mime_type?,content_url}]`；旧视频可能走 `legacy_content_url`。单任务可有多个制品，`type=video` 才放入播放器，其余按类型展示附件；不把任意首项当视频。制品 key、类型、重复项、数量上限 64 和 URL 校验沿用现有契约。

打开预览或主动下载时才请求制品；关闭时卸载播放器/移除 src 并释放对象 URL。选中视频用原生 `<video controls playsInline preload="metadata">`，点击播放再加载，不自动播放多个视频。没有真实 poster 的历史非托管任务使用中性入口；用户打开后由原生播放器显示首帧，不依赖跨域 canvas。

通用 `content_url` 由 [BuildTaskArtifactContentURL](../../../service/task_artifact_access.go) 生成，是绑定 task/key 的长期 capability，**没有 expires_at 或自动续签保证**，与 Studio 短签名必须分开处理。持有者可访问该制品，故不写入 analytics、日志、localStorage 或分享功能。保留匿名入口验签、task 唯一性、所有者启用状态、限流、脱敏与 `private,no-store`。

通用下载沿用内容 GET，在 URL 加唯一可选 `download=1`，服务器校验后在流式响应中覆盖安全 `Content-Disposition: attachment`；文件名由安全 task ID 与 MIME 白名单生成，不采纳请求 filename。保留 Range/206/HEAD 与取消传输，安全头不变；参数不扩大 capability 权限。前端解析器只额外允许此参数。该扩展目前不存在，必须跨域真实下载验证；不依赖 `<a download>` 或整文件 Blob。托管下载已经有附件签名，不能为它重复实现此代理扩展。

## 8. 不可用与重试契约

| 结果 | 解释与页面行为 |
| --- | --- |
| dashboard 401 | 登录失效/用户不可用；停止轮询，按统一登录流程处理 |
| dashboard 403 | 权限不足；禁止重试风暴，不显示其他用户记录 |
| 制品清单 404 | 记录不存在或当前身份无权访问；统一“作品不可用”，不泄露归属 |
| 通用 content capability 404 | 统一隐藏不存在、无权、被丢弃及代理失败等细节；不能直接标“链接已过期” |
| Studio 短签名到期 | 只说明 URL 到期；重新获取同身份投影续签一次，不能显示“作品已失效” |
| 409 `artifact_not_ready` | 未就绪（仅未被 capability 遮蔽的调用可见）；更新任务状态，保留生成中状态 |
| 410 `artifact_gone` | 确认内容不可取回（可能被 capability 遮蔽）；可显示“作品已失效”，不自动承诺具体过期时间 |
| 429 | 遵循 Retry-After；禁用立即重复请求，保留明确重试入口 |
| 5xx/网络/解码失败 | “暂时无法加载”或“不支持此视频格式”；可重试一次或下载，保留任务/计费信息 |

上游 401/403 在通用代理中映射为上游鉴权失败，不能弹成用户无权；capability 调用仍统一遮蔽为 404。Studio 对象存储短链的 403 不等于 dashboard 身份 403，先按签名有效期尝试一次重新投影；真正 dashboard 403 则停止。HTMLMediaElement `error` 本身无法可靠给出 HTTP 原因，前端不得由此推导过期。

“重新加载”仅取消当前媒体请求、重新获取一次授权制品投影并重新挂载所选播放器；清除该任务局部失败状态，不能提交新生成任务。通用长期 URL 可能不变且仍失败；Studio 路径重新签发短链，签名文本是否变化不作为成功断言。一次重试后保持不可用态，允许用户再次主动尝试，不循环刷新。设计稿中的“链接已过期”仅指有明确证据的媒体失效分支，不能用于可续签的 URL 到期；一般失败使用“作品暂时无法加载”。

前端 query key 至少包含用户/租户、权限 scope、task ID；列表另包含全部筛选和分页。制品缓存仅内存，沿用 30 秒 staleTime；身份/租户切换清空旧数据，已失效或主动重试时不复用缓存。不能共享跨用户制品缓存，也不能将签名 URL 放进持久化查询缓存。

## 9. 实现前后必须通过的契约检查

- 普通用户、管理员、root、租户账号四组权限；猜测他人 task/key、重复 task ID、禁用所有者均不得越权。
- 视频分类含新旧 action；混合音频/图片记录不进入视频总数；搜索、状态计数和分页在三种数据库上语义一致，DB 失败不是空态。
- 0/缺失/旧 epoch/转换错误；无BillingContext但金额转换可信仍显示金额；冻结价格与当前配置不一致；未知状态、失败未退款、completed 但提交结果不明、退款后 0；不得误称免费或结算。
- 白名单拒绝私有字段和嵌套 usage 泄露；真实进度与阶段占位统一不显示数字；列表查询不执行插件或拉取媒体。
- 托管 ready 成功视频显示真实首帧海报；数字 Task.ID 关联、owner错配/禁用、重复公开ID、已删除generation/asset、Studio访问关闭、签名失败逐项验证；worker关闭但访问允许时已有ready资产仍可读。普通用户及管理员访问正确视频，但原Studio资产接口的所有者边界不放宽。
- 当前页至多三次媒体批量DB查询和一次viewer/owners批量身份检查；列表签名无远程Head/视频读取，任何GET/签名/重试不写DB、不启动归档/回填。媒体失败与任务列表失败分开。
- Studio短签名正常到期后可重新投影恢复；通用capability重取可能同URL。按需预览、Range/HEAD、两条下载路径的attachment、跨域媒体、404/403/410、限流与重试；未验证运行配置不能声称生产媒体永久保留。

本次只完成开发文档，没有运行测试、访问生产或修改应用源码；以上是后续开发验收范围，不是已通过报告。

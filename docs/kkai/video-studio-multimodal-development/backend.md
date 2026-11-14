# 视频工作室多模态参考：后端开发文档

日期：2026-11-14。范围：本地实现已落地，工作树未提交、未部署；真实定向验证和未验证边界见 [实现与验收记录](implementation-qa.md)。

上位依据：[产品方案](../video-studio-multimodal-design-20261114.md)、[完整 Web 生成页设计稿](../video-studio-multimodal-web/index.html)。Web 稿用于交互评审，其中的型号、数量、价格、状态均不能直接当成服务合同。本文件第 3—7 节是前后端契约的唯一来源；前端与 QA 文档引用这些章节，不另建不同名称或不同单位的字段副本。

## 1. 开发边界与固定决策

- 只建设一套新创作流程。单图、单视频及混合素材均通过有序参考集合提交；不做旧 UI、旧标量请求、旧草稿或旧样片重放，不提供 v1/v2 协商或降级转换层。
- 统一表单没有顶层模式标签。无有效参考素材表示文字生成，有参考素材表示参考生成；模型允许的模式、视频必填、音频不能单独使用等规则必须由后端再次检查，不能自动更换型号、分组或计价 SKU。
- 首版必须贯通图片、视频、音频。首帧/首尾帧仅在精确型号已验收时开放明确高级方式，与多模态参考互斥；不得把一张图片自动解释为首帧。
- 当前工作室使用用户 session 访问，实际 Relay 仍由工作室 Token、分组、模型权限和渠道选取约束。原型没有 Token 设置面板，不表示可以绕过 `ValidateVideoStudioToken`。
- 已有作品、素材、任务记录、账目不能删除；已经受理的任务继续查询、归档、结算或退款。这是数据与扣费安全，不是旧客户端兼容项目。
- 不新增通用媒体中心、提示词 AST、引用芯片、自动转码、时间线或任意约束 DSL。素材内的音轨优先级没有已验证契约，不增加声音优先级字段。

## 2. 现有实现与改造地图

本表保留实施前定位的文件和改造目标，不是当前未完成清单。已落地实现的实际证据见 [实现与验收记录](implementation-qa.md)；仍注明待验收或拟新增的内容不据此宣称已完成。

| 责任域 | 现有文件 / 符号 | 必须改动及完成条件 |
| --- | --- | --- |
| 路由与鉴权 | `router/kkai_routes.go::registerVideoStudioAPIRoutes`；`router/relay-router.go::SetRelayRouter` | 保留现有 URL 与 session/Token 边界；增加只读回执查询。新建入口关闭时不阻断已受理任务的查询与后台处理 |
| 能力目录 | `controller/video_studio_catalog.go::ListVideoStudioModels`；`service/video_model_profiles.go::ListEffectiveVideoModelProfiles`、`normalizeVideoModelProfileInput`、`decodeVideoModelProfile` | 返回已验收的新能力；未知总数量、未验收适配器或无法匹配渠道时不得启用 profile；新 profile 保留实际 `model` 字符串 |
| 规格与参数 | `service/video_model_spec.go::VideoModelSpec`、`ValidateVideoModelSpec`、`validateVideoReferenceInputs`；`service/video_parameter_validation.go` | 增加明确的媒体约束，移除一角色只能一份素材和必须映射标量 request key 的假设；新模式只接受新合同 |
| 提交规范化 | `service/video_studio_submission.go::VideoStudioSubmissionRequest`、`NormalizeVideoStudioSubmission`、`normalizeVideoReferences`、`buildVideoTaskPayload`、`ApplyVideoStudioEffectiveGroup` | 规范化有序集合，验证媒体证据、所有权、组合、精确型号及能力版本；不再构造单图兼容别名 |
| 报价与幂等 | `service/video_submission_integrity.go::VideoStudioIdempotencyFingerprint`、`NewVideoStudioQuote`、`ValidateVideoStudioQuote`；`service/idempotency.go` | 两类 hash 纳入同一有序语义；绑定任务不因 URL 重签改变；保护未终结的视频幂等记录，新增只读 lookup |
| Controller 顺序 | `controller/video_studio_relay.go::PrepareVideoStudioTaskRequest`、`QuoteVideoStudioTask`、`SubmitVideoStudioTask`、`prepareVideoStudioIdempotency`、`respondVideoStudioIdempotentReplay` | 合同拒绝、幂等重放、能力检查、报价、provisional hook 的顺序按第 8 节执行；不在拒绝后预扣或派发 |
| 上传生命周期 | `controller/video_studio_assets.go`；`service/video_assets.go::CreateVideoAssetUpload`；`service/video_multipart_upload.go`；`service/video_multipart_completion.go`；`service/video_upload_lifecycle.go` | 增加 `reference_audio` 上传用途，保留分片恢复、重复 complete、安全 abort；上传完成与 ready 分开 |
| 真实媒体检查 | `service/video_media_processor.go::Inspect`、`parseVideoProbeOutput`、`videoMediaMIMEType`；`service/video_media_image.go`；`service/video_asset_pipeline.go::HandleInspect`、`videoReferenceMediaCategoryMatches` | 支持纯音频分支、准确时长/FPS、编码白名单和分类校验；音频成功直接 ready，不生成视频海报/预览 |
| 素材限额 | `setting/video_studio_setting/config.go::Setting`、`UploadLimits`、`Get`；`service/video_asset_pipeline.go::copyObjectToTemporaryFile` | 上传安全上限与模型可用上限分开；音频必须有明确的体积安全上限，不复用 1 GiB 视频归档上限 |
| 任务关联与视图 | `model/video_studio.go`；`model/task.go::TaskPrivateData`；`service/video_generations.go::CreateVideoGeneration`、`VideoGenerationView` | 复用 asset、generation、task link；task link 的 `Position` 改为对应 role 内位置；事务内保存规格版本，详情必须返回完整输入引用快照；创建前再次锁定并验证资产 |
| 样片 | `service/video_samples.go::VideoSampleInput`、`VideoSampleView`；`service/video_sample_references.go`；`validatePublishedSamplesAgainstSpec` | 新写入/新编辑只接受完整有序引用；用户目录不把旧样片推断成新格式。管理员可按新格式重新配置 |
| 清理与引用保护 | `service/video_reference_assets.go::CleanupAbandonedVideoReferenceAssets`、`DeleteVideoReferenceAsset`；`service/video_asset_pipeline.go` | 新的图片/音频 role 纳入活动任务引用保护；同时继续保护存量任务使用的已有 role，不能误清理其文件 |
| 应用插件 | `plugins/tasks/sora/plugin.js::specialSeedanceRequest`、`buildSubmitRequest`、`extractUsage`；`plugins/sora_responses_test.go` | 完整投影三类素材和顺序，拒绝未实现字段而非丢弃；保留明确 `generate_audio:false`；请求与计费实际型号一致 |
| 派发/结算 | `relay/relay_task.go::QuoteTaskSubmission`、`RelayTaskSubmit`、`TaskSubmitResult.CanRetry/CanRefund`；`controller/task_submission.go::executeTaskSubmission`；`service/task_billing.go`、`task_polling.go`、`task_billing_recovery.go` | 继续使用现有可靠任务/账务状态机，不在 Studio 重写扣费；新增测试覆盖多模态输入及未知受理，不扩大改造范围 |
| 外置适配器 | 同级 infra `roles/platform/defaults/main.yml`、`roles/platform/templates/openresty.conf.j2` 仅有 `seedance-special-adapter:8080` 路由引用 | 当前未定位实现源码。接入负责人必须提供维护方、精确版本、源码与测试入口；应用插件完成不等于整条链路完成 |

说明：`service/video_parameter_validation.go` 等表中的文件在编写文档时必须能在仓库定位；若实施期间发生重命名，以对应现有符号定位，不复制过时文件名创建第二套服务。

## 3. 单一能力合同

### 3.1 保留的身份字段

当前模型目录是 `GET /api/video-studio/models?token_id=<id>`，返回 `VideoModelProfileView`。其 `id` 是数据库 profile ID，`model` 是调用的精确客户型号字符串。

当前 `/pg/videos/quote`、`/pg/videos` 使用 `token_id` 和 **`model` 字符串**，没有 `model_id`。`model_profile_id` 出现在前端草稿、样片和作品记录，不替代生成请求中的 `model`。不因为设计稿的模型选择控件而重命名这些身份字段。

新请求包含 `specification_version`，必须等于所选目录条目的版本；它是能力版本，不是双协议协商。`specification.version` 与外层 `specification_version` 保持一致。报价时发现不一致返回 `video_capabilities_stale`；提交时已绑定幂等键的回执恢复不受能力版本后续变化影响。

### 3.2 完整可解析示例

下面的配置示例使用 `max_total_count:null`，因为供应商文档只分别给出图片、视频、音频的限制，没有声明三类素材合计上限。启用时后端以各媒体策略的 `max_count` 之和作为本站数据库查询和请求放大的安全上界，不把 `9+3+3` 冒充供应商合同总数；用户目录只返回已启用且通过新合同验证的 profile。

```json
{
  "id": 42,
  "model": "sd_2.5_special_720p",
  "display_name": "Seedance 2.5 · 720p",
  "description": "多模态参考配置示例，尚未验收",
  "provider_label": "Seedance Global",
  "specification_version": 2,
  "specification": {
    "version": 2,
    "modes": ["text_to_video", "reference_to_video"],
    "parameters": [
      {
        "key": "duration",
        "label": "Duration (seconds)",
        "request_key": "duration",
        "control": "number",
        "required": true,
        "min": 4,
        "max": 30,
        "step": 1
      },
      {
        "key": "ratio",
        "label": "Aspect ratio",
        "control": "select",
        "required": true,
        "options": [
          {"label": "16:9", "value": "16:9"},
          {"label": "9:16", "value": "9:16"}
        ]
      },
      {
        "key": "resolution",
        "label": "Resolution",
        "control": "select",
        "required": true,
        "options": [{"label": "720p", "value": "720p"}]
      },
      {
        "key": "generate_audio",
        "label": "Video sound",
        "control": "switch",
        "required": true
      }
    ],
    "reference_policy": {
      "max_total_count": null,
      "requires_visual": true,
      "requires_video": false,
      "audio_requires_generate_audio": true,
      "image": {
        "allowed_mime_types": ["image/jpeg", "image/png", "image/webp"],
        "max_count": 9,
        "max_size_bytes": 20971520,
        "min_width": 301,
        "max_width": 5999,
        "min_height": 301,
        "max_height": 5999,
        "min_aspect_ratio_exclusive": 0.4,
        "max_aspect_ratio_exclusive": 2.5
      },
      "video": {
        "allowed_mime_types": ["video/mp4", "video/quicktime"],
        "max_count": 3,
        "max_size_bytes": 49999999,
        "min_duration_seconds": 2,
        "max_duration_seconds": 15,
        "max_total_duration_seconds": 15,
        "min_frame_rate": 24,
        "max_frame_rate": 60
      },
      "audio": {
        "allowed_mime_types": ["audio/mpeg", "audio/wav"],
        "max_count": 3,
        "max_size_bytes": 14999999,
        "min_duration_seconds": 2,
        "max_duration_seconds": 15,
        "max_total_duration_seconds": 15
      }
    }
  },
  "default_parameters": {
    "duration": 5,
    "ratio": "16:9",
    "resolution": "720p",
    "generate_audio": true
  },
  "enabled": false,
  "sort_order": 0,
  "created_at": 0,
  "updated_at": 0
}
```

该示例仅展示字段形状：精确型号是否接受三类组合、全部画幅选项、实际数值与价格仍由验收记录决定，不通过示例扩大型号能力。`requires_video:true` 的型号不得声明无视频也可提交；客户端不能自动切到另一个 SKU。

### 3.3 字段、单位与验证规则

| 字段 | 要求 | 含义 |
| --- | --- | --- |
| `specification.modes` | 必填、非空、无重复 | 新普通流程仅 `text_to_video` / `reference_to_video`；已验收的高级方式可增加明确的 `first_frame_to_video` / `first_last_frame`，不默认开放 |
| `reference_policy` | 含参考模式时必填 | 取代旧参考输入的标量字段映射；管理员只选择已验证预设及收紧限制 |
| `max_total_count` | 可选；供应商未声明合计上限时保持 null | 为空时由各媒体策略 `max_count` 之和作为本站安全上界；不会对外宣称供应商合计上限 |
| `requires_visual` | 必填 bool | 参考生成须至少一张图片或一个视频，不能只有音频 |
| `requires_video` | 必填 bool | 当前精确型号必须至少有一个参考视频；无素材不能转为文字生成 |
| `audio_requires_generate_audio` | 支持音频时必填 bool | true 是本站首版保守规则；只有已验收音频+静音组合才可设 false，不声称供应商强制要求 |
| `image` / `video` / `audio` | 各对象可省略 | 省略表示不支持，不表示没有上限；存在时其类型相关约束必须完整 |
| `allowed_mime_types` | 存在该媒体时必填、非空 | 规范化且经真实内容验证的 MIME；不按文件扩展名决定 |
| `max_count` | 正整数 | 分类上限，启用值不能超过供应商与适配器已验收范围 |
| `max_size_bytes` | 正整数、包含边界 | **有效最大字节数**；合并本站安全上限与供应商严格边界，前端直接用 `size <= max_size_bytes` |
| 图片宽高 | 整数 px、包含边界 | 示例 301—5999 对应文档的开区间 (300,6000)，不能误把 300/6000 放行 |
| 图片比例 | 两个 exclusive 字段 | 严格开区间，使用真实 width/height；未知或零高拒绝 |
| 时长 | 有限正数，单位秒 | `min/max_duration_seconds` 包含边界；视频和音频分别计算分类总时长，不能混为输出时长 |
| 帧率 | 有限正数，单位 fps | 使用探测到的有理数帧率；区间包含边界。VFR 的支持与判定须随适配器验收，未验证时拒绝此编码组合 |

本方案的 MB 显示采用十进制 `1 MB = 1,000,000 bytes`；小于 50 MB 对应最大 49,999,999 bytes，小于 15 MB 对应最大 14,999,999 bytes。本站现有图片默认上限 `20 << 20` 是 20 MiB，所以示例有效上限为 20,971,520 bytes，而不是宣称能上传到 30 MB。最终发布配置再次核对供应商单位，使用三方约束中最严格的字节数。

持续使用 `duration_seconds`，不为了 UI 再造毫秒合同；展示可格式化，判断不先四舍五入。单项下限/上限检查原探测值；分类和用有限数值的十进制表示累加，避免浮点累积误差与显示舍入使 15.01 秒过关。当前已安装 `shopspring/decimal` 可用于这一有限求和；不将媒体时长强转为整数计费。

Go DTO 中可选标量使用指针和 `omitempty`，保留显式 0/false；必填的 `position` 也用可判定缺失的指针解码，防止缺字段被默认为有效 0。`parameters.generate_audio=false` 是有效显式输入，不得被默认值覆盖。

## 4. 生成、报价与引用合同（本地实现，未部署）

### 4.1 请求与模式

继续使用 `POST /pg/videos/quote` 与 `POST /pg/videos`。生成请求基于报价请求增加已有报价保护字段；`group` 不由客户端指定，后端从验证后的 Token 取得。新请求必须完整提供创作内容，`sample_id` 仅记录来源，不再从样片隐式补齐 model、prompt、mode 或素材。

以下是字段形状示例，使用的是经测试替身确认能力的型号；不是对该真实型号已可调用的承诺。

```json
{
  "token_id": 101,
  "model": "sd_2.5_special_720p",
  "specification_version": 2,
  "mode": "reference_to_video",
  "prompt": "参考产品外观、镜头运动和音乐氛围，生成一段产品短片。",
  "parameters": {
    "duration": 5,
    "ratio": "16:9",
    "resolution": "720p",
    "generate_audio": true
  },
  "reference_assets": [
    {"asset_id": 801, "role": "reference_image", "position": 0},
    {"asset_id": 802, "role": "reference_image", "position": 1},
    {"asset_id": 803, "role": "reference_video", "position": 0},
    {"asset_id": 804, "role": "reference_audio", "position": 0}
  ]
}
```

- 客户端明确提交：`token_id`、`model`、`specification_version`、`mode`、`prompt`、`parameters`、`reference_assets`；纯文字使用 `reference_assets:[]` 和 `mode:text_to_video`。当前后端会合并模型默认参数，未提供参考数组时按空集合处理；不能将客户端完整快照要求描述成服务端已逐字段检查 JSON 键存在。
- 可选：`sample_id`，只能指向用户可访问且按新格式配置的样片。提交全部实际输入，不允许后台把已排除素材补回。
- 普通参考角色只有 `reference_image`、`reference_video`、`reference_audio`；新请求不接受旧 `reference` role、单值 `image/reference_image/reference_video` 或旧 `image_to_video` mode。
- 首帧/首尾帧使用专用 `first_frame` / `last_frame`，各 position=0，必须匹配明确高级方式，不能与普通三个参考角色混用。实际型号未声明对应 mode 时拒绝。
- `position` 是**每角色从 0 开始的连续整数**。同角色缺号、重复、负数、小数或缺字段均拒绝；同一 asset 在普通参考集合重复添加拒绝。首尾帧是否允许相同图片单独按已验收规则定义。
- 后端使用固定 role 次序 `reference_image → reference_video → reference_audio`，组内按 position 规范化；帧控制顺序 `first_frame → last_frame`。改变 HTTP 数组物理顺序但 position 相同，语义不变；交换同角色 position，语义改变。
- 媒体类型、大小、时长、所有权和存储键都从服务端资产记录取得；客户端不提交签名 URL、ObjectKey、MIME 或“校验通过”标记。

### 4.2 报价响应与提交

保留 `VideoStudioQuote` 的现有字段；成功外层仍为当前 `common.ApiSuccess` 封装。

```json
{
  "success": true,
  "message": "",
  "data": {
    "quota": 1000,
    "display_amount": "$0.002",
    "request_hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "expires_at": 1800000000,
    "other_ratios": {}
  }
}
```

金额与时间为协议示例。`quota` 单位是本站整数额度，UI 展示 `display_amount`，不能拿 quota 当货币。`expires_at` 是 Unix 秒；quote 当前有效期为 5 分钟，继续沿用。这里名为 `request_hash` 的返回值是服务端签名，提交时原样放到 `quote_hash`。

提交使用相同实际创作快照，并增加 `max_quota`、`quote_hash`、`quote_expires_at`，设置 `Idempotency-Key` 请求头。`max_quota` 是本次接受的最大整数额度，不是素材数量。成功提交继续返回现有视频回执形状（`id`/`task_id` 均指公开任务 ID），不把 generation 的数字主键冒充 task ID。

任何实际型号、Token、有效分组、能力版本、prompt、参数、角色、asset_id、position 变化均使报价失效。移除本次未使用区中的文件等纯 UI 操作不改变请求，不必重报价。报价后费率升高导致当前费用高于 `max_quota`，继续返回现有 `quote_stale`，由用户确认新费用后再次提交。

### 4.3 两种 hash 的职责

1. 规范化请求 hash：继续在 `ApplyVideoStudioEffectiveGroup` 生成，绑定用户实际 Token、profile ID、specification version、精确 model/group/mode、完整 prompt、有效参数及按上述顺序的 role/position/asset_id。URL、上传进度、客户端条目 ID、显示文件名、报价期限不属于语义。
2. 幂等 fingerprint：在进入存储和渠道分发前验证请求形状并生成，同样保留 role/position 顺序及客户端明确的 specification version；排除 `max_quota/quote_hash/quote_expires_at`，这样相同创作只重报价不制造新的创作身份。业务变更必须用新提交身份。

两者必须复用同一“引用规范化”业务规则，不分别按 asset ID 排序。重新签发同一不可变资产的下载 URL 不改变任何语义 hash。新资产替换旧资产必须分配新 asset ID，不允许写同一对象键后继续复用旧报价。

## 5. 上传、媒体检查与新样片合同（本地实现，未部署）

### 5.1 上传接口沿用现有路径

| 接口 | 现状 / 改动 |
| --- | --- |
| `POST /api/video-studio/uploads` | 现有；purpose 增加 `reference_audio`，其余字段仍为 filename/mime_type/size_bytes/multipart |
| `GET /api/video-studio/uploads/:id` | 现有；读取该资产上传/检查状态，owner 校验不可省略 |
| `POST /api/video-studio/uploads/:id/parts/:part_number` | 现有；签分片，短期 URL 与准确 Content-Length 规则保留 |
| `GET /api/video-studio/uploads/:id/parts` | 现有；按存储事实恢复已完成分片，不仅相信客户端 ETag |
| `POST /api/video-studio/uploads/:id/complete` | 现有；重复 complete 幂等，返回 accepted 不等于 ready |
| `DELETE /api/video-studio/uploads/:id` | 现有；取消尚未完成上传，不用于从草稿移除 ready 资产 |
| `GET /api/video-studio/assets/:id`、`.../content`、`.../download` | 现有；继续鉴权后读取/签名，不公开 bucket |

图片上传 purpose 仍为 `reference`，视频为 `reference_video`，音频新增 `reference_audio`；**上传用途与生成角色是不同字段**，不要把图片 purpose 误写成生成 role。样片视频继续使用管理员专属 `sample`。

```json
{
  "purpose": "reference_audio",
  "filename": "music.mp3",
  "mime_type": "audio/mpeg",
  "size_bytes": 3456789,
  "multipart": true
}
```

保留现有 `VideoAssetUpload` 响应：`asset`、`upload_mode`、`part_size`、`expires_at`、`max_size_bytes`、`upload_limits`、可选 single PUT `request`。上传 reservation 的 `max_size_bytes` 是存储安全限额；模型能力的有效上限可以更低，前端按较低值预检，quote 再校验。上传时不要求已有图片/视频，因此只有音频也能上传。

### 5.2 音频与视频实际检查

- 扩展 FFprobe 输入容器白名单，仅新增已验收的 MP3/WAV；禁止远程协议、本地越界路径、符号链接、无界资源消耗的现有防护保留。
- `parseVideoProbeOutput` 新增 audio-only 分支。MP3 接受 `mp3` 音轨；WAV 使用明确验证的 PCM codec 清单，不接受所有未知 WAV 编码。若允许封面 attached picture，必须显式识别该 disposition，不能误把普通视频当音频；首版不支持的包装给出可操作错误。
- 读取真实 stream/format 时长并交叉校验，拒绝缺失、负数、NaN/Inf、异常 stream 数量或超出资源上限的数据；不能通过 filename/MIME 伪装通过。MP3 VBR 等真实文件需纳入验证 fixture。
- 视频提取 fps 有理数、容器、codec 与尺寸；图片已有像素上限保留。不同 MIME 别名在边界归一化，例如有效 WAV 的浏览器 `audio/x-wav` 归为 `audio/wav`；归一化不能代替内容探测。
- 音频检查成功直接 `ready`，width/height=0，poster/preview URL 省略；播放器使用授权后的原音频 content URL。不得排入 `HandlePoster`，不得生成空封面假装成功。
- 文件可用性与请求组合分开：一段合法 8 秒音频可 ready，但只有音频的生成请求仍拒绝；模型 15 秒分类总长在 quote/submit 组合检查，不在单文件检查中猜测其它资产。

### 5.3 样片与历史引用

实施前的样片引用使用 `{asset_id,role,request_key}` 及平行 ID/URL 数组。本地实现已改为以下明确集合；旧平行字段不再序列化到新响应，历史类型声明不用于构造新生成请求。

新样片与新 generation 详情返回同一 `reference_assets` 读取形状：每项包含 `asset_id`、`role`、`position`、`asset`、`unavailable`。可访问且可使用时 `asset` 是 `VideoAssetView`（真实 MIME、状态、时长及授权内容地址），`unavailable:false`；素材已删除、无权读取或已不再可用时保留原绑定和位置，返回 `asset:null`、`unavailable:true`。`unavailable` 是稳定的必填 bool，不依赖 URL 是否存在或前端猜测；不泄露无权读取素材的文件名、内容地址等信息。只有明确没有参考素材的文字任务才返回空数组，不能因为缺媒体缩短列表。

后台样片写入只接收 `{asset_id,role,position}`。移除新接口中的 `request_key`、平行 ID/URL 数组，不要求前端推断类型或配对顺序。样片仍保留准确的 `model_profile_id`、`model`、`model_version` 等现有身份字段；`model_version` 是样片自身保存的能力版本，读取 shape 相同不要求将现有身份字段一并重命名。

新样片按新结构保存引用、能力版本、参数与提示词快照；`sample_id` 只是引用来源。旧样片不自动转换，用户目录不提供旧样片的“使用同款”；管理员可重新配置。旧记录及素材不批量删除。新草稿是前端新结构，不为旧 localStorage 建迁移代码。

生成结果视图继续使用现有 `id`（generation 数字 ID）、`task_id`（公开任务 ID）、`status/progress/quota/failure_code`。新 generation 详情必须提供上述完整 `reference_assets` 和只读 `specification_version`，用于结果页“本次使用”及复用新任务设置；不是可选 UI 增强。绑定从 task link 读取，缺媒体仍保留绑定。复用时恢复完整输入，逐项重新验证当前能力、权限和素材可用性；不把不可用项静默过滤后提交。

`CreateVideoGeneration` 已在 provisional persist 同一事务中，将 `normalized.SpecificationVersion` 写入 `task.PrivateData.StudioSpecificationVersion`；不保存签名 URL、凭据或重复完整 payload。generation 现有 model/mode/prompt/parameters 列，加上 task link 的 asset_id/role/position 和此版本，组成新任务完整输入快照，不必为了 generation 版本新建数据库列。读取时不得用当前 profile 的最新版本代替任务快照。旧记录的 `specification_version` 可以缺省，仅查看结果，不提供输入复用或伪造版本；不为其补写迁移。

## 6. 错误合同

本地 `controller/video_studio_response.go::respondVideoStudioError` 对结构化校验错误返回 `{success:false,message,code,issues}`，`issues` 位于顶层；普通错误保留 `{success:false,message,code}`。公共 Relay 失败仍可能返回 `dto.TaskError`，不在本轮统一重构。

```json
{
  "success": false,
  "message": "Reference video duration exceeds the model limit",
  "code": "video_reference_limit_exceeded",
  "issues": [
    {
      "field": "reference_assets",
      "role": "reference_video",
      "asset_id": 803,
      "position": 0,
      "rule": "max_total_duration_seconds",
      "actual": 15.01,
      "limit": 15
    }
  ]
}
```

外层 success/message/code 必填；结构化校验错误另带顶层 `issues`，没有逐项详情时可为空。每个 issue 必填 field/rule，其余字段仅在相关时出现。actual/limit 使用对应规则的单位，不把秒转换成显示文本。返回 asset_id/position 前必须完成归属判断，不能让错误消息泄露他人资产。前端按稳定 code 映射并保留安全回退文案；逐项 rule 的完整 UI 映射仍按验收计划核对，不能仅凭返回字段宣称已完成。500 隐藏内部错误和签名 URL。

| HTTP | code | 状态 | 前端动作 / 后端不变量 |
| --- | --- | --- | --- |
| 400 | `video_request_contract_invalid` | 本地已落地 | 旧格式、未知字段/role/mode、缺 position 或位置不连续；刷新/调整，任务及预扣均未创建 |
| 400 | `video_reference_combination_invalid` | 本地已落地 | 只有音频、缺必须视频、帧与多模态混用；按 issues 补素材 |
| 400 | `video_reference_limit_exceeded` | 本地已落地 | 类型数量、总数、字节、尺寸、时长、FPS 不符；保留其它有效文件 |
| 400 | `video_audio_output_required` | 本地已落地 | 当前预设未验收参考音频+关闭输出声音；用户选择开启或移除音频 |
| 409 | `video_capabilities_stale` | 本地已落地 | 重新获取能力、保留草稿、重新报价；不自动提交 |
| 409 | `video_reference_unavailable` | 本地已落地 | 自己的素材未 ready、已删或缺必要探测证据；查询/重传，不提交子集 |
| 409 | `video_idempotency_conflict` | 本地已落地分类 | 已有 `ErrIdempotencyConflict` 细分；同键不同语义明确拒绝，不自动换键 |
| 409 | `video_submission_pending` | 本地已落地分类 | 已有 `ErrIdempotencyInProgress` 细分；查询同键回执，不创建第二任务 |
| 409 | `quote_stale` | 已有，统一封装 | 过期、内容不匹配或当前报价超过 max_quota；刷新报价后由用户重新点击 |
| 400 | `video_quote_required` / `idempotency_key_required` | 已有 | 保留现有保护含义 |
| 400 | `video_token_required` | 已有 | 缺少工作室 Token；重新取得 Token 状态，不创建任务 |
| 403 | `video_token_invalid` / `video_token_group_invalid` / `video_token_model_forbidden` / `video_token_ip_forbidden` / `video_token_group_unavailable` / `video_asset_access_denied` | 已有 | Token、分组、型号、IP 或资产权限不符；不可绕过重试到其它身份 |
| 409 | `video_token_limit_reached` | 已有 | 工作室 Token 创建达到上限；保留草稿，提示处理 Token 状态 |
| 503 | `video_token_models_unavailable` | 已有 | 当前 Token 没有可用模型；更新能力状态，不自动更换身份 |
| 410 | `video_upload_expired` | 已有 | 旧上传会话过期；只重传该文件，不影响生成任务身份 |
| 503 | `video_storage_unavailable` / `video_asset_processing_unavailable` | 已有 | 保留草稿和成功素材；不因资源故障自动重建所有上传 |
| 503 | `video_model_capability_unavailable` | 本地已落地 | 型号/适配器/可选渠道无法满足该组合，阻止报价与派发 |

异步媒体检查失败保留 `asset.state=failed`、安全 `failure_reason`，拟新增可选 `failure_code` 以稳定映射 `video_media_invalid` / `video_media_limit_exceeded` / `video_media_processing_failed`。不能通过解析任意内部错误字符串伪造稳定 code，持久化方案见第 10 节。

## 7. 只读提交回执查询（已落地，未部署）

为了无 task ID 的断网/刷新恢复，已落地最小只读接口（当前工作树未提交、未部署）：

`GET /api/video-studio/submissions/by-key`，请求头 `Idempotency-Key: <原键>`，仍用用户 session。只读 `(user_id, operation=video.submit, key)`；不调用会回收/创建行的 `ReserveIdempotencyKey`。对应 `controller.LookupVideoStudioSubmission` 和只读 `service.LookupVideoSubmission`。

```json
{
  "success": true,
  "message": "",
  "data": {
    "status": "accepted",
    "task_id": "task_example",
    "generation_id": 9001
  }
}
```

- `accepted` 表示**本站已有 durable task 绑定**，不保证上游已确认成功；task 可能处于 dispatching/unknown/processing。返回 task_id；可定位 generation 时返回 generation_id，便于使用现有作品查询。
- `pending` 表示键已保留但尚未绑定任务，或已有绑定但对应 task 暂时缺失。`not_found` 表示当前查不到键；两者都不表示可安全创建新任务。接口不产生副作用、不更新 TTL、不替用户重发。
- 成功包中的 `status` 必填，task_id/generation_id 仅在存在时提供。未提供/无效键 400，跨账号查同键只返回该账号的结果，不暴露另一个账号的存在。
- 前端保留原提交快照与原键。未知受理先查回执，**不自动新键重发**；短暂 not_found 可能是 POST 尚在传输。对长期未决提供查询/人工处理，不伪造“肯定失败”。
- 当前幂等默认 TTL 为 24 小时；本地实现已保护视频非终态、受理不明或缺失 task 的绑定，避免过期后原键变成新的付费任务；直接复用现有记录与按任务状态判断，不新增迁移平台。已知明确未派发且未绑定的失败仍可安全释放保留。
- 新入口关闭时 owner 仍应能查该回执/任务。将该只读路由放在 owner 鉴权下，不以当前 profile enabled 或 Token 可生成该型号为查询前提；账号本身的有效性和数据隔离照常检查。

## 8. 状态链与失败边界

| 阶段 | 持久状态 / 关键操作 | 失败与恢复规则 |
| --- | --- | --- |
| 上传预留 | 资产 `pending_upload`，创建单对象/分片会话 | 只接受 owner 与已知安全用途；不创建 generation 或预扣 |
| PUT / complete | 核对对象长度、类型、分片、ETag/已知 checksum；`uploaded` → 检查 | R2 已完成而 DB 写失败时复用现有 recoverCompleted 路径；重复 complete 不产生第二份对象/任务 |
| 媒体检查 | 图像可沿用 inline 检查；视频/音频走受限 worker | 音频成功 ready；视频继续 poster/preview 后 ready；失败标文件，不让部分检查通过冒充全部可用 |
| quote | Token、能力版本、有效素材、组合、实际分组与渠道能力；规范化并计算报价 | 无扣费/无生成任务；失败保留 ready 资产；签名 URL 不入 hash |
| submit 入口 | 检查新请求形状与 required guard，计算 fingerprint；查/保留原幂等键 | 已有匹配绑定优先返回回执，不因 quote 现已过期再执行生成；冲突直接拒绝 |
| 新 submit 校验 | 未绑定新请求重新检查 Token/模型/能力、媒体状态、quote 签名及有效期，再计算当前费用 | 任何变化在派发前拒绝；高于 max_quota 不能扣费；unbound 保留按现有安全条件释放 |
| provisional persist | 现有 `SetTaskProvisionalPersistHook` 在事务内绑定幂等键、创建 generation 与 task assets；锁定并复核资产 | 资产被同时删除/失去权限则失败；不发送缺失后的子集；失败不得留下已扣未关联的悬空任务 |
| reserve / dispatch | 继续现有账务 `pending → reserved → dispatching`，持久任务先于网络派发 | 已进入 dispatching 且结果未知不能盲目退款或跨渠道再提交；信任 `CanRetry/CanRefund` 的可靠性边界 |
| accepted / ambiguous | 现有 task acceptance 或 unknown 记录；关联仍保留完整输入 | HTTP 响应丢失不代表未受理；lookup 返回 durable task，后台继续查；不让浏览器重传素材触发第二扣费 |
| poll / settle | 既有 polling、失败快照、结算/退款；结果归档进入 archiving/ready | 业务失败、归档失败、网络失败分别归因；不因播放器暂不可用直接退款；终态结算保持 exactly-once |

补充约束：

- 原请求形状验证在任何预扣之前；现有 `PrepareVideoStudioTaskRequest` 在分发前运行，可沿用边界。现有 idempotent replay 早于对象存储与分发，继续保留该安全顺序，不保留旧语义解析。
- 新合同使用严格解码/未知字段检测；JSON 仍通过 `common.Unmarshal/DecodeJson` 等项目 wrapper，不在业务代码直接调用 `encoding/json` marshal/unmarshal。解析所需 `RawMessage` 类型不受此限制。
- URL 在临近实际提交时从 ready 资产重新签发；有效期覆盖经验证的适配器拉取窗口，不把报价阶段生成的旧 URL 当成不变合同。不能无限扩大公开有效期，也不能把私有 bucket 放开解决过期。
- 同型号多渠道必须全部满足当前输入，或者在分发/重试候选选择时过滤到满足能力的渠道。具体实现优先使用现有渠道约束与分组，不先建设通用能力平台；无可证明匹配渠道时 profile 不开放。
- 调整参数、替换文件或排除素材只影响新表单；已派发任务固定其输入快照。关闭新增入口不取消在途任务，也不释放其未结算资金。

## 9. 外置适配器与计费责任

应用内部只使用一份规范化参考集合。`buildVideoTaskPayload` 输出给已确认适配链路的有序数据，不再同时产生 `images/image/reference_image` 兼容副本。供应商字段 `content[]` 或 `reference_images/reference_videos/reference_audios` 由适配边界投影，首版只选择一种已验证上游协议。

接入负责人交付适配器合同证据，至少包含：精确客户 model → 上游 model 的映射；每类素材数量/总数/格式；每角色顺序是否保留；三类组合与音频+generate_audio=false 的结果；URL 拉取时机及鉴权；任务提交/查询/结果错误映射；计费使用的型号、输出秒数及声音条件。未定位适配器源码与版本前，相关能力保持关闭。

本地 `plugins/tasks/sora/plugin.js` 已将有序集合投影为三类数组或首尾帧字段，并显式拒绝未知 metadata、旧字段和位置冲突，不透传任意 JSON。外置 adapter 仍需按这些实际输出字段完成独立合同验收；应用插件测试通过不构成真实上游贯通证据。组合支持与预扣前拒绝仍须在实际渠道链路验证。

计费沿用当前 `QuoteTaskSubmission → 预扣 → durable dispatch → settle/refund`。输入文件数量或音频时长不是默认计费倍率；只有真实商业契约要求时才加入，并同时修改 quote、预扣、结算和日志。所有乘数先有上界，额度转换使用 `common.Quota*Checked`，饱和标记继续进入 admin-only quota_saturation 审计，不使用裸 `int(float*ratio)`。若实际修改动态计价表达式，先完整读取 `pkg/billingexpr/expr.md`，本文件不授权顺带改计价语言。

## 10. 数据结构与三数据库边界

### 10.1 可直接复用

`KKAIVideoAsset` 已有 MIME、字节、时长、尺寸、codec、状态和对象键；`KKAIVideoTaskAsset` 的唯一键是 task_id/role/position，天然可表达同角色多素材；profile specification 与样片引用快照现为 TEXT JSON。新任务的规格版本优先保存于现有 `task.PrivateData` JSON，与 generation 现有创作字段和 task links 形成完整输入快照，不为该版本新增 schema。新 role 和 mode 是字符串值，不必为其建新表。旧记录不批量改写或删除；目前只有下节的媒体探测证据字段需要评估 DDL。

生成关联已使用规范化引用自己的 role 内 `Position`。清理角色白名单必须包含新 image/audio role，也继续识别存量任务关联，防止取消旧流程后误删在途文件。

### 10.2 必要探测证据与持久化方案

`KKAIVideoAsset.MediaMetadata` 已在本地实现为 `media_metadata` TEXT JSON 字段，记录 inspection_version、真实容器、视频 frame_rate_num/frame_rate_den；现有 duration/size/width/height/codec 继续使用已有列。只依赖 `state=ready` 不足以证明素材具有新约束要求的媒体证据。

扩展使用独立 `video-media-metadata-v1` receipt，保持 canonical runtime 21 + RC49；CLI 只在显式 proof 和授权 apply 时执行。本地定向验证已完成，生产 DDL 尚未执行，仍须单独授权和版本专用 runbook。缺证据资产不能用于新生成，不批量改写存量媒体或旧草稿。

异步资产检查目前保留 `failure_reason`；本节原先拟定的结构化 `validation_error/failure_code` 尚未落地，不能解析任意内部错误字符串伪造稳定 code。该未实现项与已存在的媒体证据字段分开记录。

SQLite、MySQL ≥5.7.8、PostgreSQL ≥9.6 统一使用 TEXT 存 JSON，避免仅某数据库支持的 JSON 操作；正常查询和写入用 GORM。需要行锁时 model 层使用现有 `lockForUpdate` 路径，service 复用当前视频锁封装，不另写 SQLite 不支持的 SQL。默认值按业务归一化，不增加 `gorm:"default:true"`。若新增列，三数据库迁移各自验证，SQLite 不用 ALTER COLUMN。

## 11. 可执行任务顺序

| 任务 | 具体输出 | 依赖 / 完成门槛 |
| --- | --- | --- |
| BE-01 冻结能力 | 第 3 节字段落为 Go DTO 和验证器；管理员新预设；精确型号验收清单 | 总数、声音组合、渠道/适配器证据未完成的 profile 不能启用 |
| BE-02 媒体证据 | audio purpose、MP3/WAV 内容探测、FPS/证据持久化、直接 ready 和错误投影 | 拍定第 10.2 节方案；三类文件的安全边界与失败重试清晰 |
| BE-03 规范化集合 | DTO 的 position、模式/组合/数量检查、引用 canonicalizer、quote/fingerprint 改造 | 只采用新语义；false/0 不丢；同 role 换序确实改变 hash |
| BE-04 提交与回执 | 新错误封装、只读 by-key、幂等 TTL 保护、provisional关联使用真实position | 无重复派发/扣费；不把 lookup not_found 当授权重建任务 |
| BE-05 适配与渠道 | 插件 + 外置 adapter 完整投影，报价和派发使用同一实际型号/能力 | mock HTTP 断言全部素材、顺序；未验收合同不能放量 |
| BE-06 样片/查询/清理 | 新样片结构、新任务完整输入快照与必返引用详情、完整关联保护；旧样片按需重新配置 | 规格版本不取当前 profile 冒充；缺媒体保留绑定并返回 asset:null/unavailable:true；数据不删 |
| BE-07 联调与切换 | 定向测试、前端/QA联合验收、正式 release contract 评估 | 只读查询/在途后台持续，创建入口可短暂停写，旧格式预扣前拒绝 |

跨仓适配器代码、迁移或发布门禁不是“后端提交了 PR”即可关闭的任务。若依赖未取得，明确记录未开放的精确型号，不用未经验证的能力让前端先显示可用。

## 12. 定向检查入口与实际执行边界

以下是前序方案保留的检查入口建议，并非每条命令均已执行；当前实际命令、匹配范围与结果见 [实现与验收记录](implementation-qa.md)，不能用计划前缀替代真实测试名称。新增/重写 Go 测试使用 testify require/assert，显式初始化 DB、Token、用户、渠道与计价状态，不做 sleep、随机压力或覆盖率凑数测试。

```bash
go test ./service -run '^(TestVideoMultimodal|TestVideoAudio|TestVideoSubmissionLookup|TestNormalizeVideoStudioSubmission|TestVideoStudio(Idempotency|Quote|RequestHash)|TestValidateVideoModelSpec|TestVideoIdempotency|TestReleaseUnboundVideoIdempotency)' -count=1 -timeout=120s
go test ./service -run '^(TestMultipartVideoUpload|TestCompleteReferenceVideoUpload|TestVideoAssetPipeline|TestFFmpegVideoMediaProcessor|TestParseVideoProbeOutput|TestSingleVideoUploadAbort|TestExpireVideoAssetUploads)' -count=1 -timeout=120s
go test ./controller -run '^(TestVideoMultimodal|TestVideoSubmissionLookup|TestPrepareVideoStudioTaskRequest|TestVideoStudioDistribution|TestVideoStudioSoraHTTPRejection)' -count=1 -timeout=120s
go test ./plugins -run '^TestSoraSpecialSeedance' -count=1 -timeout=120s
go test ./router -run '^(TestVideoMultimodal|TestVideoSubmissionLookup|TestVideoStudioMediaRoutesKeepAssetOwnershipBoundary|TestVideoStudioJSONRoutesAcceptSessionWithoutUserHeader)' -count=1 -timeout=120s
```

只在修改了相关共享边界时追加最近回归，而非全部 workspace：

```bash
go test ./relay -run '^(TestVideoMultimodal|TestRelayTask)' -count=1 -timeout=120s
go test ./service -run '^(TestTaskBillingRecoveryDoesNotRefundDispatchingCrash|TestPreConsumeTaskBillingWalletIsExactlyOnceAcrossRetryAndRefund|TestUpdateVideoSingleTaskHTTPErrorDoesNotRefund|TestFailPollingTaskDoesNotOverwriteConcurrentSuccessOrRefund)' -count=1 -timeout=120s
go test ./relay/channel/task/jsplugin -run '^(TestTaskAdaptorJSONOrderCapability|TestTaskAdaptorBuildSubmitReceivesMappedUpstreamModel|TestTaskAdaptorReDecodesFinalCandidateAndRejectsModelDrift)' -count=1 -timeout=120s
```

若采用 schema 扩展，先增加精确 `TestVideoMultimodalSchema` 系列覆盖三方 dialect，再仅执行 `go test ./pkg/kkaimigrate -run '^TestVideoMultimodalSchema' -count=1 -timeout=120s`，并完成版本专用 mandatory gates。本命令不能代替真实三数据库验证，也不表示已有可用测试数据库。

必须新增的真实断言：音频+图片成功、纯音频拒绝、视频必填拒绝、未知总限不可启用、分类与合计边界、严格字节边界、真实 MP3/WAV/FPS、同 role 换序及数组物理乱序区别、false 保留、签名重签 hash 不变、跨用户/已删除/非 ready 拒绝、样片不隐式补素材、新任务详情返回创建时规格版本且缺媒体仍保留绑定、旧记录无版本时不提供输入复用、unknown 回执查询、TTL 不重复派发、失败按真实账务状态结算。

mock 服务验收不能宣称上游生成质量或付费集成已通过；外置适配器真实验证由接入负责人另提供证据。执行者先确认本机缓存和工具链，若某命令耗时未知或预计超过五分钟，按仓库规则说明精确命令与预计耗时并取得授权；不以 `-timeout` 冒充编译耗时上限。不运行 `go test ./...`、全仓 lint/typecheck/build 作为默认补充。

## 13. 发布边界与本次完成记录

未来单一新接口切换按正式 runbook 和当前 controller/console contract 执行：必要时短暂停止新增生成，仍服务任务查询与后台结算；前端、应用、外置 adapter 均满足新合同后恢复入口。旧格式明确拒绝并提示刷新重新配置，不做重放桥接。破坏性合同必须声明接口变化并通过对应发布 gate，不能凭“不要兼容”绕过 release contract；新增 schema 操作另行授权。

本次只编写此开发文档，不修改运行代码、原 PRD、原型、迁移或部署配置；不访问生产、不付费生成、不运行未来测试命令。当前不能据此宣称多模态能力、回执查询接口或新错误码已经存在。

---
title: "Seedance 2.0 / 2.5 特价版快速开始"
description: "使用 KKRICH 公共 API 提交、查询并下载 Seedance 2.0 / 2.5 特价版视频。"
outline: [2, 3]
pageClass: kkr-seedance-page kkr-seedance-landing
---

# Seedance 2.0 / 2.5 特价版快速开始

<div class="seedance-hero">
  <div class="seedance-hero-copy">
    <p class="seedance-kicker">SEEDANCE 2.0 / 2.5 SPECIAL</p>
    <p class="seedance-headline">从一次提交，到拿到可下载的视频。</p>
    <p class="seedance-summary">使用 KKRICH Token 调用异步视频任务。所有 API 请求、任务查询和视频下载都通过本站完成。</p>
    <div class="seedance-actions">
      <a class="kkr-button primary" href="#五分钟跑通">五分钟跑通</a>
      <a class="kkr-button" href="/docs/api/video-generation">完整 API</a>
    </div>
  </div>
  <div class="seedance-task-panel" aria-label="Seedance 视频任务流程示例">
    <div class="seedance-panel-top">
      <span>VIDEO TASK</span>
      <span class="seedance-live-dot">READY</span>
    </div>
    <div class="seedance-frame">
      <span>POST /v1/video/generations</span>
      <strong>16:9</strong>
    </div>
    <dl class="seedance-task-meta">
      <div><dt>MODEL</dt><dd>sd_2.5_special_720p</dd></div>
      <div><dt>STATUS</dt><dd class="seedance-status-ready">completed</dd></div>
      <div><dt>OUTPUT</dt><dd>/content</dd></div>
    </dl>
  </div>
</div>

<div class="seedance-facts" aria-label="Seedance 接入要点">
  <div><span>Base URL</span><strong>https://api.kkrich.ltd</strong></div>
  <div><span>认证</span><strong>Bearer Token</strong></div>
  <div><span>任务模式</span><strong>提交 → 查询 → 下载</strong></div>
</div>

::: warning 地址以本站为准
本文所有 API 地址都属于 `https://api.kkrich.ltd`。不要把其他平台或渠道文档中的域名、路径、模型名和鉴权方式复制到本站请求中。
:::

> **协议范围**：2.0 与 2.5 新建流程均采用下方统一参考物结构，旧标量参考字段不再兼容。
> 本地实现与外置 adapter 的验收状态见本页提示。

::: warning 本地 Seedance 新建协议待验收
当前本地新建流程统一使用 `metadata.reference_assets`，每项包含 `role`、`position` 和
`url`；本地插件会将其投影为供应商的 `reference_images`、`reference_videos`、
`reference_audios`、`first_image` 或 `last_image`。外置 adapter 尚未验收，本页新建请求
仅作为本地协议对照，不表示该链路已经上线。认证、查询和下载仍按现有公共接口说明。
:::

## 开始前准备

1. 在 [令牌管理](https://api.kkrich.ltd/keys/) 创建或选择可调用 **Seedance 视频** 分组的 Token。
2. 把 Token 放入环境变量，不要写进前端代码、截图或代码仓库。
3. 使用本站模型列表确认当前 Token 可以看到目标模型。

```bash
export KKAI_API_KEY="你的 Token"

curl "https://api.kkrich.ltd/v1/models" \
  -H "Authorization: Bearer $KKAI_API_KEY"
```

如果模型列表中没有目标模型，先检查 Token 分组和账号权限，不要仅凭旧文档强行提交。

## 先选版本

| 需求 | 建议模型 | 时长 | 输出与参考 |
| --- | --- | --- | --- |
| 2.5 720p | `sd_2.5_special_720p` | 4-30 秒 | 720p；文生或 `reference_image` 角色 |
| 2.5 1080p | `sd_2.5_special_1080p` | 4-30 秒 | 1080p；文生或 `reference_image` 角色 |
| 2.5 视频参考 720p | `sd_2.5_special_720p_with_video_ref` | 4-30 秒 | 720p；必须 `reference_video` 角色 |
| 2.5 视频参考 1080p | `sd_2.5_special_1080p_with_video_ref` | 4-30 秒 | 1080p；必须 `reference_video` 角色 |
| 2.0 快速 720p | `sd_2.0_fast_special_720p` | 4-15 秒 | 文生或 `reference_image` 角色 |
| 2.0 指定分辨率 | `sd_2.0_special_720p` / `1080p` / `2k` / `4k` | 4-15 秒 | 分辨率写在模型名中 |
| 2.0 视频参考 | 名称以 `_with_video_ref` 结尾的 2.0 模型 | 4-15 秒 | 必须包含 `reference_video` 角色 |

完整的 2.0 模型名称和差异见 [模型与能力矩阵](/api/video-generation#模型与能力矩阵)。模型是否可用始终以当前 Token 调用 `/v1/models` 的结果为准。

四个 `sd_2.5_special_*` 名称沿用既有 Seedance 2.0 命名规则，是本站正式客户模型名，分别对应四个计费能力档。所有 2.5 名称都支持以下画幅：`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`、`adaptive`。
2.5 的普通名称（不带 `_with_video_ref` 后缀）用于文生或图片参考；带 `_with_video_ref` 后缀的名称
必须在 `metadata.reference_assets` 中包含 `reference_video` 角色，不能只传提示词。四个正式名称都接受 `generate_audio: true`
或 `false`，建议显式传入。

2.0 也支持以上 7 种画幅；2.0 的视频参考能力仍由模型名末尾的 `_with_video_ref` 决定。

## 用户价格

以下是当前 `Seedance 视频` 分组的用户价格，按余额单位/秒计费。本站当前充值比例为 `1:1`，即人民币 1 元充值到账 1 个余额单位；控制台的 `$` 是余额显示符号，表中价格不再乘汇率。当前分组倍率为 `1`，因此实际扣费为「视频时长（秒）× 表中单价」。价格按生产环境 `/api/pricing` 核对，最后核对时间：2026-10-19（北京时间）。

| 模型 | 单价（余额单位/秒） |
| --- | ---: |
| `sd_mini_special_720p` | 0.12 |
| `sd_mini_special_720p_with_video_ref` | 0.30 |
| `sd_2.0_fast_special_720p` | 0.25 |
| `sd_2.0_fast_special_720p_with_video_ref` | 0.55 |
| `sd_2.0_special_720p` | 0.60 |
| `sd_2.0_special_720p_with_video_ref` | 1.40 |
| `sd_2.0_special_1080p` | 0.60 |
| `sd_2.0_special_1080p_with_video_ref` | 1.45 |
| `sd_2.0_special_2k` | 0.70 |
| `sd_2.0_special_2k_with_video_ref` | 1.50 |
| `sd_2.0_special_4k` | 0.70 |
| `sd_2.0_special_4k_with_video_ref` | 1.50 |
| `sd_2.5_special_720p` | 0.80 |
| `sd_2.5_special_720p_with_video_ref` | 2.90 |
| `sd_2.5_special_1080p` | 0.85 |
| `sd_2.5_special_1080p_with_video_ref` | 3.10 |

例如，`sd_mini_special_720p` 生成 5 秒视频扣除 `0.60` 个余额单位。请使用表中的正式模型名；原兼容别名 `seedance-2.5` 当前已不在可用模型列表中。视频参考模型必须同时满足该模型的参考素材要求；同一模型的单价不会因是否传入参考素材而改变。

### 时长兜底

`duration` 和 `seconds` 都是兼容字段，均可省略。缺失、`null`、空字符串、`0` 或 `1-3`
会由本站按 `4` 秒处理，并按 `4` 秒计费；负数、小数、无法解析的值和超过模型上限的值
会返回 400。2.0 的上限是 15 秒，2.5 的上限是 30 秒。客户端仍建议显式传入有效范围内
的整数（2.0 传 `4-15`，2.5 传 `4-30`）。同时传两个字段时，`null`、空字符串和 `0`
视为未提供，由另一个字段接管；两个非空且非 `0` 的值会先各自按最少 4 秒规范化，结果必须一致。

## 五分钟跑通

### 1. 提交 2.5 文生视频

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.5_special_720p",
    "prompt": "原创雨夜街道，一列有轨电车缓慢驶过，镜头平稳下降，路面有霓虹倒影，无文字、无 Logo",
    "duration": 5,
    "ratio": "16:9",
    "resolution": "720p",
    "generate_audio": true
  }'
```

成功响应会包含本站公开任务 ID。客户端应保存 `id`；`task_id` 是兼容字段，不要把它当作唯一长期契约。

```json
{
  "id": "task_01JVIDEOEXAMPLE",
  "task_id": "task_01JVIDEOEXAMPLE",
  "object": "video",
  "model": "sd_2.5_special_720p",
  "status": "queued",
  "progress": 0,
  "created_at": 1787587200,
  "seconds": "5"
}
```

### 2. 查询原任务

把提交响应中的 `id` 保存到变量，然后轮询同一个任务。建议从 20 秒间隔开始，逐步退避到最多 60 秒。

```bash
TASK_ID="task_01JVIDEOEXAMPLE"

curl "https://api.kkrich.ltd/v1/video/generations/$TASK_ID" \
  -H "Authorization: Bearer $KKAI_API_KEY"
```

```json
{
  "code": "success",
  "message": "",
  "data": {
    "task_id": "task_01JVIDEOEXAMPLE",
    "status": "SUCCESS",
    "result_url": "https://api.kkrich.ltd/v1/videos/task_01JVIDEOEXAMPLE/content",
    "fail_reason": ""
  }
}
```

通用查询中，`SUCCESS` 表示完成，`FAILURE` 表示失败；`SUBMITTED`、`QUEUED`、`IN_PROGRESS` 都应继续查询原任务。不要在等待期间重新发送付费 `POST`。

### 3. 从本站下载

只有任务完成后才能下载，并且下载请求仍要携带同一个用户的有效 Token。

```bash
curl -L "https://api.kkrich.ltd/v1/videos/$TASK_ID/content" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -o seedance.mp4
```

不要直接依赖响应中可能出现的临时结果地址。固定使用本站 `/v1/videos/{task_id}/content`，需要长期保存时及时下载到自己的存储。

## 2.0 最小请求

2.0 的分辨率由模型名决定。下面是 720p 快速版文生视频：

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.0_fast_special_720p",
    "prompt": "原创海边日出，镜头缓慢前移，海浪和海风环境音，无文字、无 Logo",
    "duration": 4,
    "ratio": "16:9",
    "generate_audio": true
  }'
```

视频参考模型必须使用带 `_with_video_ref` 后缀的 2.0 模型，或使用上表中的 2.5
`_with_video_ref` 正式名称，并在 `metadata.reference_assets` 中传 `reference_video` 角色。
具体素材组合和数量以已验收的模型能力为准。

## 图片与视频参考

2.5 单图参考示例（720p）：

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.5_special_720p",
    "prompt": "保持主体和构图，让树叶随风摆动，镜头轻微推进",
    "duration": 6,
    "ratio": "1:1",
    "resolution": "720p",
    "metadata": {
      "reference_assets": [
        {
          "role": "reference_image",
          "position": 0,
          "url": "https://media.your-domain.example/reference.jpg"
        }
      ]
    },
    "generate_audio": false
  }'
```

参考素材必须遵守以下规则：

- 新建请求只使用 `metadata.reference_assets`，每项形如
  `{ "role": "reference_image", "position": 0, "url": "https://..." }`。
  `role` 可为 `reference_image`、`reference_video`、`reference_audio`、`first_frame` 或 `last_frame`。
- 同一角色的 `position` 必须从 0 连续递增；图片、视频和音频按位置投影为供应商数组，
  首帧和尾帧分别投影为 `first_image` 与 `last_image`。
- 2.5 `_with_video_ref` 正式名称必须有一个 `reference_video` 角色，不能只传提示词。
- `reference_audio` 不能脱离图片或视频单独提交；首帧、首尾帧和多模态参考按 profile 约束互斥。
- 公开素材建议使用无需登录即可访问的 HTTPS 直链。本站会拒绝私网地址、本地路径、`data:` 和带凭据的 URL。
- 已经由平台签发且仍有效的素材可使用 `assetId://<asset_id>`；不要自行编造资产 ID。
- 不要发送旧的 `reference_image`、`reference_video`、`reference_audio`、`input_reference`，
  或供应商侧的 `reference_images`、`reference_videos`、`reference_audios`、`first_image`、
  `last_image` 等字段；供应商参考物字段由 adapter 生成，`tools` 不在支持范围内。

完整约束见 [请求字段](/api/video-generation#请求字段)。

### 2.5 视频参考示例（1080p）

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.5_special_1080p_with_video_ref",
    "prompt": "保持主体动作节奏，改为原创电影感冷色调，无文字、无 Logo",
    "duration": 8,
    "ratio": "9:16",
    "resolution": "1080p",
    "metadata": {
      "reference_assets": [
        {
          "role": "reference_video",
          "position": 0,
          "url": "https://media.your-domain.example/reference.mp4"
        }
      ]
    },
    "generate_audio": true
  }'
```

需要 720p 视频参考时，将 `model` 和 `resolution` 同时改为
`sd_2.5_special_720p_with_video_ref` 与 `720p`。`resolution` 必须与所选 2.5 名称一致。

### 多模态参考示例（本地协议）

以下仅用于核对本地请求结构，外置 adapter 验收通过前不代表生产可用：

```json
{
  "model": "sd_2.5_special_720p_with_video_ref",
  "mode": "reference_to_video",
  "prompt": "保持人物动作和声音氛围，生成原创电影感片段",
  "duration": 8,
  "metadata": {
    "reference_assets": [
      {"role": "reference_image", "position": 0, "url": "https://media.your-domain.example/subject.jpg"},
      {"role": "reference_video", "position": 0, "url": "https://media.your-domain.example/motion.mp4"},
      {"role": "reference_audio", "position": 0, "url": "https://media.your-domain.example/voice.mp3"}
    ]
  },
  "generate_audio": true
}
```

## 两套查询格式

本站同时提供两套客户接口。提交和查询应配套使用，避免混淆响应结构。

| 格式 | 提交 | 查询 | 完成状态 |
| --- | --- | --- | --- |
| 通用任务格式 | `POST /v1/video/generations` | `GET /v1/video/generations/{task_id}` | `data.status === "SUCCESS"` |
| OpenAI 视频兼容格式 | `POST /v1/videos` | `GET /v1/videos/{task_id}` | `status === "completed"` |

两套格式共用下载接口：`GET /v1/videos/{task_id}/content`。所有路径都拼接在 `https://api.kkrich.ltd` 后面。

## 接下来

<div class="seedance-tool-list">
  <a href="/docs/api/video-generation"><strong>完整 API 参考</strong><span>全部模型、字段、响应、Python 和 Node.js 示例。</span></a>
  <a href="/docs/support/seedance-video"><strong>错误排查</strong><span>参数、认证、任务状态、素材和下载问题。</span></a>
</div>

价格表按生产环境当前配置维护；如控制台价格与本文不一致，以生产环境 `/api/pricing` 和实际用量记录为准。

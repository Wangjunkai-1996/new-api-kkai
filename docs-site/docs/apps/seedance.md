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
      <div><dt>MODEL</dt><dd>seedance-2.5</dd></div>
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

> **2.0 协议说明**：2.0 特价版的服务端协议已切换 v2，但客户侧模型别名、请求字段、
> 本站接口地址和鉴权方式不变。现有客户请求无需改成其他地址。

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
| 使用 2.5 特价版 | `seedance-2.5` | 4-30 秒 | 当前 720p；文生或单图参考 |
| 2.0 快速 720p | `sd_2.0_fast_special_720p` | 4-15 秒 | 文生或单图参考 |
| 2.0 指定分辨率 | `sd_2.0_special_720p` / `1080p` / `2k` / `4k` | 4-15 秒 | 分辨率写在模型名中 |
| 2.0 视频参考 | 名称以 `_with_video_ref` 结尾的 2.0 模型 | 4-15 秒 | 必须提供单个 `reference_video` |

完整的 2.0 模型名称和差异见 [模型与能力矩阵](/api/video-generation#模型与能力矩阵)。模型是否可用始终以当前 Token 调用 `/v1/models` 的结果为准。

2.0 和 2.5 都支持以下画幅：`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`、`adaptive`。

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
    "model": "seedance-2.5",
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
  "model": "seedance-2.5",
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

视频参考模型必须使用带 `_with_video_ref` 后缀的模型，并传一个 `reference_video`。不要把视频参考字段发给普通 2.0 模型或 `seedance-2.5`。

## 图片与视频参考

2.5 单图参考示例：

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.5",
    "prompt": "保持主体和构图，让树叶随风摆动，镜头轻微推进",
    "duration": 6,
    "ratio": "1:1",
    "resolution": "720p",
    "reference_image": "https://media.your-domain.example/reference.jpg",
    "generate_audio": false
  }'
```

参考素材必须遵守以下规则：

- 只传一个图片字段：`reference_image` 或兼容别名 `input_reference`，二选一。
- 2.5 不接受视频或音频参考；不要发送 `reference_video`、`reference_audio` 及其数组形式。
- 公开素材建议使用无需登录即可访问的 HTTPS 直链。本站会拒绝私网地址、本地路径、`data:` 和带凭据的 URL。
- 已经由平台签发且仍有效的素材可使用 `assetId://<asset_id>`；不要自行编造资产 ID。
- 不要发送 `reference_images`、`reference_videos`、`reference_audios`、`first_image`、`last_image`、`tools` 等未列入本站协议的字段。

完整约束见 [请求字段](/api/video-generation#请求字段)。

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

本文不维护价格或倍率；相关信息以本站控制台实时配置和实际用量记录为准。

# Seedance 视频生成 API（2.0 / 2.5 特价版）

本文覆盖 KKRICH 当前已接入的 Seedance 2.0 和 2.5 特价版，包含模型选择、参数、
提交、查询、下载、错误处理和完整代码示例。价格、分组倍率和最终扣费以控制台及
使用日志为准，不在本文重复维护。

本文所有可直接调用的接口均属于 KKRICH 公共 API，统一使用
`https://api.kkrich.ltd`。示例中的素材 URL 是客户自行托管的公开文件地址，不是 API
地址。

> 适用版本：Seedance 2.0 / 2.5 特价版。协议核验日期：2026-08-25。

> **模型名和地址必须以本站为准**：上游/供应商文档中的模型标识不是客户请求
> `model` 值，本站只接受 `/v1/models` 返回的客户模型名。当前 2.5 正式名称为
> `sd_2.5_special_720p`、`sd_2.5_special_1080p`、`sd_2.5_special_720p_with_video_ref` 和
> `sd_2.5_special_1080p_with_video_ref`；已有 `seedance-2.5` 继续兼容。不要把上游 API 地址、路径、鉴权方式或模型 ID
> 带到本站调用中。

> **2.0 协议说明**：2.0 特价版的服务端协议已切换到 v2 适配，但本站客户模型名、
> 请求字段、本站接口地址和鉴权方式均不变。客户仍按本文示例调用，不需要拼接任何
> 内部或渠道地址。

## 1. 适用范围

KKRICH 的视频接口是异步任务接口：提交请求只负责创建任务，客户端必须保存任务
ID 并轮询，任务成功后再下载视频。任务和视频内容都按 Bearer Token 对应的用户
隔离；查询和下载都必须携带任务所属用户的有效 Token，建议始终复用提交任务的 Token。

公开接口有两套响应形状。下面的路径都要拼接到本文的 Base URL：

| 用途 | 提交 | 查询 | 下载 |
| --- | --- | --- | --- |
| 通用任务格式（推荐用于本平台） | `POST https://api.kkrich.ltd/v1/video/generations` | `GET https://api.kkrich.ltd/v1/video/generations/{task_id}` | `GET https://api.kkrich.ltd/v1/videos/{task_id}/content` |
| OpenAI 视频兼容格式 | `POST https://api.kkrich.ltd/v1/videos` | `GET https://api.kkrich.ltd/v1/videos/{task_id}` | `GET https://api.kkrich.ltd/v1/videos/{task_id}/content` |

两套接口可以使用同一组 Seedance 模型和请求字段，但查询响应不能混用：通用任务
查询返回 `{code, message, data}`，OpenAI 查询返回视频对象。客户端应根据实际
调用的提交接口选择对应的查询接口。

当前 Seedance 特价版不支持 `POST /v1/videos/{video_id}/remix`；该路径仅为其他
兼容适配器保留，不能用于本文模型。

## 2. 认证与请求约定

### Base URL

```text
https://api.kkrich.ltd
```

### Headers

```http
Authorization: Bearer $KKAI_API_KEY
Content-Type: application/json
```

Token 必须属于可用的 `Seedance 视频` 分组。不要把 Token 写入前端代码、日志、截图
或版本库。`GET` 查询和 `/content` 下载都需要再次发送 `Authorization`；建议复用提交
任务的 Token。

请求体使用 JSON。字段名区分大小写；模型名必须完整匹配。本文没有列出的供应商或
其他平台字段不要自行拼接到请求中。

### 先确认 Token 可见模型

模型启用和分组权限可能随账号变化。正式提交前可用本站模型列表确认：

```bash
curl "https://api.kkrich.ltd/v1/models" \
  -H "Authorization: Bearer $KKAI_API_KEY"
```

下文的模型矩阵是当前已核验的客户名称；如果 `/v1/models` 没有返回某个名称，不能
仅凭本文或历史截图调用它。

## 3. 当前模型与能力矩阵

下表列出当前适配器确认的客户模型名。模型是否对某个 Token 可见，仍由 Token 分组
和渠道启用状态决定；以本站 `/v1/models` 返回结果为最终准入清单。

### Seedance 2.0 特价版

所有 2.0 模型的时长为 **4-15 秒整数**，支持文生视频。没有 `_with_video_ref`
后缀的模型支持通过 `reference_image` 做图生视频；带后缀的模型要求
`reference_video`，用于视频参考生成。以上是当前 Seedance 特价适配器的模型级校验，
不是其他视频模型的通用规则。

带 `_with_video_ref` 后缀的模型至少要提供 `reference_video`；如业务需要，也可以再
提供一个 `reference_image`，但图片和视频都必须通过素材 URL/资产校验。

| 模型 | 输出分辨率 | 参考方式 |
| --- | --- | --- |
| `sd_2.0_fast_special_720p` | 720p | 文生 / `reference_image` |
| `sd_2.0_special_720p` | 720p | 文生 / `reference_image` |
| `sd_2.0_special_1080p` | 1080p | 文生 / `reference_image` |
| `sd_2.0_special_2k` | 2k | 文生 / `reference_image` |
| `sd_2.0_special_4k` | 4k | 文生 / `reference_image` |
| `sd_2.0_fast_special_720p_with_video_ref` | 720p | 必须 `reference_video`；可选 `reference_image` |
| `sd_2.0_special_720p_with_video_ref` | 720p | 必须 `reference_video`；可选 `reference_image` |
| `sd_2.0_special_1080p_with_video_ref` | 1080p | 必须 `reference_video`；可选 `reference_image` |
| `sd_2.0_special_2k_with_video_ref` | 2k | 必须 `reference_video`；可选 `reference_image` |
| `sd_2.0_special_4k_with_video_ref` | 4k | 必须 `reference_video`；可选 `reference_image` |

2.0 支持以下 `ratio`：`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`、`adaptive`。
`generate_audio` 接受 `true` 或 `false`；客户端应显式传值，避免依赖缺省行为。分辨率
已经编码在模型名中；如果传 `resolution`，必须与模型能力一致。

### Seedance 2.5 特价版

当前正式客户模型有四个，命名沿用既有 2.0 规则，分别对应分辨率和参考方式：

| 模型 | 输出分辨率 | 参考方式 |
| --- | --- | --- |
| `sd_2.5_special_720p` | 720p | 文生 / 单图参考 |
| `sd_2.5_special_1080p` | 1080p | 文生 / 单图参考 |
| `sd_2.5_special_720p_with_video_ref` | 720p | 必须单个视频参考；可选单图参考 |
| `sd_2.5_special_1080p_with_video_ref` | 1080p | 必须单个视频参考；可选单图参考 |
| `seedance-2.5` | 720p | 旧客户兼容别名；文生 / 单图参考 |

模型是否对当前 Token 可见，仍以本站 `/v1/models` 返回结果为准；如果控制台没有显示
某个名称，就不能调用它。

- 时长：4-30 秒整数。
- `ratio`：`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`、`adaptive`。
- 普通正式名称（不带 `_with_video_ref` 后缀）：使用一个公开 HTTPS 或有效 `assetId://` 的
  `reference_image`；不支持 `reference_video`。
- `_with_video_ref` 正式名称：必须使用一个 `reference_video`，也可以附带一个图片参考。
- `generate_audio` 接受 `true` 或 `false`；为避免依赖缺省行为，客户端应显式传值。

## 4. 请求字段

| 字段 | 类型 | 必填 | 2.0 | 2.5 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `model` | string | 是 | 支持 | 支持 | 使用上面的完整客户模型名，大小写敏感。 |
| `prompt` | string | 是 | 支持 | 支持 | 描述主体、动作、镜头、时序、声音和限制条件。去除首尾空白后不能为空，最多 8192 字节，不能包含 NUL 字符。 |
| `duration` | integer 或整数字符串 | 否 | 4-15 | 4-30 | 推荐字段。缺失、空值、`0` 或 `1-3` 按 `4` 处理并按 `4` 计费。 |
| `seconds` | integer 或整数字符串 | 否 | 4-15 | 4-30 | 兼容字段；缺失、空值、`0` 或 `1-3` 按 `4` 处理并按 `4` 计费。 |
| `ratio` | string | 否 | 7 种 | 7 种 | 默认 `16:9`；支持 `16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`、`adaptive`。 |
| `resolution` | string | 否 | 由模型决定 | 按模型名为 `720p` 或 `1080p` | 2.0 分辨率由模型名决定；2.5 必须匹配所选模型名。 |
| `reference_image` | string | 否 | 图生时使用 | 普通名称单图参考；视频参考名称可选 | 只能传一个图片引用；推荐使用公开 HTTPS URL。 |
| `input_reference` | string | 否 | 兼容别名 | 兼容别名 | 与 `reference_image` 互斥；新代码优先使用 `reference_image`。 |
| `reference_video` | string | 条件 | `_with_video_ref` 必填 | `_with_video_ref` 名称必填；普通名称禁止 | 只能给支持视频参考的模型使用。 |
| `generate_audio` | boolean | 否 | 支持 | 支持 | 显式传 `true` 或 `false`，不要依赖缺省值。 |

`duration` 和 `seconds` 都可以省略；缺失、`null`、空字符串、`0` 和 `1-3` 会由本站
服务端统一规范为有效时长 `4`，并按 `4` 秒计费。负数、小数、无法解析的值以及超过
模型上限的值会直接返回 400（2.0 上限 15 秒，2.5 正式名称和兼容别名上限 30 秒）。两者同时传时，
`null`、空字符串和 `0` 视为未提供，由另一个字段接管；两个非空且非 `0` 的值会先
各自按最少 4 秒规范化，结果必须一致。为避免歧义并便于客户端展示，仍建议显式传入 2.0 的
`4-15` 或 2.5 的 `4-30` 整数秒，例如 `5` 或 `"5"`；不要传 `5.0`、`"05"`。

参考素材规则：

- 一个请求最多一个图片参考，`reference_image` 与 `input_reference` 不能同时出现。客户端只
  发送单个字段，不要自行改成 `reference_images[]`；服务端适配器会按内部协议投影。
- 公开 URL 必须使用 HTTPS 443 端口；不能带用户名密码、fragment、base64、`data:` 或
  本地路径。本站安全策略还会拒绝指向私网/保留地址或解析出混合公私网地址的域名。
- 2.0 和 2.5 可以使用公开 HTTPS URL；也可以引用平台已经签发且处于可用状态的
  `assetId://<asset_id>`。这些是参考素材地址/资产 ID，不是本站 API 地址；本文公共协议不
  提供创建资产 ID 的上传接口，普通 API 客户应使用公开 HTTPS URL。视频参考遵循同样的规则。
- 不要发送 `reference_images[]`、`aspect_ratio`、`width`、`height`、`fps`、`seed`、
  `n`、`size`、`metadata`、`content`、`tools` 等本文未列出的字段；严格的 Seedance
  适配器会拒绝未知字段，其他通用适配器也不保证透传结果。

其他平台或渠道页面中的模型名和字段不属于本站公共请求协议；本站只接受本文列出的
客户模型、字段和本站地址。

常见渠道字段到本站字段的对应关系：

| 渠道文档概念 | 本站公共请求 | 说明 |
| --- | --- | --- |
| 2.5 模型名称 | 本站四个 `sd_2.5_special_*` 正式名称及兼容别名 | 客户模型名以本站 `/v1/models` 为准；不要发送上游模型 ID。 |
| `aspect_ratio` | `ratio` | 只发送本站枚举值。 |
| `reference_images[]` | 单个 `reference_image` 或 `input_reference` | 两个别名不能同时传。 |
| 多个视频/音频参考、首帧/尾帧、`seed`、`tools` | 不支持 | 不要改名后强行透传。 |
| 渠道专用任务提交/查询路径 | 本文列出的本站 `/v1/...` 路径 | 客户只请求 `api.kkrich.ltd`。 |

素材 URL 由客户自行托管。下文的 `your-public-host.example` 仅用于参考素材占位，不能作为
KKRICH API Base URL，也不需要向该域名发送 API 请求；所有 API 请求仍只发送到
`https://api.kkrich.ltd`。

本站公共接口没有可依赖的幂等键。不要把 `Idempotency-Key` 当成去重保证；提交超时或
收到 `task_submission_unknown` 时只保存任务上下文并 GET 查询，不能自动重发 POST。

## 5. 最小可运行示例

### 2.0 文生视频

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.0_fast_special_720p",
    "prompt": "原创海边日出，镜头缓慢前移，海浪和海风环境音，无文字、无 Logo",
    "seconds": "4",
    "ratio": "16:9",
    "generate_audio": true
  }'
```

### 2.0 视频参考（完整请求）

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.0_special_1080p_with_video_ref",
    "prompt": "保持主体和动作节奏，改成电影感冷色调，无文字",
    "duration": 8,
    "ratio": "9:16",
    "reference_video": "https://your-public-host.example/reference.mp4"
  }'
```

### 2.0 图生视频

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.0_special_720p",
    "prompt": "保持原图主体和构图，让树叶随风摆动，镜头缓慢推进，无文字",
    "duration": 6,
    "ratio": "16:9",
    "reference_image": "https://your-public-host.example/reference.jpg"
  }'
```

### 2.5 文生视频

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.5_special_720p",
    "prompt": "一列有轨电车穿过雨夜街道，镜头从高处缓慢下降，霓虹倒影，无字幕",
    "duration": 5,
    "ratio": "16:9",
    "resolution": "720p",
    "generate_audio": true
  }'
```

### 2.5 图生视频

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.5_special_720p",
    "prompt": "让画面中的树叶随风摆动，镜头轻微推进，保持原有构图",
    "duration": 6,
    "ratio": "1:1",
    "resolution": "720p",
    "reference_image": "https://your-public-host.example/reference.jpg",
    "generate_audio": false
  }'
```

### 2.5 1080p 文生视频

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.5_special_1080p",
    "prompt": "原创城市夜景，镜头平稳向前推进，无字幕、无 Logo",
    "duration": 5,
    "ratio": "16:9",
    "resolution": "1080p",
    "generate_audio": true
  }'
```

### 2.5 视频参考

带 `_with_video_ref` 后缀的正式名称必须提供一个 `reference_video`。下面以 720p 为例；使用
1080p 时将模型和 `resolution` 一起改为 `sd_2.5_special_1080p_with_video_ref` 与
`1080p`。

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.5_special_720p_with_video_ref",
    "prompt": "保持主体动作节奏，改为原创电影感冷色调，无文字、无 Logo",
    "duration": 8,
    "ratio": "9:16",
    "resolution": "720p",
    "reference_video": "https://your-public-host.example/reference.mp4",
    "generate_audio": true
  }'
```

## 6. 提交响应与任务 ID

视频提交成功时会返回 OpenAI 风格的视频对象。`id` 是本站公开任务 ID；提交响应可能
同时返回同值的兼容字段 `task_id`。客户端必须保存 `id`，并把这个值原样用于后续查询
和下载。

```json
{
  "id": "task_01JVIDEOEXAMPLE",
  "task_id": "task_01JVIDEOEXAMPLE",
  "object": "video",
  "model": "sd_2.5_special_720p",
  "status": "queued",
  "progress": 0,
  "created_at": 1764347090,
  "seconds": "5"
}
```

提交成功只代表任务已被接受，不代表已经有可下载文件。响应可能没有任何结果 URL；
此时仍然只保存 `id` 并查询，不要重复发送付费 POST。

## 7. 查询、状态与下载

### OpenAI 兼容查询

```bash
curl "https://api.kkrich.ltd/v1/videos/$TASK_ID" \
  -H "Authorization: Bearer $KKAI_API_KEY"
```

后续查询响应中的 `task_id` 是旧兼容字段，可能缺失，不属于稳定客户契约。轮询和下载
始终使用提交时保存的 `id`，不要用查询响应中的兼容字段覆盖它。完成响应如果包含
`video_url`，本站会将其改写为本站内容代理地址；客户端仍应按固定的 `/content` 路径下载。

状态通常使用小写值：

| 状态 | 含义 | 下一步 |
| --- | --- | --- |
| `queued` | 已提交，等待排队 | 继续 GET 查询 |
| `processing` / `in_progress` | 正在生成 | 继续 GET 查询 |
| `completed` | 已完成 | 调用 `/content` 下载 |
| `failed` | 失败 | 读取 `error.message`，不要盲目重试 POST |
| `unknown` | 服务端无法确认当前状态 | 保留任务 ID，联系支持，不要重试 POST |

为兼容未来版本或其他视频适配器，客户端应容忍 `pending`、`cancelled` 等额外状态值，
不要因为出现未知值或未知字段而解析失败，并始终以本站 `/content` 下载接口为准。

### 通用任务查询

```bash
curl "https://api.kkrich.ltd/v1/video/generations/$TASK_ID" \
  -H "Authorization: Bearer $KKAI_API_KEY"
```

响应固定是外层 envelope，最小结构如下：

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

通用查询的内部状态可能是 `NOT_START`、`SUBMITTED`、`QUEUED`、`IN_PROGRESS`、
`SUCCESS`、`FAILURE` 或 `UNKNOWN`。只有 `SUCCESS` 才能下载；`UNKNOWN` 表示提交
或查询结果不确定，应该保留原任务 ID 并联系支持，不要重新创建任务。

查询不存在的任务时，本站公共接口返回 HTTP `400`（不是 `404`），例如：

```json
{
  "code": "task_not_exist",
  "message": "task_not_exist",
  "data": null
}
```

### 下载视频

```bash
curl -L "https://api.kkrich.ltd/v1/videos/$TASK_ID/content" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -o output.mp4
```

需要断点续传时，在同一个本站地址带标准 `Range` 请求头：

```bash
curl -L "https://api.kkrich.ltd/v1/videos/$TASK_ID/content" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Range: bytes=0-1048575" \
  -o first-megabyte.bin
```

`/content` 是本站稳定的下载入口，响应为视频流（通常是 `video/mp4`），支持标准
`Range` 请求并可能返回 `206 Partial Content`。它只允许任务所属用户访问，并且任务
未完成时会返回错误。下载失败时先重新查询任务状态；不要把结果临时 URL 写死或长期
缓存。

建议查询间隔保持在 20-60 秒，并将轮询截止时间设为业务允许的最长等待时间。
轮询循环只能重复 GET，不能回到提交节点。

### Python（OpenAI 兼容格式）

下面示例使用 Python 3 和 `requests`，演示提交、轮询和本站代理下载。轮询超时后
只保留任务 ID，不会再次提交任务。

```python
import os
import time
from pathlib import Path

import requests

BASE_URL = "https://api.kkrich.ltd"
TOKEN = os.environ["KKAI_API_KEY"]
HEADERS = {"Authorization": f"Bearer {TOKEN}"}

payload = {
    "model": "sd_2.5_special_720p",
    "prompt": "一列有轨电车穿过雨夜街道，镜头缓慢下降，无字幕",
    "duration": 5,
    "ratio": "16:9",
    "resolution": "720p",
    "generate_audio": True,
}
submitted_at = int(time.time())
try:
    submitted = requests.post(
        f"{BASE_URL}/v1/videos",
        headers={**HEADERS, "Content-Type": "application/json"},
        json=payload,
        timeout=30,
    )
except requests.RequestException as exc:
    raise RuntimeError(
        "submission outcome is unknown; do not resend the POST; "
        f"record model={payload['model']} and submitted_at={submitted_at}, then contact support"
    ) from exc
try:
    body = submitted.json()
except ValueError:
    body = {}
submission_uncertain = False
if not submitted.ok:
    if submitted.status_code == 502 and body.get("code") == "task_submission_unknown":
        task_id = body.get("data")
        submission_uncertain = True
    else:
        raise RuntimeError(f"submit failed ({submitted.status_code}): {body}")
else:
    task_id = body.get("id") or body.get("task_id")
if not task_id:
    raise RuntimeError(f"response did not contain a task id: {body}")

deadline = time.monotonic() + 30 * 60
while time.monotonic() < deadline:
    result = requests.get(
        f"{BASE_URL}/v1/videos/{task_id}", headers=HEADERS, timeout=30
    )
    try:
        state = result.json()
    except ValueError:
        state = {}
    if not result.ok:
        if (
            submission_uncertain
            and result.status_code == 400
            and state.get("code") == "task_not_exist"
        ):
            time.sleep(30)
            continue
        raise RuntimeError(f"query failed ({result.status_code}): {state}")
    status = state.get("status")
    if status == "completed":
        with requests.get(
            f"{BASE_URL}/v1/videos/{task_id}/content",
            headers=HEADERS,
            timeout=120,
            stream=True,
        ) as video:
            video.raise_for_status()
            with Path("output.mp4").open("wb") as output:
                for chunk in video.iter_content(chunk_size=1024 * 1024):
                    if chunk:
                        output.write(chunk)
        break
    if status in {"failed", "cancelled", "unknown"}:
        raise RuntimeError(
            f"task ended without a downloadable result; do not resubmit: "
            f"{state.get('error') or state}"
        )
    time.sleep(30)
else:
    raise TimeoutError(
        f"task status is unresolved; keep the id and do not resubmit: {task_id}"
    )
```

### Node.js 18+

Node.js 18 及以上可直接使用内置 `fetch`；下面同样只轮询本站接口：

```js
import { createWriteStream } from "node:fs";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";

const baseUrl = "https://api.kkrich.ltd";
const token = process.env.KKAI_API_KEY;
const headers = { Authorization: `Bearer ${token}` };

const payload = {
  model: "sd_2.5_special_720p",
  prompt: "一列有轨电车穿过雨夜街道，镜头缓慢下降，无字幕",
  duration: 5,
  ratio: "16:9",
  resolution: "720p",
  generate_audio: true,
};
const submittedAt = new Date().toISOString();
let submit;
try {
  submit = await fetch(`${baseUrl}/v1/video/generations`, {
    method: "POST",
    headers: { ...headers, "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
} catch (cause) {
  throw new Error(
    `submission outcome is unknown; do not resend the POST; record model=${payload.model} ` +
      `and submittedAt=${submittedAt}, then contact support`,
    { cause },
  );
}
let created;
try {
  created = await submit.json();
} catch {
  created = {};
}
let taskId;
let submissionUncertain = false;
if (!submit.ok) {
  if (submit.status === 502 && created.code === "task_submission_unknown") {
    taskId = created.data;
    submissionUncertain = true;
  } else {
    throw new Error(`submit failed (${submit.status}): ${JSON.stringify(created)}`);
  }
} else {
  taskId = created.id ?? created.task_id;
}
if (!taskId) throw new Error("submit response has no task id");

const deadline = Date.now() + 30 * 60 * 1000;
let downloaded = false;
while (Date.now() < deadline) {
  const query = await fetch(
    `${baseUrl}/v1/video/generations/${encodeURIComponent(taskId)}`,
    { headers },
  );
  let envelope;
  try {
    envelope = await query.json();
  } catch {
    envelope = {};
  }
  if (!query.ok) {
    if (
      submissionUncertain &&
      query.status === 400 &&
      envelope.code === "task_not_exist"
    ) {
      await new Promise((resolve) => setTimeout(resolve, 30_000));
      continue;
    }
    throw new Error(`query failed (${query.status}): ${JSON.stringify(envelope)}`);
  }
  const task = envelope.data ?? {};
  if (task.status === "SUCCESS") {
    const video = await fetch(`${baseUrl}/v1/videos/${encodeURIComponent(taskId)}/content`, {
      headers,
    });
    if (!video.ok) throw new Error(`download failed: ${video.status}`);
    if (!video.body) throw new Error("download response has no body");
    await pipeline(Readable.fromWeb(video.body), createWriteStream("output.mp4"));
    downloaded = true;
    break;
  }
  if (["FAILURE", "UNKNOWN"].includes(task.status)) {
    throw new Error(
      `${task.fail_reason || `task ended: ${task.status}`}; do not resubmit`,
    );
  }
  await new Promise((resolve) => setTimeout(resolve, 30_000));
}
if (!downloaded) {
  throw new Error(`task status is unresolved; keep the id and do not resubmit: ${taskId}`);
}
```

## 8. 错误格式与处理

### 下载错误

`/v1/videos/{task_id}/content` 失败时返回 OpenAI 风格错误：

```json
{
  "error": {
    "message": "Task is not completed yet",
    "type": "invalid_request_error"
  }
}
```

异步任务的提交和查询错误使用任务错误 envelope（包括 OpenAI 兼容路由的任务错误）：

```json
{
  "code": "invalid_request",
  "message": "request validation failed; see message for details",
  "data": null
}
```

`/v1/video/generations` 也使用上述格式。

严格适配器返回的参数错误经过公共网关后，外层 `code` 可能显示为
`fail_to_fetch_task`，而供应商或适配器的 `invalid_*` 细节出现在 `message` 中。
客户端应同时记录 HTTP 状态、`code` 和 `message`，不要依赖供应商错误码一定原样透传。

常见状态和处理建议：

| HTTP | code / 特征 | 处理 |
| --- | --- | --- |
| 400 | Seedance 参数校验通常为 `invalid_duration` 或 `invalid_request`；`invalid_seconds` 仅用于旧的非 Seedance 共享路径；也可能是 `task_not_exist` 或下载时任务未完成/Range 无效 | 修正字段或确认任务 ID；不要盲目重发未修改的 POST，只有明确修正参数后再由业务决定是否新建任务。 |
| 401 | Token 缺失、无效或下载未带 Token | 检查 Bearer Token 和请求头。 |
| 403 | 分组、权限或内容策略限制 | 检查 Token 分组及提示词，必要时联系支持。 |
| 404 | `/content` 的任务不存在或当前用户无权访问 | 确认任务 ID、账号和下载路径没有混用。 |
| 429 | 频率或服务负载限制 | 降低轮询频率，稍后继续 GET。 |
| 500 | 本站任务或渠道处理异常 | 保存请求时间和任务 ID，联系支持；不要盲目重试付费 POST。 |
| 502 | 提交返回 `task_submission_unknown`，或 `/content` 获取/代理视频失败 | 提交错误表示任务是否已创建无法确定；保留任务 ID 和原请求上下文，禁止自动重发 POST。下载错误则先重新查询原任务。 |
| 503 | 本站暂时无法完成策略审计或任务接入 | 保存原请求上下文，稍后重试 GET；对于结果不确定的 POST，先联系支持，不要自动重发。 |

`task_submission_unknown` 是付费任务保护信号，不等同于“肯定没有创建”。错误响应
中的 `data` 通常会带上本站任务 ID。如果客户端在网络超时后再次提交，可能产生重复
任务和重复扣费。

示例：

```json
{
  "code": "task_submission_unknown",
  "message": "task submission outcome is unknown",
  "data": "task_01JVIDEOEXAMPLE"
}
```

收到此错误时不要自动重发 POST；保留 `data` 中的任务 ID、请求时间和模型信息，先
查询原任务或联系支持。

## 9. 调用检查清单

提交前：

1. Token 属于 Seedance 视频分组，且模型名在当前可见模型列表中。
2. `duration`/`seconds` 可省略，缺失、空值、`0` 或 `1-3` 会按 4 秒处理并按 4 秒计费；负数、小数和超出模型上限的值会被拒绝。客户端仍建议显式传入 4-15（2.0）或 4-30（2.5）的整数；两者同传时，`null`、空字符串和 `0` 视为未提供，两个非空且非 `0` 的值规范化后必须相等。
3. `ratio` 使用本站支持的 7 个枚举值之一。
4. 2.5 普通名称的 `resolution` 与模型分别为 `720p`/`1080p`；视频参考名称同样要匹配分辨率。
5. 2.0 视频参考模型和 2.5 `_with_video_ref` 正式名称带一个 `reference_video`；普通 2.5 名称只使用 `reference_image`。
6. 2.5 的图片/视频素材是公开 HTTPS URL 或有效 `assetId://` 引用，并显式发送布尔值 `generate_audio`。
7. 已准备记录本次调用时间、模型和客户端追踪 ID，方便提交结果不确定时支持排查。

任务处理中：

1. 只轮询原任务 ID，不重复 POST。
2. 处理 `queued`、`processing`、`IN_PROGRESS` 等非终态。
3. 任务失败时记录安全的错误代码和 message，不记录 Token、完整提示词或素材 URL。

下载后：

1. 使用 `/v1/videos/{task_id}/content` 并继续带 Bearer Token。
2. 检查 HTTP 状态和 `Content-Type`，再保存文件。
3. 临时视频链接可能过期；需要长期保存时应在业务侧及时下载并自行管理存储。

## 10. 公共 API 与 Video Studio 的边界

本文只覆盖带 Bearer Token 的本站公共 `/v1` 接口。控制台 Video Studio 使用的
`/pg/videos`、`/api/video-studio/*` 是另一套会话、报价和资产流程，不能把它们的请求体、
认证头或路径替换到本页的公共 API 示例中。客户接入时只使用本文列出的
`https://api.kkrich.ltd/...` 地址，并以本站 `/v1/models` 和实际返回的字段为准。

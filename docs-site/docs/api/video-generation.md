---
title: "Seedance 视频生成 API"
description: "KKRICH Seedance 2.0 / 2.5 特价版模型、参数、提交、查询、下载、错误和代码示例。"
outline: [2, 3]
pageClass: kkr-seedance-page
---

# Seedance 视频生成 API

本文是 KKRICH Seedance 2.0 / 2.5 特价版的客户接口参考，覆盖模型选择、请求字段、提交响应、状态查询、视频下载和错误处理。

::: warning 只调用本站地址
API Base URL 固定为 `https://api.kkrich.ltd`。不要使用其他平台或渠道文档中的请求域名、接口路径、模型名或鉴权方式。
:::

> **2.0 协议说明**：2.0 特价版已在服务端切换 v2 适配协议，但本站客户模型名、
> 请求字段、本站接口地址和鉴权方式保持不变。客户无需修改现有请求，也不要拼接
> 任何内部或渠道地址。

> 协议核验日期：2026-08-25。模型可见性以当前 Token 调用本站 `/v1/models` 的结果为准。

## 接口总览

视频生成是异步任务：创建任务后保存公开任务 ID，轮询原任务，完成后再从本站下载。

| 用途 | 方法 | 本站地址 |
| --- | --- | --- |
| 通用格式提交 | `POST` | `https://api.kkrich.ltd/v1/video/generations` |
| 通用格式查询 | `GET` | `https://api.kkrich.ltd/v1/video/generations/{task_id}` |
| OpenAI 兼容格式提交 | `POST` | `https://api.kkrich.ltd/v1/videos` |
| OpenAI 兼容格式查询 | `GET` | `https://api.kkrich.ltd/v1/videos/{task_id}` |
| 下载视频 | `GET` | `https://api.kkrich.ltd/v1/videos/{task_id}/content` |

两套提交接口使用相同的 Seedance JSON 字段，但查询响应不同：

- 通用查询返回 `{code, message, data}`，任务状态为大写。
- OpenAI 兼容查询返回视频对象，任务状态通常为小写。
- 两套格式共用 `/v1/videos/{task_id}/content` 下载入口。

Seedance 2.0 / 2.5 特价版不支持 `/v1/videos/{video_id}/remix`。

## 认证

所有提交、查询和下载请求都必须携带有效 Bearer Token：

```http
Authorization: Bearer $KKAI_API_KEY
Content-Type: application/json
```

Token 需要能够使用 **Seedance 视频** 分组。查询和下载按用户隔离，建议始终复用提交任务时的同一个 Token。

先检查当前 Token 可见的模型：

```bash
curl "https://api.kkrich.ltd/v1/models" \
  -H "Authorization: Bearer $KKAI_API_KEY"
```

模型没有出现在本次返回结果中时，不应提交。不要把真实 Token 写入前端代码、URL 查询参数、日志、截图或代码仓库。

## 模型与能力矩阵

### Seedance 2.5 特价版

| 客户模型名 | 时长 | 分辨率 | 参考方式 |
| --- | --- | --- | --- |
| `seedance-2.5` | 4-30 秒整数 | 720p | 兼容别名；文生或单个图片参考 |
| `sd_2.5_special_720p` | 4-30 秒整数 | 720p | 文生或单个图片参考 |
| `sd_2.5_special_1080p` | 4-30 秒整数 | 1080p | 文生或单个图片参考 |
| `sd_2.5_special_720p_with_video_ref` | 4-30 秒整数 | 720p | 必须单个视频参考 |
| `sd_2.5_special_1080p_with_video_ref` | 4-30 秒整数 | 1080p | 必须单个视频参考 |

四个 `sd_2.5_special_*` 名称沿用既有 Seedance 2.0 命名规则，是本站正式客户模型名，
分别对应四个计费能力档；原有 `seedance-2.5` 仅作为 720p 兼容别名保留。上述名称都支持：

- `ratio`：`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`、`adaptive`。
- `generate_audio`：接受布尔值 `true` 或 `false`，建议显式传值。
- 普通正式名称（不带 `_with_video_ref` 后缀）使用单个 `reference_image` 或 `input_reference`。
- `_with_video_ref` 正式名称必须使用单个 `reference_video`，也可以附带一个图片参考。
- 参考素材可使用公开 HTTPS URL 或有效 `assetId://` 引用；这些素材地址不是本站 API 地址。

普通 2.5 名称不支持视频或音频参考；所有 2.5 名称都不接受图片、视频、音频数组字段。

### Seedance 2.0 特价版

所有 2.0 模型的时长都是 4-15 秒整数，并支持与 2.5 相同的 7 种 `ratio`。

| 客户模型名 | 输出分辨率 | 参考方式 |
| --- | --- | --- |
| `sd_2.0_fast_special_720p` | 720p | 文生 / 单图参考 |
| `sd_2.0_special_720p` | 720p | 文生 / 单图参考 |
| `sd_2.0_special_1080p` | 1080p | 文生 / 单图参考 |
| `sd_2.0_special_2k` | 2k | 文生 / 单图参考 |
| `sd_2.0_special_4k` | 4k | 文生 / 单图参考 |
| `sd_2.0_fast_special_720p_with_video_ref` | 720p | 必须单个视频参考；可选单图参考 |
| `sd_2.0_special_720p_with_video_ref` | 720p | 必须单个视频参考；可选单图参考 |
| `sd_2.0_special_1080p_with_video_ref` | 1080p | 必须单个视频参考；可选单图参考 |
| `sd_2.0_special_2k_with_video_ref` | 2k | 必须单个视频参考；可选单图参考 |
| `sd_2.0_special_4k_with_video_ref` | 4k | 必须单个视频参考；可选单图参考 |

2.0 分辨率已经编码在模型名中。通常可以省略 `resolution`；如果发送，必须与模型名一致。`generate_audio` 接受 `true` 或 `false`，建议显式传值。

## 请求字段

请求体必须是 JSON。

| 字段 | 类型 | 必填 | 2.0 | 2.5 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `model` | string | 是 | 支持 | 支持 | 大小写敏感的本站客户模型名。 |
| `prompt` | string | 是 | 支持 | 支持 | 去除首尾空白后不能为空；最多 8192 字节，不能包含 NUL 字符。 |
| `duration` | integer 或整数字符串 | 否 | 4-15 | 4-30 | 推荐字段；缺失、空值、`0` 或 `1-3` 按 `4` 处理并按 `4` 计费。 |
| `seconds` | integer 或整数字符串 | 否 | 4-15 | 4-30 | `duration` 的兼容字段；缺失、空值、`0` 或 `1-3` 按 `4` 处理并按 `4` 计费。 |
| `ratio` | string | 否 | 7 种 | 7 种 | 缺省为 `16:9`；枚举见模型矩阵。 |
| `resolution` | string | 否 | 由模型名决定 | 按 2.5 模型名为 `720p` 或 `1080p` | 2.0 和 2.5 都必须匹配所选模型名。 |
| `reference_image` | string | 否 | 单图参考 | 普通名称单图参考；视频参考名称可选 | 公开 HTTPS URL 或有效 `assetId://`；与 `input_reference` 二选一。 |
| `input_reference` | string | 否 | 兼容别名 | 兼容别名 | 与 `reference_image` 二选一；新接入推荐前者。 |
| `reference_video` | string | 条件 | 仅视频参考模型 | `_with_video_ref` 名称必填；普通名称禁止 | 只能给支持视频参考的模型使用。 |
| `generate_audio` | boolean | 否 | 支持 | 支持 | 显式传 `true` 或 `false`，不要传字符串。 |

`duration` 和 `seconds` 都可以省略。缺失、`null`、空字符串、`0` 和 `1-3` 会由本站
服务端规范为 `4` 秒并按 `4` 秒计费；负数、小数、无法解析的值及超过模型上限的值
返回 400（2.0 上限 15 秒，2.5 正式名称和兼容别名上限 30 秒）。两者同时传时，`null`、空字符串和 `0`
视为未提供，由另一个字段接管；两个非空且非 `0` 的值会先各自按最少 4 秒规范化，结果
必须一致。客户端仍建议显式传入 2.0 的 `4-15` 或 2.5 的 `4-30` 整数，例如 `5` 或
`"5"`，不要传 `5.0`、`"05"`。

### 提示词

提示词建议按“主体 + 动作 + 场景 + 镜头 + 声音 + 限制”组织。例如：

```text
原创雨夜街道，一列无品牌标识的有轨电车缓慢驶过，镜头从高处平稳下降，
路面有霓虹倒影和细雨声，无字幕、无 Logo、无水印。
```

`generate_audio: true` 只表示允许生成音轨。需要环境音、音效或对白时，仍应在提示词中明确描述。

### 参考素材

- 一个请求最多发送一个 `reference_image` 或 `input_reference`；不要同时发送。
- 2.0 视频参考模型和 2.5 `_with_video_ref` 正式名称必须发送一个 `reference_video`；普通 2.5 名称禁止该字段。
- 公开 URL 必须使用 HTTPS，且无需 Cookie、登录态或自定义请求头即可读取。
- 本站会拒绝私网地址、保留地址、本地路径、带用户名密码的 URL、fragment、`data:` 和 base64。
- 已由平台签发且仍有效的素材可使用 `assetId://<asset_id>`；不要自行构造资产 ID。
- 不要发送 `reference_images`、`reference_videos`、`reference_audios`、`first_image`、`last_image`、`aspect_ratio`、`seed`、`tools`、`width`、`height`、`fps`、`n`、`size` 等未列入本站协议的字段。

普通 2.5 名称只支持单图参考；2.5 `_with_video_ref` 正式名称必须使用视频参考。所有 2.5
名称都不支持音频参考或数组素材。严格适配器会拒绝不支持的字段，而不是静默忽略。

## 请求示例

### 2.5 文生视频

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.5_special_720p",
    "prompt": "原创雨夜街道，一列有轨电车缓慢驶过，镜头平稳下降，路面有霓虹倒影和细雨声，无文字、无 Logo",
    "duration": 5,
    "ratio": "16:9",
    "resolution": "720p",
    "generate_audio": true
  }'
```

### 2.5 图生视频

```bash
curl -X POST "https://api.kkrich.ltd/v1/videos" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.5_special_720p",
    "prompt": "保持主体和原有构图，让树叶随风摆动，镜头轻微推进",
    "duration": 6,
    "ratio": "1:1",
    "resolution": "720p",
    "reference_image": "https://media.your-domain.example/reference.jpg",
    "generate_audio": false
  }'
```

### 2.5 1080p 文生视频

将模型名和分辨率同时切换为 1080p；其余字段与普通 2.5 请求相同：

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
1080p 时将模型和 `resolution` 一起替换为 `sd_2.5_special_1080p_with_video_ref` 与
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
    "reference_video": "https://media.your-domain.example/reference.mp4",
    "generate_audio": true
  }'
```

### 2.0 文生视频

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

### 2.0 图生视频

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.0_special_1080p",
    "prompt": "保持人物造型和构图，人物缓慢转身看向镜头，电影感光线",
    "duration": 8,
    "ratio": "9:16",
    "reference_image": "https://media.your-domain.example/portrait.jpg",
    "generate_audio": false
  }'
```

### 2.0 视频参考

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sd_2.0_special_1080p_with_video_ref",
    "prompt": "保持主体动作节奏，改为原创电影感冷色调，无文字、无 Logo",
    "duration": 8,
    "ratio": "9:16",
    "reference_video": "https://media.your-domain.example/reference.mp4",
    "generate_audio": true
  }'
```

## 提交响应

创建成功返回视频对象。`id` 是本站公开任务 ID，必须原样保存；它用于后续查询和下载。

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

`task_id` 是兼容字段，可能缺失或在未来调整。客户代码应始终以提交响应中的 `id` 为准，不要要求 `task_id === id`。

提交成功只表示任务已被接受，不表示已经生成完成。不要因为响应中没有结果 URL 而重复提交。

## 查询任务

### 通用任务格式

```bash
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
    "progress": "100%",
    "result_url": "https://api.kkrich.ltd/v1/videos/task_01JVIDEOEXAMPLE/content",
    "fail_reason": ""
  }
}
```

| `data.status` | 含义 | 客户端动作 |
| --- | --- | --- |
| `NOT_START` / `SUBMITTED` / `QUEUED` | 已接受或排队中 | 继续查询原任务 |
| `IN_PROGRESS` | 正在生成 | 继续查询原任务 |
| `SUCCESS` | 已完成 | 调用本站 `/content` 下载 |
| `FAILURE` | 生成失败 | 记录 `fail_reason`，停止轮询 |
| `UNKNOWN` | 结果无法确定 | 保留任务 ID，停止自动操作并联系支持 |

查询不存在的任务时，通用接口返回 HTTP `400` 和 `task_not_exist`，不是 `404`。

### OpenAI 视频兼容格式

```bash
curl "https://api.kkrich.ltd/v1/videos/$TASK_ID" \
  -H "Authorization: Bearer $KKAI_API_KEY"
```

客户端重点处理以下状态：

| `status` | 含义 | 客户端动作 |
| --- | --- | --- |
| `queued` | 已提交，等待排队 | 继续查询 |
| `processing` | 正在生成 | 继续查询 |
| `completed` | 已完成 | 调用本站 `/content` 下载 |
| `failed` | 失败 | 读取 `error.message`，停止轮询 |

共享视频接口可能返回 `in_progress`、`pending`、`cancelled`、`unknown` 或未来新增值。解析器应容忍未知字段和状态；只有明确的成功状态才能下载。查询响应中的旧 `task_id` 不应覆盖提交时保存的 `id`。

## 轮询规则

- 从 20 秒间隔开始，逐步退避到最多 60 秒，避免高频查询。
- 轮询只能重复 `GET`，不能回到创建步骤。
- 设置业务可接受的总等待时间；超时后保存任务 ID，稍后继续查询原任务。
- `FAILURE`、`failed` 或 `UNKNOWN` 出现后停止自动轮询。
- 网络错误只重试查询，不要自动重发创建任务的 `POST`。

本站公共接口没有可依赖的幂等键。创建请求超时并不等于任务肯定未创建，自动重发可能造成重复任务和重复扣费。

## 下载视频

```bash
curl -L "https://api.kkrich.ltd/v1/videos/$TASK_ID/content" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -o output.mp4
```

下载入口要求任务已经完成，并且请求用户拥有该任务。需要断点读取时可以发送单段 `Range`：

```bash
curl -L "https://api.kkrich.ltd/v1/videos/$TASK_ID/content" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Range: bytes=0-1048575" \
  -o first-megabyte.bin
```

成功时通常返回 `video/mp4`；Range 请求可能返回 `206 Partial Content`。不要把 Token 拼进 URL，也不要长期缓存可能过期的临时结果地址。

## Python 完整示例

下面的脚本只提交一次，并始终通过本站查询和下载。需要 Python 3 和 `requests`。

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
    "prompt": "原创雨夜街道，有轨电车缓慢驶过，镜头平稳下降，无文字、无 Logo",
    "duration": 5,
    "ratio": "16:9",
    "resolution": "720p",
    "generate_audio": True,
}

try:
    response = requests.post(
        f"{BASE_URL}/v1/videos",
        headers={**HEADERS, "Content-Type": "application/json"},
        json=payload,
        timeout=30,
    )
except requests.RequestException as exc:
    raise RuntimeError(
        "创建请求结果未知；不要重新发送 POST，请保存请求时间和模型并联系支持"
    ) from exc

try:
    created = response.json()
except ValueError:
    created = {}

submission_unknown = False
if response.ok:
    task_id = created.get("id")
elif response.status_code == 502 and created.get("code") == "task_submission_unknown":
    task_id = created.get("data")
    submission_unknown = True
else:
    raise RuntimeError(f"创建失败 ({response.status_code}): {created}")

if not task_id:
    raise RuntimeError(f"响应没有本站任务 ID: {created}")

print(f"task_id={task_id}")
deadline = time.monotonic() + 30 * 60

while time.monotonic() < deadline:
    result = requests.get(
        f"{BASE_URL}/v1/videos/{task_id}",
        headers=HEADERS,
        timeout=30,
    )
    try:
        task = result.json()
    except ValueError:
        task = {}

    if not result.ok:
        if (
            submission_unknown
            and result.status_code == 400
            and task.get("code") == "task_not_exist"
        ):
            time.sleep(30)
            continue
        raise RuntimeError(f"查询失败 ({result.status_code}): {task}")

    status = task.get("status")
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
        print("saved output.mp4")
        break

    if status in {"failed", "cancelled", "unknown"}:
        raise RuntimeError(f"任务未成功: {task.get('error') or task}")

    time.sleep(30)
else:
    raise TimeoutError(f"等待超时；保留任务 ID，稍后继续查询: {task_id}")
```

## Node.js 18+ 完整示例

Node.js 18 及以上可以直接使用内置 `fetch`。不要在浏览器前端运行包含真实 Token 的代码。

```js
import { createWriteStream } from "node:fs";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";

const baseUrl = "https://api.kkrich.ltd";
const token = process.env.KKAI_API_KEY;
if (!token) throw new Error("KKAI_API_KEY is required");

const headers = { Authorization: `Bearer ${token}` };
const payload = {
  model: "sd_2.5_special_720p",
  prompt: "原创雨夜街道，有轨电车缓慢驶过，镜头平稳下降，无文字、无 Logo",
  duration: 5,
  ratio: "16:9",
  resolution: "720p",
  generate_audio: true,
};

let response;
try {
  response = await fetch(`${baseUrl}/v1/video/generations`, {
    method: "POST",
    headers: { ...headers, "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
} catch (cause) {
  throw new Error(
    "创建请求结果未知；不要重新发送 POST，请保存请求时间和模型并联系支持",
    { cause },
  );
}

let created;
try {
  created = await response.json();
} catch {
  created = {};
}

let taskId;
let submissionUnknown = false;
if (response.ok) {
  taskId = created.id;
} else if (response.status === 502 && created.code === "task_submission_unknown") {
  taskId = created.data;
  submissionUnknown = true;
} else {
  throw new Error(`创建失败 (${response.status}): ${JSON.stringify(created)}`);
}
if (!taskId) throw new Error("响应没有本站任务 ID");
console.log(`task_id=${taskId}`);

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
      submissionUnknown &&
      query.status === 400 &&
      envelope.code === "task_not_exist"
    ) {
      await new Promise((resolve) => setTimeout(resolve, 30_000));
      continue;
    }
    throw new Error(`查询失败 (${query.status}): ${JSON.stringify(envelope)}`);
  }

  const task = envelope.data ?? {};
  if (task.status === "SUCCESS") {
    const video = await fetch(
      `${baseUrl}/v1/videos/${encodeURIComponent(taskId)}/content`,
      { headers },
    );
    if (!video.ok || !video.body) {
      throw new Error(`下载失败: ${video.status}`);
    }
    await pipeline(Readable.fromWeb(video.body), createWriteStream("output.mp4"));
    downloaded = true;
    console.log("saved output.mp4");
    break;
  }

  if (["FAILURE", "UNKNOWN"].includes(task.status)) {
    throw new Error(task.fail_reason || `任务未成功: ${task.status}`);
  }

  await new Promise((resolve) => setTimeout(resolve, 30_000));
}

if (!downloaded) {
  throw new Error(`等待超时；保留任务 ID，稍后继续查询: ${taskId}`);
}
```

## 错误响应

任务提交和查询通常返回任务错误 envelope：

```json
{
  "code": "invalid_request",
  "message": "request validation failed",
  "data": null
}
```

认证、限速或其他网关层错误也可能使用 OpenAI 风格：

```json
{
  "error": {
    "message": "error details",
    "type": "invalid_request_error"
  }
}
```

客户端应同时记录 HTTP 状态、`code`、`message` 和 `error.message`，不要只匹配某一种中文文案。

| HTTP | 适用接口 | 常见原因 | 处理 |
| --- | --- | --- | --- |
| `400` | 提交 / 查询 / 下载 | JSON 或字段无效、时长越界、任务不存在、任务未完成、Range 无效 | 修正确定的参数问题；查询原任务；不要盲目重发原 POST。 |
| `401` | 全部 | Token 缺失或无效 | 检查 `Authorization: Bearer ...`。 |
| `403` | 全部 | Token 分组、账号权限、内容策略或下载安全策略限制 | 检查分组和输入，必要时联系支持。 |
| `404` | 下载 | 任务不存在、用户不匹配或内容不可通过该入口提供 | 检查公开任务 ID和请求用户。 |
| `429` | 提交 / 查询 | 请求频率或当前分组负载限制 | 降低频率；只重试查询，创建请求需确认结果后再决定。 |
| `500` | 提交 / 查询 / 下载 | 本站任务、计费、数据库、渠道或响应处理异常 | 保存任务 ID 和请求时间，联系支持。 |
| `502` | 提交 / 下载 | 创建结果不确定，或视频内容获取失败 | 创建时禁止自动重发 POST；下载时先重新查询原任务。 |
| `503` | 提交 / 查询 | 暂时无法完成策略审计或任务接入 | 保存上下文并稍后查询；不要自动重发结果未知的 POST。 |

`/content` 没有承诺返回 `429` 或 `503`；下载错误应按实际 HTTP 状态处理。

### `task_submission_unknown`

这表示本站无法确认创建请求最终是否被接受，不表示任务肯定没有创建。响应中的 `data` 可能带本站任务 ID：

```json
{
  "code": "task_submission_unknown",
  "message": "task submission outcome is unknown",
  "data": "task_01JVIDEOEXAMPLE"
}
```

收到后应保存 `data`、请求时间、模型和本地业务单号，查询原任务或联系支持。禁止自动重发创建请求。

## 上线前检查

1. Base URL 是 `https://api.kkrich.ltd`，所有接口路径来自本文。
2. Token 可以通过本站 `/v1/models` 看到所选客户模型名（四个 2.5 正式名称均以此结果为准）。
3. 2.0 时长是 4-15 秒，2.5 正式名称和兼容别名的时长是 4-30 秒，且为整数。
4. `ratio` 是 7 个支持值之一；2.5 分辨率必须与模型名匹配（`720p` 或 `1080p`）。
5. `generate_audio` 是布尔值，并由客户端显式传入。
6. 普通 2.5 名称只发送一个图片引用；`_with_video_ref` 名称必须发送一个视频引用；所有 2.5 名称都不发送音频或数组素材。
7. 客户端持久化提交响应的 `id`，轮询只执行 `GET`。
8. 完成后从本站 `/v1/videos/{task_id}/content` 下载，并继续携带 Token。

价格和倍率不在本页维护，以本站控制台实时配置和实际用量记录为准。遇到失败可继续查看 [Seedance 视频错误排查](/support/seedance-video)。

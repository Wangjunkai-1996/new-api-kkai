---
title: "Seedance 视频错误排查"
description: "排查 KKRICH Seedance 2.0 / 2.5 特价版的认证、参数、素材、任务状态和下载错误。"
outline: [2, 3]
pageClass: kkr-seedance-page
---

# Seedance 视频错误排查

先保存任务 ID、HTTP 状态和完整错误响应。只要创建请求已经发出，网络超时或错误响应都不代表任务肯定没有创建；排查期间不要自动重新提交付费任务。

::: warning 只使用本站接口排查
提交、查询和下载都必须请求 `https://api.kkrich.ltd`。不要用其他平台或渠道的地址测试本站 Token。
:::

## 快速定位

| 现象 | 常见原因 | 先做什么 |
| --- | --- | --- |
| `401` | Token 缺失、失效或格式错误 | 检查 Bearer 请求头 |
| `403` | Token 分组、账号权限、内容策略或下载安全限制 | 检查 **Seedance 视频** 分组和输入内容 |
| `400` | 字段、类型、时长、模型或任务状态不符合约束 | 对照最小请求逐项删除非必要字段 |
| `404` | 下载任务不存在、用户不匹配或内容不可用 | 检查公开 `id`、Token 所属用户和 `/content` 路径 |
| `429` | 提交或查询过于频繁 | 降低频率，不要并发轮询 |
| `500` | 本站任务、计费、数据库、渠道或响应处理异常 | 保存任务 ID 和请求时间，联系支持 |
| `502` | 提交结果不确定，或视频下载代理失败 | 禁止自动重发 POST；先查询原任务 |
| `503` | 策略审计或任务接入暂时不可用 | 保存上下文，稍后查询原任务 |
| 一直排队 | 任务仍在等待，或客户端轮询逻辑错误 | 每 20-60 秒查询同一个任务 ID |
| 完成但下载失败 | 下载没带 Token、任务用户不匹配或结果暂不可取 | 使用同一 Token 请求本站 `/content` |

## 先做三项基础检查

### 1. 验证 Token 和模型

```bash
curl "https://api.kkrich.ltd/v1/models" \
  -H "Authorization: Bearer $KKAI_API_KEY"
```

确认：

- Token 没有多余空格、换行或引号。
- 请求头格式是 `Authorization: Bearer <Token>`。
- Token 可以使用 **Seedance 视频** 分组。
- 返回的模型列表中确实存在本次请求的完整模型名。

### 2. 确认保存的是公开 `id`

创建响应中的 `id` 是客户后续查询和下载应使用的本站任务 ID：

```json
{
  "id": "task_01JVIDEOEXAMPLE",
  "status": "queued"
}
```

不要从日志、其他字段或其他平台复制任务 ID。查询响应中的旧 `task_id` 可能缺失，不应覆盖创建时保存的 `id`。

### 3. 用最小请求复现

先移除所有参考素材和可选字段，只保留本站协议中的最小 2.5 请求：

```bash
curl -X POST "https://api.kkrich.ltd/v1/video/generations" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2.5",
    "prompt": "原创海边日出，镜头缓慢前移，无文字、无 Logo",
    "duration": 4,
    "ratio": "16:9",
    "resolution": "720p",
    "generate_audio": false
  }'
```

每次只增加一个字段。这样可以定位是基础权限、模型、素材还是某个可选参数导致失败。真实创建会生成新任务，确认后再执行，不要用循环脚本反复试错。

## `400` 参数错误

### 模型名错误

常见情况：

- 使用了其他平台的模型名。
- 大小写、标点或分辨率后缀不一致。
- Token 当前看不到该模型。

处理：从本站 `/v1/models` 响应复制模型名。当前 2.5 客户模型名是 `seedance-2.5`；2.0 使用 [完整模型矩阵](/api/video-generation#seedance-2-0-特价版) 中的名称。

### 时长错误

| 版本 | 有效时长 |
| --- | --- |
| Seedance 2.0 | 4-15 秒整数 |
| Seedance 2.5 | 4-30 秒整数 |

至少发送 `duration` 或 `seconds` 一个。两者同时发送时必须相等。使用 `5` 或 `"5"`，不要使用小数、负数、`"05"` 或超出范围的值。

### 画幅或分辨率错误

两代模型支持 `16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`、`adaptive`。

- 字段名是 `ratio`，不是其他平台常见的画幅字段名。
- 2.5 当前只接受 `resolution: "720p"`。
- 2.0 分辨率由模型名决定；发送 `resolution` 时必须匹配模型名。

### `generate_audio` 类型错误

必须发送 JSON 布尔值：

```json
{
  "generate_audio": true
}
```

不要发送 `"true"`、`1`、`"1"`。2.0 和 2.5 都接受 `true` 或 `false`；为避免依赖缺省行为，建议显式发送。

### 2.5 参考素材错误

2.5 只支持一个图片参考：

```json
{
  "reference_image": "https://media.your-domain.example/reference.jpg"
}
```

也可以使用平台已经签发且仍有效的 `assetId://<asset_id>`。不要自行编造资产 ID。

以下情况会失败：

- 同时发送 `reference_image` 和 `input_reference`。
- 发送 `reference_video` 或音频参考。
- 发送 `reference_images`、`reference_videos`、`reference_audios` 等数组字段。
- 发送 `first_image`、`last_image`、`tools`、`seed` 等未支持字段。
- 素材 URL 需要登录、Cookie、Referer 或自定义请求头才能访问。
- URL 指向私网、本机、保留地址，或使用 HTTP、`data:`、base64、本地路径。

### 2.0 视频参考错误

只有名称以 `_with_video_ref` 结尾的 2.0 模型接受 `reference_video`，并且该字段必填。普通 2.0 模型不能发送视频参考。

```json
{
  "model": "sd_2.0_special_1080p_with_video_ref",
  "reference_video": "https://media.your-domain.example/reference.mp4"
}
```

视频必须是无需登录即可读取的公开 HTTPS 直链，或有效的 `assetId://` 引用。

### 未知字段

Seedance 特价版使用严格字段校验。不要假设服务端会忽略拼错或多余的字段。出现参数错误时，把请求缩减为 [最小示例](/api/video-generation#请求示例)，再一次增加一个字段。

## `401` 认证失败

正确格式：

```http
Authorization: Bearer $KKAI_API_KEY
```

检查以下问题：

- 请求头是否真的发出，而不是只在代码里定义。
- `Bearer` 后是否有一个空格。
- Token 是否复制完整，是否包含换行。
- 查询和下载是否也携带 Token。
- 反向代理或 SDK 是否删除了 `Authorization` 请求头。

不要把 Token 改放到 URL 查询参数中。

## `403` 权限或策略限制

`403` 可能来自账号、Token 分组、模型权限、内容策略或下载安全检查。

1. 调用本站 `/v1/models`，确认 Token 可见所选模型。
2. 确认 Token 使用 **Seedance 视频** 分组，而不是聊天或图片分组。
3. 移除私人地址、带凭据 URL 和无法公开读取的素材。
4. 将提示词改成原创内容，避免品牌 Logo、车牌、文字、知名角色或受版权保护的元素。
5. 保存原始 HTTP 状态和错误消息后联系支持。

## `404` 查询或下载不到任务

通用查询中，任务不存在通常返回 HTTP `400` 和 `task_not_exist`。`404` 更常见于下载入口。

正确查询：

```bash
curl "https://api.kkrich.ltd/v1/video/generations/$TASK_ID" \
  -H "Authorization: Bearer $KKAI_API_KEY"
```

正确下载：

```bash
curl -L "https://api.kkrich.ltd/v1/videos/$TASK_ID/content" \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -o output.mp4
```

确认任务 ID 来自创建响应的 `id`，并且当前 Token 与任务属于同一个用户。不要混用其他账号、测试环境或旧任务的 ID。

## `429` 请求过于频繁

`429` 适用于任务提交或查询，通常表示频率或当前分组负载限制。

- 一个任务只保留一个轮询器。
- 从 20 秒间隔开始，逐步退避到最多 60 秒。
- 为网络错误加入随机抖动，避免多个实例同时重试。
- 查询可重试；创建请求只有在明确未创建且业务允许时，才由业务侧决定是否新建任务。

本站 `/content` 下载入口没有承诺返回 `429`。下载失败时以实际 HTTP 状态为准。

## `500` / `502` / `503`

### `task_submission_unknown`

创建接口可能返回 HTTP `502`：

```json
{
  "code": "task_submission_unknown",
  "message": "task submission outcome is unknown",
  "data": "task_01JVIDEOEXAMPLE"
}
```

这不是“确定失败”。正确处理：

1. 保存 `data` 中的任务 ID、请求时间、模型和本地业务单号。
2. 查询这个任务 ID，不要生成新的 ID。
3. 任务仍无法确认时联系支持。
4. 禁止 SDK、队列或网关自动重发原 `POST`。

### 其他服务端错误

- `500`：保存任务 ID、请求时间和错误响应，联系支持。
- 下载 `502`：先重新查询原任务是否仍为成功，再重试同一个本站 `/content` 地址。
- `503`：可能暂时无法完成策略审计或任务接入；保存上下文，稍后查询，不要自动重发结果未知的创建请求。

## 一直 `queued` 或 `processing`

任务生成耗时会受时长、分辨率和排队情况影响。只要状态不是终态，就继续查询同一个任务：

- 通用格式非终态：`NOT_START`、`SUBMITTED`、`QUEUED`、`IN_PROGRESS`。
- OpenAI 兼容格式非终态：`queued`、`processing`，以及共享接口可能出现的 `pending`、`in_progress`。

客户端应设置总等待时间。达到本地超时后，保存任务 ID 并停止当前等待；稍后仍然查询原任务，不能重新创建。

## 任务 `failed` 或 `FAILURE`

记录：

- HTTP 状态码。
- 提交响应中的公开任务 `id`。
- `error.code`、`error.message` 或 `fail_reason`。
- 模型名、请求时间和时区。
- 是否使用参考素材及素材类型。

常见原因包括参数、素材不可读、内容审核或生成失败。只有在明确修正原因后，才能由业务侧决定是否创建新任务；失败任务不会自动恢复。

## 视频没有声音

确认请求显式发送了：

```json
{
  "generate_audio": true,
  "prompt": "原创海边日出，镜头缓慢前移，包含海浪、海风和远处海鸟的环境音，无文字、无 Logo"
}
```

`generate_audio: true` 只允许生成音轨，不保证在没有声音描述时产生预期声音。已经完成的静音任务不会自动补音，重新生成会创建新任务。

## 下载失败

### 任务尚未完成

如果 `/content` 返回 `400` 并提示任务未完成，先回到查询接口等待 `SUCCESS` 或 `completed`，不要重复提交。

### Token 或用户不匹配

下载仍需要认证。使用提交任务的同一用户 Token；未携带 Token 会返回认证错误，其他用户的 Token 无法读取该任务。

### Range 请求错误

本站只接受标准单段 Range，例如：

```http
Range: bytes=0-1048575
```

不要发送多个 Range、超长值或非 `bytes` 单位。第一次排错时先移除 Range，确认完整下载可用。

## 联系支持前

请准备以下信息：

- 公开任务 `id`。
- 请求时间和时区。
- 本站 API 路径和 HTTP 方法。
- HTTP 状态码。
- `code`、`message`、`error.message` 或 `fail_reason`。
- 模型名称、时长、画幅和分辨率。
- 是否使用参考图片或视频。
- 控制台对应的任务或用量记录截图。

不要发送完整 Token、`Authorization` 请求头、私人素材原地址或账号密码。完整协议见 [Seedance 视频生成 API](/api/video-generation)。

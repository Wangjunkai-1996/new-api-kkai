---
title: "图片生成 API"
description: "使用 KKRICH image 分组 Token 调用图片生成与编辑接口，并保存返回的图片。"
outline: [2, 3]
---

# 图片生成 API

用本站的 OpenAI 兼容图片接口生成图片。第一次接入建议先用非流式请求生成一张，再按模型能力添加尺寸、质量或图片编辑参数。使用图形界面可看 [Cherry Studio 生图教程](/apps/image2)；希望让 Codex 自动调用接口，可看 [Codex 生图教程](/apps/codex-image)。

## 准备 Token 和模型

1. 在 [令牌管理](https://api.kkrich.ltd/keys/)创建一个专用 Token，分组选择 **image 图片**。复制后妥善保管，不要写入代码仓库、截图或聊天记录。
2. 在当前终端设置 `KKRICH_IMAGE_API_KEY` 环境变量。下面的命令会读取它，不需要把密钥直接写进命令。其他终端窗口不会自动继承这个变量。
3. 用该 Token 查询本站当前可见的模型，从结果的 `data[].id` 选择支持图片生成的模型，并核对控制台中的模型能力和价格。模型列表可能包含其他能力的模型；能查到名称不代表它支持图片接口。

```bash
read -r -s 'KKRICH_IMAGE_API_KEY?粘贴 image 分组 Token：' && export KKRICH_IMAGE_API_KEY && echo
curl --fail-with-body -sS "https://api.kkrich.ltd/v1/models" \
  -H "Authorization: Bearer $KKRICH_IMAGE_API_KEY"
```

上面的 `read` 写法适用于 macOS 默认的 zsh。其他 shell 可按其语法设置相同环境变量。选好模型后设置：

```bash
export KKRICH_IMAGE_MODEL='从模型列表复制的图片模型 ID'
```

API Base URL 为 `https://api.kkrich.ltd/v1`。请求均使用 `Authorization: Bearer <Token>`；不要把 Image Studio 控制台使用的登录态接口当作客户 API。

## 生成第一张图片

`POST /v1/images/generations` 接收 JSON。下面的命令提交**一次**付费请求，并把完整 JSON 响应保存在当前目录的 `image-response.json`。先用 `n: 1`，其余模型专属参数暂不传。

```bash
curl --fail-with-body -sS --max-time 300 \
  "https://api.kkrich.ltd/v1/images/generations" \
  -H "Authorization: Bearer $KKRICH_IMAGE_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"$KKRICH_IMAGE_MODEL\",\"prompt\":\"一座雨后的城市花园，柔和晨光，画面干净，无文字\",\"n\":1}" \
  -o image-response.json
```

正常的非流式结果采用 `data` 数组。每个元素通常包含 `b64_json` **或** `url`，也可能有 `revised_prompt`；不要假设返回的一定是可直接下载的 URL。响应中的其他字段以实际模型为准。

```json
{
  "created": 1760000000,
  "data": [{ "b64_json": "<较长的 Base64 字符串>" }]
}
```

上面是结构示意，不是真实生成结果。部分模型返回 `{ "data": [{ "url": "https://..." }] }`。

### 保存图片

以下 Python 3 标准库示例读取刚保存的响应，只保存 `data[0]`。如果有多张图，遍历 `data` 并分别命名文件。URL 分支会直接下载图片，**不会**向图片 URL 发送本站 Token；拿到 URL 后请尽快保存，链接可能过期。

```bash
python3 - <<'PY'
import base64
import json
from pathlib import Path
from urllib.parse import urlsplit
from urllib.request import urlopen

result = json.loads(Path("image-response.json").read_text())
image = result["data"][0]
if image.get("b64_json"):
    content = base64.b64decode(image["b64_json"], validate=True)
elif image.get("url"):
    url = image["url"]
    if urlsplit(url).scheme != "https":
        raise ValueError("图片 URL 必须使用 HTTPS")
    with urlopen(url, timeout=60) as response:
        content = response.read()
else:
    raise ValueError("data[0] 没有 b64_json 或 url")

if content.startswith(b"\x89PNG\r\n\x1a\n"):
    extension = ".png"
elif content.startswith(b"\xff\xd8\xff"):
    extension = ".jpg"
elif content.startswith(b"RIFF") and content[8:12] == b"WEBP":
    extension = ".webp"
else:
    raise ValueError("无法识别返回的图片格式")
path = Path("generated" + extension)
path.write_bytes(content)
print(path.resolve())
PY
```

## 参数与能力边界

| 字段 | 用途 | 首次请求建议 |
| --- | --- | --- |
| `model` | 本站模型 ID，大小写敏感。 | 必填；从当前 Token 的模型列表和控制台选择图片模型。 |
| `prompt` | 图片内容描述。 | 必填；写清主体、场景、风格和不希望出现的元素。 |
| `n` | 期望生成数量，影响费用。 | 先用 `1`；模型可能有更低上限。 |
| `size` | 输出尺寸。 | 先省略；只填所选模型支持的值，用字母 `x` 而非乘号 `×`。 |
| `quality` | 输出质量档位。 | 先省略；可选值因模型而异。 |
| `response_format` | 请求 `url` 或 `b64_json` 形式。 | 先省略并同时处理两类结果；部分模型不支持指定。 |

本站接入不同图片模型，支持的尺寸、质量、格式、编辑能力和价格会不同。不要把一个模型的参数直接套用到另一个模型；不支持的参数可能被拒绝。确认当前控制台说明后再添加参数，生成数量也应从 `1` 开始。首次接入不建议启用流式响应，流式结果需要另写事件解析逻辑。

## 图片编辑（进阶）

支持图片编辑的模型可调用 `POST /v1/images/edits`，使用 `multipart/form-data` 上传本地图片。先在控制台确认所选模型确实支持编辑；只支持生成的模型不能因为出现在模型列表里就用于编辑。下面的 `reference.png` 是你自己准备的本地文件。

```bash
curl --fail-with-body -sS --max-time 300 \
  "https://api.kkrich.ltd/v1/images/edits" \
  -H "Authorization: Bearer $KKRICH_IMAGE_API_KEY" \
  -F "model=$KKRICH_IMAGE_MODEL" \
  -F "image=@./reference.png" \
  -F "prompt=保留原图主体，把背景改成清晨的花园" \
  -F "n=1" \
  -o image-response.json
```

编辑结果也按上面的 `data[].b64_json` 或 `data[].url` 保存。不要在 multipart 请求中手动设置 `Content-Type: application/json`；cURL 会生成正确的上传边界。蒙版、多张参考图等进阶能力以所选模型实际支持情况为准。

## 错误、计费与重试

| 情况 | 先检查 |
| --- | --- |
| `401` / Token 无效 | Bearer 头、Token 内容、是否使用 image 分组。 |
| `403` / 模型不可用 | 当前 Token 的分组、模型权限、控制台可用状态。 |
| `400` / 参数无效 | 模型名、必填字段、`n`、尺寸与质量是否匹配该模型。 |
| `429` / 请求受限 | 余额、额度、并发或频率限制；按响应信息处理。 |
| 网络超时或空响应 | 先到 [使用日志](https://api.kkrich.ltd/usage-logs/common)核对请求时间、模型、状态和消耗。 |

生图请求可能在客户端超时后仍完成并产生费用。**结果不明时不要自动重试**；先查看使用日志，再决定是否重新提交。每次重新提交都可能再次生成和计费。实际价格与消耗以控制台和使用日志为准，别用示例中的模型、尺寸或响应结构推断价格。

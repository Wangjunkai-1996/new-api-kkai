---
title: "用 Codex 生图"
description: "通过独立 image Token、本地脚本和 Codex Skill 调用 KKRICH 图片生成 API，并将图片保存到本地。"
outline: [2, 3]
---

# 用 Codex 生图

想在 Codex 中说一句需求就得到本地图片，可以让 Codex 运行一个调用 KKRICH 图片 API 的脚本，再用 Skill 固定这个工作流程。**把 KKRICH 配成 Codex 的推理模型**与**让 Codex 调用 KKRICH 生图接口**是两件事；本页只设置后者，不改动 Codex 当前的对话模型。

下面示例适用于 macOS、Linux 和 Windows WSL。它只使用 Python 3 标准库，不需要额外安装 SDK。直接调用接口的字段与响应格式见[图片生成 API](/api/image-generation)；习惯图形界面可看 [Cherry Studio 生图](/apps/image2)。

## 1. 准备 Token 和模型

在 KKRICH [令牌管理](https://api.kkrich.ltd/keys/)创建一个专供生图的 Token，分组选择 **image 图片**。不要复用 Codex 对话模型所用的其他分组 Token，也不要将 Token 写入 `SKILL.md`、脚本、项目文件或聊天记录。

先在控制台确认可用的图片模型与价格。也可以用该 Token 查询当前可见模型：

```bash
printf 'Image Token: '
read -rs KKRICH_IMAGE_API_KEY
printf '\n'
export KKRICH_IMAGE_API_KEY

curl --fail-with-body https://api.kkrich.ltd/v1/models \
  -H "Authorization: Bearer $KKRICH_IMAGE_API_KEY"
```

`/v1/models` 显示的是当前 Token 可见的模型，不能仅凭模型名字判断它一定支持生图。请结合控制台的图片模型说明，记下确认可用的模型 ID。

Codex CLI 可从设置了上述环境变量的终端启动。若使用桌面 App，它通常不会继承另一个终端临时设置的变量。可以选择把 Token 存在当前用户的私有文件中，供下方脚本读取；这个文件在项目目录之外，不要同步或分享：

```bash
umask 077
mkdir -p ~/.config/kkrich
printf '%s' "$KKRICH_IMAGE_API_KEY" > ~/.config/kkrich/image-token
chmod 600 ~/.config/kkrich/image-token
```

这会在本机以明文保存 Token。只使用 CLI 且能继承环境变量时，可跳过这一步。

## 2. 添加生图脚本

创建 `~/.agents/skills/kkrich-image/scripts/generate.py`，粘贴以下完整内容。若只想在某个项目中使用，把 `~/.agents/skills` 换成该项目的 `.agents/skills`，后续命令的脚本路径也相应更改。

```bash
mkdir -p ~/.agents/skills/kkrich-image/scripts
```

```python
#!/usr/bin/env python3
"""Generate one image through KKRICH and save it without overwriting a file."""
import argparse
import base64
import binascii
import json
import os
from pathlib import Path
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import Request, urlopen

API_URL = "https://api.kkrich.ltd/v1/images/generations"
MAX_RESPONSE = 80 * 1024 * 1024


def read_response(request):
    with urlopen(request, timeout=300) as response:
        data = response.read(MAX_RESPONSE + 1)
    if len(data) > MAX_RESPONSE:
        raise ValueError("response exceeds 80 MB")
    return data


def extension(data):
    if data.startswith(b"\x89PNG\r\n\x1a\n"):
        return ".png"
    if data.startswith(b"\xff\xd8\xff"):
        return ".jpg"
    if data[:4] == b"RIFF" and data[8:12] == b"WEBP":
        return ".webp"
    raise ValueError("unsupported image format")


def main():
    parser = argparse.ArgumentParser(description="Generate one KKRICH image")
    parser.add_argument("prompt")
    parser.add_argument("--model", required=True)
    parser.add_argument("--output", required=True, help="output path without extension")
    args = parser.parse_args()

    token = os.environ.get("KKRICH_IMAGE_API_KEY")
    if not token:
        token_file = Path.home() / ".config/kkrich/image-token"
        if token_file.exists():
            if token_file.stat().st_mode & 0o077:
                parser.error("private Token file must have mode 600")
            token = token_file.read_text().strip()
    if not token:
        parser.error("set KKRICH_IMAGE_API_KEY or create the private Token file")
    if not args.prompt.strip():
        parser.error("prompt cannot be empty")

    output_base = Path(args.output).expanduser()
    if any(output_base.with_suffix(suffix).exists() for suffix in (".png", ".jpg", ".webp")):
        parser.error("output file already exists; choose a new name before generating")
    output_base.parent.mkdir(parents=True, exist_ok=True)

    payload = json.dumps({"model": args.model, "prompt": args.prompt, "n": 1}).encode()
    request = Request(API_URL, data=payload, headers={
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
    })
    result = json.loads(read_response(request))  # Exactly one paid POST; no retry.
    item = result["data"][0]
    if item.get("b64_json"):
        image = base64.b64decode(item["b64_json"], validate=True)
    elif item.get("url"):
        url = item["url"]
        location = urlsplit(url)
        if location.scheme != "https" or not location.hostname or location.username or location.password:
            raise ValueError("image URL is not a valid HTTPS URL")
        image = read_response(Request(url))  # Never forward the API Token to the image URL.
    else:
        raise ValueError("response has no image")

    output = output_base.with_suffix(extension(image))
    with output.open("xb") as file:
        file.write(image)
    print(output.resolve())


if __name__ == "__main__":
    try:
        main()
    except HTTPError as error:
        sys.exit(f"HTTP {error.code}; check Token, model, quota, and usage logs")
    except (URLError, ValueError, TypeError, KeyError, IndexError, OSError, binascii.Error) as error:
        sys.exit(f"Image not saved ({type(error).__name__}); check usage logs before retrying")
```

脚本不打印 Token 或服务器返回的原始错误内容；如果超时、断线或保存失败，**不要立刻重跑**。生成请求可能已经计费，先到 [使用日志](/guide/usage-log)确认这一笔请求，再决定是否重新提交。脚本不会覆盖同名文件，也不会自动重试。

## 3. 先直接验证

使用第 1 步确认的图片模型。脚本从当前终端的环境变量或上述私有文件读取 Token：

```bash
export KKRICH_IMAGE_MODEL='从模型列表复制的图片模型 ID'
python3 ~/.agents/skills/kkrich-image/scripts/generate.py \
  --model "$KKRICH_IMAGE_MODEL" \
  --output ./images/rainy-street \
  '雨后的城市街角，柔和自然光，画面干净，无文字，无 Logo'
```

成功时，脚本只输出新图片的绝对路径，扩展名按返回内容自动确定为 `.png`、`.jpg` 或 `.webp`。这是一次付费生成请求；图片 URL 下载只是获取已生成的结果。目标文件已存在时，脚本会在提交前要求更换输出名。

## 4. 添加 Codex Skill

在同一目录创建 `~/.agents/skills/kkrich-image/SKILL.md`，粘贴以下内容。`name` 和 `description` 让 Codex 能识别何时使用它；脚本才是真正执行 API 请求的工具。

```markdown
---
name: kkrich-image
description: Generate image files through KKRICH's image API when the user asks Codex for KKRICH or KKAI image generation.
---

# KKRICH image generation

Resolve `scripts/generate.py` relative to this `SKILL.md`, then run it with the user's image prompt, a confirmed KKRICH image model, and a new output path. The script reads `KKRICH_IMAGE_API_KEY` from the environment or the user's private `~/.config/kkrich/image-token` file and prints the saved image path.

- Never put the Token in a prompt, project file, or response. If neither credential source is available, ask the user to set the environment variable or private file themselves.
- Use a model available to the image Token; ask the user when the model is unclear. Do not treat a Codex conversation model as an image model.
- Each script run sends one paid generation request. Run it only for the user's image request, never retry automatically, and never overwrite an existing result.
- On timeout or uncertain failure, direct the user to KKRICH usage logs before another attempt. Return the saved file path when successful.
```

Skill 可放在用户级 `~/.agents/skills/kkrich-image/`，或项目级 `.agents/skills/kkrich-image/`。重新打开 Codex 会话后，可以显式调用：

> $kkrich-image 用 KKRICH 生一张雨后街角的横幅图，模型用我已确认的 `<图片模型 ID>`，保存到当前项目的 `images/rainy-street`；完成后告诉我实际文件路径。

Codex 运行脚本时必须能读到环境变量或上述私有文件。桌面 App 不会自动继承你在另一个终端临时设置的变量；此时使用私有文件即可。不要让 Codex 把 Token 回显到对话里。

## 常见问题

**Codex 只回复提示词，没有图片文件？** 检查 Skill 是否在 Codex 可发现的目录内、会话是否重新打开，以及 Codex 是否允许运行本地脚本。Skill 是工作流程说明，不会仅凭安装就新增内置生图 API。

**401 或 403？** 检查 Token 是否有效、是否为 `image 图片`分组，以及当前模型是否在该 Token 的可用范围内。不要把完整 Token 发给他人排错。

**返回 URL 或 Base64 会有区别吗？** 脚本同时处理两种响应，URL 下载时不携带 KKRICH Token。若模型返回了其他图片编码，按[图片生成 API](/api/image-generation)核对该模型的响应约定。

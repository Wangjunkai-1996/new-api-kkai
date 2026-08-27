---
title: "接口端点"
description: "KKRICH API 常用 OpenAI 兼容端点速查。"
outline: [2, 3]
---

# 接口端点

本文汇总 KKRICH / KKAI New API 常用端点。更多可用能力以控制面板和接口实际响应为准。

## 请求地址

通用 Base URL：

```text
https://api.kkrich.ltd/v1
```

| 功能 | 方法 | 路径 | 完整地址 |
| --- | --- | --- | --- |
| 模型列表 | `GET` | `/models` | `https://api.kkrich.ltd/v1/models` |
| 对话补全 | `POST` | `/chat/completions` | `https://api.kkrich.ltd/v1/chat/completions` |
| Responses | `POST` | `/responses` | `https://api.kkrich.ltd/v1/responses` |
| 通用格式创建视频 | `POST` | `/video/generations` | `https://api.kkrich.ltd/v1/video/generations` |
| 通用格式查询视频 | `GET` | `/video/generations/{task_id}` | `https://api.kkrich.ltd/v1/video/generations/{task_id}` |
| OpenAI 兼容格式创建视频 | `POST` | `/videos` | `https://api.kkrich.ltd/v1/videos` |
| OpenAI 兼容格式查询视频 | `GET` | `/videos/{task_id}` | `https://api.kkrich.ltd/v1/videos/{task_id}` |
| 下载视频 | `GET` | `/videos/{task_id}/content` | `https://api.kkrich.ltd/v1/videos/{task_id}/content` |

::: warning 只调用本站地址
不要把上游渠道文档中的域名、路径或鉴权信息用在客户请求中。视频接口的完整地址始终以 `https://api.kkrich.ltd` 开头。
:::

## 认证

```http
Authorization: Bearer $KKRICH_API_KEY
Content-Type: application/json
```

查询和下载视频也必须携带有效 Token，建议始终复用创建任务时的同一个 Token。

## 最小参数

| 端点 | 最小参数 |
| --- | --- |
| `/models` | 无请求体。 |
| `/chat/completions` | `model`、`messages`。 |
| `/responses` | `model`、`input`。 |
| `/video/generations` | `model`、`prompt`。Seedance 特价版的 `duration` 和兼容字段 `seconds` 可省略，本站按 4 秒处理。 |
| `/videos` | `model`、`prompt`。Seedance 特价版的 `duration` 和兼容字段 `seconds` 可省略，本站按 4 秒处理。 |

Seedance 2.0 接受 4-15 秒整数；四个成本表对应的 Seedance 2.5 名称（`sd_2.5_special_720p`、
`sd_2.5_special_1080p`、`sd_2.5_special_720p_with_video_ref`、
`sd_2.5_special_1080p_with_video_ref`）接受 4-30 秒整数，原有 `seedance-2.5*` 名称仍兼容。普通 2.5 名称用于文生/单图参考，
`with-video-ref` 名称必须提供单个 `reference_video`。缺失、`null`、空字符串、`0` 或 `1-3`
按 4 秒处理；负数、小数、非法字符串和超过模型上限的值返回 400。完整字段和响应协议参见
[Seedance 视频生成 API](./video-generation)。

## 示例

```bash
curl "https://api.kkrich.ltd/v1/models" \
  -H "Authorization: Bearer $KKRICH_API_KEY"
```

```bash
curl "https://api.kkrich.ltd/v1/chat/completions" \
  -H "Authorization: Bearer $KKRICH_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"<model-from-panel>","messages":[{"role":"user","content":"Hello"}]}'
```

## 错误

接口路径错误通常返回 404 或客户端连接错误，参数错误通常返回 400。认证、权限、余额、模型和限速问题请结合响应体与控制面板排查。

## 计费说明

端点是否可用、模型价格、请求额度和限速规则以控制面板实时显示为准。本文不维护模型价格或倍率。

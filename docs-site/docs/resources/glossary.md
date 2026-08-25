---
title: "术语表"
description: "KKRICH API 接入常见术语解释。"
outline: [2, 3]
---
<!-- Recovered from the public production rendering on 2026-08-25. -->
<div class="kkr-recovered-page" v-pre>
<div><h1 id="术语表" tabindex="-1">术语表 <a class="header-anchor" href="#术语表" aria-label="Permalink to &quot;术语表&quot;">​</a></h1><table tabindex="0"><thead><tr><th>术语</th><th>说明</th></tr></thead><tbody><tr><td>KKRICH / KKAI New API</td><td>KKRICH 提供的 OpenAI 兼容 API 服务。</td></tr><tr><td>Base URL</td><td>SDK 或客户端请求的基础地址，KKRICH 为 <code>https://api.kkrich.ltd/v1</code>。</td></tr><tr><td>API Key</td><td>控制面板生成的访问密钥，用于 Bearer Token 认证。</td></tr><tr><td>Bearer Token</td><td><code>Authorization: Bearer &lt;API Key&gt;</code> 认证格式。</td></tr><tr><td>Model</td><td>请求中的 <code>model</code> 参数，必须使用控制面板显示的可用模型名称。</td></tr><tr><td>Chat Completions</td><td>OpenAI 兼容的 <code>/chat/completions</code> 对话接口。</td></tr><tr><td>Responses</td><td>OpenAI 兼容的 <code>/responses</code> 请求接口。</td></tr><tr><td>Token</td><td>模型处理文本的计量单位之一，常用于估算输入和输出消耗。</td></tr><tr><td>Stream</td><td>流式输出模式，适合边生成边展示。</td></tr><tr><td>Rate Limit</td><td>请求频率、并发或额度限制，以控制面板实时显示为准。</td></tr><tr><td>Usage Log</td><td>控制面板中的用量记录，用于核对请求是否到达和消耗情况。</td></tr></tbody></table><h2 id="实时信息" tabindex="-1">实时信息 <a class="header-anchor" href="#实时信息" aria-label="Permalink to &quot;实时信息&quot;">​</a></h2><p>模型、价格、额度、上下文和频率限制以控制面板实时显示为准。术语解释只用于帮助理解接入概念。</p></div>
</div>

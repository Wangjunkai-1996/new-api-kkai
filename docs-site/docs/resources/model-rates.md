---
title: "模型费率"
description: "如何查看和理解 KKRICH 控制面板中的模型费率。"
outline: [2, 3]
---
<!-- Recovered from the public production rendering on 2026-08-25. -->
<div class="kkr-recovered-page" v-pre>
<div><h1 id="模型费率" tabindex="-1">模型费率 <a class="header-anchor" href="#模型费率" aria-label="Permalink to &quot;模型费率&quot;">​</a></h1><p>模型费率、可用状态和额度会随模型、账号和控制面板配置变化。请始终以控制面板实时显示为准。</p><h2 id="建议查看字段" tabindex="-1">建议查看字段 <a class="header-anchor" href="#建议查看字段" aria-label="Permalink to &quot;建议查看字段&quot;">​</a></h2><p>在选择模型前，建议确认以下信息：</p><table tabindex="0"><thead><tr><th>字段</th><th>用途</th></tr></thead><tbody><tr><td>模型名称</td><td>代码中 <code>model</code> 参数必须与可用模型名称一致。</td></tr><tr><td>可用状态</td><td>确认模型当前是否可调用。</td></tr><tr><td>输入价格</td><td>估算提示词、上下文和历史消息成本。</td></tr><tr><td>输出价格</td><td>估算生成内容成本。</td></tr><tr><td>上下文能力</td><td>判断长文本、多轮对话是否适合。</td></tr><tr><td>额度与限速</td><td>判断是否满足并发、批量任务和生产请求。</td></tr></tbody></table><h2 id="选型建议" tabindex="-1">选型建议 <a class="header-anchor" href="#选型建议" aria-label="Permalink to &quot;选型建议&quot;">​</a></h2><ul><li>简单问答和轻量任务优先选择成本更可控的模型。</li><li>长文本、复杂推理或高质量生成任务应先做小样本评估。</li><li>生产任务不要只看单次价格，也要看稳定性、响应时间、上下文和限速。</li></ul><h2 id="重要说明" tabindex="-1">重要说明 <a class="header-anchor" href="#重要说明" aria-label="Permalink to &quot;重要说明&quot;">​</a></h2><p>本文不列固定价格。价格、模型、额度、上下文和频率限制以控制面板实时显示为准。</p></div>
</div>

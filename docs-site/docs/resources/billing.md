---
title: "计费说明"
description: "KKRICH API 计费、余额和用量记录说明。"
outline: [2, 3]
---
<!-- Recovered from the public production rendering on 2026-08-25. -->
<div class="kkr-recovered-page" v-pre>
<div><h1 id="计费说明" tabindex="-1">计费说明 <a class="header-anchor" href="#计费说明" aria-label="Permalink to &quot;计费说明&quot;">​</a></h1><p>KKRICH API 的账单信息以控制面板实时显示为准。文档不提供固定价格表，也不建议把价格、额度或模型状态写死在业务代码中。</p><h2 id="计费依据" tabindex="-1">计费依据 <a class="header-anchor" href="#计费依据" aria-label="Permalink to &quot;计费依据&quot;">​</a></h2><p>常见模型请求通常会根据模型、输入、输出和实际处理情况计量。不同模型可能有不同价格、上下文长度、可用能力和额度策略。</p><p>请在控制面板确认：</p><table tabindex="0"><thead><tr><th>项目</th><th>说明</th></tr></thead><tbody><tr><td>余额</td><td>当前账号可用余额或资源状态。</td></tr><tr><td>模型价格</td><td>输入、输出或其他计费维度的实时价格。</td></tr><tr><td>用量记录</td><td>请求时间、模型、消耗和状态。</td></tr><tr><td>额度限制</td><td>当前账号、模型或 API Key 的可用额度与频率限制。</td></tr></tbody></table><h2 id="接入建议" tabindex="-1">接入建议 <a class="header-anchor" href="#接入建议" aria-label="Permalink to &quot;接入建议&quot;">​</a></h2><ul><li>开发阶段先使用小输入、小输出验证请求是否正常。</li><li>批量任务上线前先跑小规模样本，观察控制面板中的用量变化。</li><li>在业务侧设置预算、并发和重试上限，避免异常循环请求。</li><li>余额不足时先暂停自动任务，补足余额或调整模型后再恢复。</li></ul><h2 id="实时显示优先" tabindex="-1">实时显示优先 <a class="header-anchor" href="#实时显示优先" aria-label="Permalink to &quot;实时显示优先&quot;">​</a></h2><p>价格、可用模型、额度、上下文、频率限制和账单统计均以控制面板实时显示为准。示例代码只说明调用格式，不代表固定费用。</p></div>
</div>

# 图片失败协议审查

审查角色：协议架构。审查对象：`plan.md` v0.1（2026-11-15）。
方法：按 ponytail full 与 code-review 核对真实调用链、状态证据、组合请求语义及两端兼容性；本轮只写审查记录，未修改代码、未执行测试。

## 结论

**Request changes：主方向成立，补齐下列 P1 协议约束后方可冻结方案。**

保留 NewAPI 的稳定错误码与原始 HTTP 200/502 白名单，在 Sub2 的来源端判定完整终态，能够修复本次没有原始 code 的完整 HTTP 502 JSON 错误，而无需通过中文文本猜测失败。公共安全摘要和管理员诊断分开也符合现有权限模型。

历史完整 JSON 未取得，当前只能证明最终错误出口丢失 code，不能宣称已证明该历史响应满足新结构条件；部署验收须用匹配事故特征的受控 fixture，而不能将截图当成机器协议证据。

## 必须落实的修改

| 编号 | 优先级 | 方案位置 | 问题与必须补充的约束 |
| --- | --- | --- | --- |
| P-01 | P1 | 3.1、3.2、4 | 单个 `ImageOutcomeDefinitive=false` 不能区分“尚未派发”与“派发后未知”，最后一次错误上的 true 也不能证明整个请求。方案必须规定：逻辑请求和 OAuth 批次分别累计已派发未知；只要一次未知，最终整单失败标志永久禁止。已确认未发出的连接/凭据失败不得被误当成未知，但也不能凭它创造已取得上游终态的证据。后续代码不得仅复制最后一个错误的 bool。 |
| P-02 | P1 | 3.1、6 | HTTP 完整性必须在**所有图片 HTTP 错误读取入口**建立，覆盖 API-key、OAuth direct/Responses、OAuth batch；不能只改公共耗尽 handler 或一个读取点。完整 JSON 后仍返回 read error、limit+1、错误字段类型、error 与成功输出并存都不满足明确失败条件。已有 `readUpstreamErrorBody` 重新包装 bytes reader 后会抹去读取失败，终态证据必须在第一次读取时决定并传递。 |
| P-03 | P1 | 3.1、3.2、6 | 多图“先明确错误后无终态 SSE”的选择逻辑仍可能保留第一条错误；现有测试只保证部分 transport/read error 能压过早先 HTTP 错误。必须在批次结果选择前保留 unknown，而非依赖 `firstFailure` 或 HTTP 优先级；至少测试明确→未知、未知→明确、成功→未知。已有可交付图片维持部分成功，不能为整个请求发送 `image_provider_error`。 |
| P-04 | P1 | 3.1、3.2 | 明确冻结专用语义的优先级。表格泛称 HTTP error 可规范化，但 3.2 又要求鉴权、容量、余额、策略独立。必须写清：既有 content policy、credential、balance、capacity 等专用出口优先且不被通用 marker 覆盖；通用图片服务失败才使用本协议。终态证据与可重试/账号健康是不同判断，新增 marker 不得改变现有账号处罚，也不因有终态标志新增重放。 |
| P-05 | P1 | 3、3.2、5 | 默认示例的固定 message 与“NewAPI 管理员看到详细原因”之间缺少 wire 契约。如果 Sub2 默认映射仍只发 `Upstream service temporarily unavailable`，NewAPI 无法恢复原文。必须指定：已确认的通用图片失败在最终 envelope 中传递经过 Sub2 既有脱敏和长度限制的有效 message；空诊断回退固定安全文案，policy/credential 等仍走专用摘要。透传规则自定义 message 的优先级和原始诊断是否另存 Sub2 ops 日志要明确，不能以修复 NewAPI 日志为名声称恢复 Sub2 未发送的原文。 |
| P-06 | P1 | 3.2、6 | 透传改状态与稳定 code 的行为必须写成确定规则。推荐：客户端最终状态为 502，或仅 JSON whitespace keepalive 导致实际 HTTP 200 时输出 marker；规则显式将状态改成其他值时保留现有规则结果、**不发这个 marker**，记录管理员规则造成的兼容限制并测试。不得一边宣布现有 NewAPI 会立即退款，一边允许任意 passthrough 状态保留 marker。 |

## 应在冻结版本中写明的细节

- JSON 证据需要求完整根对象、对象型 `error`、非空字符串 `error.type` 与 `error.message`；不得用 gjson 的 `.String()` 把数字或对象隐式当作字符串，也不得仅检测字段存在。`error.code` 可以缺失、null 或未知，不能作为本案生产者证据的必要条件。
- 检查无成功输出必须采用已支持的图片输出形状，至少包含 `data` 中非空 `b64_json/url`、Responses 已完成图片结果；同时出现 error 与可交付图时不可发整单失败 marker。应复用已有解析结果，避免新增另一套与成功解析不一致的猜测器。
- 明确增加 `response.completed` 且无图片的处理：完整终态可作为无可交付图片证据，不依赖模型文字含义。只有 `output_text.delta`、`response.in_progress`、空流或 EOF 时仍是未知。`response.incomplete`、`response.failed`、`error` 需合法完整事件，不能仅凭 event 名称或不完整帧判终态。
- SSE 已有 partial image 字节不等于完整可交付图片，但也不得另写普通 JSON；终态错误仍须走原流式序列化。已有完整图片则保留成功/部分成功及计费路径。
- 明确区分两层“原始 HTTP”：Sub2 内部是实际 provider 状态；NewAPI 的 original status 是 Sub2 输出的 wire 状态。本地 NewAPI 状态映射不改变后一证据；Sub2 自身的错误映射则会改变它。
- NewAPI 当前 `IsDefinitiveImageProviderError` 要求 OpenAI error 类型、code 字符串匹配、`ToOpenAIError().Type` 非空及原始 200/502；它并不要求原始 `type` 精确等于 `upstream_error`，且通用构造器可能填充缺失 type。方案“严格 code/type”不得被解释成已经存在更强的原始 JSON 字段校验。本轮保持消费者规则即可，严格来源检查由生产者负责。

## 已对照的 Sub2 代码事实

本轮只读确认 Sub2 工作树干净，HEAD 为 `4d32760b5834378b6a6b9d4ac10a66f551de86db`，与提供的线上 image 标签短 SHA 一致。未读取生产可变状态，未将该检查当作上线状态证明。

| 源码位置（Sub2 仓库相对路径） | 已确认行为 | 对方案的影响 |
| --- | --- | --- |
| `backend/internal/handler/openai_images.go` | 账户选择耗尽、最大切换次数、429 停止等都进入共享 `handleFailoverExhausted` | 最终输出必须统一消费来源证据，不能漏掉“只有一个账号、排除后无账号”入口 |
| `backend/internal/handler/openai_gateway_handler.go:3982` | 透传/default 最终调用不带 code 的 `handleStreamingAwareError` | code 丢失原因成立；不应全局改变文本入口 |
| `backend/internal/service/openai_gateway_upstream_errors.go:627` | 有界读取丢弃 readErr，limit 没有 +1 检测 | 不可将结果当成完整终态证明 |
| `backend/internal/service/upstream_response_limit.go:26` | 已有读取错误传播、max+1 超限检测 | 可复用，避免另建读取框架 |
| `backend/internal/service/openai_images.go:1094` | API-key HTTP 200 内嵌 error 已输出 `code=image_provider_error`；目前仅检查 error 对象中字段存在 | 需要收紧结构/无输出条件，保留已有 keepalive 与日志处理 |
| `backend/internal/service/openai_images_responses.go:1352` | 收集器返回 foundFinal，调用方丢弃；空输出与仅进度 EOF 会进入相近的 failover 路径 | 终态信息必须保留，不能把已有错误类型本身当成明确失败证据 |
| `backend/internal/service/openai_images_responses.go:1924` | batch 用 firstFailure 与 HTTP 优先级选择最终错误，部分 transport/read error 会主动覆盖 | 须保证所有未知类别均粘滞，不能只测试已有 read error 保护 |
| `backend/internal/service/openai_images_responses.go:2520` | HTTP 200 后读错不 failover；已写出结果不重新请求 | 保留此保障，不能因协议整理移除 |
| `backend/internal/service/openai_pre_send_transport_error.go` | 仅有明确未发出的 transport 证据才允许安全 failover | 不将这类情况错误累积为已派发未知，也不拓宽既有判断 |

全库唯一原生 `image_provider_error` 生成点将它放在 `Code`，`ErrorType` 为 `upstream_error`。没有证据支持在 NewAPI 兼容“Type=image_provider_error”作为本次根因修复。

## 冻结前验收矩阵补充

### 可接受的本次范围收敛

本次事故来自原生 Images HTTP JSON 错误，可将新增终态生产限定为原生 Images 的完整 HTTP 错误及既有 HTTP 200 内嵌错误路径。OAuth/Responses（包括 batch）本次不新增终态 marker，其内部错误类型和 SSE 事件不自动转换为 `ImageOutcomeDefinitive=true`；保留原重试、输出与结算行为。这样最终合同仍准确：marker 只在有本次已验证来源证明且无未知尝试时产生，未覆盖路径继续保守处理。

采用该收敛时，P-02 的新完整性改动只需覆盖所有原生 Images 读取入口，P-03 的 OAuth 终态生成/批次聚合重构可后续独立完成；本次仍须测试 OAuth 不误标、已有部分成功不受影响，以及任何跨账号或路由的已派发未知不能被后续原生明确错误覆盖。若实现证明 OAuth 与原生请求互不跨路由，也需在开发文档引用实际选择分支，不以猜测省略约束。图片最终错误的所有耗尽/透传/keepalive 出口仍必须覆盖，文本共享出口保持原行为。

以下均应是确定性自动化用例，不连接真实模型或创建生产账号：

| 类别 | 必须覆盖的输入 | 结果约束 |
| --- | --- | --- |
| 事故复现 | 130 秒耗时无需模拟；完整 HTTP 502、未知/缺失/null code、正常中文 message、单账号排除耗尽 | 最终稳定 marker；NewAPI failed/refunded；一次 POST/一次退款 |
| 完整性 | 读错但已返回看似完整 JSON、JSON 后附垃圾、limit+1、空/HTML、错误字段类型 | 不发 marker，不重放已派发未知 |
| 跨尝试 | 明确失败→明确失败；明确失败→未知；未知→明确失败；未派发失败→明确失败 | 仅无未知且有来源证明的整单终态可发 marker |
| 多图 | 全明确失败；明确→进度 EOF；进度 EOF→明确；成功→未知；成功→明确失败 | 未知不被先后顺序覆盖；已完成图保留并部分结算 |
| 专用语义 | policy、credential、capacity、balance、request validation | 原专用 code/status、处罚及重试规则不变 |
| 输出出口 | 默认映射、passthrough 原状态、自定义 message、映射非 200/502、JSON keepalive、已写 SSE | 按冻结规则输出 marker；无第二个 JSON；错误诊断可追溯 |
| 两端兼容 | Sub2 fixture 的 502 与 keepalive 200，NewAPI 本地状态映射和幂等恢复 | 当前消费者能识别；无需同时切流；各自回滚不会把未知转为确定 |

定向 handler 测试必须带 `-tags=unit`。本轮未执行测试；全套未运行，无测试通过声明。

## 处置状态

上述 P-01 至 P-06 待主方案负责人逐项写明处置并更新方案版本。协议审查暂不批准新增实现；方案修订后复核本文件所列约束即可，无需重新扫描仓库或扩大测试范围。

### v1.0 复核（2026-11-15）

**Approve：主方案 v1.0 已落实 P-01～P-06；按原生 Images 最小范围实施。**

本轮只读生产路由证据确认：22:06:36.369 的同时间/模型路由日志为 `/v1/images/generations account_type=apikey`；事故 request ID 的失败日志给出 account_id=16841，当前 accounts 交叉查询同 ID 为 apikey/openai。账号当前 updated_at 晚于事故，历史判断使用当时路由日志；不把当前记录冒充历史快照。原始完整 JSON 仍未取得。

Sub2 开发文档已落到其确认仓库的 `docs/kkai/image-failure-development-20261115.md`。它限定原生 HTTP JSON 证明、为 OAuth 实际派发累计保守否决位、保留明确未派发分支，并覆盖共享最终出口的默认/透传/keepalive 行为。OAuth terminal/batch 全面重构按本审查允许范围延后。具体字段和脱敏实现仍需编码后独立审查；此批准不表示测试、生产业务验收或发布门禁已通过。

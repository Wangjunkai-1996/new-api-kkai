# Managed recovery 流式修正开发规格

日期：2026-11-05；基线验证状态更新于 2026-11-06，opaque 本地实现状态更新于 2026-11-07。状态：原流式修正基线的两端本地代码、受管恢复诊断记录、定向验证和本地首增量/分配小样本对照已完成，进入配套发布准备；CPU、磁盘大请求、真实客户端和线上验收仍待完成。本文件新增的 `encrypted_content` opaque 恢复优化是独立开发批次，已完成本地代码、定向测试和有界本地性能对照；代表性负载、真实客户端与生产验收尚未完成，未上线。

上位行为以[修正方案](managed-recovery-streaming-correction-20261105.md)和[产品需求基线](managed-recovery-product-requirements-20261107.md)为准。本规格仅覆盖 NewAPI → Sub2 的 OpenAI HTTP Responses/Chat 受管恢复，保留渠道级 `managed_recovery_v1` 开关。Sub2 旧版 `KKAI_REQUEST_RECOVERY_DEVELOPMENT.md` 中要求 JSON/工具暂存到成功、首轮固定 600 秒和 NewAPI 930 秒硬截止的段落，均由本次修正规格替代；其余已实现的重试预算、账号调度、签名、别名和计量约束继续有效。两个仓库的变更必须作为同一协议版本的配套修改验收，不能只更新一端便开启渠道。

## 联合契约

| 请求或事件 | NewAPI | Sub2 | 对客户端的结果 |
| --- | --- | --- | --- |
| 含 `encrypted_content` 的请求 | 进入 `opaque_input`；不因密文整请求退出（详见文末新增开发批次） | 仅按原账号、原 egress/binding、原 endpoint/transport 和原协议模式恢复 | 原始上下文保持不变，不解密、不删除、不改写 |
| 显式 `json_object`/`json_schema`，或现有不支持的输入形态 | 在握手、超时和重试归属决策前退出受管模式 | 不安装受管 writer；若收到不匹配的受管握手，明确拒绝供尚未提交的请求安全回放 | 原有路径实时流式交付；仅响应提交前的安全重试，无新增 422 |
| 文本流，包括声明 `tools`/`tool_choice:auto` 但尚未输出工具事件 | 受管握手后由 Sub2 独占恢复；过滤内部控制，隔离失败尝试用量 | 文本增量实时发出；可恢复失败按现有有界预算同号重试/切号 | 输出后失败可见“正在重新生成”；最终只有一个逻辑终态 |
| 首个 Chat 工具 delta 或 Responses 可执行工具项目 | 继续透传业务事件，不开启外层重试 | 转发首个事件前标记该逻辑请求不可再恢复；后续工具增量立即交付 | 后续失败按协议结束，不再派发模型或拼接第二轮工具 |
| 非流式请求 | 维持现有结算与响应处理；受管请求保留从请求开始起算的 930 秒总截止 | 响应尚未提交前可安全重试 | 保持完整响应语义 |

工具边界按**实际输出事件**判断，不能因请求仅声明工具就关闭文本恢复。显式 JSON 按最终出站请求的结构化格式判断；提示词里要求 JSON 而未声明格式的请求仍视为文本。工具事件已经公开后，客户端可能持有部分参数，服务端只保证不将失败轮标为成功，也不自动产生第二份调用。无需客户端 reset 或新增公共协议事件。

## Sub2 工作包

1. 在 `backend/internal/handler/openai_gateway_handler.go`、`openai_chat_completions.go` 和 `openai_managed_recovery.go` 的 writer/context 安装点统一入口资格。显式 JSON 走普通路径；直接带受管头却不支持的请求保留明确 capability rejection，避免一端以为已握手、另一端已降级。受管请求的工具声明本身不改变资格。
2. 在 `backend/internal/service/openai_recovery_stream_writer.go` 保留普通文本的实时输出与现有 attempt 状态机。Chat `handleChatFrame` 不再把 `tool_calls` 暂存于 `heldChatFrames`；当前直接拒绝的旧式 `delta.function_call` 也须按原协议实时转发，并触发同一不可重试边界。Responses `handleResponsesFrame` 对可执行工具项目不再等 `CommitAttempt` 释放。首个工具事件**转发前**锁定不可重试状态，后续帧按原顺序转发；完成时不二次释放工具。复用现有 item ID、output index、display 和终态正规化路径，确保 Responses 完整输出与先前增量一致。reasoning、压缩及其他非工具项目不要因为本次改动无条件解封。
3. 两个 handler 的失败分支在 `AbandonAttempt`、同号等待和切号之前检查工具边界。已公开工具后，按现有控制协议发送 `terminal(failed)` 和公共失败事件，结束该逻辑请求；不得出现 marker、下一次 `attempt_begin` 或额外模型 dispatch。必要时调整 service 的响应读取错误分类，使错误能回到 handler 形成唯一终态，而不是在底层续接。
4. 移除 `openAIManagedAttemptContext` 对健康首轮的恢复专用 600 秒截断。当前受管 Chat 已在 `beginOpenAIRequestBudget(..., true)` 装入 900 秒硬截止；改为只用预算限制实际失败后的下一次派发，不中断仍健康的首轮。保留原有客户端取消、首个有效输出/空闲超时和真正失败后的有界重试；不要一并删除 Responses 原有 900 秒请求预算，也不要改变普通 Chat 的现有期限。将超时原因与普通客户端取消区分。
5. `CommitAttempt` 的 Responses 别名发布仍须在可继续使用的 logical ID 完成前成功。发布失败不能宣称成功、静默换账号或再派发一次已完成生成；发出明确失败终态，并保持别名/后续续聊不指向错误账号。旧受管响应后若续聊请求改走普通 JSON 路径，仍须解析已有别名并遵守绑定账号/历史约束。

## NewAPI 工作包

1. 在 `relay/channel/api_request.go` 的 `DoApiRequest`，参照现有加密输入例外，依据**最终序列化出站请求体**判断显式 `text.format.type` 或 `response_format.type` 的 `json_object`/`json_schema`，再决定 `ManagedRecoveryV1`、请求 context、HTTP transport、握手头与后续重试归属。检查请求转换和参数覆盖后的实际形态，不能只看可能过期的 `info.Request`。不要按 `tools`/`tool_choice:auto` 直接排除。`relay/common/recovery.go`、`controller/relay.go` 的判断要共享同一次逐请求结果，避免双方都不重试或双方重试。
2. 保留 `relay/channel/openai/recovery_v1.go` 的 v1 控制帧校验，以及 Responses/Chat 流处理器的 attempt 用量隔离。工具事件后由 Sub2 发 `terminal(failed)` 再发公共失败；NewAPI 识别失败而不在已输出流外重试。核对原生和现有转换路径的工具索引、唯一终态、最终有效尝试用量及失败时已有的部分用量规则，不把工具失败统一写成零费用或成功。
3. 协同调整 `relay/channel/api_request.go`、`service/http_client.go` 与 `relay/helper/stream_scanner.go` 中受管流式请求的 930 秒固定取消来源：健康长流不能仅因恢复预留而被截断，客户端取消仍须传播，真实故障后的 Sub2 重试仍有界。非流式受管请求保留从 `StartTime` 起算的 930 秒总截止，避免响应头到达后正文停顿而无限等待。避免退回更短的全局 `RelayTimeout`；`ResponseHeaderTimeout` 只约束响应头，不作为整段流时限。保留现有渠道开关、签名、握手失败的明确处理和非受管请求行为。
4. 将受管恢复的入口原因和 Sub2 终态诊断写入现有管理员日志 `other.admin_info`，只保留结构化计数和停止原因，不新增数据库表、客户端响应头或逐帧持久化。入口记录包括握手成功、JSON/加密输入/不可重放请求体排除、能力降级，以及握手前传输/HTTP/协议错误；终态记录包括同账号重试次数、切账号次数、`completed/incomplete/failed`、救回判定和工具输出后停止原因。每次外层重试清除上一尝试诊断，避免跨请求串线。内部 `X-KKAI-Recovery-Diagnostics` 仅在 NewAPI 入口消费并移除。

## 开发顺序与验收

先固定两端的逐请求资格和失败终态契约，再修改 Sub2 writer/handler，最后核对 NewAPI 解析和计量；双方代码配套后才生成真实 Sub2 SSE 样本并从 NewAPI 原生与现有转换入口重放。每个工作包只改与上述行为有关的调用点，不另起恢复框架、客户端协议或配置体系。

用可控上游和同步屏障做六组确定性用例，避免靠 sleep 猜首字时间：

1. 正常 Chat/Responses 文本的首个增量在上游终态前到达；通过检查实际 context/client/scanner 截止配置或可控时钟验证健康长流不触发恢复专用 600/930 秒取消，无需真实等待十几分钟。
2. 文本在首字前和中途分别遇到 429/暂时性 5xx，实际同号重试或切号，只有一个最终逻辑终态；NewAPI 只结算有效尝试，保留已有失败部分计量规则。
3. 声明 `tools`/`tool_choice:auto` 但只输出文本的请求，中途失败后仍能恢复。
4. Chat `tool_calls` 与 Responses function/web-search 项的首个工具事件在终态前到达；随后失败只发一个失败终态，不再派发、不发重生成提示、不重复工具帧。
5. Chat `response_format` 与 Responses `text.format` 的显式 JSON 均不握手、不出现新 422；成功时实时输出，半截 JSON 失败时不拼接，非流式提交前重试仍可用。
6. 受管 Responses 成功续聊和别名发布失败分别验证；两端握手/降级归属一致，取消仍有效。

以 Sub2 `openai_recovery_stream_writer_test.go` 和受管 handler 测试、NewAPI `recovery_protocol_test.go`、`recovery_v1_live_http_test.go`、`relay_responses_billing_test.go` 等现有夹具为基础。此文档不代表线上功能已经修复。

## 诊断查询约定

诊断不增加页面或专用接口。操作员按渠道、分组和 UTC 时间窗口查询现有日志，使用[受管恢复查询说明](managed-recovery-log-query.md)及只读脚本 `scripts/kkai/query-managed-recovery.sql`。脚本按 `request_id` 在窗口内去重，汇总总请求、进入受管数、计数已知/未知、同号重试、切账号、救回、失败、工具停止及原因分布；历史日志缺少计数时标为未知，不能当作零次重试。`recovered` 只统计最新日志为消费终态、终态为 `completed`、无协议错误且确有重试或切号的请求，普通成功、错误日志和协议错误完成均不计入。

查询前按 NewAPI 发布入口确认基础设施 checkout 并执行一次 `make newapi-status`；随后只读访问生产日志。查询窗口最多六小时，不输出客户正文、账号或请求 ID。Sub2 无终态的断开记录为 `openai.recovery_incomplete`，同样不能算作救回。

## 本次验证记录（2026-11-06）

以下 NewAPI 定向命令均通过，未运行仓库全套：

```sh
go test ./relay/channel -run 'TestManagedRecovery(ContextPreservesParentDeadlineAndCancellation|CancelsSub2AfterHeadersWithoutWaitingForPolicyDrain|SelectsProtocolFromOutboundBody|ChecksFinalResponsesInput|ProtocolRequiresSignedChannelAndResponseEcho)' -count=1 -timeout=90s
go test ./relay/channel/openai -run 'TestManagedRecovery|TestRecoveryV1' -count=1 -timeout=90s
go test ./relay/helper -run 'TestStreamScannerHandler_ManagedRecovery' -count=1 -timeout=90s
go test ./service -run 'TestManagedRecoveryHTTPClients' -count=1 -timeout=90s
go test ./controller -run 'Test.*Retry' -count=1 -timeout=90s
python3 relay/channel/openai/testdata/sub2_recovery/capture.py --sub2 /Users/tokk/Documents/Codex/2026-08-09/li/work/sub2api
go test ./relay/channel/openai -run '^TestManagedRecoveryCapturedSub2' -count=1 -timeout=90s
git diff --check
```

真实 Sub2 writer 生成的 17 个 SSE 样本已重放，包含 Chat/Responses 原生和双向转换的工具失败路径；manifest 的三项源码 SHA 与捕获时 Sub2 文件一致。截止测试覆盖流式无固定总截止、非流式 930 秒截止及父请求取消。未加入依赖近到期等待的正文停顿测试。

Sub2 本地开关对照命令（在其 `backend/` 执行）已通过：

```sh
go test -tags unit ./internal/handler -run '^TestOpenAIRecoveryToggleStreamsBeforeUpstreamTerminal$' -count=1 -v -timeout=120s
```

NewAPI 受管恢复诊断字段由现有 `service/log_info_generate` 管理员日志路径写入；对应协议、入口原因、终态计数和日志归一化测试均通过。查询脚本已用本机 PostgreSQL fixture 验证请求去重、旧日志未知计数、协议错误、工具停止和后续错误日志不会误计为救回。未访问或修改生产环境。

同一可控上游、25 ms 终态屏障的小样本：关闭时首增量 2.316 ms、完成 29.922 ms、624 allocs；开启时首增量 0.524 ms、完成 27.737 ms、1008 allocs（`AllocsPerRun(3)`）。这是本地功能与分配观察，不用于推断生产更快或零开销；输出可从本次执行记录回溯，没有独立性能报告文件。

剩余验收：CPU 及代表性负载、真实客户端体验、配套发布后的最小业务与生产门禁。NewAPI 对磁盘型 `ReplayableBody` 仍需完整读取以判断最终 JSON/密文形态，其额外磁盘读取与内存峰值尚未量化；未以关闭大文本恢复来规避该成本。内存型重放体复用现有只读字节。

## encrypted_content opaque 恢复优化（本地实现与定向测试完成）

### 目标与现状

上线后效果查询显示，渠道 31/default 的受管恢复诊断链路可以记录握手和终态，但约 96% 的请求因 `encrypted_content` 被整请求排除，观察窗口没有可证明的重试、切账号或救回案例。因此本批次先修复恢复资格判断，再评估救回效果；不能把删除排除条件本身当作功能完成，也不能把诊断记录当作救回成功证明。

本批次只允许安全地携带原始上下文恢复。它不解密、不持久化、不删除、不改写 `encrypted_content`，也不把带密文的历史重建成普通文本。跨账号携带密文暂不开放，必须等真实同协议、真实上下文和真实账号池验证通过后另行立项。

### 联合契约

| 请求 | 入口资格 | 恢复与路由 | 用户可见结果 |
| --- | --- | --- | --- |
| 普通文本、无 `encrypted_content` | 沿用现有受管文本资格 | 现有同账号重试后再切账号；预算和工具边界不变 | 正常首字实时到达；可恢复错误按既有重试语义处理 |
| 含 `encrypted_content` 的 Responses 请求 | 进入内部 `opaque_input` 模式；不因密文整请求退出 | 初次派发外最多 2 次额外重试；只允许原账号、原 egress/binding、原 endpoint/transport 和原协议模式；不满足硬约束时不发送 | 原始请求上下文保持不变；恢复成功才返回后续结果，失败保留上游错误 |
| 含密文且带 `previous_response_id` | 继续进入 opaque 模式 | ID 绑定的 account、egress、endpoint 必须全部匹配；不删 ID、不做纯文本重建 | 缺少可移植快照不阻断正常请求，也不阻断原账号、原出口和原协议路线的原字节重试；首次派发外最多重试 2 次，禁止跨账号恢复或重建历史 |
| 显式 `json_object`/`json_schema`、不可重放请求体或既有不支持形态 | 走原有非受管路径 | 不安装受管 writer；保留原有提交前安全重试 | 不新增 422，不拼接半截 JSON |
| 上游 `invalid_encrypted_content` | 受管尝试终止 | 记录 `invalid_encrypted_content`/`opaque_context_rejected`；不删除密文伪装成功 | 返回原始错误语义 |
| 实际工具事件已公开 | 沿用工具输出护栏 | 不再重试或切账号 | 保持一个失败终态，不重复工具调用 |

“含密文”只改变恢复资格，不改变客户端协议；不会新增请求头、数据库表、管理页面或客户端参数。显式 JSON 的判断仍依据最终序列化出站请求体，不能只看尚未转换的入参。

### 实现约束

1. **NewAPI 入口**：在 `relay/channel/api_request.go` 根据最终出站 body 标记 `opaque_input`，移除仅针对 `encrypted_content` 的整请求排除；保留显式 JSON、不可重放 body 和既有不支持形态的排除。原始 body 必须可重复读取，重试前后字节完全一致。
2. **Sub2 入口与转发**：在 `backend/internal/handler/openai_managed_recovery.go` 及 gateway handler 传递 opaque 标记；恢复路径绕过 `deriveOpenAIForwardAttemptBody`、lineage 预清理、reasoning 剥离和删除密文后的重试路径。不得解密或落盘密文。
3. **硬路由 fence**：`WithOpenAIManagedRetryAccount` 只表示偏好，不能作为保证。opaque 请求在账号选择、账号槽位获取/刷新和每次 Forward 前后都校验 account ID、egress/binding、endpoint/transport、协议模式及 passthrough 标志；校验失败记录 `route_unavailable`，不发送错账号请求，不回退普通调度器。
4. **重试预算**：opaque 请求最多 2 次额外重试，且只能同账号同路线；不开放跨账号切换。普通文本继续现有同账号重试后切账号。每个逻辑请求只保留一个最终计量终态。
5. **续聊绑定**：`previous_response_id` 的 owner 路由约束优先于可用性；没有完整可移植 snapshot 时不做跨账号恢复，不删除 ID、不用文本摘要替代；正常首次请求不因此失败。
6. **诊断**：复用现有 `other.admin_info.managed_recovery`，增加入口/终止原因 `encrypted_input_opaque`、`route_unavailable`、`opaque_context_rejected`、`invalid_encrypted_content`，并保留 `same_account_retries`、`account_switches`、`recovered`、`final_outcome`。不记录密文本身、请求正文、账号标识或新逐帧日志。

### 两端开发包

**NewAPI**

- 将资格判断和 `ManagedRecoveryV1` 握手选择改为一次性使用最终出站 body 的结果，避免 controller、relay 和 Sub2 对同一请求作出不同决定。
- 保留内部诊断头消费后删除；opaque 请求的重试 body 使用同一可重放内容，禁止任何字段清理或密文剥离。
- 复核 Responses/Chat 的计量、取消和唯一终态；正常成功只做一次 upstream dispatch，不因诊断写入等待首字。

**Sub2**

- 在 managed recovery context 中携带 opaque policy 和原始 route fence；scheduler 不能把“原账号不可用”静默变成其他账号。
- 在 acquire、refresh、forward 三个阶段执行同一 fence 校验；失败只返回 `route_unavailable`，不产生上游请求。
- 对 `invalid_encrypted_content` 保留原始错误并结束尝试；工具事件后沿用现有停止逻辑。
- 普通文本的同号重试/切账号、显式 JSON 和工具输出边界必须保持原行为。

### 定向测试与验收门槛

使用可控上游和同步屏障，不用依赖真实等待或生产数据：

1. 含密文的正常 Responses 流：首个真实文本增量早于终态；原密文字节不变；只发生一次正常 dispatch。
2. 含密文遇 429/502：同账号、同 egress、同协议重试，最多两次额外重试；最终成功时 `same_account_retries>0`、`account_switches=0`、`recovered=true`。
3. 账号或 egress 发生变化：不 Forward，记录 `route_unavailable`。
4. `previous_response_id`：owner 路由不漂移，ID 不删除，不做纯文本重建；无可移植快照不阻断本轮正常成功。
5. 普通无密文文本：保留同号重试后切账号；显式 JSON 行为不变。
6. 工具事件公开后：不重试、不切号、不重复工具帧；只有一个失败终态。
7. `invalid_encrypted_content`：密文和原始错误保留，原因可从管理员日志查询。
8. 计费与诊断：重试只产生一个最终逻辑终态，历史未知计数不被误报为零。
9. 性能对照：相同 payload、相同上游分别开关受管恢复，记录首字 P50/P95、完成延迟、CPU/alloc 和 Redis alias publish error。验收目标是正常成功路径无新增等待、无整段缓存、无额外 dispatch；不得写成绝对零性能影响。

两端代码、定向测试和本地性能对照全部通过后，才可进入独立发布准备。未完成前不得打开渠道 31 的新行为开关，也不得以历史基线的发布记录代替本批次验收。

### 2026-11-07 本地实现记录

- NewAPI 按最终出站 body 允许密文 Responses 进入受管恢复，记录 `encrypted_input_opaque`；显式 JSON 继续排除，内部诊断消费后删除。已协商的失败终态由 Sub2 收口，避免 NewAPI 再次重试同一逻辑请求。查询脚本同步计入 opaque 受管请求。
- Sub2 保留原始 `input` 和 `previous_response_id`，固定重放首个实际派发 body；不重建密文历史、不持久化密文 snapshot。首个实际派发后锁定账号、当前或 legacy egress/binding、代理、URL/method 及协议模式，初次派发外最多两次重试；缺少可移植 snapshot 不阻断此类原字节重试。派发前的本地失败保留原有安全选路；派发后不可退回普通账号池。
- 每个受管响应别名额外保存实际路由的摘要指纹，不保存密文或代理凭据；后续 opaque 续聊在派发前校验该指纹，端点或代理变化不产生新派发。旧别名缺少指纹时不虚构历史路由证据。
- 明确 HTTP/plugin `RequestNotSent` 的首轮失败保留原安全换号，退还物理派发预算，不记作重试或切号；已经实际发送后的路线不会因后续未发送失败解锁。未知发送状态按可能已发送处理。实际发送最多 3 次，内部 attempt 序号独立限制为 11，保持协议 v1 控制帧边界。
- HTTP 错误与 HTTP 200 SSE 的 `response.failed`/`error` 均识别密文拒绝，保留错误语义并停止；native、passthrough、流式与非流式路径都有定向覆盖。工具事件公开后仍停止恢复。
- 普通 JSON 未携带内部握手时行为不变；显式请求内部握手但不支持的 JSON、图像或 compact 请求，保留既有的派发前 `422 recovery_capability_unavailable`，不能静默生成成功响应而漏掉握手回显。

本批次定向命令及结果（未运行全量测试）：

NewAPI 仓库根目录：

```sh
go test ./relay/channel ./relay/channel/openai ./controller -run 'Test(ManagedRecovery|RecoveryV1Terminal)' -count=1 -timeout=120s
git diff --check
```

以上通过。Sub2API 的 `backend/` 目录：

```sh
go test -tags unit ./internal/service -run '^Test(OpenAIOpaqueRecovery|OpenAIRecoveryAlias)' -count=1 -timeout=90s
go test -tags unit ./internal/service -run '^Test(OpenAIModelDispatch|OpenAIRetryBudget|OpenAIGatewayService_Forward_HTTPRetryRecovery|OpenAIGatewayService_Forward_HTTPThinkingSignatureRecovery|OpenAIGatewayService_SelectAccountWithScheduler|OpenAIStreaming(ResponseFailed|PassthroughResponseFailed|ContextWindowResponseFailed|PolicyResponseFailed)|NonStreamingTerminalFailureFailover)' -count=1 -timeout=120s
go test -tags unit ./internal/handler -run 'TestOpenAIManagedCapabilityHandshakePreservesNoDispatchFallback|TestOpenAIManagedOpaque' -count=1 -timeout=120s
git diff --check
```

三个测试命令分别通过，包内耗时为 0.068s、0.072s、3.176s；diff 检查通过。此前普通受管 handler 与首增量开关对照的定向回归亦通过。测试使用可控上游验证重试上限、原始 body、跨轮端点/代理漂移、空密文 snapshot、唯一终态与首增量早于上游终态。

查询脚本已使用本机独立 PostgreSQL 18 容器验证：`python3 .local-releases/opaque-recovery-20261107/check-query.py` 通过。fixture 覆盖 opaque 分类、缺失终态计数保留为 unknown、请求去重、新停止原因，以及失败/协议错误不误计为 recovered；使用合成数据、无网络容器，结束后已删除测试容器。

最后修正未发送请求的物理预算、路由锁和诊断后，追加以下定向验证（均通过）：

```sh
# Sub2API backend/
go test -tags unit ./internal/service -run '^TestOpenAIOpaqueRecovery' -count=1 -timeout=90s
go test -tags unit ./internal/service -run '^Test(OpenAIModelDispatch|OpenAIManagedDispatch|OpenAIManagedFirstOutput|OpenAIRetryBudget)' -count=1 -timeout=120s
go test -tags unit ./internal/handler -run 'TestOpenAIManagedOpaque|TestOpenAIManagedCapabilityHandshakePreservesNoDispatchFallback' -bench '^BenchmarkOpenAIOpaqueRecovery$' -benchtime=200x -count=1 -benchmem -timeout=120s
# NewAPI 根目录：以最终 Sub2 源码捕获并回放协议样本
python3 relay/channel/openai/testdata/sub2_recovery/capture.py --sub2 /Users/tokk/Documents/Codex/2026-08-09/li/work/sub2api
go test ./relay/channel/openai -run '^TestManagedRecoveryCapturedSub2' -count=1 -timeout=90s
```

服务层两项耗时 0.070s、0.066s；handler 回归与 benchmark 合并命令耗时 4.512s；最终捕获 0.060s、回放 0.055s。捕获器现在连接真实派发计数 observer，17 个 SSE 样本及来源指纹一起更新。

本地开关对照使用相同 2,457 字节密文 payload、相同可控 SSE 上游、API-key 账号和 miniredis，各预热一次并记录 200 个请求，无人工等待或时间阈值断言：

| 指标 | 关闭恢复 | 开启恢复 |
| --- | ---: | ---: |
| 首字 P50 / P95（ms） | 0.499 / 1.047 | 0.541 / 0.977 |
| 完成 P50 / P95（ms） | 0.535 / 1.086 | 0.804 / 1.341 |
| 进程 CPU（ns/op） | 740,005 | 1,285,775 |
| B/op / allocs/op | 163,831 / 1,007 | 648,834 / 2,725 |
| 实际上游派发次数/请求 | 1 | 1 |
| alias 发布错误/请求 | 不适用 | 0 |

首字中位数增加约 0.043 ms，完成中位数增加约 0.269 ms，存在明确 CPU/分配开销。CPU 和分配包含 fixture、进程后台任务和同进程 miniredis/Lua；不能将差值当作生产网关本身的开销，也不能由小样本 P95 下降声称性能改善。初始 500 次采样触发既有 256-alias/scope 容量保护，未修改或绕过保护；成功路径比较控制在容量以下。独立同步屏障测试证明真实文本增量早于上游终态到达。

本轮仅完成本地代码、定向测试、有界性能对照和文档，未提交、构建、部署或访问生产，也未调整渠道开关。代表性负载、磁盘大请求、生产 Redis/真实客户端和配套发布后的验收仍待完成；插件内部的重定向/重试没有独立运行证据。以上本地结果不代表生产发布门槛全部通过。

### 非目标

- 不做跨账号 `encrypted_content` 兼容，不实现密文解密、重新签发或历史迁移。
- 不新增客户端协议、数据库表、管理页面、全量请求正文存储或新的重试框架。
- 不扩大到极端格式组合、长时间压测或全量测试套件；发现正常请求首字、计费或安全边界回归时，必须先修复再进入发布。

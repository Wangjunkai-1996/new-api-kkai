# Managed recovery 流式修正开发规格

日期：2026-11-05；验证状态更新于 2026-11-06。状态：两端本地代码、受管恢复诊断记录、定向验证和本地首增量/分配小样本对照已完成，进入配套发布准备；CPU、磁盘大请求、真实客户端和线上验收仍待完成。

上位行为以[修正方案](managed-recovery-streaming-correction-20261105.md)为准。本规格仅覆盖 NewAPI → Sub2 的 OpenAI HTTP Responses/Chat 受管恢复，保留渠道级 `managed_recovery_v1` 开关。Sub2 旧版 `KKAI_REQUEST_RECOVERY_DEVELOPMENT.md` 中要求 JSON/工具暂存到成功、首轮固定 600 秒和 NewAPI 930 秒硬截止的段落，均由本次修正规格替代；其余已实现的重试预算、账号调度、签名、别名和计量约束继续有效。两个仓库的变更必须作为同一协议版本的配套修改验收，不能只更新一端便开启渠道。

## 联合契约

| 请求或事件 | NewAPI | Sub2 | 对客户端的结果 |
| --- | --- | --- | --- |
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

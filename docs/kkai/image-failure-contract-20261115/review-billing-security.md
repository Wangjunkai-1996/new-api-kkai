# 计费、安全与测试方案审查

日期：2026-11-15（Asia/Shanghai）。审查对象：`plan.md` v0.1；审查时 NewAPI 基线为 `55a55f56e7ae969b090fa475dafec1dece27cc46`，工作区另有两处尚未批准的诊断日志修改。

使用 `code-review` 与 `ponytail` 技能。此次只读检查源码和方案，不访问生产、不执行业务请求、不修改代码。

## 结论

**方案方向通过；以下 R1–R3 必须在冻结方案及开发文档中明确，实施完成后重新审查代码。当前“直接将 relayErr 原文传给共享日志函数”的两行补丁不批准上线。**

方案正确地区分了明确终态失败和请求结果未知，保留已有事务结算、fence、幂等与输出优先级。第 5 节采用公开摘要与管理员诊断分离，解决了当前初始补丁的主要泄漏问题。图片工作室无需改变状态模型；新增管理员日志诊断需要最小前端展示改动。

此结论不代表测试已通过、生产状态已确认或发布已获验收。

## 必须明确的事项

| 编号 | 优先级 | 要求与理由 | 完成证据 |
| --- | --- | --- | --- |
| R1 | P1 | 明确脱敏顺序和失败回退：先移除当前实际使用渠道 key 的精确回显，再清理常见凭据、地址和控制字符，最后按有效 UTF-8 边界限长；脱敏失败或无法安全解析时只保留结构化诊断。不得先截断 key 再脱敏，也不得把原始文本放入任何 fallback。`MaskSensitiveInfo` 不能独立承担此合同。 | 开发文档给出字段名、上限与处理顺序；测试覆盖跨长度边界的 key、大小写/空白/引号形式、URL fragment、非法 UTF-8 和控制字符。 |
| R2 | P1 | 诊断元数据必须写入既有 `admin_info` 权限容器；只读投影和复制内容都只能使用服务端已脱敏值。public `logs.content`、generation/outbox 错误、系统日志不得旁路保存原文。保留原 `relayErr` 的类型、code、原始 status、policy evidence；日志处理不能修改失败分类、渠道健康或资金行为。详细诊断收集/写日志失败不应让已确认失败退回 unknown，更不能触发第二次 POST。 | 后端持久化与 self/token/tenant/admin/root 投影测试；原错误对象未变测试；独立 frontend 权限与复制测试；代码审查全部日志出口。 |
| R3 | P1 | 定向命令必须显式覆盖实际计费模式，不能把被 `MembershipEnabled()` 跳过的 legacy 测试报告为退款验收通过。至少跑 legacy 钱包的 worker/fence/replay 回归，以及会员与会员钱包回退的既有图片结算/退款合同；若修改计费代码，再补对应来源的 worker 明确失败测试。验收应检查最终实际执行数和 skip 原因。 | 开发/验收文档列明环境变量、精确命令、是否 skip，以及额度/预留/账本状态断言。 |

R1–R3 是本方案的实现与验收约束，不要求新增通用日志框架、数据库字段或重写计费层。

## 已核实的计费边界

`service/image_async_execution.go` 的 `CompleteImageExecution` 应保持现有顺序：

1. 在事务中检查 worker/outbox/leader fence，并锁定 execution。旧 worker 无权退款、结算或改终态。
2. 读取持久化 accounting 及有效输出。只有 `ready > 0`、`ready <= requested_count`、`staging == 0` 且 accounting 的 `OutputCount` 等于 ready 数量，才允许结算。
3. 满足上述条件时先结算，全部或部分成功优先于传入的任何失败码。不得因为后来收到明确失败标志而删除已可交付结果或退款。
4. 无可结算输出且有明确失败原因时，在同一事务内清理未交付资产、释放预留、写 generation 终态与 `final_quota=0`、将 execution 置为 done，并清理输入引用。
5. `image_provider_error` 进入既有失败分支；其他已派发但不明确的错误保持 unknown/reconcile，不能通过文案或最终账号耗尽推断失败。

底层 `RefundImageGenerationBillingTx` 对 refunded 状态幂等，拒绝从 settled 状态退款；会员路径通过已有账本 release。`FinishImageGeneration` 同时校验预期状态和 settled/refunded 账务状态。outbox 重投时，`AdoptImageExecution` 对 done 直接返回；相同幂等键仍指向原任务，不产生第二次请求。

**本次不需要修改这些算法。** 测试应把新终态证据接入上述边界，而非增加新退款入口。已有成功输出必须附带完整 accounting；仅存在孤立 ready 行不能视作可收费成功。

## 初始日志补丁的阻塞问题

`controller/image_studio_worker.go` 当前未提交修改删除了固定审计文案，将原 `relayErr` 直接传入 `ProcessChannelErrorAfterPolicy`。共享函数将 `MaskSensitiveErrorWithStatusCode` 的结果同时写到系统日志和 `logs.content`。`model.formatUserLogs` 只过滤 `other.admin_info/root_info/audit_info`，不隐藏 content；用户日志详情直接渲染 content。因此该补丁会扩大上游自由文本的公开范围。

现有 `relaykit/relayconvert/kitutil/mask.go` 的保护包括特定 `api_key:xxx` 格式、URL userinfo/query、IP 与配置上游地址，但并不完整覆盖以下情况：

- `Authorization: Bearer VALUE`、JSON `"api_key": "VALUE"`、`api_key=VALUE`、大小写及空白变体。
- 原始活动 key 以未知字段或普通文字形式回显。
- 非上游 URL 的 fragment、路径中的秘密，以及上游回显的 prompt、账户内部信息、政策或充值指引。

worker 当前调用还固定传 `policyDetected=false`；不能用这条调用证明已有固定 policy 摘要已经生效。方案应基于已有结构化 policy 分类投影安全摘要，避免重新触发任何政策副作用。

当前 integration test 把完整中文 `providerDiagnostic` 写入 public `logs.Content` 作为期望，同时删除了旧的原文不公开断言。最终实现必须替换这条期望：公开字段只含受控摘要；脱敏详情仅出现在规定的管理员字段；敏感样本在持久化、用户投影和系统日志中均不可出现。

第 5 节提出的管理员详情属于受控运维诊断，不承诺任意自由文本都可安全公开。即使已脱敏，也不能把该字段复用于图片工作室用户提示。

## 最小测试矩阵

| 领域 | 输入/场景 | 必须观察的结果 |
| --- | --- | --- |
| 消费者明确失败 | 正确 OpenAI envelope，`code=image_provider_error`，原始 HTTP 200/502 | failed、upstream、refunded、quota=0、execution done；结构化分类不依赖中文文本。 |
| 状态映射 | 原始 502 映射为 400/200，原始 200 映射为 502 | 使用原始状态决定语义；展示状态映射不伪造或抹掉终态证据。 |
| 不完整证据 | 缺少 type、仅 message、generic upstream_error、无效/超限/读取失败、网络断开 | 保持 unknown 路径；不立即退款并允许重发，不新增第二次 POST。 |
| 结果优先级 | 有完整 accounting+ready；部分 ready；staging 或 count 不匹配 | 完整可交付结果优先 settle/partial；不匹配不得收费；失败码不能覆盖成功。 |
| 所有权与幂等 | 旧 fence、同一幂等键、outbox 重投、已 done 再投 | 旧 worker 零资金/状态变更；单次上游 POST、单次退款/结算、单次统计样本。 |
| 事务一致性 | 完成状态写入失败导致整个事务回滚，再正常恢复 | 不能出现钱已退但任务仍可结算、或任务已失败但预留未释放。可复用已有同事务回滚测试，不为覆盖率复制实现。 |
| 资金来源 | legacy 钱包；会员；会员钱包回退 | 钱包、token 额度及会员预留/账本按原合同恢复；没有重复 release、额外信用或错误扣费。使用明确计费模式运行既有测试。 |
| 脱敏 | exact key、Bearer、JSON/等号/冒号秘密、URL userinfo/query/fragment、IP、控制字符、超长/非法 UTF-8 | 任何秘密值不落公开内容或系统日志；管理员字段只有脱敏且有效 UTF-8 的有界内容。先脱敏再截断。 |
| policy 与错误对象 | policy evidence、本地 policy code、普通 provider_error、记录诊断前后比对 | policy 原文不公开、不新增策略副作用；原始 code/type/status/evidence、健康采样及失败分类不变。 |
| 权限与 UI | self/token/tenant、管理员、root；复制操作 | 低权限响应没有诊断字段；管理员/root按既有授权可见；复制仅复制已展示的脱敏详情。 |

消费者正常失败路径应使用 Sub2API 生产者测试的相同响应 fixture，避免两端分别通过但协议不一致。生产者侧的“任一次未知尝试使整单未知”与部分成功规则必须由协议审查确认；本角色不以最后一次失败替代整单证据。

## 前端范围

- 图片工作室的 `checkingOutcome` 条件是 `status=submitting && execution_state=unknown`。后端正确返回 failed 后，现有组件会展示失败并允许用户主动重试；无须改状态判断或展示 raw `error_message`。
- 既有 unknown/confirming 状态文案和“不提供计费重试”测试继续保留。任务详情不得因新日志能力取得自由文本诊断。
- 日志详情需要增加专门的管理员“上游错误详情”展示和复制，不能把管理员详情拼入通用 content。复用已有详情组件、权限判断、翻译键和 clipboard 能力，不新增依赖。
- 前端测试应同时证明授权可见、普通用户不可见和复制脱敏内容。使用真实现有文案键时无需重复造键；新增文字按 i18n 技能维护所有要求的语言。

## 验证记录

本次审查未运行测试或构建，未执行全量套件。已阅读相关 worker、错误分类、结算/退款、日志权限投影、脱敏与前端展示实现及现有回归测试。审查时运行 `git diff --check`，结果通过；它只证明当前差异无空白错误，不证明行为、安全或计费正确。

待主方案吸收 R1–R3、开发文档明确字段和命令后，可进入实现；代码审查必须重新确认初始公开原文补丁已被安全投影实现替换。

## v1.0 复审与接口冻结

已复核 `plan.md` v1.0。主方案已吸收 R1–R3，包括精确 key 优先删除、UTF-8 限长与安全回退、管理员权限容器、日志失败不改变资金/终态，以及 legacy/会员分模式验证。**本角色的方案审查通过，可以按开发文档实施；原有危险日志补丁仍不能单独上线。**

管理员接口固定为 `admin_info.image_upstream_error = {message?, type?, code?, status_code?, truncated}`。message 最多 2048 UTF-8 bytes，type/code 每项最多 128 ASCII 安全标识 bytes；不安全则省略；status_code 仅记录有效原始 wire HTTP 状态，无证据时省略；truncated 为必有 boolean。处理顺序为精确活动 key/配置地址 → 常见凭据/完整 URL/IP → 控制字符 → UTF-8 边界限长。未知格式、非法编码或超过 64 KiB 处理上限的正文直接退化为安全结构化字段，不扩展为通用消毒器。

`newapi-backend-development.md` 已明确基于现有 `RelayInfo.DurableImageExecution` 进行小范围日志投影，复用 `LogOther.SetAdmin` 权限容器，不改变原始错误和计费算法；列出 legacy 与 membership 的定向测试命令。其实现仍需独立代码审查及实际执行证据。

复审同时接受主方案对现有消费者 type 校验边界的纠正：构造器可补默认 type，严格原始 JSON envelope 完整性证明由生产者负责，不能把消费者当前检查描述为对原始字段的独立证明。

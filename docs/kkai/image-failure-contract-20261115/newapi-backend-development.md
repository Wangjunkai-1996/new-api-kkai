# NewAPI 图片失败诊断后端开发说明

日期：2026-11-15（Asia/Shanghai）。状态：实现和定向验证完成，等待发布集成；依据主方案 v1.0。

本说明落实 `plan.md` 与 `review-billing-security.md` 的 R1–R3。只涉及 NewAPI Go 后端和定向测试；前端由独立开发文档约束。生产部署、schema 合同与清理由发布文档负责。

## 1. 行为与范围

Sub2API 在完整终态失败证据成立时输出稳定 `image_provider_error`；NewAPI 沿用现有 `IsDefinitiveImageProviderError`、`ClassifyImageGenerationError`、`CompleteImageExecution`。本次不新增数据库字段、不改变退款算法、fence、accounting、幂等键、重试与恢复窗口。

修复后后台图片请求的错误日志有两种内容：

- `logs.content` 与系统日志继续使用固定安全摘要，例：`status_code=502, image provider request failed`。
- `logs.other.admin_info.image_upstream_error` 保存经过专门脱敏的诊断及结构化错误坐标。管理员/root 可取得，普通用户、token、自助和租户客户日志投影不可取得。

工作室 generation 错误、outbox last_error 和任务状态保持现有受控值。新增诊断不是失败证明，不参与资金或状态判定。

## 2. 管理员诊断合同

字段名固定为 `other.admin_info.image_upstream_error`。类型为 JSON 对象：

```json
{
  "message": "Image generation failed after the provider finished processing",
  "type": "upstream_error",
  "code": "image_provider_error",
  "status_code": 502,
  "truncated": false
}
```

| 字段 | 类型 | 来源与限制 |
| --- | --- | --- |
| `message` | 可选 string | 独立复制原错误正文后脱敏；最多 2048 UTF-8 bytes。空值、非法编码、无法安全解析/清理时省略，只保留安全结构化字段。policy 仅允许固定摘要。 |
| `type` | 可选 string | 上游 OpenAI error type；最多 128 bytes，只接受 ASCII 字母、数字及 `_ . : -`；缺失、含敏感精确回显、`sk-` 凭据或 `api_key:`/`password:` 等凭据表达、其他字符则省略。 |
| `code` | 可选 string | 原始 error code；仅 string，规则同 type。不把数字、对象或任意 JSON 序列化成字符串。 |
| `status_code` | 可选 number | NewAPI 实际收到的原始 HTTP status，仅保留 100–599；无有效原始 wire 状态则省略，不用映射后状态伪造来源。 |
| `truncated` | boolean | 必有。只表示通过安全清理的 message 超过字节上限并被裁剪；结构非法或无法安全清理导致省略 message 时保持 false，不能把内容遗漏误述为截断。 |

调用方/前端必须容忍所有可选字段缺失，不用原 `logs.content`、`error_message` 或任意响应字段填补诊断。新结构只增加管理员 API 内容，不改变已有公开 API 错误语义。

## 3. 接入点与最小实现

### 3.1 共享日志函数

在 `service/relay_error.go:ProcessChannelErrorAfterPolicy` 内使用已存在的 `relayInfo != nil && relayInfo.DurableImageExecution` 限定异步图片审计分支。它已由 worker 建立，不新增通用 logger 配置或 context 字段。

分支行为：

1. 原 `err` 继续供 `IsRequestCancelled`、渠道健康与原失败控制流读取。
2. 用纯函数生成独立的图片上游诊断视图；函数不写数据库、不启动 goroutine、不触发 policy 行为。
3. public/system `message` 设置固定摘要；policy 情形只使用固定 policy 摘要。普通 relay 保持原行为。
4. 创建现有 `model.LogOther` 时，使用 `other.SetAdmin("image_upstream_error", diagnostic)`。不通过 `SetPublic`，不复制到顶层或 `content_parts`。
5. 异步图片 public `error_code` 使用既有受控分类（无匹配时 `image_generation_failed`），不公开任意供应商 code；`error_type` 保留 NewAPI 内部固定类型，状态保留数字。原始上游 code/type 只在上述安全管理员字段内。
6. 系统日志始终只有摘要和已有请求/渠道坐标；不额外打印诊断结构。由于 `model.RecordErrorLog` 也记录 content，必须在进入它之前完成公开摘要替换。

诊断函数放入 `service/image_generation_diagnostics.go`，只承担“图片上游诊断投影”这个可直接测试的业务概念。复用 `common.UpstreamAddresses` 提供配置上游与代理地址，复用 `common.MaskSensitiveInfo` 的地址处理能力，但不能仅靠它处理凭据。无需修改整个 relaykit 的通用脱敏合同。

补充审计出口：`service/error.go:RelayErrorHandler` 在无效 JSON 错误体路径原先直接记录 body。使用已有 `ImageExecutionFenceFromContext` 仅识别后台图片，将此处系统日志改为状态与固定摘要；普通 relay 保持原行为。必须以定向测试验证原始 body 不出现。

### 3.2 替换初始危险补丁

`controller/image_studio_worker.go` 继续把原 `relayErr` 交给共享错误记录入口；这只有在上述 `DurableImageExecution` 安全分支完成后才可保留。删除“共享敏感脱敏即可安全”的不准确注释，说明公开摘要和管理员诊断分开生成。原错误不能 `SetMessage`，也不能通过日志副本改变 `RelayError`、code/type/original status/policy evidence。

`controller/image_studio_async_integration_test.go` 中现有期望完整 `providerDiagnostic` 出现在 `logs[0].Content` 的断言必须替换：public content 固定摘要；中文正常原因只在 `admin_info.image_upstream_error.message` 中保留；所有测试秘密值均不出现在序列化后的日志任何字段。继续验证 generation/outbox 不含原文。

原来已在仓库内实现的 provider_error 识别和立即事务退款继续使用，不为“修复日志”改写计费分支。

## 4. 脱敏规则与执行顺序

为整个诊断单独复制字符串。任何一步都不修改 `relayErr`。顺序如下：

1. **确认输入编码与来源。** 仅接受不超过 64 KiB、有效 UTF-8 的普通错误正文；不展开任意响应 JSON、请求参数或配置。整个正文是 JSON/HTML/markup、超过处理上限、结构不完整或无法安全清理时直接省略 message，不编写通用解析器。原始非法 UTF-8 输入省略 message，不截断后勉强解码。
2. **先删除精确敏感值。** 移除当前实际 `ChannelError.UsingKey` 的完整字节回显，以及配置的上游/代理地址；处理其 JSON 转义回显。空 key 跳过，不能把空串替换扩展成大字符串。多 Key 渠道只使用此次实际派发的 key，不读取其他 Key。
3. **清理常见凭据表达。** 对大小写不敏感的 Authorization/Bearer/Basic，以及 `api_key`/`api-key`/`apikey`、token、secret、credential 的冒号/等号/JSON 引号形式遮盖完整值。Bearer 的两个 token 不可只遮盖 `Bearer` 而留下后半段。处理 `sk-` 样式凭据作为补充，不能以其替代精确 key 删除。
4. **清理网络地址。** 所有带 scheme 的 URL 完整替换为固定占位，覆盖 userinfo、query、fragment 和路径；对配置的无 scheme 上游 host/地址及 IP 使用既有地址清理能力。无法安全解析的 URL 状文本不原样保留。此处不需要把文档/充值链接提供给用户，管理员诊断保留自然语言原因即可。
5. **清理控制字符。** CR/LF/tab 转为空格；移除其他 Unicode control/format 字符，避免日志伪造、ANSI 控制与双向文本混淆。无需将文本当 HTML 渲染。
6. **最后限长。** 完成脱敏后按 UTF-8 rune 边界截到不超过 2048 bytes，并设 truncated=true；不能先截断后删除 key。短正常错误保持全部自然语言原因。对精确 key 已跨越 2048 边界的输入仍必须完整删除后再限长。

无法安全清理时返回不含 message 的结构化视图。纯函数不抛出可阻止任务完成的错误；日志数据库写入失败保持既有非阻塞审计处理，不改变 worker 的终态分类和退款路径。不得在错误回退、debug log、测试失败输出中打印真实凭据。

policy 判断复用现有结构化 `ClassifyKKAIUpstreamPolicyError`、本地 policy code 和明确 content-policy 分类。这里只决定摘要/是否保留诊断 message，不执行 `ProcessKKAIPolicyAPIError`，不重复风险事件、封禁、扣费或冷却。普通 provider_error 不因为文字提及 policy 就被改成明确政策失败。

## 5. 权限投影

复用 `model.LogOther.SetAdmin`、`formatUserLogs`、`FormatAdminLogs` 与 `FormatRootLogs`。不新增权限逻辑或特殊 user bypass。

- self/user、token、tenant 客户入口继续删除整个 admin_info，因此新诊断不可见。
- 管理员日志入口保留 admin_info，root 保留 admin_info 和 root_info。
- 导出与列表使用相同权限投影；新增字段不能只在 UI 隐藏而由低权限 API 实际返回。
- 前端仅在已有管理员权限条件下显示这个可选字段，并复制已脱敏视图。后端不能把原文发给前端让前端脱敏。

## 6. 定向测试与命令

复用现有测试夹具和 `testify/require`、`testify/assert`。增加一个诊断纯函数表测和一个日志集成测试；扩充现有 worker、权限投影及计费表测，不建立新框架。

必须断言：

- 诊断秘密格式、精确 key、URL/IP、跨字节上限、无效编码与 policy 行为；正常中文原因保留；原错误对象不变。
- 实际持久化日志 public 摘要、admin 诊断和系统输出均符合合同；logger 失败不触发二次请求或阻断已确认退款。
- provider_error 原始状态白名单、mapped status、generic gateway unknown、完整输出优先、旧 fence、replay 与单 POST 保持现有合同。构造器会补默认 type，NewAPI 消费者测试不能宣称独立验证了原始 JSON type；严格原始 envelope 校验由生产者负责。
- public/self/token/tenant 投影剥离，admin/root 保留；不靠截图或前端隐藏代替后端权限断言。
- provider_error 的 ready/stale-fence 情形复用原 policy completion 表测。会员与会员钱包回退复用已存在账本测试；此任务不修改资金算法。

实际命令（从 NewAPI 根目录执行；均为定向范围）：

```sh
gofmt -w service/image_generation_diagnostics.go service/image_generation_diagnostics_test.go service/relay_error.go service/error.go service/image_async_billing_test.go controller/image_studio_worker.go controller/image_studio_async_integration_test.go controller/log_projection_test.go model/log_format_test.go
git diff --check

KKAI_MEMBERSHIP_ENABLED=false go test ./service -run '^(TestImageUpstreamErrorDiagnostic|TestImageAsyncErrorLogDiagnostics|TestImageAsyncMalformedResponseOmitsBodyFromSystemLog|TestImageAsyncPolicyCompletionPreservesResultAndFence|TestImageAsyncDefinitiveProviderErrorRefundsImmediately|TestImageAsyncUnknownErrorClassificationPreservesBillingAndFence|TestClassifyImageGenerationErrorUsesStructuredDiagnostics|TestImageGenerationViewsExposeOnlyPublicErrors)$' -count=1 -timeout=3m

KKAI_MEMBERSHIP_ENABLED=false go test ./controller -run '^(TestImageAsyncWorkerPolicyRejectionRefundsWithoutResubmitting|TestGetAllLogsProjectsDiagnosticsForOperatorRole)$' -count=1 -timeout=3m

KKAI_MEMBERSHIP_ENABLED=false go test ./model -run '^(TestImageBillingSettlementAndRefundParticipateInCallerTransaction|TestImageWorkerFenceRejectsLostEventOrLeadership|TestImageGenerationWalletBillingIsDurableAndIdempotent|TestImageGenerationSettledBillingCannotBeRefunded|TestImageGenerationSubscriptionBillingSettlesAgainstLockedLedger|TestLogProjectionProtectsTenantPricingAndStreamDiagnostics)$' -count=1 -timeout=3m

KKAI_MEMBERSHIP_ENABLED=true go test ./model -run '^TestMembershipImage(SettlementSharesDurableAccountingTransaction|WalletFallbackRefundIsIdempotent)$' -count=1 -timeout=3m

KKAI_MEMBERSHIP_ENABLED=false go test ./service -run '^(TestImageUpstreamErrorDiagnostic|TestImageAsyncErrorLogDiagnostics)$' -count=1 -timeout=3m

KKAI_MEMBERSHIP_ENABLED=false go test ./service -run '^TestImageUpstreamErrorDiagnostic$' -count=1 -timeout=3m
```

结果：gofmt 与 diff 检查通过；controller 0.545s、legacy model 0.102s、membership model 0.162s 通过。service 第一组仅 `control_reassembles_url` 测试失败：原测试 URL 已在删除控制字符前被完整遮盖，实际输出为安全的 `htt[redacted URL]`，与测试要求省略正文不一致。将输入改为 scheme 与斜线都被控制字符分隔、确实会在清理后重组 URL 的情况；同时显式固定诊断集成夹具 SQLite 类型后，两项定向重跑通过（0.052s）。独立安全 review 随后要求拒绝 metadata 中的凭据表达，新增两项 fixture；最后纯函数重跑通过（0.048s）。其余已通过且输入未变的测试未重复运行。

已逐一确认目标测试函数存在。legacy worker/service 使用显式 false，不触发会员模式的 skip；membership 测试夹具显式启用会员账本且不含 skip。未执行无目标匹配的命令。所有测试使用本地 SQLite 或 httptest，不读取生产数据。

必须显式检查 `KKAI_MEMBERSHIP_ENABLED` 两种运行模式是否实际执行目标测试。上述 `go test ./model` 不是全仓套件，只执行列出的账务边界；不运行未配置生产数据库的测试或借生产数据做测试。现有 PostgreSQL 专用夹具若不属于本次改动，不为消除 skip 而连生产。

实现如触及上述计划之外的文件，先更新此文档说明原因，再执行最小相关验证。所有通过命令在相关输入不变时不重复运行。

## 7. 审核与交付

Go 实现已完成；未修改数据库结构、状态识别、退款算法或部署配置。新增 `service/image_generation_diagnostics.go` 与同名测试，接入 `service/relay_error.go`、`service/error.go`、worker；扩充现有 worker、账务与日志权限测试。

全仓测试、全仓 lint/build 均跳过。验证缺口：未执行 MySQL/PostgreSQL 实库回归（本次无数据库实现变化），本地定向检查不能证明生产安装或发布验收；后者由主代理按正式手册完成。导出路径沿用既有权限投影，已做代码审阅，本次未另建导出集成夹具。

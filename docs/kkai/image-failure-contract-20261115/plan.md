# 图片失败协议与诊断修复方案

状态：v1.0，已吸收协议、计费/安全、发布三角色审查，进入开发文档与实现阶段。日期：2026-11-15（Asia/Shanghai）。

## 1. 目标与交付顺序

修复图片上游已返回完整失败响应，但图片工作室仍长期显示“正在确认结果”，以及 NewAPI 错误日志缺少可排查原因的问题。

用户已授权：方案核实、文档、专业审查、开发文档、开发、必要本地提交、Mac 构建、上传、正常蓝绿灰度、发布后验证和受控清理。不得创建新分支/worktree、做代码托管联网同步或将数据库维护混入发布。用户不提供登录账号，明确不要求代理登录验收；发布记录必须如实注明登录业务 E2E 未执行，使用自动化与可取得的被动业务证据，不创建账号/Key/业务数据凑验收。

按以下顺序交付，不跳过文档阶段：

1. 核实两端实际代码与事故证据，形成本方案初稿。
2. 协议架构、计费/安全、发布 SRE 分别审查；记录发现、处置和最终结论。
3. 更新并冻结方案，各开发角色据此编写对应开发文档。
4. 按开发文档实现与定向验证，再进行独立代码审查。
5. 本地提交精确源码，按现有发布手册执行两端蓝绿上线。
6. 完成延迟观察、错误归因、回滚核对、清理和交付记录。

## 2. 已确认事实与证据边界

本次已确认工作目录：

| 角色 | 目录 | 开发起点 |
| --- | --- | --- |
| NewAPI | `/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai` | `production/kkrich`，`55a55f56e7ae969b090fa475dafec1dece27cc46` |
| Sub2API | `/Users/tokk/Documents/Codex/2026-08-09/li/work/sub2api` | `release/v0.2.21-kkai`，`4d32760b5834378b6a6b9d4ac10a66f551de86db` |
| NewAPI infra | `/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/kkai-infra` | 使用当前已确认 checkout，发布前核对安装 pin |

Sub2API infra 为全局手册明确引用的 `/Users/tokk/Documents/Codex/2026-08-09/li/work/kkai-infra-warmup`，已只读验证安装控制器 `e7f898f7831a50ed9675885fd92cbdf71e306847` 与 operator-reviewed-v1 能力，不扫描历史 clone。

2026-11-15 事故，只读日志和数据库证明：

- NewAPI 请求 `202611151406355241109758268d9d6eNKLuV0q`，任务 `7734`，模型 `gpt-image-2.6-sunburst`，渠道 49 指向 `sub2api-image:8080`。
- Sub2API 请求 `f25adfe6-a717-4a6a-97a0-00137d7541cc`，关联 client request `9d805a73-1954-4d7f-ad44-d7cc724ec02f`。
- 北京时间 22:08:46，上游耗时约 130.5 秒返回 HTTP 502；Sub2API 尝试切换后因 pool=1、excluded=1 无账号可选，走最终失败出口。
- NewAPI 日志记录 `error_type=openai_error,error_code=unknown_error,status_code=502`；任务进入 unknown 核对路径。
- 22:30:38，任务完成核对超时处理，`status=unknown,billing_state=refunded,final_quota=0,execution_state=done`。
- Sub2API 通用 `handleFailoverExhausted` 经过透传或默认映射，调用不带 code 的错误序列化；NewAPI worker 又将日志正文覆盖成固定 `image provider request failed`。
- 历史路由日志另证实账户16841、account_type=apikey、endpoint=/v1/images/generations；本次只为原生 HTTP 图片路径增加终态 marker，OAuth/Responses 保守无新增 marker，仍须跨尝试 unknown 粘滞。
- 历史原始响应没有从持久化运维日志中取得。不能宣称已恢复其完整 JSON，也不能声称本次原始 code 必然是 `image_provider_error`。截图文本只作事故输入，不是协议判定条件。

上述时间/版本只作事故记录。发布时重新获取运行状态、槽位、镜像和回滚证据。

## 3. 最终错误协议

复用现有 OpenAI JSON error envelope 和 `image_provider_error`，不增加数据库字段或前端任务状态。

```json
{
  "error": {
    "type": "upstream_error",
    "code": "image_provider_error",
    "message": "Sanitized useful provider failure reason"
  }
}
```

`image_provider_error` 的含义：当前同步图片逻辑请求已取得完整终态失败证据，没有成功交付、没有已派发但结果未判明的尝试。它不声明供应商没有计算成本，不授权自动重新提交。

消费者目前接受原始 HTTP 502，或 JSON keepalive 已提交 HTTP 200 后的该结构化错误；本次保持这两个既有状态契约。须区分 provider→Sub2 的原始状态与 Sub2→NewAPI 的 wire 状态：Sub2 最终 502 或 JSON keepalive 已提交 200 才发 marker；透传规则明确映射到其他状态时保留规则并抑制 marker。NewAPI 自身的状态映射继续保留它接收的原始 wire 状态作为证据。不得把任意原始 HTTP 4xx/5xx、中文失败关键词或单独的 `X-Sub2-Retry-Status: exhausted` 当成该证据。

### 3.1 证据来源

| 输入/过程 | 能否规范化为明确失败 | 约束 |
| --- | --- | --- |
| 原生图片 HTTP 错误，完整读取的 JSON 对象，具有对象型 error、非空字符串 message/type，无图片成功输出 | 可以 | 读取无错误、未超限/截断；code 可以缺失；判断结构而非自然语言内容 |
| HTTP 200/keepalive 后完整的结构化 error | 可以 | 复用已有错误路径，补齐同样的完整性/无输出条件 |
| OAuth/Responses 明确的 failed/incomplete/error 终态且无输出 | 可以，须显式保留终态证据 | 只收到进度、delta、EOF 不能冒充终态 |
| HTML、空体、无效 JSON、错误字段类型不合法、只凭状态 502 | 不可以 | 保持未知/普通错误语义 |
| 读取超时、断连、响应截断、错误体读取超限、已派发结果不明 | 不可以 | 不重新派发；NewAPI 保持现有核对路径 |
| 批次部分成功或已有可交付结果 | 不可以用整单失败覆盖 | 保留结果与既有部分成功结算 |
| 多次尝试中任何一次已派发且未判明 | 不可以 | unknown 必须粘滞，不能由最后一次明确错误覆盖 |

实现需复用现有有界读取能力，错误体读取必须保留 read error 与超过上限的信号。现有 `readUpstreamErrorBody` 忽略 readErr，不可作为完整性证明。优先为图片路径复用 `readUpstreamResponseBodyLimited`，不把文本 relay 的读取语义一起重构。

### 3.2 生产者传递

- 在读到证据的服务层附加内部、默认 false 的明确失败标志，建议字段 `ImageOutcomeDefinitive`，不能在 handler 仅看最终错误字符串重建。false 只表示没有完整失败证明，不表示未派发。请求级和批次级分别累积已派发 unknown；任何此类尝试永久否决整单 marker。明确的派发前本地失败不产生 unknown，也不提供终态证据。
- 在 `OpenAIImagesUpstreamError` 与必要的 `UpstreamFailoverError` 间保留标志、原始错误状态及受控诊断。
- 账户重试、最后账号不可用、最大重试次数、透传规则、自定义文案、keepalive/SSE 错误序列化都不得丢标志。
- 只在图片请求的最终错误出口设置稳定 code。文本请求、鉴权、容量不足等现有专用错误码和策略错误保持独立语义。
- 透传规则可选择客户端状态/文案；不得凭自定义文案创造明确失败证据，也不得在客户端状态不满足消费端契约时伪造可消费的终态标志。自定义消息优先，默认明确失败消息必须保留有用的有界脱敏原因（最多 2048 UTF-8 字节）；空消息才退回安全通用摘要。运维日志保留受控脱敏原因，不能靠消费端恢复从未发送的文本。
- dedicated policy、credential、balance、capacity 等既有语义优先，新 marker 不修改健康判定、重试资格或渠道策略。首次 HTTP 读取必须同时保留 readErr 与 limit+1 信号；对已读取字节重新读取不能补证明。严格检查 root/error 对象、非空实际字符串 type/message；code 缺失或 null 可接受，错误体夹带成功图片不能视作整单失败。
- OAuth 是否新增终态证明以事故真实路由与开发文档证据决定；若事故属于 native，可只为 native 增加 marker，OAuth 保守无 marker。但必须覆盖全部最终出口和跨路径未知粘滞；若事故属于 OAuth，须保留 failed/incomplete/error 终态及批次 unknown/结果优先级，不得用范围收敛掩盖事故未修复。
- 任何已经写入实际图片/结果的路径按既有逻辑处理，不写第二个普通 JSON 错误体。

### 3.3 NewAPI 消费与用户展示

- 继续使用 `IsDefinitiveImageProviderError` 的结构化 code 与原始状态白名单。
- 明确失败进入现有 `CompleteImageExecution`，无可结算输出时立即事务退款、标记 failed 与 execution done。
- 断流/未知保持既有核对窗口；不缩短所有任务的恢复时间，不取消 fence、幂等键或持久化 accounting。
- 图片工作室复用受控错误码映射与 failed 展示，不展示上游自由文本，不从 HTTP 状态自行推测任务终态。
- 历史任务不批量改状态，不重新扣款，不重发请求。任务 7734 已退款，只保留事故记录。

## 4. 计费不变量

1. 先检查 execution fence，旧 worker 不得修改结果或资金。
2. 持久化 accounting 与 ready 输出齐全且数量一致时，优先 settle/partial；失败码不得覆盖真实成功。
3. 没有可结算输出的明确失败只能退款一次，最终 quota=0；事务同时完成 generation、execution、引用清理。
4. outbox 重复投递、相同幂等键再次提交、进程恢复不得产生第二次上游 POST 或第二次退款。
5. 任何未知尝试不得通过新增标志变成可自动重试，新增诊断不得改变计费决策。

## 5. 日志与敏感信息

初始日志补丁直接恢复 relayErr 正文，尚未作为最终方案批准。现有 `MaskSensitiveInfo` 的格式覆盖有限，普通日志 content 对用户可见，不能把“已有脱敏”视为完整的凭据保护。

最终方案：

- public 日志 content 与工作室保持安全摘要；详细原因放入 `other.admin_info.image_upstream_error`，通过既有权限投影仅允许管理员/root读取。字段为 `{message?,type?,code?,status_code?,truncated}`；message 最多 2048 UTF-8 字节，type/code 最多各 128 字节且仅安全标识字符，否则省略；status_code 记录 NewAPI 接收到的有效原始 wire 状态，无有效HTTP状态则省略。
- 详细诊断在进入任何持久化/系统日志之前，删除当前活动渠道 key 的精确回显，清理常见 Authorization/Bearer、JSON/等号/冒号形式的 API key、URL userinfo/query/fragment、控制字符和配置上游地址；设明确长度上限，完整保留正常短错误原因。
- 系统日志不直接输出自由文本，使用 request ID、渠道 ID、状态、结构化分类等诊断坐标。
- policy evidence 继续使用固定安全摘要，不绕过既有策略保护，不重复触发策略副作用。
- 不改写用于状态和计费判定的原始 `relayErr`，审计视图使用独立副本或独立结构。
- 管理员日志详情增加可复制的“上游错误详情”，展示有权限的脱敏原因及结构化码/状态；普通用户视图不得取得该字段。新增 UI 文案按项目 i18n 规则完整翻译。
- 脱敏顺序固定：活动 key 与配置地址精确移除 → 常见 Bearer/Authorization/API key（JSON/冒号/等号、大小写/空格变体）和 URL/IP 清理 → 控制字符及非法 UTF-8 清理 → UTF-8 边界限长。无法安全处理时仅保留安全结构化字段，绝不 fallback 原文。日志失败不能改变任务终态、资金或产生第二次 POST。
- 开发文档指定复用点，避免另建通用日志框架。

## 6. 验证矩阵与通过标准

### Sub2API

- 完整 HTTP 502 JSON error（code 缺失）最终输出稳定 code；不同 error type/message 不依赖中文文本。
- HTTP 200 内嵌失败、keepalive 200 后错误、原生生成与编辑、最终账号耗尽、次数耗尽、透传与默认映射。
- HTML、空体、非法 JSON、字段类型错误、完整 JSON 后附 read error、limit+1、无终态 SSE：不得给明确失败标志。
- 多次尝试/批次的未知优先级、部分成功不丢图片、实际写出结果后不二次输出。
- 通用文本错误、内容策略、鉴权/容量/余额专用路径无语义回归。

### NewAPI 与前端

- code/type/原始 HTTP 200/502 的立即失败退款，包括本地状态映射。现有构造器会补默认 type，因此 raw JSON 严格字段校验由生产者实现，不能声称消费端已独立证明原始 type。
- ready+accounting 优先结算、partial、staging/count 不匹配、旧 fence、重复事件与幂等重放。
- 同一 Sub2 响应 fixture 进入 NewAPI worker，验证 failed/refunded、详细诊断脱敏和请求不重复提交。
- 凭据多种格式、精确 key 回显、URL、控制字符、长度上限、policy、原 error 不被修改。
- public/self/token/tenant 不泄漏详细诊断；admin/root 可看；日志详情权限和复制行为。
- 保留现有 unknown/confirming 前端行为测试，明确失败由后端状态自然驱动失败展示。

测试使用最小相关包/入口，Sub2 handler unit 文件需 `-tags=unit`。NewAPI 显式分别使用 `KKAI_MEMBERSHIP_ENABLED=false` 和 `true` 验证 legacy worker/fence/replay 与会员/会员钱包回退结算退款，记录执行和 skip，不能把跳过测试当通过。具体命令、预估耗时和结果写入开发/验收文档。除发布强制门禁外不跑无关全仓套件，不重复已通过且输入未变的检查。

## 7. 部署与回滚

以两份全局手册和本次确认安装的控制器为执行准绳：

- `/Users/tokk/Documents/Codex/runbooks/sub2api-upgrade-and-deployment.md`
- `/Users/tokk/Documents/Codex/runbooks/newapi-upgrade-and-deployment.md`

冻结串行顺序：**NewAPI 后端 → NewAPI 前端 → NewAPI 完整观察与收尾 → Sub2API image → Sub2API 完整观察与收尾**。先建立安全诊断能力再启用生产者协议。两端独立制品/独立回滚，禁止并发 stage/canary/promote（Sub2 控制器保护非目标容器生命周期）。协议标志为 additive，不要求同时切换。

Sub2API：Mac `kkai-mirror-builder` 构建唯一版本；安装身份、迁移契约和计划核对；`plan-stage`；候选直连/健康/受影响自动化证据；`10% → 50% → 100% → complete` 各级至少120秒；按既定 v1 聚合与人工逐组归因，不伪造成功样本。用户免登录验收不等于代理声明真实登录 E2E 通过；各阶段使用自然流量真实正数 baseline/candidate samples，每120秒检查一次，单阶段最多等待30分钟。缺样本到期保留原活动版并用官方控制器停止/回退，不伪造数据。逐组复核 count/status/origin/reason/upstream_status，只有已独立证实与发布无关的错误才可走绑定 exact identities 和文件 SHA 的 operator acceptance。

NewAPI：精确 target commit 与实时只读 schema/digest/必要扩展回执决定 profile；禁止默认 bridge 或顺便迁移。后端 external 构建、stage、精确候选、三档灰度、promote，立即和五分钟后 status、等长脱敏错误窗口、60秒写入保护、回滚就绪。前端按实际已安装 format2 兼容协议或legacy format1配对路径，不凭源码推断能力。

当前源码两个 schema profile 都要求 canonical21 与 `rc49-online-v1`、`video-media-metadata-v1` 两项扩展，仍须在精确提交上重核并取得 live digest/receipts。format2 前端事务必须在后端事务完成后开始，支持字段缺失与独立回滚。candidate 禁用后台作业，worker 验收由本地回归及 promote 后自然证据承担，不能用 candidate status 冒充 worker 业务通过。writer drain 可能最长24分钟，期间保持按 recorded identities 核对，不重发已成功动作。

验收统一注明：**依用户要求，登录和真实账号业务 E2E 未由代理执行（用户接手业务测试，代理未验证）。**

任何未解决的候选回归、schema不符、身份不符或失败证据不得靠用户笼统授权绕过。恢复采用官方控制器与已记录 release/rollout/version/slot，不重复 build/stage/promote猜结果。

收尾盘点本次和既有产物，按手册保留 current/rollback/保留期/pin。删除仅限已证明无引用并有受控入口的产物；无安全入口的历史残留记录明确 disposition，不手删线上资源。

## 8. 文档与责任分工

- `plan.md`：主方案、最终约束与验收标准，主代理维护。
- `review.md`：协议、计费/安全、发布审查发现及处置，冻结方案版本。
- `newapi-backend-development.md`、`frontend-development.md`：NewAPI后端审计、幂等/计费验证及前端管理员诊断开发细节。
- Sub2API 仓库 `docs/kkai/image-failure-development-20261115.md`：生产者内部证据、最终输出、测试与兼容性。
- `release-plan.md`：精确命令、安装能力、顺序、无账号验收处置、回滚与清理门禁。
- `acceptance.md`：最终diff范围、命令结果、提交/制品、发布证据与遗留项。

专业审查通过与开发文档落地后才开始新增实现。审查发现若改变协议/安全边界，先更新本方案再编码。

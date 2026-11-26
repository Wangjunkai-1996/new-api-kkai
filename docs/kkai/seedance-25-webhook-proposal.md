# Seedance 2.5 任务 Webhook 支持方案

**本地实现，待生产试点。** KKRICH 主站的 Seedance 2.5 支持按 API Token 登记 HTTPS 接收端，按任务选择终态通知。现有视频路由限制分销站点访问，本版沿用该限制。客户文档见 [Seedance 视频生成 API](../../docs-site/docs/api/video-generation.md#seedance-25-webhook)。

## 一、客户如何接入

1. **登记接收地址。** 账号所有者在令牌管理中为业务使用的 Token 登记 HTTPS 地址，保存一次性显示的签名密钥，完成地址验证。测试和生产可以使用不同 Token。
2. **选择需要通知的任务。** 调用现有任一创建接口时加上 `X-KKRICH-Webhook: terminal`。可选头 `X-KKRICH-Client-Reference` 原样回传，适合填订单号或素材流水号，限 1–128 个 ASCII 字符，允许字母、数字、点、下划线和连字符。两个头都由网关消费。创建响应仍以 `id` 为公开任务 ID，并给成功登记的请求返回 `X-KKRICH-Webhook: registered` 响应头。
3. **接收通知。** 收到签名的 JSON POST 后，先验签，再将事件持久化。相同 `event_id` 只处理一次，已处理的重复通知也返回 2xx。视频下载与业务更新可在响应后执行。
4. **补查或下载。** 保留创建时的 `id`。通用接口查 `GET /v1/video/generations/{task_id}`，兼容接口查 `GET /v1/videos/{task_id}`。成功后使用任务所有者的有效 API Token 请求 `GET /v1/videos/{task_id}/content`。客户等待超时或创建响应缺少登记回执时，优先查原任务。业务单号仅用于关联，重新提交可能产生第二笔任务和费用。[现有公开 API](../../docs-site/docs/api/video-generation.md)

**首版覆盖范围**为上述两套公开 API 的 JSON 创建请求，模型为 `seedance-2.5`、`sd_2.5_special_720p`、`sd_2.5_special_1080p`、`sd_2.5_special_720p_with_video_ref`、`sd_2.5_special_1080p_with_video_ref`。范围依据客户提交的公开模型名确定。Web 工作室有独立的资产归档和下载权限，单独评估。进度仍可通过查询接口取得。

**提交示例**

```bash
curl -i https://api.kkrich.ltd/v1/video/generations \
  -H "Authorization: Bearer $KKAI_API_KEY" \
  -H 'Content-Type: application/json' \
  -H 'X-KKRICH-Webhook: terminal' \
  -H 'X-KKRICH-Client-Reference: order-5821' \
  -d '{"model":"sd_2.5_special_720p","prompt":"海边灯塔，镜头缓慢推近","duration":5,"ratio":"16:9","generate_audio":false}'
```

省略两个头按现有接口处理。显式请求通知时，未配置或未验证接收端返回 `409 webhook_endpoint_not_ready`。只传业务单号、重复或无效的头值、选用范围外的模型，在这两个创建入口返回 `400`。全局暂停新登记时返回 `503 webhook_unavailable`。这些检查在可能产生费用前完成。客户提交的 JSON 仍遵循原有模型字段约束。

## 二、回流字段和事件语义

以下是 v1 业务事件的完整字段。相同事件在自动重试和人工重投期间保持相同的 ID 和正文字节。

| JSON 字段 | 类型 | 出现条件 | 契约 |
| --- | --- | --- | --- |
| `version` | integer | 必有 | 固定为 `1`。客户端容忍新增的可选字段 |
| `event_id` | string | 必有 | 平台分配的公开事件标识，用作接收方去重键 |
| `event_type` | string | 必有 | `video.completed` 或 `video.failed` |
| `task_id` | string | 必有 | 与创建响应的公开 `id` 一致 |
| `client_reference` | string | 提交时指定 | 客户提交的业务单号 |
| `model` | string | 必有 | 客户使用的公开模型名，保留兼容别名 |
| `status` | string | 必有 | `SUCCESS` 对应 completed，`FAILURE` 对应 failed。两种提交格式共用此回调枚举 |
| `finished_at` | integer | 必有 | 平台记录的任务结束时间，Unix 秒。每次投递的时间另见签名头 |
| `result_url` | string | 成功 | 本站鉴权下载入口。地址本身不包含 Token |
| `error_code` | string | 失败 | 平台公开错误分类，例如 `content_policy_violation`。未知分类回退 `generation_failure` |
| `fail_reason` | string | 失败 | 可展示给用户的简短原因。没有可公开的说明时使用通用失败文案 |

**成功事件**

```json
{
  "version": 1,
  "event_id": "evt_5acc0476a2084cab9e1f89e7deef2608",
  "event_type": "video.completed",
  "task_id": "task_example01",
  "client_reference": "order-5821",
  "model": "sd_2.5_special_720p",
  "status": "SUCCESS",
  "finished_at": 1795651240,
  "result_url": "https://api.kkrich.ltd/v1/videos/task_example01/content"
}
```

**失败事件**

```json
{
  "version": 1,
  "event_id": "evt_82a0486d68f7457595797d2ef0874c63",
  "event_type": "video.failed",
  "task_id": "task_example02",
  "model": "seedance-2.5",
  "status": "FAILURE",
  "finished_at": 1795651300,
  "error_code": "content_policy_violation",
  "fail_reason": "Video generation failed the content policy check."
}
```

**成功的含义是网关已确认生成完成，并提供本任务的下载入口。** 现有下载接口在请求时检查归属和状态，再访问媒体。客户遇到临时 5xx 可重试下载。联调验收应实际读取视频字节。首版没有永久存储或下载可用时长承诺。[下载处理](../../controller/video_proxy.go) · [公开结果边界](../../model/task_result.go)

**失败以已受理任务的最终状态为准。** 排队、生成中、`UNKNOWN`、无法判断上游是否已受理、单次网络超时，均继续等待可确认结果。平台超时清理若已把受理任务持久化为 `FAILURE`，发失败事件。同步鉴权失败、参数错误、余额不足且未受理等由创建接口直接返回。只要后续恢复出有效受理记录与终态，就按该终态通知。通知内容只描述生成结果，费用和退款以账务记录为准。[超时与轮询](../../service/task_polling.go) · [受理记录恢复](../../service/task_billing_recovery.go)

## 三、为什么在网关做

| 已核实的代码行为 | 对设计的影响 |
| --- | --- |
| 两套创建入口在不同的路由链执行鉴权、插件准备和分发，最终进入任务提交。Seedance 特价版对上游 JSON 字段有明确白名单 | 在两个公开创建入口、鉴权之后和插件读取请求之前消费平台头，再通过服务端上下文传到任务创建。上游只接收原有模型参数。[通用视频路由](../../router/video-router.go) · [兼容视频路由](../../router/task-plugin-protocol-router.go) · [Seedance 请求投影](../../plugins/tasks/sora/plugin.js) |
| 临时任务先落库，然后预扣、声明本次提交、调用上游。模型可以在提交时直接返回终态。崩溃后还可能通过受理凭证恢复 | 在首次创建任务的事务里保存回调订阅。由后台读取已经提交的任务状态。覆盖即时完成、轮询、超时和恢复，也覆盖客户未收到 HTTP 响应的情况。[首次建任务事务](../../relay/relay_task.go) · [受理与状态更新事务](../../model/task_billing.go) |
| 用户设置里的 Webhook 服务于账户通知，有通用消息信封、个人通知限额和可选 Worker 转发 | 视频回调使用独立的端点、密钥和 JSON 契约。可复用 HMAC 和受保护拨号的实现经验，避免把账户通知送往客户业务系统。[账户通知](../../service/user_notify.go) · [通知发送器](../../service/webhook.go) |
| `kkai_outbox` 有唯一事件键、领取租约、失败退避、死信和原记录重投。现有 Webhook 执行通道还处理充值、发票和商城事件 | 共享表和处理器，给视频回调用独立主题和有并发上限的后台作业，慢客户不会占用支付通知的执行名额。[事件模型](../../model/kkai_risk.go) · [任务处理器](../../service/kkai_outbox.go) · [现有后台分工](../../service/kkai_outbox_worker.go) |

## 四、后端如何落地

### 端点登记和隔离

- 在现有 `/api/token/:id` 管理路由下新增 `video-webhook` 子资源。`PUT` 创建待验证版本，`GET` 查看地址和状态，`POST .../verify` 发验证请求，`DELETE` 撤销该令牌的接收授权。管理动作使用账号管理权限并核对租户、用户与 Token 的归属，执行限速、审计和禁止缓存。模型 API Token 只用于提交任务。涉及明文密钥的一次性响应要求有效的交互会话并沿用敏感操作保护。[令牌管理路由](../../router/api-router.go) · [令牌归属检查](../../controller/token.go)
- 平台为每个端点版本生成 192 位随机 ID，以独立的 256 位主密钥通过 HMAC-SHA256 派生客户签名密钥。派生输入绑定协议、租户、用户、Token、版本 ID 和原始 URL。数据库仅保存主密钥版本，客户密钥仅在创建响应中显示一次。密钥缺失时暂停该功能并告警。普通令牌详情和用户设置只返回脱敏状态。[项目中的版本化密钥存储示例](../../service/shop_secrets.go)
- 验证使用同样的签名和出站限制。向新地址发送独立的 `webhook.challenge`，含本次 HTTP 交换的单次随机 `challenge`（总时限 5 秒），接收端验签后以 2xx JSON 原样回传该值。在本次请求内收到正确回显，且事务内重验待验证版本与归属后，事务性切换为当前接收端。普通业务通知只要求 2xx。
- 改址或常规轮换创建新版本。验证成功后新任务绑定新版本，旧任务继续向原已授权版本投递。客户可以立即撤销旧版本（管理接口应支持指定版本），紧急撤销整个 Token 端点时停止所有版本的后续尝试。已发出的 HTTP 请求可能仍然到达。密钥轮换期间客户按版本保留所需旧验签密钥，撤销后通过原查询接口补查。

### 数据与事务

新增两张表。这样能够按 Token 管理版本、只扫描选择回调的任务，并让账务恢复与 HTTP 投递各自独立。实际迁移需覆盖 SQLite、MySQL 5.7.8+、PostgreSQL 9.6+。

| 存储 | 首版所需内容 |
| --- | --- |
| `video_webhook_endpoints`（新增） | 不可变版本 ID、租户/用户/Token、URL、主密钥版本、待验证/启用/退役/撤销状态、创建和更新时间。切换时锁同一 Token 行，确保每个 Token 只有一个接新任务的版本 |
| `video_webhook_tasks`（新增） | 以任务行主键为唯一键，通过不可变端点版本关联租户/用户/Token，保存端点版本、公开模型名、可选业务单号、公开事件 ID、处理状态和下次检查时间。给处理状态和下次检查时间建联合索引，支持批量取到期任务。按归属和公开事件 ID 查询投递记录 |
| `kkai_outbox`（已有） | 新主题 `video.webhook.terminal.v1`。唯一事件键按内部任务行 ID 派生，如 `video-webhook-terminal:<row_id>`，成功和失败共享同一键。一条记录保存端点版本引用与固定的客户正文，公开事件 ID 不暴露内部行号。已有列承载投递状态、失败次数、下次重试、租约和投递时间 |

1. **提交。** 只有显式选择通知的两个公开创建入口才打上服务端标记并登记订阅。校验模型、地址归属和授权后，利用 `persistProvisionalTask` 的现有事务，把新任务与订阅一起写入。事务内重验端点状态，原端点若在此期间被替换或撤销，返回 409 并回滚新建任务。订阅落库失败时在预扣与上游调用前结束。内部重试复用原任务和端点版本。Studio 也使用同一个持久化钩子，因此只在公开路由安装回调逻辑。[提交事务与预扣顺序](../../relay/relay_task.go) · [Studio 的持久化钩子](../../controller/video_studio_relay.go)
2. **发现终态。** 后台按索引每批查询最多 100 条到期订阅，逐条在短事务内认领并读任务状态，核对三层归属、持久化受理证据及终态。正常每 20 秒复查，UNKNOWN 每 5 分钟复查，观察失败的记录错后 1 分钟再试。该作业只读现有任务结果，不去上游轮询，也不触发扣款。未受理且已明确关闭的记录结束观察。冻结时间以真实 `FinishTime` 为准，缺失或数据矛盾进入可定位的异常记录。
3. **形成事件。** 在一个事务内锁定订阅、重验状态、生成固定正文、插入唯一 Outbox 记录，再把订阅标为已入队。事务失败则保留待观察状态。在事务提交后宕机，发送作业仍可接续。重放或多个观察者竞争均复用原事件。无需在每个生成或退款分支增加一次 HTTP 副作用。[已有事务入队接口](../../service/kkai_outbox.go)
4. **限定对外字段。** 成功时核对结果可取回和公开 API 来源，用受信任的本站地址与公开任务 ID 构造 `/content`。遇到已丢弃结果、Studio 托管关联或归属不符时转异常处理。失败分类取平台允许公开的枚举，原因按平台分类映射为固定文案，未知分类用通用说明。正文用专用 DTO 和 `common.Marshal`，禁止复制整个任务或上游响应。这样客户只会收到第二节列出的字段。[下载 URL 构造](../../relay/channel/task/taskcommon/helpers.go) · [失败信息投影](../../model/task_failure.go) · [托管资产识别](../../service/task_video_media.go)
5. **投递。** 新主题使用已有 `KKAIOutboxProcessor`，在独立、受 leader 租约管理的后台作业中运行。发起 HTTP 前重新核对端点版本、租户、用户及 Token 的授权，已撤销、已删除或显式禁用时停止投递并记录原因。余额用尽与任务授权撤销分开判断，最后一次付费请求也应收到通知。网络失败只更新投递状态。[作业注册](../../service/kkai_runtime_jobs.go) · [后台作业接口](../../service/background_job.go)

采用定向检查会增加一次发现延迟，也会增加数据库读取。容量可按“待处理订阅数 ÷ 平均复查间隔”估算每秒检查的记录数，再按批量大小估算查询次数。对当前存在多处终态写入的实现，这个取舍方便验证提交即完成、恢复和超时清理都能产生同一份通知。若试点测得时延或查询成本超标，再评估在终态事务中直接入队，并保留针对未完成订阅的补偿检查。

## 五、验签、重试与排障

**签名。** POST 使用 `Content-Type: application/json`。请求头 `X-KKRICH-Webhook-Key-Id` 标明端点密钥版本，`X-KKRICH-Webhook-Timestamp` 是本次投递的 Unix 秒，`X-KKRICH-Webhook-Signature` 是 `v1=<小写十六进制摘要>`。以一次性显示的密钥字符串的 UTF-8 字节作为 HMAC-SHA256 密钥，对 `ASCII(timestamp) + "." + 原始 HTTP 正文字节` 签名。客户按 Key ID 从自己保存的密钥中选择，校验时间偏差不超过 300 秒，用常量时间比较摘要，再解析 JSON。每次重试使用新的投递时间和签名。全程使用 TLS，接收端不会收到客户的模型 API Token。

下例可用 Python 标准库离线核对。实际接收时，`raw` 取自 HTTP 原始请求体，`now` 取接收端当前 Unix 秒。示例中的密钥和固定时间仅用于测试。签名验证成功的 challenge 响应为 `{"challenge":"demo-01"}`。

```pycon
>>> import hmac
>>> import re
>>> def verify(secret, timestamp, signature, raw, now):
...     if not re.fullmatch(r"[0-9]{1,12}", timestamp) or abs(now - int(timestamp)) > 300:
...         return False
...     if not re.fullmatch(r"v1=[0-9a-f]{64}", signature):
...         return False
...     signed = timestamp.encode("ascii") + b"." + raw
...     digest = hmac.digest(secret.encode("utf-8"), signed, "sha256").hex()
...     return hmac.compare_digest(signature, "v1=" + digest)
>>> raw = b'{"event_type":"webhook.challenge","challenge":"demo-01"}'
>>> signature = "v1=3102bea5596271d001d40b8c26b45614ab1d0e901b3a89aa36a44046ef45bff1"
>>> verify("demo-key-for-offline-use", "1795651242", signature, raw, 1795651242)
True
>>> verify("demo-key-for-offline-use", "1795651242", signature, raw + b" ", 1795651242)
False
>>> verify("demo-key-for-offline-use", "1795651242", signature, raw, 1795651543)
False

```

**接收顺序。** 客户在同一数据库事务内写入唯一 `event_id` 和待处理业务，再返回 200 或 204。若落库失败，返回 5xx 以便重投。一个事件可能投递多次，尤其是接收端已落库而 2xx 在网络上丢失时。下载视频放在接收事务之后。事件可能早于客户处理创建响应，可用 `client_reference` 匹配，或按 `task_id` 暂存。

| 平台观察 | 投递行为 |
| --- | --- |
| 2xx | 记为已送达 |
| 连接超时、临时 DNS/网络错误、408、429、5xx | 自动重试。沿用 Outbox 默认值，首次失败后约 5 秒起指数退避，单次间隔最多 1 小时，累计 12 次失败进入死信。实际到达时间受排队影响 |
| 3xx、其余 4xx、目标违反安全策略或授权撤销 | 停止自动投递，保存可排查的原因。对配置错误可修复接收端后人工重投，对撤销的版本持续阻断 |
| 客户长时间未收到通知 | 用所属账号按任务 ID 或事件 ID 看投递状态和最近一次结果，同时用原有 GET 接口补查任务。对允许重投的死信使用原记录、原事件 ID 和原目标，记录操作者与原因 |

**出站保护。** 登记、验证与每次投递均限制为公网 HTTPS（首版 443）、有效 TLS 证书和允许的公网地址。URL 允许路径，拒绝用户信息、查询串和片段。解析与实际拨号共同校验所有候选 IP，禁用跳转，使用固定的严格出站策略。现有受保护拨号逻辑可复用，其可配置的私网例外、环境代理与通用 Worker 路径需从该发送器排除。单次总超时 5 秒，限制响应读取，验证按账户限速，首版业务投递作业全局串行执行，每批最多 10 条。投递记录保留尝试次数、重试时间、送达时间和错误类别。[受保护拨号实现](../../service/protected_fetch_client.go) · [现有通用发送器](../../service/webhook.go)

**支持与保留。** 令牌所有者仅可读本 Token 的任务、事件、最后一次投递结果和下次重试时间。通过 `GET /api/token/:id/video-webhook/deliveries?task_id=...` 提供查询。试点由有权限的运维人员按主题、归属、端点状态和死信状态校验后重投，记录审计。同一重投操作使用同一操作键。首版按 30 天阈值分批清理已结束记录，延迟或待处理记录保留至处理完成；停用版本在其未结任务和允许重投的记录释放后清理密钥。复用现有 Outbox 重投时需核对主题与账务历史保护规则。[原记录重投逻辑](../../service/kkai_outbox_redrive.go)

## 六、交付顺序与验收

| 阶段 | 交付与通过条件 |
| --- | --- |
| 1. 冻结客户契约 | 与试点客户确认每个 Token 的接收地址、业务单号格式、两个事件样例和签名规则。交付参考验签代码、签名测试向量和挑战示例，客户可以离线验证 |
| 2. 后端与数据库 | 实现版本化端点、两套入口、订阅观察、Outbox 投递和归属受限的排障查询。使用 GORM 和项目 JSON 包装，三种数据库验证迁移、事务回滚、并发唯一性和重复领取。评估到期索引及按试点流量测得的队列处理能力 |
| 3. 隔离联调 | 使用模拟上游与受控接收端跑下表。通过后在交互式令牌管理加入入口和客户文档。可先通过管理 API 与试点对接 |
| 4. 灰度开通 | 先完成单独授权的 schema 扩展及新旧版本兼容验证。后端按项目 runbook 发布，验收时使用受控接收端。待所有服务写入方及有权运行作业的版本均兼容后，仅给试点 Token 开放登记。监测达标再扩大范围 |
| 5. 故障与退回 | 支持独立暂停新登记和暂停对外发送。暂停发送时保留订阅及待发事件，恢复后按原 ID 续投。回退到不识别此功能的版本前，先关闭新登记并处理或明确保留在途任务，通知试点使用查询补查。旧版本兼容期间保留扩展表和未过期密钥 |

| 必测场景 | 验收结果 |
| --- | --- |
| 两个创建入口 × 720p 别名/正式模型，1080p 和视频参考变体 | 公开任务 ID 与回调一致。上游看到的请求体、头和计费参数不含平台回调字段。普通请求、Studio 和其他模型维持其既有行为 |
| 普通成功、提交即成功、失败、超时落失败、受理凭证恢复 | 已确认的每个受理终态恰好生成一个逻辑事件。事件字段和查询状态一致。成功案例用同账号 Token 取得视频字节，另一账号无权下载 |
| 未受理、未知后恢复、响应丢失、并发观察、入队事务故障、进程重启 | 未受理无业务事件，未知状态不误报；恢复后生成正确事件。订阅和事件均可追溯，无重复生成、扣款或退款 |
| 签名有误、正文被改、时间过期、密钥轮换、挑战重放 | 接收端拒绝不可信请求。合法重试使用新时间、原事件 ID 和固定正文，旧任务只用已授权的端点版本 |
| 客户落库后断连、429/5xx、永久错误、死信重投 | 客户业务只执行一次。投递失败不修改生成或计费状态，人工重投保留原有归属与审计 |
| 跨租户管理、暂停/撤销、私网解析、重定向、慢接收端 | 阻止不合规投递与越权查询或重投。慢端点不会阻塞既有账务、发票或商城通知，终态事件积压可观测 |

**本地验证。** 两个真实创建路由使用内置 Sora 适配器和本地模拟上游，核对五个模型名、登记回执、上游请求隔离、同账号下载字节与跨账号 404。后台作业使用受控 TLS 接收端验证 challenge、暂停后续投及事件送达。另覆盖受理恢复、失败字段、429 重试、重复工单和版本撤销。迁移和唯一约束在 SQLite、MySQL 5.7 和 PostgreSQL 9.6 验证，后两者额外验证了请求已建立数据库快照后并发撤销端点。客户网络和生产时延留待试点测量。

**试点观察指标。** 统计终态落库到首次尝试的 p50/p95、首次及最终 2xx 比例、最老待发年龄、死信数、长期未确认任务数，再对照视频创建与原有通知的延迟。建议以受控接收端正常应答时首次尝试 p95 在 60 秒以内作为试点验收目标；对外时效以实际联调结果为准。

## 七、启用与维护

| 配置 | 用途 |
| --- | --- |
| `KKAI_VIDEO_WEBHOOK_ENABLED=true` | 开启可选功能。缺省关闭。外部管理 schema 的进程启动时检查表、列及索引，失败时记录错误并关闭本进程的 Webhook |
| `KKAI_VIDEO_WEBHOOK_TOKENS` | 逗号分隔的试点 Token 数字 ID。空值关闭新登记，显式 `*` 开放全部主站 Token |
| `KKAI_VIDEO_WEBHOOK_KEY_ID` | 新端点使用的主密钥版本名 |
| `KKAI_VIDEO_WEBHOOK_KEYS_FILE` | 挂载一行 JSON，格式为 `{"v1":"<32 字节随机密钥的标准 Base64>"}`。独立安装也可用 `KKAI_VIDEO_WEBHOOK_KEYS` 提供同样的 JSON。各实例使用相同的映射。轮换时增加版本并切换 KEY_ID，旧版本保留至在途任务及重投窗口结束 |
| `KKAI_VIDEO_WEBHOOK_PAUSE_REGISTRATION=true` | 暂停端点创建、验证及新任务订阅。已启用功能的查询和撤销仍可用 |
| `KKAI_VIDEO_WEBHOOK_PAUSE_DELIVERY=true` | 暂停业务事件投递，队列保留。紧急停用具体端点可直接撤销该版本。环境变量变更随相应进程重启生效 |

下载链接使用后台的 `ServerAddress`，必须是主站 HTTPS origin。验证和业务投递都必须能从实际发送节点访问客户的公网 HTTPS 443 端口。

**Schema 和发行。** 应用提供 `kkai-migrate --video-webhook-expand plan --json` 和 `check --json`（完整调用为 `kkai-migrate --video-webhook-expand check --json`）。它们支持独立迁移进程的现有 DSN 输入方式。plan 给出所连数据库中的 webhook 表状态及编译计划摘要，check 校验完整结构。外部管理模式只允许 PostgreSQL 的独立授权 apply，要求当前应用契约及其既有扩展、固定只读挂载 `/run/video-webhook-expansion/proof.json`、数据库身份、计划摘要和 active/rollback/writer 进程证据。PostgreSQL 在共享迁移锁下使用一个事务创建表并复验结构。Webhook 是可选扩展，兼容性检查以两表的实际结构为准。生产控制器的对应接线及版本化运维手册需在生产迁移前完成和验收。本次在本地验证数据库兼容性。运行时自管数据库仅在显式开启功能且节点有迁移职责时创建缺失表。

**重投。** 平台 root 交互会话可向 `POST /api/video-webhook/events/{event_id}/redrive` 提交 `{"redrive_key":"ticket-5821","reason":"receiver repaired"}`。每次操作使用唯一工单键，同一操作重试复用原键，重投操作记录保留 30 天。已送达或待发返回 `applied:false`。重投死信前重验主题、版本和归属，成功后继续使用原正文。控制台按公开事件或任务定位记录。试点期通过投递查询与后台作业日志检查积压、拒收和未知终态，首次投递时延需在真实接收端联调中测量。

**给客户的答复**

> Seedance 2.5 的终态 Webhook 已完成开发，正式开通时间以试点联调为准。开通后，您可以为 API Token 登记 HTTPS 地址，在创建视频时按任务订阅成功或失败通知。通知会带上任务 ID、可选业务单号、模型、状态和结束时间。成功附鉴权下载地址，失败附错误码和原因。您可先按文档联调验签，现有任务查询接口也可继续使用。

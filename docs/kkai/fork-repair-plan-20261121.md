# KKAI fork 审查后的修复方案

2026-11-21 · 本地应用基线 `production/kkrich@6b76edd7c` · 方案已对照源码复核，当前工作区已完成代码落地并通过定向验证，待提交与环境级验收。本文供任务服务、会员支付、图片 Relay 开发及验收人员使用。依据来自本地源码与定向验证，线上发生次数和历史资金影响尚无结论。

## 本轮实施记录

| 模块 | 实施和本地验收 |
| --- | --- |
| 任务与账务 | 批量轮询按渠道和受理凭据分组，回包先排除未知或重复编号，再匹配本地行。待受理任务留给提交流程，避免把本地任务号发给上游。成功时把最终应收和终态一起落库。[轮询回归](../../service/task_polling_scope_test.go) |
| 提交与恢复 | 首次受理、恢复和拒绝后重置派发状态时，均在数据库行锁内核对渠道与凭据。任务换绑后，旧尝试收到成功回执、HTTP 400 或发送前错误，都保留新尝试的派发状态和预留额度。恢复队列保留归属有效的首份回执及其金额；旧回执失效后，核实新回执归属即可替换。[提交竞争](../../relay/relay_task_reliability_test.go) · [回执重放](../../service/task_billing_recovery_receipt_test.go) |
| HTTP 与媒体 | 插件轮询的请求、解析和用量钩子支持取消。内置 Sora 单任务状态元数据按解压后 1 MiB 限制读取。Vertex 以超过 1 MiB 的有效 MP4 验证轮询及逐字节下载。Vertex 的最大合法响应和并发峰值尚待测量，本轮仅对 Sora 启用新上限。[插件预算](../../relay/channel/task/jsplugin/adaptor.go) · [Vertex 下载用例](../../controller/task_vertex_inline_video_test.go) |
| 凭据与归档 | 需要提供商认证的路径使用该次受理的凭据。密钥缺失时保留归档源并推迟一分钟重试。插件可从任务内已存数据投影公开 CDN 链接，免认证下载保留原有地址校验。[归档流程](../../service/video_asset_pipeline.go) · [下载回归](../../controller/task_generic_test.go) |
| 前台查询 | 同步图片按用户、平台和公开任务号重读已提交行，覆盖完成、失败、断开和超时。Gemini/Vertex 本次查询遇到上游错误时也重读；后台若已完成，前台直接返回该结果。[图片等待](../../controller/plugin_protocol_image_poll_test.go) · [视频查询](../../relay/task_result_visibility_test.go) |
| 会员异常队列 | 超管按异常状态及游标查询；普通用户与普通管理员通过真实路由验证权限。前端用旧版回包验证整页重复、部分重叠、停止提示和原游标重试。查单或处置成功，两个列表均重新请求，避免使用操作前的在途响应。[权限](../../router/membership_test.go) · [队列交互](../../web/default/src/features/membership/membership.test.tsx) |
| 图片及渠道诊断 | 普通 OpenAI 图片增加脱敏超发观测；对照实发数量、上游返回和账本验证现有计费、退款与重试规则。管理员渠道测试改用带边界检查的额度转换。[图片账本合同](../../controller/relay_image_billing_contract_test.go) · [渠道额度](../../controller/channel_test_internal_test.go) |

双渠道同号用例通过内置 Suno 插件请求两个独立的本地 HTTP 服务。两名用户各有 1,000 额度，各预扣 100。成功渠道返回两段音频后实扣 150，余额 850。失败渠道退款 100，余额恢复至 1,000。第二轮轮询后，每个渠道累计收到一次请求，每个用户对应的任务有一条结算或退款审计事件。[SQLite 与 HTTP 集成用例](../../relay/task_suno_polling_integration_test.go)

## 实施顺序与责任

| 顺序 | 牵头角色 | 交付及完成条件 |
| --- | --- | --- |
| 1 | 后端任务开发，账务开发复核 | 已完成任务归属、终态账务和同步图片等待。两个渠道复用编号时分别入账，后台先完成时前台能读到正确结果。 |
| 2，可与 1 并行 | 会员支付后端、管理端前端 | 已完成超管找回并处理旧异常单，超过一页的异常积压可遍历。 |
| 3 | 任务插件与媒体开发 | 已完成在途取消、HTTP 状态、连接关闭、渠道配置和合法大媒体轮询归档验证。 |
| 随对应模块交付 | 前端、图片归档、渠道诊断开发 | 已修复两项可复现的测试失败，并保护管理员渠道测试日志的额度换算。 |
| 单独交付 | 图片 Relay 与计费开发 | 已按确认的产品范围补充现行收费合同用例和脱敏超发观测。 |

本轮基线上的修复可以直接在现有代码上开发。自定义图片尺寸已由 `eefbd30e7` 等价吸收，图片批处理恢复快照的主要能力已拆入 `e7e67a3f4`、`83afb3d15`。认证桥接已有后续加固提交 `7d2cb6ca6`。以上历史分支均无整支移植需求。

## 验收边界

本地回归使用隔离数据库和可控上游。线上发生频率、历史资金影响、真实厂商时序、历史后端二进制联调以及 Vertex 最大样本的并发峰值，仍缺环境数据。

另在凭据读取复核中发现，旧版渠道缓存刷新读取多密钥轮询游标时持有的锁，与派发更新游标时持有的锁不同。此次凭据解析仅读取密钥配置，存量游标的竞争情况未做专项竞态验证。[缓存刷新](../../model/channel_cache.go) · [派发选 key](../../model/channel.go)

## 一、跨渠道任务归属与结算

**故障链。** 同一平台内，轮询先按 `upstreamID` 建一张任务表，再按渠道保存 ID 列表。渠道 A、B 各有一个编号相同的任务时，后写入的本地记录会覆盖前一条。A 查到的终态可能写到 B 的任务，后续结算或退款也可能选错账本。数据库快照比较更新（CAS）保护的是已经选中的行，行归属必须在查询和回写之前确定。[任务分组、批量回写和单任务轮询](../../service/task_polling.go) · [任务模型及旧数据回退](../../model/task.go) · [账务条件更新](../../model/task_billing.go)

1. **将查询绑定到本地行。** 在 `RunTaskPollingOnce` 从原始任务按平台、渠道分组，每项保留本地行 ID、上游 ID 和渠道。单查携带该行副本及对应的查询配置。批量和 Suno 只用本次实际发出的任务构造索引。保留渠道间并发、渠道内节奏及旧任务的 `GetUpstreamTaskID()` 回退。会员派发中尚无上游回执的记录继续交给既有跳过或恢复路径。
2. **核实凭据作用域。** 批量请求和解析共用同一批的适配器配置、凭据、请求头与代理。一个批次只含能以同一凭据查询的任务。支持按任务保存凭据的渠道沿用既有绑定。多 key 渠道的 `ch.Key` 可能包含整份列表，历史任务也未必保存当次 key。优先使用该协议已有、能确认归属的查询方式。确实无法确定时暂缓受影响项并记录本地行和渠道，正常任务继续。不新增明文凭据存储，不记录 key。[任务凭据](../../model/task.go) · [渠道密钥模型](../../model/channel.go) · [批量查询适配器](../../relay/channel/task/jsplugin/adaptor.go)
3. **在回包仍是数组时判定歧义。** 请求侧按本地行去重，同批、同凭据内两个本地行共享上游 ID 时暂缓这两行。响应侧在 `ParseBatchResult` 调用用量钩子和转成 map 之前核对实际发出的 ID 集合。跳过未知、空及重复 ID 的全部返回项，保留其他唯一项。否则后出现的相同 ID 会覆盖前项，服务层已经看不到冲突。服务层写入仍校验本批绑定。通用批量回包若缺少必需状态或给出协议不支持的状态，仅跳过该项。保留 Suno 合法的增量回包，以及用量无法确认时既有的待核查语义。单查允许协议省略 ID 或使用编码后的操作名，仅在协议定义了可比较的标识时核对。后台一次只读取有限数量的任务，上述同批检查不能证明历史 ID 全局唯一。[批量解析与用量提取](../../relay/channel/task/jsplugin/adaptor.go)
4. **按数据库接受的状态转换发起账务。** 在单查、通用批量和 Suno 复核成功、失败及退款分支。只有非终态首次进入终态，且快照条件更新成功，才发起该终态的结算、退款和统计。CAS 也允许某些同终态的元数据更新，重复观察、陈旧快照、写失败均不得再次发起终态账务。Suno 既有非会员按失败原因退款的兼容分支单独验证触发条件与去重。受持久账务管理的任务复用既有 outbox 和账务记录恢复状态已写、资金尚未变动的中断。凡成功实扣可偏离预留，均要验证恢复能读到正确目标额度。若该路径未保存目标，在同一终态事务内保存已有恢复机制需要的数据后再交付。旧任务的空 `BillingState` 会直接退出恢复器，有争议的历史差额按凭据对账。[账务条件更新](../../model/task_billing.go) · [账务恢复](../../service/task_billing_recovery.go)
5. **让同步调用者拿到已提交的终态。** `waitTaskPluginImageTask` 检查提交时传入的对象，而默认轮询复制该对象后写库。一次轮询返回 nil 也不代表该项已完成。等待循环应复用 `deps.loadTask`，按发起用户、平台及公开任务号读取，核对本地行 ID 和渠道，再把读到的记录返回给处理函数。每轮复用最近一次读到的快照，只轮询仍在进行的任务。轮询后读库，即使后台抢先完成或本次 CAS 未胜出，也可用已存的图片数据渲染成功响应，或返回已存的失败原因。保留原请求截止时间，客户端断开只结束等待，后台接续同一任务。同步入口的截止时间依赖第三节的在途请求取消，二者一起验收。Gemini/Vertex 按需刷新也应在终态或 CAS 竞争后使用该行的已存数据，沿用已有权限与脱敏输出。[图片协议等待](../../controller/plugin_protocol_image.go) · [现有归属查询依赖](../../controller/plugin_protocol.go) · [按需 Relay 刷新](../../relay/relay_task.go)

**交付前必须打通的三组用例**

| 入口 | 数据与可观察结果 |
| --- | --- |
| 调度与插件批量解析 | 同平台 A、B 两个渠道及不同用户复用一个上游编号，A 成功、B 失败。断言实际请求的渠道与凭据、各行结果、A 一次实扣及 B 一次退款。再给同批的一个 ID 返回相反终态并颠倒顺序，两次都只处理其他唯一项。夹带的未请求 ID 不调用用量钩子。覆盖同渠道不同已绑定 key、输入歧义及旧 ID 回退。 |
| 前台图片等待与按需刷新 | 在本地测试库用默认轮询函数和可控上游，从异步提交走到同步响应。分别验证图片成功、失败、后台抢先提交、客户端断开和真正超时。成功响应中有对应图片，已落库的成功不会误报 504，各路径都只提交一次。保留原有“轮询不修改调用者快照”测试。 |
| 账务与并发 | 重放陈旧及同终态快照，模拟终态写失败，并检查重复观察不会重复统计。另模拟终态已提交、结算前中断并重复运行恢复器。用成功实扣与预留不同的样本，按本地任务验证用户余额、账务记录和差额。会员与支持持久恢复的非会员路径各自断言。历史空账务状态单列，避免误追扣。 |

[轮询与快照测试](../../service/task_polling_test.go) · [终态统计](../../service/task_polling_metrics_test.go) · [持久账务与恢复](../../service/task_billing_test.go) · [会员终态额度恢复](../../service/task_membership_billing_test.go)

## 二、超管可遍历的付款异常队列

修复前，管理端仅读取全站最近 50 笔会员订单。模型已支持 `status=paid_exception`，会在数据库截取记录前筛选未处理订单、异常收款回执以及适用的待核查支付。管理端现在复用这个查询找回被普通新单挤出的异常款项，超管还能通过游标继续处理第 51 笔及更早的异常单。[管理端查询](../../web/default/src/features/membership/components/orders.tsx) · [列表接口](../../controller/membership.go) · [数据库筛选](../../model/membership_order.go)

- **查询。** 超管订单页保留“最近订单”，增加“付款异常”，只有选中时请求 `GET /api/membership/admin/orders?status=paid_exception`。沿用 `RootAuth`、默认 50、上限 100、按 `id DESC` 的数组响应。在管理列表上增加可选正整数 `before_id`，后续页在相同权限与筛选条件内使用 `id < before_id`。省略游标即首页，非法游标返回参数错误。复用已有 GORM 查询与参数绑定，无需改表。[管理路由](../../router/membership.go) · [前端 API](../../web/default/src/features/membership/api.ts)
- **翻页与兼容。** 后续页以末行 ID 为游标，每条返回记录必须严格小于发出的游标，页内按 ID 降序，按本地订单 ID 去重。先验证，再追加或判断短页结束。恰好满页允许再查一次。旧后端忽略游标时，即使首屏因新单而局部变化，也应停止并提示分页不可用。加载失败保留已有页，使用原游标重试。身份或筛选切换隔离缓存及迟到响应。刷新从头查询，才能看见翻页后才转为异常的旧订单。查单或处置成功后刷新两个管理列表。后端游标先交付。
- **款项处置。** 筛选命中只代表需要核查。`ResolveMembershipOrder` 在事务中检查 `PaidAt`，发放还需回执、金额、币种、供应商等符合原单条款。已处理原单若仍有未处置的额外异常回执，仍应可见并沿用适用的 `dismiss`。仅待核查的支付展示现有状态和 `can_check_payment` 允许的操作。两个超管同时操作时用已有操作 ID、原因和行锁避免重复发放，冲突后重读。个人订单限本人可见，管理查询和处理限超管。[支付状态与查单能力](../../controller/membership_payment_check.go) · [处理事务](../../model/membership_order.go) · [处理弹窗](../../web/default/src/features/membership/components/resolve-order-dialog.tsx)
- **界面与验收。** 尽量复用 `Payment exceptions`、`Load more` 等已有文案。新增文案覆盖 en、zh、zh-TW、fr、ja、ru、vi。分别验证加载失败重试、成功空态、键盘切换和处置后刷新。数据用例为① 一笔旧异常加 50 笔新正常单，② 51 笔以上异常且翻页间插入新单，③ 待核查、额外回执、已处理三种状态，④ 超管、普通管理员和个人三种访问身份。按订单 ID 核对翻页结果、可用操作和账务副作用。[会员界面用例](../../web/default/src/features/membership/membership.test.tsx) · [会员支付模型用例](../../model/membership_payment_test.go)

## 三、上游轮询的响应处理

**先修响应生命周期。** `UpdateBatchTasks`、`updateSunoTasks` 和 `updateVideoSingleTask` 每次取得可读响应体便负责关闭，覆盖错误状态、读失败、解析失败和正常结束。逐请求关闭，显式处理空响应、nil body 及零字节结果。Suno 沿用 HTTP 200，单查询先检查 2xx，通用批量按插件成功状态约定再解析。只有成功解析出的可信业务终态才推进账务。单渠道失败时继续其他渠道。通用批量补齐 `ChannelSetting` 和 `HeadersOverride`，继续传递现有代理参数。[三个轮询入口](../../service/task_polling.go) · [插件获取和解析响应](../../relay/channel/task/jsplugin/adaptor.go)

**把取消传到在途请求。** 旧接口未传入轮询上下文。新增的可选上下文接口把后台调度或同步等待的取消传至 Go 查询、解析和用量钩子，JS 宿主以 `http.NewRequestWithContext` 创建请求。保留插件 v1 的 JS 方法和参数。单渠道超时结束该次查询，整个调度取消才停止余下渠道。同步客户端离开后只取消这次观察，已受理任务继续由后台轮询。用可控 HTTP 上游先发响应头，再阻塞响应体，等待已开始读取后取消，验证读取退出、连接释放且没有误记失败或退款。[Go 适配器接口](../../relay/channel/adapter.go) · [JS 宿主请求](../../relay/channel/task/jsplugin/adaptor.go)

**再交付读取上限。** 插件内部 `utils.fetch` 的 1 MiB 上限适用于辅助 HTTP 请求，Vertex 的轮询结果可以是带 Base64 视频的合法 JSON。归档允许的原始视频大小也不能直接作为进程内 `ReadAll` 的预算。任务插件开发按下面两类分别交付，并在实现记录中给出选定的上限、依据和适用入口。[插件辅助请求上限](../../pkg/jsplugin/fetch.go) · [Vertex 视频结果协议](../../plugins/tasks/vertex-ai/plugin.js) · [归档大小设置](../../setting/video_studio_setting/config.go)

| 响应类型 | 实现及通过条件 |
| --- | --- |
| 元数据、短 URL、批量状态 | 按协议和批量规模选择可容纳最大合法样本的预算。对实际读取的解压后字节使用 `limit + 1` 检测，支持缺失或不可信的 Content-Length。超限、截断、读取错误均结束本轮查询，下一轮仍可重试。 |
| 内嵌媒体 | 先核对内置 Vertex 非 Omni 的操作查询、用量提取和制品下载。状态轮询可优先在进 JS 前有界跳过已知的大型 Base64 字段，保留操作状态、错误、视频对象、媒体类型、gcsUri 及计费所需字段。内嵌制品经现有下载入口重新获取。按具体插件与协议启用优化，定制插件及 Omni 单独核对钩子输入。按原始文件、Base64、JSON 封装和并发核算预算。验收峰值内存，防止流式读取或临时文件最终又被整段读入。完成最大合法样本与下载验证后再启用该路径的限制。 |

验收使用可跟踪关闭的响应体。覆盖空响应、502、恰到上限、多 1 字节、未知长度、解压后超限、读到一半失败、一次轮询中好坏渠道并存，以及超过 1 MiB 的合法 Vertex 格式样本。检查失败路径的数据库状态、额度、解析调用次数和关闭次数。检索共享 `FetchTask` 的按需刷新和归档源刷新入口，给读取同一协议的入口应用相同预算。直接传输视频字节的接口保留其媒体合同。异常日志只留渠道、本地任务标识、状态与尺寸类别。[归档源刷新](../../service/video_asset_pipeline.go)

## 四、两项测试修复及渠道诊断额度

| 项目 | 修改位置与做法 | 验收 |
| --- | --- | --- |
| 邀请分页测试 | 故意缺少 `id` 的对象用于检验调用方的运行时 guard。在该用例显式调用 `requireInvitationPage<unknown>`，保留非法输入和抛错断言。[测试](../../web/default/src/features/invitations/api/result.test.ts) | 该用例通过，前端类型检查中的 TS2741 消失。如果删除 guard，测试应能发现。 |
| 图片归档边界测试 | 改用固定、可解码且原始长度非 3 的倍数的微型 PNG 测试样本。现有动态样本在上一轮验证中生成了 96 字节，Base64 无填充，前置断言 `96 > 96` 失败。[测试](../../service/image_archive_fetch_test.go) | 明确检查 Base64 带填充。实际大小等于上限时归档成功且字节数准确，同一有效样本在上限减 1 时被拒绝。 |
| 管理员渠道测试日志 | `settleTestQuota` 校验 token 数和倍率，避免整数加法溢出。按原顺序先将加权完成 token 四舍五入，与输入 token 相加，再乘模型倍率并四舍五入。模型倍率非零时保留最低 1，固定价沿用截断。只在最终结果应用项目统一的 checked 额度边界，避免中间结果先截到 int32 导致合法小倍率变价。计费诊断与上游健康结论分别传递。[渠道测试](../../controller/channel-test.go) · [额度转换](../../common/quota_math.go) | 覆盖正常分数、固定价尾数、零价、最低 1、中间值大而最终合法、极大 token、负值、非有限倍率及阶梯价。异常不得产生错误消费日志或单独触发自动启停。 |

## 五、OpenAI 图片超发的收费规则

**开发共识。** 先用测试固定现行收费和断连语义，并观察超发。分别记录本次实际派发的出站张数、按协议观测的输出数、用于结算的张数。出站数取渠道改写后、本次准备成功并实际派发的 `ImageRequestCount`。该字段在预留成功前就会赋值，免费和纯 token 价也可有这个字段。实收和预留金额应查账务记录。每次尝试独立计数，原始请求与表达式 `param("n")` 继续读取冻结输入。[出站准备与预留](../../service/image_billing.go) · [表达式合同](../../pkg/billingexpr/expr.md) · [按张结算入口](../../relay/image_handler.go)

| 路径 | 首批必须保留并验证的行为 |
| --- | --- |
| 标准上游 JSON，输出 JSON 或转换为 SSE | 按既有 URL/Base64 计数口径识别可用结果，覆盖混合表示。确认少发时按已有张数计价规则结算。普通空 `data` 沿用本次有效出站数。JSON 转 SSE 仍可正常结束。 |
| 原生 SSE | 现有代码按 completed 事件类型计数，计数发生在写出前。partial 或单独 usage 不计。正常结束可按完成事件数下调。请求 3 张、完成 1 张即断连时，当前保留 3。请求 1 张、完成 2 张才断连时，现有用例按 2。测试分别断言有效载荷与下游写入结果。已经输出的流不重新派发。 |
| 上游 SSE 转客户端 JSON | 桥接器只采纳有效完成图。正常 DONE/EOF 且零完成图返回 `EmptyResponse` 并禁止重试。首图前读流失败或上游报错保留各自错误。取得完成图后，覆盖后续读取错误、usage 异常、客户端断开及下游写失败时的部分成果结算。 |
| Image Studio、SiliconFlow 及其他已有严格适配器 | 保持各协议既有的数量校验、归档及交付条件。真实 token 用量沿用其自身计价规则，不能因为观测到多图就把聚合 token 再乘一遍张数。 |

首批在计数点保留每次尝试的一条脱敏超发诊断，注明渠道、计数来源、出站数与观测数。应在 `updateOpenAIImageCount` 丢弃超过 `dto.MaxImageN` 的计数之前采集。超限在管理员诊断中可表示为“上限以上”，该标记不传入计费公式。按可观测事实区分 JSON 可用载荷、SSE 完成事件、桥接器收集的实体和下游写入结果。日志不含图片、提示词或签名地址。[OpenAI 图片处理](../../relay/channel/openai/relay_image.go) · [JSON 桥接](../../relay/channel/openai/image_json_bridge.go) · [已有严格校验](../../service/image_output_billing.go)

**本轮收费范围。** 按产品决定，普通图片接口仅增加脱敏超发诊断，保留现有渠道参数覆盖、计费、退款及重试规则。超发指上游输出超过本次实际派发的张数。只配置了张数覆盖且上游按派发数量返回时，这条日志不会触发。

验收以最终账本和异常诊断为准。覆盖少发、足额、上游异常超发、零完成、断连前后的部分交付，以及失败后换渠道。断言公开响应、冻结输入、客户端实际读到的内容、实扣、退款和上游请求次数。超发样本核对当前计费合同及观测值。固定价、含 `image_count` 的表达式、真实 token 价各有对照。[流式合同测试](../../relay/channel/openai/image_stream_test.go) · [桥接合同测试](../../relay/channel/openai/image_json_bridge_test.go) · [计费测试](../../service/image_output_billing_test.go)

## 定向验证与交付

### 本轮收尾实测

以下 Go 命令从仓库根目录运行。双渠道同号、派发前隔离、后台先完成、受理归属以及账务恢复用例均通过。

```sh
go test ./relay -run '^TestSunoPollingSettlesSharedProviderIDPerChannel$' -count=1 -timeout=60s
go test -count=1 ./service -run '^Test(PollingWaitsForSubmissionReceipt|MembershipTaskPollingDoesNotReviewAnActiveDispatcherWithoutProviderID|PollingBindsSharedProviderIDsToChannelAndCredential)$'
go test -count=1 ./relay -run '^TestVideoFetchByID(ReturnsCommittedResultAfterFailedRefresh|PersistsManagedRealtimeResultWithoutExposingIt)$'
go test ./model ./service ./relay -run '^(TestTaskAcceptanceBindsUnconfirmedReceiptToClaim|TestTaskAcceptanceRecoversLegacyReceiptWithoutDigest|TestTaskClaimReplacesCredentialsAcrossRetries|TestTaskRecoveryOutboxBindsFirstReceiptAndIgnoresReplay|TestPersistTaskSubmissionAcceptanceRejectsDispatchingCredentialMismatch|TestMembershipTaskReviewFinalizesOriginalFundingAndRejectsLateResults|TestSubmitPreparedTaskRecoversAcceptedReceiptAfterTaskPersistenceRetriesExhausted)$' -count=1
go test ./service ./relay -run '^(TestTaskRecoveryOutbox(BindsFirstReceiptAndIgnoresReplay|PreservesConfirmedLegacyReceipt)|TestSubmitPreparedTaskRecoversAcceptedReceiptAfterTaskPersistenceRetriesExhausted)$' -count=1
go test ./relay -run '^TestSubmitPreparedTask(LeavesReboundClaimDispatching|RecoversAcceptedTaskAfterTransientFinalPersistFailure|RetriesAcceptedPersistenceWithoutResubmitting|RecoversAcceptedReceiptAfterTaskPersistenceRetriesExhausted|KeepsUnknownResidualWhenTaskAndReceiptWritesFail|MarksTransportFailureUnknownWithoutRetryOrRefund)$' -count=1 -timeout=45s
go test ./model ./service ./relay -run '^(TestRejectedTaskAttemptCannotResetAnotherClaim|TestSubmitPreparedTask(KeepsExplicitHTTPRejectionRetryable|KeepsDefinitiveApplicationRejectionRetryable|KeepsPreWriteTransportFailureRetryable|TreatsFailedPreWriteClaimResetAsUnknown|UpgradesRejectedResponseToUnknownWhenClaimResetFails)|TestClaimTaskSubmission(PersistsRetryGroupOnlyForWinningClaim|AllowsOnlyOneConcurrentDispatcher)|TestTaskClaimReplacesCredentialsAcrossRetries|TestTaskAcceptanceBindsUnconfirmedReceiptToClaim)$' -count=1 -timeout=45s
```

恢复队列新增换绑用例后，第一条命令中的入队后换绑子例遇到测试样本将 JSON `null` 与 Go `nil` 直接比较的问题。修正样本后，第二条重跑该子例通过。最后一次派发归属校验修改完成后，第三条把双渠道结算和回执恢复一起复验，两包均通过。

```sh
go test ./relay ./service -run '^Test(SubmitPreparedTaskLeavesReboundClaimDispatching|TaskRecoveryOutboxBindsFirstReceiptAndIgnoresReplay|TaskRecoveryOutboxPreservesConfirmedLegacyReceipt)$' -count=1 -timeout=45s
go test ./relay -run '^TestSubmitPreparedTaskLeavesReboundClaimDispatching$/after_recovery_receipt' -count=1 -timeout=45s
go test ./relay ./service -run '^(TestSunoPollingSettlesSharedProviderIDPerChannel|TestSubmitPreparedTaskLeavesReboundClaimDispatching|TestTaskRecoveryOutboxBindsFirstReceiptAndIgnoresReplay|TestTaskRecoveryOutboxPreservesConfirmedLegacyReceipt)$' -count=1 -timeout=60s
```

以下媒体查询、归档、凭据和会员控制器用例均通过。

```sh
go test ./service ./controller -run '^Test(ResolveVideoArchiveSourceScopesSoraProviderContentToConfiguredBase|CredentiallessPluginArtifactAfterKeyRemoval|TaskArtifactContentAllowsConfiguredPrivateChannel|VideoProxyPreservesCrossOriginGeminiMediaQueryWithoutProviderHeader)$' -count=1 -timeout=60s
go test ./model ./controller -run '^Test(ResolveChannelCredentialPreservesTaskScope|GetGeminiVideoURLScopesCredentialsToProviderOrigin|VideoProxyAllowsProviderManagedPrivateBaseURL|VertexInlineVideoPollAndArtifactDownloadOverOneMiB)$' -count=1 -timeout=60s
go test ./controller -run '^TestPluginArtifactWithMissingKeyDoesNotFetchSignedURL$' -count=1 -timeout=60s
go test ./relay/channel/task/jsplugin ./controller -run '^Test(UpstreamContextsUseGatewayBearerOnNewAPIChannels|TaskVideoPosterSourceKeepsProviderCredentialsOnOrigin|DisabledArtifactStorePreservesPluginUpstreamContent)$' -count=1 -timeout=60s
go test ./controller -run '^Test(LegacyGeminiVideoWithoutSavedKeyCannotUseCurrentKey|VideoProxyPreservesCrossOriginGeminiMediaQueryWithoutProviderHeader|VideoProxyScopesGeminiAPIKeyAcrossRedirects)$' -count=1 -timeout=60s
go test -v -count=1 ./controller -run '^TestAdminMembershipOrderQueueResponses$'
```

以下命令从 `web/default` 运行。会员管理的 7 个前端用例和定向 lint 均通过。

```sh
bun run test src/features/membership/api.test.ts src/features/membership/membership.test.tsx -t 'membership order list transport|handles an exception queue|refreshes both order views'
./node_modules/.bin/oxlint -c .oxlintrc.json src/features/membership/api.ts src/features/membership/api.test.ts src/features/membership/membership.test.tsx
```

### 按模块复验

下列命令用于复验前几节的其他行为，可按改动选用。本轮收尾按项目规则跳过全仓测试。

**任务与账务，从仓库根目录运行。** 新增的分组、在途取消、批量回包与恢复用例放在对应测试名前缀下。`TestPluginProtocolImagePoll` 覆盖同步入口的成功、失败、后台抢先完成、断开和超时路径。

```sh
go test -v -count=1 ./service -run '^Test(TaskPolling|UpdateVideoTasks|MembershipTaskPolling|MembershipTaskCompletionPersistsActualForCrashRecovery|TaskBillingRecoveryRefunds|DurableSettlementSurvivesStalePollerWriteExactlyOnce)'
go test -v -count=1 ./relay/channel/task/jsplugin -run '^Test(TaskAdaptorBatch|TaskQueryRedirects|DurableTaskPolling)'
go test -v -count=1 ./controller -run '^TestPluginProtocolImagePoll'
go test -v -count=1 ./relay -run '^TestVideoFetchByID'
```

**付款异常、归档和渠道诊断，从仓库根目录运行。** 模型与控制器的 `TestAdminMembershipOrder` 覆盖筛选、权限和游标测试。

```sh
go test -v -count=1 ./model ./controller -run '^Test(AdminMembershipOrder|MembershipPaymentReview|MembershipPaymentManualResolution|MembershipOrderCannotBeReadByAnotherUser)'
go test -v -count=1 ./service -run '^TestHTTPImageArchiveFetcher'
go test -v -count=1 ./controller -run '^TestSettleTestQuota'
```

**普通图片 Relay 收费，从仓库根目录运行。** 用对应包的用例验证传输与入账。插件图片同步入口另见第一组。

```sh
go test -v -count=1 ./relay/channel/openai -run '^Test(OpenaiImage|OpenAIImageStudio|SiliconFlowImages|ImageExpressionSettlement)'
go test -v -count=1 ./relay/channel -run '^TestOpenAIImageJSONBridgeDrains'
go test -v -count=1 ./service -run '^Test(ApplyImageOutputBillingCount|ImageOutputSettles|ImagePricingSettlement|ImageStudioDeliveryBilling)'
go test -v -count=1 ./controller -run '^TestOrdinaryImageRelayOverdeliveryBilling$'
```

**管理界面，在 `web/default` 运行。** 定向验证会员交互与邀请 guard。涉及文案时另跑 `bun run i18n:check` 并核对七种语言。邀请类型错误用前端 `bun run typecheck` 复核，按项目的全量检查授权规则单独执行。

```sh
bun run test src/features/membership/membership.test.tsx src/features/invitations/api/result.test.ts
```

### 发布与止损

任务归属和同步等待可先于大媒体策略交付，会员游标先验证后端，再启用界面翻页。正式发布按当时的 NewAPI 手册执行。候选后台作业关闭，切换后需用自然轮询核对本地行状态、账务记录与差额。会员检查下一页是否可达，媒体和图片分别检查合法响应拒绝情况、超发与实扣。未出现碰撞的线上样本不能替代双渠道同号回归。

若发现新串单或重复结算，停止继续放量，按现有运维能力隔离受影响的处理入口并保留待结预留和关联凭据。按手册核实回退版本在发生问题的路径上能安全处理后再回退。若它也包含已知串单路径，保持隔离并修复后验证。媒体限额独立交付，出现合法响应被拒时可单独撤回。历史账务按本地任务号、原始预留和最终账务记录核对，资金纠正另行处理。

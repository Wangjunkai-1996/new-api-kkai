# 大请求压缩：实施与验证记录

日期：2026-10-17（北京时间）。用户已授权实施、执行 `make policy`，并提供测试凭据用于真实请求验证。已完成本地修复、定向检查及有限线上实验；部署了无正式路由的 Worker，未提交代码或构建/发布 NewAPI。历史超时不能宣称已修复。

现场发现当前 API 经 SingG SaaS CNAME 接入，不能直接依赖自有 Cloudflare zone 的 Worker route。隐藏三臂实验首个 control 在外层 Edge 返回 405 后立即停止并撤掉临时路由；B 臂未发送。随后改用现有正常 `/v1/responses` 做原文/客户端 gzip 两臂验证，无需改变入口。Worker 回源压缩收益仍未验证。

## 已实现

基础设施仓库 `cloudflare/workers/newapi-request-gzip/`：

- 压缩资格上限由 8 MiB 调整为 32 MiB；超过上限的生产请求仍原样转发。保持原有流式压缩、单次回源、取消信号和禁止自动重发 POST。
- 初始配置明确为 `allowlist`，不沿用线上遗留 `sample/100%`。本地配置不是线上已生效配置。
- 既有生成器支持 2/4/8/24/32 MiB、确定性普通文本和伪随机 base64 两类语料。
- Runner 固定三臂轮换：control、Worker gzip、client_gzip；每轮 3 请求，默认 3 轮/9 请求，最多 20 轮/60 请求。C 臂发送真实 gzip 长度并设置 Content-Encoding，通过 Worker 已有编码旁路；没有新增 Worker 模式。
- 客户端 gzip 在每批计时前预计算一次，单独记录准备耗时；包含准备耗时的收益字段是估算，不冒充实测上传时间。任一失败立即停止，不自动重发。
- 集成测试证明三臂通过实际 Worker handler 后只回源一次、解码字节一致，客户端 gzip 不被再次压缩；SSE 合约继续按各 API 检查。

应用仓库：

- `middleware/gzip.go`：gzip/br 解压后删除旧 Content-Length，并把 Request.ContentLength 设为 -1。
- `common/gin.go`：缓存首次请求体读取失败，后续中间件保留同一错误，不重新处理已经消费/关闭的上传。
- `middleware/distributor.go`：保留原错误身份，超限返回 413。
- `middleware/task_plugin.go`：插件识别时遇到超限立即返回 413；普通识别失败仍保留既有 fallback。
- 新增实际中间件链的行为回归：正常 gzip/br、已知/未知长度、损坏/截断、解压后超限、普通读取失败及 context.Canceled。无效请求在分发/计费前终止。

先运行新回归复现旧行为：解压后长度错误、超限返回 400、失败上传在 fallback 后重复 Close；随后修复并验证。没有证据证明这些问题就是历史五条超时的唯一原因。

## 定向验证

以下定向检查均成功。应用未跑全套；基础设施按用户授权运行完整策略入口，最终树 `make policy` 退出 0，首次失败及补验历史见后文。Worker 测试从其目录执行；Go 测试从应用根目录执行。

```sh
node --test --test-timeout=10000 test/policy.test.js test/worker.test.js test/canary-payload.test.js test/config-policy.test.js
```

首轮 40/40 通过。之后仅把一项累计大包测试拆成四项独立体量测试，避免贴近单项 10 秒截止；重跑下列最近范围，28/28 通过（8.05 秒，最大单项 2.93 秒）：

```sh
node --test --test-timeout=10000 test/worker.test.js
```

三臂 runner 加入安全提取源站请求 ID、修正完整成功计数后最终 15/15 通过（约 0.31 秒），包含 runner→Worker 集成及 32 MiB/+1 配置边界：

```sh
node --test --test-timeout=10000 test/canary-runner.test.js
```

语法检查与部署预演通过；bundle 16.35 KiB/gzip 4.43 KiB。之后另按隔离实验范围实际发布，见下文：

```sh
npm run check:syntax
npm run deploy:dry-run
```

后端相关中间件新旧回归通过（0.139 秒；外层编译/执行限制 180 秒）：

```sh
GIN_MODE=test GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go test ./middleware -run '^(TestDecompressRequestBodyMetadataAndReplay|TestCompressedRelayBodyRejectsInvalidAndOversizedBeforeDispatch|TestRelayBodyReadFailureIsNotRetriedAfterPluginFallback|TestTaskPluginBodyParseMissPreservesOrdinaryFallback|TestTaskPluginEndpointMissPreservesOrdinaryRequestBody|TestTaskPluginEndpointUsesOneCanonicalModelForDuplicateJSONKeys|TestPinTaskPluginEndpoint.*|TestPrepareTaskPluginEndpoint.*|TestGetModelRequest.*|TestDistributor.*)$' -count=1 -timeout=90s
```

随后在同一读取失败用例中追加 context.Canceled，仅重跑该项，成功（0.067 秒）：

```sh
GIN_MODE=test GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go test ./middleware -run '^TestRelayBodyReadFailureIsNotRetriedAfterPluginFallback$' -count=1 -timeout=90s
```

公共存储已有生命周期合约通过（0.014 秒）：

```sh
GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go test ./common -run '^TestNewReplayableBodyReaderKeepsStorageLifecycleWithCaller$' -count=1 -timeout=60s
```

两个仓库的 `git diff --check` 均通过。基础设施强制策略门槛按用户明确授权执行：

```sh
make policy
```

首次执行 209.10 秒后退出 2，停在 Invitations 策略测试的 rollback 子项之后，未留下足以归因的诊断。没有改代码或环境，仅补验最近失败范围：

```sh
bash -x roles/invitations/tests/test-policy.sh
```

退出 0（15.94 秒），包括 rollback、PostgreSQL 18 ACL 和 localhost Ansible。再从原 Makefile 按相同顺序、变量和参数提取 Invitations 后剩余 13 个 recipe，执行：

```sh
make -f Makefile -f /var/folders/tm/gclh_4_j3530jvnqmxrhjv7c0000gn/T/kkai-policy-tail-nolr4sr3.mk policy-tail
```

退出 0（20.15 秒）。因此每个 policy recipe 都有成功证据；完整 `make policy` 首次退出不为 0 的原因仍未确定，不能写成首次全量命令通过。随后 runner 又修正成功计数，因此针对最终树重新运行完整入口；这次重跑由实际代码变化触发。

最终完整 `make policy` 已执行完毕，退出 0，包含先前 Invitations 范围及后续全部 recipe；完整日志保留为本机实验目录的 `policy-final.log`。最终脚本的 `npm run check:syntax` 也再次通过。全套基础设施策略检查已运行，应用全仓测试仍按范围跳过；不会将首次偶发失败抹去或虚报为已定位根因。

## 本地容量与压缩观察

临时独立 Go 探针在 macOS arm64/Go 1.26.5 执行真实 32 MiB Responses 结构，unknown Content-Length，磁盘缓存阈值 10 MiB。处理链为 CreateBodyStorageFromReader → Bytes → Unmarshal → GetTokenCountMeta → Marshal；各阶段保留数据，校验输入文本全文摘要和重编码字节一致。两次均实际落盘、成功并清理缓存。每次独立进程，运行限制 45 秒，离线构建限制 90 秒。

| 指标 | 并发 1 | 并发 2 |
| --- | ---: | ---: |
| 最大 RSS | 252.05 MiB | 532.33 MiB |
| 所测链总时长 | 730.84 ms | 752.88 ms |
| 所测链累计分配 | 329.81 MiB | 659.54 MiB |
| Marshal 阶段末 HeapAlloc | 226.11 MiB | 450.12 MiB |

本地真实 DTO 路径可处理上述有限负载，暂未发现必须先重写 BodyStorage 的证据。探针不含真实网络、gzip 解压、完整模型扫描、tokenizer、鉴权、计费、PrepareResponsesRequest 深拷贝及上游处理；重复 ASCII 语料只用于此容量探针，不用于声称压缩收益。不能据此推断 Linux 生产峰值或安全并发。

临时目录 `newapi-responses-capacity-_8eqpivb` 保留小型源码、完整日志和 README（精确构建/运行命令）；已删除生成的 32 MiB fixture 与二进制。

另外对两类合成语料运行本机 gzip，属于压缩率观察，未发送网络请求：

| 原始字节 | 普通文本 gzip/原文 | base64 gzip/原文 |
| --- | ---: | ---: |
| 21,903,139 | 48.45% | 75.75% |
| 25,165,824（24 MiB） | 48.45% | 75.75% |
| 33,554,432（32 MiB） | 48.45% | 75.75% |

本机准备 gzip 约 471–986 ms，不能当成 Worker CPU。语料不含客户原文，结果不能代表客户请求压缩率；节省字节也不证明首内容延迟下降。

## 只读现场与历史核查

- 找到 2026-08-27 23:06:54 用户主动要求取消该功能的历史记录，随后正式路由被移除；没有证据把停用归因为已知压缩正确性事故。历史 4 MiB 样本有收益，不能替代本次 24/32 MiB 验收。
- 用户本轮直接提供测试 key 并明确授权使用，已用于有限真实请求；凭据不写入仓库、报告或命令参数。
- 当前 Workers 付费计划未能证实：官方订阅 API 返回 403/code 10000；account-settings 的 `default_usage_model=standard` 只证明计量模型，不能证明 Paid。另尝试浏览器只读核实，控制台要求登录；临时标签已关闭。已修正 Worker README 中过时的 Paid 断言，未更改订阅。
- 线上 Router 已有 bench 关联日志及请求头剥离，外层 OpenResty 有效配置没有相应日志。`FORWARD_BENCH_ID_TO_ORIGIN` 必须保持 0；正式 route 前仍需补齐外层门槛。
- 本轮重新执行 `make newapi-status` 为 HEALTHY，active blue，版本 `kkai-prod-20261016.1792154080-b851781f7`，rollback ready。这是该次读取的快照，不是部署完成证据。
- 同次只读观察：active 容器内存限制 10 GiB、使用约 1.358 GiB；进程 RSS 约 1.401 GiB、历史高水位约 2.997 GiB；主机 MemAvailable 约 21.094 GiB。仅表示瞬时余量，不能替代 Linux 负载验证。

## 线上入口与隔离实验

两台权威 NS 对 `api.kkrich.ltd` 均返回 `CNAME saas.sin.fan`，递归后续为 `singgcdn.singgnetworkcdn.com`。infra 迁移记录写明 DNS-only/SingG，现有 DNS 维护脚本拒绝该 API 记录设为 proxied。当前 DNS API 读取 403，因此没有直接 API 证据断言现时 proxied 字段；权威 DNS 与本次行为一致。

Cloudflare [Routes](https://developers.cloudflare.com/workers/configuration/routing/routes/) 与 [SaaS O2O](https://developers.cloudflare.com/cloudflare-for-platforms/cloudflare-for-saas/saas-customers/how-it-works/) 说明对应 zone 的代理入口要求。在自有 zone 新建 route，不证明当前 SaaS CNAME 流量会执行它；不能据本次结果断言历史 Worker 从未有效。

隔离 Worker 发布版本 `5cace6c7-dd5b-4d4e-8b8c-ad0161bb3652`：32 MiB、production/allowlist、仅测试凭据 cohort、临时回源字节计数开启。正式 `/v1/*` route 始终没有挂载。短暂添加隐藏 production-canary route 后：

- 第一个 control：2,097,152 字节，HTTP 405，HTML 响应 154 字节，客户端总耗时 1,346.6 ms；无源站请求 ID，立即停止，B/C 均未发送。
- 外层 Edge 日志唯一匹配到 2026-10-17 04:50:01 UTC：相同路径、POST/405、154 字节、处理 0.000 秒。有效配置默认 `location /` 对非 GET/HEAD 返回 405，该分钟 Router/NewAPI 无此路径记录。隐藏前缀未被 Worker 去掉，而到达外层 fallback。
- Edge 日志不含 CF-Ray，不能再用该字段交叉关联；路径/时间/状态/字节一致支持上述定位。Tail 没有事件只作辅助，不能单凭空 Tail 定论。
- 临时路由已撤销，没有改 DNS、SaaS Custom Hostname 或其他 route。

诊断结束后重新发布安全配置版本 `ecf1379e-b5e0-4b98-9521-0abfddf65774`。API 复核：`ACCOUNT_UPSTREAM_BODY=0`、`FORWARD_BENCH_ID_TO_ORIGIN=0`、production/allowlist、workers.dev/preview 关闭，正式和两条隐藏 gzip route 均无；原有其他 route 保留。自建 Tail 已停止，API tail_count=0。此发布没有让普通 API 请求启用压缩。

## 正常 API 客户端 gzip 对照

测试来自本机，经现有 HTTP 代理 `127.0.0.1:7897`，使用相同 NRT POP、`gpt-6.1-sol`、并发 1、输出上限 64。未使用客户正文：普通代码/工具日志/中文混合合成 padding 被 NewAPI DTO 丢弃，模型实际输入保持很小。先用 tiny baseline 验证 key，再直接访问正常 `/v1/responses`，不经过新 Worker。

两臂临时脚本复用已测试 runner 的请求/SSE解析，只将路径改为正常 API、模式收敛为原文和 client_gzip，删除诊断请求头。每请求超时有界、无自动重发，任一功能失败立即停止。gzip 在网络计时前生成，准备耗时单列，发送真实压缩 Content-Length。24 MiB 交换两臂顺序。

| 原文体量 | 实际 gzip 字节 | 原文首内容 | gzip 首内容 | gzip 准备 | 功能结果 |
| --- | ---: | ---: | ---: | ---: | --- |
| 2 MiB | 1,016,200 | 8.212 s | 4.039 s | 47.7 ms | 两臂 200，SSE 完整 |
| 8 MiB | 4,064,409 | 25.639 s | 11.264 s | 185.7 ms | 两臂 200，SSE 完整 |
| 24 MiB | 12,192,938 | 65.826 s | 34.216 s | 549.4 ms | gzip 完整成功；原文 200 后流中错误，整组失败 |

tiny baseline、2/8 MiB 两臂及 24 MiB gzip 的源站数据库记录均为 channel 31、同一测试 token、成功消费，input 4,391/output 5 tokens。合成 padding 未增加上游计量。它验证上传/解析链，不能替代同等真实模型上下文的业务容量。

2 MiB 两次请求均成功，随后临时汇总脚本因不存在的 B 臂索引退出；已修复汇总且没有重发这两个 POST。8 MiB 两臂及汇总均通过。

24 MiB 原文在 107.746 秒结束，10 个 SSE 事件、1 个错误事件、0 个成功终止事件，整组 `pass=false`；不是测试端 120 秒超时。gzip 同体量 37.764 秒完整成功。按停止条件没有继续 32 MiB 线上请求，也没有重发失败的 POST。生成但未发送的 32 MiB fixture 与本轮其他大体量语料已清理。不能把该原文 HTTP 200 当作业务成功，也不能在未追查前把流中错误归因于上传。

源站进一步确认原文请求 `202610170458186087449298268d9d6s1FWpygT` 已进入 channel 31，上游 HTTP 200、开始输出后第 10 个事件为 `response.failed`：`openai_error/server_error`，消息明确指向服务器过载、要求稍后重试。NewAPI 记录 relay_status 502、output_started/terminal_error=true、retry_allowed=false；GIN 为 HTTP 200/107.091 秒。数据库为 type 5 错误记录、quota=0；错误日志 tokens 固定为 0，不能推断上游没有消耗。不能指认具体 Sub2 账号故障或 NewAPI 本机 CPU 过载，不能把该失败当作“请求没发出去”，也不能把原文/gzip 的全部总时差归功于压缩。

同时修正正式 runner 的 `successful_samples`：此前只按 HTTP 2xx 计数，现与实际完整 SSE 成功契约一致，原有 `pass` 已能正确识别本次失败。最近定向回归仍为 15/15，通过后重新执行用户已授权的策略门槛。

每体量只有一组，观察到的时差同时包含网络、上游及流式处理波动；不输出有统计意义的 P95，不把单组改善当作用户网络或历史超时已修复的证明。

临时脚本及脱敏 JSONL 保留在本机私有目录，实际执行命令如下（32 MiB 未执行）：

```sh
env HTTPS_PROXY=http://127.0.0.1:7897 HTTP_PROXY=http://127.0.0.1:7897 NODE_USE_ENV_PROXY=1 node /var/folders/tm/gclh_4_j3530jvnqmxrhjv7c0000gn/T/kkai-gzip-live-20261017-vjbif6s0/run-direct.mjs responses-text-2m.json direct-text-2m
env HTTPS_PROXY=http://127.0.0.1:7897 HTTP_PROXY=http://127.0.0.1:7897 NODE_USE_ENV_PROXY=1 node /var/folders/tm/gclh_4_j3530jvnqmxrhjv7c0000gn/T/kkai-gzip-live-20261017-vjbif6s0/run-direct.mjs responses-text-8m.json direct-text-8m
env HTTPS_PROXY=http://127.0.0.1:7897 HTTP_PROXY=http://127.0.0.1:7897 NODE_USE_ENV_PROXY=1 KKAI_DIRECT_REVERSE=1 node /var/folders/tm/gclh_4_j3530jvnqmxrhjv7c0000gn/T/kkai-gzip-live-20261017-vjbif6s0/run-direct.mjs responses-text-24m.json direct-text-24m
```

退出状态依次为 1（仅汇总错误，请求成功）、0、1（上游业务失败）。初始实验合计 7 个正常 API 模型请求（含 tiny baseline），另 1 个隐藏路径请求在 Edge 405，未到模型。

## 追加复测：交错 3 组、12 条请求

为确认 24 MiB 原文失败是否是体量必现，按交错顺序追加 3 组，每组各发送一次 8 MiB 和 24 MiB 的原文/客户端 gzip 对照；顺序为 `8 A→C, 24 C→A`、`24 A→C, 8 C→A`、再重复第一组。仍是本机经 `127.0.0.1:7897`、NRT、同一模型和测试 key、输出上限 64；没有并发、自动重试或 Worker 路由。

12 条全部 HTTP 200、SSE 完整成功，并由源站白名单日志逐条核对：`channel_id=31`、`token_id=861`、`type=2`、流式成功、输入/输出 `4391/5 tokens`，没有失败记录或未转发样本。源站 GIN 总耗时中位数如下：

| 体量 | 原文 A 中位数 | 客户端 gzip C 中位数 | 体积比例 | 成功数 |
| --- | ---: | ---: | ---: | ---: |
| 8 MiB | 26.556 s | 13.699 s | 48.45% | 3/3 对照 |
| 24 MiB | 64.084 s | 35.606 s | 48.45% | 3/3 对照 |

这组复测说明：**24 MiB 并不是因为超过 8 MiB 就必然失败**。之前那条 24 MiB 原文的 `response.failed/server_error` 是一次上游过载事件；追加的 3 条 24 MiB 原文都成功。压缩在本组样本中稳定减少首内容/源站总耗时，8 MiB 约降低 48.4%，24 MiB 约降低 44.4%（只作为这 3 对样本的描述性结果，不是 P95 或生产保证）。

追加样本的数据库 `quota` 分为 260 和 345 两档；本次白名单没有取价格配置，不能把该差异归因于压缩，也不用于延迟结论。客户端 gzip 的本地准备耗时约 182–584 ms，未计入请求计时；将其加回仍明显低于对应原文中位数。

追加复测使用了同一临时脚本，输出仅保留阶段耗时、HTTP/SSE 状态、CF-Ray、origin request ID 和上述白名单关联字段；实验完成后会清理测试 key、payload 和原始 JSONL。

本轮临时 API key、cohort secret/HMAC、Worker secrets 文件及原始凭据验证响应已从本机实验目录删除；保留脱敏测量、小型脚本与配置回退证据。Cloudflare 的账户凭据未改动。

## 剩余门槛

1. 当前 SaaS 入口须有单独确认的接入方案，才能执行 Worker B 臂；不能直接恢复自有 zone route，或在性能实验中顺便改变 DNS/入口。
2. Worker 正式接入前仍须核实 Workers 计划/限额，取得实际回源字节、CPU 和延迟证据；本轮 B 臂尚未验证。
3. 客户端 gzip 已有有限功能/时延证据；需在实际客户端和用户网络验证支持情况与重复样本，不能承诺所有现有客户端已有压缩开关。
4. 后端修复仍未发布；若进入用户流量，按正式手册完成本地构建、蓝绿、候选合约及错误窗口验收。
5. 正式 `/v1/*` 接入须先满足外层日志、旁路等价、Host/TLS/SNI、单次回源和精确回退等门槛。只有隔离结果达标才推进 allowlist → 10% → 50% → 100%，不会因本地测试通过自动放量。

当前交付包括可审查的后端本地修复、无业务路由的 Worker 准备版本，以及正常 API 客户端 gzip 的有限实测。生产历史超时改善幅度、Worker 完整回源压缩率与 CPU 分位仍是验证缺口。

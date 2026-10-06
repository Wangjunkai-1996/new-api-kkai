# rc.41 完整接入：本地实现与验证记录

记录日期：2026-10-06。官方目标提交：
`2035a82aeb5414253a728bd937d4b8f97aa99b9b`。

本文记录后续完整接入修正的实际范围，接替
[前期架构准备记录](rc41-upgrade-readiness.md) 作为本轮源码整合的说明。
它不是全套验收通过声明，也不是生产状态快照。此前记录中的提交、前端产物、测试和
生产观察只证明当时的源码与现场，不能直接作为本轮变化后的发布证据。

以下为开始最终验收前的记录：本轮完整接入尚未部署，变更尚未提交，也未构建对应的生产镜像或发布产物。
未执行生产迁移、stage、前端 activate、canary、promote 或公开切流。
本文未重新检查生产，不推断当前线上版本、颜色、健康或回滚状态。

## 整合方式

官方目标中的前端源码位于 `web/src`，KKAI 实际默认前端位于 `web/default/src`。
本轮按照官方完整 UI 的功能与数据流接入默认前端，保留 KKAI 的分组、币制、Image Studio
和 Video Studio 行为；不能以出现一个插件入口或个人设置卡片作为前端整合完成的依据。
下文仅列出已有实现和验证证据，其他界面的最终验收由文末汇总补充。

RelayKit 按官方独立模块迁移到 `relaykit/`，包含协议 DTO、类型、转换器注册、
reasoning、流式事件、工具和引用转换及相应测试。主仓库通过模块依赖使用它；
已移除迁出的旧协议 DTO/types 文件及 `service/relayconvert`，没有用类型别名壳继续维持
两套协议实现。应用自身的任务、渠道筛选、账务和持久化类型仍归应用所有。

保留 KKAI 所需行为的方式是在实际业务链路中接入：包括 USD credit epoch、int64 钱包、
额度上界与审计、冻结图片报价和持久化结算，以及 Responses 图片工具的 quality/size
计价。不能用官方通用工具调用次数价格替代这些业务规则。

## 已完成并有本地证据的范围

### 模型、价格和默认前端

- 模型元数据与仅渠道存在的模型列表、可见性控制；厂商创建、编辑、分配、合并和删除。
- 模型卡片、列表与详情；统一价格面板和侧栏、任务插件价格信息。
- 条件树价格编辑、价格模拟、上游价格三方比较和同步。同步比较遵循有效计费模式，
  不把被表达式覆盖的旧倍率当成另一份价格，也不把空表达式导入成免费价格。
- `/api/ratio_config` 现导出有效表达式和计费模式；此漏项由回归实际发现后补齐。
- 模型设置中的 JSON 编辑器、正则黑名单说明及表单读取方式同步；保留 KKAI 分组标签、
  auto2 等命名自动分组和币制显示。

### 令牌自动分组和渠道选择

- 单令牌可选择有序自动分组子集，默认数量上限为 5；服务端提供候选和上限，校验重复、
  越界以及所选自动分组配置。省略更新保留原值，清空继承默认，变更分组清除失效子集。
- 实际转发使用令牌分组子集；显式子集过滤为空时不回退到未经授权的候选。
- 渠道选择统一使用 `GetRandomSatisfiedChannel(group, model, retry, filters)`，移除
  `WithFilters` 兼容入口。数据库和内存路径均先应用请求路径、插件身份、WebSocket、
  KKAI 能力限制及失败渠道排除，再按优先级和权重选择。
- 精确模型候选无法满足约束时，归一模型候选执行相同约束；保留自动分组跨组重试边界。
- 原生 Responses WebSocket 要求支持的渠道类型和原生 Responses 路由，禁止误选转换路由。
- VLLM/SGLang 的内置 preset 已接入验证、运行时读取、路径筛选、定价端点和模型发现；
  读取 preset 不重写数据库，命名渠道不能被保存的自定义路由覆盖。补齐 ToolLossPolicy 校验。
- 渠道批量删除返回实际删除行数，响应和审计不再把重复或不存在的 ID 计作成功删除。

### RelayKit、模型身份和计费

- reasoning 修饰符、旧推理后缀、正则黑名单以及路由/计费模型身份按官方链路归一；
  保留 KKAI 显式价格覆盖的优先级。
- 输入预扣乘数、固定价格零用量结算、nil 音频用量处理、表达式固定价格、工具调用附加费、
  缓存图片 token 拆分和逐次重试预扣均已接入。
- 余额可信阈值以精确数值比较 int64 钱包，避免大于 `2^53` 后的浮点取整误判。
- Studio 图片固定报价继续使用冻结的单价、数量、分组倍率和额度单位；实际发送数量与
  报价不符时在预扣之前拒绝。未让请求中的额外倍率覆盖冻结报价。
- Responses 图片工具保留 quality/size 明细计价；明确的空调用列表也优先于旧上下文标记，
  避免重复或幽灵扣费。图片重试即使初次请求走可信余额路径，也必须原子预留资金。
- 额度饱和保护、审计标记、int64 钱包和 Studio 持久化结算边界继续保留。

### Codex 模型发现和数据库支持

- Codex 渠道模型发现使用 `/backend-api/codex/models`，发送 account ID、Bearer token、
  CLI 版本参数及相应 User-Agent，整理并去重返回的模型 slug。
- CLI 稳定版本缓存一小时；刷新失败可使用已有稳定缓存。401 对已保存渠道进入凭证刷新
  后重试路径；未保存渠道明确要求先保存。版本和协议测试不访问真实 GitHub/Codex 服务。
- PostgreSQL JSON 列写入返回 string，读取兼容 string/bytes，防止 simple protocol 将
  JSON 作为 bytea 编码；TaskPrivateData 的 KKAI 计费、归档等字段完整保留。
- GORM 日志接入参数与驱动错误脱敏；PostgreSQL 关闭显式 prepared statements。
  MySQL decimal 默认值及 PostgreSQL CHAR/唯一约束的迁移比较采用官方修复。
- 已合入 token 唯一约束、prefill group 唯一约束、options 主键和旧前端设置迁移代码。
  options 修复保存原表备份；旧前端设置按项迁移，目标值优先，坏值保留。
- 钱包 BIGINT 检查适配 KKAI 的 `quota`、`used_quota`，保留现有邀请额度类型边界。
  移除无人调用且绕过顺序修复的旧快速 AutoMigrate 路径。

## 已执行的定向验证

以下记录实施时实际执行的命令与结果。文档整理未重复运行已有测试；随后针对根协调任务
发现的迁移 CLI 摘要 fixture 失败，仅执行了下列单个失败用例。
除特别说明外，命令在仓库根目录执行；结果不能外推成生产或所有数据库的验收。

### RelayKit 和模型设置

在 `relaykit/` 中执行：

```sh
go test ./... -timeout=90s
go test ./dto -count=1 -timeout=90s
```

第一次独立模块测试的其他包通过，DTO 因重复测试声明编译失败。删除与官方已保留用例
重复的测试文件后，仅复跑 DTO 包并通过；没有再运行整套主仓库测试。

```sh
go test ./setting/reasoning ./setting/model_setting ./setting/ratio_setting ./setting/operation_setting -count=1 -timeout=90s
```

结果：通过。

### 价格和账务

```sh
go test ./relay/helper -run 'Test(ModelPrice|FixedPrice|ResolveIncomingBillingExpr|BuildBillingExpr|InputPreConsumeMultiplier)' -count=1 -timeout=90s
go test ./service -run 'Test(CalculateTextQuota|ComposeTieredTextQuota|GroupStatusCache|IsGroupStatusCache|.*ImagePricing.*|ResponsesTool|TryTieredSettle|WalletTrust)' -count=1 -timeout=90s
go test ./service -run '^TestFixedPriceBillingDatabaseMatrix$' -count=1 -timeout=90s
```

结果：通过。固定价格数据库矩阵曾发现音频零用量误退款和 nil 用量 panic，修复后重跑
该矩阵通过；实际执行的是 SQLite 分支，MySQL/PostgreSQL 分支因缺少测试 DSN 跳过。

### 渠道、模型发现和数据库门禁

```sh
go test ./model -run 'Test(ChannelSelection|ChannelWebSocket)' -count=1 -timeout=90s
go test ./service -run 'Test(Codex|ChannelSelectionExcludes|ExhaustedPool|AutoGroupSelection|ImageStudioChannelSelection)' -count=1 -timeout=90s
go test ./model -run 'Test(JSONColumn|ChannelValidateSettings|AdvancedCustomChannelRequires|InferencePreset|SanitizeDBError|GormLogger|MigrationSchemaStability|MigrateTokenKey|MigratePrefill|MigrateRetiredFrontend|LegacyConsoleList|RetiredTheme)' -count=1 -timeout=90s
go test ./model -run 'Test(ExternalSchemaStartup|WalletSchema|BatchDeleteChannels|ValidateMainSchema|ValidateLogSchema|ValidatePostgresApplication)' -count=1 -timeout=90s
go test ./model -run '^TestOptionPrimaryKeyRepair' -count=1 -timeout=90s
```

结果：全部通过。筛选回归覆盖内存/数据库两条路径、先筛选后按优先级、归一模型回退、
失败池排除、auto2 配置和 Studio 多参考图限制。external 启动用例验证旧 options 内容
和无主键形状保持不变，且未创建业务表。options 修复用例验证备份保留与重复执行无 DDL。

### 令牌和价格同步接口

```sh
go test ./controller -run 'Test(AddToken|UpdateToken|GetToken|DeleteToken|TokenAutoGroups|PricingSync|RatioConfigExports|UserAutoGroup|RequestTokenAutoGroup)' -count=1 -timeout=90s
go test ./controller -run 'TestRatioConfigExportsEffectiveExpressions' -count=1 -timeout=90s
```

组合命令中令牌、自动分组和三方价格同步用例通过，表达式导出用例失败。
修复 `controller/ratio_config.go` 后，只重跑失败用例并通过；不把首次组合命令记作通过。

### 默认前端模型与价格相关交互

在 `web/default/` 中执行：

```sh
bun run test src/features/models/__tests__/model-listing.test.tsx src/features/system-settings/models/__tests__/model-ratio-table-selection.test.tsx src/features/system-settings/models/auto-group-profiles-editor.test.tsx src/features/system-settings/models/group-ratio-visual-editor.test.ts
```

结果：4 个文件、31 个测试通过。该记录只证明对应交互；其他默认前端变更、完整类型检查
和本轮统一检查结果由文末汇总补充，不能将这 31 个测试称为全站 UI 验收。

### 迁移 CLI 的摘要 fixture 收尾

根协调任务执行 `go test ./cmd/kkai-migrate -count=1 -timeout=90s` 时，仅
`TestDescribeContractJSONUsesFeatureRuntime` 失败：测试仍期待旧 `cc1fd8e…` 摘要。
核对历史提交 `d28441bdedb5bd1197aa49779de40f1ecff385c2` 后确认，v9 的
ImplementationID 已由 v2 升至 v3，并增加 credit receipts、wallet_delta 和 source_epoch；
CLI 测试 fixture 没有随之更新。当前 canonical `ContractForDialect` 的实际输出和维护
prepare 脚本使用的摘要一致：
`sha256:4e65c4c6c49ad3ce1d87e3a144df3b0a57a3f408f3323226dd81b6b7a16972c1`。

本次只更新 feature/bridge 两份测试中同源的固定期望，没有更改生产摘要、迁移目录、
schema contract 或版本门禁。按失败范围执行：

```sh
go test ./cmd/kkai-migrate -run '^TestDescribeContractJSONUsesFeatureRuntime$' -count=1 -timeout=90s
```

结果：通过。其余默认构建用例沿用根协调任务首轮通过的结果；没有重跑整个包。
bridge fixture 同步采用相同已核对摘要，随后执行：

```sh
go test -tags kkai_bridge ./cmd/kkai-migrate -run '^TestDescribeContractJSONUsesBridgeRuntime$' -count=1 -timeout=90s
```

结果：通过；日志 `/tmp/newapi-migration-cli-bridge-final.log`。

## 维护与尚未验证的边界

主仓库未执行 `go test ./...` 或整个 workspace 的测试、lint、typecheck、build 全套。
实施时 `TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN` 均未配置；本轮新增数据库行为没有完成
真实 MySQL/PostgreSQL 矩阵。历史 PostgreSQL 演练不覆盖本次后续新增的修复代码。

`CanRunRuntimeAutoMigrate` 的权限和模式判断没有放宽。上述官方修复只接在受控的
runtime 迁移流程；external 管理的生产服务启动不会自动执行，也不会因候选启动隐式修库。

以下动作不属于现有 additive v9 expand，不能混入普通应用发布或误称已执行：

- PostgreSQL token/prefill 旧唯一约束修复包含独占表锁和删除旧约束，需要实际目录检查、
  停写边界、备份及对应数据库验收。
- options 主键修复可能去重、备份及换表，需要先检查冲突值并评审恢复路径。
- `MigrateRetiredFrontendOptions(db)` 会删除成功迁移的旧设置键，需要明确维护步骤与
  新旧控制台回退边界；external 启动不会调用它。

这些实现尚未接入现有 v9 additive 事务；是否需要执行取决于维护时的真实 schema 和配置。
必须在显式维护流程中落实调用、验收和恢复方案，不能靠临时启用 runtime AutoMigrate 代替。
本地代码存在、SQLite 测试通过、文档写有流程，都不能证明生产控制器能力已安装或线上已迁移。

## 最终验证汇总

本轮源码仍在 `production/kkrich` 工作区，基线 HEAD 为
`708e21b759b2e90afdee0246055151bfd9999713`；尚未形成可发布的新提交。
以下是本轮后续收尾证据，不将局部测试表述成全套验收。

### 其他实际接入与修复

- 个人设置 `/profile` 集成账号绑定、双因素、Passkey、PAT 等安全功能；保留会话隔离、
  退出清理、查询失效和弹窗重新打开的状态边界。未新增独立安全侧栏入口。
- 登录挑战、Passkey AbortSignal、Telegram 配置状态和多 RP ID 由前后端真实接口串联。
- 默认前端补齐渠道、日志、任务插件、价格与模型界面；用户余额排序现在发送服务端排序
  参数并重置分页，原有“只改变表格图标”问题已由行为测试发现并修复。
- OpenAI / Responses 使用官方转换、模型观察、工具用量与终态；保留请求策略拦截、
  图片 quality/size 明细、读取上限、错误脱敏和首包/首事件计时。
- AWS / Vertex 接入新版 Claude/Gemini 规范化；Gemini 图片输出计数位于实际图片处理路径。
- 订阅全部六条升降级路径从数据库回读刷新分组缓存；性能统计先过滤不可见分组再汇总，
  保留自定义 Auto 分组和缓存指标。
- Notice/About/terms/privacy/homepage 接入 ETag 条件请求；保留响应合同与变更后缓存失效。
- 生产外置前端启动继续受显式 schema 门禁保护。新版 RelayKit 被复制进后端 Docker 构建
  上下文；本轮仅检查本地编译，尚未运行镜像构建来证明 Docker 链路通过。

### 前端最终定向检查

在 `web/default/` 中执行：

```sh
bun run typecheck
bun run i18n:sync
bun run test src/features/auth src/features/security src/features/profile src/features/users/components/dialogs/__tests__/user-binding-dialog.test.tsx src/features/redemption-codes/components/__tests__/redemptions-mutate-drawer.test.tsx src/features/users/components/__tests__/quota-display.test.tsx src/components/data-table/toolbar/__tests__/mobile-filter.test.tsx src/components/data-table/core/__tests__/pagination.test.tsx src/components/floating-window/__tests__/floating-window.test.tsx src/features/system-settings/auth
bun run test src/features/security/__tests__/enrollment.test.tsx src/features/redemption-codes/components/__tests__/redemptions-mutate-drawer.test.tsx src/features/users/components/__tests__/quota-display.test.tsx
bun run test src/features/users/components/__tests__/quota-display.test.tsx
```

- 最终类型检查通过；日志 `/tmp/newapi-full-integration-typecheck-final.log`。
- i18n 同步通过，六语言缺失键均为 0；已有未译条目未全部清除，不能称为全语言翻译完成。
- 首次上述组合测试为 26 文件、219 测试，196 通过、23 失败。补齐三个测试 fixture 的
  QueryClient 后，失败范围复跑为 26 通过、1 失败。最后修复服务端余额排序，单文件
  10/10 通过；没有为重复确认而重新执行已通过的其他范围。
- 日志分别为 `/tmp/newapi-root-ui-after-lint.log`、`/tmp/newapi-root-ui-lint-rerun.log`、
  `/tmp/newapi-users-quota-rerun.log`。
- 对根任务负责的 278 个 TS/TSX 文件执行 `bun x oxfmt -c .oxfmtrc.json --write <files>`，
  保留原始版权头；之后执行 `bun x oxlint -c .oxlintrc.json --format unix <files>`。
  最终 0 error、9 warning。确切文件参数见 `/tmp/newapi-root-lint-files.json`；
  结果见 `/tmp/newapi-root-format-final.log` 和 `/tmp/newapi-root-lint-final.log`。

### 渠道、日志、令牌和插件 UI 的修后证据

在 `web/default/` 中执行：

```sh
bun run test src/features/channels --reporter=json --outputFile=/tmp/newapi-channels-final-tests.json
bun run test src/features/keys/components/__tests__/api-key-listing.test.tsx src/features/keys/components/api-key-group-cell.test.tsx src/features/keys/components/api-key-group-combobox.test.tsx src/features/task-plugins/__tests__/plugin-icon-image.test.tsx src/features/task-plugins/__tests__/plugin-changelog-panel.test.tsx src/components/ai-elements/__tests__/code-block-editor.test.tsx --reporter=json --outputFile=/tmp/newapi-shared-changed-tests.json
bun run test src/features/channels/components/__tests__/model-mapping-editor.test.tsx src/features/channels/lib/__tests__/channel-field-update.test.ts src/components/json-code-editor --reporter=json --outputFile=/tmp/newapi-editor-tests.json
bun run test src/features/keys/components/__tests__/auto-group-order-editor.test.tsx src/features/keys/lib/api-key-form.test.ts src/features/keys/components/api-key-group-cell.test.tsx --reporter=json --outputFile=/tmp/newapi-auto-order-final-tests.json
bun run test src/features/keys/components/__tests__/auto-group-order-editor.test.tsx --reporter=json --outputFile=/tmp/newapi-display-name-tests.json
bun run test src/features/usage-logs/components/__tests__/task-artifacts-cell.test.tsx src/features/keys/lib/api-key-form.test.ts --reporter=json --outputFile=/tmp/newapi-logs-keyform-test.json
```

结果依次为 287/287、48/48、22/22、17/17、8/8、8/8。范围有重叠，不相加为唯一测试数。
对应日志为同名 `.log` 文件。早期组合命令的失败被精确范围修后报告取代，不将早期失败
日志改写为通过。

另一次失败范围复跑的精确命令为：

```sh
bun run test src/features/channels/components/__tests__/model-mapping-editor.test.tsx src/features/channels/lib/__tests__/new-api-channel.test.ts src/features/keys/components/dialogs/cc-switch-dialog.test.tsx src/features/keys/components/__tests__/api-key-group-combobox.test.tsx src/features/keys/components/__tests__/api-key-listing.test.tsx src/features/usage-logs/components/__tests__/detail-preview.test.tsx src/features/keys/lib/api-key-form.test.ts --reporter=json --outputFile=/tmp/newapi-failed-rerun.json
```

该轮 78/80，不能记为整轮通过；其中日志详情 20/20、CC switch 5/5、分组选择 3/3、
new-api-channel 11/11、api-key-form 6/6 已通过。其余 model mapping / API key listing
由上述 editor 22/22、shared-changed 48/48 报告覆盖修后结果。

### 后端与编译检查

在仓库根目录执行：

```sh
go test ./relay/channel/claude ./relay/channel/gemini ./controller -run 'Test(PublicContentConditionalRequests|OpenAIChatRequestToClaudeMessages_|Gemini|VLLMStatusPartialFailureAndCredentials|ImageStudio)' -count=1
go test ./service -run 'Test(.*HttpClient|.*HTTPClient|.*Transport|.*Attribution|.*Redirect|ImageStudio|ImagePricing|ImageOutput)' -count=1
go test ./relay/channel/claude ./relay/channel/vertex ./relay/channel/aws ./relay/channel/advancedcustom ./controller -run 'Test(FormatClaude|Claude|OpenAIChatRequestToClaude|VLLM|SGLang|ParseVLLM|PublicContent)' -count=1
GOPROXY=https://goproxy.cn,direct go test ./pkg/perf_metrics -count=1
GOPROXY=https://goproxy.cn,direct go test ./controller -run '^TestPerfMetricsExcludesInactiveGroupsAndIncludesNamedAutoProfiles$' -count=1
GOPROXY=https://goproxy.cn,direct go test ./relay/channel/openai -run 'Test(OpenAIStreamHandlersInterceptPolicyErrorsBeforeForwarding|ResponsesHandlersBillActualToolCalls|ResponsesToolBilling|OaiResponsesStreamHandlerBillsTokenLimitTermination)' -count=1
GOPROXY=https://goproxy.cn,direct go test ./relay/channel/openai -run 'Test(ChatHandlersBillPricedFunctionCalls|ResponsesHandlersBillActualToolCalls)' -count=1
GOPROXY=https://goproxy.cn,direct go test ./model -run 'Test(SubscriptionUpgradeRefreshesAuthoritativeGroup|AdminBindSubscriptionAlreadyInGroupDoesNotReportUpgrade|CompleteSubscriptionOrder_RejectsMismatchedPaymentProvider|AdminReset.*Subscriptions)' -count=1 -timeout=60s
GOPROXY=https://goproxy.cn,direct go test ./controller -run '^(TestGetStatusExposesPasskeyAndTelegramConfiguration|TestSendEmailVerificationRejectsAccountEmailPolicyViolations)$' -count=1 -timeout=90s
go test -tags external_frontend . -run '^$' -timeout=90s
```

上述命令均通过。部分 adapter 包在正则限定下没有匹配测试，不能称为对应适配器的行为验收。
默认嵌入前端的 `go test . -run '^$'` 曾因本地无 `web/default/dist` 失败；按实际外置前端
构建标签检查主程序后编译通过，没有生成虚假 dist 来绕过。

`/status` 定向测试覆盖 Passkey 显式/默认 RP ID 与旧配置去重、Telegram 配置状态以及
注册邮箱策略拒绝。未连接真实 OAuth 提供商。

### Responses WebSocket / HTTP 收尾结果

- WebSocket 真实适配器连接、连接复用、逐请求计费、流终态、错误脱敏及 HTTP/WS
  成功限额共享均已接线。失败响应不计成功限额；内存限流使用在途预约并在失败后释放。
- `response.failed` / 普通上游错误退款；明确超时或真实客户端取消且有可测输出/用量时
  才允许有限部分结算。无输出退款，普通 EOF 不作为成功结算，未完成图片/工具不计费。
- 流状态和协议终态仍记录在 `admin_info.stream_status`，未放宽普通用户的诊断可见性。
- 最后组合定向检查中，relay、common、middleware、relay/channel、relay/channel/openai、
  relay/helper、router 七个包通过；controller 剩余三项失败。原始日志
  `/tmp/newapi-request-policy-final-tests.log` 保留失败结果，不改写为整轮通过。
- 三项失败分别涉及错误对象可选字段、旧失败扣费预期、管理员流状态路径。保留实际错误
  协议与 KKAI 退款/隐私规则，更新行为断言后执行：

```sh
go test ./controller -run '^TestResponsesWebSocket(AmbiguousControlErrorClosesAndSettlesOnce|IgnoresLateErrorForPreviousResponse|ForwardsErrorForOtherResponseWithoutEndingRequest)$' -count=1 -timeout=60s
```

结果：通过，0.220s；日志 `/tmp/newapi-ws-control-final-rerun.log`。
此次定向检查已发现的失败项全部修复并在对应范围复验通过。最终 `git diff --check` 通过。

### 尚未完成的发布验收

- 主仓库全套测试、全前端测试和生产资源打包：本轮尚未执行；架构/依赖跨模块变化需要
  该层验收，执行前遵循全局 AGENTS 的完整命令与耗时确认规则。
- 真正 MySQL/PostgreSQL 上的新增迁移、维护锁/备份/恢复流程仍未验证。
- 生产镜像、当前生产状态、候选验收、前端激活与切流均未执行；仍需遵守发布 runbook
  及用户的切流前确认边界。

## 本地完整验收与发布授权（2026-10-06 更新）

本轮包含独立模块迁移、共享计费/鉴权/限流、ORM 与前端依赖升级；定向回归不能覆盖
所有包的旧测试和生产资源打包链路，因此需要最终跨模块验收。拟执行：

1. 仓库根目录：`go test -tags external_frontend ./... -count=1 -timeout=180s`。
2. `web/default/`：`bun run test`。
3. `web/default/`：`bun run build`。

用户随后明确授权“赶紧验收完 然后蓝绿上线啊”。上述完整验收已执行；修复发现的问题后，仅复验失败范围。
当前授权包含本次正常蓝绿发布的构建、候选验收、灰度、晋级和前端激活，无需重复确认。


### 本次实时发布前证据

从已确认 infra checkout 执行 `make newapi-status`，结果 HEALTHY、snapshot stable、rollback ready；
当次版本 `kkai-prod-20261005.1791225184-d28441bde`，active blue。该结果只代表本次采集时刻。
实际控制器 pin 为 `2cc7136164d33e4f17d3c8e10f30061afa4a5772`，与应用 contract 一致。
独立 standby 只读 schema 观察为 v9，digest
`sha256:4e65c4c6c49ad3ce1d87e3a144df3b0a57a3f408f3323226dd81b6b7a16972c1`。
已安装 feature9 preflight 返回 ready；本次按 feature `(9,9,9)` / external 普通应用发布，
不执行数据库迁移。当前前端为 format 2/API 2，
`kkai-frontend-20261006.1791253701-708e21b75`。
证据 `/tmp/newapi-production-before-rc41.log`、`/tmp/newapi-release-preflight.log`。

### 完整前端检查修后结果

`bun run test` 完整执行 218 套、2385 用例：初轮 2377 通过、8 失败。
旧 API 能力/侧栏预期和 QueryClient fixture 已修正，复杂可视计费的并发超时定向复验通过。
真实产品回归为兑换码创建后关闭抽屉导致生成结果状态丢失，已将导出结果放稳定父层。
失败范围复跑 66/67 后，兑换码两套最终 16/16 通过。最终 `bun run typecheck`、
`bun run build`、相关文件 oxlint 和 diff 检查均退出 0。
日志 `/tmp/newapi-full-ui-acceptance.log`、`/tmp/newapi-full-ui-failures-rerun.log`、
`/tmp/newapi-redemptions-export-final.log`、`/tmp/newapi-full-ui-typecheck.log`、
`/tmp/newapi-full-ui-build.log`、`/tmp/newapi-full-ui-lint.log`。

完整后端初轮日志为 `/tmp/newapi-full-backend-acceptance.log`；其失败项与超时后的未执行
范围正在收尾，不能将该初轮记为通过。Relay 图片分组策略测试改用 RelayKit 支持的
native tool payload（原 fixture 的裸 image_generation 不是有效 Chat tool），定向复验通过：
`go test -tags external_frontend ./relay -run '^TestTextHelperEnforcesImageGenerationPolicyAfterChatToResponsesConversion$' -count=1 -timeout=30s`。


### 后端完整验收已收齐

初次完整测试的其他包沿用已通过结果；失败范围修后如下：

```sh
go test -tags external_frontend ./model -count=1 -timeout=90s
go test -tags external_frontend ./service ./service/authz -count=1 -timeout=90s
go test -tags external_frontend ./controller -run 'Test(ModelPricingConversionDatabaseMatrix|VendorManagementDatabaseMatrix)' -count=1 -timeout=90s
go test -tags external_frontend ./controller -count=1 -timeout=180s -v
```

全部通过。最终 controller 包 6.815 秒，277 个顶层测试通过、3 个外部环境测试跳过。
修复真实问题：默认品牌读取在刷新定价期间写 vendor，触发递归刷新自锁；现仅生成稳定
内存展示项。有效 canonical BillingUsage 的审计来源也改为与实际结算优先级一致。
其他修复为测试的独立令牌/套餐缓存隔离、权限新增资源预期及真实计费模型 fixture。
本机未配置的真实 MySQL/PostgreSQL matrix 仍是覆盖缺口；本次发布不执行迁移，线上
schema9 精确观察和 image prerequisites 仍须通过控制器门禁。

生产 Dockerfile 修正：仅应用构建阶段复制 RelayKit 模块清单；独立 runtime-tools
模块没有该依赖，移除指向不存在 build context 路径的 COPY。正式镜像构建验证该链路。

最终验收日志归档到本机 `.local-releases/rc41-full-20261006/`。本次在现有
`production/kkrich` 上冻结新提交，保留已提交的 CC Switch 修复，不同步远端代码仓库。

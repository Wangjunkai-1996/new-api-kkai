# KK 商城实施与验证记录

更新：2026-10-28。首版应用代码已落地，商城仍处于本地开发验收阶段，未上线。生产目标数据库为 PostgreSQL；MySQL 仅是项目要求的兼容性覆盖。已通过的本地流程与未验范围分别记录，不能据此宣称真实渠道或生产验收完成。

源码基线：`production/kkrich` / `1ead6eb1fccbdc766b009aab755237d6f41d220e`，商城代码保留为未提交工作区修改。已恢复并保留其他对话完成的视频发布代码，包括 task-video-poster 注册；未重复应用 stash，未创建分支或 worktree，未提交、构建发布镜像或部署商城，也未执行生产迁移；本轮使用本机临时隔离 PostgreSQL 完成真实 HTTP 与浏览器联调，迁移和控制器验证均为本地证据。

## 实现范围

| 模块 | 已实现 |
| --- | --- |
| 商品与订单 | 分类、商品草稿/上下架、结构化政策及快照、单件下单、库存/人工容量预留、到期释放、关闭及幂等恢复 |
| 商城付款 | 复用既有 Epay 配置与 SDK；独立 attempt/receipt；签名回调、原交易查单、商户/环境/金额/币种/会话身份核实、未知/迟到/额外付款责任 |
| 交付 | 加密账号/卡密导入、自动发货、人工交付、补充资料、SLA、补发、限时查看及审计、风险暂停 |
| 售后与资金 | 售后会话/决策、退款批准/登记/操作记录/结果核实、原渠道证据、外部退款及拒付调整、金额与重复退赔保护 |
| 权限与秘密 | 主站限定、会话及对象归属、管理员操作证明、严格 JSON、HMAC 幂等摘要、独立 AES-GCM 密钥及 AAD、无正文审计、敏感结果仅组件内存 |
| 任务与账户 | 复用 leader/outbox；业务事务内核验租约与事件归属；恢复/通知/保留清理；账户软删与硬删的未结责任保护 |
| schema | schema 19，16 张 additive 商城表；canonical receipt 与物理形状门禁；新增独立 `shop-migration-v1` 控制器与固定 root proof；普通 CLI target19 仍只读；控制器仅本地实现，未安装 |
| 前端 | 站内商城入口、商品/结账/支付/订单/售后，运营设置/商品/库存/订单/售后/收款/退款；浅深色主题、22 张 WebP 切图；七语言 |

商城没有 TopUp、API quota 充值或充值返佣调用。首版只配置 Epay/CNY，退款的外部操作由运营在原渠道执行，再以证据登记与核实；按钮不自动代表资金已退。无购物车、物流、多商家或钱包支付。

## 复审修正

- 严格版本、重复 JSON 键、未知字段及回调参数/体积校验；错误响应不回显秘密。
- 退款批准绑定服务端可退金额；只有记录动作接受已绑定证明的公开备注；核实动作不能重写未绑定渠道引用。
- 支付状态合并创建/可信关闭/已付款事实，paid 优先；已准备的付款入口在刷新后可恢复，未知交易仍禁止新建。
- 外部退款/拒付争议下隐藏新增退款尝试和撤销入口，保留事实补录与核实。
- 历史分页不能丢失当前交付、付款、有效收款、退款及活动售后引用。
- Money 保留 int64 精度；设置版本溢出被拒绝；导入、下单不确定结果沿用原幂等身份。
- 退款/风险/调整在交付和秘密查看事务中重新检查；失去 leader 或 outbox 归属的 worker 不能提交业务写入。
- 真实 MySQL 测试发现默认 REPEATABLE READ 的初始普通读取会保留旧快照：相同 key 可能误报缺货，已提交退款可能被交付路径漏看。商城 MySQL/PostgreSQL 事务现显式采用 READ COMMITTED，保留原锁序、CAS 和唯一约束；SQLite 策略不变。未来调用者不得用不同隔离级别的外层事务包裹商城写入。
- 支付请求是否未提交由服务端事实决定：仅 `service.ShopPaymentNotCommittedError` 标记创建事务内已确认回滚的业务拒绝，控制器返回 `data.request_outcome=not_committed`。普通 400/409/404、提交结果不明或创建后失败不能据此换 key；前端曾有 unknown 时，即使后来返回该标记也保留原请求。
- 普通订单读取保留数据库故障为 500；已提交写入的后置响应读取失败统一返回 `409 SHOP_REQUEST_UNRESOLVED`，不误报为未提交，也不泄露无权限订单。已复现提交后停用用户导致普通 404 的路径，并验证修复后恢复用户、同 key 重放仍仅有一单和一次人工名额占用。
- 商城独立 keyring 支持 `SHOP_SECRET_KEYS_FILE` / `SHOP_FINGERPRINT_KEYS_FILE`，优先于对应内联变量；8 KiB 有界读取，文件缺失/损坏时不回退旧环境值。基础设施模板仅在显式配置后向两槽及 writer 挂载专用秘密文件；配置与销售开关分离。
- 实际浏览器暴露回调证据未齐时提前发布 `payment_received`，查单确认后同一收据又发布一次的问题。仅 matched/extra 才发布公开收款事件；review 只保留待核实责任。真实隔离 PG 确认为一笔收款、一次交付，问题是时间线误报；没有改写历史测试事件来制造干净截图。

## 实际验证

工作目录默认仓库根目录。命令只覆盖受影响包及直接依赖，没有执行 `go test ./...` 或前端全仓测试、lint、typecheck、生产 build。较早已通过且输入不变的命令不重复运行。

| 命令 | 结果与证据范围 |
| --- | --- |
| `go test ./common -run '^TestUnmarshalStrict' -count=1 -timeout=90s` | PASS，0.014s，严格 JSON 边界 |
| `go test ./model ./pkg/kkaimigrate -run '^TestShop\|^TestTenantAccountDeletionWaitsForFinancialSettlement$' -count=1 -timeout=90s` | PASS，模型 0.666s、schema 1.027s；schema 未随后修改 |
| `go test ./model -run '^TestShop\|^TestTenantAccountDeletionWaitsForFinancialSettlement$' -count=1 -timeout=90s` | PASS，1.517s；新增库存候选争抢、资金/交付/查看围栏、历史截断回归后 |
| `go test ./model -run '^TestShopCreateOrderSnapshotsTermsAndReservesLastImportedUnit$' -count=1 -timeout=90s` | PASS，0.089s；真实设置/商品发布/库存导入/下单，快照、幂等、条款变化和最后一件库存保护 |
| `go test ./service ./middleware ./controller -run '^TestShop' -count=1 -timeout=90s` | PASS，0.072s / 0.069s / 0.175s；服务/权限及控制器初次集成 |
| `go test ./service -run '^TestShopDeliveryProofBindsReplacementCase$' -count=1 -timeout=90s` | PASS，0.043s，补发证明绑定订单与售后版本 |
| `go test ./controller -run '^TestShopSettingsRejectsVersionOverflowWithoutMutation$' -count=1 -timeout=90s` | PASS，0.116s，不写入溢出政策版本或幂等结果 |
| `go test ./controller ./router -run '^TestShop' -count=1 -timeout=90s` | PASS，控制器/路由定向集成，包含公开备注、状态投影、回调体积及退款动作回归 |
| `go test ./controller -run '^TestShopOrder' -count=1 -timeout=90s` | PASS，0.119s；补充资料重新请求时不误标完成，查单时间引用当前支付尝试 |
| `go test ./pkg/kkaimigrate ./cmd/kkai-migrate -run '^(TestContractForDialectRequiresTenantSchema\|TestDescribeContractJSONRequiresTenantSchema\|TestShopOrdinaryTargetCannotMutate)$' -count=1 -timeout=90s` | PASS，0.045s / 0.034s，CLI/runtime 声明与禁止普通入口安装 |
| `go test ./service -run '^(TestKKAIOutboxRuntimeJob\|TestKKAIOutboxRuntimeWorker)' -count=1 -timeout=90s` | PASS，0.070s，共享队列隔离/重试/取消/预算 |
| `go test . -run '^TestApplicationBackgroundJobsDeclareLeaderWriteBoundary$' -count=1 -timeout=90s` | PASS，0.071s，共享任务注册 |

前端工作目录：`web/default/`。

| 命令 | 结果 |
| --- | --- |
| `bunx vitest run src/features/shop/lib.test.ts src/features/shop/recovery.test.ts src/features/shop/hooks/use-shop.test.tsx` | PASS，3 文件 25 项，1.58s；Money/恢复/敏感响应生命周期及缓存隔离 |
| `bunx tsgo --project tsconfig.shop.tmp.json --noEmit` | PASS，商城、后台、路由、导航及受影响依赖闭包；不等于全仓 typecheck |
| `bunx oxlint -c .oxlintrc.json src/features/shop src/features/shop-admin src/routes/shop src/routes/_authenticated/shop-admin src/hooks/use-sidebar-data.ts src/hooks/use-top-nav-links.ts src/features/auth/secure-verification/types.ts` | PASS，无诊断 |
| `bunx oxfmt --check src/features/shop src/features/shop-admin src/routes/shop src/routes/_authenticated/shop-admin src/styles/shop.css src/hooks/use-sidebar-data.ts src/hooks/use-top-nav-links.ts src/features/auth/secure-verification/types.ts` | PASS，56 文件 |
| `node scripts/add-missing-keys.mjs`，随后 `bun run i18n:sync` | PASS，7 语言各新增 299 项，共 2093 项；未引入新依赖 |
| `node /tmp/kk-shop-check-i18n.mjs` | PASS，411 个实际商城/导航键在七语言中无缺失、无插值差异；品牌及法语同源词保留 |
| `git diff --check -- src/features/shop src/features/shop-admin src/routes/shop src/routes/_authenticated/shop-admin src/styles/shop.css src/hooks/use-sidebar-data.ts src/hooks/use-top-nav-links.ts src/features/auth/secure-verification/types.ts src/i18n/locales` | PASS |

付款页早期集成另执行以下定向回归，当时合计 **31 项不同前端测试通过**（历史记录；“首次明确拒绝”的判断已被下述服务端 `not_committed` 契约收紧，不能继续按 HTTP 状态推断）：

- `bunx vitest run src/features/shop/pages/payment.test.tsx src/features/shop/hooks/use-shop.test.tsx`：PASS，12 项，2.83s。覆盖首次明确拒绝解锁、网络未知后保持原幂等身份、已确认关闭恢复、付款提示与允许动作一致。
- 测试类型/lint 修正后 `bunx vitest run src/features/shop/pages/payment.test.tsx`：PASS，6 项，2.54s。
- `bunx tsgo --project tsconfig.shop.tmp.json --noEmit`：最终退出码 0。
- `bunx oxlint -c .oxlintrc.json src/features/shop/api.ts src/features/shop/hooks/use-shop.ts src/features/shop/pages/payment.tsx src/features/shop/pages/payment.test.tsx`：PASS。
- `bunx oxfmt --check src/features/shop/api.ts src/features/shop/hooks/use-shop.ts src/features/shop/pages/payment.tsx src/features/shop/pages/payment.test.tsx`：PASS，4 文件。
- `git diff --check -- src/features/shop/api.ts src/features/shop/hooks/use-shop.ts src/features/shop/pages/payment.tsx src/features/shop/pages/payment.test.tsx`：PASS。
- `node /tmp/kk-shop-check-i18n.mjs`：最终 411 键 × 7 语言通过；本次修正未新增文案。

集成静态检查：`git diff --check` 通过；未跟踪文本的空白扫描通过；本地文档链接检查无缺失，V2 原型 SHA256 保持 `17b79c15ffe5045bcaecc1cfe88782d317466fc134f1a01c5076bf4002fee8fc`。检查脚本仅使用 pathlib/re/hashlib 读取文档，不运行应用或外部网络。

前端临时配置、路由检查 shim、翻译填充脚本已清理，精确副本保留在 `/tmp/kk-shop-verified-tsconfig.shop.tmp.json`、`/tmp/kk-shop-verified-shop-router-check.tmp.ts`、`/tmp/kk-shop-verified-add-missing-keys.mjs`。它们仅为此次检查记录，不是新增生产依赖或长期验证工具。后续应据实际改动重新生成定向配置。

## 本轮隔离联调补充（2026-10-28）

以下 Go 命令在仓库根目录执行；真实数据库通过仅绑定本机的临时凭据连接，每个 fixture 使用独立库/schema，输出不包含 DSN。schema 命令外层另设 120 秒进程上限。全部结果均为本地开发证据。

| 精确命令 | 结果与边界 |
| --- | --- |
| `go test ./service ./controller -run '^(TestShopPaymentRejectionDistinguishesCommittedAttempt\|TestShopSignedCallbackAndQueryNeverCreditRecharge\|TestShopPaymentErrorMarksOnlyProvenUncommittedRequest)$' -count=1 -timeout=90s` | PASS，service 0.167s / controller 0.073s；区分创建事务拒绝与 attempt 已提交后的同名错误；本地 TLS 假网关验证错误签名拒绝、重复签名回调、原交易查单、单份收据/履约事件以及 TopUp 数量为零、quota 不变。**不等于真实商户验收** |
| `go test ./controller -run '^TestShopBuyerInputHTTPRequiresLiveOwnedSession$' -count=1 -timeout=90s` | PASS，0.120s；真实 JWT/持久化会话、UserAuth/AdminAuth、主站/session 守卫和 AES-GCM buyer_input：本人/管理员成功，其他人/匿名/撤销/子站拒绝，Origin 写保护、订单 DTO 无明文、全部响应 no-store/no-referrer、仅两次授权读取审计。可信 Host 使用 fixture，未挂完整 router/schema/rate-limit；PUT 只验证 Origin 拒绝与同源非法体 400，不代表有效写入或完整服务器链路 |
| `go test ./model -run '^TestShopLiveDatabaseContracts$' -count=1 -timeout=120s -v` | PASS，6.972s；MySQL 5.7 五项 5.19s，PostgreSQL 18.4 五项 1.74s；独立连接覆盖同 key 下单/最后库存、库存候选竞争、退款/交付/领取、退款提交与交付交错、leader/outbox 失效与真实行锁 |
| `go test ./model -run '^TestShopLiveDatabaseContracts/(mysql\|postgres)/order_idempotency_and_last_stock$' -count=1 -timeout=90s -v` | PASS，0.809s；加强两个初始授权读取的确定性 barrier 后，仅重跑修改的子项 |
| `go test ./model -run '^TestShop\|^TestTenantAccountDeletionWaitsForFinancialSettlement$' -count=1 -timeout=90s` | PASS，1.673s；READ COMMITTED 改动后 SQLite 回归；此命令未设置 live DSN，真实数据库子项明确 SKIP，不重复计为数据库验收 |
| `go test ./model -run '^(TestShopOrderReadPreservesDatabaseErrorsAndHidesInactiveUsers\|TestShopCommittedReplayRechecksActorAndTenant\|TestShopCreateOrderSnapshotsTermsAndReservesLastImportedUnit)$' -count=1 -timeout=60s` | PASS，0.328s；最后修正授权查询错误传播后验证公开订单读取保留 `context.Canceled`，缺失/禁用用户仍隐藏为 `SHOP_NOT_FOUND`，并回归已提交重放重新鉴权及下单。新增用例修前 FAIL（0.210s）；本次没有复现提交后 HTTP 故障或重复订单 |
| `go test ./pkg/kkaimigrate -run '^TestShopRealDatabaseSchemaContract/postgres$' -count=1 -timeout=90s -v` | PASS，最终 22.82s；PostgreSQL 18.4 完整 canonical 18→19、16 表字段/索引、重放保留旧回执和钱包数据、旧钱包/发票/工单/tenant 门禁、缺/错回执及丢索引拒绝 |
| `go test ./pkg/kkaimigrate -run '^TestShopMySQLSchemaDDLContract$' -count=1 -timeout=90s -v` | PASS，2.02s；MySQL 5.7 的 schema 19 DDL、16 表模型、索引、真实重复预留 1062 及重复执行；**不代表完整历史迁移链通过** |
| `KKAI_SHOP_MYSQL_BOOTSTRAP_TEST=1 go test ./pkg/kkaimigrate -run '^TestShopRealDatabaseSchemaContract/mysql$' -count=1 -timeout=90s -v` | 历史前置阻塞的显式复现入口。首次执行 FAIL，15.26s：schema 18 创建 `wallet_topup_runs` 时 `cursor BIGINT` 未引用 MySQL 保留字，返回 1064；尚未进入 schema 19。额外开关随后加入以明确区分此复现，未把失败断言改成 PASS |

上述认证 HTTP 测试首轮因 fixture 缺少独立双 keyring 失败，补全 fixture 后通过；未改变权限规则以迎合测试。本轮新增付款错误契约和认证 HTTP 测试经独立只读复审，未新增未关闭发现；该结论不扩展为整个商城或生产已通过。

MySQL 的 schema 18 SQL 与 canonical digest 保持不变。完整 MySQL 18→19 链路仍受此前置错误阻断；业务矩阵使用独立模型表，不能替代 canonical 迁移。两个临时容器、匿名数据卷、测试 namespace 及凭据文件均已清理，现有用户容器未改动。未运行全量测试或 `-race`。

最后授权查询错误传播修正的 `gofmt -l model/shop.go model/shop_test.go` 无输出；只重跑上述三个相关模型用例，没有重跑真实数据库矩阵或浏览器场景。

付款页采用服务端 `not_committed` 契约后的最终前端回归（工作目录 `web/default/`）：

- `bunx vitest run src/features/shop/pages/payment.test.tsx src/features/shop/hooks/use-shop.test.tsx`：PASS，2 文件 14 项，2.19s。当前原 25 项加付款页 8 项，共 **33 项不同前端用例分批通过**；不能把早期 31 项再重复累加。覆盖有标记且从未不确定时解锁、裸 4xx 保留原请求、先前 unknown 后即使收到标记仍保留、可信关闭/失败恢复及提示与动作一致。
- `bunx oxlint -c .oxlintrc.json src/features/shop/api.ts src/features/shop/pages/payment.tsx src/features/shop/pages/payment.test.tsx`：PASS，退出码 0。
- `bunx oxfmt --check src/features/shop/api.ts src/features/shop/pages/payment.tsx src/features/shop/pages/payment.test.tsx`：PASS，3 文件，259ms。
- 最后补强既有 transport/unknown 两个用例：后续 409 均显式带 `not_committed`，保留锁定与相同 key/body 的断言通过。`bunx vitest run src/features/shop/pages/payment.test.tsx`：PASS，1 文件 8 项，2.46s；仅重跑改动的付款测试，没有新增用例，累计仍为 33 项。`bunx oxlint -c .oxlintrc.json src/features/shop/pages/payment.test.tsx`、`bunx oxfmt --check src/features/shop/pages/payment.test.tsx`、`git diff --check -- src/features/shop/pages/payment.test.tsx` 均 PASS。

### React 实现浏览器验收

使用隔离无头 Google Chrome 和随附 Playwright 检查当前 React 实现，未使用共享浏览器配置。开发服务命令（`web/default/`）：`KKAI_EXTERNAL_FRONTEND_BUILD=1 bunx rsbuild dev --host 127.0.0.1 --port 5178 --strict-port --no-env`；首次开发编译 3.23s，**不是生产构建门禁**。应用 API 全部由确定性的内存 fixture 接管，外部请求被阻止；二维码、交付内容和验证证明均为无效测试数据，没有真实付款或生产后端。

| 已检查场景 | 观察结果 |
| --- | --- |
| 目录 | 1440×1000 / 390×844、浅色/深色四种组合；6 张商品卡片及插图加载，无整页横向溢出 |
| 移动商品与结账 | 未勾条款时不发送下单 POST；勾选后提交并进入付款页 |
| 付款恢复 | 有 `not_committed` 标记的首次 409 可释放方法/key；刷新到 v2 并变更方法后使用新 key；503 保留锁定，重试 key/body 完全相同；二维码可见且没有矛盾的禁止付款提示 |
| 交付查看 | 初始遮罩，明确操作后显示，`pagehide` 清理；localStorage/sessionStorage 无交付秘密 |
| 商品管理 | 负价格在 PATCH 前被阻止；`29.50` 精确转为 `2950` 最小单位；分类/封面清空字段正确；390px 无整页溢出 |
| 人工交付 | 提交前进入验证对话框；证明绑定 `shop.delivery.create`、`first` 及相同幂等 key；验证提供者为 mock，未证明真实验证链 |
| 库存导入 | 错误行在预览 POST 前被拒绝；有效合成行完成预览/带 key 提交并清空输入，浏览器存储无库存秘密 |

最终记录 **0 页面错误、0 未知 API 路径**。早期 status/notice 形状、invitations 路径及 import-commit 路由问题均修正于测试 fixture，不属于生产功能修复。脱敏断言及相对截图路径见[浏览器证据 JSON](./kk-shop-screenshots/implementation/browser-review.json)，共 14 张实现截图，例如[桌面浅色目录](./kk-shop-screenshots/implementation/01-catalog-desktop-light.png)、[移动深色目录](./kk-shop-screenshots/implementation/04-catalog-mobile-dark.png)、[付款页](./kk-shop-screenshots/implementation/07-payment-mobile-light.png)、[人工交付验证](./kk-shop-screenshots/implementation/13-admin-delivery-verification-mobile-dark.png)。截图中的 TanStack/React devtools 是开发控件。

付款契约最终修改后，`bunx tsgo --project tsconfig.shop.tmp.json --noEmit` 定向检查再次 PASS（退出码 0）；商城、后台及路由闭包由临时配置和路由 shim 覆盖，精确副本位置见上文。前述商城文件范围的 `git diff --check -- src/features/shop src/features/shop-admin src/routes/shop src/routes/_authenticated/shop-admin src/styles/shop.css src/hooks/use-sidebar-data.ts src/hooks/use-top-nav-links.ts src/features/auth/secure-verification/types.ts src/i18n/locales` 同样 PASS。隔离浏览器已关闭，开发服务 Ctrl-C 后退出码 130；`lsof -nP -iTCP:5178 -sTCP:LISTEN` 未发现监听（退出码 1），临时类型配置已删除。

## 继续联调与发布准备（2026-10-28）

| 精确命令 | 结果与边界 |
| --- | --- |
| `go test ./controller -run '^(TestShopCommittedOrderSurvivesAuthorizationChangeBeforeResponse\|TestShopOrderReadOwnershipAndNormalResponseSecrecy\|TestShopBuyerInputHTTPRequiresLiveOwnedSession)$' -count=1 -timeout=60s` | PASS，0.213s。新故障注入用例修前返回 404 而失败；修后返回未决错误、隐藏资源、同 key 恢复仅一单和一次人工容量占用。使用控制器与 SQLite，完整 HTTP/PG 证据另列 |
| `bunx vitest run src/features/shop/hooks/use-shop.test.tsx`（`web/default/`） | PASS，7 项，1.27s；首次 409 `SHOP_REQUEST_UNRESOLVED` 与后续普通拒绝均不清恢复标记、不换 key。单文件 oxlint/oxfmt 同样通过；累计不同前端用例 34 项 |
| `go test ./service -run '^(TestShopMountedKeyringsOverrideEnvironmentAndFailClosed\|TestShopSecretBindingAndKeyRotation\|TestShopKeyringRejectsAmbiguousOrSharedSecrets)$' -count=1 -timeout=60s` | PASS，0.049s；挂载文件优先、两类 keyring 可完成加解密/HMAC，缺失/空/过大/非法文件 fail closed，保留旧 key 与 AAD/独立密钥保护 |
| `bash roles/newapi/tests/test-compose.sh`（同级 `kkai-infra/`） | PASS，35.76s，外层 180 秒上限；真实 Ansible 渲染与 Docker Compose 解析：默认不注入商城配置，启用后 blue/green/writer 一致挂载独立文件且无内联密钥，无关服务不挂载；错误 ID/缺失文件/宽松权限拒绝。本地角色范围检查，不是完整 `make check` 或生产 dry-run |

- `ansible-playbook playbooks/newapi-stage-candidate.yml --syntax-check`（infra）：PASS，0.69s；`shellcheck -S warning roles/newapi/tests/test-compose-shop.sh roles/newapi/tests/test-compose.sh`：PASS。
- `go test ./cmd/kkai-migrate -run '^TestShop' -count=1 -timeout=90s`：首轮 3 项通过，inspection 夹具缺少显式删除条件失败；修复夹具后 `go test ./cmd/kkai-migrate -run '^TestShopInspectionIsReadonlyAndRejectsUnreceiptedObjects$' -count=1 -timeout=90s` PASS，1.120s。未把初次整条命令计作通过。
- `go test ./cmd/kkai-migrate -run '^(TestReseller|TestRequestTicket|TestInvoice|TestWalletPayment)' -count=1 -timeout=90s`：PASS，17 项，6.015s；旧只读入口支持新 runtime，旧写入 proof 的版本/摘要权限保持冻结。
- Infra：`PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v scripts.tests.test_newapi_shop_migration scripts.tests.test_newapi_schema_runtime scripts.tests.test_newapi_installed_cli` 与 `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v scripts.tests.test_newapi_shop_migration scripts.tests.test_newapi_tenant_online_evidence scripts.tests.test_newapi_tenant_online_preflight scripts.tests.test_newapi_wallet_payment_migration` 均 PASS，合计 53 个不同专项测试（重复 shop 用例不累加）。
- Infra：`bash scripts/tests/test_newapi_tenant_schema_contract.sh`、`bash -n scripts/production/kkai-newapi-manual-deploy scripts/production/install-newapi-manual-deployer.sh scripts/tests/test_newapi_tenant_schema_contract.sh`、改动 `git diff --check` 均 PASS。

经用户明确批准，2026-10-28 在同级 `kkai-infra/` 执行完整 `make check`：PASS，退出码 0，296.548s。25 个 Ansible playbook 语法检查、完整 `make policy` 及末尾 ShellCheck 均通过，运行中无需修复；见[脱敏检查证据](../../../kkai-infra/docs/evidence/2026-10-28-shop-infra-check.json)。这是修改中工作区的本地验证，没有执行生产主机 dry-run、安装控制器、改应用 pin、生成真实商城密钥或写生产配置；应用全套测试仍跳过。

安装验收仍是独立门禁。具体发布次序与回退前提见同级 infra 的 [runbook 31](../../../kkai-infra/docs/runbooks/31-newapi-shop-schema.md)。必须先在 schema18 接受两份实际源码里程碑 A/B，两者均具备 max19 兼容与商城存量处理能力，且不可变 Compose 均已配置 keyring；其后独立迁移19。当前合并工作区不等于已形成、构建和验收这对回退版本。

## 真实 PostgreSQL 与 HTTP 验收

本轮在 PostgreSQL 18.4 隔离库中启用实际 `SetApiRouter`、主站识别、密码登录、会话、操作验证、leader 与 outbox；仅外部 Epay 由本机 TLS 模拟器代替。HTTP 驱动执行 33 个断言、31 个不同检查，全部通过，见[脱敏 HTTP 报告](./kk-shop-screenshots/implementation/real-http-review.json)。

- 自动交付：真实登录、同 key 下单与恢复、匿名/其他用户拒绝、签名回调、实际维护任务查单和 outbox 履约、普通 DTO 遮罩、本人限时查看与唯一收据。
- 退款：未验证操作拒绝，真实密码 proof 绑定退款批准，批准即禁止领取，操作登记保持 unknown，凭据核实后才成功；退款只产生一笔 125 分调整，API quota 仍为 12345，TopUp 为零。
- 人工交付与库存：真实预览/导入、幂等恢复、重复库存识别、列表不含明文或密文；人工单实际收款后等待操作，密码 proof 验证交付、重放不重复、本人领取内容一致。

准备命令为 `go test ./pkg/kkaimigrate -run '^TestShopHTTPFixtureSchema$' -count=1 -timeout=120s`（PASS，12.174s）；服务命令为 `go test ./router -run '^TestShopLiveHTTPServer$' -v -count=1 -timeout=0`，它是显式开启的本机验收服务，不是无超时测试套件。原驱动命令及结果完整保留在 JSON；整理后的入口为 `python3 scripts/kkai/tests/shop-http-acceptance.py PRIVATE_FIXTURE_DIR` 和 `python3 scripts/kkai/tests/shop-http-admin-acceptance.py PRIVATE_FIXTURE_DIR`，准备和清理步骤见[本地复跑说明](./kk-shop-local-acceptance.md)。脚本语法检查通过，未因移动脚本重复执行业务链。

夹具随后补齐全局状态恢复，首轮 `go test ./router -run '^$' -count=1 -timeout=60s` 仅编译通过（0.071s，无测试执行），当时保留浏览器正在使用的后端。后续新进程的实际启动与正常 STOP 另由下述 6 项定向 HTTP 验证完成。审查确认 DB 入口只接受专用 loopback 临时库；驱动禁代理/重定向且只接受本机服务与对应 TLS 网关，不输出私密响应。

控制器 fixture 同时清空两个 `*_KEYS_FILE`，防止本机配置覆盖合成 inline keyring。`SHOP_SECRET_KEYS_FILE=/nonexistent/fixture-secret SHOP_FINGERPRINT_KEYS_FILE=/nonexistent/fixture-fingerprint go test ./controller -run '^(TestShopCommittedOrderSurvivesAuthorizationChangeBeforeResponse|TestShopBuyerInputHTTPRequiresLiveOwnedSession|TestShopCommittedRefundReplayChecksActorAndBodyBeforeProof|TestShopSettingsRejectsVersionOverflowWithoutMutation)$' -count=1 -timeout=60s` PASS，0.247s，覆盖四个实际使用 keyring 的控制器场景。

边界：本机 HTTP 使用非 Secure cookie；生产 HTTPS、反向代理、2FA/Passkey/OAuth 未验。真实商户扣款、退款和对账没有发生，仍由用户接手。

## React 与真实后端浏览器验收

隔离 Chrome 使用真实买家/管理员密码登录，React 连接上述 Go/PG 服务，没有应用 API mock 或注入 token。15 个场景检查完成（含最后两项视觉修正），捕获 130 个商城 HTTP 响应均成功，0 个页面 JavaScript 异常；详见[真实浏览器 manifest](./kk-shop-screenshots/real-integration/manifest.json)与其中 16 张截图。

- 完成条款拦截、下单、模拟商户付款、后台任务自动交付、秘密领取后 pagehide 清理及存储检查。
- 完成管理员真实密码 proof 的人工交付、买家领取；售后申请/补充、受理/公开回复/解决及买家结果展示。
- 商品价格从 125 改为 135 分，旧订单仍为 125 分；库存非法行拦截，真实预览/提交插入一条并清空输入。
- 修复空订单时间线、后台概览与子导航同时高亮、售后 Resolution 误译；管理端查看秘密的必填原因增加可见标签。390px 深色订单页的长订单号完整换行，无裁剪；保存[截图](./kk-shop-screenshots/real-integration/15-buyer-order-mobile-dark-real.png)，另有[后台订单](./kk-shop-screenshots/real-integration/08-admin-order-timeline-fixed.png)和[售后结果](./kk-shop-screenshots/real-integration/12-aftersale-resolution-buyer-real.png)。

前端工作目录 `web/default/`：`bunx vitest run src/features/shop/components/common.test.tsx` PASS，6 项、1.69s；`bunx tsgo --project tsconfig.shop.tmp.json --noEmit` PASS。7 个修改文件的 oxlint/oxfmt、`node /tmp/kk-shop-check-i18n.mjs`（422 键 × 7 语言）及 diff 检查均 PASS，精确命令见 manifest。前端至此累计 40 个不同用例分批通过，不是一次全套运行。

最后的原因标签/换行改动完成单文件 oxlint、oxfmt 和受影响类型闭包检查，并重拍两张截图。所有本轮隔离浏览器、前端开发服务及临时类型配置均已关闭/清理，5178 端口无监听。

网络错误并非零：初始 fixture 重启曾使 status/notifications 返回 504，旧会话返回 401；最终 READY 后重新登录。独立邀请服务未启动，`/invitations/api/invitations/status` 返回 504，后台早期 toast 来源于此；外部 GitHub 版本查询被隔离浏览器阻止。上述没有用应用 mock 绕过，邀请功能不在此次商城验收范围。截图中的 devtools 为开发控件。

截图保留修复前后端已经写入的两条收款事件。修复后运行 `go test ./model ./service -run '^(TestShopVerifiedReceiptRequiresFullIdentityAndRecovers|TestShopDistinctExtraReceiptKeepsItsOwnPaymentEvent|TestShopLateReceiptForRetiredBuyerRetainsResponsibility|TestShopSignedCallbackAndQueryNeverCreditRecharge)$' -count=1 -timeout=60s` PASS，model 0.210s / service 0.104s；新增断言修前复现失败，修后 review 及重试零公开收款事件、补证后重试仅一条，真实额外交易各保留一条但只触发一次履约。未重启旧浏览器后端或把旧截图当作这项修复后的重跑证据。

最终使用新私有目录、新 PostgreSQL 库和当前修复源码的新 HTTP 进程，仅复验受影响链路。`python3 scripts/kkai/tests/shop-http-acceptance.py --payment-events-only /tmp/kk-shop-http-final-yeygdxf1` PASS，6 项、30.495s：签名回调后 review 保留一笔收据、零收款事件/交付；实际查单与 worker 完成后为一笔收据、一条公开收款事件、一次交付，真实 HTTP 时间线也只有一条。schema fixture 准备 PASS，13.872s；新服务收到 STOP 后退出码 0、测试 PASS，3000 无监听。总运行 59.065s（180s 外层上限），没有重复退款/导入等整套验收。精确 argv、源码摘要及清理证据保存在 HTTP JSON 的 `post_fix_real_http`，历史 33 个断言不与本次 6 项混加为不同测试总数。

本轮两套 HTTP 夹具的容器、匿名卷、进程和私有凭据目录均按实际身份删除并验证，3000、5178 及两个临时 PostgreSQL 端口关闭。保留仓库中的脱敏报告、截图和复跑驱动；未清理用户其他容器或服务。

## PostgreSQL 迁移 CLI 实际演练

Linux ARM64 临时 CLI 在隔离 PostgreSQL 中执行 8 项 root-proof 检查，全部通过，运行 65.59s，临时编译 7.05s。验证只读 plan、过期证明/错误权限/可写挂载/旧 max18 回退版本拒绝、有效 root 0400 只读证明 apply、完成后以只读数据库身份重放。注入 `shop_stock_units` DDL 失败后，16 张商城表均回滚且收据保留 schema18；迁移账户最终 NOLOGIN、零会话。

精确编译、fixture、容器 CLI 命令与边界见[迁移演练 JSON](../../../kkai-infra/docs/evidence/2026-10-28-shop-root-proof-rehearsal.json)。数据库容器、proof 卷与临时二进制已清理。运行时身份为合成证明，不能替代正式 AMD64 不可变镜像、生产三进程身份、已安装控制器 self-test、实际 A/B 回退版本验收。

## 剩余验收与启用条件

1. **真实 HTTP 核心交易链已通过；浏览器按实际场景记录。** 31 个不同 HTTP 检查覆盖登录、导入、下单、受控付款、自动/人工交付与退款。其他浏览器版本、完整无障碍矩阵以及未列明异常流程没有据此视为通过。设计原型的 11 页/26 场景截图不作为当前 React 实现证据。
2. **真实支付：用户接手，代理未验证。** 真实 Epay 商户能力、实际付款/到账、重复或迟到通知、退款取证及原充值交叉投递仍需按下方清单验收。本地 TLS 假网关和签名测试只证明受控协议路径，不证明商户已开通、真实交易已成功或生产回调可达。
3. **数据库已有定向真实实例证据，仍有明确缺口。** PostgreSQL 18.4 完整 18→19、MySQL 5.7 schema 19 DDL 和两库独立连接业务矩阵已通过；MySQL 完整历史迁移仍被 schema 18 `cursor` 1064 阻断。未验 PostgreSQL 9.6、其他版本组合、实际多进程故障转移及线上部署拓扑，不能从双连接测试扩称全部环境通过。
4. **schema 安装与发布未执行。** 专用控制器、runbook、本地 CLI 演练与获批的完整 infra `make check` 均已完成；仍需实际控制器安装、正式 AMD64 镜像验收、精确源码 A/B 版本和回退证据，再按独立授权执行生产扩展。不能运行普通 CLI 或 AutoMigrate 绕过，也不能将合成 proof 当作生产证明。
5. **运营和密钥需实际配置。** 既有 Epay 商户配置、站点 HTTPS 回调地址、销售开关、金额/库存/SLA/保留规则需真实校验。生产使用 `SHOP_SECRET_ACTIVE_KEY_ID` / `SHOP_SECRET_KEYS_FILE` 和 `SHOP_FINGERPRINT_ACTIVE_KEY_ID` / `SHOP_FINGERPRINT_KEYS_FILE`；两个独立 JSON keyring 使用不同 256-bit key，文件为运行 UID/GID、0400、最多 8 KiB。配置缺失/损坏时拒绝服务，不回退内联旧值。A/B 两份 Compose 都要具备挂载，配置密钥不等于开启销售。保留旧 key 以读取历史密文与身份指纹；未提供在线批量重加密工具。商品素材、渠道及政策未确认前不应启用新单。
6. **订单提交后权限变化恢复已定向验证。** 确定性注入实际用户停用，确认未决错误保留原 key；恢复权限后返回原订单、人工名额仅占用一次。普通 GET 继续隐藏无权资源。此结果不等于所有动作和进程故障均已穷尽验证。

详细验收基线继续见[质量与验收规格](./kk-shop-quality-and-acceptance.md)，产品行为见[PRD](./kk-shop-prd.md)与[业务规则](./kk-shop-business-rules.md)。

## 真实支付用户自测清单（尚未上线）

商城当前未上线，以下清单记录用户接手的未来验收；不要求现在请求生产，也不授权安装 schema、发布或打开销售开关。先完成正式环境准备和独立发布/迁移门禁，再由用户在确认的商户与允许的小额订单中验证：

1. 确认实际 Epay 商户、币种、付款方式、回调地址与查单/关闭/退款取证能力；商品售价和交付/售后政策已确认，使用已配置可售的专用小额测试商品。记录订单号、渠道交易号、金额/币种和时间，报告不含密钥或交付秘密。
2. 正常支付后核对真实到账、同一商城订单的实收和唯一交付；回跳本身不标记成功。对比付款前后 API 余额/TopUp，商城购买不产生充值额度或充值返佣。
3. 刷新、返回付款页或网络结果未知时恢复原订单/原尝试，不再付第二次。仅明确未提交标记且此前无 unknown 才能重新组织请求；可信关闭/失败由渠道证据确认后才开始下一尝试。
4. 在渠道允许的重放/测试设施中验证重复或迟到通知，以及正常充值与商城回调隔离；不为了造异常主动重复扣款。核对一笔实收不会重复交付或入额度，异常款有独立待办。
5. 验证自动交付或人工补资料/交付、限时查看及售后；如需退款，由运营在原渠道操作并登记证据，确认真实返还后才核实成功。未知结果不重复退款，商品退款与额外款退款分开核对。
6. 记录 PASS/FAIL、脱敏证据和未测项，发生金额/归属/重复交付异常即暂停新单并保留原责任。只有用户提供实测结果后，才把对应“用户接手，代理未验证”项目更新为已验收。

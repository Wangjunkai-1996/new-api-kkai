# KKAI 发票中心实施与验证记录

- 日期：2026-10-23。
- 基线：[PRD](./invoice-center-prd.md)、[开发文档](./invoice-center-development.md)、[UI规格](./invoice-center-ui-spec.md)、[冻结设计](./invoice-center-design-preview.html)。
- 状态：前后端、独立v17迁移应用协议及基础设施控制器已在本地实现，并完成下列定向验证；尚未安装新控制器、执行生产迁移或部署本功能。最终11表已通过SQLite及隔离PostgreSQL/MySQL schema定向验证；真实私有对象存储、SMTP和前后端端到端联调仍待验收。

## 1. 已实现范围

| 能力 | 实现行为 |
| --- | --- |
| 付款来源 | 充值与直接现金付款的会员/订阅订单；资格来自实收金额、币种、付款回执。会员到期/额度用尽不影响已付款资格。余额购买、赠送和缺少可信实收的历史订阅明确不可用。 |
| 申请 | 三步选择、资料、核对提交；搜索/来源/日期/币种、分页保留选择、最多50笔整单合并、提交前quote；服务端事务重新核验。 |
| 重复与金额 | 充值及会员使用同一真实渠道回执派生的全局claim，跨来源、跨owner只能保留一个活动申请；排除会员/历史订阅TopUp镜像，其他owner的占用不泄漏申请号。使用整数最小单位，接口金额为字符串，前端BigInt合计和格式化。 |
| 常用抬头 | 本人新增、查看、编辑、选择、软删除；CAS版本保护；申请使用不可变资料快照。资料和敏感说明使用专用AES-GCM密钥加密。 |
| 管理 | 待审核、待开票、异议、全部队列；计数遵守统一筛选规则；审核通过/拒绝、上传后明确交付、回复和更正补发、财务暂停/恢复。 |
| 收票反馈 | 当前版确认、已确认后仍可反馈、异议追加说明/佐证、同版回复后重核、新版补发后重确认；历史正式文件保留。 |
| 文件 | PDF/PNG/JPEG，单文件20MiB，私有S3兼容存储；受权代理预览/下载、内容校验、暂存过期清理，用户不可见未交付草稿。 |
| 付款异常 | 充值签名回调持久化独立核验收据；应付快照和手工补单不能充当实收证据。充值异常回执、会员额外实收与关联申请暂停同事务；保留claim、暂停前阶段和已交付版本。会员额外回执须有既有财务处理记录才能恢复，不累加额外款项开票。 |
| 通知 | 事件/站内未读、现有持久outbox进度邮件、重试/死信状态；邮件不携带税号、票据附件或直链，失败不撤销交付。 |
| 页面入口 | 用户侧栏、钱包充值记录、会员订单；管理员侧栏。复用现有shell、Base UI、Hugeicons、主题和六语言，生成繁中。 |
| 权限 | 主站session身份；全部发票API拒绝PAT，管理员需主站权限；资源按owner/tenant校验，私有文件每次授权。 |

“同意申请”只进入待开票，管理员在平台外开具税务发票后上传正式文件并交付。本期不连接税务自动开票/红冲系统，不以确认收票限制后续更正反馈。

正式票据内容通过校验后、慢速对象上传之前即记录已开具证据。后续即使上传失败，也不能拒绝申请释放付款；通过重新上传或财务暂停处理。已交付申请始终保留付款占用和历史票据。

## 2. 代码落点

| 层 | 文件/目录与责任 |
| --- | --- |
| 数据 | `model/invoice.go`：11张业务表模型；`model/invoice_payment_receipt.go`：经验证充值回调收据；`model/invoice_payment_safety.go`及`model/membership_order.go`：付款异常协同。 |
| 资格/状态 | `service/invoice_core_{types,candidates,profiles,mutations,queries}.go`：DTO、付款资格、资料、状态事务、列表/详情/计数。 |
| 安全/文件/通知 | `service/invoice_{crypto,policy,assets,notification,configuration}.go`；接入现有outbox与leader后台任务，敏感配置可从挂载文件读取。 |
| HTTP | `router/invoice.go`、`controller/invoice.go`、`middleware/invoice_schema.go`及session-only路由策略。 |
| schema | `pkg/kkaimigrate/schema_invoice.go`、catalog/runtime contract、`cmd/kkai-migrate/invoice_migration.go`独立受控应用协议；普通`--target 17`仍禁止写入。 |
| 前端 | `web/default/src/features/invoice/`；`routes/_authenticated/{invoices,invoice-admin}/`；钱包/会员订单快捷入口和sidebar。 |
| 回归 | 上述模块中的`invoice*_test.go`、`invoice-flow.test.tsx`、`lib/format.test.ts`。 |

前端主路径：`/invoices/new`、`/invoices?tab=records`、`/invoices?tab=profiles`、`/invoices/:public_id`、`/invoice-admin`及`/invoice-admin/:public_id`。用户API为`/api/invoices`和`/api/invoice-profiles`，管理API为`/api/admin/invoices`。

事务使用统一顺序锁定规范付款及镜像，再锁申请和相关附件/异议；MySQL重复读下使用锁定查询直接返回的付款和回执快照，避免读取旧的非锁定事务快照。幂等重放先于CAS；同key异payload拒绝；表单未知结果重试保持原key。

## 3. 启用前置条件

### 3.1 schema v17

v17包含不可变DDL、方言定义、校验及迁移回执，运行时不会AutoMigrate。feature运行时保留14–17兼容前缀，bridge上限仍是14。发票schema gate校验v17规范回执与实际表结构，约30秒复核一次；未就绪时policy返回`SCHEMA_NOT_READY`，业务API失败关闭。

**独立v17扩展控制器及应用协议已本地实现，尚未安装或用于生产迁移。** 普通`--target 17`的apply（包括offline）仍明确拒绝；生产写入只允许独立`invoice-migration-v1`协议，不复用或放宽v16工单迁移协议，不能手工建表或伪造回执绕过。

应用侧入口如下；它们是协议说明，不是绕过基础设施控制器的手工操作步骤：

```sh
/kkai-migrate --invoice-migration plan --json
/kkai-migrate --invoice-migration status --json
/kkai-migrate --invoice-migration apply --invoice-controller-proof /run/invoice-migration/proof.json --json
```

apply仅支持PostgreSQL>=12的external-schema镜像。控制器proof必须从固定路径的root-owned 0400文件以独立只读挂载提供，绑定数据库/namespace、v16/v17规范digest、编译计划checksum、已安装infra SHA、不可变镜像、nonce及最长120秒时效；还核对active/rollback/writer的容器及实际应用进程身份、精确14–17兼容契约、租户感知及主库日志。应用在迁移锁和事务内重新检查proof、schema及计划，完成后核查v17回执和物理结构。源v16发现未登记的发票表时拒绝自动收养。

最终11表版本的v17规范digest为`sha256:4a3539505f44bb997cf361e570fd08a0b2b37bbb3a393b793a041278d37d6533`，计划checksum为`dcc9ed9a91853fd05df2bf4571781fad9a85779ac857812a1460754f62ec48ad`。这些值用于本次源码核对；实际发布仍从精确构建镜像获取契约，并按版本对应的迁移runbook安装、验收和执行控制器。本文不证明生产迁移已完成或功能已启用。

基础设施实现位于确认的`kkai-infra` checkout：`scripts/production/newapi_invoice_migration_{contract,controller,cli}.py`，安装后入口为`kkai-newapi-schema-migrate invoice plan|status|apply --confirm-plan-sha256 ...`，版本对应操作说明为`docs/runbooks/29-newapi-invoice-schema.md`。控制器在proof落盘fsync后、启用migrator LOGIN前再次核对live runtime，并在完成后复核；与NewAPI、frontend、platform和rollout变更互斥。源码存在及本地测试通过不表示该版本已安装。

### 3.2 政策与专用密钥

`common.OptionMap["InvoicePolicy"]`非空时优先，否则读取`KKAI_INVOICE_POLICY`。通过现有受控配置渠道录入，不新增财务设置后台。下例仅展示JSON结构，卖方、税目和票种必须由财务确认，不能原样用于真实业务：

```json
{
  "enabled": true,
  "policy_version": "invoice-policy-2026-01",
  "seller_id": "replace-with-approved-seller-id",
  "seller_name": "财务确认的销售方名称",
  "tax_group": "replace-with-approved-tax-group",
  "invoice_types": [
    {
      "id": "electronic_ordinary",
      "label": "电子普通发票",
      "required_fields": ["email"]
    }
  ],
  "currencies": [{"currency": "CNY", "exponent": 2}],
  "sources": ["topup", "membership"],
  "providers": ["epay"],
  "timezone": "Asia/Shanghai"
}
```

首期只接受CNY、exponent=2，政策强制providers仅为`["epay"]`，限主站单一Epay商户；将来新增多商户必须先补齐不可变商户命名空间，配置不能绕过。充值要求Epay签名核验后的`invoice_payment_receipts`记录与订单owner、tenant、渠道、金额及币种一致且状态为verified；`TopUp.PaidMinor/Currency`只是订单预期条款，不能独自证明到账。TopUpID及渠道回执身份摘要各自唯一，充值/会员同一实收回执共享全局claim。历史记录、手工补单及其他浮点渠道在没有可信收据时继续不可开票；迟到的真实签名回执只有精确匹配才可补证。不能仅扩大providers名单放行。会员订单需唯一匹配的成功主收据，其他收据必须resolved且有非空财务处理记录。额外回执或同回执金额/字段变化均保留异常并暂停关联申请。条件字段只支持`tax_id/address/phone/bank_name/bank_account/email`；企业税号和有效邮箱还受基础规则约束。`sla`可选，未配置不显示承诺时限。

专用密钥配置：

```text
KKAI_INVOICE_KEY_ID=invoice-key-1
KKAI_INVOICE_KEYS={"invoice-key-1":"替换为32字节随机密钥的标准Base64"}
KKAI_INVOICE_POLICY=上面的完整JSON
```

密钥不写入源码、文档、日志或前端；由密钥管理/运行环境注入。生产可用`KKAI_INVOICE_KEYS_FILE`指向只读挂载的单行密钥JSON文件，优先于直接环境值；文件不可读、为空、超限或正文含换行时失败关闭，允许一个末尾换行。轮换时保留旧key ID对应密钥，才能读取历史资料；缺失或认证失败时失败关闭。上线前仍需验证备份恢复与旧key可读。关闭新申请时保留历史解密密钥和对象存储访问，避免同时破坏历史查询。

### 3.3 私有对象存储与邮件

```text
KKAI_INVOICE_S3_PRIVATE=true
KKAI_INVOICE_S3_ENDPOINT=私有S3兼容服务地址
KKAI_INVOICE_S3_REGION=auto
KKAI_INVOICE_S3_BUCKET=发票专用私有bucket
KKAI_INVOICE_S3_ACCESS_KEY_ID=环境注入
KKAI_INVOICE_S3_SECRET_ACCESS_KEY=环境注入
```

复用既有S3/R2适配，使用独立invoice前缀和业务记录，不发布公开URL；必须实际核对bucket匿名访问及访问凭据权限，配置标识本身不能证明存储私密性。文件响应含`private,no-store`、nosniff、安全Content-Disposition、Referrer-Policy及sandbox CSP，前端PDF iframe也使用sandbox。

S3凭据也支持`KKAI_INVOICE_S3_ACCESS_KEY_ID_FILE`和`KKAI_INVOICE_S3_SECRET_ACCESS_KEY_FILE`只读挂载文件；存在对应`_FILE`设置时优先读文件，配置错误不会回退到旧环境密钥。

邮件复用现有SMTP和outbox配置。API区分未配置、排队、重试、成功与死信状态；当前详情页对重试和失败显示专用提示，其余状态显示账户内安全访问说明。临时文件约24小时到期，由hourly leader任务清理，已绑定正式/佐证文件不按暂存清理。

## 4. 已执行验证

以下均在本地当前工作目录执行。后端测试使用隔离fixture和可销毁数据库，不使用生产库；未配置DSN的真实数据库子测试会skip，不能把包整体`ok`理解成三数据库验收。

### 后端

```sh
go test ./model ./service ./controller ./router ./middleware ./pkg/kkaimigrate ./cmd/kkai-migrate -run 'TestInvoice|TestMembershipCheckoutClaimAndAdditionalReceiptAudit|TestDescribeContractJSON|TestRequestTicket|TestResellerPrefix' -count=1 -timeout=180s
```

结果：7个包均通过。覆盖付款唯一占用/幂等与拒绝释放、主收据/镜像资格、交付/回复/补发/历史访问、常用抬头和通知隔离、附件类型/权限/清理、session/PAT/tenant、schema gate、v17迁移回执及既有工单/分销前缀兼容。会员新增实收同事务暂停及财务解决后恢复均有确定性回归。

```sh
go test -tags kkai_bridge ./pkg/kkaimigrate ./cmd/kkai-migrate -run 'TestContractForDialect|TestDescribeContractJSON|TestInvoiceOrdinary' -count=1 -timeout=90s
```

结果：2个包均通过；bridge契约没有随feature schema上限扩大。

### 独立迁移协议与真实数据库

```sh
go test ./cmd/kkai-migrate -run 'TestInvoice|TestRequestTicket|TestResellerPrefix|TestTenantOnlineController' -count=1
go test ./cmd/kkai-migrate -run '^TestInvoice' -count=1
go test -tags kkai_bridge ./cmd/kkai-migrate -run '^TestInvoiceOrdinaryTargetCannotApply$' -count=1
go test ./pkg/kkaimigrate -run '^TestInvoiceExpand' -count=1 -v
```

结果：上述定向检查通过；专用proof的协议/路径、数据库及镜像身份、进程证据、时效、契约和冲突参数均有拒绝路径测试，普通target17仍不可写入。最后一条未配置DSN时的MySQL/PostgreSQL子测试skip，不能作为真实数据库证据。

最终11表已在隔离PostgreSQL18.4上执行：

```sh
KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN='postgres://postgres@127.0.0.1:58104/postgres?sslmode=disable' \
go test ./pkg/kkaimigrate -run '^TestInvoicePostgresGuardAndReceiptAreAtomic$' -count=1 -v
```

结果：通过。覆盖preflight后schema漂移、advisory lock内再次授权、proof过期不落DDL/回执、回执故障回滚事务DDL及成功重试不改历史ledger。最终11表的PostgreSQL schema兼容检查也已通过。

PostgreSQL schema兼容子例的真实执行来自下列联合定向命令：

```sh
env KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN='postgres://postgres@127.0.0.1:58104/postgres?sslmode=disable' \
KKAI_IMAGE_ASYNC_MYSQL_TEST_DSN='root@tcp(127.0.0.1:58105)/?parseTime=true&charset=utf8mb4' \
go test ./pkg/kkaimigrate -run '^TestInvoice' -count=1 -timeout=180s
```

该轮SQLite、PostgreSQL schema和guard子例成功；整条命令因下面的MySQL测试容器故障exit 1，不能将该轮包结果记为全部通过。

最终11表MySQL5.7兼容检查首轮出现`unexpected EOF/bad connection`，已核对为共享隔离测试容器OOMKilled（exit137）；恢复原测试容器后，仅复测失败范围：

```sh
env KKAI_IMAGE_ASYNC_MYSQL_TEST_DSN='root@tcp(127.0.0.1:58105)/?parseTime=true&charset=utf8mb4' \
go test ./pkg/kkaimigrate -run '^TestInvoiceExpandDatabaseCompatibility/mysql$' -count=1 -timeout=180s
```

结果：通过（8.951秒）。本次使用最终11表DDL，覆盖真实MySQL5.7的扩展、付款claim唯一约束及规范结构/迁移回执核验。它不替代业务层重复读事务测试或生产迁移验收。

最终11表业务层MySQL5.7重复读回归独立执行：

```sh
env KKAI_TEST_MYSQL57_DSN='root@tcp(127.0.0.1:58105)/kkai_test_invoice_receipts_20261023_0958?parseTime=true&charset=utf8mb4' \
go test ./service -run '^TestInvoiceMySQLCurrentPaymentFactsAfterRepeatableRead$' -count=1 -timeout=90s -v
```

结果：最终锁顺序调整后4个子项全部通过（0.542秒）；先建立旧RR快照，再由另一连接提交充值状态、充值收据异常、跨来源全局claim或会员收据异常，验证普通读仍为旧快照，而付款锁后的当前读必须拒绝开票且金额为0。专属`kkai_test_*`数据库已DROP清理。

基础设施控制器本地验证（工作目录为确认的`kkai-infra`）：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v scripts.tests.test_newapi_invoice_migration scripts.tests.test_newapi_schema_runtime scripts.tests.test_newapi_tenant_online_evidence scripts.tests.test_newapi_installed_cli scripts.tests.test_tenant_business_edge scripts.tests.test_newapi_request_ticket scripts.tests.test_newapi_tenant_online_preflight
bash scripts/tests/test_newapi_tenant_schema_contract.sh
python3 scripts/check_source_size.py
git diff --check
```

结果：59个定向unittest、契约脚本、源码大小检查和diff检查通过；相关Bash语法检查也通过。没有据此宣称生产控制器已经安装。

最终付款、回调及直接依赖补测：

```sh
go test ./model ./service ./controller -run '^TestInvoice|^TestFinalizeTopUp|^TestRechargeEpay|^TestEpayCallbacks|^TestMembershipPaidExceptions' -count=1 -timeout=120s
go test ./model -run '^TestInvoiceTopUp' -count=1 -timeout=90s
go test ./model -run '^TestInvoice|^TestMembershipPaidExceptions' -count=1 -timeout=90s
go test ./controller ./router ./middleware ./cmd/kkai-migrate -run 'TestInvoice|TestEpayCallbacks|TestRequestTicket|TestResellerPrefix|TestTenantOnlineController|TestDescribeContractJSON' -count=1 -timeout=180s
go test ./model -run '^TestMembershipConcurrentCallbackAndFundingSelection$|^TestMembershipCheckoutClaimAndAdditionalReceiptAudit$|^TestMembershipLegacyPendingCheckoutAndLateLegacyPayment$|^TestMembershipGrantDoesNotDismissAnAdditionalPayment$|^TestResellerTopUp|^TestManualTopUpCompletion|^TestManualCompleteTopUp' -count=1 -timeout=120s
go test ./service -run '^TestKKAIOutboxRuntime' -count=1 -timeout=120s
```

以上命令全部通过；覆盖回调验签、精确实收、防跨来源/用户重复付款、付款异常暂停、手工补单后迟到真回执、既有会员回调/租户充值，以及注册新通知topic后的outbox运行集成。它们是定向验证，没有执行全仓测试。

### 前端（工作目录`web/default`）

```sh
bunx --no-install vitest run src/features/invoice
bunx --no-install oxlint -c .oxlintrc.json src/features/invoice src/routes/_authenticated/invoices src/routes/_authenticated/invoice-admin src/hooks/use-sidebar-data.ts src/features/membership/components/orders.tsx src/features/wallet/components/dialogs/billing-history-dialog.tsx
bunx --no-install tsgo -p tsconfig.invoice-check.json
bun run i18n:sync
```

结果：Vitest 2文件16/16通过；范围lint、范围类型检查exit 0。i18n新增189个key，经临时标准补key脚本和sync流程加入六语言并生成繁中，最后sync报告7个locale的missingCount均为0；既有与本次无关的未译条目未改写。

范围TS使用下列临时配置，从`web/default/tsconfig.invoice-check.json`运行后已删除。复现时可临时写入相同内容；`main.tsx/routeTree`提供现有router类型注册，ambient/test-setup提供共享类型上下文；它不是全库`typecheck -b`或构建。

```json
{
  "extends": "./tsconfig.app.json",
  "compilerOptions": {"noEmit": true, "incremental": false},
  "include": [
    "src/test-setup.ts",
    "src/env.d.ts",
    "src/tanstack-table.d.ts",
    "src/main.tsx",
    "src/routeTree.gen.ts",
    "src/features/invoice/**/*",
    "src/routes/_authenticated/invoices/**/*",
    "src/routes/_authenticated/invoice-admin/**/*",
    "src/hooks/use-sidebar-data.ts",
    "src/features/membership/components/orders.tsx",
    "src/features/wallet/components/dialogs/billing-history-dialog.tsx"
  ]
}
```

最终手机底栏裁切修复后，重新执行以下受影响检查：

```sh
bunx --no-install vitest run src/features/invoice/invoice-flow.test.tsx
bunx --no-install oxlint -c .oxlintrc.json src/features/invoice/new.tsx
```

结果：11/11流程测试、局部lint通过。随后仅校正手机标题/步骤字号及原生日期控件深色配色，并做范围lint与浏览器核对：

```sh
bunx --no-install oxlint -c .oxlintrc.json src/features/invoice/workspace.tsx src/features/invoice/admin.tsx
```

结果：exit 0。最终根目录静态检查`git diff --check`、改动Go文件`gofmt -l`以及`python3 /tmp/verify-invoice-implementation.py`均通过；后者检查文档本地链接/JSON/空白、截图尺寸、冻结设计校验值及新增翻译。7个locale均新增189个key，177个直接`t(...)`字面量key与全部新增key均存在，既有翻译没有被改写。临时补key脚本和TS配置均已删除。

最终UI独立审查补齐了管理员列表申请人、两列表实际页大小、图片解码失败后的重试/下载，以及异议分类和提出时间。PDF内置查看器没有可靠的跨浏览器内部渲染失败事件，界面持续提供独立刷新和下载出口，不以iframe加载事件冒充文件解码成功。所有现有拒绝、暂停和回复说明对用户可见；本期没有内部备注输入。

```sh
bunx --no-install vitest run src/features/invoice/invoice-ui-review.test.tsx
bunx --no-install oxlint -c .oxlintrc.json src/features/invoice/components/files.tsx src/features/invoice/components/detail.tsx src/features/invoice/components/records.tsx src/features/invoice/admin.tsx src/features/invoice/invoice-ui-review.test.tsx
```

新增5个行为回归全部通过，范围lint通过。最终集成类型检查采用上面的同内容临时配置运行`bunx --no-install tsgo -p <临时配置>`，exit 0，随后删除配置。未新增locale key，复用已有翻译。下列截图保留原页面验证证据，未重新截图覆盖这些末轮小修。

### 浏览器与独立审查

真实React页面在本地开发服务器核对，`/api/**`由Playwright拦截为虚构登录和业务fixture，未访问生产数据。已完成：

- 充值+订阅混合选择→填写企业抬头→核对→提交→实际详情路由；显示服务端夹具申请号。
- 1440px用户双栏及管理员窄队列/宽详情；管理员容器1168px时左344.32px、右806.68px、gap17px，符合公式。
- 375×812管理队列→独立详情→保留`q/queue/page`返回；没有持续横向溢出。
- 375×812及320px申请页固定底栏；修复前主按钮右边界超出视口，修复后按钮完全在内。320px俄语长文案可换行、两按钮至少44px。
- 1024×900中等宽度深色主题：内容752px时按容器断点堆叠，原生日期控件随主题配色。
- 页面控制台未见error；开发工具浮标仅在截图夹具中隐藏，未加入生产逻辑。刷新未提交草稿触发beforeunload提醒。

后端/权限、财务异常和设计映射经过分工及独立复审，发现的问题已修复；最终复审在已检查范围内没有剩余P1/P2发现。这不代替真实环境验收，也不构成无缺陷承诺。

## 5. 页面证据

以下是正式前端的截图，数据来自测试夹具，**不是实际用户申请或有效税务发票**。它们与冻结原型用途不同：原型用于设计基线，这些图用于检查当前实现。

- [用户桌面：订单选择](./invoice-center-screenshots/user-desktop.png)（1440×960）。
- [管理员桌面：队列与详情](./invoice-center-screenshots/admin-desktop.png)（1440×1000）。
- [用户手机：核对与底栏](./invoice-center-screenshots/user-mobile.png)（375×812）。
- [中等宽度深色页面](./invoice-center-screenshots/user-dark-medium.png)（1024×900）。

## 6. 尚未覆盖及启用门槛

| 项目 | 当前证据与剩余工作 |
| --- | --- |
| 生产v17迁移 | 最终11表schema、独立应用协议及infra控制器已本地实现；尚未安装控制器、生产执行或验收。需按维护中的迁移及发布runbook继续完成。 |
| 真实MySQL业务事务 | 最终11表真实MySQL5.7重复读4个子项通过，覆盖付款状态、充值/会员收据异常与新全局claim的锁后当前读；这是定向回归，非全面并发或容量测试。 |
| 真实三DB schema | 最终11表SQLite、PostgreSQL18.4、MySQL5.7定向检查已通过；MySQL首轮因隔离容器OOM中断，恢复后失败范围复测通过。所有真实数据库均为可销毁fixture，非生产库；仍不代表生产迁移已验收。 |
| S3/SMTP | 当前文件/通知测试用可控存储和发送fixture；真实上传、下载、匿名拒绝、超时、清理、发信、重试/死信需联调。 |
| 外部退款 | 主站没有通用退款写入入口，未登记的外部/手工退款不能被自动检测；启用前确认财务对账及人工暂停流程，新增退款入口须同事务接入。 |
| PDF | 当前为基础结构/已知主动内容校验，配合CSP/sandbox；真实PDF兼容性、浏览器预览及安全隔离需验收，未运行全面恶意文件扫描。 |
| 浏览器端到端 | 夹具验证了实际UI与请求路径，Go测试验证业务事务；尚无浏览器连接真实后端、真实存储完成全生命周期的证据。 |
| 无障碍/多语言 | 翻译完整性与320px俄语底栏已检查；完整屏读、全键盘、AA对比度、全部页面/语言/弹层组合仍未全面验收。 |
| 大历史量 | 候选合并目前读取单用户全部付款历史到内存，保持历史可查且无30天截断；quote限定最多50笔。未做大账户容量测试，规模扩大时应以实际测量决定是否改SQL投影。 |
| 全量回归 | 按仓库成本规则跳过全套Go/前端测试、全库类型检查及生产构建。没有新增依赖或改动lockfile；上线仍执行对应发布runbook的必需门槛。 |

本次记录覆盖可审查的本地业务代码、迁移协议和页面。正式启用必须完成核心数据、财务、隐私与真实环境验收，并满足对应版本的迁移和发布门槛；迁移、配置就绪和部署成功须分别留证，不能互相替代。

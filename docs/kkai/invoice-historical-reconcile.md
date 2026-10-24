# 历史易支付付款补证

`cmd/kkai-invoice-reconcile` 是一次性数据维护工具，不迁移数据库、不启动应用、
不重复充值，也不修改充值倍率。开票金额只计算人民币付款金额，倍率和到账额度均不参与。

先由财务核实原商户账单或认证回调，确认付款、归属、金额、时间、真实易支付
交易号及无退款。工具检查文件摘要和数据库一致性，不代替原回调验签或账单真实性
审核。普通补证路径需要原付款凭据，不得编造支付交易号或付款时间。

准备一个受控的原始证据文件及 JSON 清单。下面是故意不可执行的空模板：

```json
{
  "operator": "",
  "evidence_source": "merchant_statement",
  "evidence_sha256": "",
  "orders": [
    {
      "tenant_id": 0,
      "user_id": 0,
      "order_no": "",
      "amount_minor": 0,
      "currency": "CNY",
      "paid_at": 0,
      "provider_receipt_id": ""
    }
  ]
}
```

普通补证的 `evidence_source` 为 `merchant_statement` 或 `signed_callback`；
`evidence_sha256` 为原文件的 64 位小写 SHA256；`paid_at` 为真实付款 Unix 秒。
原始证据、审核后清单和运行结果必须一起留存，不要提交到代码库。

先在 Mac 构建一次维护工具：

```sh
go build -o /tmp/kkai-invoice-reconcile ./cmd/kkai-invoice-reconcile
```

通过受控数据库访问路径，将单行 DSN 从标准输入提供给已构建的程序；不要将生产
DSN 写进命令行、清单或输出文件。使用 `--dsn-stdin` 时须清除冲突的
`KKAI_INVOICE_RECONCILE_DSN` 和 `SQL_DSN` 环境变量。

```sh
/tmp/kkai-invoice-reconcile --dsn-stdin --manifest reviewed.json --evidence original.csv
/tmp/kkai-invoice-reconcile --dsn-stdin --manifest reviewed.json --evidence original.csv --apply
```

默认只预览。整份清单的订单归属、状态、交易号唯一性、精确金额和时间先验证，
输入不合法时零写入；仅平台租户成功的 Epay 普通充值可补证。应用后逐单提交，
遇到冲突即停止，输出 `not_applied` 的订单尚未处理，不能宣称整批成功。

每笔补证复用 `model.ReconcileInvoiceTopUpPayment`，并在已安装的
`wallet_payment_events` 记录 `invoice_reconcile`、证据来源、原文件摘要和操作人。
同一证据及结果重复运行不重复创建审计。补证与审计分别提交，避免审计失败回滚
已经保存的财务冻结；若提示补证已提交但审计失败，检查状态后重跑同一清单补审计。
已有异常不会被清除，也不会释放已占用的发票申请。

站点所有者已明确认可全部用户现存已支付成功的历史易支付充值，无须补原网关流水。
这类维护使用独立的 `owner_approved_legacy` 来源，`--evidence` 文件保存本轮授权原文、
范围截止时间和只读数据库快照。它证明财务认可，不能标注为认证网关流水。
清单逐笔列出原归属用户和本站订单号，不接受通配符或自动扩大范围。

人工认可时 `provider_receipt_id` 必须为空；`paid_at` 填 0，工具仅采用已有完成时间，
否则采用原创建时间并标记 `estimated_paid_at` 和
`HISTORICAL_PAYMENT_TIME_ESTIMATED`。金额必须等于存量 `Money` 用 decimal
四舍五入到人民币两位小数后的整数分；记录过小数规范化的订单会在审计引用中保留
`amount_rounded:true`。已有非零 `paid_minor` 不一致时停止，不覆盖原精确金额。

```sh
/tmp/kkai-invoice-reconcile --dsn-stdin --manifest approved-batch.json --evidence owner-authorization.json
/tmp/kkai-invoice-reconcile --dsn-stdin --manifest approved-batch.json --evidence owner-authorization.json --approve-legacy --apply
```

每批最多 100 笔，可包含多个用户；`--owner-user-id 140` 可额外限制为一个用户。
仅接受平台租户、支付成功、Epay、`wallet_lifecycle_version=0` 的历史普通充值。
未支付、失败、其他渠道、新钱包流程、订阅镜像、异常收据或已有发票占用均不会放行。
已有匹配的真实核验收据保持原样；新认可身份使用独立的
`invoice:owner-approved-legacy:v1:` 哈希命名空间，不生成易支付交易号。
收据、缺失字段和 `wallet_payment_events` 人工认可审计在同一事务提交，任何一笔
检查失败均整批零写入。同一清单重跑不重复充值、不重复创建收据或认可审计。

可使用 `--check-selectable` 单独执行只读候选和报价验收；此开关与 `--apply` 互斥。
它沿用当前应用的发票政策来源（数据库覆盖或线上环境配置），并使用实际的发票候选与报价逻辑。

工具成功表示付款核验或财务认可记录已补齐。最终验收仍须查询目标用户的发票候选接口，逐单确认
`eligible=true`、`allowed_actions` 包含 `select`、实付金额正确，并进行合计报价。
不开具发票、不代替客户提交申请。此维护路径不需要重新发布应用或改数据库 schema。

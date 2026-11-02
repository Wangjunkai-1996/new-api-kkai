# 会员支付可靠性 schema 20

本文件说明应用侧增量结构和迁移协议。新增代码不代表生产控制器已经安装，
也不授权执行数据库迁移、发布、历史补查或真实支付验收。
生产准备仍从 `/Users/tokk/Documents/Codex/runbooks/newapi-upgrade-and-deployment.md` 进入。

## 数据范围

仅从完整、规范的 schema 19 扩展至 20；重复执行已完成的规范 schema 20 返回原回执。
创建两张独立表和六个索引，不修改原会员订单、会员账户、旧收款回执或发票关联，
不根据历史订单状态推断付款、凭据或应发权益，不自动填充历史行。

| 表 | 作用 | 数据库约束 |
| --- | --- | --- |
| `kkai_membership_payment_checks` | 原订单商户加密快照、检查状态、领取令牌、租约、退避及通知到达时间 | `order_id` 主键；`trade_no` 唯一；用户索引；`(status,next_check_at,claim_until)` 排程索引 |
| `kkai_membership_payment_evidence` | 认证查单取得的付款事实、支付方时间、核验时间、发放重试和阻断原因 | `id` 自增主键；`receipt_key` 唯一；订单索引；`(status,retry_at)` 重试索引 |

所有非主键列均为 `NOT NULL`，没有数据库默认值。金额和时间使用 64 位整数，
商户及流水引用沿用明确长度的字符串；密文使用 `TEXT`。回调线索不占用证据表的流水唯一键。
既有会员回执仍由成功发放事务维护，新证据表不会把尚未发放的付款伪装成旧回执。
历史凭据不能证明属于原订单时，必须进入复核，不能借迁移把当前配置填成历史事实。

DDL 为 SQLite、MySQL 5.7.8+ 和 PostgreSQL 9.6+ 分别定义。
生产控制器协议仍仅接受 PostgreSQL >=12、external schema 模式。
MySQL 非事务性 DDL 的隔离恢复能力不构成生产绕过控制器的授权。

## 运行时契约

默认 feature runtime 的声明由 `14..19` 扩展为 `14..20`，启动迁移目标仍为 14。
bridge 的 `11/14` 契约不变。schema 20 及其摘要须包含在准确构建镜像的
`/kkai-migrate --describe-contract --dialect postgres --json` 输出内。

`CheckMembershipPaymentRuntimeSchema` 同时检查规范版本回执、字段类型/长度/可空性/
默认值、主键，以及全部六个索引的列顺序和唯一性。缺列、丢失唯一性、缺少排程索引、
只有物理表却没有回执、回执摘要错误，均不允许启用新支付流程。
该检查只读取迁移回执和这两张表的元数据，不执行历史业务数据全表审核。
完整迁移维护审核使用 `CheckMembershipPaymentSchema`。

新增表不会进入旧的 `MembershipModels()` 自动建表清单。
较早功能可接受规范的 v20 回执，钱包和商城请求的精确运行时检查仍只审核自己的物理结构。
旧 v15–v19 控制器协议的授权版本保持冻结，不能使用旧协议凭证执行 v20。

## 显式 CLI

以下只读命令使用 stdin 接收 DSN，避免把密码放入进程参数或终端历史：

```sh
/kkai-migrate --dsn-stdin --precheck --target 20 --json
/kkai-migrate --dsn-stdin --target 20 --dry-run
/kkai-migrate --dsn-stdin --membership-payment-migration plan --json
/kkai-migrate --dsn-stdin --membership-payment-migration status --json
```

普通 `--target 20` 不接受写入，即使内部调用提供 `Offline` 也不能绕过 CLI 控制。
只有独立安装并验收的 root 控制器可以执行以下入口：

```sh
/kkai-migrate --dsn-stdin --membership-payment-migration apply \
  --membership-payment-controller-proof /run/membership-payment-migration/proof.json --json
```

凭证必须来自固定的 root 所有、只读挂载，协议为 `membership-payment-migration-v1`，
schema 为 `kkai_membership_payment_migration_proof_v1`，操作为 `apply`。
应用侧校验原库身份、schema 19/20 摘要、编译迁移摘要、不可变镜像、基础设施版本、
最长 120 秒的有效窗口，以及 active、rollback、writer 三个真实进程的独立身份。
三个运行时均须声明准确的 external `14..20` 契约；写入锁取得后再次核验。
控制器必须独立证明这些事实，手工拼装 JSON 不构成迁移许可。

本地基础设施控制器提供 `kkai-newapi-schema-migrate membership-payment plan|status|apply`，
`apply` 还必须携带只读 `plan` 给出的 `--confirm-plan-sha256`。控制器源码完成不代表
已经安装到生产；安装与迁移须分别按生产 runbook 验证和授权。

未记录回执的部分物理扩展会被生产 CLI 拒绝，交由版本专属恢复流程处理。
应用不会在启动、请求或后台任务中调用 `ApplyMembershipPaymentExpand`。

## 交付和回滚边界

上线顺序为：完成隔离验收、准备可读 19/20 的兼容 active/rollback/writer、
单独授权并安装 v20 控制器、验证原库和精确镜像契约、执行迁移、重新检查规范回执及业务功能。
本次应用变更不安装基础设施控制器，也不执行上述生产步骤。

过渡窗口须按支付维护处理：v20 程序在 schema 19 或两个开关未启用时不提供新的易支付会员销售；
旧易支付会员回调也不会退回只凭通知发放。schema 20 就绪后先启动认证查单，核实在途订单及
无法恢复原商户凭据的待审清单，再启用自动发放。不能把迁移前收到 `fail` 的通知当作未付款，
也不能依赖上游一定重试。其他会员支付渠道按原契约验收。

schema 20 后，最大版本 19 的旧镜像不能作为回滚目标。回滚需使用已验证可读 v20 的
兼容镜像，保留两张表中的付款事实、查单快照和待处理记录，不执行删表降级。
业务开关、原商户快照校验、人工复核及放量策略仍按
[会员支付可靠性方案](membership-payment-reliability-plan-20261102.md) 验收。

## 定向验收

`pkg/kkaimigrate` 的会员支付测试覆盖：附加 DDL、旧回执及原订单数据保留、
无授权拒绝、预演不写入、规范回执门禁、丢列/索引/唯一性拒绝、跨单流水争用和 DDL 幂等。
`cmd/kkai-migrate` 测试覆盖：v20 只读 target、独立固定凭证、过期或身份漂移拒绝、
不兼容回滚/后台进程拒绝，以及旧迁移协议的版本边界。

真实数据库 DDL 验收复用现有仅本机、专用测试库的
`KKAI_SHOP_MYSQL_TEST_DSN` / `KKAI_SHOP_POSTGRES_TEST_DSN` 夹具，
每次创建并清理独立命名空间；未配置时明确跳过，不能以 SQLite 结果代替数据库验收。
具体已执行命令和结果以实施记录为准；本文件不宣称生产迁移或全量测试已经通过。

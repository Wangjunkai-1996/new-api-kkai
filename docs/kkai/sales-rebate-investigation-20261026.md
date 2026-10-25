# 销售返利故障排查与本地修复

排查日期：2026-10-26。范围：截图中的销售受邀客户、销售返利审批，以及相邻的充值事件、销售业绩和发放链路。排查阶段仅只读检查生产并修改本地代码，没有构建、部署或修改生产业务数据；后续按用户授权将两仓库的修复提交、推送至各自的 `production/kkrich` 分支。代码同步不表示生产已上线。

## 已确认的根因

Invitations 服务 `src/sales_customers.rs` 的 `qualified` CTE 使用 `SELECT t.*`，后续 `credited` CTE 又增加一个名为 `credited_quota` 的派生列。NewAPI 钱包生命周期提交 `2677f8281` 新增物理列 `top_ups.credited_quota` 后，两列同名，后续聚合触发 PostgreSQL `42702`：

```text
column reference "credited_quota" is ambiguous
```

生产返利服务最近 24 小时、最多 12000 条容器日志中，该数据库错误签名出现 85 次；日志只输出脱敏后的错误签名及计数。对生产数据库执行 `BEGIN READ ONLY`、零行 CTE 查询，也复现了相同错误，没有读取或修改客户明细。

影响范围：销售受邀客户列表、客户充值明细、管理员销售账号业绩汇总。独立 Invitations 服务故障；NewAPI 主服务健康不代表这些接口正常。

本地修复在已确认的 Invitations checkout：

```text
/Users/tokk/Documents/Codex/2026-08-05/li/work/invitations-current-src
```

该 checkout 的 HEAD 与本次线上来源均为 `b183b26b8a24a6c0ef01d9e223babce5960fb64d`。仅修改 `src/sales_customers.rs`：显式选择查询实际使用的列，避免随 NewAPI 增加字段而污染 CTE 输出；金额仍以不可变充值事件快照为准。测试表补入原生 `credited_quota=9000000`，故意与事件额度不同，验证不会误用该列。

## 销售审批截图的解释及前端修复

销售审批列表不使用上述 CTE。本次只读查询所得申请数量：销售申请为 0；普通申请为待审批 12、已完成 569。因此截图中的销售“暂无返利申请”与当前数据一致，不能把相邻 toast 单独归因于审批列表失败；截图没有提供对应请求的网络记录。

另确认前端缺陷：React Query 后台刷新失败会保留缓存数据，审批页原来可能同时展示错误和旧空态/旧表格，并保留之前打开的操作。已在本地修改 `rebate-approvals.tsx`，只在查询成功时显示数据及操作，失败清理选择与操作并阻止提交；增加缓存为空、缓存有记录两种刷新失败与恢复回归。

## 资金和事件检查

通过 `sys1` 只读访问，主机为 `ovh-sys1-prod`。本次状态快照：

- `make newapi-status`：`HEALTHY`，版本 `kkai-prod-20261025.1792900666-077fa81b5`，snapshot stable，rollback ready。
- Invitations 容器运行，版本 `kkai-prod-20261021.1792604379-b183b26b8`。
- `make invitations-status`：`ATTENTION`，结算 enabled；pending=0、dead=0；对账周期 300 秒、lag 280 秒；因 2 个 open cases 返回非零。

对最近 48 小时成功的主站充值执行只读聚合，排除订阅镜像、余额/兑换来源及分销商订单：

| 核对项 | 结果 |
| --- | --- |
| 成功充值 | 169 |
| 缺少 outbox / pending / dead | 0 / 0 / 0 |
| delivered 事件 | 169 |
| 新生命周期订单 | 163 |
| 冻结到账额度与事件额度不一致 | 0 |
| 新流程支付凭据 | 163，全部 credited |
| Invitations 回执 | 169：32 created、137 ignored_no_inviter |
| 缺失回执 / created 回执缺少返利记录 | 0 / 0 |

静态检查确认：新流程仍在同一事务内提交充值成功、余额入账和返利事件，金额使用冻结的 `CreditedQuota`；销售申请保留人工审批、行锁、金额边界和防重复发放保护。本次样本没有发现漏事件或额度变化，不代表对全部历史资金进行完整审计。

## 历史对账告警

2 条 open cases 为 `2025/2026`，均于 `2026-10-20T16:13:10Z` 创建，对应订单 `11371`：`topup_outbox_missing` 和 `topup_event_receipt_missing`。该订单 `tenant_id=1`，属于分销商订单。

NewAPI 对分销商充值使用独立 payment lot，按设计不发送主站邀请返利事件；Invitations `src/routes/system.rs::sync_topup_orders` 却扫描全部成功充值，未排除分销商订单，因此误报。本次没有关闭告警、补发返利或修改对账范围。后续修复应统一主站订单范围，并保留旧 NewAPI schema 兼容性；历史告警的处理应有独立审计记录。

## 定向验证与边界

后端使用本任务独立的本机 PostgreSQL 18 容器 `kkai-sales-rebate-test-20261026`，没有使用生产连接。先仅补真实字段，保留旧查询：客户统计和账号统计两项稳定失败，错误为 `42702`；再应用显式列修复，以下命令 3 项全部通过：

```sh
# 在 Invitations checkout 执行；端口是本次临时测试实例的端口。
TEST_DATABASE_URL=postgresql://postgres@127.0.0.1:65475/postgres \
  cargo test --offline --locked sales_customers::tests -- --include-ignored --nocapture
rustfmt --edition 2021 --check src/sales_customers.rs
git diff --check
```

格式和 diff 检查通过；临时 PostgreSQL 容器已停止并删除。

前端在 `web/default` 执行：

```sh
bun run test src/features/invitations/__tests__/sales-approvals.test.tsx
bun x --no-install oxlint -c .oxlintrc.json src/features/invitations/components/admin/rebate-approvals.tsx src/features/invitations/__tests__/sales-approvals.test.tsx
git diff --check
```

结果分别为 5/5 通过、lint 通过、diff 检查通过。新增的两种刷新失败回归先复现失败，再验证修复后恢复、隐藏旧列表和关闭旧发放弹窗，且没有发出 POST。

两文件格式检查保留版权头，实际命令：

```sh
python3 - <<'PY'
from pathlib import Path
import re
import subprocess
for name in [
    'src/features/invitations/components/admin/rebate-approvals.tsx',
    'src/features/invitations/__tests__/sales-approvals.test.tsx',
]:
    source = Path(name).read_text()
    match = re.match(r'^/\*\nCopyright \(C\)[\s\S]*?QuantumNous[\s\S]*?\*/\n+', source)
    assert match, name
    result = subprocess.run(
        ['./node_modules/.bin/oxfmt', '-c', '.oxfmtrc.json', '--stdin-filepath', name],
        input=source[match.end():], text=True, capture_output=True, check=True,
    )
    assert source == match[0] + result.stdout, name
    print('format check passed:', name)
PY
```

退出 0，两文件通过。

范围类型检查使用临时配置，扩展 `tsconfig.app.json`，仅包含两个变更文件、`src/test-setup.ts` 和 `src/env.d.ts`，禁用增量输出；没有引入 `main.tsx` 的全局 Router 注册：

```sh
bun x --no-install tsgo --project /tmp/newapi-sales-approvals-typecheck-20261026.json --noEmit
```

退出 1，唯一诊断在传递依赖，而非本次变更文件：

```text
src/components/page-transition.tsx(78,7): error TS2322: Type 'RouterState<any, RouteMatch<any, any, any, any, any, any, any>>' is not assignable to type 'Key | null | undefined'.
```

因此不将类型检查记作通过，也不据此判断完整应用是否存在该错误；保留这一验证缺口，没有扩大为全仓检查或修改无关组件。

未运行全仓测试、全仓类型检查、生产构建或部署。未做真实登录后的线上页面验收、申请/审批/发放写操作；因此本地修复通过不等于线上故障已经恢复。Invitations 服务和 NewAPI 前端为两个独立发布产物，发布本次后端修复需要更新 Invitations。

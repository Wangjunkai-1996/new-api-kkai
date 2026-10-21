# 销售账号业绩汇总

## 页面与统计口径

管理员「销售账号」页同时展示全量汇总和逐人业绩：

- 邀请注册人数：按邀请关系累计统计，包含历史记录。
- 有效客户数：至少存在一笔成功的付费钱包充值；之后消费余额不减少此人数。
- 累计确认到账充值额度：仅累计不可变到账快照确认的额度，使用服务端返回的当前币制。不是钱包余额、原始支付金额或销售返利。
- 返利分类：冷却中、可申请、待审核、已审核待发放、已确认发放、锁定、已关闭，另提示待计算笔数。
- 历史充值缺少到账证据时显示待核对笔数，不推算或补造金额。

汇总覆盖所有已登记的销售账号，包含停用账号、成为销售前的邀请与充值，不随当前页变化。返利展示复用销售个人客户页的组件和金额格式化。金额以服务端整数分值为准；全量金额是逐销售展示金额之和。

账号列表保持每页 20 条的服务端分页，显示账号总数、页码和上一页/下一页。账号编辑、启停和备注继续使用原接口；新增手动刷新。窄屏可横向滚动查看完整表格。

## 接口与兼容

`GET /invitations/api/admin/sales-accounts?page=1&pageSize=20` 由独立 Invitations 服务提供，保留 `items/total/page/pageSize`，新增：

- 每行 `performance`：`invitedCount/effectiveInvitedCount/rechargeAmount/unverifiedRechargeCount/rebates`。
- 全量 `summary`：与 `performance` 结构一致，不能由当前页在浏览器中相加生成。
- `creditCurrency`：`legacy_075` 或 `usd_credit_v1`。

前端仅发送一次分页列表请求，不逐销售请求客户数据。后端按全部销售批量聚合，邀请人数、充值和返利分别计算，避免多表连接放大金额。

前后端可以分开发布。建议先更新 Invitations 服务，再更新前端；旧服务缺少新增字段时，账号维护仍可用，业绩位置显示「—」及升级提示，不能显示虚假的零。此功能没有新增数据库迁移或依赖。

## 验证记录

前端定向验证（在 `web/default`）：

```sh
bun run test src/features/invitations/__tests__/sales-accounts.test.tsx
bun run test src/features/invitations/__tests__/sales-customers.test.tsx
bun x --no-install oxlint -c .oxlintrc.json \
  src/features/invitations/api/sales.ts \
  src/features/invitations/types/sales.ts \
  src/features/invitations/components/admin/sales-accounts.tsx \
  src/features/invitations/components/admin/sales-performance-summary.tsx \
  src/features/invitations/components/sales-rebate-amounts.tsx \
  src/features/invitations/components/user/sales-customers.tsx \
  src/features/invitations/__tests__/sales-accounts.test.tsx
bun run i18n:sync
```

6 项账号页测试和 2 项客户页测试通过。覆盖 41 个账号的三页切换、全量汇总、停用账号历史、真实零与缺失数据、两种币制、刷新、编辑及错误重试。新增 6 个文案在 7 个现有语言文件中齐全，插值变量一致；定向格式化保留版权头。

范围限定的 TypeScript 检查没有报告本次修改文件错误，但被未修改的 `src/components/page-transition.tsx:78` 路由类型诊断阻挡；仅检查该依赖也复现同一诊断。此记录不等于全前端类型检查通过。

Invitations 后端的数据库回归见该仓库 `docs/sales-invitations-validation.md`。本次没有运行全仓测试、生产构建或生产部署，也未验证生产数据规模下的查询耗时。

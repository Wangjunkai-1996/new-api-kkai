# 订阅会员前端验证记录

## 范围与售卖状态

验证工作区：`/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai`。没有修改原始 checkout，没有构建生产镜像、连接生产或部署。

覆盖黄金 / 钻石 / 王者用户页、移动端、升级确认、订单与异常收款处理、管理配置、模型授权上限、待核对调用处理、钱包入口、充值优惠停用提示和模型广场双价格展示。会员倍率来自服务端报价；原有 `group_ratio` 保持钱包价格语义。

正式目录参数已确定为 CNY ¥600/¥2000/¥5000，额度显示值为 600/2000/5000，逐组折扣由后台配置；
初始配置不会默认开售，正价格和正额度仍是开售校验条件。截图中的 99/199/399 元、额度和折扣
只是本地浏览器拦截 API 返回的视觉测试 fixture，未写入业务配置或数据库。

## 易支付专项（2026-10-18）

修复异常款处理的 `trade_no` 请求契约，以及会员表单异步打开新窗口可能被拦的问题。
会员现在通过当前页 POST 支付，共享钱包 helper 原默认导航方式保留。新增测试实际
检查表单字段/提交目标、防重复下单、原价续付、返回参数不授予权益、异常款接口 URL
与请求体，不用 mock 会员 API 掩盖其传输契约。

以下命令在 `web/default` 执行：

```sh
bun x vitest run src/features/membership/membership.test.tsx src/features/membership/api.test.ts
bun x vitest run src/features/membership/membership.test.tsx
node_modules/.bin/tsgo --noEmit -p /tmp/kkai-membership-epay-tsconfig.json
bun x oxlint -c .oxlintrc.json src/features/membership/api.ts src/features/membership/api.test.ts src/features/membership/lib.ts src/features/membership/components/resolve-order-dialog.tsx src/features/membership/membership.test.tsx src/features/wallet/lib/payment.ts
bun x oxfmt --check src/features/membership/api.ts src/features/membership/api.test.ts src/features/membership/lib.ts src/features/membership/components/resolve-order-dialog.tsx src/features/membership/membership.test.tsx src/features/wallet/lib/payment.ts
```

首次组合中 API 2 项通过，会员 20/21；仅测试的 Chrome UA 写法不真实，修正后只重跑
会员范围，21/21 通过。类型子集包含本轮 6 文件、测试及直接依赖，检查通过。
lint/格式通过；lint 只有钱包既有 `prefer-includes` warning。未运行全仓检查或构建。

## 自动化验证（前阶段）

除 Go 命令外，下列命令在 `/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default` 执行。

| 检查 | 结果 |
| --- | --- |
| 会员交互测试 | 16 项通过 |
| 定价相关最近一次回归 | 3 文件、46 项通过 |
| 先前会员 + 侧边栏 + 定价联合回归 | 5 文件、71 项通过；后续仅重跑变动范围 |
| 本次修改 33 个 TS / TSX 文件 lint | 通过 |
| 33 文件格式检查 | 0 差异，保护版权头，未重写源文件 |
| 会员目录及 4 个变更定价文件的 TypeScript 检查 | 通过，包含传递依赖 |
| i18n 同步 | 通过；7 语言各增加 147 个键 |
| 修改范围 diff whitespace 检查 | 通过 |

精确测试命令：

```sh
bun x vitest run src/features/membership/membership.test.tsx
bun x vitest run src/features/pricing/__tests__/use-pricing-data.test.ts src/features/pricing/__tests__/model-cards.test.tsx src/features/pricing/__tests__/task-price-display.test.tsx
```

先前联合回归命令（没有在输入不变时重复运行）：

```sh
bun x vitest run src/features/membership/membership.test.tsx src/hooks/__tests__/sidebar-config.test.tsx src/features/pricing/__tests__/use-pricing-data.test.ts src/features/pricing/__tests__/model-cards.test.tsx src/features/pricing/__tests__/task-price-display.test.tsx
```

精确 scoped lint 命令：

```sh
node_modules/.bin/oxlint -c .oxlintrc.json \
  src/features/membership/form.ts \
  src/features/membership/api.ts \
  src/features/membership/types.ts \
  src/features/membership/lib.ts \
  src/features/membership/hooks.ts \
  src/features/membership/index.tsx \
  src/features/membership/membership.test.tsx \
  src/features/membership/admin.tsx \
  src/features/membership/components/catalog-editor.tsx \
  src/features/membership/components/resolve-order-dialog.tsx \
  src/features/membership/components/tier-card.tsx \
  src/features/membership/components/wallet-summary.tsx \
  src/features/membership/components/discount-comparison.tsx \
  src/features/membership/components/current-membership.tsx \
  src/features/membership/components/orders.tsx \
  src/features/membership/components/purchase-dialog.tsx \
  src/features/membership/components/discount-editor.tsx \
  src/features/wallet/index.tsx \
  src/features/subscriptions/index.tsx \
  src/hooks/use-sidebar-data.ts \
  src/hooks/use-sidebar-config.ts \
  src/hooks/__tests__/sidebar-config.test.tsx \
  src/features/pricing/hooks/use-pricing-data.ts \
  src/features/pricing/types.ts \
  src/features/pricing/components/model-price-cell.tsx \
  src/features/pricing/components/cached-price-cell.tsx \
  src/features/system-settings/integrations/payment-settings-section.tsx \
  src/features/system-settings/models/ratio-settings-card.tsx \
  src/features/system-settings/models/group-ratio-form.tsx \
  src/features/system-settings/models/group-ratio-visual-editor.tsx \
  src/routes/_authenticated/membership/index.tsx \
  src/features/membership/components/model-limit-editor.tsx \
  src/features/membership/components/billing-reviews.tsx
```

类型检查命令：

```sh
node_modules/.bin/tsgo --noEmit -p /tmp/kkai-membership-tsconfig.json
```

该临时配置内容如下；相对路径应按实际 checkout 调整：

```json
{
  "extends": "/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default/tsconfig.app.json",
  "compilerOptions": {
    "tsBuildInfoFile": "/tmp/kkai-membership.tsbuildinfo",
    "typeRoots": [
      "/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default/node_modules/@types"
    ]
  },
  "include": [
    "/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default/src/features/membership",
    "/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default/src/features/pricing/components/model-price-cell.tsx",
    "/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default/src/features/pricing/components/cached-price-cell.tsx",
    "/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default/src/features/pricing/hooks/use-pricing-data.ts",
    "/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default/src/features/pricing/types.ts",
    "/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default/src/**/*.d.ts"
  ],
  "exclude": [
    "/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default/src/**/*.test.ts",
    "/Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default/src/**/*.test.tsx"
  ]
}
```

格式检查命令为 `python3 /tmp/kkai-membership-format-check.py`。此脚本只读取上述 33 个文件，移除内存中的版权头后以 stdin 调用 `node_modules/.bin/oxfmt -c .oxfmtrc.json --stdin-filepath <文件>`，比较输出与原 body，不修改源文件。文件清单保存在 `/tmp/kkai-membership-files.json`。

翻译同步命令为 `bun run i18n:sync`；翻译按临时 `scripts/add-missing-keys.mjs` 写入后同步。临时脚本已从源码工作区清理。

```sh
git diff --check -- web/default controller/pricing.go service/membership_budget_test.go relay/common/membership_override_test.go docs/kkai/subscription-membership-frontend-validation.md
```

独立账务回归由本前端评审补充了 CacheCreation5m 最大费用、隐藏服务端上下文、冻结价格与 capped settlement、整笔回退钱包、出站参数限制和虚拟 / 已删除分组报价测试。最近一次预算验证（在工作区根目录）：

```sh
KKAI_MEMBERSHIP_ENABLED=true go test ./service -run '^TestMembershipBudget' -count=1 -timeout=90s
```

结果通过。后台仍有其他协作者继续修改，最终后台定向验证以主任务最终记录为准。

## 类型检查范围说明

扩展到钱包 / 订阅管理等集成入口的 scoped TypeScript 检查出现：

```text
src/components/page-transition.tsx(78,7): TS2322
RouterState<any, RouteMatch<any, any, any, any, any, any, any>> is not assignable to Key | null | undefined.
```

同样的 scoped 配置在未改动的原 checkout 重现同一诊断。原因是这个子集没有应用入口的 TanStack Router Register 路由上下文；没有改动无关的 PageTransition。会员目录及精确变更定价文件的检查已通过，完整集成 TypeScript 检查仍未证明通过。

## 浏览器视觉验证

开发服务器命令：

```sh
KKAI_EXTERNAL_FRONTEND_BUILD=1 VITE_REACT_APP_SERVER_URL=http://127.0.0.1:9 bun run dev --port 4178 --host 127.0.0.1
```

使用单独无头 Chrome 和本地 API fixture，不依赖真实支付或生产后端。执行命令：

```sh
node /tmp/kkai-membership-visual.cjs
node /tmp/kkai-membership-dark-visual.cjs
```

已查看桌面 1440 × 1100、手机 390 × 844、手机下方套餐、手机升级弹窗和管理页。浅色与深色均无横向溢出，无 pageerror。修复了项目语言代码 `zhCN` / `zhTW` 直接用于 Intl 导致的 RangeError，统一调用现有 `toIntlLocale`。

| 截图 | 路径 |
| --- | --- |
| 浅色桌面 | `/tmp/kkai-membership-desktop.png` |
| 浅色手机首屏 | `/tmp/kkai-membership-mobile.png` |
| 浅色手机套餐 | `/tmp/kkai-membership-mobile-tiers.png` |
| 浅色升级弹窗 | `/tmp/kkai-membership-purchase.png` |
| 浅色管理页 | `/tmp/kkai-membership-admin.png` |
| 深色桌面 | `/tmp/kkai-membership-dark-desktop.png` |
| 深色手机首屏 | `/tmp/kkai-membership-dark-mobile.png` |
| 深色手机套餐 | `/tmp/kkai-membership-dark-mobile-tiers.png` |
| 深色升级弹窗 | `/tmp/kkai-membership-dark-purchase.png` |
| 深色管理页 | `/tmp/kkai-membership-dark-admin.png` |

开发服务器已停止。源码目录中的验证清单、临时 tsconfig 和翻译脚本均已清理；可重用的验证脚本与截图保留在 `/tmp`。

## 未覆盖范围

没有运行全仓测试、全量 lint / TypeScript 或生产构建。没有执行真实支付商回调、真实后台的端到端付款或上线验收。浏览器验证覆盖界面和客户端分支，不替代支付、并发扣费、迁移与上线验证。

# KK 商城前端开发规格

版本：D2；日期：2026-10-28；状态：**买家端与运营后台应用代码已落地，含浅深色主题、原创插画和多语言**。定向检查与真实浏览器验收缺口见[实施状态](./kk-shop-implementation-status.md)。

本文件将 [PRD](./kk-shop-prd.md)、[业务规则](./kk-shop-business-rules.md) 与 [V2 UI 规格](./kk-shop-ui-spec.md) 转为 `web/default` 的开发任务。页面效果以 [V2 原型](./kk-shop-design-preview.html) 和 [视觉复审](./kk-shop-visual-review.md) 为依据；请求、字段、状态枚举、错误码、权限 scope 和幂等语义以 [API 总契约](./kk-shop-api-contract.md) 为唯一来源。本文中的文件树及组件名称为计划，不表示已经存在。

首版同时完成自营账号/卡密自动交付、人工交付、订单与售后、人工原路退款登记，以及运营异常闭环。商城收款不能进入 API 额度、钱包充值或返佣流程。首发真实商品、币种、渠道及服务条款尚待确定；演示数据只能用于隔离测试与设计预览，不能作为可付款的生产种子数据。

## 1. 工程基线与实际接点

已检查现有源码。采用现有 React 19、TypeScript、Rsbuild、TanStack Router/Query、Base UI、Tailwind 4、React Hook Form、Zod、Hugeicons 和 Sonner，不引入另一套商城前端框架。

| 领域 | 当前源码 | 商城如何接入 |
| --- | --- | --- |
| 依赖、脚本 | [package.json](../../web/default/package.json)、[components.json](../../web/default/components.json) | `base-nova`、`rsc:false`、Hugeicons；无需 Next.js/RSC、Radix 或另一套状态管理依赖 |
| 根挂载、Query 和路由 | [main.tsx](../../web/default/src/main.tsx)、[根路由](../../web/default/src/routes/__root.tsx) | 复用唯一 QueryClient、认证 bootstrap、站点维护检查、主题与 Toast；不在商城另建根 Provider |
| 代码拆分、静态资源前缀 | [rsbuild.config.ts](../../web/default/rsbuild.config.ts) | 当前生产启用 TanStack `autoCodeSplitting`；资源遵守 `KKAI_FRONTEND_ASSET_PREFIX`，不硬编码站点根路径 |
| 公共页面框架 | [PublicLayout](../../web/default/src/components/layout/components/public-layout.tsx)、[PublicHeader](../../web/default/src/components/layout/components/public-header.tsx) | 买家使用公共布局；保留品牌、账号、语言与主题，页头增加商城及本人订单入口 |
| 主导航来源 | [use-top-nav-links](../../web/default/src/hooks/use-top-nav-links.ts)、[PublicNavigation](../../web/default/src/components/layout/components/public-navigation.tsx)、[默认配置](../../web/default/src/components/layout/config/top-nav.config.ts) | 动态导航优先且默认数组为空。必须修改真实导航生成接点；仅补静态配置不会可靠显示入口 |
| 站点身份 | [site-config API](../../web/default/src/features/site-config/api.ts)、[Host 边界](../../web/default/src/lib/host-scope.ts) | `tenant_id === 0` 才提供商城；以服务器按 Host 返回的站点为准，不能读取 URL 的 tenant 参数充当授权 |
| 登录和返回地址 | [auth-session](../../web/default/src/lib/auth-session.ts)、[认证路由](../../web/default/src/routes/_authenticated/route.tsx)、[auth-redirect](../../web/default/src/features/auth/lib/auth-redirect.ts) | 使用 `resolveAuthentication`、`getSignInSearch`/`sanitizeAuthRedirect`；登录后返回原页面，重新核对，不自动下单 |
| 管理端布局与守卫 | [AuthenticatedLayout](../../web/default/src/components/layout/components/authenticated-layout.tsx)、[invoice-admin 守卫](../../web/default/src/routes/_authenticated/invoice-admin/route.tsx) | 复用主站管理员守卫模式，同时接受商城权限与动作约束；不把前端 role 判断作为接口鉴权 |
| 后台工作区导航 | [use-sidebar-data](../../web/default/src/hooks/use-sidebar-data.ts)、[sidebar-view-registry](../../web/default/src/components/layout/lib/sidebar-view-registry.ts)、[AppSidebar](../../web/default/src/components/layout/components/app-sidebar.tsx) | 增加商城入口和工作区配置；复用 URL 驱动的下钻侧栏，不再造一套原型侧栏 |
| 请求、错误 | [api](../../web/default/src/lib/api.ts)、[http-client](../../web/default/src/lib/http-client.ts)、[server-error-message](../../web/default/src/lib/server-error-message.ts)、[handle-server-error](../../web/default/src/lib/handle-server-error.ts) | 统一 Axios、Bearer/刷新和业务错误；商城 API 模块负责 DTO 校验与安全错误投影 |
| Query 行为和身份切换 | [query-client](../../web/default/src/lib/query-client.ts)、[auth-query-cache](../../web/default/src/lib/auth-query-cache.ts)、[private-query-cache](../../web/default/src/lib/private-query-cache.ts)、[auth-store](../../web/default/src/stores/auth-store.ts) | 继承身份/SID 变化清缓存；私有查询增加 Host、用户和 SID 维度；秘密不进入缓存 |
| 强验证 | [verification types](../../web/default/src/features/auth/secure-verification/types.ts)、[useSecureVerification](../../web/default/src/features/auth/secure-verification/hooks/use-secure-verification.ts)、[security helper](../../web/default/src/lib/secure-verification.ts) | 沿用 proof 获取、取消和一次性使用模式，新增商城 scope/context；当前类型尚无商城 scope |
| 管理权限 | [admin-permissions](../../web/default/src/lib/admin-permissions.ts) | 当前资源为 audit/channel/task_plugin；商城能力来自后端目录扩展，不以渠道权限代替、不创建独立商城角色系统 |
| 表单与基本组件 | [Button](../../web/default/src/components/ui/button.tsx)、[Field](../../web/default/src/components/ui/field.tsx)、[Dialog](../../web/default/src/components/ui/dialog.tsx)、[AlertDialog](../../web/default/src/components/ui/alert-dialog.tsx) | 直接复用；Base UI 组合使用 `render`，不是照搬 Radix `asChild` |
| 精确金额格式 | [payment-flow](../../web/default/src/features/wallet/lib/payment-flow.ts) 的 `formatPaymentMoney` | 已有字符串拆位 + BigInt 格式器，可提取为共享纯函数并保留钱包回归；不调用充值请求或额度换算 |
| 主题 | [ThemeProvider](../../web/default/src/context/theme-provider.tsx)、[主题定制](../../web/default/src/context/theme-customization-provider.tsx)、[theme.css](../../web/default/src/styles/theme.css)、[theme-presets.css](../../web/default/src/styles/theme-presets.css) | 使用根元素 light/dark 与现有 semantic tokens；商城只增加必要的作用域视觉变量 |
| i18n | [config](../../web/default/src/i18n/config.ts)、[languages](../../web/default/src/i18n/languages.ts)、[static-keys](../../web/default/src/i18n/static-keys.ts) | 当前实际含七种语言；使用英文原文平面 key，`toIntlLocale` 转换 zhCN/zhTW |
| 定向测试 | [vitest.config](../../web/default/vitest.config.ts)、[test-setup](../../web/default/src/test-setup.ts) | 已安装 Vitest、jsdom、Testing Library/user-event；使用既有环境和实际行为断言 |

遵循 [根目录约定](../../AGENTS.md) 与 [前端约定](../../web/default/AGENTS.md)。前端约定中“层级翻译 key”与根约定冲突时，商城采用根约定的 **flat English source keys**；其常规全项目 typecheck 示例也受根目录成本验证规则约束。不能为了套模板改写既有 `new-api / QuantumNous` 标识、版权或归属。

## 2. 路由、布局与访问边界

### 2.1 页面落点

原型的 hash 和 `?scene=` 仅用于设计审核。生产使用 TanStack 文件路由，ID 和订单号按字符串处理，路由参数经 Zod 校验。以下是页面 URL；`_buyer`、`_authenticated` 是无 URL 段的布局层。

| 页面 | 生产 URL / 文件落点（计划） | 页面职责与进入条件 |
| --- | --- | --- |
| PAGE-01 | `/shop`；`routes/shop/index.tsx` | 公共商品列表、分类、搜索与分页；主站可浏览；关闭新购买时说明原因并保留订单入口 |
| PAGE-02 | `/shop/products/$productId`；`routes/shop/products/$productId.tsx` | 公共详情与图集；下架/不存在统一不可购；不获取任何库存秘密 |
| PAGE-03A | `/shop/checkout/$productId`；`routes/shop/_buyer/checkout/$productId.tsx` | 登录后的**未建单确认页**；读取当前商品版本、必要资料、条款、渠道；勾选后显式提交下单 |
| PAGE-03B | `/shop/orders/$orderNo/pay`；`routes/shop/_buyer/orders/$orderNo/pay.tsx` | 已建立订单的支付页、渠道返回页与恢复入口；读取订单快照，创建/恢复付款必须由明确动作触发 |
| PAGE-04 | `/shop/orders`；`routes/shop/_buyer/orders/index.tsx` | 本人订单分页；筛选/search 只影响列表，不改变订单状态 |
| PAGE-05 | `/shop/orders/$orderNo`；`routes/shop/_buyer/orders/$orderNo/index.tsx` | 本人订单、公开事件、额外款提示、当前版本领取与处理记录 |
| PAGE-06 | `/shop/orders/$orderNo/aftersales`；对应 `_buyer/orders/$orderNo/aftersales.tsx` | 订单内售后页面；展示当前/历史申请，提交和追加说明；无实时聊天 |
| PAGE-07 | `/shop-admin`；`routes/_authenticated/shop-admin/index.tsx` | 主站平台管理员的待办概览；统计来自事实 API，明确时间范围与币种 |
| PAGE-08 | `/shop-admin/products`；对应 `products/index.tsx`；`products/new.tsx`、`products/$productId.tsx` 为下钻 | 商品列表、新建、编辑、发布检查；草稿可以不完整，上架须满足服务器条件 |
| PAGE-09 | `/shop-admin/inventory`；对应 `inventory/index.tsx` | 脱敏库存与导入、隔离弹层；导入原文在独立临时内存中，不进路由 |
| PAGE-10 | `/shop-admin/orders`；对应 `orders/index.tsx`，`orders/$orderNo.tsx`；付款差错下钻位于同工作区 | 人工队列、订单事实、必要资料、交付与暂停；差错明细按收款 ID 独立可达，不能强依赖已有订单 |
| PAGE-11 | `/shop-admin/aftersales`；对应 `aftersales/index.tsx`、`aftersales/$caseId.tsx` | 售后审批、补发与退款责任；退款记录可通过同工作区 `refunds/$refundId.tsx` 定位，不另增加一级产品页面 |

PAGE-03 的两个 URL 是一个确认/付款功能的两种阶段。不能为了沿用原型 `checkout/1` 在页面打开或 React Effect 中自动创建订单。确认成功得到 `order_no` 后导航至原订单支付页；网络结果未知时沿同一次下单幂等键恢复，不导航至新商品重买。

新增 `routes/shop/route.tsx` 使用 `PublicLayout` 和 `<Outlet />`。布局仅做主站边界与商城公开配置读取，**不得因暂停新订单而拦截全部 `/shop/*`**。`routes/shop/_buyer/route.tsx` 负责登录守卫；公开详情仍允许访客访问。商城管理使用 `routes/_authenticated/shop-admin/route.tsx`，继承认证布局，再校验主站、平台管理员及服务端能力。

不手写 `routeTree.gen.ts`；由已配置的 TanStack 插件生成。路由组件保持薄层，只承担 search/params 校验、守卫、无秘密的 loader 与 feature 入口。预加载只能进行幂等普通查询，不能创建付款、领取秘密或触发管理员操作。

### 2.2 搜索参数、登录返回与入口关闭

- 公开列表 URL 允许 `q`、分类、分页；订单列表允许订单号、业务状态筛选和分页。白名单、长度、页大小边界跟随 API 契约；切换筛选重置页码。查询参数禁止邮箱、密码、卡密、内部证据、proof 和完整表单内容。
- 登录返回地址通过现有同源清洗函数构造。登录后只回到原商品/原订单，不恢复未经确认的提交、付款或复制动作；必要资料和协议重新确认。
- 公开页与管理页均等待站点配置成功后再作决定。配置请求失败显示可重试错误，不把 `undefined tenant_id` 当作主站许可。
- “暂停新订单”关闭新的购买与付款创建；既有订单、已存在支付尝试的核实、交付、售后和退款继续可达。界面必须依赖服务端逐动作许可，不能把一个总开关套到所有按钮。
- 用户菜单或商城导航始终保留本人订单入口；后台只对符合条件的管理员显示，租户站点不显示。隐藏入口与路由守卫只改善体验，服务器仍承担授权。

## 3. 功能目录与组件职责

采用两个 feature，买家端与管理端按路由拆包。不要建通用低代码表单、万能状态引擎或跨商城微前端。

```text
web/default/src/
  features/shop/
    api.ts                    # 买家普通查询/写入，严格使用 API 总契约
    schemas.ts                # 响应 DTO 与表单输入验证；导出推导类型
    types.ts                  # 共享领域 DTO 类型；不再手写另一份状态枚举
    query-options.ts          # 查询键、取消、轮询与有限失效范围
    constants.ts              # labelKey、展示配置；不是状态计算器
    pages/                    # Catalog / Product / Checkout / Payment / Orders / Order / Aftersales
    components/               # ProductCard / Gallery / PurchaseSummary / OrderFacts / DeliveryReveal 等
    hooks/use-delivery-reveal.ts # 短生命周期秘密请求、代次检查与清理
    lib/                      # 仅放有独立业务含义的展示、精确金额/时间规则
  features/shop-admin/
    api.ts / schemas.ts / types.ts / query-options.ts
    pages/                    # Overview / Products / ProductEditor / Inventory / Orders / Aftersales
    components/               # ImportPreview / ManualDelivery / RiskReview / RefundRecord 等
    hooks/                    # 有共享需求才提取受控请求、强验证编排
  components/layout/config/shop-admin.config.ts
  routes/shop/...             # 公共布局与 _buyer 受保护子布局
  routes/_authenticated/shop-admin/...
  styles/shop.css             # 仅视觉布局/作用域 tokens，不写业务可见性规则
  assets/shop/                # 优化 WebP；母图留在 docs，不进前端产物
```

允许合并简单的入口文件；目录树不是创建空文件的清单。页面内部单次使用的小段布局直接内联，有稳定职责、多个调用点或需要独立行为测试时才拆组件。管理端可以复用买家 feature 的 DTO 和无副作用展示组件，买家入口不得反向导入管理页面/库存解析器。

| 组件/模块 | 输入与责任 | 明确不承担 |
| --- | --- | --- |
| `ProductCard`、`ProductGallery` | 商品公开 DTO、图片资源；整卡 Link、可键盘切图、缺图 fallback | 定价、库存分配、订单状态 |
| `PurchaseSummary` / `CheckoutReceipt` | 当前确认信息或订单快照、金额、条款勾选、当前允许动作 | 由商品 ID 重建历史快照；推测支付成功 |
| `OrderFacts` / `OrderTimeline` | 普通订单 DTO 的付款、交付、售后、风险和额外款事实 | 自造聚合枚举；把普通售后当作禁止领取 |
| `DeliveryReveal` | 专门领取的临时内存、当前合法版本、查看期限、复制状态 | Query cache、DOM 隐藏明文、持久化和自动领取 |
| `ManualDeliveryForm` | 原订单身份、必要资料、交付输入、确认摘要与强验证 | 把普通备注当交付；在提交成功前显示已交付 |
| `InventoryImport` | 固定模板原文、服务端预检摘要、提交后批次引用、确认结果 | 浏览器自行决定去重/可售；整行明文错误报告 |
| `RefundRecord` | 一项退款责任及关联尝试、证据摘要、剩余可执行动作 | 调用渠道退款；混合原商品款和额外款；本地勾选等同退款成功 |
| `PaymentExceptionDetail` | 独立收款身份、差异、原映射、责任人、证据与下一步 | 跨用户改绑；把短款/异币种人工标为有效款 |

## 4. API 接入与基础数据表示

### 4.1 单一契约

买家前缀为 `/api/shop`，管理员前缀为 `/api/admin/shop`。`api.ts` 集中编码路径和请求，组件不拼端点。下表只映射已协调的买家动作，管理端完整路径和 proof scope 直接引用总契约，不能在本文件独立新增别名。

| 前端动作 | 总契约动作 | 前端约束 |
| --- | --- | --- |
| 确认购买 | `POST /api/shop/orders` | 商品 ID/版本、条款版本、`accepted_terms:true`、必要 `buyer_input`；同次请求保留幂等键，客户端不提交最终定价 |
| 恢复未知建单结果 | `GET /api/shop/order-requests/:key` | 只读同一 actor/主站/下单作用域；recorded 得到 order_no 后读取原单，not_observed 不能当作失败 |
| 查看原订单 | `GET /api/shop/orders/:order_no` | DTO 不含完整凭据；读不可变商品快照与最新动作 |
| 创建/恢复支付入口 | `POST /api/shop/orders/:order_no/payment-attempts` | 服务器决定恢复、允许新尝试或仍待核实；前端不换号强制重建 |
| 刷新付款结果 | `POST /api/shop/orders/:order_no/reconcile` 后查询原订单 | 只请求调度核实，返回受理不代表已付款；遵守限流与建议重试时间 |
| 结束本次购买 | `POST /api/shop/orders/:order_no/close` | 使用当前版本/幂等信息；仍可接收迟到款，不声称已关闭渠道 |
| 查看/复制 | `POST /api/shop/orders/:order_no/reveal` | 传当前 `delivery_id`、`intent=view\|copy`，复制另传 `field_key`；每次都重新授权；直接受控 Promise |
| 查看必要资料 | `GET /api/shop/orders/:order_no/buyer-input` | 完整值专门按需读取；直接受控 Promise，no-store，不混入普通订单 Query |
| 补必要资料 | `PUT /api/shop/orders/:order_no/buyer-input` | 只提交订单约定字段，不重设截止时间 |
| 售后及追加 | `GET/POST /api/shop/orders/:order_no/aftersales`；`POST /api/shop/aftersales/:case_id/messages` | 同时仅一个活动申请，重复提交定位现有申请；历史不可覆盖 |

ID、订单号、业务版本和金额原始值保持字符串。现有 `AuthUser.id` 是 number：可继续用于已有认证键，但不能据此对商城的 string ID 执行 `Number()`。时间为 UTC RFC3339，金额为 `{ amount_minor: string, currency: string, exponent: number }`；业务 version 按总契约的十进制版本比较，不能用字典序比较 `"10"` 和 `"9"`。

`expected_version` 始终取动作所属根资源：订单子动作取订单 version；售后子动作取案件 version，补发另带 order_version；退款尝试/登记/核实取退款责任 version；收款调整及复核取收款处理投影 version。商品、库存、分类、设置取自身 version。响应推进对应根版本后，失效和重读同一根资源；不能用子记录版本代替。首次创建不虚构 version `"0"`。

### 4.2 精确金额与时间

1. 金额解析只接受规范十进制字符串，校验符号、位数与 int64 范围；真实允许价格上下限以服务端为准。禁止 `parseFloat(amount_minor) / 100`、JS number 运算或 API quota 换算。格式器采用现有 `formatPaymentMoney` 的字符串拆位 + BigInt 方法，提取共享时只迁移纯格式功能，不带入钱包订单逻辑。
2. 金额始终与币种并列；不依据主题/站点汇率把 CNY 改为 USD。后台实收、应付、退款分别展示；不合计不同币种。异常金额不退化为 0，显示不可用并阻止依赖它的确认。
3. 价格输入先保留字符串，按所选币种 exponent 严格转换为最小单位；过多小数、负值、指数形式、超界拒绝。前端校验不替代服务器上架校验。
4. 日期用服务端 UTC 时间和订单快照时区显示，并标出时区。现有 Day.js 只注册 relativeTime；需要时区格式可直接用 `Intl.DateTimeFormat`，不假定 timezone 插件已启用。i18n locale 先经 `toIntlLocale`。
5. 倒计时参考 `server_time` 与本地单调经过时间显示，仅表达预留/查看窗口；到零后收起秘密、查询原订单，不在浏览器把订单关闭、付款失败或 SLA 重置。后台未提供交付截止时只显示起算条件，不能由演示“24 小时”补全。

### 4.3 结构化政策和期限展示

商品编辑器与买家展示直接使用 API §4.1 的 `delivery_policy`、`view_policy`、`warranty_policy`、`refund_policy`、`entitlement_policy`；条款摘要为纯文本，不解析文案生成时间或权限。草稿允许缺项，上架表单按服务器字段错误提示补齐；运营未确认的数值保持空，不填原型演示时长。

- 人工交付显式选择自然经过时间 `elapsed` 或营业时间 `business`、正整数分钟、IANA 时区；business 编辑有限每周时段，同日不重叠、跨午夜拆段，最多 14 段。是否因缺原定必要资料暂停须明确选择。前端可校验格式，实际截止由服务器按成交快照计算，不能在浏览器独立运行一套 SLA 引擎。
- 买家展示起算依据、营业口径/时区、是否按约定暂停及服务器给出的 `delivery_due_at`。起算是可信付款与必要资料首次齐全的较晚者；资料补交、普通备注、风险解除均不由客户端刷新起点。DST 与期限计算以服务器绝对 UTC 结果为准。
- 查看期限、商品权益期限和商业质保分栏展示。D1 查看期从每次交付完成起算，补发有新查看截止；补发不自动延长原权益或质保。没有额外商业质保不隐藏售后入口；权益为 described 时展示明确说明，不渲染成永久有效。
- 订单页面只读成交政策快照及真实期限；后续改商品条款不能覆盖旧订单。商品款退款为全额、管理员渠道后台原路执行；界面不提供尚未约定的部分退款或自动退款能力。

## 5. 服务端状态与前端状态边界

服务器返回普通订单事实、主展示状态、版本和 `allowed_actions`。前端只把已知状态映射为文案、图标与按钮；动作名直接使用总契约定义，不由 UI 文案反推枚举。

领域字段固定为 `purchase_state`、`payment_state`、`delivery_state`、`aftersale_state`、`refund_state`、`risk_hold`、`display_status`、`notice_codes`，枚举完整集合直接采用 API §2。不得另起 `order_state`、`after_sale_state` 等近义字段；`payment_state:paid` 和 `delivery_state:delivered` 是历史事实，退款或过查看期不将其改回未付款/未交付。

- `allowed_actions` 是当前状态的体验提示，不是授权凭证。点击前可重新读取，提交时携带预期版本；接口仍须检查最新权限/状态。409 冲突刷新记录并说明变化，不自动重复危险操作。
- 不复制原型 `state.scenario`、`currentStatus()` 或 `orderProduct()` 的内存业务模型。原型正常/异常场景成为测试 fixture，不能成为生产查询参数开关。
- 页面永远按 `order_no` 的订单快照渲染。浏览另一商品、切换分类、修改商品草稿都不得改变历史订单的金额、名称、交付字段或规则。
- `isSubmitting`、已选图集、筛选、弹层是否打开和表单 dirty 是前端 UI 状态；付款、库存、交付、退款、风险暂停和截止时间是服务端事实。不能把后者复制进 Zustand 后独立修改。

| 服务端事实组合 | 前台展示及允许动作 | 前端必须保持的边界 |
| --- | --- | --- |
| 未付款且原入口可用 | 继续付款、结束本次购买 | 未知结果不能新建第二次交易；无凭据 |
| 创建/付款核实中 | 刷新、查看原订单、勿重复支付 | 回跳、二维码关闭或超时不是成功/失败证据 |
| 本地已关闭 | 查看历史，重新购买当前商品；必要时核实原支付 | 不复活旧价格；不保证外部入口已经失效 |
| 已付待自动/人工交付 | 查看进度、必要时售后 | 不出现“再次付款”；界面不自行完成交付 |
| 人工资料未齐 | 明确缺项、补充信息、显示起算条件 | 无伪造截止，不要求密码/验证码 |
| 迟到款/库存不足/解密失败 | 已收款及交付待处理、公开说明、售后 | 不以网络错误隐藏已收到钱的事实 |
| 已交付可领取 | 主动查看、逐字段复制、售后、版本截止 | 每次操作独立授权；不消耗第二份库存 |
| 普通售后、无其他限制 | 处理进度与补充说明；合法内容仍可看 | 售后不生成新交付、不自动暂停、不解除已有暂停 |
| 独立风险暂停/未决争议 | 公开原因与处理入口 | 禁止新揭示/复制，内部证据不进入买家 DTO |
| 当前版本查看到期 | 查看期限结束、历史及售后仍可达 | 刷新、售后、复核恢复均不能重开旧窗口 |
| 原商品款退款获批/未知/失败 | 对应阶段、责任记录与进度 | 三阶段区分；失败仍暂停，不能视为已退款或恢复领取 |
| 原商品款确认全额退款 | 渠道已确认退款、原交易关联、重新购买 | 不承诺银行到账时间，不自动回库或恢复秘密 |
| 额外收款未结/退款失败 | 独立提示，正常商品交付按自身资格继续 | 可与原款退款/售后并存，不能显示所有款项结清 |
| 售后补发完成 | 新版本与新查看截止、历史关系 | 仅新版本可揭示；旧秘密停显，不隐含延长权益/质保 |
| 未知服务端枚举/字段不兼容 | 无破坏性的刷新/联系处理提示 | 不默认当作已付/可退款；未知动作不生成按钮 |

服务器主状态优先级见业务规则第 4 节；风险、期限、额外款为独立事实展示，不能用单个 Badge 丢失信息。拒付结果单独显示，不能冒充商家退款成功。

## 6. Query、轮询、竞态与秘密内存

### 6.1 普通数据查询

公开查询键采用 `['shop-public', host, 'products', normalizedFilters]` / `['shop-public', host, 'product', productId]`。私有查询优先复用 `privateUserQueryKey(auth.user.id, 'shop', host, sid, ...)`；管理数据在同一身份边界下用 `'shop-admin'` 子段，不能与买家 DTO 共用 key。不要把 buyer_input、秘密或 proof 放进 queryKey。

| 数据 | 建议起点 | 失效/清理策略 |
| --- | --- | --- |
| 公开目录、分类 | `staleTime:0`；分页查询，不加额外业务缓存 | 分类/搜索变更、重新挂载、商品发布后读取最新；库存仅展示近况，下单再验 |
| 商品详情 | `staleTime:0`；结账进入时刷新 | 版本变化重置条款确认，明确显示变化，不静默涨价 |
| 本人订单列表 | `staleTime:10s`，不跨账户保留 previousData | 下单/支付/售后动作后仅失效相关用户列表 |
| 原订单、售后、管理员详情 | `staleTime:0`，当前资源身份变更立即清理展示 | 变更成功刷新对应对象、队列与概况；不全局 invalidate 全站 |
| 管理队列/概况 | `staleTime:10–30s`，可见时低频更新 | 按响应中事实更新，不因计数变化自动处理订单 |
| 领取、复制、完整买家资料、支付入口、库存原文、强验证 proof | **不进入 Query/Mutation cache** | 按 6.2、6.3 的局部内存方案 |

所有 queryFn 接受 `AbortSignal` 并传入 Axios。现有 Axios GET 自身按 Host/SID/URL 去重，Query 已有请求共享；商城受 Query 管理且需要取消的请求使用 `disableDuplicate:true`，避免 Axios 再共享另一个订阅的 AbortSignal。仍使用统一 `api`，不另建 fetch 认证链。

显式配置私有查询的重试行为，不能默认沿用生产环境最多多次重试的全局设置：401/403/404/409/429 不盲目重试；网络读失败可有限重试一次，429 遵守 Retry-After。普通查询 DTO 经 schema 校验后才能进入缓存，业务 `success:false` 通过 `requireServerSuccess` 或契约适配器拒绝；解析错误只报告安全错误码，不打印带数据的 Zod/Axios error。

现有身份缓存边界会在 user ID 或 SID 改变时删除除 site-config/status 外的 Query，并清空 MutationCache。商城仍必须取消自己的在途请求、检查响应所属 Host/用户/SID，防止旧响应在清缓存后重新写入。只有同一订单身份、同一会话且版本不倒退的响应才能覆盖最新视图。

### 6.2 付款轮询与写入

- 进入原订单支付页先查询服务端。前台可见且处于创建/付款核实/短期自动交付处理中时，建议前 30 秒每 3 秒、之后每 10 秒查询；连续 2 分钟未变转为手动刷新提示。该数值是客户端流量策略，不能成为到账或交付承诺；服务器 Retry-After/轮询提示优先。
- 页面隐藏、离线、离开订单或注销时停轮询；恢复可见时立即读取原订单。人工待交付、售后和退款未知可采用可见时 30 秒刷新或手动刷新，无需秒级常驻。任何窗口停止只停止 UI 查询，不结束后台核实责任。
- “刷新付款结果”使用 reconcile 请求并短暂禁用重复点击；普通 GET 轮询不每次调用 reconcile。收款已确认后停止支付轮询，保留仍待处理的额外款/交付提示。
- 创建订单、payment-attempts、关闭、售后及管理动作 `retry:false`，不做已付/已交付/已退款乐观更新。请求超时显示结果待核实，查询原资源或按契约用同一幂等键重放；不能生成新 key 后偷偷再提交。
- 同一意图的 key 在用户点击时生成并保留到结果确定，不能在每次 render、Effect 或 HTTP 自动重试里生成。跨刷新恢复只持久化非秘密的 key/订单引用/scope/target 及 Host/用户/SID绑定和 TTL；绝不持久化 buyer_input、库存、交付内容、证据正文或 proof。刷新遇到未决建单意图先调用 `GET /api/shop/order-requests/:key`；recorded 后导航原订单，not_observed 只表示当前未观察到提交，不能证明未运行。保留原 key，必要时由用户重新确认并输入相同资料后显式重放；不自动换 key，也不把恢复标记 TTL 到期视为原请求失败。
- 现有拦截器可在 401 后刷新并重放普通请求。商城写入须携带原幂等键且后端拒绝失效动作；带 proof 的动作使用现有 `singleUseAuthorization`/`X-Security-Proof` 机制，先刷新身份，不在消费 proof 后自动重放。
- MutationCache 会保存 `variables`、data 和 error；仅请求与响应均无秘密的动作可用 useMutation。下单 buyer_input、资料补充、人工交付、原始库存及含敏感证据的提交采用受控 Promise + 本地 pending，失败转换为安全 DTO；不能以 `gcTime:0` 声称敏感变量从未进入缓存。
- `payment-attempts` 的 CheckoutResult 含 URL/二维码，因此也采用受控 Promise：普通 `order` 脱敏投影可更新订单 Query，`checkout` 只放支付组件短期内存，到期/离开/身份变更即清理。页面恢复后由显式动作请求原尝试入口，不从 storage、普通订单 DTO 或日志重建。
- 买家和管理员完整 `buyer-input` 使用独立 no-store 受控读取，只在本次查看/编辑/履约表单内保留必要值；按身份、订单和请求代次检查迟到响应。关闭、隐藏、离开、失去权限时清理；不得把完整资料回填到订单 Query、查询键、MutationCache 或通用审计正文。

### 6.3 领取与复制的严格边界

`DeliveryReveal` 使用组件 state/ref 和 AbortController。`api.reveal` 返回 Promise，**不使用 useQuery/useMutation、不放 loader、不预取**。普通订单 DTO 只包含当前版本 ID、脱敏摘要、截止和许可；未点击时 DOM、React props、可访问性树中都没有完整秘密。

每次查看和复制按以下顺序执行：

1. 从当前认证状态记录 Host、user ID、SID、order_no、当前交付 version 和本地 request generation；清理上次已展示内容，禁用本次按钮并建立 AbortController。
2. 用专门 `POST .../reveal` 携带当前 `delivery_id`，发起 `intent=view` 或 `intent=copy`。复制必须指定 `field_key` 并再次走服务端，不从上一次展开的字符串直接绕过权限。请求本身不提供“已付款”等客户端事实。
3. 响应回来先检查组件仍挂载、请求代次相同、身份/订单/交付版本未变且本地尚未过查看截止；任何不一致立即丢弃。订单版本推进、暂停、退款或新补发到来时废弃在途揭示和旧明文。
4. 查看只展示服务端允许的当前版本字段；复制只处理指定字段。`navigator.clipboard.writeText` 成功才提示已复制；网络后可能失去浏览器剪贴板用户激活，失败允许用户在当前已授权窗口内选择文本手动复制，不能假报成功，也不能为了绕过权限长期保存旧副本。
5. 主动收起、订单路由变更、交付版本变化、身份/SID变化、`visibilitychange` 隐藏、`pagehide`、组件卸载和已知到期时：abort、增加代次、清空 ref/state 和显示 DOM。`pageshow` 从 bfcache 返回也默认遮罩并先刷新资格。
6. 401/403、已暂停/过期/退款等拒绝立即清空全部明文并刷新普通订单；网络错误也收起并提示重新查询。原型“保留展示直到点击收起”不能替代这些生产边界。

服务器领取响应必须 no-store；浏览器不接入持久化、Service Worker cache、离线快照、开发 Query devtools 或分析事件。日志/Toast只记录安全错误码、请求 ID、操作结果；禁止 `console.error(error)` 把 Axios config.data、response、proof 或凭据传入日志。前端可释放引用，不能保证从 JS 内存、系统剪贴板或用户截图物理擦除；文案不能承诺撤回已查看信息。

管理员单条秘密查看遵循同样内存规则并先强验证。当前 `SecurityProofScope` 不包含商城，必须按总契约扩展 scope 和 context（绑定目标 ID、动作和必要版本），使用现有 `useSecureVerification` 与 dialog；验证取消/过期后不提交，不能借 `channel.key.read` 通过商城检查。请求理由和公开说明与秘密输入分开。

查看未售库存的确认摘要必须说明服务器会先将 available 原子隔离为 quarantined；不是无副作用的列表展开。reserved 不提供独立查看；已售内容按关联订单的当前交付、风险/退款及查看期限检查。管理员身份不提供绕过这些限制的“强制查看”。

## 7. 表单、确认、错误与支付返回

使用 React Hook Form + Zod 和现有 FieldGroup/Field/FieldLabel/FieldDescription/FieldError。Field 的 `data-invalid` 与控件的 `aria-invalid`/`aria-describedby` 一起设置；重要输入有可见标签。Base UI Select 的 items/组合按已安装组件 API 使用；动作等待时使用 `disabled` + Spinner，不传不存在的 `isLoading`。

| 表单 | 客户端校验/交互 | 提交与失败后的行为 |
| --- | --- | --- |
| 确认购买 | 商品/条款版本、单件事实、服务端可用渠道；协议默认不勾；必要字段按商品模板 | 价格/规则变更重新展示并清除勾选；缺货/无渠道不创建付款；未知建单结果保留恢复入口 |
| 买家补资料 | 最多约定的三个受限字段；不增加自定义字段生成器 | 服务器返回新进度/截止；禁止客户端重置 SLA；失败留在当前表单且不写持久化 |
| 售后申请/追加 | 固定问题类型、期望方案和受限文字，长度以契约为准；提示不要填密码卡密 | 已存在活动申请跳到原记录；追加文本不可编辑历史；质保外允许提交问题但不承诺赔付 |
| 商品草稿/发布 | 类型、交付模板、价格字符串、条款、时限口径/时区、查看期、容量、封面 | 保存草稿与发布校验分开；服务端逐字段错误回填；变更交付类型有历史/库存限制时不能仅前端切换 |
| 库存导入 | 固定账号/卡密模板；限制文件/行数；原文仅临时内存；预览显示行号/计数/脱敏原因 | 有错误整批不导入；确认传同一规范 rows 与 preview_digest/版本，服务端再查重；未知结果沿原幂等意图核实，不能直接再导入；成功/取消清原文 |
| 人工首交付 | 显示订单/商品/付款身份；输入实际交付内容，核对确认，必要强验证 | 原文不进入备注和 MutationCache；重复/并发冲突读取现有交付，不覆盖或变为补发 |
| 风险暂停/恢复 | 内部理由、买家可见说明、证据引用分栏；恢复显示仍有效的限制 | 没有后端许可不显示恢复动作；服务端恢复拒绝保留解释，不本地打开领取 |
| 补发 | 原申请、原订单、新库存/人工内容、依据；明确新窗口不延长其他权益 | 同一申请幂等返回既有补发；与退款冲突刷新，不尝试补发第二份 |
| 退款 | 选择明确收款责任、全额金额币种、理由；批准、渠道尝试登记、结果核实独立步骤 | 只操作同一责任；失败与未知分开；有成功/在途/拒付冲突时禁重试；原款和额外款保持不同记录 |

退款、隔离、暂停、首交付、补发等影响交易或秘密的操作使用带标题和摘要的 Dialog/AlertDialog。按钮文字描述本步事实，例如“批准退款”“登记渠道结果”，不能用一个“完成”按钮同时表示批准、资金执行和确认。Modal 打开也不发 mutation；验证/确认被取消不得消费后续操作。

库存 preview 使用 `product_id,product_version,template_version,rows`，响应是脱敏行号、计数与 `preview_digest`，**不会建立可恢复的预览批次资源**。commit 重新提交相同规范 rows 和摘要/版本，首次成功后才获得 `batch_id`。两步都直接受控请求；原文变化使旧摘要失效，必须重新预览。建议上限 200 条/1 MiB，以服务端为准。若响应丢失，内存仍在时只能复用原 key 和相同内容；刷新后调用 `GET /api/admin/shop/inventory/import-requests/:key?product_id=...` 查询原意图。recorded 返回 batch_id/inserted_count；not_observed 不证明未提交，仍保留原 key。需重传时由管理员重新选取原内容并确认，以同 key/摘要校验；内容不一致停止并说明冲突，不能自动改 key 再导入，也不能持久保存秘密以便恢复。

网络/服务端错误统一映射但避免双 Toast：局部 Field/Alert 处理的 Query/Mutation 设置 `meta.errorToast:false` 或显式 onError；底层使用 `skipErrorHandler:true` 配合安全的 feature 错误 DTO。未知错误只显示通用提示及请求引用，不拼接服务器内部 message 或导入原文。

| 错误分类 | 展示与恢复 |
| --- | --- |
| 401 | 清秘密，沿现有认证处理重新登录；返回原订单，不自动重复动作 |
| 403 / `SHOP_SESSION_REQUIRED`、`SHOP_MAIN_SITE_ONLY` | 说明当前会话/站点不支持该动作并停止；不能通过换 API Key、主机参数或其他入口回退 |
| 403 / 既有 SECURITY 错误 | 清除失效 proof，按用户明确动作重新验证；不自动重放敏感写入 |
| 403 / `SHOP_REVEAL_BLOCKED` | 清当前明文和在途揭示，展示公开限制原因并重读订单 |
| 404 / `SHOP_NOT_FOUND` | 对无权对象和不存在统一“不存在或不可访问”及本人订单入口，不泄露他人对象存在性 |
| 409 / version、条款、库存、动作冲突 | 刷新目标资源、展示已改变内容；不自动覆盖表单或继续扣款 |
| 400 / `SHOP_VALIDATION_FAILED` | 映射 field_errors 到字段并聚焦首个错误；不回显秘密原值 |
| 410 / `SHOP_REVEAL_EXPIRED` | 立即清明文、废弃在途揭示和旧复制内容；保留原交付事实、历史和售后入口 |
| 429 | 说明稍后再试，遵守 Retry-After；按钮短暂冷却不是资金结果 |
| 网络中断/5xx/创建超时 | 区分普通加载失败与已发交易结果未知；交易先查原对象，不提示随意重付 |
| 领取服务暂不可用 | 收起秘密，保留订单已付/已交付历史；提示稍后重新尝试与售后入口 |
| 退款证据不匹配/结果未知 | 保留责任与旧尝试，说明待核实；不能将客户端请求成功当作退款成功 |

渠道跳转 URL/二维码只使用服务器按已核实渠道返回的入口；禁止 query 参数任意跳转、`javascript:`、注入渠道 HTML 或把支付 URL 写入分析事件。外部跳转在明确用户动作中执行，按总契约校验 scheme/允许目标；站内 `Link`/navigate 用 TanStack。返回页只读取/调度原单核实，支付商回传参数不会直接改客户端订单状态。二维码展示可复用已安装 qrcode.react，首发渠道不需要时不做额外入口。

## 8. V2 视觉、素材与可访问性落地

### 8.1 组件和主题

V2 的核心是奶油纸面、钴蓝主动作、墨色正文、纸艺图像和编辑式商品卡。正式版保留构图，不搬入原型约 160KB 的 HTML、全局覆盖 CSS、原生 SVG 字符串和演示工具条。

- `Button`、`Badge`、`Alert`、`Empty`、`Skeleton`、`Separator`、`Tabs`、`Dialog`、`Sheet`、`Table` 使用已有 UI 组件；图标采用 components.json 指定的 Hugeicons，按钮图标设置 `data-icon`，不用动态字符串图标注册表。
- `shop.css` 只定义必要的页面构图、收据装饰、图集比例及商城作用域变量。主动作与状态映射 semantic tokens，主题预设和用户主题切换仍生效；禁止覆盖全站 `:root`、复制一个不随主站变化的 ThemeProvider 或在交易组件散落硬编码色值。
- 奶油背景/图像底板可在商城 scope 定义局部展示变量，浅深色由同一主题系统管理。Dialog 在 portal 中也须获得一致 token 作用域；不能只在页面容器加变量而让弹层丢失主题。
- 原型深色侧栏是视觉参考，正式后台通过既有 Sidebar tokens 和工作区配置实现；不改全站侧栏颜色来迁就商城。根字体沿用现有字体体系，不新增网络字体。
- 当前 Button 默认 32px、lg 36px，不能假定已满足移动触控。商城主按钮、复制、菜单、关闭目标显式达到至少 44×44px；这是布局尺寸调整，无需改所有公共按钮默认值。

### 8.2 图片接入

资产权威表见 [素材包 README](./kk-shop-assets/README.md) 和 [manifest](./kk-shop-assets/asset-manifest.json)。22 个 WebP 总计 844,186 字节；9 张 PNG 母图仅留档。生产将所需 WebP 复制到 `src/assets/shop/` 后用静态 import 交给 Rsbuild 生成带前缀的资源 URL，不能直接引用 `docs/` 或硬编码 `/shop-assets/...` 绕过发布资源路径。

| 资产 | 实际尺寸 | 使用方式 |
| --- | --- | --- |
| hero.webp / hero-mobile.webp | 1536×1024 / 900×600 | 首页 `picture`，当前首屏图 eager、fetchPriority high；装饰性 alt 为空；只有当前响应式资源下载 |
| product-1…6.webp | 1024×1024 | 商品卡/详情完整概念图，contain 避免切掉书/色样边缘；真实商品绑定自己的封面，不按数据库 ID 拼演示文件名 |
| product-N-thumb.webp | 320×320 | 结账、订单与管理列表；图像旁已有商品名时 alt 为空，避免重复播报 |
| product-N-detail.webp | 900×900 | 图集局部，明确“概念插画，非真实服务界面”；不声称新增权益 |
| service-guide.webp | 1536×1024 | 商品后的购买指南，lazy、alt 为空；步骤用真实 HTML 文字 |
| empty-state.webp | 1024×1024 | 无结果/暂无订单等普通空载；资金差错和退款失败不用可爱插画替代事实 |

声明真实 width/height 与稳定 aspect-ratio。Hero/详情主图优先加载；首屏可见商品图不必强制 lazy，按实际位置决定，以下内容 lazy。图片缺失仍保留名称、金额、库存和入口，以无歧义占位代替。深色保留纸张底色，不反相或重滤镜；展示图不是实物照片，商品类型在文字层始终可读。

### 8.3 页面验收形态

| 视口/区域 | 必须实现 |
| --- | --- |
| 1440×900 首页 | 左文右图 Hero 约 322px；商品名与金额在图上方，首排核心事实处于首屏；信任说明在商品后 |
| 390px 首页 | Hero 约 294px；右下完整 174×120 图，不裁到画布外；两列商品，名称可两行、交付标签不固定高空白 |
| 320px 首页 | 一列商品，Hero 保留约 143×107 图；中文/英文等长文案仍能自然排版 |
| 商品详情 | 桌面图集+购买摘要；手机图集、购买摘要、规则章节顺序清楚；底部购买栏不遮条款和提示 |
| 结账 | 收据摘要明确商品、币种和最终应付；装饰伪元素以收据自身 `position:relative` 定位，避免 V2 曾发现的 1px 溢出 |
| 订单/领取 | 主状态、额外款、截止和处理记录独立；长凭据按字段换行，复制不压缩到不可点 |
| 后台/弹层 | 表格仅自身容器横滚；小屏表单单列；底部动作在 100dvh/安全区内可滚动到达，沿用现有 dialog 可用高度变量 |
| 深色/200% 缩放 | 所有交易事实、焦点、原生控件及弹层可辨，无整页横溢出；图像不吞掉主要内容 |

商品卡整体一个 Link，不嵌套购买 Button。图集缩略图有明确 label、选中状态与可见焦点；换图保持当前按钮焦点，用 React 更新图像，不重建整页 DOM。章节导航尊重 reduced-motion，必要时把焦点移到章节标题，不只滚动视觉。

表单错误、订单状态更新使用适当的 `role=alert`/`aria-live=polite`，不把秘密区域设为自动朗读。完整键盘、读屏、200% 缩放、剪贴板拒绝及减少动画是实施验收项目；原型仅完成部分抽样，不能直接继承“WCAG 已通过”的结论。

## 9. 国际化与文案数据边界

根目录要求优先：locale 使用 `translation` 下的平面 JSON，key 为英文原文。不要为商城增加 `shop.order.status.paid` 这类新嵌套 key；现有历史 feature 的 dotted keys 不是新商城的规范。

- 每个独立 React 组件通过 `useTranslation()` 订阅语言。状态常量用 `labelKey` 保存英文源字符串，组件调用 `t(labelKey)`；动态映射 key 登记于 `static-keys.ts`，避免提取遗漏。
- 商品名称、规则快照、买家文字、公开处理说明和渠道证据是业务内容，直接按文字安全呈现，不能 `t(product.name)` 或拼成翻译 key；首版不偷偷引入商品多语内容管理。
- 当前实际 locale 是 `en`、`zh`、`zh-TW`、`fr`、`ja`、`ru`、`vi`，运行语言代码含 `zhCN`、`zhTW`。开发不能只完成技能旧清单里的六种语言；日期/金额经 `toIntlLocale` 进入 Intl。
- 新增 UI 文案覆盖菜单、禁用原因、表单、Toast、错误、安全说明、键盘标签、时间单位和复数，不只是标题。插值使用完整语句和稳定 `{{variable}}`；不拼接各语言的半句。
- 示例源 key：`View delivery details`、`Payment is being verified. Do not pay again.`、`The viewing period has ended.`、`An additional payment is still being processed.`。这些是文档示意，未写入 locale；实际落地前检索已有 key，复用同义且上下文一致的文案。
- 实际写 locale 前完整加载 [i18n-translate 技能](../../.agents/skills/i18n-translate/SKILL.md)，通过其脚本维护全部实际语言并按要求 sync，保留排序和占位符。文档阶段不运行会改写 locale 的 sync。
- 法/俄/越长文案、中文换行、日期时区、超长订单号和大额字符串纳入 320px 检查。不得用缩小交易正文到 10px 的方式解决溢出。

## 10. 开发拆分与依赖

每个里程碑交付可审查 diff 和本阶段行为证据；不以骨架页上线当作商城完成。

| 工作包 | 交付物 | 依赖与退出条件 |
| --- | --- | --- |
| FE-01 契约与边界 | DTO schema、精确金额/时间、普通 API、Query keys、公共/买家/管理员路由骨架、导航 | 总契约冻结基本 DTO；主站/租户、访客/管理员直达测试通过；不创建真实交易 |
| FE-02 商品与视觉 | PAGE-01/02，V2 资产接入、分类搜索分页、主题与图集 | 真实字段接口或明确隔离 fixture；1440/390/320 无溢出、缺图可用；全部文案进入 i18n |
| FE-03 确认与支付 | PAGE-03/04，幂等确认、原订单恢复、回跳、未知状态和精确金额 | 一个渠道契约可用；连点/超时/变价/无渠道/缺货/登录返回不重付；商城不调用充值 API |
| FE-04 交付与售后 | PAGE-05/06、受控 reveal/copy、资料补充、售后及补发版本展示 | 自动与人工 DTO 齐备；身份切换/离页/乱序响应/暂停/到期均不泄露，普通售后保留合法领取 |
| FE-05 商品与库存运营 | PAGE-07/08/09、草稿/发布门禁、脱敏导入和隔离 | 管理权限与批次契约；错误整批拒绝、确认前变化冲突、无明文缓存/日志；统计可复核 |
| FE-06 人工/异常/退款运营 | PAGE-10/11、强验证、首交付、风险、独立差错、补发、退款责任和重试 | 商城 scope 和 proof 后端可用；两个管理员冲突、原款/额外款并存、失败/未知/争议分离通过 |
| FE-07 集成收口 | 全部启用语言、可访问性、错误恢复、相邻导航/认证/钱包回归 | 自动+人工完整闭环、24 AC/40 BR 中前端责任通过；真实支付与服务端验收另由对应工作包证明 |

库存、售后、退款不能被以“后续增强”方式排除首版。未确定经营参数的工作可用隔离 fixture 继续开发，发布条件保持未满足。只为当前页面和稳定业务概念拆函数；不为以后购物车、多商户、优惠券建立隐藏配置或预留空页面。

## 11. 定向测试与可观察验收

测试聚焦真实用户行为、秘密边界和契约。原型 `/tmp` Node 断言不是生产测试；应将其保护的不变量转换为 React/API 级用例，不能把演示 `state.scenario` 引入应用。

| 建议测试文件/范围（待创建） | 关键断言 | 对应条款 |
| --- | --- | --- |
| `shop/schemas.test.ts`、金额/时间测试 | int64 字符串完整展示；异常格式不变 0；币种 exponent 与 locale正确；UTC/时区、无截止不伪造 | AC-02/14，BR-03/15 |
| `shop/checkout.test.tsx` | 默认未勾协议；变价重确认；同 key 连点恢复同单；未知不重下；登录返回不自动付款 | AC-02/03/04，BR-02/04/06 |
| `shop/payment.test.tsx` | 回跳仅核实；轮询退出清理；迟到款显示已付异常；新旧版本响应不倒退；关闭不保证渠道关单 | BR-04/06/11/13/29 |
| `shop/order.test.tsx` | 浏览其他商品不改订单快照；原款退款与额外款未结同屏；普通售后不会造出交付 | AC-08/09/11，BR-20/23/30/31 |
| `shop/hooks/use-delivery-reveal.test.tsx` | 未主动查看无秘密；每次复制重新请求；切用户/路由/版本/隐藏/到期后清理；迟到响应不重现；缓存/Toast无明文；剪贴板失败不假报 | AC-06/17/18/24，BR-17/18/33/34/40 |
| `shop-admin/inventory.test.tsx` | 脱敏预检、整批拒绝、同批次恢复、确认冲突、取消清原文 | AC-10，BR-28 |
| `shop-admin/fulfillment.test.tsx` | 必要内容与确认；强验证取消不提交；并发冲突读取原记录；待补资料不重设时限 | AC-07/18，BR-15/16/34 |
| `shop-admin/refunds.test.tsx` | 审批与核实分开；失败/未知分开；重试绑定原责任；在途/拒付冲突不能继续；额外款不禁正常领取 | AC-09/20/21，BR-21/22/23/36/37 |
| 相关路由/缓存集成 | 访客、租户、API Key/PAT不通过商城会话边界；管理员直达失败；logout/SID切换清理；已有钱包与导航保持行为 | AC-06/15/22/23 |
| 浏览器交易与视觉抽样 | 11 页、主要异常、1440/390/320、深色、200%缩放、键盘/读屏、焦点恢复 | AC-13/14；V2视觉复审未测项 |

前端 fixture 可以模拟可信付款、暂停和退款 DTO，以验证显示和动作；不能用它证明真实验签、数据库原子性、加密、三数据库兼容或资金到账。后端并发、接口越权直调、原充值/商城同渠道交叉通知仍须单独验收。

### 11.1 实施时的最小命令范围

以下命令是**实施后的运行建议，本文编写未执行它们**。路径在文件创建后使用，不能对不存在的 feature 宣称测试通过。

```sh
# 在 web/default 下，只运行商城新增测试；若抽取共享金额/认证逻辑，再加入受影响的现有测试文件。
bun run test -- src/features/shop src/features/shop-admin

# 仅检查本次实际修改文件/目录；导航或共享模块改动须显式追加到参数。
bunx --no-install oxlint -c .oxlintrc.json src/features/shop src/features/shop-admin src/routes/shop src/routes/_authenticated/shop-admin
```

共享金额提取应追加现有 `src/features/wallet/lib/payment-flow.test.ts`；改动身份边界应追加 `src/lib/auth-query-cache.test.ts` 和对应路由测试。不是对整个 wallet/auth 目录反复运行。受保护版权头不能被格式化删除或改写。

当前 `bun run typecheck` 为 `tsgo -b`，`tsconfig.app.json` 包含整个 src；`lint`、`format:check` 以及 build 也覆盖前端工程，不能伪装成单文件检查。L0–L2 不自动运行这些全量命令；如类型检查没有现成且可靠的局部项目，报告缺口，并在执行全项目命令前按根约定说明准确命令、必要性和预计时间，取得明确授权。正式发布仍按发布手册的必需门禁执行；本开发规格不授权发布。

### 11.2 本文验收记录

本轮仅新增本开发文档；未修改 HTML、locale、路由、组件、支付或业务代码，未启动应用、未访问生产、未执行测试/lint/typecheck/build。实际完成的是源码接点核查、与 PRD/业务规则/V2 的逐项对照及文档链接/空白检查。实现状态、真实端点可用性、完整可访问性和交易正确性均等待上述工作包交付后验证。

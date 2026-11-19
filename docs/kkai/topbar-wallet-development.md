# 顶栏钱包入口开发规格

日期：2026-11-19。状态：开发基线。

本文把已确认的顶栏钱包设计、钱包充值产品文档和当前 `web/default` 源码整理成可直接拆分执行的开发规格。交付目标是已登录控制台中的顶部钱包入口、余额展示、快捷浮层和钱包页导航。顶栏只负责余额读取、展示与导航，充值订单、支付状态和账务事实继续由现有钱包页与钱包支付生命周期承载。

## 1. 依据与优先级

实现时按以下顺序解释需求。

1. 当前文档的明确决策。
2. [钱包充值订单生命周期 PRD](./wallet-payment-lifecycle-prd.md) 中已生效的订单、支付和结果规则。
3. [钱包充值前端开发规格](./wallet-payment-frontend-dev-spec.md) 中的查询、身份、竞态、支付和兼容约束。
4. [钱包充值验收规格](./wallet-payment-acceptance-spec.md) 中的资金与兼容验收要求。
5. [顶栏钱包交互原型](/Users/tokk/.codex/visualizations/2026/11/19/01a1f15d-a95f-7332-9ef8-87a50e78a22c/topbar-wallet.html) 与用户提供的顶部导航截图。

原型中的余额、订单、按钮和时间均为示例数据。示例数据不能进入业务代码，不能改变已有支付生命周期，也不能替代接口或真实状态验收。

## 2. 产品目标与范围

### 2.1 目标

- 在搜索与通知之间增加紧凑的钱包入口。
- 默认显示当前可用余额，让用户无需离开当前页面即可确认余额。
- 鼠标移入后自动展开快捷浮层，鼠标离开整个入口与浮层区域后自动收起。
- 支持键盘、触屏、外部点击、隐私遮罩和减少动态效果。
- 通过“查看钱包”和“去充值”进入现有钱包页，不在顶栏创建订单或执行付款。
- 让顶栏与钱包页使用同一份余额数据、同一套货币格式和同一条缓存边界。

### 2.2 本期边界

- 只接入已认证的 `web/default` 控制台头部 `AppHeader`。
- 访客公共头部不展示钱包入口。`public-header.tsx` 保持现有职责。
- classic 主题不在本次实现范围内。
- 不新增余额接口、数据库字段、支付协议、订单状态或充值表单。
- 不把会员额度、邀请奖励待结算额度或累计充值金额并入钱包余额。
- 不在浮层内放充值金额、支付方式、支付报价或订单创建按钮。
- 不新增生图资源。钱包、眼睛、加号、箭头和下拉图标沿用现有图标依赖，动效使用现有 CSS 和 Base UI 能力。

## 3. 已确认的视觉与交互决策

| 项目 | 开发决策 |
| --- | --- |
| 入口位置 | `Search` 之后、`NotificationPopover` 之前，语言切换与头像之后的现有顺序保持不变 |
| 入口形态 | 紧凑胶囊，桌面目标尺寸约 `112×32px`，余额数字使用 `tabular-nums` |
| 正常金额颜色 | 浅色主题使用 `#865A16`，深色主题使用 `#F2C879`，金额不使用绿色 |
| 浮层大额颜色 | 使用正文层级颜色，入口金额保留香槟金强调 |
| 主要动作颜色 | “去充值”使用现有蓝色主按钮体系 |
| 桌面展开 | `75ms` 悬停意图延迟 |
| 桌面收起 | 入口和浮层都离开后 `140ms` 收起延迟 |
| 安全区域 | 入口与浮层使用 Base UI Popover 的安全多边形，跨越浮层间隙时保持打开 |
| 移动端行为 | 点击触发同一个锚定浮层，不使用 hover，不切换成底部抽屉 |
| 隐私默认值 | 默认显示，遮罩状态只保留在当前组件会话，不写入 `localStorage` |
| 浮层内容 | 钱包可用余额、刷新状态、最近成功更新时间、显示／隐藏余额、查看钱包、去充值 |
| 动效 | 轻微透明度、位移、缩放和一次性余额变化反馈，不使用持续发光或闪烁 |
| 减少动态效果 | 关闭位移、缩放和数字变化动画，保留内容和状态切换 |

金额颜色只承担视觉层级。低余额、过期、刷新失败和成功状态都必须同时提供文字、图标或状态标记，不能依赖颜色单独传达含义。

## 4. 源码事实与实现选择

| 已核实源码事实 | 实现选择 |
| --- | --- |
| `web/default/src/components/layout/components/app-header.tsx` 已集中渲染搜索、通知、语言和头像 | 新增 `HeaderWallet`，插入默认右侧工具区。保留 `rightContent` 覆盖行为 |
| `web/default/src/components/layout/components/public-header.tsx` 是独立头部实现 | 本期只改认证控制台的 `AppHeader`，不把钱包入口扩散到公共页面 |
| 头像菜单通过 `useIsSidebarModuleVisible('/wallet')` 控制钱包链接 | 顶栏沿用同一可见性规则。钱包被管理员或用户配置隐藏时，组件不渲染，也不发余额请求 |
| `getWalletBalance()` 请求 `GET /api/user/self` | 继续读取 `data.quota`。不新增顶栏专用 API |
| 钱包页现有查询键为 `['wallet-payment', ...identity, 'balance']` | 顶栏与钱包页共享完整用户数据和同一查询键 |
| `useWalletPaymentIdentity()` 包含 host、tenant、user、session | 顶栏必须沿用该身份边界。主站 `tenantId = 0` 是有效值，不能用真假判断过滤 |
| 认证缓存边界在身份变化时清理私有查询 | 顶栏在身份变化时立即撤下旧金额，并等待新身份查询成功后再显示新余额 |
| `/wallet` 当前支持 `show_history`、`trade_no`、`history_scope`、`invalid_payment_return` | 顶栏不发明充值搜索参数，不携带未知参数进入钱包页 |
| `RechargeFormCard` 使用 `wallet-recharge-heading` 作为标题标识 | “去充值”使用该稳定定位点。旧版 `wallet-add-funds` 只保留兼容用途，不作为通用入口目标 |
| 生命周期钱包在已有订单时优先渲染 `CurrentTopupOrder` | “去充值”只能导航到钱包页。已有当前订单时由钱包页继续展示订单，不能强行打开充值表单 |
| 充值成功和兑换成功已有余额刷新路径 | 所有成功入账、兑换成功和人工入账后的相关 mutation 都必须失效或刷新 `balance` 查询 |
| 会员中心也直接订阅同一 `balance` 查询键 | 会员中心改用共享余额 hook，避免顶栏、钱包页和会员中心各自实现一份余额读取 |
| 运行时语言资源包括 `en`、`zhCN`、`zhTW`、`fr`、`ru`、`ja`、`vi` | 所有新增可见文案和无障碍名称覆盖七套资源 |

## 5. 角色分工与交付物

| 角色 | 负责内容 | 交付物与完成条件 |
| --- | --- | --- |
| 产品与交互 | 确认入口位置、颜色、状态、动效时序、移动端行为和两个动作的语义 | 设计决策与验收编号不再出现“底部抽屉”“绿色金额”“点击才展开”等旧方案 |
| 前端架构 | 提取余额查询、接入头部、处理可见性、路由和支付失效 | `HeaderWallet`、共享余额 hook、头部接入、导航落点和相关单测 |
| UI 实现 | 使用 Base UI Popover、现有 Button、Skeleton、图标和主题 token | 入口、浮层、遮罩、焦点、窄屏和减少动效均符合本规格 |
| API 与数据评审 | 核对 `/api/user/self`、查询键、身份边界、缓存和 429 规则 | 无新增接口。余额不与会员额度混算，异常数据不转成零 |
| QA 与无障碍 | 验证计时器、Portal 间隙、键盘、触屏、隐私、七语、响应式和竞态 | 定向测试与真实浏览器验收通过，关键证据记录在交付单中 |
| 代码评审 | 检查重复请求、旧余额串显、支付导航绕过和不必要依赖 | 变更集中在钱包 feature 与认证头部，未引入独立缓存或新 UI 库 |
| 技术文档 | 维护本规格和现有支付文档的引用关系 | 产品规则只保留一个来源，接口字段不在多个文档中复制定义 |

## 6. 前端实现方案

### 6.1 文件计划

| 文件 | 改动 |
| --- | --- |
| `web/default/src/components/layout/components/app-header.tsx` | 在搜索与通知之间挂载 `HeaderWallet`，只在默认右侧内容路径出现 |
| `web/default/src/features/wallet/components/header-wallet.tsx` | 新增入口、浮层、隐私遮罩、状态展示和两个导航动作 |
| `web/default/src/features/wallet/hooks/use-wallet-balance.ts` | 从钱包页提取共享余额查询、身份就绪和 429 冷却判断 |
| `web/default/src/features/wallet/index.tsx` | 使用共享余额 hook，保持钱包页现有统计和支付流程行为 |
| `web/default/src/features/membership/index.tsx` | 使用共享余额 hook，保留会员购买页读取钱包余额的现有用途 |
| `web/default/src/features/wallet/components/recharge-form-card.tsx` | 保持 `wallet-recharge-heading` 稳定。只有在当前结构无法可靠定位时才调整锚点 |
| `web/default/src/features/wallet/lifecycle-wallet-page.tsx` | 核对已有订单分支、支付成功刷新和充值标题定位 |
| `web/default/src/features/wallet/legacy-wallet-page.tsx` | 核对旧模式充值定位与余额刷新，不让旧锚点成为新页面的唯一目标 |
| `web/default/src/features/wallet/components/dialogs/billing-history-dialog.tsx` | 核对历史订单成功操作后的 `balance` 查询失效 |
| `web/default/src/i18n/locales/*.json` | 按项目现有 i18n 维护脚本增加七语文案，再执行 `bun run i18n:sync` |
| `web/default/src/features/wallet/components/header-wallet.test.tsx` | 新增行为测试，覆盖浮层、隐私、焦点和导航 |
| `web/default/src/features/wallet/index.test.tsx` | 扩展共享查询、身份切换、零余额、缓存失败和请求去重回归 |

组件放在钱包 feature 内，避免把余额与支付业务复制到 layout 层。`AppHeader` 只负责挂载，不直接读取 `/api/user/self`。

### 6.2 共享余额查询

`useWalletBalance` 必须成为顶栏、钱包页和会员中心的唯一余额读取入口。hook 接收 `enabled` 参数。`HeaderWallet` 传入钱包模块可见性，钱包页和会员中心传入自身页面所需的启用条件。

- 调用现有 `getWalletBalance(signal)`。
- 查询键固定为 `['wallet-payment', ...identity, 'balance']`。
- `identity` 使用 `useWalletPaymentIdentity()` 的 host、tenant、user、session 顺序。
- 身份的每一部分都就绪后才启用查询。`tenantId = 0` 不能被判定为未就绪。
- 继续使用 `networkMode: 'always'`、`retry: false` 和 `meta.errorToast: false`。
- 保留现有 `refetchOnMount`、`refetchOnWindowFocus`、`refetchOnReconnect` 的 429 截止时间判断。
- 继续把 `AbortSignal` 传给请求。组件卸载、身份切换和路由切换不能让迟到响应写入新身份查询。
- 不添加轮询。悬停、展开、收起和隐私切换都不能触发余额请求。
- 保留完整的 `UserWalletData`，钱包页继续读取 `used_quota` 和 `request_count`。
- 响应必须同时满足成功标记、存在 `data` 和有限数值 `data.quota`。缺失、`NaN`、无穷或错误结构显示“暂不可用”，不能显示为 `0`。
- 真实 `quota: 0` 显示格式化后的零余额。
- 首次加载无数据时显示稳定骨架或占位。已有同身份数据刷新失败时保留旧余额，并显示刷新失败或可能过期状态。
- 429 有可信 `Retry-After` 时遵守截止时间。没有可靠截止时间时允许用户手动重试，但不自动快速重发。

顶栏单独挂载时只发余额请求。它不能因为展开浮层而启动充值配置、支付报价、订单轮询、历史记录或任何写入接口。

### 6.3 可见性与身份隔离

- `HeaderWallet` 使用 `useIsSidebarModuleVisible('/wallet')`。
- 钱包模块隐藏时不渲染触发器，不渲染浮层，不创建余额查询订阅。
- 访客、身份未完成初始化和用户退出时不显示旧余额。
- host、tenant、user 或 session 任一变化时，入口先回到加载或不可用状态，不允许旧账号金额闪现。
- 依赖现有 `installAuthQueryCacheBoundary` 清理私有查询，不新增 `localStorage` 锁或余额副本。
- 会员额度来自独立会员数据源。顶栏只显示 `data.quota`，不读取或合并 `membership` 的可用额度。

### 6.4 Popover 交互

使用项目现有 `@/components/ui/popover`，不要添加新的 Popover 库或复制原型中的独立 DOM 控制器。

- `Popover.Trigger` 使用 `openOnHover`、`delay={75}`、`closeDelay={140}`。
- 依赖 Base UI 的 `safePolygon` 处理触发器与 Portal 浮层之间的移动路径。
- 鼠标从入口进入浮层时不重复播放展开动画，不重新请求余额，也不重置隐私状态。
- 鼠标离开入口和浮层整体区域后才开始收起计时。计时期间返回任一部分都取消收起。
- 快速掠过入口时，未达到 `75ms` 不展开。
- 点击触发器可以在特殊鼠标环境下切换状态。点击不是桌面端唯一打开方式。
- 键盘聚焦触发器后，`Enter` 和空格都能打开。打开后 `Tab` 能访问隐私按钮、查看钱包和去充值。
- `Escape` 立即关闭并将焦点返回触发器。鼠标悬停触发的打开不能抢走搜索框或页面其他控件的焦点。
- 点击 Popover 外部关闭。浮层关闭后内部控件不进入 Tab 顺序和读屏树。
- Escape 关闭后，指针停留在入口上不能形成重开抖动。需要通过真实浏览器验证，而不能只依赖组件单测。

### 6.5 余额隐私

- 默认显示真实余额。
- 隐藏状态同时覆盖入口金额、浮层金额和所有由该余额生成的可访问名称。
- 隐藏后只展示固定遮罩，例如 `••••••`。不得在 `aria-label`、`title`、tooltip、live region、`data-*` 属性或错误提示中保留原金额。
- 隐藏和显示只改变展示状态，不触发重新请求，不改变 React Query 数据。
- 默认不跨刷新、跨标签页或跨会话记忆隐藏状态。若未来需要记忆，另行确认隐私策略后实现。
- 图标为装饰内容时使用 `aria-hidden="true"`。隐私按钮使用翻译后的“显示余额”或“隐藏余额”名称和 `aria-pressed` 状态。

### 6.6 导航落点

#### 查看钱包

- 从其他页面进入 `/wallet` 概览。
- 从钱包页自身触发时保留已通过 `walletSearchSchema` 校验的当前回跳定位参数，不复制无关查询参数。
- 不创建订单，不改变当前订单，不清除有效回跳结果。

#### 去充值

- 从其他页面进入 `/wallet#wallet-recharge-heading`，实现时使用稳定的 `wallet-recharge-heading` hash，不添加新的 recharge search 参数。
- 空闲钱包加载完成后，滚动到两种钱包模式共用的充值标题，并将焦点交给可操作的充值区域或其标题。
- 钱包使用懒加载时，必须在充值组件挂载后再次处理定位，不能依赖路由首次解析时恰好存在 DOM。
- 已有当前订单时，钱包页优先显示当前订单。hash 不能解锁充值表单，不能绕过订单互斥规则。
- 充值配置失败或支付暂不可用时，保留钱包页现有原因和刷新动作，不把“去充值”变成创建请求。
- 在同一 `/wallet` 路由重复点击时仍能定位或聚焦当前可操作区域。

路由动作的最终实现可以使用 `Link` 或类型安全的 `navigate`。不能直接拼接未经校验的 `window.location`，不能把顶栏点击误认为支付成功、取消或返回凭证。

### 6.7 充值后的余额失效

顶栏余额必须跟随现有钱包动作更新。

- 生命周期支付进入 `success` 或 `credit_review` 后失效同身份 `balance` 查询。
- 旧版充值成功后通过现有 `onRefreshUser` 或同一查询失效路径更新余额。
- 兑换成功、邀请奖励入账、人工入账刷新和历史订单已确认收款后的成功操作都检查 `balance` 查询失效。
- `pending`、`processing`、仅本地 `ended` 或仅渠道处理中不能伪造余额已增加。
- 查询失效不能改变当前订单、回跳参数或隐私遮罩状态。

### 6.8 低余额状态

设计稿没有展示独立的低余额卡片。本期不新增业务阈值，也不在组件内硬编码金额阈值。若产品后续确认在顶栏展示低余额状态，必须复用现有站点 `quota_remind_threshold` 和用户 `quota_warning_threshold` 的解析规则。

启用后遵循以下视觉约束。

- 正常金额仍使用香槟金，不切换成绿色。
- 低余额使用暖色标记、图标或文字说明，不能只换金额颜色。
- 不使用持续闪烁、呼吸光或强制打断用户的动画。
- 没有有效阈值时不显示低余额告警。

## 7. 视觉实现要求

### 7.1 主题与层级

- 浅色主题金额使用 `#865A16`，在白色背景上的对比度约为 `6.02:1`。
- 深色主题金额使用 `#F2C879`，在 `#1C1C1C` 背景上的对比度约为 `10.80:1`。
- 入口边框、图标底色和浮层边框使用现有主题 token 混合，不写死整套全局颜色。
- 浮层余额标题使用 muted 文本，余额主体使用正文层级，更新时间使用 muted 文本。
- “去充值”沿用蓝色主按钮和相应前景色，按钮文字必须在浅色与深色主题下满足 AA 对比度。
- 余额数字使用 `font-variant-numeric: tabular-nums`，长金额不能造成入口横向跳动。

### 7.2 尺寸与响应式

- 桌面浮层宽度约 `275px`，靠右对齐，带安全间隙。
- `595px` 以下隐藏顶部普通导航，但钱包入口仍可见。
- `410px` 以下允许隐藏“钱包”文字，只保留钱包图标和金额。
- `410px` 以下浮层宽度使用 `min(275px, calc(100vw - 24px))`，确保不越出视口。
- 粗指针下入口、隐私按钮和两个动作的可点击区域至少 `44×44px`。
- `320px`、`375px`、`410px`、`411px` 和桌面 `200%` 缩放都不能新增横向滚动。
- 法语、俄语和越南语长文案与大额余额都必须经过窄屏验证。

### 7.3 动效边界

- 展开使用轻微透明度、向下位移和缩放。
- 收起使用短过渡，不延迟按钮点击反馈。
- 余额真实变化时最多播放一次轻量变化反馈。相同金额重新渲染不能重播。
- 充值成功反馈由钱包页负责，顶栏只刷新余额并保持克制的状态提示。
- 余额没有变化时不得循环播放数字动画。
- 不使用持续发光、大面积闪烁或把低余额做成持续跳动。
- `prefers-reduced-motion: reduce` 下移除位移、缩放、数字滚动和非必要过渡。

## 8. 国际化与无障碍

### 8.1 国际化

- 组件使用 `useTranslation()`，禁止直接写用户可见中文或英文。
- 优先复用已有 `Wallet`、`Current Balance`、`Not available`、`Retry balance`、`Last updated:` 等 key。
- 新增 key 时按项目现有 i18n 维护脚本写入全部语言，再执行 `bun run i18n:sync`。不得只改单个 locale 文件。
- 覆盖 `en`、`zh`、`zh-TW`、`fr`、`ru`、`ja`、`vi`。
- 入口名称、浮层标题、显示／隐藏余额、刷新状态、过期提示、不可用提示、查看钱包和去充值都需要翻译。
- 文案切换后浮层不应关闭，当前焦点和隐私状态保持不变。
- 时间格式使用项目现有 `toIntlLocale` 或同一语言格式化路径，不在组件内手写语言判断。

### 8.2 无障碍

- 触发器使用明确的可访问名称，包含“钱包”和当前展示状态。
- 同步 `aria-expanded`、`aria-controls` 和隐私按钮的 `aria-pressed`。
- 浮层有可访问标题，例如“钱包快捷操作”。
- 加载、刷新、不可用和过期状态使用 `role="status"` 或 `role="alert"` 的正确语义，避免重复播报。
- 真实余额变化只播报最终展示值。隐藏状态下不能通过读屏输出原金额。
- 颜色不能作为唯一状态表达方式。
- 键盘焦点顺序为触发器、隐私按钮、查看钱包、去充值。
- Escape 后焦点回到触发器。浮层关闭后内部控件离开可访问树。
- 鼠标悬停展开不应抢焦点，避免打断正在使用搜索框或其他控件的用户。

## 9. 验收矩阵

| 编号 | 前置条件 | 操作 | 通过条件 | 主要负责人 |
| --- | --- | --- | --- | --- |
| A01 | 钱包模块可见，余额已加载 | 鼠标进入入口，分别停留 `74ms` 与 `75ms` | `74ms` 不展开，`75ms` 展开。快速移走不产生误闪烁 | 前端、QA |
| A02 | 浮层已展开 | 从入口斜向穿过间隙进入浮层 | 浮层保持展开，按钮可点击，不重新播放展开动画 | 前端、QA |
| A03 | 浮层已展开 | 离开入口与浮层，分别观察 `139ms` 与 `140ms` | `139ms` 仍展开，`140ms` 开始收起。延迟期间返回会取消收起 | 前端、QA |
| A04 | 页面焦点在搜索框 | 鼠标悬停钱包 | 浮层展开但搜索框焦点不被抢走 | 前端、无障碍 |
| A05 | 入口可聚焦 | Tab、Enter、空格、Tab、Shift+Tab、Escape | 可完成打开、访问三个控件、关闭，Escape 后焦点回入口 | 前端、无障碍 |
| A06 | 触屏或粗指针 | 轻点入口、再次轻点、点外部 | 一次触摸只切换一次，点外关闭，所有触控目标至少 `44×44px` | 前端、QA |
| A07 | 首次余额请求慢 | 打开页面并等待响应 | 显示稳定占位，不闪现错误 `0`。成功后显示 `formatQuota` 结果 | 前端、QA |
| A08 | 接口返回 `quota: 0` | 查看入口和浮层 | 明确显示真实零余额，不能被当作不可用或加载中 | API、QA |
| A09 | 首次请求失败 | 查看入口和浮层 | 显示暂不可用和可用重试，不显示 `0` | API、QA |
| A10 | 已有缓存后刷新失败 | 触发窗口焦点或手动刷新 | 保留旧余额，显示可能过期或刷新失败状态，并保留最后成功时间 | 前端、QA |
| A11 | 接口返回 `429` | 在冷却期间切窗、重连、展开和收起 | 不快速重试。达到可靠截止时间后才允许重试 | 前端、QA |
| A12 | 已登录账号 A 切换到 B | 让 A 的响应延迟到 B 身份就绪后返回 | B 页面不出现 A 的余额，旧响应不能写入 B 的查询 | 前端、QA |
| A13 | 钱包页、会员中心和顶栏同时挂载 | 观察 Network 和 Query Cache | 同一身份只发一次在途余额请求，三处显示相同钱包余额 | 前端、性能 |
| A14 | 余额隐藏 | 点击眼睛按钮，检查入口、浮层、DOM、tooltip、读屏文本 | 所有可见和可访问文本都不暴露原金额，切换不发请求 | 前端、无障碍 |
| A15 | 空闲钱包 | 点击“查看钱包”和“去充值” | 分别进入钱包概览和充值标题定位，两个动作都不创建订单 | 前端、QA |
| A16 | 生命周期钱包有当前订单 | 点击“去充值” | 保留当前订单详情，不能通过 hash 打开充值表单或绕过订单互斥 | 前端、产品 |
| A17 | 旧版钱包有充值表单 | 点击“去充值” | 旧版充值区可见可操作，不能只落到无效的生命周期锚点 | 前端、QA |
| A18 | 充值配置不可用 | 点击两个动作 | 进入钱包页并显示既有原因和刷新动作，不发支付写入请求 | 前端、QA |
| A19 | 支付成功、兑换成功或人工入账 | 完成现有成功路径 | 顶栏余额随同一 `balance` 查询更新，隐私状态不被重置 | 钱包、前端 |
| A20 | 会员额度存在 | 查看顶栏和会员页 | 顶栏只显示钱包 `quota`，不显示会员额度或合并后的数字 | 产品、QA |
| A21 | 七种语言与长金额 | 切换语言并测试窄屏 | 文案完整，入口不挤掉通知和头像，浮层不越界或产生横向滚动 | i18n、QA |
| A22 | 浅色、深色与减少动态效果 | 切换主题和系统设置 | 金额颜色符合稿件，正常金额不使用绿色。减少动效后内容和操作完整可用 | UI、QA |

## 10. 测试与质量门禁

### 10.1 代码测试

实现后优先执行受影响范围的定向测试。

```sh
cd /Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default
bun run test src/features/wallet/components/header-wallet.test.tsx src/features/wallet/hooks/use-wallet-balance.test.tsx src/features/wallet/index.test.tsx src/features/membership/membership.test.tsx src/features/wallet/wallet-payment-i18n.test.ts
./node_modules/.bin/oxlint -c .oxlintrc.json src/features/wallet/components/header-wallet.tsx src/features/wallet/hooks/use-wallet-balance.ts src/features/wallet/index.tsx src/features/membership/index.tsx src/components/layout/components/app-header.tsx src/features/wallet/components/header-wallet.test.tsx src/features/wallet/hooks/use-wallet-balance.test.tsx src/features/wallet/index.test.tsx src/features/membership/membership.test.tsx src/features/wallet/wallet-payment-i18n.test.ts
bun check -b
bun run i18n:check
```

若实际文件名或测试落点调整，命令必须同步更新。测试应覆盖真实用户行为、共享查询、身份隔离和支付后余额刷新，不用 sleep、随机循环或只断言内部实现的测试替代行为验证。

### 10.2 浏览器验收

- 用真实鼠标验证 Portal 浮层跨间隙移动。
- 用键盘和 VoiceOver 验证焦点、名称、展开状态、隐藏状态和关闭后的焦点回收。
- 在 `320px`、`375px`、`410px`、`411px` 和桌面 `200%` 缩放下检查布局。
- 在系统减少动态效果开启与关闭时分别检查展开、收起和余额变化。
- 在七种语言中检查入口宽度、按钮换行、浮层边界和更新时间。
- 通过 Network 检查顶栏单独挂载时没有订单、报价、充值配置和支付写入请求。

### 10.3 文档与差异检查

```sh
cd /Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai
git diff --check -- docs/kkai/topbar-wallet-development.md
rg -n 'wallet-recharge-heading|wallet-payment.*balance|F2C879|865A16|75ms|140ms|zh-TW' docs/kkai/topbar-wallet-development.md
```

本次文档任务属于 L0 文档变更。编写文档时不运行前端全套测试、构建或生产检查。代码落地后仍需执行上面的定向测试和真实浏览器验收。全套测试只有在单独授权后再执行。

## 11. 交付顺序与回退

1. 前端负责人提取 `useWalletBalance`，先让钱包页继续通过原有测试。
2. 接入 `HeaderWallet`，完成可见性、余额状态和 Popover 交互。
3. 完成充值标题定位和已有订单保护，验证旧版与生命周期钱包。
4. 完成七语文案和无障碍名称，通过 i18n 脚本同步资源。
5. 执行定向测试、类型检查、lint 和浏览器验收。
6. 代码评审确认支付、会员额度、旧余额和身份竞态没有回归后，再进入前端发布流程。

回退时先移除 `AppHeader` 的 `HeaderWallet` 挂载，保留共享余额 hook 对钱包页的兼容。顶栏发布不需要数据库回退，也不改变支付订单、余额账务或生产控制器。任何余额串显、遮罩泄露、重复创建订单、充值落点错误或当前订单被绕过的情况都阻止继续放量。

## 12. 当前未完成项

- 本文已完成设计与实现规格整理，尚未修改前端代码。
- 顶栏组件、共享余额 hook 和对应测试仍待实现。
- A01—A22 是实现后的签收条件，目前没有真实组件通过记录。
- 本轮没有访问生产环境，没有执行 schema 迁移，没有构建或部署生产镜像。

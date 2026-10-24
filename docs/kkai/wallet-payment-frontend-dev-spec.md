# 钱包充值订单前端开发规格

**2026-10-23 生效修订：** [本站超时与废单规范](./wallet-payment-local-timeout-spec.md)已获用户确认，统一规定本站 30 分钟期限、本地结束／超时结果及“返回充值”、废单禁止继续支付／人工补单、晚到款继续处理。本文下方与修订冲突的“仅本地超时不能结束、未知永久锁定、30 分钟仅期望、废单仍可人工核实”条款停止适用；其余资金、权限和互斥布局要求保留。

日期：2026-10-23。状态：已确认的开发基线；当前本地实现、验证与渠道限制见 [实施记录](./wallet-payment-implementation.md)。下文“本轮只编写文档”等内容保留为设计阶段记录。

## 1. 依据、优先级与实施范围

产品事实以 [PRD](./wallet-payment-lifecycle-prd.md) 和 [UI 规格](./wallet-payment-ui-spec.md) 的现行章节为准；页面层级与操作位置对照本地原型：

`/Users/tokk/.codex/visualizations/2026/10/23/01a16810-aff1-7911-8ea3-630944f7ae7d/wallet-payment-home.html`

请求路径、请求正文、DTO、错误码和兼容策略只在 [后端开发规格](./wallet-payment-backend-dev-spec.md) 定义；本稿引用这些契约，不能作为另建一套 wire 字段的依据。跨端验收以 [验收规格](./wallet-payment-acceptance-spec.md) 为准。后端契约尚未实现的内容须标为开发依赖，不能以模拟接口或原型状态替代交付。

本稿实施 `web/default` 中的钱包充值与充值记录。最关键的最终决策如下：

- 主区阶段互斥：空闲、恢复中、创建中、当前订单、处理／待核实、结果；订单阶段仅一块订单详情，不并列余额卡、表单或其他购买入口。
- 本站期限为创建后 30 分钟，取已知更早渠道截止；无已确认收款时到期显示“本次充值已超时”，主动结束显示“本次充值已结束”，直接提供“返回充值”，无须等待渠道关闭。用户点击并经服务器精确确认后才恢复表单，不自动跳转；说明原支付页可能仍可付款并提示避免重复支付。
- 钱包始终留在原页；支付使用外部新标签页。关闭支付页不取消订单、不清除当前流程；重开只恢复同一订单的同一会话。
- 当前流程由服务器决定。历史查看不替换当前订单；未结束且未到期的未知、失败重试、处理中不绕过流程；未确认收款可按服务端能力结束／到期后精确返回，已确认收款继续核实。历史待核实又不能无差别锁死账户。
- 不把原型未展示的现有功能视为删除授权，不把原型示例金额、点数、期限或演示开关带入业务。

### 1.1 default / classic 范围决策及源码证据

| 证据 | 本期决策 |
| --- | --- |
| `Dockerfile` 仅安装／构建 `web/default`，拷贝 `web/default/dist`；`router/web-router.go` 的 `ThemeAssets` 和静态服务仅包含 default | 本期完整 UI 落地 default，不双写 classic 的 React 18／Semi Design 界面 |
| `web/default/src/routes/_authenticated/wallet/index.tsx` 为 `/wallet`；`src/routes/console/topup.tsx` 将旧路径重定向到钱包并支持打开记录 | 保留旧路径兼容，扩展并校验订单定位参数，不新建独立订单中心 |
| `web/classic/src/App.jsx` 仍含 `/console/topup`；classic 的 `components/topup/index.jsx` 和 `modals/TopupHistoryModal.jsx` 仍调用旧创建、查询与 `/topup/complete` | classic 不能被当作不存在的客户端。后端须兼容旧协议、统一阶段 guard 与收款规则，旧界面不能绕过当前流程或凭 Pending 补额度 |
| `common/constants.go` 仍保留 theme 路径兼容逻辑 | 不能仅凭残留 theme 值推断本次服务 classic，也不能由本稿断言生产已停用某客户端 |

此范围是本次源码与产品目标下的实施选择，不是生产状态结论。如果实际交付环境仍将 classic 作为受支持的钱包入口，应先在总开发计划中增加 classic 独立适配与验收，再声称这些用户的问题已修复；不能仅修改 default 后扩大完成范围。不得为本次工作修改主题、分支、构建或部署配置。

## 2. 现状与根因对应

以下路径相对于仓库根目录，均为当前源码事实，不是已经完成的改动。

| 现有位置 | 当前行为 | 必要改造 |
| --- | --- | --- |
| `web/default/src/features/wallet/index.tsx` | 一直渲染 `WalletStatsCard`、充值卡及平台会员／订阅模块；付款成功返回布尔值后关闭确认框、刷新余额 | 接入服务端当前流程；主区按单一阶段渲染；“发起支付”与“已到账”分开 |
| `hooks/use-payment.ts`、`use-creem-payment.ts`、`use-waffo-payment.ts` | 异步创建后直接打开 URL／提交表单，缺少持久订单恢复 | 所有余额充值渠道接统一创建、结果和续付控制；保留渠道报价与配置能力 |
| `hooks/use-waffo-pancake-payment.ts` | `window.location.href` 同页跳转；响应含会话相关字段但不用于恢复 | 改为外部标签页；列表、查询和浏览器存储不接收支付凭据；续付只用统一 resume |
| `lib/payment.ts` | 普通支付动态创建 POST form；Safari 不设 `_blank` 而在当前页提交 | 删除钱包余额充值路径上的 Safari 同页例外；保留表单型渠道支持，显式外部目标与受控恢复 |
| `types.ts`、`lib/billing.ts` | `TopupStatus` 只声明 success/pending/expired，未知回退 Pending；状态配置 label 直接展示 | 接受统一 `display_status`／`allowed_actions`，未知明确不可用；状态标签经 i18n，不从原始字符串猜动作 |
| `hooks/use-billing-history.ts` | `useIsAdmin()` 直接决定本人／全平台查询；只有关键词、分页及 complete | 增加显式 scope、六类筛选、精确查询、聚合计数、真实核实；管理员本人钱包不能默认变全平台 |
| `components/dialogs/billing-history-dialog.tsx` | 订单详情缺币种／有效期，额度套当前显示换算，管理员 Pending 可“Complete Order”，另开补单确认弹层 | 使用冻结快照、同卡展开详情／确认，移除无凭据快捷补单，管理入口展示核实责任 |
| `src/routes/_authenticated/wallet/index.tsx` | search 仅有 `show_history`；钱包入口清理时直接 `history.replaceState` | 使用 TanStack Router 校验和清理定位参数；参数只定位，不能解锁或冒充付款结果 |

保留已有报价竞态保护、金额输入与最小金额校验、租户隔离、错误处理、版权和项目标识；不能因为接统一流程而回退这些保护。

## 3. 页面与组件结构

### 3.1 最小组件边界

保留 `SectionPageLayout` 作为页面骨架和“订单记录”二级入口；不要复制原型导航、品牌栏或演示控件。

建议在 wallet feature 内增加一个 `CurrentTopupOrder` 组件和一个 `useWalletPaymentFlow` hook。前者渲染当前订单、行内结束／取消确认、进度和结果；后者拥有服务端当前流程 query 与创建／恢复／结束／取消／核实／返回 mutation。已有 `BillingHistoryDialog` 继续负责记录；复用同一订单字段和状态呈现，只有真正存在两个调用方的订单详情／动作区域才抽成共享子组件。无需增加全局支付 store、通用状态机依赖、弹窗管理框架或新订单中心。

服务端状态使用现有 TanStack Query；表单草稿和行内展开状态用组件局部 state。不同时维护一份本地订单数组、一份 current 布尔值和一份服务器状态。`showForm`、`canResume` 等简单视图值由当前 query 和 action 派生，不能用多组 effect 互相修补。

### 3.2 阶段与主区可见性

| 服务端／本地条件 | 主区内容 | 允许恢复表单的条件 |
| --- | --- | --- |
| 首次恢复请求未完成（最多20秒） | “正在恢复订单”，加载反馈与必要重试 | 无；不能短暂先露出购买表单 |
| 恢复超时、响应无效、网络／鉴权失败 | 恢复失败说明、重试／登录路径；已知订单继续保留 | 无；失败不等于 idle |
| `flow.phase=idle` 且 `can_create=true` | 余额、充值表单和原本允许的空闲模块 | 可确认新报价与提交一次创建 |
| 合法 `idle` 且 `can_create=false` | 空闲余额及按各自配置允许的兑换／订阅等模块；充值购买禁用，显示服务端给出的安全业务原因与适当重试 | 无；充值关闭、额度容量或渠道购买限制不等于恢复失败，不阻断独立业务 |
| 本次确认后或 `creating` | 创建进度；未知时核实说明，不再露表单 | 无已确认收款时可按服务器能力结束／到期后精确返回，不能凭网络失败本地放行 |
| `order` | 冻结订单详情、按能力重开原支付页／结束本次充值／取消／刷新 | 无 |
| `processing`／`review` | 未结束且未到期的进度、按能力结束／核实／客服；已确认收款继续到账核实 | 无；本地结束／超时后由服务器切 result |
| `result` | 对应真实结果、查看记录；可靠终态或本地结束／超时均直接显示“返回充值”主按钮 | 点击 acknowledge，收到新的合法 WalletState 后才切空闲模块；购买仍受 can_create 约束 |
| phase 或动作组合无法识别 | “状态暂不可用”、刷新／客服，保留已知事实 | 无；不上推为待付款或 idle |

`can_create=true` 却不是无活动订单的合法 idle，或 phase、order、action 互相矛盾时才视为契约异常，停止购买并重新恢复，不自行选一个较宽松字段。合法 idle 仍可能被服务端购买规则禁用；前端不根据余额、角色或“没有当前单”自行猜原因／放行。加载、异常是本地渲染状态，不加入后端 phase 枚举。

新 API 能力必须符合总开发计划的兼容门禁。服务端尚未支持 current／quote／统一创建，或返回 404、不可识别响应、HTTP 错误时，不能自动回退到现有无当前锁的 `/pay`、`/stripe/pay`、`/creem/pay` 等旧购买链路。按已经确认的兼容模式展示暂不可用／安全只读及重试，保留可查记录；旧端兼容只由后端统一 guard 承担，不是新前端遇错降级开单的理由。旧创建缺幂等键时后端可返回 `CLIENT_UPGRADE_REQUIRED` 并拒绝渠道请求；不能把“保留旧协议外壳”解释为所有旧客户端仍可无条件购买。

订单卡内顺序固定：标题／完整状态 → 按定价事实标注的订单／应付金额币种与充值额度／付款方式 → 订单号、创建及截止 → 当前说明 → 主次操作 → 行内确认／详情／客服 → 最近检查 → 其他本人未完成订单入口。金额标注遵守第4.1节，不为布局固定成未经确认的“最终应付”。订单号完整可读且可复制，不用长串截断替代详情。

### 3.3 非目标模块保全

| 现有能力 | 当前订单／处理／结果阶段 | 用户返回后的空闲阶段 |
| --- | --- | --- |
| `WalletStatsCard`、独立会员摘要 | 主区不挂载独立卡片；余额可后台更新 | 按既有条件恢复，显示最新余额 |
| `RechargeFormCard` 的自定义金额、预设、普通／Stripe／Waffo／Pancake | 不挂载购买表单，也不能被旧确认框绕过 | 保留最低金额、金额边界、配置和付款方式过滤 |
| Creem 余额充值产品卡／确认 | 同样是本期充值购买入口，隐藏并关闭残留确认 | 保留产品配置，走统一 quote/create/current；不要漏作第二套直跳路径 |
| 兑换码及外部购买兑换码链接 | 不在当前订单主区并列，不删除能力 | 恢复既有 `enable_redemption`、链接校验和兑换反馈 |
| `MembershipWalletSummary`、`SubscriptionPlansCard` | 钱包主区隐藏独立摘要和订阅购买入口，避免破坏“只显示订单”要求 | 保留现有 `tenant_id===0` 等条件；不重写订阅支付业务 |
| 邀请、返佣、转移及合规限制 | 不在订单主区新增入口 | 原能力与权限保留；当前真实入口在 invitations feature，不能因 wallet 目录仍有组件就新增或删除导航 |
| 发票入口 | 记录内保留既有平台租户／权限条件，展示资格依照真实订单和原发票规则 | 不以支付 Pending 或新建成功冒充已收款可开票 |

钱包充值与订阅付款是不同业务订单。本稿覆盖余额充值，包括 Creem 余额产品；订阅方案购买沿用 subscription feature/API/订单语义，不纳入余额充值当前锁或套用余额 OrderDTO。共享渠道 webhook 的后端分流仍须保持。隐藏钱包上的订阅入口不等于关闭其他订阅页，也不授权删除订阅、兑换或邀请逻辑。

## 4. 接口消费与类型边界

以下为调用职责索引；完整路由／DTO 及错误码以 [后端开发规格](./wallet-payment-backend-dev-spec.md) 为唯一权威。`api.ts` 统一使用 `@/lib/api` 的现有实例和项目鉴权／CSRF／错误处理，不另建 fetch 客户端。

| 意图 | 后端接口职责 | 前端要求 |
| --- | --- | --- |
| 进入／跨设备恢复 | `POST /api/user/topup/current/restore` | 页面入口显式调用一次；可传经校验的定位订单号；已存在当前流程时不能被历史替换 |
| 读取当前 | `GET /api/user/topup/current` | 后续只读刷新／短时轮询，不以 restore 充当轮询 |
| 确认新报价 | `POST /api/user/topup/quote` | 冻结付款币种金额、充值额度、渠道及报价期限；金额／产品参数按后端模式，不自行推断 |
| 创建本次单 | `POST /api/user/topup/orders` | `Idempotency-Key` 加 `quote_token`、`expected_flow_revision`；data 为 `{wallet,order}`，order 是原幂等操作结果，wallet 是最新当前流程；正文不再提交“可信”金额或截止 |
| 本人详情 | `GET /api/user/topup/orders/:trade_no` | 用于记录定位；返回 order 与 wallet 的职责分清，不直接拿该 order 覆盖 current |
| 重开原会话 | 同订单 `/resume` | 携带 `{flow_id,flow_revision}`；唯一 checkout 凭据出口；返回 wallet 和可为空的 checkout，必须先合并最新 wallet，再判断是否仍可打开 |
| 取消／核实 | 同订单 `/cancel`、`/reconcile` | 分别携带 `{expected_order_version}`、`{}`；data 为 `{wallet,order}`，按能力与实际结果渲染；202 不代表取消完成或到账 |
| 结束本次充值 | 同订单 `/end` | 携带 `{expected_order_version}`；data 为 `{wallet,order}`，结束本站操作不宣称渠道关闭；历史结束不改变当前另一单，废单无再次结束入口 |
| 返回充值 | `POST /api/user/topup/current/acknowledge` | 携带后端规定的当前流程身份／版本与订单号；可靠终态及本地 ended/timed_out 允许，响应丢失不本地解锁 |
| 本人／全平台记录 | `GET /api/user/topup/self`、`GET /api/user/topup` | 本地本人／管理视图与 wire 范围分清；六类筛选统一显式 `scope=wallet`，`scope=unfinished` 仅用于“其他未完成”入口；status_group、关键词、精确订单号、分页和聚合计数一致 |
| 管理员详情／核实 | 同订单 `/admin`、`/admin-reconcile` | 分别为 GET 与 POST；核实带 `{reason}`，data 返回本人 wallet 与受权目标 AdminOrderDTO；要求 AdminAuth 对应入口及 billing:write，不把目标用户设为本人current |
| 存量报告／批次控制 | `/api/user/topup/reconcile-runs` 专用 report／start／status／pause／resume | 复用管理任务区，仅 RootAuth 交互式 session；输入、版本与 RunDTO 完全按后端第8节，不凭普通单笔核实权限启用批量 |

### 4.1 关键 DTO 消费规则

- `WalletState` 的 `flow.id`、`flow.revision`、phase、order、`can_create`、`create_block_reason_code`、`other_unfinished_count`、`server_time` 是当前流程依据。合法 idle 的禁用原因按 `PAYMENT_DISABLED`、`CHANNEL_UNAVAILABLE`、`QUOTA_CAPACITY_EXCEEDED` 映射可翻译说明；活动流程／可恢复订单依后端相应原因恢复，不自行当作空闲。未知原因使用安全通用禁用说明并允许刷新，不猜测放行。revision 和 order version 按后端字符串契约处理，不能 `Number()` 转换、字典序比较或当时间戳。
- `OrderDTO.display_status`、`status_group`、`allowed_actions` 决定标签、筛选与动作；前端穷尽已知值，未知安全退化。`creating` 归 processing，不可冒充 pending 或已分配责任的 review；ended/timed_out 归 closed，只有后端允许 acknowledge 才显示可执行的“返回充值”；不按客户端 created_at 自行解锁。local_ended_at/local_end_reason 为本地操作事实，不能等同 closed_at/close_reason 的渠道证据。
- `expected_payment`／`payable_payment`／`received_payment` 使用 `amount_minor` 字符串、currency、exponent，充值使用 `credited_quota`／`credit_display`。统一在 wallet 的金额格式化边界处理精度；不得把 int64 先转 JS number 再格式化。可以复用 `Intl.NumberFormat`，但必须证明转换安全；大整数用字符串／BigInt 分解，不引入新的十进制依赖。
- 报价与订单统一按 `pricing_mode` 显示：fixed 的 expected_payment=payable_payment，标签“应付金额”；channel_adjusted 在 payable_payment=null 时以 expected_payment 显示“订单金额”，将 `pricing_notice_code=CHANNEL_ADJUSTMENTS_APPLY` 翻译为“最终金额以支付页合法优惠、税费为准”。渠道可信确认 payable_payment 后才显示“应付金额”，不能覆盖冻结基础快照；received_payment 始终独立标“已收款”。unknown 仅用于缺证据旧单，显示待核实，不允许拿它确认新购买。上述规则应用到空闲支付明细、当前卡、历史卡、取消／续付确认，不能按 provider 猜标签或把基础值写成“最终应付”。
- 订单／应付金额、已收款、充值额度分别显示。不能再以 payment_method 猜币种，不能将历史充值额度套用当前汇率或站点显示配置重建订单快照。缺失字段保留“正在核实／未取得”，不补造值；旧展示字段严格按后端 legacy 语义单独标注。
- `deadline_at` 为空不伪造倒计时；有可信 deadline 才相对 server_time 展示剩余时间，时间均按后端秒单位处理。本地倒计时只停止入口并读取服务端当前状态，不自行写过期终态或对废单发起人工核实。
- 列表、current、普通详情、query cache、URL 和日志中不放 checkout／token。checkout 仅在用户发起 resume 的短暂内存中使用；不回写全局可持久 query、不存 localStorage／sessionStorage，不让客服复制包含凭据的页面 URL。
- 服务端 reason code 映射成可翻译业务说明，不向普通用户展示 SQL、内部租约、堆栈、原始提供商错误或密钥；使用项目错误处理同时保留订单卡内可见反馈。

restore／current／acknowledge 的 data 直接为 WalletState；orders／cancel／end／reconcile 的 data 是 `{wallet,order}`，必须分别更新当前流程和被操作记录，不能把历史 order 当 current。orders 重放已 acknowledge 的旧操作或已出现另一当前流程时，order 仍指原操作，不能复活／重绑它，也不能误开 wallet 内另一笔当前单的支付页。admin-reconcile 的 wallet 仍是管理员本人，order 才是受权目标；详情 timeline 使用后端定义的事件／时间／summary_code，在界面翻译，不新增第二套事件字符串。所有错误若带当前状态，统一从 `data.wallet` 合并；错误正文不得提取或展示 checkout。

### 4.2 查询缓存与身份隔离

当前流程、详情及历史 query key 必须包含当前 host／tenant、user 与现有认证 session 标识；列表另包含 scope、状态、精确定位、关键词、页码、页大小。不能用一个 `['topup']` key 在不同用户／租户／管理范围复用。

沿用现有 auth/query-client 清理机制；登出、换用户、换 host 或会话身份变化时终止／隔离旧请求、清理局部确认和 checkout 引用。当前恢复、配置和本人余额可以并行读取；仅当前恢复成功决定是否可展示表单，不等待历史全量查询，也不因配置先返回提前允许购买。记录弹层关闭时停止该列表查询。

## 5. 各条用户流程

### 5.1 空闲报价、创建与创建不确定

1. 服务器恢复为合法 idle 后恢复空闲模块；仅 `can_create=true` 时允许充值购买，false 时依据后端原因禁用，不连带停用独立配置允许的兑换／订阅。自定义额度必须保留安全整数、正值、渠道最低额和服务端上限校验；输入草稿与提交值分离，输入后首次点击购买应立即有效。
2. 预设／金额／付款方式／Creem 产品变化立即使旧报价无效。新 quote 按输入模式发送正整数字符串或产品 ID；支付明细直接展示同一报价的金额、币种和额度，付款方式与选择保持一致，金额标签及变量说明严格按 pricing_mode／payable_payment／pricing_notice_code。报价异步旧响应不能覆盖新输入；Stripe优惠／Pancake税费等 channel_adjusted 且尚无可信最终应付时明确显示基础“订单金额”与规则提示，不能标注未经确定的最终应付。
3. 点击“核对并支付”直接调用统一创建流程，进入创建中并隐藏空闲模块，不先设置确认状态或展开第二个创建按钮。提交前仍校验 can_create、报价与输入一致、非报价请求中及报价未到期；失效则重新报价，不沿用旧报价或刷新后自动下单。Creem 先选择产品查看报价，再单击支付按钮，选择产品不创建订单。同一提交生成一个幂等 key，重复点击和网络重试复用；不得自动重新生成 key，不以超时回到可购买表单。返回后真正新购买才生成新 key。
4. create 返回 `{wallet,order}`，先采用最新 wallet，并仅当本次返回 order 正是该当前单且允许 resume 时再取入口；create 本身不提供 checkout，也不证明付款页已打开。旧幂等 key 的重放不能复活已结束的旧 flow；若钱包已有另一当前单，不因旧操作响应自动打开那笔新单。创建期间服务端若返回已有流程冲突，恢复真实当前流程，不弹出第二笔购买入口。
5. 远端结果不确定时本地单已存在，显示创建／支付结果核实并按原订单 reconcile。若连本地提交响应丢失，先用同 key 重试或 current/restore；数据库／网络不可用不等于未创建。不能无界重试创建；余额不因 create／打开页面而乐观增加。
6. 可靠确认未形成订单／会话时显示失败结果及合法返回路径，不能把所有 HTTP 非 2xx 都当作安全未创建。创建幂等恢复不能依赖 checkout token 在浏览器持久化。

### 5.2 外部新标签页、拦截与原会话重开

所有余额充值渠道执行同一原则，包括普通 POST form、Stripe、Creem、Waffo、Pancake；不得保留 Pancake 或 Safari 在钱包内 `_self` 的例外。

- 在“核对并支付”或“重新打开支付页面”的直接用户事件中，可同步预开由本页控制的空白占位标签页；创建／resume 完成后仅向该占位目标导向经过校验的 URL 或提交已验证的 POST form。使用不可预测的单次 target，隔离外部页 opener；只在导航前操作自己的同源占位，导航后不读第三方内容。
- URL／form action 只允许后端契约支持的绝对 HTTP(S) 目标，生产策略依照后端渠道配置；禁止 javascript/data 或将任意后端字符串塞进 innerHTML。form fields 通过 DOM 属性构建，提交后清理。不得把所有 http 链接假设为可信服务商。
- 预开失败、返回 null、导航／form 提交失败或无法证明打开结果时，钱包仍保留原订单并显示“订单已创建。如支付页面未打开，请点击‘重新打开支付页面’。”提供直接手动入口。不能只显示“跳转成功”toast，不自动切当前钱包页。
- `window.open` 配合 noopener 可能拿不到句柄，不能仅据此断言被拦；具体占位及 opener 隔离实现需 Chromium 与 Safari 浏览器验证。无法安全预开时允许保留原订单并让用户手动打开，不以关闭标签页或再次新建绕过拦截。
- resume 失败、到期或竞态产生 checkout=null 时使用最新 wallet 状态：可靠结果显示返回按钮，处理中继续核实。若预开的仍是本页空白占位，可以安全收起；不操作已导航的第三方页，不根据其是否关闭释放订单。
- 重开不重新报价、不延长期限、不重建会话，不调用旧 create API。直接主按钮已展示本单冻结信息，无需再进记录／二层弹窗；记录里的“继续支付”按 PRD 在原卡确认冻结信息，且先由 restore 原子绑定合法对象，已有其他当前流程则保留原流程并提示。

支付标签页可能有多个浏览器实例，但只能指向同一可付款会话。防止同一操作并发发出 resume；不能承诺识别第三方标签页关闭、自动关闭该页或从其 DOM 读取支付成功。

### 5.3 取消、到期、成功和返回充值

| 场景 | UI 行为 |
| --- | --- |
| 用户取消且允许 cancel | 本订单卡内展开订单号、按定价事实标注的金额与取消不等于退款说明；确认／返回订单；不另开嵌套弹窗 |
| cancel 请求已提交 | 停本单续付，显示“取消处理中”，重复操作返回同一进度；不先显示取消成功 |
| 渠道不支持 cancel | 不出现假取消按钮；有效期限内保留结束本次充值及按能力核实／客服，仍可付时恢复原会话；本地结束不会关闭原支付页 |
| 到达服务端截止且无已确认收款 | 停续付并刷新服务端；收到 timed_out/result 即显示“本次充值已超时”及返回充值，不等待后台扫描或渠道关单 |
| 用户明确结束且允许 end | 结束后显示“本次充值已结束”及返回充值；说明原支付页可能仍可付款、已付款继续核实，提示关闭旧页及避免重复支付 |
| 可靠确认未付款且原会话已关闭／到期 | 保留真实 cancelled/expired 及渠道关闭说明；显示返回充值、原订单及查看记录，不与本站 timed_out/ended 混同，不自动恢复表单 |
| 已付款／收款未入账 | 显示真实到账或到账核实；明确请勿重复付款，不能被到期／取消旧响应覆盖 |
| success、主动 cancelled 或可靠 failed | 保留相应结果和“返回充值”，返回前仍不展示购买入口 |
| 未结束且未到期的未知、查询失败、关单中或 review | 保留处理与帮助，按服务端能力可结束；无直接购买捷径；credit_review 不因年龄或本地结束变废单 |
| 点击“返回充值” | 调 acknowledge，按钮等待中；服务器确认并返回合法 idle 后恢复空闲模块，按 can_create 决定购买及新报价是否可用；旧记录保留，不自动下单 |
| acknowledge 超时／冲突 | 仍保留结果，重新获取服务器当前状态；不能先 `setHideCurrent(true)` 乐观放行，也不能用历史定位替代 acknowledge |

“返回充值”是结果主操作：使用现有主色 Button，直接放在说明下的操作区，移动端允许整行，不能仅为导航小字、仅在记录里出现、被刷新按钮替代或必须 hover 才看到。本地结束／超时、可靠渠道关闭、成功、可靠失败的返回动作相同，文案按结果区分。

成功后从服务端同步余额、current、本人历史和计数；管理员操作也只刷新对应查询范围。禁止将本地金额直接加余额。晚到付款更新订单和资金事实；若用户已创建另一笔当前单，历史付款只能刷新历史／余额并给可读提醒，不能把钱包主区切回旧单或覆盖新单。

## 6. 订单记录、全平台管理与存量

本人钱包“订单记录”始终以 self scope 打开，即使登录者是管理员也如此。全平台从明确的管理入口进入，标题“全平台充值订单”；新增入口位置沿用管理导航权限模式，不给普通用户角色切换开关。共享同一结构，通过显式 scope 和服务端权限区分数据，不能继续让 `useIsAdmin()` 自动将本人列表替换为全平台。

记录顺序为标题／范围／关闭 → 全部、待付款、处理中、待核实、已完成、已关闭六类筛选 → 订单号搜索／刷新 → 分页卡片及行内详情 → 分页／结果数量。桌面约 940px，手机全屏，只有一层记录弹层；完整列表功能不能因主页原型只演示少量记录而省略。

- 状态筛选、总数、各类计数和列表来自同一 scope／查询；不能拿前端当前页数量当总数，不在混币种数据上叠加出无意义裸总额。已有今日汇总按后端可信口径保留为记录内次要信息，不能冒充待付款数或扩大主区。
- 搜索草稿使用现有 debounce；切状态／关键词／scope／页大小重置页码，旧请求丢弃。六个筛选全部显式使用 `scope=wallet`，集合固定为“最近30天所有订单 ∪ 全部账龄尚无成功或可靠渠道终态的历史记录”，含未完成／未定性及 ended/timed_out 废单，范围提示不得把废单称为未完成；列表与各类 counts 都在此集合计算，不能切 tab 同时换时间范围。`scope=unfinished` 仅用于“其他未完成”特定入口，不作为六 tab 的隐含映射。精确本人订单号走 exact_trade_no 且与 keyword 互斥，可查全部账龄；权限、限流和分页仍由后端校验。无结果、加载失败、空历史分别展示，不用空列表掩盖失败。
- 卡片展示订单号、状态、创建、冻结充值额度、按第4.1节定价事实标注的付款币种金额、付款方式和可用动作；详情展开截止／完成／关闭、时间线、可信收款事实、最近核实与业务原因。缺历史快照不按当前汇率补造。
- 查看历史只展开该记录；关闭记录回到原钱包阶段。存在 current 时不允许选历史单替换；无 current 且确为可处理旧单时，用户续付须经 restore guard 确认，再显示该当前单。历史 review 保留记录与管理员责任，不自动绑定。
- 废单仍保留只读详情、证据及客服，移出其他未完成计数；列表、详情、钱包、管理页及历史批次无续付／恢复／再取消／人工核实／补单入口，旧 API 同样拒绝。创建时间未知且无收款事实则 unavailable，不伪造期限或动作。
- 记录内结束／取消／核实遵守同一能力规则，且不解除其他当前流程；操作使记录离开筛选时同步计数并说明去向，提供定位入口，保持键盘焦点。
- 用户联系客服只展示订单编号、所需付款凭据和已有真实客服入口；不自动发送消息，不让用户分享 token；本地结束须调用 end 再精确 acknowledge，不能由前端“一键归档”代替。
- 管理卡显示必要用户信息与 scope；“核实支付”展示原快照、可信证据、金额／币种差异、最近核实、负责人、业务原因和操作记录。缺证据不显示“补单成功”，不能保留 Pending→直接发额度按钮。
- 管理员核实重复点击须单订单等待／禁用，真实权限与幂等仍在服务器；收款已验证而入账失败继续显示 credit_review，不能伪装为 success。无凭据人工额度调整属于独立功能和审计，不塞入本记录快捷操作。
- 存量任务界面复用既有任务面板，显示只读报告／批次处理进度、已到账／关闭／待核实／重试计数，并交付已授权任务的暂停／续跑与原因记录。暂停／续跑是必需能力，按后端固化的最窄任务控制接口接入；未实现应列阻塞，不能以“现有框架没有”省略或用假进度交付。历史批量处理独立授权，不随前端发布自动运行。

### 6.1 历史批次任务的最小交互

在现有管理员任务区接入后端第8节专用 Run 契约，不新建通用任务平台。仅平台 RootAuth 的交互式 session 可进入；普通管理员的 billing:write、租户管理权限与 scoped token 均不能代替该权限。

1. 报告、选单与执行前均排除本地已结束／超时废单，不提供人工历史核实通道；已保存可信收款的补偿独立继续。填写显式渠道、原商户／环境、租户和创建时间范围，以及批量／速率限制，生成 report；空范围不能表示无界全量。报告展示冻结范围、max_order_id、counts、capability_gaps 与过期时间，不能标为已修改订单。
2. start 须引用现有报告的 report_id、scope_digest、真实独立的数据处理 authorization_ref 与原因，同次提交复用 Idempotency-Key。设计确认、上线授权、一个任意填写的引用字符串均不证明历史处理已获授权；授权记录尚未接入时，按运行册由获授权操作者人工核对引用及范围，界面不自动生成批准。
3. status 使用 RunDTO，明确展示 report_only／queued／running／pausing／paused／completed／failed；复用任务表只承载执行关联，不将 SystemTask succeeded 显示成整个批次已完成。counts 依后端 reviewed／credited／closed／review_required／retry_failed 口径标注，不拿当前页条数或动画进度充当真实结果。
4. pause 携带 expected_control_version 和原因，先显示 pausing，服务器确认释放在途执行后才显示 paused；不乐观显示“已暂停”。暂停历史任务仍可能保存其在途可信收款事实，且不暂停正常订单回执与收据入账，界面说明此真实边界。
5. resume 携带最新 expected_control_version、原 scope_digest 和独立 authorization_ref，仅从原冻结范围／游标继续；范围变化重新 report 与授权。409 RUN_CHANGED 刷新真实状态，不用旧版本循环覆盖。按钮等待、错误、刷新／卸载后恢复均以服务器 run 为准，不自动 start/resume，不从浏览器持有唯一游标。

未获历史业务处理授权时只能停在安全报告／只读状态；不因文档、前端发布或任务界面可见而实际处理旧订单。

## 7. 刷新、轮询与竞态约束

一个当前流程 hook 统一接收 focus、visibility、回跳定位、刷新按钮和 mutation 后刷新；同一实例不让当前卡、历史每行和页面各自注册监听／计时器。事件只触发服务端核实，不形成支付证明。URL 的 success／cancel 参数不更改资金状态。

可见的当前处理订单短时每 5 秒查询、单个等待窗口最多 2 分钟；终态、页面隐藏、卸载、身份变化或窗口结束停止，仍留手动刷新／客服。不要对所有历史订单轮询；不要每次请求结束无条件重置两分钟窗口。页面重新可见可启动一次新的有界核实；频率服从后端限流／重试指示。普通 GET current 不代替必要的 reconcile，也不能每次焦点都无界调用远端渠道。

当前状态按 flow 身份、revision 和订单 version 的后端契约合并；经格式验证的十进制版本可用 BigInt 比较，不能借 JS number 或字典排序丢精度。mutation 前取消／隔离同对象旧读请求，mutation 返回后再统一更新／失效相关 query；旧响应不复活续付、不让 success 退成 pending、不让旧 acknowledge 清掉新 flow。跨 tab 竞争以服务器 409／最新 WalletState 收敛，不依赖 localStorage 锁；如已有同源同步机制可用于提示刷新，广播内容不能携带支付凭据或被视作权威。

POST create/cancel/end/ack 不在 mount effect 或响应 effect 内自动触发；放明确用户事件。查询可用 AbortSignal 和现有去重，不能把“取消客户端请求”解释为“服务器取消订单”。mutation 禁止无条件自动重试，按幂等契约／错误类型选择原操作重试；401/403、参数错误、流程冲突和未知网络结果分别处理，不能统一重新下单。

## 8. 视觉、可访问性与国际化

### 8.1 组件与样式复用

`web/default/components.json` 为 base-nova；实际 Button import 为 `@base-ui/react/button`。复用 `@/components/dialog`、`ui/dialog`、Button、Card、Alert、Skeleton、StatusBadge／Badge、Field、InputGroup、Tabs／ToggleGroup、Select、Pagination、Separator 与现有复制 hook。使用 Base UI `render` 合成，不能复制 Radix `asChild` API，不能在 Button 内再套 button。新增状态变体优先在 wallet 局部组合，除非确需共享，不改全站基础组件行为。

使用语义 CSS token 与 Tailwind 移动优先；不复制 HTML 原型大段内联样式、innerHTML、全局 scene 控制、演示余额或本地订单状态。保持原型中中性主按钮、细边框、状态文字／圆点、金额层级与行内确认位置。设计图仅参照既有互斥布局；现行行为以 UI 规格与本站超时修订为准。UI 规格中的前版截图／验证记录不证明本地超时、结束或废单门禁通过。

### 8.2 可访问性与响应式验收要求

- 主区用语义 section 与状态标题，订单字段用清楚标签；状态颜色必须带文字。处理中给 `aria-busy` 和节制的 live status，计时每秒变化不逐秒读屏。
- 取消、续付确认在当前订单内展开，`aria-expanded`／`aria-controls`、确认标题和返回路径齐全；无需为取消再叠 AlertDialog。新单在表单支付明细内核对，单击“核对并支付”直接提交，不增加行内二次确认；直接操作不能让第一次点击只更新草稿而丢失提交。
- 记录弹层沿现有可访问 Dialog，保留 Title／Description、焦点锁定、Escape／关闭、触发点焦点恢复、背景滚动管理；手机全屏也不能堆第二层详情／确认弹层。
- 创建后焦点转到订单标题／主动作；取消提交转进度；结果转说明／可用主动作；返回充值后转表单标题或额度输入。列表项离开筛选后焦点有明确落点，刷新不反复抢焦点。
- 图标按钮有翻译后的 aria-label，装饰图标隐藏；复制与网络错误均有可读反馈。键盘 Tab／Shift+Tab／Enter／Space 可操作；不只靠 toast 承载唯一资金事实。
- 最窄 320px、375px、736px、1024px 内容宽度、手机浏览器动态高度及粗指针均验收；订单号可断行，状态／付款方式／法俄越长文案可换行，金额不溢出。主按钮不被裁切或只藏在折叠区；触控目标约 44px、输入文字至少 16px。
- 浅色、深色、跟随系统和减少动态效果；正文对比度满足 WCAG 2.1 AA。不得通过删文案、截断关键币种或缩小触控目标掩盖布局问题。

### 8.3 全语言交付

实施前读取 `.agents/skills/i18n-translate/SKILL.md`，沿用英文源文案作为 flat key；此项目根规则与实际locale格式优先于旧前端规范中层级key的例子。当前 `src/i18n/config.ts` 实际支持 **en、zhCN、fr、ru、ja、vi、zhTW**，对应 `en.json`、`zh.json`、`fr.json`、`ru.json`、`ja.json`、`vi.json`、`zh-TW.json`；不能只按旧技能说明的六语言漏掉繁体中文。

所有新改文本都纳入范围：页面／弹层标题、六类筛选、完整状态、按钮、行内确认、取消与超时说明、手动开页提示、报价失效、恢复／网络错误、无记录／无结果、客服说明、管理员核实／证据／进度、分页／计数、时间单位、复制与 aria-label。组件自行使用 `useTranslation()`，常量只存可扫描的 labelKey 并在渲染时翻译；必要静态 key 登记 `src/i18n/static-keys.ts`。不要重复现有状态 label 未经 t 的问题。

本稿不添加 locale key 或翻译。后续实现的 locale 写入必须通过 skill 指定的 `add-missing-keys.mjs` 方式并覆盖上述运行语言，随后 `bun run i18n:sync`；不直接编辑 JSON。`scripts/check-i18n.mjs` 当前只强制 en／zh，故单独通过 `i18n:check` 不能证明七语言完成；还须用钱包范围的翻译契约测试检查七语言 key、占位符和关键文案均存在，并人工查看长语言布局。保留现有受保护标识和产品名。

## 9. 按文件实施计划

以下为开发时的有界改动清单，不意味着本轮修改这些文件。以现有文件承担既有职责，新增文件只为稳定组件／hook 或明确行为测试；不要把一次任务铺成通用框架。

| 文件／位置 | 计划变更 | 必须保留／对应验收 |
| --- | --- | --- |
| `web/default/src/features/wallet/types.ts`、`api.ts` | 接后端 DTO／接口，现有旧 response 仅用于兼容，明确当前、checkout、self/admin作用域；传递 signal 与幂等信息 | 不发送可信金额／期限；checkout 不混入列表；A02/A03/A07/A26 |
| `hooks/use-wallet-payment-flow.ts`（新增） | current restore/read 与统一 create/end/cancel/reconcile/ack mutation、有限轮询、身份／响应隔离 | 不本地解锁；未知创建可恢复；A03/A08/A09/A18/A21/A26/A29/A32 |
| `components/current-topup-order.tsx`（新增） | 当前详情、行内结束／取消、进度／核实、本地超时结果、显眼返回按钮 | 对照原型 orderCard 布局；A01/A05/A09/A14/A22/A23/A27/A28 |
| `index.tsx` | 接阶段容器、本人记录入口；非idle隐藏独立卡及所有购买模块；余额统一刷新 | 不删兑换／订阅／租户条件；当前优先；A15/A19/A24/A27/A30 |
| `lifecycle-wallet-page.tsx`、`components/recharge-form-card.tsx`、`components/creem-products-section.tsx` | 接冻结 quote，支付明细核对后单击直接创建、创建中退场；Creem纳入同一guard，不增加新单二次确认 | 保留金额边界、输入首次购买、预设／最低额与配置；不残留隐藏弹窗可触发创建 |
| `hooks/use-payment.ts`、`use-creem-payment.ts`、`use-waffo-payment.ts`、`use-waffo-pancake-payment.ts`、`lib/payment.ts` | 将余额充值创建／开页责任收敛到统一流程，复用必要报价／目标提交逻辑；删除已替代的同页跳转和布尔成功语义 | 不影响订阅调用方；实现前 rg 全部调用者；A01/A04/A19/A20 |
| `hooks/use-billing-history.ts`、`components/dialogs/billing-history-dialog.tsx` | 显式scope、筛选／精确搜索／分页计数、同卡详情确认、核实支付、管理证据与任务进度 | 保留可信今日汇总／发票条件；无无凭据补单；A02/A07/A12/A16/A24/A30 |
| `lib/billing.ts`、`lib/format.ts`、必要 `constants.ts` | 统一展示状态／分组／labelKey、未知fallback、冻结Money与额度格式 | 不按渠道猜币种、不拿当前汇率改历史；A13与金额验收 |
| `routes/_authenticated/wallet/index.tsx`、`routes/console/topup.tsx` | Zod校验定位／范围，保留旧路径和 show_history；参数经路由清理 | 定位不提供权限、不覆盖current；回跳不等于支付凭据 |
| `src/hooks/use-sidebar-data.ts` 及既有管理导航／权限入口 | 加显式全平台记录入口，使用 `src/lib/admin-permissions.ts` 对应能力，复用记录结构；不单凭 `useIsAdmin()` 取代精细权限 | 管理员本人wallet仍self；不新增普通用户切角色开关 |
| `features/system-info/api.ts`、`types.ts`、`components/system-tasks-panel.tsx`／`system-tasks-table.tsx`，必要时新增业务组件 `wallet-topup-reconcile-run.tsx` | 复用任务区，最小接入RootAuth报告／启动／状态／暂停／续跑及RunDTO；钱包单笔AdminAuth与批次RootAuth分开；缺接口列交付阻塞，不省略需求 | 不扩通用任务控制框架，不把生产存量处理并入前端发布；不伪造授权引用或把单段task成功当run完成 |
| `src/i18n/static-keys.ts`、运行配置中的七种locale | 仅按i18n skill补齐本次UI key、占位符与状态 | 不手写JSON、不漏zh-TW、不批量改无关旧文案 |
| `hooks/index.ts`、`lib/index.ts` | 只导出需要的新接口、移除实际失效引用 | 不顺带做无关barrel/全站重构 |

`wallet-stats-card.tsx`、订阅／邀请／转移组件原则上不改内部业务，只在钱包入口调整可见阶段；如发现共享调用必须改，先列出具体回归用例并在总计划记录，不借此扩大需求。

## 10. 需求到 UI 与测试的对应

| PRD 验收 | 页面／契约落点 | 最小有效验证 |
| --- | --- | --- |
| A01/A04/A19/A20/A28 | 单击支付即创建，无二次确认；创建后仅当前单，外部标签／POST form／被拦手动入口 | 组件mock创建与resume，覆盖Creem先选产品再单击支付、旧报价禁提交与失效刷新不自动创建；真实浏览器Chromium+Safari验证新tab、钱包不跳离，关闭页后原单仍在 |
| A03/A27 | quote变更失效、同key创建重试、创建未知锁定 | 可控deferred响应测试；同次重复点击与网络超时不二次建单，输入后首次购买有效 |
| A05/A06/A07/A22/A25 | 行内取消、能力限制及资金竞争 | 取消先processing；mock晚到paid后UI不退回cancelled；无取消渠道不出现假按钮 |
| A08/A09/A14/A23/A31/A32 | 本地结束／超时、可靠结果与明确返回 | 未确认收款的未知单可按能力结束／到期；结果保留主按钮，精确 ack 才现表单，失败不解锁；废单无人工入口，paid 优先且 A 晚款不覆盖 B |
| A02/A12/A24/A26/A30 | 服务端restore、self/admin、旧单精确定位及历史隔离 | 换scope／身份及乱序响应；历史展开不改current；旧review不误锁，跨设备以接口契约验收 |
| A10/A11/A15/A18/A21/A29 | 回跳／可见性核实、有限轮询、到账核实 | URL成功参数不入账；隐藏／卸载／终态停止；两分钟停止；旧current响应不覆盖新flow；review无购买捷径 |
| A13 | 全状态、未知fallback、冻结Money及定价模式 | 表驱动状态映射；failed/unavailable不回退Pending；fixed与channel_adjusted金额标签、基础／最终／实收分离；大整数／不同exponent／缺币种／缺期限不造值 |
| A16 | 全平台核实和真实证据 | 普通用户无管理入口；重复核实禁并发且不显示无凭据成功；真实幂等入账由后端验收 |
| A17 | 响应式／键盘／焦点／多语言 | 320/375/736/1024px、浅深色、长语言，读屏状态反馈，单层记录焦点锁定／返回焦点 |
| 存量批次控制 | RootAuth、报告／启动／状态／暂停／续跑 | 报告不处理订单且排除废单，执行前再检查；范围与授权引用冻结；pausing不冒充paused；RUN_CHANGED刷新；SystemTask succeeded不等于run completed；普通AdminAuth无批量入口 |
| 现有功能回归 | tenant、兑换、Creem、订阅、邀请及发票 | 更新tenant-wallet既有测试，空闲恢复原允许模块，订单阶段全部隐藏；合法idle+can_create=false仅禁在线充值；不改订阅支付与回调分流 |

建议新增 `wallet-payment-flow.test.tsx`（用户阶段／动作集成）、`hooks/use-wallet-payment-flow.test.tsx`（请求与竞态）、`wallet-payment-i18n.test.ts`（包含本期任务文案的七语言契约），以及 `features/system-info/__tests__/wallet-topup-reconcile-run.test.tsx`（报告与批次业务状态／控制）。沿用并更新钱包的 `hooks/use-payment.test.ts`、`hooks/use-billing-history.test.tsx`、`lib/payment.test.ts`、`lib/format.test.ts`、`tenant-wallet.test.tsx` 和现有 `features/system-info/__tests__/system-tasks-panel.test.tsx`。只为真实行为加例，不重复测试所有私有函数或用超大snapshot固定组件树。

Vitest 使用显式 QueryClient 和认证／tenant fixture，RTL优先角色／可访问名称查询；轮询用假时钟、竞争用可控Promise，不用真实sleep／随机压力／仅日志断言。checkout安全与浏览器拦截须另有真实浏览器检查，mock window.open通过不代表Safari已通过；前端测试不能宣称资金幂等、真实渠道关单或数据库兼容完成。

## 11. 开发验证命令与边界

本轮为 L0 文档任务，仅检查本文件差异与文档一致性，不运行应用测试、typecheck、构建、i18n同步或生产操作。以下是**实现后**的范围建议，不是本轮已执行结果。

从 `web/default/` 执行受影响测试（新增测试文件落盘后）：

```sh
bun run test -- src/features/wallet/wallet-payment-flow.test.tsx src/features/wallet/hooks/use-wallet-payment-flow.test.tsx src/features/wallet/wallet-payment-i18n.test.ts src/features/wallet/hooks/use-payment.test.ts src/features/wallet/hooks/use-billing-history.test.tsx src/features/wallet/lib/payment.test.ts src/features/wallet/lib/format.test.ts src/features/wallet/tenant-wallet.test.tsx src/features/system-info/__tests__/wallet-topup-reconcile-run.test.tsx src/features/system-info/__tests__/system-tasks-panel.test.tsx
bunx --no-install oxlint -c .oxlintrc.json src/features/wallet src/features/system-info src/routes/_authenticated/wallet/index.tsx src/routes/console/topup.tsx src/hooks/use-sidebar-data.ts src/i18n/static-keys.ts
```

若实现未改某个现有文件且其输入契约也未变化，不为安心重跑其已通过测试；新增共享format／http／基础组件行为才扩大到直接调用方。测试文件命名变动时同步修改以上命令，不能保留指向不存在文件的验证记录。

格式／类型检查必须遵循仓库受保护头部和成本规则：现有 `bun run format:check` wrapper 只支持整个前端范围，`bun run typecheck` 是整个 TS project。不能把它们伪称 scoped，也不能用裸tsc逐文件绕过项目配置。实施者先在总开发计划说明确切命令、必要性与预计耗时；如触及全量检查或耗时未知，按全局规则取得明确授权后执行。没有目标类型检查时如实记录缺口，不能用完整构建偷偷代替。前端全套测试、构建和生产发布均不由本文授权。

后续翻译脚本及 `bun run i18n:sync` 仅在真实locale改动时按skill执行，并检查是否产生无关重写；七语言钱包测试独立覆盖。提交验收记录应列出实际命令／退出码、浏览器与尺寸、未验证的真实渠道／资金并发／跨设备能力；未完成项不得因设计稿通过而标完成。

## 12. 本轮文档验证记录

已只读核对现有default／classic源码、现行PRD／UI和原型，并与后端第4／6／8节交叉核对API／DTO／状态／错误、报价金额口径、合法购买禁用和RootAuth历史批次控制；验收负责人确认当前阶段、ack与模块保全边界无冲突。实际验证命令：`git diff --no-index --check /dev/null docs/kkai/wallet-payment-frontend-dev-spec.md`，无空白错误输出（未跟踪新文件存在差异时退出码1）。文档引用链接由总开发入口统一检查。

本轮只新增本文件，未修改旧设计、原型、业务代码、翻译、数据库、分支或生产数据；未运行应用测试、全套测试、lint、typecheck、构建或i18n同步。真实渠道、新标签页／Safari、服务端恢复、资金并发和应用代码质量检查均属于后续实施验证，本文不声称已完成。

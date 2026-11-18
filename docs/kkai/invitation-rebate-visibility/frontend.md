# 工单 #28：默认前端、交互与国际化开发规格

**状态：待实现、待联调。** 依据[产品需求 A01–A11](../invitation-rebate-visibility-prd.md)；角色分工与发布顺序见[开发手册](./README.md)。本文只规定 `web/default` 的实现和自测，不代表代码、翻译或线上页面已变更。

**两处展示，两个数据来源：** 打开 `/invitations`，在分享前看到「现在好友充值返多少、按什么算」；打开「返利记录」，在金额旁看到「这一笔记的比例」。前者只读登录人的 `my-code`，后者只读这一行 `rebate-records`。无邀请、无记录也能显示当前规则；切换账号、刷新失败、服务回退时不能用旧数字占位。

## 1. 按文件实施

下表路径以 `web/default/src/` 为基准。「现状」是源码行为，「拟改」是开发任务。

| 文件 | 现状 | 拟改或保留 |
| --- | --- | --- |
| [features/invitations/index.tsx](../../../web/default/src/features/invitations/index.tsx) | 标题、刷新、Tabs 都在此；普通默认 `invite`，已登记销售默认 `customers`，不允许的标签回落 `invite`。 | 由页面持有唯一的严格型 `my-code` 查询。在 `SectionPageLayout.Content` 开头、`Tabs` **之前**放规则摘要，把同一查询结果传入 `InvitationSummary`。首屏和重试不要求先激活「我的邀请」。保留 URL、标题、标签顺序及深链。 |
| [types/user.ts](../../../web/default/src/features/invitations/types/user.ts)、[api/user.ts](../../../web/default/src/features/invitations/api/user.ts) | `InvitationStats` 没有规则字段；`getMyInvitationCode(config?)` 已支持传请求配置；`RebateRecord.rebateRatio?: number \| null` 已有。 | 只给 `InvitationStats` 增加可选 `topupRebatePolicy`：`available` 带数值 `ratio`，`not_configured / disabled / unavailable` 不带比例。网络输入按第 3 节校验，不能只靠 TS 断言。复用现有 GET、`requireInvitationData` 和记录分页；不另开请求，不上传 `userId` 供服务器选人。 |
| [hooks/use-invitation-feature-status.ts](../../../web/default/src/features/invitations/hooks/use-invitation-feature-status.ts)、[status.ts](../../../web/default/src/features/invitations/status.ts) | `status`、按账号的 `sales-profile` 共同决定访问权。归一化用 `=== true`，不能从其默认 `false` 反证服务器明确关闭。 | 保留 `deriveInvitationAccess`、错误、重试和导航行为。给**展示层**保留成功 `/status` 原响应中 `orderRebateEnabled` 的三态证据（`true / false / unknown`），在同一次查询内派生；不另取状态，不把未知改成授权。 |
| [components/user/invitation-summary.tsx](../../../web/default/src/features/invitations/components/user/invitation-summary.tsx) | 自己查询 `my-code`，持有二维码对话框、分享链接、复制、人数与返利汇总。 | 移除组件内的第二次查询，消费页面传下来的查询结果。原骨架、整份摘要错误与 `refetch`、两个复制按钮、`QRCodeSVG` 和 `showRebates` 条件保留。策略缺失不是摘要错误。 |
| [features/kkai-wallet/invitation-rewards-card.tsx](../../../web/default/src/features/kkai-wallet/invitation-rewards-card.tsx) | 与邀请页**同键、不同查询语义**，详见第 2 节。 | 仅隔离钱包的查询键并加登录身份 guard；不添加比例、文案、卡片、按钮或入口。不改 `features/wallet` 的传统邀请卡。 |
| [components/user/rebate-records.tsx](../../../web/default/src/features/invitations/components/user/rebate-records.tsx)、[format.ts](../../../web/default/src/features/invitations/format.ts) | `RebateRecordList` 分别渲染桌面表和手机卡；`RecordStatus` 已处理销售状态。当前只渲染金额。 | 两种布局调用同一个行内「本笔比例」渲染分支，放在已有金额下方；本 feature 的 `format.ts` 放首页和历史共用的比例校验、格式化。记录查询键补登录人；筛选、每页 10 条、金额和徽标不变。 |
| [components/user/sales-customers.tsx](../../../web/default/src/features/invitations/components/user/sales-customers.tsx)、[sales-customer-recharges.tsx](../../../web/default/src/features/invitations/components/user/sales-customer-recharges.tsx)、[types/sales.ts](../../../web/default/src/features/invitations/types/sales.ts) | 客户汇总已有 `userId` 键；客户充值弹窗已有 `viewerId + customer.userId + page` 键。`SalesRecharge` 有金额和返利状态，**没有 `rebateRatio`**。 | 保持原样；销售通过共同首屏和「返利记录」看比例。不要在客户列表、弹窗或 `SalesRebateAmounts` 重复展示，不根据充值金额反推，也不逐客户补查。保留这些组件原有的轮询，不给规则加轮询。 |
| `features/invitations/components/user/rebate-transfer.tsx`、`rebate-requests.tsx` | 可申请额度和申请列表沿用各自的读键；提交前再次检查可申请额度查询是否成功。 | **仅回归，不改这两个组件。** 保留读键、启用条件、轮询、页头刷新和提交前检查。规则摘要成功或失败都不能改变转入权限、确认框或已申请列表。 |
| `i18n/locales/*.json`、按需 `i18n/static-keys.ts` | 邀请功能使用英文源文案作为扁平 key。 | 仅在实现阶段按第 6 节补新文案；优先可扫描的 `t('…')` 字面量。无需新依赖、全局 Store、新路由或管理端改造。 |

## 2. 查询、账号隔离与刷新

**邀请页与分享卡共用一次读取；钱包不共用错误结果。** 目前两端都用 `['kkai', 'invitations', 'my-code']`：邀请页调用 `requireInvitationData`，业务失败抛错并显示可重试的 `ErrorState`；钱包使用 `skipBusinessError: true, skipErrorHandler: true`，业务失败返回 `null`，`retry: false, staleTime: 60_000`，没有邀请码时按原行为回退 `AffiliateRewardsCard`。若只给旧键加 `userId`，同一账号仍可能把钱包缓存的 `null` 当成邀请页的成功结果。

**查询键改动仅限三处：** `my-code` 的邀请页和钱包两个消费者，以及返利记录列表。三处均保留前缀 `K = ['kkai', 'invitations']`；`userId` 来自 `useAuthStore(state => state.auth.user?.id)`，不是邀请码、路由参数或被邀请人 ID。

| 读请求 | 查询键与启用条件 |
| --- | --- |
| 页面规则和「我的邀请」 | `[...K, 'my-code', userId]`，仅 `userId` 有效且 `feature.userVisible` 为真。页面只建一个 `useQuery`，子组件直接消费；切标签、展开规则、切语言、复制和扫码都不触发重复 GET。严格型查询用 `getMyInvitationCode({ signal, disableDuplicate: true })`：由 React Query 合并本页读取，避免底层 [HTTP 客户端](../../../web/default/src/lib/http-client.ts) 按 SID、URL 和参数合并同一在途 GET 时复用钱包的请求配置或取消信号。无需修改 HTTP 客户端。 |
| 钱包 | `[...K, 'wallet-my-code', userId]`，同样要求有效身份且有权限。保留原 queryFn、静默选项、加载及回退；权限读取失败仍走原 `retryAccess()`，不能降级为普通钱包转入。允许两个**不同页面**各自首访取一次，避免两种错误约定污染彼此。 |
| 「返利记录」 | `[...K, 'records', userId, page, status]`；仅在允许查看记录且登录人已确定时读取。切换账号不能复用上一人的同页同筛选结果；不为首屏摘要预取明细。 |

- 上述三处无有效身份时不发请求、不展示缓存；不跨账号沿用 `placeholderData`、旧邀请码、二维码、费率或记录。必要时仅在分享卡内关闭旧二维码，不重新挂载整组 Tabs。A 的慢响应不能覆盖 B 的数据。其他页签和 `sales-profile`、客户列表／明细的键不动。
- [auth-query-cache.ts](../../../web/default/src/lib/auth-query-cache.ts) 已监听 `userId` **或** `session.sid` 的改变并清理非公共缓存；这是第二道防线，不代替显式个人键。无需改全局缓存、延长 TTL 或缓存格式化后的字符串。
- **页头刷新保持原样。** `Refresh` 仍调用 `invalidateQueries({ queryKey: K })`，按钮禁用和旋转仍看 `feature.query.isFetching`。三个新键保留 `K` 前缀，现有刷新就能重取本账号活跃的摘要和记录，也不会漏掉其他活跃页签。不改失效范围或忙态，不为这次点击额外调用 `my-code.refetch()`。规则区的 `Retry` 只重取当前账号的 `my-code`；访问错误仍用 `retryAccess()`。
- **「转入／申请」只验原行为。** 保留 `[...K, 'available-rebates']`、`[...K, 'rebate-requests', page]`。在该页签点页头刷新，可申请额度和当前页申请列表仍会读取；刷新本身不提交申请。提交前仍以原 `AVAILABLE_REBATES_KEY` 检查查询成功、额度为正、有记录 ID 且允许提交；额度刷新失败或销售被停用，应关闭已打开的确认框、不发 POST。仅个人规则失败时不禁用本来合法的操作。

## 3. 首屏状态及优先级

在现有标题和刷新按钮下紧邻显示正文。这里只消费服务端的选择结果，不读取管理员规则、不按客户端分组、销售标签、时间或历史最高比例做匹配。按表自上而下判定，命中即停止；不能用 `if (data)` 替代状态判断。

| 顺序 | 条件 | 用户应看到／还能做什么 |
| --- | --- | --- |
| 1 访问 | 未登录、身份待定，或 `status`／`sales-profile` 发生访问错误（含后台失败且还留有缓存） | 不显示个人比例、邀请码或历史。保持既有登录、加载或访问级 `ErrorState + Retry`；不据旧权限发 `my-code`，不因读取失败误跳钱包。权限已确定不可见才按原逻辑跳 `/wallet`。 |
| 2 明确关闭 | 可进入，且本次成功取得的全局订单返利开关明确为 `false` | 「订单返利未开启」。优先于个人加载、旧成功值、个人失败和旧版缺字段；不是 `0%`。仅开注册奖励仍可展开固定奖励说明。 |
| 3 读取失败／进行中 | 未明确关闭，当前 `my-code.isError`（含 `isRefetchError`），或身份对应请求尚未成功 | **错误先于残留 `data`**：错误时「暂时无法显示充值返利比例」＋重试，无旧数字。首次或重新核对期间显示短加载提示／骨架，不先闪 `0%` 或上次费率；重试未成功前不恢复旧值。整份 GET 超时、业务失败或无基础数据，「我的邀请」保留原错误与重试，其他获准标签不被替换。 |
| 4 策略明确关闭 | 已获当前账号的成功摘要，`topupRebatePolicy.state === 'disabled'` | 同「订单返利未开启」。即便比公共开关刷新得早，也不显示数字；其他非 `available` 状态携带多余 `ratio` 一律不展示该数值。 |
| 5 开放且可用 | 确认开关为 `true`，`state === 'available'` 且 `ratio` 为 `[0,1]` 有限数 | 「好友充值返利：{{rate}}」「按好友实际到账额度计算」。**数值 `0` 有效**。不讲实付现金、即时到账或所有受邀行为通用。 |
| 6 开放但未匹配 | 确认开关为 `true`，`state === 'not_configured'` | 「当前暂无适用的充值返利规则」。这是成功读取后的业务结论，不是网络异常。 |
| 7 其余 | `unavailable`、旧版缺整字段、`null`、未知状态、坏对象、`available` 无有效数值，或开关尚无可信证据 | 「暂时无法显示充值返利比例」，可重试。成功的基础摘要仍显示邀请人数、邀请码、复制链接和二维码；**不要为了校验新增字段而抛掉整份响应**。 |

「其他奖励规则」默认收起：订单返利确认开启且未被个人策略明确关闭时，可解释「订阅或兑换是否返利、返多少，以该笔记录为准」；`invitationSignupRewardEnabled === true` 时说明「注册奖励为固定额度，不按充值比例计算」。两者皆无则不渲染展开项。`rebateToBalanceEnabled` 或 `settlementEnabled` 关闭不等于费率为零。已登记但停用的销售保留原历史入口和申请禁用；能进入页面时只展示其**当前**规则，不从 `salesVisible` 推出专属比例。

## 4. 每笔比例：分类先于数值，数值先于状态

只读该行的 `orderType`、`rebateRatio` 和已有状态字段。以下规则只生成金额旁的**说明行**，不替换 `RecordStatus`、不触发重新计算、不改变筛选或可申请性。

| 顺序 | 判定 | 说明行 |
| --- | --- | --- |
| 1 固定奖励 | `invite_inviter` 或 `invite_invitee`；即使错误地附带了 `0` 或非零比例 | `固定奖励（不按比例）`。不依赖可能缺失的 `source` 来识别。 |
| 2 订单且有有效快照 | `topup`、`subscription`、`redemption` 的 `rebateRatio` 是有效数值 | `本笔比例：{{rate}}`，包括 `0%`；在确认中也可有快照，不表示已发放。兑换经充值事件形成 `topup` 时也只读本行快照，不去判断上游来源。 |
| 3 不能信任的输入 | 未知 `orderType`，或已知订单的比例**有值但非法**（字符串、布尔值、非有限值、负数、超过 1） | `暂无比例记录`，无数字；不把脏值伪装成等待补算。 |
| 4 缺值但已过计算阶段 | 比例 `null` 或缺字段；`issuedAt` 有值，或原始 `status` 为 `requested / approved / completed / rejected`，或 `displayStatus` 明确为 `claimable / available / requested / pendingApproval / pending_approval / approved / paid / issued / completed / closed / rejected` | `暂无比例记录`。**这些证据优先**：例如 `rejected + estimated`、`issuedAt + initializing` 不说「待核定」。审批和发放仍是两件事。 |
| 5 缺值且明确未核定 | 没有上一行证据，且 `displayStatus` 为 `confirming / initializing / pending_calculation / not_generated / estimated`；或没有 `displayStatus` 且原始 `status === 'initializing'` | `比例待核定`。既有 `RecordStatus` 只在非注册奖励、`estimated` 且比例为空时把它解释为 `pending_calculation`；已有合法比例优先按第 2 行。 |
| 6 其余缺值历史 | 仅有 `status === 'pending'`、仅有冷却／锁定标记（`cooling / locked / waiting_unlock`）、未知显示状态、老数据无确认线索 | `暂无比例记录`。等待解锁、等待申请并不等于等待计算。 |

桌面 `md` 及以上沿用 `TableCell`；在「返利金额」下换行，覆盖该单元格或说明的默认 `whitespace-nowrap`。手机沿用 `<dl>` 中金额对应的 `<dd>`，`min-w-0`、允许折行，不加列、不增加横向滑动。销售仍隐藏订单金额；`legacy_075` 保留原站内额度格式，`usd_credit_v1` 用原美元额度格式，普通用户仍用 `formatRebateAmount`。不除金额、不用今天的费率填历史、不调用销售客户明细弥补缺值。

## 5. 交互和可访问性

- 桌面与 **360px** 手机都先见比例和基数，再见可选说明与标签。用普通正文和主题色，窄屏自然换行；不做横幅广告、仅图标提示、悬停提示或强制弹窗。普通和销售两个默认落点、直接进入 `?tab=records` 都用同一组件。
- 补充说明可用原生 `details/summary`，Enter／Space 可展开收起、有可见焦点；不把比例藏在里面。加载和错误有可读文本，可用小范围 `role="status"` 通知读屏，不反复播报整张表；重试使用现有 `Button`，通知不能只有颜色或 toast。
- 保留 `generateAffiliateLink` 生成同源 `/sign-up?aff=…`（按原方式编码）。「复制邀请码」复制原码，「复制邀请链接」和二维码编码同一完整链接。策略局部失败后仍可执行三项操作；禁用剪贴板仍走原 `Copy failed`，二维码支持键盘关闭及焦点返回。没有合法的本账号基础响应时不合成邀请码或链接。

## 6. 文案和百分比契约

英文列是拟用的可扫描 flat key，中文是交付口径；`{{rate}}` 传**已按当前语言格式化、包含百分号的字符串**。不要复用钱包／充值定价的 `Top-up ratio`（充值倍率）、通用 `Ratio` 或状态徽标 `Awaiting Rebate Calculation` 来翻译「本笔比例」。`Refresh`、`Retry`、加载与复制的既有操作沿用原 key。

| 英文源文案 | 简中 |
| --- | --- |
| `Rebate on a friend's top-up: {{rate}}` | 好友充值返利：{{rate}} |
| `Based on the credits actually added to your friend's account.` | 按好友实际到账额度计算。 |
| `Other reward rules` | 其他奖励规则 |
| `For subscriptions and redemptions, see each record for any rebate and its rate.` | 订阅或兑换是否返利、返多少，以该笔记录为准。 |
| `Sign-up rewards are fixed credits, not a percentage of a top-up.` | 注册奖励为固定额度，不按充值比例计算。 |
| `No top-up rebate rule currently applies.` | 当前暂无适用的充值返利规则。 |
| `Order rebates are not enabled.` | 订单返利未开启。 |
| `The top-up rebate rate is temporarily unavailable.` | 暂时无法显示充值返利比例。 |
| `Rate for this record: {{rate}}` | 本笔比例：{{rate}} |
| `Rate pending confirmation` | 比例待核定 |
| `No rate recorded` | 暂无比例记录 |
| `Fixed reward (not percentage-based)` | 固定奖励（不按比例） |

- 用 `useTranslation()` 随 UI 切换语言；用 [toIntlLocale](../../../web/default/src/i18n/languages.ts) 转换 `i18n.resolvedLanguage ?? i18n.language` 后传给 `Intl.NumberFormat`，尤其不要直接传 `zhCN` 或 `zhTW`。比例仅接受 `typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 1`；不 `Number(value)`、`parseFloat`、`ratio || 0` 或截断越界值。
- 将合法的**分数本身**交给 `Intl.NumberFormat(locale, { style: 'percent', maximumSignificantDigits: 15 })`。不先乘 100、不拼 `%`、不改通用 [lib/format.ts](../../../web/default/src/lib/format.ts) 的 `formatPercent`（它接百分数再除 100，且未接 UI locale）。简中夹具 `0 / 0.0001 / 0.075 / 1` 应为 `0% / 0.01% / 7.5% / 100%`；更小的正值也不应被固定小数位抹成显式零。法、俄等语言保留 Intl 的小数逗号及百分号间距，不用英文字符串断言所有语言。
- [运行配置](../../../web/default/src/i18n/config.ts)覆盖**六种语言、七个界面选项**：中文、英语、法语、日语、俄语、越南语；中文的简体 `zhCN`（`zh.json`）与繁体 `zhTW`（`zh-TW.json`）分别验收，其余五项为 `en / fr / ja / ru / vi`。七份资源均逐 key 检查真实译文和插值：两条比例句各保留一次 `{{rate}}`，其余句子不增添它。切换选项只更新文案与数字，不重发 `my-code` 或记录请求；不能用英文回退或简中截图替代繁中验收。
- 按仓库 `i18n-translate` 技能以临时 `scripts/add-missing-keys.mjs` 写入这些资源，最后执行 `bun run i18n:sync` 并检查无无关改写；该临时脚本并非仓库现成命令。`bun run i18n:check` 的 `REQUIRED_LOCALES` 只有 `en / zh`，不能作为全语言完成证明。本次文档交付不执行这些写操作。

## 7. 定向自测和交接

以下是**实现后的计划**，不是测试结果。复用 Vitest、Testing Library、独立 `QueryClient({defaultOptions:{queries:{retry:false}}})` 和显式登录夹具；用可控 Promise 制造迟到／失败响应，不用 sleep 或真实业务订单。

| 测试入口与现状 | 待补的行为断言 | 对应 PRD |
| --- | --- | --- |
| **拟新增唯一主组件测试** `src/features/invitations/__tests__/rebate-policy-visibility.test.tsx` | 零邀请普通落点与销售客户落点均能看到规则；严格按 `my-code` 服务端夹具切换专属／分组／通用，不在浏览器验证匹配算法。页面摘要＋分享卡只一次 GET；切标签不重复查。仅开注册／关闭转入／停用销售的可见性与申请限制不变。 | A01–A03、A08 |
| 同一拟新增文件，使用同一个 QueryClient | 区分合法零、关闭＋旧字段缺失、未匹配、仅规则失败、整个接口失败、未知状态及非法输入。A→B 后让 A 最后返回，在相同页码和筛选下 B 不闪现 A 的码、二维码、费率或记录。用原页头刷新，当前摘要只发一次 GET；后台失败撤旧数字，成功重试才恢复。局部失败仍可复制码／链接和扫码。钱包的业务失败 `null` 不影响邀请页的严格错误／重试，反向切回不破坏钱包回退。 | A04、A08、A09、A11 |
| **已有** `__tests__/sales-records.test.tsx`：已断言销售不造订单金额、旧币制、已发放徽标及普通金额列。优先扩展这里。 | 行级断言普通／销售、桌面 `tr` 和移动卡对应的金额＋比例；三类订单、两类注册、显式零、处理中有值、缺值、坏值、未知类型及冲突终态。首页夹具从 5% 刷为 7.5%，原订单仍为 5%，行内不发额外请求；筛选与翻页仍生效。 | A05–A07、A11 |
| **已有回归** `status.test.ts`、`__tests__/sales-profile-error.test.tsx`、`__tests__/financial-query-errors.test.tsx`、`__tests__/sales-customers.test.tsx` | 保留可见性、钱包身份错误、额度刷新失败／销售停用后禁止提交、客户汇总与历史金额的原断言，不改转入读键或测试预期。**待补页签联动**：在主组件测试中分别停留「受邀客户」「返利记录」「转入／申请」，点原页头刷新。当前标签和筛选／页码不跳转，其原有活跃查询照常刷新；仅规则读取失败时，各页签仍可按原权限使用，刷新不发提交请求。 | A03、A05、A08、A09 |
| 同一拟新增主文件＋真实浏览器 | 桌面 1280×800、手机 360×640 CSS px 各验七个界面选项（14 组，简／繁体分列）。用对应真实资源和 Intl 预期检查文案、插值、四个比例边界、金额行、键盘展开与重试；法、俄、越长句应能换行。JSDOM 不证明实际布局或扫码结果，另交浏览器截图和测试链接的扫码结果。 | A01、A10、A11 |

**实现后从 `web/default/` 执行**，按变更选择命令（`package.json` 的 `test` 是 `vitest run`，不可省略文件过滤）：

```sh
# 新入口落盘后，验证首屏、缓存和历史行
bun run test src/features/invitations/__tests__/rebate-policy-visibility.test.tsx src/features/invitations/__tests__/sales-records.test.tsx
# 已有直接相关的访问、钱包、销售、转入回归
bun run test src/features/invitations/status.test.ts src/features/invitations/__tests__/sales-profile-error.test.tsx src/features/invitations/__tests__/financial-query-errors.test.tsx src/features/invitations/__tests__/sales-customers.test.tsx
```

定向 lint 只把实际改动的 TS/TSX 文件传给已安装的 `oxlint -c .oxlintrc.json`。`bun run typecheck`（`bun check -b`）、`bun run lint` 和无文件过滤的 `bun run test` 均不是定向验证；没有模块级类型脚本时记录缺口，不用全量构建代替，不擅跑全套。

**交接给后端与 QA：** 请后端交付同一测试账号的完整旧摘要、四态新摘要（另含显式零、故意缺字段）、整份访问失败和代表性历史行；销售身份与规则生效时点以服务端验证为准。前端交付普通／销售首屏、桌面／手机明细、两账号切换和失败重试的脱敏证据，以及实际命令、命中用例数、未验项。旧前端＋新服务保留原字段；新前端＋旧服务降级提示但不破坏分享或历史；回退展示不回滚账本。以 [QA 验收矩阵](./qa.md)逐项签收，不能用截图证明结算正确，也不能用本规格宣称已经上线。

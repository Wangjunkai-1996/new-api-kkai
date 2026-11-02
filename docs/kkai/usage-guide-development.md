# 使用说明模块开发文档

状态：实现版 v1，2026-11-03；后端、前端和定向测试已落地。真实登录态视觉验收、操作系统客户端唤起验收和全量类型检查仍待执行。

本文是 KKAI NewAPI `web/default` 使用说明模块的开发合同，汇总用户需求、产品/交互/技术评审与后续确认的标准 CC Switch 协议取舍。需求基线见第 2 节，逐项验收见第 12 节。

设计基线已随仓库保存：[可交互预览](usage-guide-design/preview.html)、[原始预览片段](usage-guide-design/prototype.html)、[桌面截图](usage-guide-design/desktop.png)。预览保留原稿，不包含真实业务；本文是模型选择、错误恢复、安全和实现范围的补充依据。与旧聊天评审中“ticket 阻断首版”的结论冲突时，以第 2.2 节的新决策为准。

本次实现不包含生产访问、生产发布、真实收费请求或代用户安装客户端；真实登录态和客户端验收需要使用批准的测试账号单独完成。

## 1. 目标与范围

### 1.1 用户目标

登录用户进入「使用说明」后，应能：

1. 了解 CC Switch、Codex、Claude Code、Claude Desktop、Cherry Studio 等工具的用途和官方安装入口；
2. 选择 GPT / Codex 或 Claude Code 快捷配置；
3. 在明确确认已安装并打开 CC Switch 后，创建指定分组的专用 Key，并尝试打开 CC Switch；
4. 在浏览器拒绝唤起、客户端未安装、模型不可用或网络结果不明确时，继续使用同一个 Key 手动配置或进入 API 密钥管理；
5. 在首次请求前分清“Key 已创建”“已请求打开”“已导入”和“首次调用成功”四种状态。

### 1.2 P0 范围

- 一个认证路由 `/usage-guide`；
- 页面页签：快速开始、工具下载、常见问题；
- GPT / Codex 和 Claude Code 两张快捷配置卡；
- CC Switch、Codex CLI、Codex 桌面入口、Claude Code、Claude Desktop、Cherry Studio 工具卡；
- API Key 创建结果返回精确 token ID；
- 依据 token ID 查询当前可用模型；
- 复用现有 CC Switch 标准深链协议；
- 手动配置入口、重试打开入口、API 密钥管理入口；
- 响应式、键盘可操作、暗色主题和当前七种前端语言：en、zh（运行时 zhCN）、zh-TW（运行时 zhTW）、fr、ja、ru、vi；
- 定向后端、前端、交互和链接验收。

### 1.3 明确不在 P0

- 帮助中心 CMS、管理员编辑教程、远程内容服务；
- 自动检测本机是否安装或正在运行 CC Switch；
- 自动下载安装、镜像下载代理或后台安装客户端；
- 为 Cherry Studio、Claude Desktop 或所有工具增加第二套自动导入协议；
- 自动认领、改名、改额度、改权限或删除已有 Key；
- 跨刷新、跨标签页、跨设备的严格全局幂等；
- 创建 Key 后自动发送真实请求或自动扣费验证；
- 用一次性 ticket 替换现有 CC Switch 协议。本次采用现有标准协议，ticket 作为后续安全增强。

## 2. 已确认的产品规则

### 2.1 两个快捷配置 preset

| 字段 | GPT / Codex | Claude Code |
| --- | --- | --- |
| 前端 preset | `gpt` | `claude-code` |
| 展示标题 | GPT / Codex | Claude Code |
| canonical group | `GPT-稳定` | `claudecode-满血企业` |
| Key name | 与 group 完全相同 | 与 group 完全相同 |
| `unlimited_quota` | `true` | `true` |
| `cross_group_retry` | `false` | `false` |
| `expired_time` | `-1`，与设计稿和当前 Key 表单默认策略一致，弹窗显示“长期有效，可随时停用” | 同左 |
| 其他创建字段 | `remain_quota=0`、`model_limits_enabled=false`、`model_limits=""`、`allow_ips=""`，不传 `auto_groups` | 同左 |
| CC Switch app | `codex` | `claude` |
| CC Switch provider name | 预填 `GPT-稳定`，不再沿用默认站点名 | 预填 `claudecode-满血企业` |
| endpoint | 规范化可信 API 基地址 + 单个 `/v1` | 规范化可信 API 基地址 |

“额度不限”只表示该 Key 不设置独立额度上限，展示文案必须使用：

> Key 不单独限额，实际使用仍受账户余额、套餐与分组权限限制。

不得展示“免费”“无限使用”或绕过账户余额的表述。`remain_quota=0` 与 `unlimited_quota=true` 组合不等于零可用额度。长期有效是本版明确的默认值，不是“额度不限”的推论；若目标站点后续设置强制有效期策略，必须遵守并同步弹窗文案，禁止偷偷覆盖。用户以后在 API 密钥页面改名，不触发分组联动改名。

分组值是后端 canonical key，展示名称仅用于文案。分组不存在、当前用户或租户无权使用、没有有效定价时，必须在创建时拒绝，不能回退默认组、当前用户组或另一个相近分组。模型以创建后 token 模型接口为权威；无模型时 Key 已创建，停止导入并允许后续恢复。P0 不新增模型预检 API，不将主站按组查询误当成已经支持租户的统一预检。

余额不足允许完成安装和创建，提示充值或检查套餐后才能调用；创建动作本身不发起计费请求。已有 Key 用户直接去 API 密钥页自行选择，不自动按同名认领或改写旧 Key。

### 2.2 CC Switch 协议决策

本次产品决策接受当前 CC Switch 标准导入协议：生成 `ccswitch://v1/import?...&apiKey=...`，由用户明确点击触发本机应用。这个决定保留现有一键配置能力，同时接受长期 Key 经过浏览器和操作系统协议处理链路的残余风险。

实现必须遵守以下边界：

- 只有用户主动点击创建/打开流程后，才读取明文 Key 和拼接深链；
- 不提供复制完整深链，不把深链写入 `localStorage`、`sessionStorage`、本站 HTTP URL/query、埋点或业务日志；这里不禁止已接受的 `ccswitch://` URI 本身；
- 不把明文 Key 写入 React Query 持久化缓存、错误对象、toast、异常上报或截图文案；
- 深链只传给本机协议处理器；外链 `<a>` 使用 `rel="noopener noreferrer"`，若复用 `window.open` 则使用对应 `noopener,noreferrer` features，并验证目标浏览器行为；
- 页面显示“已请求打开 CC Switch”，不显示“已导入”或“配置成功”；
- “再次打开”只复用同一个 token ID，不重复创建；
- 仍保留后续 ticket 方案的隔离边界。若未来改回 ticket，必须先完成客户端协议、服务端兑换、重放保护和真实 Windows/macOS 验收。

本次文档交付同步更新 [CC Switch 历史约束](cc-switch-ticket-blocker.md)、[fork manifest](fork-manifest.md) 与 [历史移植计划](legacy-port-plan.md) 的决策指向，保留旧方案依据。例外仅适用于当前默认控制台中用户明确触发的 CC Switch 导入；不授权通过其他网页地址分享凭据，也不扩大到其他客户端协议。

风险说明必须准确：该 URI 是本机应用协议，不是网站 HTTP 请求；不能断言它必然进入 Nginx 日志或浏览器历史。链接误分享、客户端诊断记录、错误的协议处理程序仍可能泄露长期 Key；因此不声称零风险或已审计所有第三方客户端版本。上线前记录测试的客户端版本与日志检查结论，不维护定制客户端。

## 3. 页面信息架构与视觉还原

页面使用现有控制台 shell、侧栏、页面容器、主题变量、shadcn/Base UI 组件和 Hugeicons。设计预览中的 `kg-*` 样式仅是参考，不得将整段裸 HTML/CSS 直接复制到产品代码。

### 3.1 页面骨架

```text
控制台 / 使用说明
└── 从这里，开始使用 AI
    ├── 选好工具，完成配置，让第一个问题得到回答。
    ├── 页签：快速开始 | 工具下载 | 常见问题
    ├── 快速开始
    │   ├── 快捷配置
    │   │   ├── GPT / Codex 卡片
    │   │   └── Claude Code 卡片
    │   ├── Key 额度说明
    │   └── 先把工具准备好：CC Switch、Cherry Studio
    ├── 工具下载
    │   ├── 操作系统筛选：全部、Windows、macOS、Linux
    │   └── 六张工具卡
    └── 常见问题
```

页面入口放在「General」导航中，位置靠近概览、API Keys 和 Usage Logs。概览现有新手引导增加「使用说明」入口；不要另建一套 onboarding 状态机。

主站及普通租户控制台用户均能查看工具说明；快捷操作只在其具有目标分组权限时可用。必须将 `/usage-guide` 加入 `TENANT_CONSOLE_ROUTES`，保留 tenant owner 原有重定向，不将平台分组强行暴露给租户。

`use-sidebar-config.ts` 将 `/usage-guide` 映射到现有 `console.detail`，遵守管理员 × 用户的可见性合并。首版不新增 `console.guide` 设置项，也不能漏映射后变成无条件可见。导航隐藏不是后端授权；路由继承登录检查，创建/读取接口仍独立鉴权。导航中只插入使用说明项，设计稿侧栏中省略的其他真实模块、品牌、footer 不删改。

### 3.2 视觉验收基线

- 内容区域以原稿为基准，页面内边距约 30px 28px；原稿 172px 侧栏是示意，真实实现保留现有侧栏宽度、Header、页面容器和 Footer；
- 快捷配置卡桌面双列，间距约 16px；窄屏单列；
- 快捷卡包含工具图标、标题、用途副标题、描述、分组、额度、主按钮；
- 页面采用现有 `background`、`card`、`muted`、`foreground`、`border`、`primary` token，浅色和暗色都要可读；
- 标题、正文、辅助文案的层级与设计稿一致，辅助文字不得小于项目可接受的最小字号；
- 原稿在 760px 以下隐藏侧栏、520px 以下单列。实现沿用现有控制台移动侧栏，在内容不足以容纳双列时切为单列（可用现有 640px 断点）；触控区域至少 44px，弹窗操作区窄屏纵向排列；
- 不依赖 hover 展示关键内容；焦点环、键盘顺序和屏幕阅读器名称由现有组件保留；
- 不能出现横向溢出、裁剪、双重滚动容器或遮挡弹窗；
- 使用现有动画和 `prefers-reduced-motion` 行为，不新增持续动画。

| 元素 | 原稿基线 | 允许偏差/实现规则 |
| --- | --- | --- |
| 标题/正文/辅助文字 | 26px / 14px / 12px；标题行高约 1.4，正文约 1.65 | 对齐现有 typography token，字号偏差不超过 2px，辅助文字不缩小换行 |
| 页签 | 文本按钮、底部横线、选中项深色短下划线 | 不改为大色块 pills；复用 Tabs 的键盘和 ARIA 行为 |
| 快捷卡 | 12px 圆角、1px 中性边框、内距 21px、两列 gap 16px | 按 4px spacing scale 取 20px；关键间距误差不超过 4px |
| 卡片图标/分组区 | 38px 图标底座、轻量 GPT/Claude 色彩；浅底两行字段 | 映射现有语义色，暗色不直接复制浅色十六进制值 |
| 主要按钮 | 两张卡各自底部满宽、中性深色、箭头 | 不新增无关渐变、大插图或第二个同等级主动作 |
| 工具卡 | 9px 圆角、15px 内距、gap 12px；首屏仅 CC Switch/Cherry | 取 8px/16px token；下载区最多双列，保持原内容顺序 |
| 弹窗 | 最大宽度 474px、内距 24px、14px 圆角 | 使用现有 Dialog，`sm:max-w-[474px]` 或最近规格；窄屏保持 16px 边距，允许内容滚动 |
| 弹窗内容 | 标题→三步指示→安装入口→Key 摘要→确认勾选→主/次动作 | 加载、模型选择与错误块在同一弹窗展开，不再叠第二个导入弹窗 |

“高度还原”按内容区结构、相对位置、文字层级、卡片密度和状态切换验收；真实 shell、图标库、可访问性和安全差异以本文为准。禁止为了截图一致而写死容器高度、隐藏溢出或缩小文字。

## 4. 工具目录合同

工具目录放在 `features/usage-guide` 的静态 catalog 中，由代码维护，不从用户浏览器实时抓取下载地址。

每条记录至少包含：

```ts
type UsageTool = {
  id: string
  name: string
  kindKey: string
  descriptionKey: string
  platforms: Array<'windows' | 'macos' | 'linux'>
  downloads: Array<{
    platform: 'windows' | 'macos' | 'linux'
    architecture?: 'x64' | 'arm64' | 'universal'
    labelKey: string
    url: string
    action: 'download' | 'official-page' | 'install-guide'
  }>
  officialUrl: string
  checkedAt: string
  notesKey?: string
}
```

规则：

- 每个平台和架构必须使用真实对应 URL；同一个官方页面可以有多个平台按钮，但按钮文字必须写“官方入口/下载页”，不能伪装成直接安装包；
- 当前官方资源或版本未知时，显示“查看官方页面”，不要制造 Windows/macOS/Linux 直链；
- 每次修改下载地址更新 `checkedAt`，链接审计至少检查 HTTP 可访问、域名归属和按钮动作；
- 外链使用新标签页和 `noopener noreferrer`；不下载、不代理、不缓存远程二进制；
- CC Switch 说明“配置管理工具”，并提示仍需安装目标工具；
- Codex CLI、Codex 桌面入口分别说明。不能把 CLI 安装脚本当成桌面安装包；
- Claude Code、Claude Desktop 分开说明。Claude 快捷配置只面向 Claude Code；Desktop 只提供独立下载和手动接入说明；
- Cherry Studio 提供聊天/写作/知识库定位和三步手动接入，不增加第二套自动导入；
- 工具名称和品牌可保留原文，所有按钮、描述、FAQ 和状态文案走 i18n。

首版官方入口基线：

| 工具 | 官方入口 | 接入说明 |
| --- | --- | --- |
| CC Switch | `https://github.com/farion1231/cc-switch/releases/latest` | 安装后打开，作为配置管理器 |
| Codex CLI | `https://learn.chatgpt.com/docs/codex/cli` | GPT / Codex 快捷配置目标 |
| Codex 桌面入口 | `https://learn.chatgpt.com/docs/app` | 独立桌面说明，不能复用 CLI 文案 |
| Claude Code | `https://code.claude.com/docs/en/setup` | Claude 快捷配置目标 |
| Claude Desktop | `https://claude.com/download` | 独立下载与手动接入 |
| Cherry Studio | `https://www.cherry-ai.com/download` | 手动填写 API 地址、Key、模型 |

在代码合入前重新核对上述入口和实际发行包；官方文档跳转或版本变化时，更新 catalog，不把本表当作永久版本锁定。

## 5. 快捷配置状态机

每次打开快捷配置弹窗只处理一个 preset。用 TypeScript 判别联合表达状态与可用字段：未创建状态不能拥有 token ID；可继续取钥、查模型或打开客户端的已创建状态必须拥有正整数 ID；`created-unknown` 只表示服务端成功但缺少 ID，不能继续取钥、查模型或唤起。这里是业务状态约束，不要求引入状态机依赖。不能通过 effect 监听某个字段后隐式再次创建。

```text
prepare
  ├─ 未勾选“已安装并打开” → 禁用“创建并打开”
  ├─ 点击手动配置 → manual-before-create
  └─ 点击创建 → creating

manual-before-create
  └─ 独立确认“创建用于手动配置” → creating（mode=manual，不自动取钥/唤起）

creating
  ├─ 明确拒绝 → error-retryable
  ├─ 超时/断网/未知 → result-unknown
  ├─ success + data.id → loading-models
  └─ success 但缺 data.id → created-unknown

loading-models
  ├─ 手动模式 → manual（可展示模型加载/空/失败，不发起唤起）
  ├─ 有可用模型 → ready-to-launch
  ├─ 无模型 → created-no-model
  ├─ 查询失败 → created-model-error（只重试查询）
  ├─ 推荐无效/未配置 → select-model（只选择真实可用模型）
  └─ 归属或租户错误 → created-management-only

ready-to-launch
  ├─ 创建并打开流程、推荐有效 → 自动尝试打开一次 → launch-requested
  ├─ 用户选模型并点击继续 / 再次打开 → launch-requested
  ├─ 取钥失败 → created-key-error（只重试取钥）
  └─ 手动配置 → manual

launch-requested
  ├─ 保留“再次打开” → launch-requested
  ├─ 手动配置 → manual
  └─ 关闭 → 保留本页 token ID；管理/验证在现有页面完成
```

P0 没有客户端导入回调，不新增 `verified`、自动检测导入成功或轮询用量的状态。结果页引导用户在客户端确认、主动发送首次请求、到现有使用记录核对；网页成功口径截止于 `launch-requested`。

### 5.1 创建前

弹窗展示：工具、Key 名称、canonical group、额度说明、有效期实际策略、CC Switch 下载入口和目标工具入口。创建并打开流程必须要求用户勾选“我已安装并打开 CC Switch”。这个勾选只是用户确认，不能写成网页已检测到客户端。

在确认项旁以辅助文案说明“将把此 Key 导入本机 CC Switch”，不额外叠加重复确认弹窗。用户未授权发送 Key 时不通过效果钩子或下载按钮隐式触发。准备阶段按可用分组加载/失败/无权限分别禁用创建并给理由；接口层仍重验权限。

移动端显示“请在电脑浏览器完成 CC Switch 配置”，不在移动端自动唤起外部协议。安装确认只约束“创建并打开 CC Switch”；用户选择手动配置时进入独立的“创建用于手动配置”确认，不要求已安装 CC Switch，也不自动取钥或唤起，只有用户点击复制/显示参数时才读取明文 Key。

### 5.2 创建请求

前端只发送产品允许的字段，preset 不允许用户改写：

```json
{
  "name": "GPT-稳定",
  "group": "GPT-稳定",
  "expired_time": -1,
  "remain_quota": 0,
  "unlimited_quota": true,
  "model_limits_enabled": false,
  "model_limits": "",
  "allow_ips": "",
  "cross_group_retry": false
}
```

Claude preset 将 `name` 和 `group` 换成 `claudecode-满血企业`。必须显式发送 `expired_time=-1`；不要依赖 DTO/GORM 的隐式默认表达长期有效。不要发送 user ID、tenant ID、Key、创建时间等服务端拥有的字段。沿用租户数据库作用域/回调，不能绕过它们直接写数据库。

创建 mutation 显式 `retry:false`，提交按钮禁用，并在处理器中防止同一组件重复进入。明确的校验拒绝可修正后重试；超时、断网、5xx 或无法证明未写入的异常禁止自动再次 POST。成功缺 ID 单独提示“Key 已创建，请到 API 密钥继续配置”，不能写成创建失败。页面提供“去 API 密钥核对”，不按名称、最新时间或列表第一条猜测新 Key。

请求发出后关闭弹窗不能被当成撤销服务端写入；页面仍接收结果并按 preset 保存非敏感 ID，不因 dialog unmount 丢失成功结果。关闭弹窗或切换 preset 会取消当前的自动取钥、复制和唤起意图：尚未发出的取钥请求不再发起，已经发出的取钥结果丢弃，重新打开后必须由用户再次点击继续。离开页面、切换用户或租户时丢弃明文与本页状态，迟到响应不得唤起客户端或显示给新身份。

### 5.3 创建成功与模型

创建成功必须从后端响应读取准确 token ID（响应字段 `data.id`），然后调用：

```text
GET /api/token/{token_id}/models
```

模型列表必须经过当前用户、租户、分组和 token model limits 过滤。复用现有 `features/keys/lib/cc-switch-defaults.ts` 的 `CCSwitchApp` 与 `CC_SWITCH_RECOMMENDED_MODELS`；本页 preset 显式指定 app，不依赖模型推断来覆盖用户已选择的 GPT/Claude。实施前确认该 helper 的最终基线并协调集成，不能覆盖既有改动。具体模型名称是可维护的推荐值，不是本文永久约束。

预设推荐模型必须在真实列表中才可预选；不存在则在原弹窗中显示带搜索的模型选择，选定后点击“打开 CC Switch”。全部非空字段（包括 Claude 的可选 Haiku/Sonnet/Opus 映射）必须来自可用集合；无有效映射就省略，不擅自把所有档位设为最昂贵模型。仅有名字不能证明所有客户端工具能力，最终需要真实客户端验收。

列表为空提示“暂无可用模型”；网络失败提示“模型加载失败”并提供“重试加载”，只重试该 GET。两种情况都保留 Key，停止唤起，提供手动查看参数和管理入口；手动页不得伪装成模型已就绪。不得取列表第一项作为隐式模型，不得删除 Key 或再次创建。

### 5.4 深链打开

只有拿到明文 Key、有效模型和可信 endpoint 后才构造深链。优先在用户点击事件中打开；如果必须等待创建/模型请求，自动打开只是 best effort，结果页必须提供用户直接点击的“再次打开 CC Switch”。

复用现有 builder，明确传 app、provider name、模型和 Key。Key 可能以原始值返回，规范化时只补一次 `sk-`。保留已有 `enabled`、`homepage`、`usageEnabled`、`usageScript`、`usageBaseUrl`、`usageAutoInterval` 的已验收行为；余额显示依旧是用户余额语义，不能因为 Key 不限额显示无限账户余额。不得把密钥复制进 usageScript 字符串。

API 基地址使用已验证的本站/租户配置。保持 HTTPS、无 username/password/query/hash，保留部署要求的合法路径前缀并消除重复尾斜线；Codex 只追加一个 `/v1`，Claude/usage 使用对应根基址。不要直接套 `new URL(...).origin` 导致路径前缀丢失。复用现有来源选择，若本机状态缓存不属于当前租户/站点则失效重取，禁止将其他域名的 Key 导入过期地址。手动页与 deep link 使用同一已验证结果，不能分别拼接或使用占位域名。

深链发起后的文案为“Key 已创建，已发起打开 CC Switch，请在客户端确认导入并启用”。浏览器未安装应用、用户取消或协议被拦截，都不能变成失败创建；保留 token ID，允许再次打开或进入手动配置。

### 5.5 明文 Key 与手动配置

用户确认的“创建并打开”包含后续取钥与一次唤起授权；“再次打开”和“复制 Key”也是明确触发点。单纯浏览页面/下载、打开准备弹窗和切换页签不能读取明文。通过受保护接口读取：

```text
POST /api/token/{token_id}/key
```

读取后仅存于当前操作的短期内存，传递/复制完成或弹窗关闭后不再保留引用；不要宣称能在 JavaScript 中保证物理内存擦除。禁止放入本站 HTTP URL、日志、埋点、持久化缓存、React Query cache/dehydration、全局 store 或 sessionStorage。可以用普通 async 调用读取，避免返回的 secret 停留在 mutation cache。页面只显示遮罩；复制成功只在 Clipboard API 实际 resolve 后反馈，拒绝/不支持时保留重试与现有 API Keys 手动路径，不能伪造复制成功。已接受的 `ccswitch://` 传递是第 2.2 节的唯一 URI 例外。

手动配置页面必须显示运行时可信 endpoint、模型选择说明、Key 保管提示和 API 密钥管理入口。首次调用由用户主动完成，并提示会按正常规则计费。

## 6. 后端 API 合同

### 6.1 `POST /api/token/` 响应扩展

现有 `AddToken` 逻辑继续负责生成 Key、保存并设置脱敏审计上下文；本轮未找到 `TokenOperationAudit` 路由注册，不能声称审计事件当前已经落库。本版不顺带改全站审计体系，也不记录密钥或序列化 Axios error/response。成功响应增加精确 ID：

```json
{
  "success": true,
  "message": "",
  "data": { "id": 12345 }
}
```

约束：

- `data.id` 必须是当前用户刚创建记录的正整数；
- 不返回明文 Key；
- 成功为 HTTP 200；明确失败继续使用现有合同（许多业务拒绝仍为 HTTP 200、`success:false`），前端必须同时检查业务结果与响应结构；
- 更新前端 `createApiKey` 类型为 `ApiResponse<{ id: number }>`；
- 旧前端只读取 `success`，增加 `data` 不得破坏 API Keys 原有创建抽屉；
- 新前端收到 `success=true` 但缺 ID 时进入 `created-unknown`，不得当失败重建；
- 后端在 `common/console_contract.json` 的两个支持 profile 声明可选 capability `token_create_result_id`，同时有返回 ID 的行为测试；本次不提升全局 API contract 版本，不把它加入 `web/default/console-contract.json` 的全局 required 列表；
- 本模块通过现有 `useConsoleContract` 读取后端能力：缺失/未知时保留教程、下载、API Keys 入口，禁用本模块的新建向导并给出刷新/重试能力读取操作，不阻断整个控制台；独立前端继续使用 `/console-runtime.json`，不另造探测接口；
- 能力存在仍需防御缺 ID 响应，兼容灰度切换或旧缓存。前端发布/后端回退时验收上述降级，不把本地源码能力当成线上已安装证据。

### 6.2 权限、租户和数量

快捷流程必须复用认证上下文和 canonical group 规则：

- 平台用户检查 `service.IsUserTokenGroupUsable` 与当前用户组；
- 租户用户继续经过 `TenantPricingService` 的独立目录校验；
- 禁止前端只做权限判断，后端必须再次校验；
- 分组缺失、无权限、无定价、跨租户访问和达到令牌数量上限都要阻止创建；
- 不静默切换到默认组或当前组；
- 不改变禁用、删除和已有 Key 管理行为。

分组校验增量放在 `AddToken` 创建边界：保留普通接口的空 group 跟随用户组、auto profile 与租户目录行为，禁止 trim/rewrite canonical key。不要直接改共用 `validateTenantTokenGroup`，无意改变旧 Key 改名、额度、启用的行为；新快捷入口固定字段不等于剥夺普通 Key 表单合法选项。

已知限制：当前普通创建流程是“先 Count 再 Insert”，跨标签页可能同时越过数量上限。P0 保留现有计数检查和本页防重入，不承诺全局严格数量上限或幂等，不扩大为所有创建入口的并发重构。若另行要求解决，应复用 `model.LockUserForTokenCreation` 并协调所有创建入口；SQLite 跳过 `FOR UPDATE`，必须验证三数据库事务/重试行为，不能只加一把本机锁便宣称解决。

### 6.3 已有 API

| 用途 | API | 要求 |
| --- | --- | --- |
| 可用分组 | `GET /api/user/self/groups` | 使用 canonical key；`display_name` 只展示 |
| token 模型 | `GET /api/token/:id/models` | 按用户、租户、group、limits 过滤；空数组可处理 |
| Key 脱敏详情 | `GET /api/token/:id` / `getApiKey` | 复用归属校验结果检查状态、分组、有效期；密钥字段为遮罩，不用于导入 |
| 读取明文 | `POST /api/token/:id/key` | 只在用户动作后调用；已有限流和禁止缓存语义 |
| API Keys 管理 | 现有 `/keys` 路由及查询 | 失败、未知结果、已有 Key 均能回到这里 |

## 7. 前端实现结构

推荐起始文件范围如下；没有实际复杂度时不创建空目录、重复 API wrapper 或单用抽象。文件边界可按组件可读性调整，但行为与验收不能删减：

```text
web/default/src/routes/_authenticated/usage-guide/index.tsx
web/default/src/features/usage-guide/
├── index.tsx
├── catalog.ts             # 工具目录和下载链接
├── components/
│   └── quick-setup-dialog.tsx
└── __tests__/
    ├── quick-setup-dialog.test.tsx
    └── usage-guide.test.tsx
```

必要的现有文件改动：

- `web/default/src/hooks/use-sidebar-data.ts`：General 入口并更新 `TENANT_CONSOLE_ROUTES`，按第 3.1 节开放普通租户指南；
- `web/default/src/hooks/use-sidebar-config.ts`：`/usage-guide` 绑定现有 `console.detail`；
- `web/default/src/features/dashboard/components/overview/overview-dashboard.tsx`：增加入口，不重建引导；
- `web/default/src/features/keys/api.ts`：更新创建响应类型，复用 `fetchTokenKey`、`getTokenModels`；
- `web/default/src/features/keys/components/dialogs/cc-switch-dialog.tsx` 及 `features/keys/lib/cc-switch-defaults.ts`：复用推荐模型与唯一 builder，若抽到 lib 则迁移旧入口并跑直接依赖测试；不通过模拟点击原按钮或挂载整套 API Keys provider 来完成流程；
- `web/default/src/i18n/static-keys.ts` 与当前七种 locale：实现前加载 i18n skill，所有 locale 写入经过允许脚本，再执行 `bun run i18n:sync`；维护当前 zhTW 资源，不因旧文档写六语种就漏掉繁体；
- `web/default/src/routeTree.gen.ts`：通过现有 TanStack generator 生成，不手改；
- `controller/token.go`、相关测试、`common/console_contract.json`：按第 6 节做最小增量；不修改 schema、依赖或发布控制器。

组件规则：

- 用现有 shadcn/Base UI Dialog、Button、Tabs、Select、Checkbox、Alert；
- 使用 `useTranslation()`，所有用户可见文本走 `t()`；
- mutation 只在事件处理器中触发，禁止 useEffect 观察 success 后创建/打开；
- API 错误使用项目统一 `handleServerError`/toast 机制，同时保留可操作的内联状态；
- 对话框打开时锁定背景，关闭后恢复触发按钮焦点；Escape、Tab、Shift+Tab、左右/Home/End 页签操作符合现有组件约定；
- 使用安全的动态链接节点，不使用 `dangerouslySetInnerHTML`；
- 明文 Key 仅保留在当前用户操作的短期内存中；`useState` 不是允许长期缓存的例外。

本模块 query key 使用现有 `privateUserQueryKey` 约定，并在站点/租户切换时遵守私有缓存清理；Key 成功创建后 invalidate 当前身份的 Key 列表，不把 secret 放入 query key 或缓存。用户切换时清除旧流程并屏蔽迟到响应。新模块通过现有公共 API helper 完成请求，不新增第二个 Axios 实例。

## 8. 失败与恢复文案合同

| 状态 | 页面必须表达 | 可用操作 |
| --- | --- | --- |
| creating | 正在创建 Key | 禁用重复提交 |
| permission denied | 目标分组不可用 | 查看分组/返回 API Keys |
| token limit | 已达到令牌数量上限 | 管理已有 Key |
| model unavailable | Key 已创建，但没有可用模型 | 重载模型、查看手动参数、管理 Key |
| model fetch error | Key 已创建，但模型加载失败 | 只重试模型查询 |
| secret fetch error | Key 已创建，但密钥暂时读取失败 | 只重试取钥，尊重限流 |
| create unknown | 创建结果未确认，可能已经创建 | 去 API Keys 核对；不自动重试 |
| created | Key 已创建 | 继续查询模型 |
| launch requested | 已发起打开 CC Switch | 再次打开、手动配置、管理 Key |
| browser blocked | 有明确异常才说明浏览器阻止；否则展示“没打开？” | 用户点击再次打开、手动配置 |
| client not confirmed | 等待用户在 CC Switch 确认 | 用户确认后继续 |
| insufficient balance | 余额或套餐不足 | 充值/查看套餐；不自动请求 |
| Dashboard 401 | 登录会话失效 | 沿现有 auth refresh/登录流程处理；不重建 Key |
| Relay 401 | 客户端请求中的 Key 被拒绝或未正确配置 | 检查客户端 Key/地址，必要时重新读取或更换 Key |
| 403 | 分组或模型权限不足 | 管理分组/选择可用模型 |
| model rejected | 当前客户端模型不可用 | 重新选择 token 可用模型 |

`window.open()` 返回 null 也可能来自 `noopener`，不能据此肯定客户端未安装或已拦截；页面失焦、visibilitychange 和超时也不是成功/失败回执。所有结果/异常页都保留真实可导航的“查看 API 密钥”入口。路径 `/keys` 已存在，但没有按 token ID 打开详情的既有 query 合同；P0 跳转列表，给出本次 Key 名称/脱敏 ID 供用户辨认，不发明不存在的 `/keys/:id` 链接。关闭弹窗不删除已创建 Key；未知结果也不生成第二个 Key。

## 9. 状态恢复与安全边界

仪表盘接口的明确认证拒绝继续沿用现有 HTTP client 的 auth refresh/有界重试。mutation 的 `retry:false` 禁止创建结果未知时重放 POST，不擅自改变全局认证机制；不能将服务端异常误当作认证拒绝重试。

P0 明确不实现持久化向导恢复：只在当前页面生命周期内保存 preset、精确 token ID 与状态（按 preset 分开）。同页关闭重开继续使用相应 ID；刷新/离开后到已有 `/keys` 管理。不得使用 `sessionStorage`、`localStorage` 或设计预览的 `window.openai.widgetState` 保存流程。

再次打开前按精确 ID 校验仍属当前用户/租户、原分组、启用且未过期；用户已在其他页面改名、额度或限制时不覆盖其修改，刷新脱敏摘要并按最新模型集合处理。已禁用、删除、换组或读取被拒绝时进入管理页，不偷偷恢复或创建替代 Key。

跨刷新严格不重复需要服务端幂等键/操作记录，不能用同名搜索替代。除非另行批准并设计幂等合同，本次不加入新 schema 或全局唯一约束。

## 10. 测试与质量门槛

### 10.1 后端

新增/修改测试必须保护真实 API 合同和权限行为，使用 `testify/require` 做 setup/fatal assertions，`assert` 做非致命值检查。至少覆盖：

- `AddToken` 成功返回 `data.id`，且不返回明文 Key；
- GPT / Claude preset 的 name、group、unlimited、cross-group 字段；
- 分组不存在、平台用户无权、租户目录不匹配、令牌上限达到时拒绝创建；
- 成功但旧响应缺 ID 的前端兼容语义由前端测试覆盖；
- token ID 归属检查、模型过滤、空模型列表和跨租户拒绝；
- 并发数量上限若纳入本次改动，则验证用户锁下 count+insert；否则记录已知限制。

定向命令模板（实现后执行，新增测试使用 `TestAddToken...` 前缀；按实际受影响范围裁剪，执行前评估时长）：

```bash
go test ./controller -run 'Test(AddToken|GetAllTokensMasksKeyInResponse|SearchTokensMasksKeyInResponse|GetTokenMasksKeyInResponse|GetTokenKeyRequiresOwnershipAndReturnsFullKey|GetTokenModelsUsesEffectiveGroupAndModelLimits|UpdateTokenGroup|TenantTokenGroup|TenantTokenStatus)'
go test ./common -run '^TestConsoleContract$'
```

### 10.2 前端

至少覆盖：

- 三页签切换、键盘操作、焦点恢复和移动端布局；
- 两个 preset 的展示值和创建 payload；
- 未勾选时不能创建并打开；手动创建必须有独立确认；提交中防连点；
- 请求中关闭/切换 preset/登出时，迟到结果不触发取钥、复制或唤起；同身份仍保留对应创建 ID；
- 明确成功读取准确 ID，不按名称猜测；
- 成功后模型查询、推荐模型失效、空模型和查询失败；
- 深链只在用户流程中构造；再次打开不重复创建；
- 读取明文 Key 只在用户动作后发生，并且不持久化；
- 创建超时、成功缺 ID、浏览器阻止、用户取消和返回 API Keys；
- 工具平台筛选、官方入口动作和链接审计；
- 七种已配置语言的关键按钮、状态、FAQ 和错误文案存在，尤其保留 zhTW。

定向命令模板（在 `web/default` 执行；`<changed-files>` 必须替换为本任务的精确文件列表，不原样执行）：

```bash
bunx vitest run src/features/usage-guide/__tests__/quick-setup-dialog.test.tsx src/features/usage-guide/__tests__/usage-guide.test.tsx src/features/keys/components/dialogs/cc-switch-dialog.test.tsx src/features/keys/components/dialogs/__tests__/cc-switch-dialog.test.tsx src/features/keys/lib/cc-switch-defaults.test.ts src/hooks/__tests__/sidebar-config.test.tsx src/lib/console-contract.test.ts src/hooks/use-console-contract.test.tsx
bunx oxlint -c .oxlintrc.json <changed-files>
bunx oxfmt -c .oxfmtrc.json --check <changed-files>
```

修改旧 CC Switch 组件/builder 时再加入 `src/features/keys/components/api-key-group-cell.test.tsx`、`src/features/keys/components/__tests__/api-key-listing.test.tsx`；修改概览时加入已有 `src/features/dashboard/components/overview/__tests__/setup-guide.test.tsx`。不要重复运行输入未变的已通过命令。

检查范围与授权必须准确：

- `bun run typecheck` 实际是整个 default 的 `tsgo -b`，目前没有配置好的单文件 typecheck；实施时需要运行才能声明类型检查完成，但必须先按用户成本规则说明精确命令、必要性、估计时长并取得广域检查授权。不能伪装成定向命令，也不能静默跳过后声称质量门禁全通过；
- `bun run format:check` 的现有包装脚本会临时改写并恢复全前端文件，不接受定向文件列表；本任务不默认运行，避免覆盖并行修改。先用上述只读范围检查，版权头差异不能通过删改受保护标识解决；
- locale 写入与 `bun run i18n:sync` 遵守 i18n skill，并核对只含本任务文案差异。`bun run i18n:check` 是全前端扫描，若需要执行同样先说明范围取得授权，不能因写了一项文案默认跑全局检查；
- `bun run build`、全仓库 `go test ./...`、不带路径的 `bun run test/lint` 均不属于本轮默认验证；任何预计超过五分钟或时长未知的命令也先说明并等待批准；
- 发布构建与强制 release gates 仅在用户另行授权发布后按 NewAPI runbook 执行。

本次实现按 L1/L2 范围运行了后端和前端定向检查；没有运行全仓库测试、完整构建或 `bun run typecheck`。

现有 `i18n:check` 只断言 en/zh 两份资源，不能以其通过证明七语言完整。实现时按本任务新 key 集合逐项核对七份文件，保留占位符、动态 catalog/static keys 和 zhCN/zhTW 运行时映射；不顺带补全整个仓库的历史翻译债务。

### 10.3 视觉与人工验收

- 使用设计稿逐项对照桌面、窄屏、暗色主题；
- 固定 1150px/1024px 桌面与 390px/320px 手机宽度，至少检查亮/暗主题、中文与英文；同状态截图对照归档设计稿，关键间距/字号按第 3.2 节，不以一个总体相似度数字替代审阅；
- 用真实无密钥测试数据检查 API Keys 列表、错误 toast 和状态恢复；
- 在受支持的 Windows/macOS 客户端上验证外部协议，记录未安装、取消、浏览器拦截和再次打开；
- 人工确认“已创建”“已请求打开”“已导入”“首次调用成功”没有混用；
- 不用真实收费请求做自动化测试；如需人工首次调用，明确用户主动触发和正常计费。

工具链测试不能代替客户端验收：用批准的测试账号/专用 Key，分别记录 Windows+Edge/Chrome 与 macOS+Safari/Chrome 的已安装、未安装、取消唤起、重试、导入后启用、配置切换行为。不得在证据截图中显示 Key/完整 URI；若某环境不可用，记录“未验证”及接手人，不写成已通过。

## 11. 交付清单

- [ ] 开发前确认两个 canonical group 在目标环境存在并可供目标用户使用；
- [x] 更新后端 `data.id` 合同与定向测试；
- [x] 实现认证路由、导航入口、概览入口和三页签页面；
- [x] 实现两个 preset 的创建、模型查询、标准 CC Switch 打开和手动配置恢复；
- [x] 实现未知结果、无模型、权限拒绝、余额、401/403 和浏览器阻止状态；
- [x] 工具 catalog 的平台/架构链接、官方域名、`checkedAt` 完成审计；
- [x] 更新当前七种语言和静态 key；
- [x] 同步记录 CC Switch 现有协议的产品安全决策；
- [x] 通过后端/前端定向测试、lint、format、i18n 检查；全量 `typecheck` 未运行；
- [ ] 完成桌面/窄屏/暗色/键盘及真实客户端唤起验收；
- [ ] 发布前按 NewAPI runbook 选择纯前端或后端/联合发布路径；本开发文档本身不授权构建、stage 或 promote。

本功能包含后端创建响应、权限和能力声明，正常实现属于后端/前端联合改动；先让后端具备能力再开放前端创建入口。若部署分两次，旧后端仅显示指南与管理入口，后端回退亦按同样逻辑降级。不能把本次完整功能申报成纯前端发布，也不需要数据库 schema 变更。

## 12. 需求追踪与原型补全

| 编号 | 需求/决策 | 开发要求 | 可观察验收 |
| --- | --- | --- | --- |
| UG-01 | 使用说明模块 | `/usage-guide`、侧栏与概览入口、三页签 | 登录态可用；隐藏规则/租户过滤生效；其他导航不变 |
| UG-02 | 常用 AI 工具说明下载 | 六工具卡、用途、平台/架构、官方来源、独立接入教程 | 按系统筛选正确；页面/安装包按钮语义真实 |
| UG-03 | GPT 一键配置 | name/group 精确为 `GPT-稳定`，额度不限 | POST payload 与落库记录一致，得到本次 ID |
| UG-04 | Claude 一键配置 | name/group 精确为 `claudecode-满血企业`，额度不限 | 不误选 Desktop/GPT app，旧 Key 不修改 |
| UG-05 | 安装并打开前提 | 显示下载入口与确认勾选 | 未确认不创建并打开；手动创建独立确认；手机不自动唤起 |
| UG-06 | 主动打开 CC Switch | 推荐可用时创建后自动尝试一次 | 只一次创建与一次自动打开；失去手势仍可手动再次打开 |
| UG-07 | 精确模型/端点 | 按 Key 查模型、复用推荐、统一端点解析 | 不存在的模型不导入；推荐失效走选择；无模型与加载失败不同 |
| UG-08 | Key 交付安全 | 已接受标准 URI；短期内存、无持久化/日志 | 合成 secret 不出现在本站地址、日志、toast、缓存与截图 |
| UG-09 | 故障恢复 | 明确失败/未知/成功缺 ID/取钥失败分支 | 恢复仅作用精确 ID；未知请求不重 POST |
| UG-10 | 手动/已有 Key | 真实复制、可信参数、`/keys` 入口、Cherry 教程 | Clipboard 成功才提示；旧 Key 不认领不升级额度 |
| UG-11 | 质量与还原 | token 组件、七语言、可访问性、截图对照 | 规定宽度无溢出；键盘/主题/多语言通过；无缺漏状态 |
| UG-12 | 兼容性 | 可选 `token_create_result_id` | 旧后端/回退可读指南，创建入口正确降级，其他控制台可用 |

| 原型中的模拟/简化 | 正式实现必须补齐 |
| --- | --- |
| `created[preset]=true` 立即成功 | API 成功与 ID 校验、业务拒绝、未知结果、迟到响应隔离 |
| 手动页静态“选择模型” | 真实 token 模型查询、推荐交集、选择控件、加载/空/错误状态 |
| 点击“再次打开”只改文案 | 按已授权操作读取 Key，构造一次真实 deep link，重试不创建 |
| 占位地址和遮罩 Key | 运行时站点地址、受保护取钥、真实 Clipboard 结果 |
| 同一工具各系统共用官网 URL | 真实下载页/安装包/架构语义与校验日期 |
| 独立模拟侧栏和交互设计稿标记 | 保留现有控制台 shell，移除“交互设计稿”和所有预览示例字样 |
| 只有准备与结果弹窗 | 增补能力缺失、模型选择、取钥失败、未知创建结果、管理入口 |
| mock 支持选择紧凑/列表外观 | 固定默认舒展卡片布局，不将 Tweak/模型宿主 API 带进产品 |

## 13. 实施顺序与评审结论模板

1. 读取当前相关文件和并行改动，确认默认模型 helper、七语言配置、租户路由规则的最终基线；不覆写别的任务。
2. 完成 AddToken ID 响应、创建权限校验、可选能力声明与最小接口测试；不引入新 endpoint/schema。
3. 用现有组件实现静态页面和工具目录，先对照截图定稿布局，再接状态流；不要把 mock 复制进生产。
4. 接入创建→模型→取钥→唤起链路、同页复用、恢复和旧后端降级，迁移共享 builder 后验证原 Key 页行为。
5. 补齐七语言、屏幕宽度、键盘、暗色、错误状态和真实客户端检查；完成第 12 节逐项证据。
6. 独立复核风险：权限/租户、准确 ID、Key 数据去向、错误重试、原功能回归、设计差异；结果明确区分通过/未验证/阻断。

实施完成报告逐项列出：改动文件、UG 编号、定向命令与结果、广域检查授权及结果、截图/客户端版本、已知并发限制、尚未验证项。所有 `[ ]` 只有真实证据成立才能勾选。文档完整不意味着实现或代码质量已经验收。

## 14. 设计资产与文档验收记录

本次归档保留原设计文件，不修改交互稿中的模拟业务。`preview.html` 为原片段的独立浏览器包装，桌面截图使用 1150px 视口。正式页面已接入真实路由、Key 创建/模型查询/CC Switch 唤起和手动恢复；设计稿截图、真实登录态、客户端协议和生产发布仍按第 10 节单独验收。

| 文件 | SHA-256 |
| --- | --- |
| `usage-guide-design/prototype.html` | `c2996de51fea6cc09e4f3e5d27326fa972743714c474f020783748343e0dc6ba` |
| `usage-guide-design/preview.html` | `a5e703739ea75909e3add4eae2dac27bf6f40faccd566c6bda77d7a9233d1d73` |
| `usage-guide-design/desktop.png` | `74ca4eb4147b89d4568f7385dd25529b9f837794a0453189b5bb95d27f491856` |

定向检查结果已记录在实现交付中：后端 controller/common 测试、前端 usage-guide/CC Switch 测试、oxlint、oxfmt、`git diff --check` 和 `bun run i18n:check` 均通过。全量类型检查、完整构建、真实 Key、操作系统客户端和生产发布验证尚未完成；不以定向检查代替这些验收。

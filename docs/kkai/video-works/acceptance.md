# 视频作品：验收与代码质量门槛

实施后的实际命令、结果、截图与未覆盖边界见 [实现与验证记录](implementation-review/README.md)。下文保留开发前验收合同，候选命令不代表已执行。
本文是后续实现的验收合同，不是已通过的应用测试报告。本轮仅交付文档；没有运行应用测试、构建或生产操作。

## 1. 验收基准与证据分层

- 视觉基准：[桌面深色](design/desktop-dark.png)、[手机](design/mobile.png)、[作品预览](design/preview.png)、[首次空态](design/empty.png)、[加载重试](design/retry.png)、[桌面浅色](design/desktop-light.png)。
- [扣费抽屉参考](design/billing-reference.png) 是早期同布局补充，不是最终视觉基准；账务字段/文案服从后端契约。归档的 `design/prototype.fragment.html` 保留最终原文，原文中的演示业务逻辑不构成实现契约。
- 最终原型用于确认排版、层级、素材和交互意图。原型的播放、下载、空态切换及静态账务数据不证明应用功能已经实现；日期选择在原型中也没有执行真实日期过滤。
- **P 层：原型验证**只证明设计稿能展示。**F 层：真实 React 页面 + 固定接口夹具**证明页面集成、视觉与前端行为。**I 层：本地/测试后端 + 已留存媒资**证明权限、接口、媒体和数据语义。F、I 层均须有证据，P 层不能替代。
- 复用已存在的测试与组件；不把 HTML 原型脚本、模拟播放器、示例用户或金额直接移入产品代码。项目全局导航沿用真实壳层，视觉比较重点是本功能内容区。

## 2. 固定夹具与截图条件

F 层固定时区 `Asia/Shanghai`、语言 `zh`、时间 `2026-10-27 10:44:06 +08:00`、默认每页 20 条、同一字体和浏览器版本、DPR 1、缩放 100%。截图前等待字体和可见图片解码，关闭动画；不得用任意 sleep 掩盖未稳定状态。

| 夹具 | 生成状态 / 媒体 | 账务事实与预期 |
| --- | --- | --- |
| A | sora-2，文生视频，720p，8 秒，成功，有真实封面 | 冻结单价 $0.10/秒 × 8 × 0.8，当前扣费 $0.64；completed 默认显示“计费已处理” |
| B | sora-2-pro，图生视频，12 秒，成功，有真实封面 | 冻结单价 $0.25/秒 × 12 × 0.8，当前扣费 $2.40；completed 默认显示“计费已处理” |
| C | sora-2，生成中，无可信数值进度 | reserved 的当前 quota 为 $0.80，显示“已预扣/当前预扣”；不是原始预扣流水字段，最终扣费未知 |
| D | 生成失败，有可公开原因，无视频 | 持久化状态明确为已退款；当前扣费 $0.00 |
| E | 成功，但媒资明确报告链接过期 | 仍显示已完成、“计费已处理/当前扣费 $2.40”；媒体不可用不改变账务 |
| F | 成功，视频存在但无封面/封面加载失败 | 中性视频占位，仍可预览真实视频；不换成 AI 示例图 |
| G | 失败 + reserved；成功 + accepted；未知 billing state | 分别显示已预扣、待结算、账务状态未知；不按成功/失败推算 |
| H | 历史记录无计费快照 / 旧 pricing epoch / 未确认 epoch | 视频模式仍给出 billing.amount_available；转换可信则保留金额，false 显示“—”；说明明细不可用，不用今日价格补算。旧响应完全没有 billing 时也显示“—” |
| J | 同页混入其他用户和跨租户任务，普通用户/admin/root 三类会话 | 后端按权限裁剪；切换“仅我的”不得残留上一权限缓存 |
| K | billing state 为 completed，但来自异常恢复且无实际结算证明 | “计费已处理/当前扣费”；amount_available=false 时金额“—”，不映射成待结算/待核对 |

A/B 的原型封面只允许用于带“示例封面”标记的视觉夹具；I 层必须换成任务对应的授权测试视频/首帧。额外的 A-S/B-S“已结算”夹具仅在 API 增加可核验 settlement 证据后启用；目前 completed 默认没有该证据，不要求应用复刻原型中的“已结算”。原始预扣、退回、最终金额只有 API 提供可核对事实时才出现在流水中；C 的当前 reserved 金额不能填充未来流水。缺字段的真实接口明确降级，不从本表反推数据。

| 截图集 | 固定视口（CSS px） | 必拍场景 |
| --- | --- | --- |
| 桌面 | 1024×1100，深/浅色 | A–E 列表、A 预览、扣费抽屉、空态、搜索无结果、重试 |
| 手机 | 375×812，深/浅色 | 同一列表全页、预览及扣费全部内容、底部动作、长文案 |
| 窄屏 | 320×812 | 无横向溢出，筛选、金额、任务 ID 和弹层动作均可到达 |
| 断点 | 740/741、900/901×1000 | 表格/卡片、侧栏/移动壳层切换不丢内容；实现断点不同则记录并补测相邻 1px |
| 宽屏 | 1440×1000 | 内容宽度受控，无异常拉伸或过度留白 |

深浅色都包括成功、生成中、失败、过期和无封面状态。截图记录构建 commit、夹具版本、浏览器、视口、主题与截图文件名；全页截图高度可随内容增加。

浏览器视口与基准图片裁剪区域是两个概念：桌面深/浅色、预览、重试基准实际为 **1024×1074**，空态为 **1024×748**，手机为 **375×2429 全页**。1024×1100 与 375×812 是测试视口；叠加/像素比较须对相同内容区按原尺寸裁剪，不能把 1074 拉伸为 1100，也不能把空态或手机全页缩放为视口高度。扣费抽屉先人工复核语义和布局，待正式实现审查后建立其独立基准。

## 3. 高还原度门槛

1. 相同内容区宽度和夹具下对照基准逐区检查：页头插画、工具栏、状态栏、封面/模型、金额、操作、预览、扣费抽屉。桌面封面 144×81、16:9；手机封面铺满卡宽；状态图保持比例。不得拉伸、裁掉主体或用一张插画替代全部状态。
2. 常规间距、圆角、字号偏差目标不超过 2 CSS px；封面、卡片/行与弹层主要尺寸偏差目标不超过 4 CSS px。真实全局壳层、原生媒体控件差异单独记录，不用改造整个后台来追平原型。
3. 同浏览器、字体、数据的内容区截图可作叠加/像素比较，差异像素目标 ≤2%；仅可屏蔽播放画面、光标等真正不稳定区域，不屏蔽金额、状态、按钮和正文。该阈值是复核触发器，不能代替人工判断或容许局部明显错误。
4. 人工必过：第一眼可识别作品、模型、当前扣费/预扣；蓝色主操作明确；文字无压缩、重叠、意外换行或低对比；图标粗细统一；深浅色图片透明边缘干净；加载过程中不明显跳动。账务文案以真实契约修正原型属于必要语义校正，必须记录，不为像素还原保留错误含义。
5. 手机不得退化成横向滚动桌面表格。模型与金额并排、底部工具条常驻卡内；触屏动作热区至少 44×44 CSS px，图标可维持稿中视觉大小。
6. 视觉偏差须附基准/实现对照图和原因；仅字体抗锯齿差异可直接记录。改变主层级、布局或素材须回到设计审查，不能自行更新基准消除失败。

真实封面是已批准视觉方案的核心。正常托管成功视频复用 Video Studio 现有输出资产的 poster 链路，核对任务关联、所有者授权和投影；不另建截图服务，不由列表批量拉视频抽帧。对应 poster 已就绪时必须展示真实首帧；旧任务、未托管或 poster 尚未就绪才走占位降级。该链路未接通不能将全部成功任务改为占位后宣称高还原度完成。

## 4. 功能与边界验收

| 编号 | 操作与期望结果 | 证据层 |
| --- | --- | --- |
| V01 | 入口、标题和相关设置显示“视频作品”；刷新与旧入口深链仍可打开；管理员全部/本人范围与普通用户本人范围正确 | F + I |
| V02 | 状态、日期、任务 ID/模型查询、渠道筛选及分页组合有效；修改筛选回到第一页，清空恢复默认；总量/状态计数遵循接口定义，不能用当前页条数冒充全量 | F + I |
| V03 | 首次无作品用空态插画和可访问的视频工作室入口；筛选无结果保留筛选器和重置；请求失败有错误/重试，不能伪装成空列表 | F |
| V04 | 成功作品点击封面/查看作品打开同一真实视频，能播放、暂停、seek、静音、全屏；长竖屏视频在容器内保持比例；关闭弹层停止播放 | F + I |
| V05 | 正常托管输出通过现有资产 poster 显示正确作品；无对应任务读取权不能取封面，管理员窄范围权限见下文。无封面、封面 404、媒资数组为空、result_discarded、历史 legacy 视频、多个制品分别有明确降级 | F + I |
| V06 | 网络超时/5xx 可重试且保持作品状态；Studio 短签名到期可重新获取授权 URL，不等于资产删除。通用 403/404/解码错误不能无依据标为“过期” | F + I |
| V07 | 托管视频使用 hosted_video.download_url 的现有 attachment 短签名；非托管 content 扩展 download=1 流式附件，验签名、解析及安全文件名。禁止前端 Blob 全量缓冲；复制完整任务 ID，剪贴板拒绝有反馈，不复制上游凭据 URL | F + I |
| V08 | 生成中无可信来源仅显示文案与辅助插画，严禁 10/20/30 或 68% 阶段占位；若未来支持数值，必须有明确来源契约并处理未知/异常值 | F + I |
| V09 | 可见页含 active 生成记录或 media.state=processing 时每 10 秒刷新；仅剩待处理账务时每 15 秒刷新；两者共存采用 10 秒，最多一个在途查询。隐藏/离线/卸载暂停，生成、媒体处理与账务均终态停止；切页/作用域忽略旧响应；轮询不生成假进度 | F + I |
| V10 | 模型取任务保存的请求模型；缺失时按接口约定回退，仍缺失显示未知。不得用插件名/当前渠道模型冒充；长模型/长 ID 可完整读取和复制 | F |

### 托管媒体：关联、授权与签名

- 按 [后端契约 §7.1](backend-contract.md#71-托管视频正常路径复用-studio不新增签名-endpoint) 验证数字 `Task.ID → KKAIVideoGeneration.TaskID → output link → asset`；构造公开字符串 task_id 与数字 ID 不对应的夹具，防止错误关联。主输出 role/position 正确，重复主输出、错配不能随便选首项。
- `generation.UserID = Task.UserId = asset.OwnerUserID`、scope=user、kind=output 同时成立。普通用户只能读取已授权本人/租户任务；平台任务管理员可在既有任务管理权范围查看他人视频，必须仍验证所有者链与 Studio 访问策略。
- 原 `/api/studio/media-urls`、asset content/download 的 user-scope owner 保护保持原样。管理员任务特例只在任务投影中生效，不能拿该权限请求任意 asset ID 或扩大原 Studio 接口。
- deleted generation/asset、重复或错配输出、restricted、签名失败逐项返回明确不可用状态且无 URL；其余正常行继续展示。整批媒体读取失败保留任务列表并返回 `data.media_warning=media_temporarily_unavailable`，不伪装为空态。存在托管记录或 AssetHostedResult 时，失败不能回退到私有上游 URL。
- 列表新增 media 摘要只消费已有 inspect/poster；normal ready 且 PosterObjectKey 存在时必须有真实海报。主数据查询之外至多三次批量 DB 读取 generation/output links/assets，行数增加不增加查询批次；查看者与本页所有者通过一次批量身份查询核验，所有者禁用/不存在时管理员也不获得签名。
- CanAccess 控制读取资格，Worker/Archive/Backfill 开关控制处理作业；关闭处理作业不能阻止仍获授权的用户读取已 ready 的资产。签名配置不可用则准确降级，不擅自开启处理开关。
- 列表不发远程 Head、不取视频、不调用插件投影、不触发归档、回填或截图；不新增 endpoint、数据库迁移或独立截图服务来满足本页。
- row.media 存在时 source=studio、state 必传，poster_url/poster_expires_at 成对；打开作品后现有 artifacts 接口的 hosted_video 必含数字 task_record_id/state，并核对 task_record_id=row.id。只有 ready 可含 content_url/download_url/expires_at，其他状态禁 URL；授权范围 public task_id 不唯一时 404 fail closed。
- hosted_video URL 严格解析与通用 task capability 分开。width/height/duration_seconds 来自实际 inspect，不能用账务倍率补齐。
- `media.poster_expires_at` 与 `hosted_video.expires_at` 只表示短签名到期。用可控时间验证授权仍有效时重取投影并恢复封面/播放/下载；自动续签最多一次，签名算法可能复用相同文本，不能断言 URL 字符串必须变化。授权撤销则停止；不得标记资产删除、重生成或反复重签。通用长期 content_url 没有此续签保证。

### 账务和权限：任何一项失败即阻断交付

- 金额使用现有额度/货币转换规则；列表与抽屉同源。检查零值、小额精度、非 USD 展示设置、旧 epoch 和会员/订阅扣费；禁止前端自行二次换算。
- 账务状态采用后端定义的公开语义，独立于视频成功、失败、过期和网络错误。`FAILURE + reserved` 仍是已预扣，`SUCCESS + accepted` 仍是待结算。`completed`（含异常恢复）统一“计费已处理/当前扣费”，不能凭该值宣称已结算，也不改成待核对/待结算；金额不可用按 amount_available=false 显示“—”。“已结算”需要额外可信 settlement 证据。
- 明细只展示冻结的单价、表达式、用量和倍率；修改当前价格后历史账单不变。size/duration/resolution 倍率不能错误显示为像素、秒或金额。
- reserved 的当前 quota 可以标“当前预扣”；它不证明原始预扣流水。原始预扣、已退回、最终扣费需要对应事实字段。只拿到当前 quota 时不生成资金流水；不能从“预扣 − 当前金额”猜退款，也不能把会员节省计为退款。缺失信息展示“未记录/不可用”。
- 对照 A 的 $0.64、G 的未结算、H 的历史缺省态；公式、金额和状态不能互相矛盾。单位/参数不完整时隐藏不可证明的公式，保留有依据的金额。
- 普通用户接口/DOM/网络响应均无 admin/root 专属字段、渠道密钥、内部节点、私有上游任务 ID、原始响应及未经脱敏的错误。管理员“仅我的”也按本人视图处理。
- 用另一用户/租户 task ID、重复历史 task ID、失效用户、失效签名访问列表、明细和媒体地址，后端拒绝；前端隐藏按钮不能充当鉴权。签名链接不写入持久存储或分析日志。
- 通用 task content_url 无过期字段，404 还会遮蔽无权、不存在或上游失败。Studio 的 poster/hosted_video 有短签名期限，但期限不代表作品寿命。仅有明确链接失效证据时展示专用状态与重新加载入口，保持任务/费用信息，不宣称资产已删除。
- 只读页面的查看、重试、下载、刷新不得发起重新生成、退款或扣费；模拟这些操作前后账务记录不变。

### 国际化、无障碍、性能

- 七语言 `en / zh / zh-TW / fr / ja / ru / vi` 均覆盖新增文案、工具提示、aria 标签、错误及空态；原地切换立即更新，无裸 key，插值一致。至少对 ru/fr 长文案在 375px 实测，日期和货币遵循现有格式器。
- 键盘可操作筛选、预览、下载、复制和抽屉；焦点可见，弹层焦点受限，Escape 关闭，关闭后回到原触发器；预览转扣费不能留下第二个可交互遮罩。正文对比度 ≥4.5:1，状态不只依靠颜色，装饰图有空 alt。
- 200% 缩放可阅读并到达全部动作；`prefers-reduced-motion` 下不靠旋转动画传达状态。手机使用真实浏览器验证视频 controls、滚动和关闭，不能仅用 JSDOM。
- 初始列表不逐行请求制品详情、不下载视频、不自动播放；只请求列表需要的封面，屏外封面懒加载。打开一条作品才请求它的媒资；点击去重，失败重试有界，关闭停止媒体活动。
- 网络记录比对 5 条与 20 条数据：除实际封面外，列表 API/详情请求数不随行数线性增长。保留取消/去重、查询权限键和服务端分页；不为此页新增图标库、状态管理库或视频播放器依赖。

## 5. 最小验证命令与前提

以下均为**实现后的候选命令，尚未执行**。先确认安装依赖可用、变更文件清单及近期同机耗时；测试使用隔离 SQLite/HTTP fixture，不使用生产数据库/真实付费生成。暖缓存定向测试预期秒级至数分钟；若实际预计超过五分钟或耗时未知，先说明精确命令、必要性与预计时长并取得批准。

| 改动范围 | 最小范围 |
| --- | --- |
| L0：本文/图片说明 | 差异审查、文档链接/资源核对、`git diff --check -- docs/kkai/video-works`；不跑应用测试 |
| L1：作品 UI、预览、扣费组件 | 下列相关 Vitest、受影响 TS files 的定向类型/lint、F 层浏览器验收 |
| L2：列表 DTO、公开账务/媒资接口或共享组件 | L1 + 下列直接相关 Go 测试 + I 层接口验收；依实际改动补直接依赖回归 |
| L3：迁移、计费写入、鉴权机制、依赖/锁文件 | 超出本页常规展示实现；重新列风险和最小验证计划，不默认启动全量检查 |

以下命令只选取**当前已存在**的测试/文件，不能据此宣称新增契约已有覆盖。现有 task-billing 的 completed→Settled 断言须随公共语义更新，不能为保住旧测试保留错误文案。从 `web/default/` 运行；未改预览/权限部分时可进一步裁剪，新增测试实现后另加实际路径：

```sh
bun run test src/features/usage-logs/components/__tests__/task-artifacts-cell.test.tsx src/features/usage-logs/components/__tests__/task-billing.test.tsx src/features/usage-logs/__tests__/artifacts.test.ts src/features/usage-logs/__tests__/task-details.test.ts src/features/usage-logs/__tests__/access.test.ts
```

定向 lint 仅传实际变更文件。下式是已有主要组件的起点，新页面/卡片/测试文件实现后显式补入，不扩大成目录全量：

```sh
bunx --no-install oxlint -c .oxlintrc.json src/features/usage-logs/components/task-artifacts.tsx src/features/usage-logs/components/task-billing.tsx src/features/usage-logs/components/columns/task-logs-columns.tsx
```

定向类型检查用项目已安装的 `tsgo`，仓库没有专用 scoped-typecheck 脚本。先在 `web/default/.video-works-tsconfig.json` 创建临时配置：继承 `./tsconfig.app.json`、`include: []`、`files` 明列本次 TS/TSX 和 `src/env.d.ts`、`src/tanstack-table.d.ts`、测试需要的 `src/test-setup.ts`，设置 `incremental: false`。保持 strict 等规则，不用 `any`/`@ts-ignore` 消除失败：

```sh
bunx --no-install tsgo --project .video-works-tsconfig.json --noEmit
```

保存临时配置内容作为证据，检查后删除。如果真实路由依赖必须引入 `main.tsx/routeTree` 导致范围接近全前端，不伪称局部检查；报告类型验证缺口，按全量批准规则处理。格式核对须保护版权头；现有 `format:check` wrapper 扫描全前端且不支持路径参数，不能冒充定向命令。

DTO/媒体契约改变时从仓库根运行相应测试，不涉及媒体时只保留 `TestTaskLogDTO`：

```sh
go test ./controller -run '^(TestTaskLogDTO|TestTaskListsOmitPersistedSnapshot|TestTaskArtifactAuthorizationKeepsForeignTasksHidden|TestTaskArtifactAccessRequiresActiveOwner|TestTaskArtifactAccessRejectsAmbiguousHistoricalTaskID|TestDashboardTaskArtifactsReturnsLegacyCapabilityWithoutUpstreamURL|TestLegacyVideoArtifactContentUsesGetResultURL|TestProxyTaskMediaForwardsRangeAndFiltersResponseHeaders|TestProxyTaskMediaPassesThroughUnsatisfiedRange)' -count=1
```

只有账务审计展示来源被修改时补充以下已有不变量测试；纯前端排版不运行它：

```sh
go test ./service -run '^(TestMembershipTaskSettlementLogsDoNotAttributeWholeTaskSavingsToRefundsOrDeltas|TestTaskBillingAuditLogDetailsKeepsManagedProviderReasonAdminOnly)$' -count=1
```

必须新增/补强的行为用例尚未实现，不能算作上述旧测试已覆盖：

| 待补契约 | 最小行为断言 |
| --- | --- |
| 列表与媒体摘要 | 服务端视频分类、筛选/计数/分页、数字 Task.ID 与 owner 链、管理员任务特例不扩大 Studio 接口、托管真实 poster、三次批量查询上限、逐项媒体失败隔离 |
| 托管短签名 | hosted_video 数字记录身份/state/URL 互斥契约，与通用 capability 分开校验；期限到后授权有效最多一次自动重投影恢复（不要求 URL 文本改变）、撤权拒绝；无远程探测/归档副作用 |
| download=1 | 非托管只允许该可选 query、保留签名鉴权、attachment 安全文件名、Range/HEAD/206 与流式取消；托管沿用现有 attachment 签名，大文件不进入浏览器 Blob 全缓冲 |
| 账务公开语义 | amount_available=false 显示“—”；异常恢复 completed 仍“计费已处理”；当前 reserved 与原始预扣流水区分；A-S/B-S 缺结算证据不得启用 |
| 页面交互/轮询 | 用户可见动作、作用域缓存隔离、10 秒 active/15 秒 pending 轮询、单个在途请求、隐藏暂停和终态停止；用假计时器/可控 Promise |
| 错误与媒体可用性 | 通用 404 不等于过期、重试重新取得授权投影、缺封面可播放、poster 失败回退、批量媒体错误保留列表并提示 warning、关闭停止播放；真实浏览器验证下载与原生播放器 |

无路径 `bun run test`、`go test ./...`、全量 lint/typecheck/build 均不属于本页默认验收。任何全量命令、耗时未知或预计超过五分钟的命令须先获明确批准；定向通过后停止扩测，失败仅重跑相关范围。翻译维护遵循 i18n 技能，但全仓扫描不能冒称定向验证。

## 6. 回归质量与交付记录

- 复用 [制品交互测试](../../../web/default/src/features/usage-logs/components/__tests__/task-artifacts-cell.test.tsx)、[账务展示测试](../../../web/default/src/features/usage-logs/components/__tests__/task-billing.test.tsx)、[媒资契约测试](../../../web/default/src/features/usage-logs/__tests__/artifacts.test.ts)、[公开 DTO 测试](../../../controller/task_log_view_test.go)。阅读 [前端脚本](../../../web/default/package.json) 核实命令。
- 现有 [手机字段测试](../../../web/default/src/features/usage-logs/__tests__/mobile-layout.test.ts) 只断言字段集合，不能证明高还原度或手机可用性。新增测试必须通过用户可见行为、可访问名称、请求与真实业务结果断言；不新增 CSS/class 字符串、源码文本或“复写实现”的测试。
- 新/重写 Go 测试使用 require/assert，显式初始化权限、数据库、价格 epoch 与缓存；异步前端测试用可控 Promise/假计时器，不靠随机值、重复循环或 sleep。
- 交付表逐项记录 `P/F/I`、测试编号、精确命令与退出结果、截图/网络证据、通过/阻断/未覆盖原因；明确“全套未运行”或获准后的实际结果。
- 必须保留的阻断项：作品与金额不突出或手机版走样；演示图冒充真实作品；假百分比；资金状态猜测；权限泄露；真实播放/下载失败；关键动作键盘不可达；用原型检查冒充应用验收。
- 本文不授权提交、部署、迁移或生产验证。真实渠道兼容、移动 Safari 媒体行为和正式环境签名/存储访问若尚未验证，必须作为未覆盖项列出，不以截图通过代替。

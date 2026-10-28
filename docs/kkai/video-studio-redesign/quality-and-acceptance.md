# 视频工作室开发质量与验收

版本：1.0 · 2026-10-28。依据：[PRD](./prd.md) 的 VS-01–VS-20、[UI 规范 v1.2](./ui-spec.md) 与当前工作区代码。本文件是未来实施的验收计划；本轮只核对文档、代码契约与命令入口，没有运行业务测试、生产构建或真实生成。

行为以 PRD 为准，色彩、圆角、排版、暗色与动效以 UI 规范 v1.2 为准。暗色原型可作为优先视觉验收画面，真实应用继续跟随现有主题设置；原型默认暗色不构成修改全站默认主题的要求。

## 1. 质量责任与停止条件

前端实施者对交互、身份隔离和查询生命周期负责；后端契约审查者核对付费动作、请求/回执、资产权限和公开错误；测试审查者核对可重复的行为证据；设计与无障碍审查者核对真实页面的呈现及可操作性。审查者需要读取最终 diff 和测试结果，不能用文档评审代替代码审查。

以下任一项未解决时不能将该批标记完成：可能重复发起付费请求；跨账号显示或写入私有数据；非 ready 资产进入生成；把未知/归档失败表述为已退款；因样式重构丢失草稿/上传；主动播放被正常签名续期重置；关键动作在键盘、320px 或 200% 文字下不可达。

不以测试数量、快照数量或覆盖率百分比作为本次通过标准。新增测试保护用户行为、付费安全或稳定契约，避免断言组件拆分、私有变量、CSS 类名及无意义循环。用确定的 API 响应、受控 Promise 与 fake timers 检查竞态和期限，不靠真实 sleep、随机压力测试或整页快照制造通过记录。

## 2. 现有证据与需要补齐的覆盖

当前测试框架为 [Vitest 配置](../../../web/default/vitest.config.ts) 中的 jsdom、React Testing Library，入口为 [package.json](../../../web/default/package.json) 的 `test: vitest run`。已有文件可以作为回归起点，测试文件存在不代表本轮已经运行或通过。

| 现有证据 | 已有测试关注的契约 | 本轮需增加的用户行为证据 |
| --- | --- | --- |
| `video-create-page.test.tsx`、`hooks/use-video-token-gate.test.tsx`、`video-token-access.test.ts` | 令牌准备、目录错误与真空区分，缓存模型刷新失败仍保留工作区 | 单实例编辑器跨布局、提交留页、首次历史失败不阻断表单、已读成果刷新失败保留 |
| `components/video-composer.test.tsx`、`video-domain.test.ts` | 样片和模型不串用、请求规范化、素材角色、false/0、报价时限与组件内未知锁 | 未决操作跨刷新、完整指纹、存储失败、改稿后改回、迟到回执、报价乱序；现有单个 composer 集成场景不足以证明这些能力 |
| `video-upload-protocol.test.ts`、`video-upload-lifecycle.test.ts`、`video-reference-hydration.test.ts`、草稿 store 测试 | 单次/分片上传、ETag/角色、恢复指纹、失效素材重验、账号草稿隔离 | 跨断点不重建 uploader；覆盖确认取消无损；恢复提醒与草稿互不覆盖 |
| `queries.test.ts`、`video-domain.test.ts` | history/head/detail 合并、活跃观察、删除墓碑、账号隔离、隐藏/终态停止轮询 | 创作页和库页选择稳定、新内容提示、30 秒发现窗口、错误旧快照和未决恢复联动 |
| `components/video-generation-card.test.tsx`、样片 gallery/preview 测试 | 卡片懒挂载播放器、终态删除、样片单活动预览 | 主成果/样片/详情双向互斥、健康播放器续签不中断、局部签名失败只恢复一次、删除后回焦 |
| `usage-logs/video-works/video-work-ui.test.tsx`、该目录 API/hook 测试、`lib/__tests__/video-works-media.test.ts` | 视频作品独立计费状态、安全失败、媒体限制、身份/租户/范围隔离、签名恢复 | 若抽取共同呈现组件，两个消费者同时验证；不能把其 TaskLog、计费、artifact 查询契约移植为 Studio 契约 |

上表前五行文件均位于 [Video Studio](../../../web/default/src/features/video-studio/)，草稿位于 [video-studio-draft-store.test.ts](../../../web/default/src/stores/video-studio-draft-store.test.ts)。视频作品证据位于 [video-works](../../../web/default/src/features/usage-logs/video-works/) 与 [媒体契约测试](../../../web/default/src/features/usage-logs/lib/__tests__/video-works-media.test.ts)。

## 3. VS-01–VS-20 追踪矩阵

“定向测试”指在相关现有测试中扩展必要场景，或为新增状态模块添加小型测试；“浏览器”指实施后的真实页面与受控测试数据，不是再次打开静态设计稿。

| 需求 | 最小验收与反例 | 验证方式 / 对应画面 |
| --- | --- | --- |
| VS-01 权限与准备 | off/admin/all、准备、组不可用、令牌上限、无模型、请求失败分别呈现；准备时不闪现无模型；失败不伪装空目录 | token gate/access 与 create-page 定向测试；浏览器 D02/D09 |
| VS-02 模型/模式 | profile 只声明两模式时不得出现首尾帧；不兼容切换取消后输入不变；确认后仅有效参数/素材进入报价 | domain/composer 定向测试；D05 |
| VS-03 提示词/参数 | ASCII、多字节中文、emoji 在 8000 UTF-8 字节边界前后；显式 false/0 保留；高级必填错误自动展开并可聚焦 | domain/composer 定向测试；键盘 D01/D05 |
| VS-04 素材 | reference/reference_video/first_frame/last_frame 不互换；上传中/检查中不能提交；失败重选/续传不丢其他素材；服务端限制权威 | upload/hydration/composer 定向测试；D05 |
| VS-05 草稿 | 用户 A/B 草稿隔离；覆盖取消不变，确认只替换约定输入；切成果不写草稿；刷新恢复失效素材仍需验证 | draft/domain + 页面交互；D01/D05 |
| VS-06 报价 | 输入变化立即失效；迟到旧报价不能覆盖新报价；过期/失败禁提交并给原因；金额与币种使用服务端展示契约 | composer/domain 定向测试；D01/D09 |
| VS-07 提交 | 双击/Enter 不重复发 POST；明确受理后留页并选择目标，草稿保留；继续编辑必须由下一次明确点击提交 | composer/create-page 定向测试；D03 |
| VS-08 未知恢复 | POST 前持久化；存储失败零 POST；刷新/断网/超时/解析失败保持未决；改稿后改回不能换新身份；仅看到 queued/processing/failed 不清未知；只读恢复零生成 POST | 新未决状态模块与 composer 集成，见第 4 节；D04 |
| VS-09 任务定位 | task_id 字符串不能当数字 generation ID；30 秒未找到仍保留回执，允许手动刷新；不能无限遍历历史或自动再生成 | 目标发现定向测试、fake timers；D04 |
| VS-10 选择/多任务 | 明确目标优先，主动选择不被后台完成抢走；目标未找到不拿其他成果冒充；活跃任务离开最新页仍被观察；刷新已知深链接目标不在 head 时仍更新至终态 | queries + 页面选择测试；D01/D03/D09 |
| VS-11 阶段 | queued/processing/archiving/ready/failed 各自真实；无可信数值无百分比；归档不提前播放下载；旧成果等待背景有标签 | 领域/呈现测试；D03/D04 |
| VS-12 播放 | 横竖方 contain；仅当前成果播放器挂载，关闭/切换/离页释放；主成果/样片/详情两方向互斥；触屏明确预览，无默认有声播放 | 媒体生命周期定向测试 + 真实媒体浏览器；D01/D06 |
| VS-13 媒体恢复 | 正常续签不改变健康播放器播放位置/暂停状态；失效只自动恢复一次，相同 URL 文本仍可恢复失败播放器；手动重试不生成 | 媒体状态定向测试 + 浏览器；D04 |
| VS-14 灵感 | 模型/分类能力、分页、动态预览沿用；无 capability 不显示分类；套用有覆盖保护；无样片不阻断编辑 | sample/domain + 页面交互；D02/D05 |
| VS-15 作品库 | status 仅空/ready，在服务端分页前筛选；日期分组只基于已加载记录；新内容提示不移动当前浏览位置；分页失败保留旧项 | queries + library 行为测试；D06/D09 |
| VS-16 删除 | 仅 ready/failed 显示删除；确认明确不取消上游/不承诺退款；失败保留项；成功清播放器/选择/缓存并回焦；迟到响应不复活 | queries + 删除交互；D06 |
| VS-17 任务页 | 公开失败原因与可复制任务号；ready 才有作品入口；刷新失败保留已读状态并说明未更新；不伪造全历史统计 | tasks 交互测试；D07/D09 |
| VS-18 响应式/导航 | 320px、断点两侧、侧栏开合、刷新/返回/深链接；一个编辑器保持草稿和上传；移动锚点不抢输入焦点；无权限/删除/未定位区分 | 页面定向测试 + 真浏览器；D08 |
| VS-19 身份/安全 | 切账号立即隐藏旧媒体、清选择/私有缓存；A 迟到回执不写 B；未授权目标不闪旧结果；公开错误不含凭据/内部诊断 | queries/store + 页面集成，见第 4 节；D04/D09 |
| VS-20 无障碍/i18n | 模态焦点、Esc、回焦、错误关联、一次播报、≥44px、文字≥4.5:1、必要轮廓≥3:1、减少动态效果、七语言 | RTL 语义交互 + 浏览器人工审查，第 5 节；D08/D09 |

## 4. 必须单独审查的安全和状态场景

### 4.1 未决提交与费用边界

建立受控 API fixture，分别模拟：明确受理、明确拒绝、发送后断网/超时、成功状态但无效响应、unknown、有/无 task_id。断言用户看到的恢复动作和生成请求次数，不能只断言 store 中多了一项。

1. 记录持久化完成后才允许首个 POST；写入抛错或存储不可用时阻止发送。记录只存操作身份、完整规范化内容摘要、回执和时间；不落 API 密钥、签名 URL、File/Blob。
2. 首个 POST 发出即卸载并重新挂载，未决提醒仍显示。切成果、改模型、套样片、编辑后改回不能抹除保护。参数键插入顺序、合法素材数组排列不同但含义相同，仍识别为等价请求；token、有效 group 或 role→asset 配对真正变化必须体现在指纹中。交换首/尾帧资产应改变指纹，仅重排完整角色条目不改变指纹。
3. 报价更新/5 分钟签名过期不换操作身份；超过视频 24 小时默认幂等窗口仍保留只读核实，不能自动清除或重放。不用本地时钟承诺服务端幂等记录仍在。
4. 有 task_id 只按既有有限列表发现/已知 generation 查询；无 task_id 不捏造精确匹配。核实、手动刷新、页面重入均不能调用 POST `/pg/videos`，token/group 失效不换令牌重发。
5. A 提交尚未返回时切 B，A 的迟到成功/失败不能覆盖 B 草稿、选择、提醒或缓存；追加 A→B→A 场景，不能仅凭 userId 又相同就让旧回执操纵新会话当前目标/表单。原 owner 的操作证据若需更新，只能定位原 operation，不触发新会话导航或自动播放。多标签页同时提交不宣称已做到原子互斥，文档与界面不得暗示这一保证。
6. 明确拒绝只按 [接口与状态契约](./api-and-state-contract.md) 的本次接口、HTTP status、应用 code 白名单判断；未知 4xx/5xx、`video_studio_conflict`、畸形 2xx 均保持保护，不从 message 猜测。回执 `id/task_id` 冲突、违反现有 `task_` 格式/128 字符限制、unknown/error 与 ID 同时出现时仍待核实。服务端 provisional generation 可早于上游受理存在：关联 task 的 queued/processing/failed 不结束 unknown；同 task 的 archiving/ready 才提供当前契约下可闭环的成功证据。只命中相似 prompt/时间、未找到、404 或删除均不构成解锁证据。
7. 读存储失败、JSON/版本无效不能当空集合覆盖原数据；操作成功后写回失败也不能先在内存解除等价保护。容量不足阻止新操作，不淘汰尚未决议记录。指纹计算期间同步门闩已生效，双击不能并行创建两个操作身份。
8. 指纹摘要等异步计算返回后、真正 POST 前再次校验 owner、会话世代、token/group、冻结请求快照与报价期限。fixture 让摘要等待期间发生切账号、退出后同账号重登或报价过期，均不得带旧上下文发送；计算使用的请求不能在 await 后混入另一版字段。尚未发送的 sending 记录仅凭本地确切“未调用 POST”证据安全处理，不把所有等待失败一概当作已发送。

新增持久化未决状态涉及持久数据与并发，按项目 L3 风险审查；这不自动授权全套测试，也不意味着需要数据库迁移。当前 composer 的 ref 锁与草稿 store 测试不能代替上述覆盖。

### 4.2 查询、选择、删除

使用两个页的已知记录、离开 head 的活跃记录、乱序 detail 响应与删除墓碑，观察最终可见记录及请求行为。列表首读失败、成功空列表、筛选空、已有数据后台失败是四种不同 fixture。后台刷新失败保留旧列表并标记状态未更新，不照搬视频作品页面的错误分支导致整列消失。

删除确认提交前再次核对当前可见状态，仅 ready/failed 允许发出 UI 删除请求；本地已知状态不再可删时关闭危险入口并重新读取。当前后端未强制终态删除，不能伪造“服务端已拒绝活跃任务”的验收；若接口确实返回权限/不存在等失败，保留失败语义和原项，不乐观移除。成功后停止相关播放、清选择/观察与媒体关联，并防止迟到 detail/head 恢复已删除项。明确目标已无权限/已删除时显示目标错误，不能悄悄选择另一作品冒充成功定位。

Studio 轮询复用 `queries.ts` 的 history/head/observed detail 机制；验证页面隐藏后不再产生周期任务查询、恢复可见后重新查询，终态停止任务轮询。允许已发出的网络响应正常收尾但不得跨身份应用；不要因为视频作品 hooks 支持 AbortSignal 就在文档里声称 Studio 已能取消每个在途请求。

当前 `useVideoGeneration(id)` 本身不轮询。刷新已知活跃 generation 深链接且该记录不在 head/已加载历史时，须把验证后的详情加入现有观察入口，沿用同一详情 query key 更新至终态；验收无第二套轮询、未授权目标不被登记、目标终态后不继续任务轮询。单次详情读到 processing 不能算深链接恢复完成。

### 4.3 媒体与复用原契约

健康播放与错误恢复分开验证：正常签名更新时不按 URL 重挂载 video、不重置 currentTime、音量或 paused；已经失败的媒体允许在一次续签后建立新的加载尝试。自动尝试失败后只能由明确点击再次重试；主播放器、样片预览与详情的激活要互相暂停，删除/换账号/离页释放 src。

`useStudioMediaUrls` 的签名、按用户 query key、批量规则由 [studio-media.ts](../../../web/default/src/lib/studio-media.ts) 负责。当前 key 根为 `studio-media-urls`，没有接入 [private-query-cache.ts](../../../web/default/src/lib/private-query-cache.ts) 的 `private-user` 清理边界；queryFn 也没有向签名请求传 AbortSignal。因此“按用户分 key”只能证明查询隔离，不能证明切账号已清除旧签名缓存/取消请求。

前端方案限定小范围修补：迁入已有 `privateUserQueryKey(userId, 'studio-media-urls', ...paths)`，将 queryFn signal 贯穿批量签名与失败后的单路径请求；取消后不得继续展开 fallback。共享接口变更具有 L2 的直接消费者影响面，此次还涉及授权拒绝与私有缓存，须按 **L3 安全门槛**审查；定向覆盖两个工作室、A→B 清缓存/隐藏媒体和 A 迟到响应，不自动扩大为全套检查。不能借机重写整个签名器。当前没有专门的 `studio-media` 或 `private-query-cache` 测试文件，需增加必要边界测试，不能报告为已有覆盖。

还须验证失败信息不被当前泛化 catch 吞掉：批量失败时仍可逐路径降级；单路径 403/404 形成明确不可访问证据（计划 `deniedPaths`），401 使整体身份失效，abort 不进入 fallback。真实 QueryClient 测试混合“允许路径＋403/404路径”、全局401、临时网络失败三组场景：被拒路径立即从新旧签名/海报/健康播放器 src 清除，允许路径继续可用，暂时网络失败标记未更新且不能误判为撤权。保留健康 src 以免正常续签重置播放，必须让位于明确撤权、删除和身份变化；图片消费者同样遵守，不能只测视频新播放器。特别检查图片成果 `presentedImage`、作品库 `wallSnapshot`/详情、样片/管理样片，以及视频素材 uploader 的本地展示快照，不得绕过拒绝路径继续渲染旧媒体。

图片消费者已有 [image-library-page.test.tsx](../../../web/default/src/features/image-studio/image-library-page.test.tsx) 与 [image-creation-results.test.tsx](../../../web/default/src/features/image-studio/components/image-creation-results.test.tsx)，但均 mock 了签名 hook。这两者用于消费层回归；新的真实 hook/QueryClient 身份生命周期测试才证明签名缓存清理、请求取消、晚响应不回写。还需核对图片样片/管理样片仍按同样接口取得允许媒体，避免签名失败让已读内容整体消失。

参考视频作品应保留的原契约：TaskLog 数字记录 ID 与公共 task_id 不可互换；admin/self/tenant 身份隔离；受限媒体不请求 artifacts；托管结果不可回退至上游 URL；金额不可得不显示 0，失败不编造退款；失败播放器相同 URL 的更新租约仍能恢复。如果仅参考样式、不改视频作品或共享组件，无需运行其整组测试；若抽取共同组件/媒体工具，必须运行双方实际消费者的相关测试。

## 5. 视觉、可访问性、国际化与性能

### 5.1 真实页面视觉验收

以 UI 规范 D01–D09 构建受控场景，覆盖浅/深主题，1440/1024/768/390/320px 与控制台侧栏展开/收起；另检查内容容器 850px 断点两侧，不把视口宽度直接当容器宽度。优先记录暗色 D01、D04、D05、D06、D08。使用实际 16:9、9:16、1:1 视频验证完整画面；示例封面只用于演示，不能作为生产兜底结果。

检查长提示词/任务号/公开错误、多语长标签、200% 文字缩放、短视口、软键盘和安全区。每种场景保留截图或人工结论和对应版本；视觉通过不是后端/计费通过。纯色值存在、页面无横向溢出也不足以证明层级和动效达标。

### 5.2 可访问性与动效

用键盘完成模型、模式、素材、折叠、提交、选择、详情与删除；读取 label、描述、错误关联和 `aria-current`/选中语义。确认弹层初始焦点不在破坏性动作，焦点限制在模态内，Esc 关闭，取消回原触发项，删除回相邻项或列表。受理/完成/失败每次状态转变只播报一次，轮询不重复播报；颜色不是唯一信号。

按真实渲染背景测量正文/有效控件文字至少 4.5:1，必要轮廓/焦点至少 3:1；暗色主色按钮使用深色前景，不能把普通白字直接放在浅靛蓝上。交互目标至少 44×44px，手机输入 16px，禁用原因保持可读对比度。

实际触发模式切换、作品切换、高级区/灵感开合和弹层，确认 160–260ms 的局部反馈不改变整页位置，不让控件跳动。`prefers-reduced-motion: reduce` 下关闭位移、缩放、循环和自动预览，状态仍有文字/图标。生成阶段不伪造百分比/剩余时间；只在真实进行中状态使用克制循环。jsdom 不证明真实焦点可见、颜色对比、播放器能力或软键盘可达，须浏览器补证。

### 5.3 国际化的实际范围

[运行时配置](../../../web/default/src/i18n/config.ts) 当前注册 **七个**语言：`en`、`zhCN`、`fr`、`ru`、`ja`、`vi`、`zhTW`；文件分别为 `en.json`、`zh.json`、`fr.json`、`ru.json`、`ja.json`、`vi.json`、`zh-TW.json`。历史规范写的六语言不能覆盖本工作区实际运行范围。

新增文案使用英文源文键和 `t()`；已有 Video Studio 的 `videoStudio.*` 键不为了本轮重构批量迁移。动态键要可被静态登记识别，检查七语言新增/改动键的值和插值集合，语种切换立即更新，不靠英文 fallback 掩盖缺译。人工重点检查繁体、法语、俄语和越南语的长标签及费用/恢复语义。

实施翻译前遵循 [i18n skill](../../../.agents/skills/i18n-translate/SKILL.md) 的脚本写入流程；写入集合必须覆盖实际七语言，不能照抄旧六语言数组遗漏繁体。该 skill 的同步步骤属于真实 locale 维护；本轮没有 locale 变更，不运行会改写文件的同步。[check-i18n.mjs](../../../web/default/scripts/check-i18n.mjs) 当前扫描整个 src 且仅强制 en/zh，不能充当七语言定向验证；未来只针对变更键核对七语与插值，再按经授权范围完成必要同步。

### 5.4 性能边界

沿用已有 React Query、Zustand、Base UI 和媒体能力，不为此改版增加播放器、状态机或动效依赖。浏览器 Network 验证同一操作无重复生成、无并行第二套任务轮询、不为每个历史卡片请求详情、不无限扫描历史。只预加载实际需要的素材；保留样片已有有界预热，不把“只挂载当前成果播放器”错误解释成所有样片预热必须删除。

主视频按需加载，缩略图 lazy/async 并保留稳定尺寸；主画面 contain，装饰图使用现有压缩资产，原始大 PNG 不作为日常页面资源。页面仅一个编辑器和 uploader 实例，切断点不卸载。通过 React Profiler/浏览器性能记录定位实际卡顿后再作 memo/虚拟化，不为了预设性能数字引入新框架。记录测试设备、场景与请求数量，避免报告没有测量依据的“提升百分比”。

## 6. 定向检查命令与审批边界

以下是**未来实施时按改动选择**的命令，工作目录均为 `web/default/`。路径已与本工作区文件核对；本轮未执行。不要一次运行全部命令来代替影响面判断。执行者须先确认本地依赖存在与耗时；任何耗时未知/预计超过五分钟的命令仍按用户规则说明命令、原因与预估，并等待授权。

表单、权限、报价、提交变化时，选择实际受影响文件：

```sh
bun run test src/features/video-studio/components/video-composer.test.tsx src/features/video-studio/video-domain.test.ts src/features/video-studio/video-create-page.test.tsx
bun run test src/features/video-studio/hooks/use-video-token-gate.test.tsx src/features/video-studio/video-token-access.test.ts
```

上传、恢复、草稿变化时：

```sh
bun run test src/features/video-studio/video-upload-protocol.test.ts src/features/video-studio/video-upload-lifecycle.test.ts src/features/video-studio/video-reference-hydration.test.ts src/stores/video-studio-draft-store.test.ts
```

查询、卡片和样片生命周期变化时，选择相关一行：

```sh
bun run test src/features/video-studio/queries.test.ts src/features/video-studio/components/video-generation-card.test.tsx
bun run test src/features/video-studio/components/video-sample-gallery.test.tsx src/features/video-studio/components/video-sample-preview.test.ts src/features/video-studio/video-sample-category.test.ts
```

共同组件/媒体工具涉及视频作品时，按实际消费者补选：

```sh
bun run test src/features/usage-logs/video-works/video-work-ui.test.tsx src/features/usage-logs/video-works/__tests__/use-video-works.test.tsx
bun run test src/features/usage-logs/video-works/__tests__/video-works-api.test.ts src/features/usage-logs/lib/__tests__/video-works-media.test.ts
```

共享签名器变化时，已有图片消费层最小回归命令为：

```sh
bun run test src/features/image-studio/image-library-page.test.tsx src/features/image-studio/components/image-creation-results.test.tsx
```

新增未决状态/选择/真实页面行为与签名生命周期测试的文件在实现后追加到对应命令，并在验收记录中写出实际路径；上面的现有文件集合不证明新增行为已覆盖。图片测试对签名 hook 的 mock 限制见第 4.3 节，不能把它们或视频作品测试当作共享签名生命周期已验证的证据。

受影响文件 lint/格式检查示例，实施时将列表调整成真实改动文件：

```sh
bunx --no-install oxlint -c .oxlintrc.json src/features/video-studio/video-create-page.tsx src/features/video-studio/components/video-composer.tsx
bunx --no-install oxfmt --check -c .oxfmtrc.json src/features/video-studio/video-create-page.tsx src/features/video-studio/components/video-composer.tsx
```

`--no-install` 避免检查时隐式安装。格式检查只读，不使用 `--write` 改动版权头。现有受保护头部可能与原始 formatter 输出不同：若发生，仅核对并记录头部差异，业务正文格式仍须修正，不能修改/删除项目与作者标识来追求格式通过。项目 `format:check` 调用的 [wrapper](../../../web/default/scripts/format-with-protected-headers.mjs) 会遍历全目录并临时写入后恢复，不能给它追加文件参数冒充定向检查。

`typecheck` 实际为 `tsgo -b`，不存在现成的单文件类型检查脚本；它覆盖前端项目，不得记作“局部 tsc”。后续 TS/TSX 变更需要类型验证，但本任务不执行。若没有经核对的局部项目配置，应把类型检查列为待批准门槛，说明准确的 `bun run typecheck` 及影响面；不能用漏掉路由注册/依赖的临时 tsconfig 制造通过结果。发布阶段的正式 runbook 门槛另外执行。

无路径 `bun run test`、`bun run lint`、`bun run format:check`、`bun run typecheck`、`bun run build`、`bun run build:check` 和 `go test ./...` 均不属于本轮默认验证。定向检查通过后停止；失败只修复并重跑相关范围。当前文档任务不需要新增测试基础设施、安装依赖或执行真实付费请求。

## 7. 每批进入下一步的条件

以下为质量门槛，不重新规定开发顺序；实施计划的每个批次按实际影响面引用对应行。

| 批次性质 | 合并前证据 | 不能以什么替代 |
| --- | --- | --- |
| 文档与设计映射 | VS-01–20 完整、源码路径/契约核对、分工与例外明确 | 代理声称“无遗漏” |
| 纯视觉与布局 | 受影响文件检查；双主题/窄屏/单实例/焦点/动效真实页面检查 | 静态稿截图、CSS 属性存在 |
| 表单/草稿/未决恢复 | 受控 API 的防重、存储失败、身份与迟到响应证据；报价/上传原契约回归 | 组件内锁或 happy path 一次成功 |
| 成果/列表/媒体 | 选择、有限发现、旧快照、真实媒体及续签、删除墓碑/回焦证据 | 总体测试数量、模拟封面播放 |
| 共享提取/视频作品复用 | 两个消费者契约回归、没有新增相互业务依赖；共享签名变化含图片工作室 | 只验证新页面能渲染 |
| 交付集成 | 每项需求对应实际证据、未测项与阻断项列明；必要类型/发布门槛已完成或明确未获执行 | 局部 Vitest 通过宣称可上线 |

每次验收记录包含：源版本/工作区范围、改动文件、实际命令与退出码、浏览器场景与证据、未执行项及原因、缺陷/剩余限制。没有执行的项目写“待实施验证”；未测试的浏览器不写“全部兼容”。真实生成、删除、上传到生产与生产验收不属于文档授权，需后续具体环境与操作范围。

未决记录新增版本后，还须审查回退兼容性：不能仅验证新代码读写成功，就声称退回不认识 pending 的旧前端安全。存在待核实记录时，应保留能展示并阻止等价重复提交的版本，或采用经验证的前向修复；发布/回退操作留待独立授权与正式 runbook。

## 8. 本轮文档交付状态

已核对 PRD 全部 20 项、UI 规范、测试配置/脚本、现有 Studio 与视频作品测试入口、运行时七语言和共享媒体签名实现。发现并纳入计划的缺口包括：未决恢复尚无跨刷新保障证据、健康播放器续签与失败重载需区分、后台错误不应隐藏旧列表、繁体语言不可遗漏、全量 typecheck/format 不能冒充定向检查。

本轮不声称上述业务场景已通过；未运行业务测试、全套测试、lint/typecheck/build、i18n 同步或生产操作。最终文档路径/引用/空白检查结果由开发文档总审记录，未来实施结果另追加实际证据。

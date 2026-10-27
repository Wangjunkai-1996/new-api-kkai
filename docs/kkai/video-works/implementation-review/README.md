# 视频作品实现与验证记录

2026-10-27。本轮已完成本地前后端代码、数据联调契约和针对性验证，尚未提交或部署。原设计基准保持不变。

## 实现结果

- `/usage-logs/task` 已使用“视频作品”专属视图。列表突出真实封面、模型、金额和计费入口；手机使用作品卡片与文字工具栏，支持深浅色、空态、失败与重试。真实应用继续使用原导航外壳。
- 服务端按视频 action 分类，执行模型/任务 ID、状态、时间、渠道过滤和分页；状态计数来自服务端同一权限范围。渠道选择器复用管理员现有搜索接口。
- 计费数据来自当前额度和冻结快照。未知金额显示 `—`，`completed` 显示“计费已处理”；未保存原始预扣/退款流水时明确说明。视频时长、分辨率和生成耗时分别取对应事实，不用计费倍率代替。
- Studio 任务按数字记录 ID、输出关联、所有者/租户/角色及资产状态投影真实海报和短签名。普通历史视频保留原通用制品流程；已知托管任务禁止回退到原始上游结果。下载沿用流式响应或对象存储 attachment 签名，不在浏览器缓冲整段视频。
- 生成或媒体处理中每 10 秒更新，仅剩待处理账务时每 15 秒更新；隐藏/离线暂停，授权失效停止，短签名最多一次自动续签。生成状态不显示没有来源保证的百分比。详情中的任务完成后移出当前标签，仍按同一授权范围和数字 ID 获取该任务。
- 七语言新文案已通过规定脚本写入并同步。没有新增依赖、数据库迁移或计费写操作。

交叉审查修复了临时不可用禁止重试、续签并发误判、已知托管媒体回退、媒体故障误标来源、负历史额度拖垮整页、任务完成导致弹层关闭，以及俄语工具栏裁切。

## 视觉与浏览器证据（F 层）

截图是实际 React 组件运行结果。API 被本地固定夹具拦截；示例封面来自设计 demo，播放与下载使用本地生成的 2 秒 MP4，均不会进入正式页面默认数据。此证据不代表真实 R2、生产渠道或完整应用外壳已经验收。

| 场景 | 实现截图 |
| --- | --- |
| 深色桌面，814px 主内容宽度 | [desktop-dark.png](desktop-dark.png) |
| 浅色桌面 | [desktop-light.png](desktop-light.png) |
| 375px 触屏手机 | [mobile.png](mobile.png) |
| 320px 窄屏 | [mobile-320.png](mobile-320.png) |
| 视频预览 / 扣费抽屉 | [preview.png](preview.png) / [billing.png](billing.png) |
| 手机预览 / 滚动到下载 / 扣费 | [mobile-preview.png](mobile-preview.png) / [mobile-preview-scroll.png](mobile-preview-scroll.png) / [mobile-billing.png](mobile-billing.png) |
| 首次空态 / 筛选无结果 | [empty.png](empty.png) / [filtered-empty.png](filtered-empty.png) |
| 生成失败 / 媒体重试 / 列表错误 | [failure.png](failure.png) / [media-retry.png](media-retry.png) / [list-error.png](list-error.png) |
| 俄语 / 法语手机 | [mobile-ru.png](mobile-ru.png) / [mobile-fr.png](mobile-fr.png) |

与定稿对照保留了 144×81 桌面封面、约 125px 行高、12px 圆角、局部深浅主题、900px 预览、245px 详情栏和 380px 扣费侧栏。账务缺失字段、真实播放器 controls、触达尺寸和应用外壳按开发契约处理差异。未执行像素差异阈值或全站壳层截图验收，不宣称像素完全一致。

本地 Chromium 验证通过：初始列表不请求逐行制品；原生视频播放/暂停/seek；关闭后卸载；预览与扣费互切后 Escape 返回封面焦点；实际附件下载字节与 MP4 夹具一致；服务端筛选/重置和本人作用域；媒体不可用及列表故障重试；1024/862/741/375/320px 无页面横向溢出；触屏操作至少 44px、ru/fr 工具位于卡片内；手机详情可滚动到下载。页面 JS 错误为 0。

浏览器执行命令（临时夹具，完成后退出开发服务器；非全站构建）：

```sh
# web/default/，仅加载视频页及其实际依赖
bunx --no-install rsbuild dev --config .video-works-browser.config.ts
# 项目根，Playwright 使用本机已安装 Chromium
node /tmp/video-works-browser/check.cjs
node /tmp/video-works-browser/focus.cjs
node /tmp/video-works-browser/mobile.cjs
```

测试页使用真实 Query/Theme/Direction/Tooltip/UsageLogsProvider 与路由容器；身份与 API 完全是本地夹具。临时浏览器入口/配置不作为产品代码保留。

## 自动验证命令与结果

所有命令退出 0；未执行仓库全套测试、全站 lint/typecheck/build。

Go（项目根；SQLite/HTTP 隔离夹具）：

```sh
go test ./controller ./service -run 'Test(VideoWorks|TaskVideoOutputs)' -count=1 -timeout=120s
go test ./controller ./service -run 'Test(TaskLogDTO|TaskLists|TaskBillingOther)' -count=1 -timeout=120s
go test ./controller -run 'Test(TaskArtifact|GetTaskArtifacts|DashboardTaskArtifacts|LegacyVideoArtifact|ProxyTaskMedia|WriteTaskMediaDataURL|ProjectedTaskArtifact)' -count=1 -timeout=120s
go test ./controller ./service -run 'Test(TaskArtifactDownloadPreservesRangeAndHead|TaskVideoOutputsDoesNotInventArchiveProgress)' -count=1 -timeout=120s
# 最后来源误标修复，仅重跑受影响范围
go test ./controller -run 'TestVideoWorks' -count=1 -timeout=120s
```

结果分别为 controller/service `0.115s/0.158s`、`0.082s/0.046s`、controller `0.155s`、`0.090s/0.057s`、controller `0.101s`。检查覆盖视频范围/聚合、历史额度语义、权限与所有者链、下载 Range/HEAD/206、托管结果防回退和局部媒体错误隔离；未使用生产数据。

前端（`web/default/`）：

```sh
bun run test src/features/usage-logs/components/__tests__/task-billing.test.tsx
bun run test src/features/usage-logs/video-works/video-work-ui.test.tsx
bun run test src/features/usage-logs/components/__tests__/task-artifacts-cell.test.tsx src/features/usage-logs/__tests__/mobile-layout.test.ts src/features/usage-logs/__tests__/task-details.test.ts src/features/usage-logs/__tests__/access.test.ts
bunx --no-install oxlint -c .oxlintrc.json src/features/usage-logs/components/task-billing.tsx src/features/usage-logs/lib/task-billing.ts src/features/usage-logs/components/log-cost-display.tsx src/features/usage-logs/components/dialogs/details-dialog.tsx src/features/usage-logs/components/__tests__/task-billing.test.tsx
node scripts/add-missing-keys.mjs
bun run i18n:sync
```

账务测试 12/12，UI 测试 6/6，旧共享组件/权限直接回归 11/11，以上 lint 通过。数据/媒体 41 条测试、精确 lint 与 scoped 类型配置见 [数据层检查](data-checks.md)；UI 组件 lint/类型配置见 [前端开发方案 §9](../frontend-implementation.md#9-本轮本地落地记录)。前端测试合计 70 条通过。翻译脚本完成后删除；变更源文件中的 365 个静态调用键在七语言逐项存在，43 个变更源文件版权头位置复核通过。

最终差异检查：`git diff --check` 通过。设计文件哈希继续按 `shasum -a 256 -c design/SHA256SUMS` 核对；截图位于本目录，不覆盖定稿。

## 未覆盖边界

- 未访问生产或其他真实后端环境；真实 R2/CORS、已有视频/海报、真实渠道及完整前后端连通仍需环境验收。本地 Go 合同检查和浏览器夹具不能替代它。
- PostgreSQL/MySQL 已审查方言及本地 JSON 列定义，但未启动真实服务做运行测试；本轮运行数据库夹具使用 SQLite。
- 未运行完整应用 TanStack 路由类型检查。组件/data 的 scoped TypeScript 已通过；完整注册依赖全站路由图，按成本规则没有扩大检查。浏览器实际运行是页面行为证据，不能替代全站静态类型验证。
- 未验证移动 Safari、200% 浏览器缩放、真实全屏媒体行为及完整应用侧栏展开/折叠组合；Chromium 手机尺寸/触屏模拟不代表真机验收。

这些是明确的环境或扩大验证边界，不应在发布前省略相关既有门禁。本任务没有进行提交、发布、迁移或配置开关操作。

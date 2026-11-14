# 多模态视频工作室：本地实现与验收记录

日期：2026-11-14。范围：本地工作树 `production/kkrich`，仅完成代码实现和定向验收；未提交、未构建生产镜像、未访问生产、未执行 schema apply、未调用真实付费供应商。

## 已落地

- 后端统一使用 `specification_version + reference_assets[{asset_id, role, position}]`，支持图片、视频、音频参考，按角色和位置校验组合、MIME、大小、尺寸、比例、时长、视频帧率和分类累计时长。
- 音频走独立 MP3/WAV 探测与 ready 生命周期，不生成视频海报；视频和音频分别限制总时长。旧 `reference_inputs`、旧标量引用字段和旧生成模式不会进入新提交。
- 视频任务保存能力版本与引用快照；样片、目录、草稿和复用只读取新结构。旧任务、作品、素材关联和账务生命周期保留。
- 新增只读幂等恢复查询，未知受理只返回 `pending`，不会自动 POST 或续 TTL；清理逻辑保护视频非终态、受理不明和缺失 task 的绑定。
- Seedance 特价插件将新引用集合投影为 `reference_images`、`reference_videos`、`reference_audios`，首尾帧投影为 `first_image`/`last_image`；未知 metadata、旧引用字段、重复/缺口 position 和冲突字段显式拒绝。外置 `seedance-special-adapter` 源码不在本仓库，未宣称真实上游贯通。
- Web 生成页使用单一创作表单和混合素材区，支持上传、检查、预览、互斥播放、排序、排除/恢复、替换、失败保留、报价阻断、提交恢复、任务/结果/近期作品和七语言文案。管理端同步维护新能力策略和有序样片引用。
- `video-media-metadata-v1` 采用独立 receipt 的可授权扩展方案，保持 canonical runtime 21 + RC49；CLI 只在显式 proof 和授权 apply 时执行。

## 发布阻断（已确认的本地事实）

当前应用的 PostgreSQL runtime contract 要求同时具备
`rc49-online-v1` 与 `video-media-metadata-v1` 两个扩展。应用 manual
deployment contract pin 为 `ec9a76512f84086e48936cdd10bf96705e33ccb0`，该
revision 的对象在已确认的本地 `../kkai-infra` 中可读。它的 manual deploy
contract 将 `required_expansions` 固定为只有 `rc49-online-v1`，schema
runtime 也没有 `video-media-metadata-v1` 的 proof 路径和操作。因此当前
应用改动不能通过该 controller 的普通 stage 镜像契约门禁；generic
preflight 不检查候选镜像契约，成功也不能解除这个阻断。本结论仅根据
本地源码和 pin，不声称 controller 已安装、生产已有该 receipt 或 schema
已完成。

解除条件是先安装并 pin 一个明确支持该扩展 digest、proof protocol 和
runtime contract 的 controller，再按扩展 runbook 获取只读 schema 证据。
不能从镜像源码推断 controller 已安装，也不能删除应用 contract 中的
扩展、直接在生产执行新 CLI，或把本地定向测试当作生产迁移回执。

## 定向验证

以下命令均在本机 Go 1.26.5 上用临时 modfile `/tmp/new-api-kkai-go-test.mod` 执行，未修改仓库 `go.mod`/`go.sum`。

```sh
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./plugins -run '^TestSoraSpecialSeedanceProjection$' -count=1 -timeout=120s
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./service -run 'TestVideoIdempotency|TestLookupVideoSubmission|TestCreateVideoAssetUpload|TestVideoMultimodal|TestValidateVideoModelSpec|TestUpdateVideoSample|TestDeleteVideoSample|TestCreateVideoSamplePersists|TestCreateVideoGeneration|TestUpdateVideoModelProfile' -count=1 -timeout=120s
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./controller -run 'TestVideoStudio(TokenErrors|MultimodalErrors)' -count=1 -timeout=120s
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./cmd/kkai-migrate ./pkg/kkaimigrate -count=1 -timeout=120s
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./service ./plugins ./cmd/kkai-migrate ./pkg/kkaimigrate -run 'Test(VideoIdempotency|LookupVideoSubmission|CleanupExpiredIdempotencyKeys|Sora|VideoMediaMetadata|ApplyVideoMedia|RC49)' -count=1 -timeout=120s
```

结果：以上定向 Go 测试通过；没有运行 `go test ./...`。

```sh
cd web/default && bunx tsc -b --pretty false --noEmit
cd web/default && bun run test src/features/video-studio/components/video-composer.test.tsx src/features/video-studio/video-submission-protocol.test.ts src/features/video-studio/hooks/use-video-submission.test.tsx src/stores/video-studio-draft-store.test.ts src/features/video-studio/components/video-reference-collection.test.tsx src/features/video-studio/video-admin-forms.test.ts src/features/video-studio/video-upload-protocol.test.ts --run
cd web/default && bun run test src/features/video-studio/video-create-page.test.tsx src/features/video-studio/components/video-composer.test.tsx src/features/video-studio/video-domain.test.ts src/features/video-studio/video-admin-forms.test.ts --run
cd web/default && bunx oxlint -c .oxlintrc.json src/features/video-studio src/stores/video-studio-draft-store.ts src/stores/video-studio-submission-store.ts
cd web/default && bun run i18n:check
git diff --check
```

结果：第一轮两组 Vitest 分别为 7 files/58 tests、4 files/55 tests 全部通过；视频工作室 scoped oxlint 通过；`git diff --check` 通过。没有运行全量前端 build/lint。

追加的最终前端定向命令：

```sh
cd web/default && bun run test src/features/video-studio/components/video-composer.test.tsx src/features/video-studio/video-domain.test.ts src/features/video-studio/video-submission-protocol.test.ts src/features/video-studio/hooks/use-video-submission.test.tsx src/stores/video-studio-draft-store.test.ts --run
cd web/default && bun run test src/features/video-studio/video-admin-forms.test.ts src/features/video-studio/components/video-reference-collection.test.tsx src/features/video-studio/components/video-sample-preview.test.ts --run
```

结果：两组分别为 5 files/57 tests、3 files/24 tests 全部通过（合计 8 files/81 tests）；TypeScript、视频工作室 scoped oxlint、i18n 7544 keys 和 `git diff --check` 均通过。没有运行全量前端 build/lint。

## 视觉与未验证边界

设计基准保存在 [video-studio-multimodal-web](../video-studio-multimodal-web/)：桌面深色、桌面浅色和移动深色图，以及完整 HTML/CSS/JS 原型。临时 DEV-only fixture 在 Chromium **1440×1000、390×844** 下检查了完整生成页：模型、三类参考素材、提示词、参数、报价/生成、当前作品、近期作品与样片均可见，无横向溢出，console error 为 0。前端执行者已确认移除预览 TSX、`main.tsx` 预览分支及临时 PNG，并停止 dev server。该 fixture 检查不能替代真实登录页面的完整端到端验收；本轮没有把空白/准备失败页当作完整页面通过证据。

仍需在有授权的测试环境补充：真实 MP3/WAV 和视频上传→探测→ready→签名播放、精确型号与外置 adapter 联调、真实报价/预扣/结算、完整页面双主题五宽度截图、真实生产 schema receipt、MySQL/PostgreSQL 有 DSN 的集成 fixture。上述缺口不应被本地 mock 或静态设计图替代。

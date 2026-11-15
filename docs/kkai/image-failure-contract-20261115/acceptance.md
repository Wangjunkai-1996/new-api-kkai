# 图片失败协议与诊断交付记录

状态：两端实现与独立代码审查完成；NewAPI 后端、前端及发布门禁完成，本次临时清理完成，受保护历史资产已记录保留处置；Sub2API 候选已构建并完成 10%/50% 观察，因 50% 达到 1800 秒仍无自然业务样本而按官方控制器回退，当前回到旧槽。记录开始：2026-11-15；更新：2026-11-16（Asia/Shanghai）。本次修复代码已完成审查和发布尝试，但未形成 Sub2API 生产上线结论。

## 文档链与范围

- 主方案 plan.md v1.1。
- 协议、计费/安全、SRE 专业审查及处置：review.md 与三个 review-*.md。
- NewAPI 后端开发：newapi-backend-development.md。
- 前端开发：frontend-development.md。
- Sub2API 开发：`/Users/tokk/Documents/Codex/2026-08-09/li/work/sub2api/docs/kkai/image-failure-development-20261115.md`。
- 串行发布：release-plan.md；实时准备证据由 release-preparation.md 记录。

没有数据库变更、历史任务重写、新账号/Key、额外分支/worktree或代码托管同步。事故原始完整响应未恢复，因此不能反推原始 JSON 必然符合新协议；当时 native/apikey 路由、HTTP502和账号耗尽已由日志证明。本次用同形协议fixture验证新行为。

## 已完成验证

从 `web/default/` 执行：

| 命令 | 结果 |
| --- | --- |
| `bun run test src/features/usage-logs/components/__tests__/image-upstream-error.test.tsx src/features/usage-logs/components/__tests__/reject-reason.test.tsx src/features/image-studio/components/image-creation-results.test.tsx src/features/image-studio/image-generation-error.test.ts` | 4文件、38测试通过，无skip。 |
| `bun x --no-install oxlint -c .oxlintrc.json src/features/usage-logs/types.ts src/features/usage-logs/components/dialogs/details-dialog.tsx src/features/usage-logs/components/__tests__/image-upstream-error.test.tsx` | 通过。 |
| `bun run typecheck` | 本机默认Bun1.3.13没有check子命令，工具入口失败；见下一行替代验证。 |
| `/Users/tokk/.bun/install/cache/@typescript/typescript-darwin-arm64@7.0.2@@@1/lib/tsc -b --pretty false --noEmit` | 通过，无诊断；使用已缓存精确版本，未改依赖。 |
| `bun run i18n:sync` | 通过；七种语言新键均补齐，zh-TW经脚本补翻译后同步。 |

3个TS/TSX文件用指定文件的oxfmt整理并逐字保留许可证头。原全目录format wrapper不支持文件子集，未用它冒充局部验证。新UI仅展示服务端脱敏admin_info并门控复制，旧日志缺字段兼容。

NewAPI后端定向验证已完成，精确命令、首轮单个fixture失败及纠正重跑、实际执行/skip见 newapi-backend-development.md 第6节；controller、legacy model、membership model通过，service各目标已通过，无目标skip。前后端独立代码审查均通过（code-review-backend.md、code-review-frontend.md）。无关全仓测试未执行，必要正式发布构建由已有完整上线授权覆盖。

Sub2API 命令在其 `backend/` 执行，使用开发文档记录的 Go1.27.3 和代理环境：

| 命令 | 最终结果及边界 |
| --- | --- |
| `go test -tags=unit ./internal/handler -count=1` | PASS，64.011s，最终 handler 源码未再改变。 |
| `go test -tags=unit ./internal/service -count=1` | FAIL，268.543s；唯一失败为未改动的 `TestInflightEstimate_AccountMappingNoDBAndBoundedMemory`，HeapAlloc 8483248 >= 8388608。图片、SSE、错误出口其余测试未报失败。 |
| `go test -tags=unit ./internal/service -run '^TestInflightEstimate_AccountMappingNoDBAndBoundedMemory$' -count=1` | PASS，0.178s；该测试/被测 billing 文件与修改前 HEAD 一致，测试经过 Anthropic/text 路径，不经过本次图片逻辑。未修改阈值，未证明全包瞬态失败原因。 |
| `go test -tags=unit ./internal/service -run 'TestForwardOpenAIImagesAPIKey_FailureContract\|TestOpenAIImagesProvider' -count=1` | PASS，0.117s；这是完整包之后唯一 UTF-8 门禁与 fixture 增量的最终验证，涵盖实际图片错误响应、非法 UTF-8、重复键、保留码、SSE 和脱敏。 |
| `git diff --check`、`git diff --cached --check` | PASS；精确提交可归档、工作树干净。 |

历史首轮完整 service unit 曾 PASS269.415s；无 tag 全包另曾出现未修改 allocation 用例失败，完整原输出被工具截断，隔离 PASS0.056s。历史/最终结果不得混写为最终 service 全包通过。完整日志保存在 Sub2API 被忽略的 `docs/kkai/image-failure-validation-20261116/`，未强行纳入代码提交。未执行 `go test ./...`、真实 MySQL/PostgreSQL 集成回归或登录/图片生成退款业务 E2E；没有数据库代码变更。

## 精确源码与发布记录

| 项目 | 本地发布源码 | 制品/记录 |
| --- | --- | --- |
| NewAPI 后端 | `f3334a7da0915dcd34e9979e7ccc1fc7ffc72bcf`，`production/kkrich` | `kkai-prod-20261115.1794758551-f3334a7da`，feature/external；完整运行时证据见 `acceptance-newapi-release.md`。 |
| NewAPI 前端 | 同一源码提交 | `kkai-frontend-20261115.1794759894-f3334a7da`，format2/API contract4；独立构建、独立激活。 |
| Sub2API image | `855bdb83bc422e4385938846b2b9a64864559a84`，`release/v0.2.21-kkai` | 已提交并通过 clean/commit-object/git-archive 门禁；生产结果待实际发布后填写。 |

后续仅文档提交不改变已部署二进制来源，不为文档重建上述不可变制品。全部构建位于 Mac；两端生产事务串行。原任务7734不改状态、不再扣款、不重放。

## Sub2API 发布结果

| 项目 | 结果 |
| --- | --- |
| 构建 | 唯一 Mac 构建成功，版本 `v0.2.21-kkai-image-errors-20261116`，镜像 `sha256:80d76634e087ffa7e9742bc1275dd2933b9c40a3bd90123687d75daad46962e1`，归档 SHA256 `aa52d379a256b469a0df386fbb0a281ccc2a93ea2493e2f38497b6043f80b6c6`。 |
| 计划/stage | `staged_0`；release `sub2api-v0.2.21-kkai-image-errors-20261116-855bdb83bc-text-green-image-green-20261115T164622Z`；plan SHA `12a78fee51432e48d7194e17277399fcf63154de5fe11329408f3c9da516817c`。 |
| 灰度 | 10% 正常通过：累计 412 秒，新旧槽各 1 个自然完成请求，HTTP 200 但业务终态为 unknown；没有失败重试、进程错误、尾部错误或 pending。50% 观察达到 1800 秒，新旧槽均无完成样本、无错误；按门禁不晋级 100%。 |
| 回退 | 官方 rollback 返回 `status=rolled_back`。立即和延迟快照确认 image/account-test 回到 blue、text 保持 green，旧 `apps-current`、router inode/hash、四槽健康、restart=0、OOM=false。 |
| 清理 | 官方 cleanup-plan 在回退后因 state 已移除且要求 `phase=complete` 被拒；未删除任何候选镜像、归档或历史证据，均保留并记录 deferred。禁止用 Docker prune/rmi 绕过控制器。 |

采集器 `passive-image-window.py` SHA256 为 `cbf29027278b578d8969ab0f825087cd173508114eb731dff3541864fe9519ff`，13 个离线 fixture 通过。它按应用完成时间、1 小时 carry-in、迟到日志、HTTP 200 内嵌失败、未知重试和 60 秒写入保护收集证据；业务成功固定为 0，HTTP 200 不被写成图片成功。10% 与 50% 的完整观察文件和回退证据保存在 ignored evidence 根目录。

Sub2API 本次未完成生产上线，不能声称该协议已通过真实图片 E2E 或已修复用户原始图片任务。用户接手登录、管理员日志交互、真实图片生成/编辑和退款业务验收，代理未验证；未创建账号/Key、未发送付费模型请求。

## 工具链

正式前端构建使用 `.local-releases/image-failure-contract-20261115/tools/bun-1.4.4/package/bin/bun`，输出版本1.4.4。制品来自npm `@oven/bun-darwin-aarch64@1.4.4`，验证registry声明的SHA512 integrity后解包；不替换全局Bun。后端使用既有Mac `kkai-mirror-builder`，读到linux/amd64支持及既定Docker代理。

## 业务验收交接

依用户要求，登录和真实账号业务 E2E 未由代理执行（用户接手业务测试，代理未验证）。包括管理员真实登录日志详情、真实图片生成/编辑及终态退款 E2E。测试fixture、健康探测和自然请求数量不能被写作该项通过。

版本、健康、schema、固定灰度计时、错误窗口审阅、回滚就绪与清理仍是必需门禁。最终生产坐标、制品、窗口及结果待实际执行后填写；此文档不预填成功结论。

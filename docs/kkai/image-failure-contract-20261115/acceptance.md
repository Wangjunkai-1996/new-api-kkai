# 图片失败协议与诊断交付记录

状态：NewAPI代码与定向验证完成，待正式构建上线；Sub2API仍在审查验证。尚未切流或宣称上线成功。记录开始日期：2026-11-15（Asia/Shanghai）。

## 文档链与范围

- 主方案 plan.md v1.0。
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

NewAPI后端定向验证已完成，精确命令、首轮单个fixture失败及纠正重跑、实际执行/skip见 newapi-backend-development.md 第6节；controller、legacy model、membership model通过，service各目标已通过，无目标skip。前后端独立代码审查均通过（code-review-backend.md、code-review-frontend.md）。Sub2/跨端生产者验证仍在进行。无关全仓测试未执行，必要正式发布构建由已有完整上线授权覆盖。

## 工具链

正式前端构建使用 `.local-releases/image-failure-contract-20261115/tools/bun-1.4.4/package/bin/bun`，输出版本1.4.4。制品来自npm `@oven/bun-darwin-aarch64@1.4.4`，验证registry声明的SHA512 integrity后解包；不替换全局Bun。后端使用既有Mac `kkai-mirror-builder`，读到linux/amd64支持及既定Docker代理。

## 业务验收交接

依用户要求，登录和真实账号业务 E2E 未由代理执行（用户接手业务测试，代理未验证）。包括管理员真实登录日志详情、真实图片生成/编辑及终态退款 E2E。测试fixture、健康探测和自然请求数量不能被写作该项通过。

版本、健康、schema、固定灰度计时、错误窗口审阅、回滚就绪与清理仍是必需门禁。最终生产坐标、制品、窗口及结果待实际执行后填写；此文档不预填成功结论。

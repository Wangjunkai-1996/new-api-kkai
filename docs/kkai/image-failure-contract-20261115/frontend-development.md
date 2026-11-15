# 管理员图片错误诊断前端开发文档

依据：plan.md v1.0、计费/安全审查 R2。范围为 web/default 日志详情；不改变 Image Studio 状态、请求、退款或重试流程。

## 接口与行为

扩展 LogOtherData.admin_info.image_upstream_error：message/type/code/status_code 均可选，truncated 为布尔值。后端已完成脱敏；前端不得从 public content、root_info 或任务错误拼接原始诊断。普通用户接口本就不返回 admin_info，UI 另使用现有 isAdmin 门禁。

在 DetailsDialog 内复用 DetailSection/DetailRow/Button 与 useCopyToClipboard，增加“Image upstream error details”区域。管理员/root 且字段存在时展示；旧日志没有字段时不显示占位或发额外请求。以 React 纯文本渲染有用原因和类型/错误码/HTTP状态，长文本换行，截断时提示。复制按钮复制本区域相同的已脱敏结构化字段，不包含整个日志或其他 metadata。正常 message 不再由前端截短。

不引入新依赖、独立状态、通用诊断框架或额外组件层。原图片工作室根据后端 failed 自然显示失败，unknown/confirming 保持现有行为。

## 文件

- src/features/usage-logs/types.ts：可选诊断结构。
- src/features/usage-logs/components/dialogs/details-dialog.tsx：管理员区域与复制行为。
- src/features/usage-logs/components/__tests__/image-upstream-error.test.tsx：权限、缺失兼容、结构化回退、截断提示和复制。
- src/i18n/locales/{en,zh,zh-TW,fr,ja,ru,vi}.json：新UI文案使用 scripts/add-missing-keys.mjs 写入并 i18n:sync；复用已有 Type/Error Code/HTTP Status 等实际存在键。

## 验证与验收

定向 vitest（预计1分钟内）：

```sh
bun run test src/features/usage-logs/components/__tests__/image-upstream-error.test.tsx src/features/usage-logs/components/__tests__/reject-reason.test.tsx src/features/image-studio/components/image-creation-results.test.tsx src/features/image-studio/image-generation-error.test.ts
```

断言：管理员可见诊断/复制一致；即使错误地提供 admin_info 普通用户也不可见；字段缺失兼容；仅结构化字段仍展示；truncated 有明确提示；Image Studio unknown 与 failed 原有契约保留。

对改动 TS/TSX 执行 oxlint 与保留许可证头的定向 oxfmt（原全仓 wrapper 不支持文件子集，故不使用）。正式发布门禁执行 bun run typecheck 与生产构建，预计分别1～3分钟和2～5分钟；源码型检查不替代构建。必要发布构建使用正式脚本，避免重复生产构建。无关全仓测试跳过。

依用户要求，登录和真实账号业务 E2E 未由代理执行（用户接手业务测试，代理未验证）。本地组件交互测试不声称等同生产登录验收。

## 实施验证记录

- 2026-11-15 定向 vitest 上述4个文件：38/38通过（包括新增6项权限/复制/缺失兼容断言），无skip。
- `bun x --no-install oxlint -c .oxlintrc.json src/features/usage-logs/types.ts src/features/usage-logs/components/dialogs/details-dialog.tsx src/features/usage-logs/components/__tests__/image-upstream-error.test.tsx`：通过。
- `bun run typecheck`：本机 Bun1.3.13 无 check 子命令，失败；随后使用本机缓存精确 TypeScript7.0.2 原生 checker：`/Users/tokk/.bun/install/cache/@typescript/typescript-darwin-arm64@7.0.2@@@1/lib/tsc -b --pretty false --noEmit`，通过，无诊断。未修改依赖/lock。
- 3个TS/TSX文件使用 oxfmt 指定路径格式化，前后逐字保留原许可证头；7语言新键经脚本写入并 `bun run i18n:sync`。
- 无关全仓测试未运行；生产构建等待最终提交及正式前端发布脚本。

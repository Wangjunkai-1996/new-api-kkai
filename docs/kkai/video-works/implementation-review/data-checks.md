# 视频作品：前端数据与媒体验证记录

验证日期：2026-10-27。以下记录对应本次前端数据层实现完成后的定向检查，不包含全仓测试、全量类型检查或生产构建；没有生产访问或部署。

## 实现范围

- 视频列表 API 与响应校验：视频筛选、状态计数、金额可用性、历史负数额度处理。
- 媒体投影：托管视频与通用制品的独立 URL 校验、数字记录 ID 校验、下载 URL。
- 查询身份与生命周期：用户、租户、会话与访问范围隔离，隐藏页面或离线时停止轮询并取消请求。
- 列表、详情和媒体更新：生成中与结算中轮询、错误退避、海报与播放地址续签、选中记录离开筛选结果后的定向刷新。

## 定向测试

工作目录：`web/default/`。

```sh
bun run test src/features/usage-logs/__tests__/artifacts.test.ts src/features/usage-logs/lib/__tests__/video-works-media.test.ts src/features/usage-logs/video-works/__tests__/use-video-works.test.tsx src/features/usage-logs/video-works/__tests__/video-works-api.test.ts
```

结果：**4 个测试文件，41 条测试全部通过**，约 1.22 秒。

覆盖重点：现有制品协议兼容性、托管与通用 URL 边界、数字任务身份、金额可用性和历史负额度、10/15 秒刷新、20/40/60 秒退避及 `Retry-After`、跨身份隔离、页面隐藏取消请求、身份加载失败、媒体懒加载、单次续签及同 URL 续期、计时器与播放错误同时触发的续签合并、续签失败、不可用与受限状态区分、海报续签，以及所选记录离开列表后的刷新与数字 ID 匹配。

## 定向 lint

工作目录：`web/default/`。

```sh
bunx --no-install oxlint -c .oxlintrc.json src/features/usage-logs/api.ts src/features/usage-logs/types.ts src/features/usage-logs/lib/task-artifacts.ts src/features/usage-logs/video-works/use-video-works-identity.ts src/features/usage-logs/video-works/use-video-works.ts src/features/usage-logs/video-works/use-video-work-artifacts.ts src/features/usage-logs/video-works/use-selected-video-work.ts src/features/usage-logs/lib/__tests__/video-works-media.test.ts src/features/usage-logs/video-works/__tests__/use-video-works.test.tsx src/features/usage-logs/video-works/__tests__/video-works-api.test.ts
```

结果：**10 个指定文件检查通过，退出码 0**。

## 定向 TypeScript 检查

工作目录：`web/default/`。临时配置位于忽略目录 `node_modules/.tmp/tsconfig-video-works-data.json`，未作为项目配置提交。

```sh
bunx --no-install tsgo --project node_modules/.tmp/tsconfig-video-works-data.json --noEmit
```

结果：**通过，退出码 0**。以下是执行时的完整配置；`files` 包含 11 个数据层源文件或测试文件，以及 3 个项目类型/测试环境文件：

```json
{
  "extends": "../../tsconfig.app.json",
  "include": [],
  "files": [
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/usage-logs/api.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/usage-logs/types.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/usage-logs/lib/task-artifacts.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/usage-logs/__tests__/artifacts.test.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/usage-logs/lib/__tests__/video-works-media.test.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/usage-logs/video-works/use-video-works-identity.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/usage-logs/video-works/use-video-works.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/usage-logs/video-works/use-video-work-artifacts.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/usage-logs/video-works/__tests__/use-video-works.test.tsx",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/usage-logs/video-works/__tests__/video-works-api.test.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/features/usage-logs/video-works/use-selected-video-work.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/env.d.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/tanstack-table.d.ts",
    "/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/web/default/src/test-setup.ts"
  ],
  "compilerOptions": {
    "incremental": false
  }
}
```

在其他检出路径复现时，仅需将 `files` 中的仓库绝对路径前缀替换为当前检出路径，保持配置文件位置及 `extends` 不变。

## 验证边界

- 已完成本范围代码的定向 diff 空白检查。
- **全量测试、全量 lint、全量类型检查及生产构建均跳过**；本次范围采用直接相关检查。
- 浏览器中的视觉还原、真实组件交互及触屏表现由集成验收记录覆盖；上述单元测试不等同于浏览器验收。
- 未验证生产环境中的实际媒体服务或线上权限配置；本次没有生产操作。
- 本次仅归档已经完成的验证，没有重复执行通过的测试或扩大检查范围。

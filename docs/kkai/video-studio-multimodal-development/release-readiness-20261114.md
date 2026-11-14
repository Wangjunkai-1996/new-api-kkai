# 多模态视频工作室：发布前审查与阻断

日期：2026-11-14。用户已授权应用提交、推送和蓝绿上线。本记录区分应用代码交付与生产发布条件；初次记录时没有构建、stage、promote 或执行生产数据库变更。

## 后续授权与审查进展

用户随后授权完成必要的配套发布操作，约束为不影响线上用户；并明确接手上线后的真实上传、生成和结算业务测试。代理不将这些业务 E2E 记为通过。下面原始阻断记录保留审查历史，最新实现以本节及 `api3-rollout-compatibility.md` 为准。

- 新创作页面仍只有一套流程；发布过渡后端已保留相邻 API3 的实际行为，同时提供 API4。持久开关默认关闭，激活新前端前须使 active、writer 和 rollback 全部支持新协议。
- API4 不得原地转换旧样片，返回 `409 video_sample_requires_copy`；API3 样片写入保留冻结能力版本。定向回归验证旧记录、引用和 outbox 不变，新格式可另建。
- Linux 真实迁移二进制隔离演练已验证过期 proof、可写 proof mount 拒绝、只读 proof 成功、扩展回执和物理列一致、canonical ledger 不变、迁移角色关闭。证据：`.local-releases/video-media-20261114/cli-proof-result.json`。这是本地证据，不是生产迁移完成。
- 外置适配器源码已定位到 `/Users/tokk/Documents/Codex/2026-07-22/ni-ka/seedance-special-adapter`。实际生产旧二进制与本地新二进制的 11 个既有型号均通过新任务 ID 查询及下载回退验证；旧标量请求的上游路径和 JSON 全等，显式 `generate_audio:false` 保留。未发布型号的创建请求拒绝，未调用付费供应商。
- 配套 infra 新增受控扩展和独立适配器切换入口。上线前仍须完成 mandatory policy、精确控制器安装、备份与扩展、正式产物验证、完整灰度、前端激活及发布后观察。

本轮追加验证：

```sh
bash scripts/kkai/build-manual-release_test.sh
bash scripts/kkai/deploy-manual-release_test.sh
PYTHONDONTWRITEBYTECODE=1 python3 scripts/kkai/prepare-maintenance-release_test.py
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./service ./controller -run '^(TestVideoStudioAPI3SamplesRetainSnapshotVersionAndRequireAPI4Copy|TestVideoStudioMultimodalErrorsHaveStableHTTPContract|Test.*VideoSample)' -count=1 -timeout=120s
git diff --check
```

以上通过；准备脚本 13 项通过。前端视频与契约范围 30 文件/293 项通过，旧样片错误提示补充的 9 项及 TypeScript 检查通过。应用全仓测试未运行。

## 本轮审查修复

- 超量引用在逐个查库前拒绝，避免请求放大数据库查询。
- Studio 入站使用严格 JSON 解码，拒绝未知/旧字段、重复键及非 JSON 表单；保留请求体重读能力。
- 实际 Submit 路径补传能力版本，防止所有有效请求被错误判为缺少报价。经过 Prepare → Distribute → Submit → 测试适配器的拒绝/退款回归已通过。
- 模式或型号变化涉及参数调整时展示差异并确认，避免隐藏参数阻断报价或静默修改输出。
- 新协议声明控制台 API 4，不将已移除的 API 3 创作流程标记为兼容。

## 本次只读生产证据

从已确认的同级 `../kkai-infra` 执行 `make newapi-status`：HEALTHY；当前版本 `kkai-prod-20261110.1794299358-8a796af03`；active slot 为 blue；rollback ready。

通过 `sys1` 读取：candidate-status 为 none；安装配置的 controller SHA 为 `ec9a76512f84086e48936cdd10bf96705e33ccb0`，与应用 pin 一致；前端为 format 2、API 3，release ID 为 `kkai-frontend-20261111.1794399259-b17930150`。

这些只是本次观测，不能作为后续发布的实时状态。针对保留 RC49 artifact receipt 的只读 status 返回失败，未取得新鲜的完整 schema/digest/扩展观察证据，不能据此声称新扩展已经存在或不存在。没有重写旧 receipt。

## 停止普通发布的原因

1. 应用的两个构建 profile 都要求 canonical 21、`rc49-online-v1` 和新增 `video-media-metadata-v1`。安装版本的控制器只接受 RC49 单扩展，不支持新扩展 proof 和受控 apply。Generic preflight 成功不能代替候选镜像 contract 或数据库证据。
2. 新创作 API 不兼容旧流程。新后端只声明 API 4；线上前端要求 API 3。runbook 21 的普通串行蓝绿路径无法安全混用它们，必须有单独审查的联合切换和回滚方案，不能篡改兼容声明绕过检查。
3. 外置 `seedance-special-adapter` 源码不在本仓库；真实素材审核/上传、供应商调用及结算尚无端到端验收。现有本地测试适配器不代表真实供应商链路已通过。

## 需要另行授权的生产变更范围

具体 schema 方案已实现并可审查：为 `kkai_video_assets` 添加可空 `media_metadata TEXT`，创建 `kkai_video_media_metadata_steps` 扩展回执表，保持 canonical ledger 版本 21。通过受控 controller proof 执行，不运行启动时自动迁移、不改写旧 ledger、不删除业务数据。DDL 需要短暂表锁，遇到冲突应失败退出；先验证当前与回滚二进制兼容和恢复证据。

继续生产发布需要先补齐并审查配套控制器，完成必需验证后安装精确版本、同步应用 pin，再取新鲜只读计划。数据库 apply 必须获得单独授权；该授权不是再次申请已经授予的应用 stage/promote 权限。API 4 的联合切换方案必须明确控制台不可用范围、在途任务处理、真实业务验收和前后端一起回退的路径。

在以上条件完成前，不构建普通发布镜像、不 stage，不以 `bridge` 名称推断兼容性，不直接调用未获授权的新迁移 CLI。

## 定向验证

本轮视频 controller/service 与两个 profile 的发布契约定向检查均通过：

```sh
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./controller -run 'Test.*Video' -count=1 -timeout=120s
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./service -run 'Test.*Video' -count=1 -timeout=120s
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./common -run '^TestConsoleContract$' -count=1 -timeout=120s
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod -tags kkai_bridge ./common -run '^TestConsoleContract$' -count=1 -timeout=120s
git diff --check
```

前端在 `web/default` 执行：

```sh
bun run test src/features/video-studio/components/video-composer.test.tsx
bun run test src/features/video-studio src/stores/video-studio-draft-store.test.ts src/lib/console-contract.test.ts src/hooks/use-console-contract.test.tsx --run
bun run test src/features/video-studio/video-sample-category.test.ts
bunx tsc -b --pretty false --noEmit
bunx oxlint -c .oxlintrc.json src/features/video-studio src/stores/video-studio-draft-store.ts src/stores/video-studio-draft-store.test.ts src/lib/console-contract.test.ts src/hooks/use-console-contract.test.tsx
bun run i18n:sync
```

参数交互专项 18/18 通过。视频与契约范围共 29 文件、253 项，首轮 252 通过；唯一失败是旧样片分类 fixture 仍使用旧引用字段。将 fixture 更新为新协议后，只重跑该文件，2/2 通过。最终 TypeScript、scoped oxlint、七语言同步和 diff 检查通过；没有为追求一次全绿而重跑已通过用例。

此前前端 81 项及后端定向验证见 [实现与验收记录](implementation-qa.md)。全仓测试和生产构建均未运行；真实供应商/媒体链路、MySQL/PostgreSQL 集成和生产 schema 仍不是已验证结果。

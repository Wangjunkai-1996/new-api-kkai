# 图片大请求 524 优化：实施与验证记录

本次实现面向所有已有 Image Studio 使用权限的用户，没有用户 ID 白名单。新路径是私有对象存储分片直传、小 JSON 原子受理、持久后台生成及轮询。模型还需支持完整计价快照；不支持的模型保留原入口。第三方客户端直接调用同步图片 API 的协议没有在本次改造中变为异步。

本文记录本地实施与验收，**不是生产上线证明**。尚未提交应用或基础设施代码、安装控制器、构建生产镜像、迁移线上 schema 或打开全局开关。

## 已落实的行为

- 参考图按 5 MiB 分片上传，签名绑定精确长度，服务端校验实际字节、SHA-256、格式与像素上限。只上传缺片；引用中的原图受到清理保护。
- 小 JSON 提交将任务、幂等标识、参考图绑定、预扣和 outbox 写入同一事务，成功返回 202 与 Location。旧同步和新异步路径共享数据库并发预算。
- 所有已受理任务可按原幂等标识只读找回，不要求报价仍有效或浏览器仍持有文件。查无记录只表示尚未确认，不自动重发。
- 发送意图先持久化。结果不明、进程重启或失去执行权时，不重复发送模型 POST；保留预扣到核对截止，无法交付时只退款一次。
- 可恢复结果与账务意图原子持久化；结算、任务终态和引用释放同事务完成。执行者在每次业务写入中校验 leader 与 outbox 领取资格，失效执行者不能发布结果或改账。
- 新版 writer 支持停止领取并等待现有任务完成，发布和回退使用精确容器、进程启动身份的排空回执。
- 上传删除后在签名与存储请求安全截止再做一次尾扫，成功后退出 GC 队列，避免历史墓碑永久占用清理批次。

默认期限：排队 10 分钟；输入准备与上游执行共 20 分钟；归档 2 分钟；核对余量 2 分钟。上传校验读取有 2 分钟期限；本地图片头解析使用有界字节和 DecodeConfig，没有宣称能硬中断 CPU 解码。

## 验证命令与结果

应用根目录：

```sh
go test ./model ./service ./controller ./relay/channel ./router . \
  -run 'Image|Idempotency|ApplicationBackgroundJobs' -count=1
```

六个包均通过。覆盖图片链路与计费、异步恢复、上传、租约、幂等、路由鉴权和后台任务边界。准备第一阶段默认契约 `(9,10,9)` 后，又用 `go test -tags kkai_bridge ./model ./service ./controller ./relay/channel ./router . -run 'Image|Idempotency|ApplicationBackgroundJobs' -count=1` 验证实际 bridge 构建条件，六包均通过；`go test ./pkg/kkaimigrate -count=1` 也通过。

最终集成验收发现并修复了内部绝对 URL 被适配器重复拼接的问题，后台请求改用纯路径。修复后执行：

```sh
go test ./controller -run '^TestImageAsyncWorkerEditExecutesAndArchivesExactlyOnce$' -count=1
go test ./controller ./relay/channel/openai \
  -run '^TestImageAsync|^TestOpenAIAdaptorPlaygroundRequestURL$' -count=1
go test ./service -run '^TestImageUpload' -count=1
```

均通过。编辑任务真实经过本地 HTTP 上游与 S3 测试服务，验证两张参考图顺序、接受后调价仍按冻结价格结算、归档成功、重复事件不再次 POST/写对象/扣款。上传回归覆盖尾扫清理迟到对象及历史墓碑不挤占后续批次。

```sh
go test ./pkg/kkaimigrate -count=1
go test -tags kkai_bridge ./pkg/kkaimigrate \
  -run 'TestContractForDialectUsesBridgeRuntime|TestImageAsync|TestMaintenance' -count=1
go test ./service -run '^TestImageSubmissionLookupIsReadOnlyAndScopedToOwner$' -count=1
bash scripts/kkai/build-manual-release_test.sh
```

均通过。只读找回测试在数据库禁止写入时执行，验证过期绑定仍可查询、跨用户不可见、查询不创建或删除记录。

真实 MySQL 5.7、PostgreSQL 18.4 使用隔离本地容器与临时数据库，注入测试专用 DSN 后执行：

```sh
go test ./pkg/kkaimigrate -run '^TestImageAsyncRealDatabaseContract$' -count=1 -v
```

两种数据库均通过（合计 6.377 秒），覆盖 v9→v10 干运行、重复迁移、历史账本不变、预扣及 outbox 外层回滚、失效领取资格。竞争事务分别得到 MySQL 1205 与 PostgreSQL 55P03，持锁事务提交后更新可完成。临时容器与网络已删除。

`web/default`：

```sh
bun run test src/features/image-studio/components/image-composer.test.tsx
bun run test src/features/image-studio/components/image-submission-recovery.test.tsx
./node_modules/.bin/tsgo --project .image-studio-final-typecheck.json --noEmit
```

前两项分别 9 项、4 项通过；局部类型检查通过，临时配置已删除。上传协议、API 与草稿恢复的定向测试也已通过。仅查询的恢复组件不会在 pending、not_found、网络错误或 unknown 时清除回执或重新 POST；异步报价失败不会改走旧的大 multipart。

基础设施目录：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -q \
  scripts.tests.test_newapi_schema_v10 \
  scripts.tests.test_newapi_schema_v8 \
  scripts.tests.test_newapi_schema_migration \
  scripts.tests.test_newapi_schema_snapshot_state \
  scripts.tests.test_newapi_schema_snapshot_controller \
  scripts.tests.test_newapi_schema_snapshot_preflight \
  scripts.tests.test_newapi_schema_snapshot_verifier \
  scripts.tests.test_newapi_schema_snapshot_apply
```

43 项通过。writer 排空 13 项测试及受影响的部署、回退、合同测试通过。

```sh
make policy
```

完整基础设施策略检查最终通过，包含本地容器蓝绿及旧拓扑演练、故障回退与 shellcheck。中途发现的旧恢复合同/测试与新排空要求不一致已修复。因为修改了 Ansible 回退行为，已获授权并执行 `make check`：语法与策略阶段通过，主机检查止于既有 storage reserve 断言（要求 >98 GiB，实际未分配 67.31 GiB）。当前 VG 475.31 GiB、没有快照；已批准的存储布局在基础设施 runbook 23 中明确预期余量 67.31 GiB。迁移快照要求至少 32 GiB，现有容量满足；没有修改存储断言、卷布局或执行 storage apply。发布相关的定向主机检查继续单独记录。应用整仓全套没有运行。

容量与检查范围补充：只读 `vgs` / `lvs` / `findmnt` 显示 7 个普通 LV，postgres 176 GiB、appdata 16 GiB、newapi_backup 64 GiB，VG 精确空闲 72,272,052,224 字节，没有 origin 或 snapshot LV。基础设施 inventory 仍保留 postgres 160 GiB、appdata 64 GiB 的早期计划，且未列入维护窗口创建的 backup LV；因此不能对这次失败运行 storage apply。runbook 23 的既有维护结果与实际容量一致，runbook 21 也保留了历史完整 `make check` 的 reserve 失败记录。

快照实现创建 rollback 16 GiB 与 verify 8 GiB，合计 24 GiB；按当前容量创建后仍余约 43.31 GiB。rollback 以 4 GiB 增量扩至最多 27 GiB，即使 verify 尚未删除，仍余约 32.31 GiB，高于实现保留的 8 GiB。上述计算只说明容量前提满足，不能替代正式快照的运行时预检、身份绑定和还原验证。本次唯一 Ansible 业务变更位于回退 writer 恢复命令，已有 `when: not ansible_check_mode` 保护，执行语义由 policy 合同测试及后续真实发布验收覆盖。未运行无关 frontend host 检查，也未把 gate 关闭后全跳过的 bootstrap dry-run 算作通过；`newapi-deploy-simple.yml --check --diff` 需等精确候选 metadata 就绪后再判断适用性。完整 `make check` 结果仍为失败（退出 2），不是通过。

真实 R2 验收还覆盖了 16,780,612 字节合法 PNG 的四分片上传：第一片完成后通过 ListParts 恢复，只补传缺片，合并后全量 SHA-256 一致；对象和残留会话均清理完成。真实 R2 与 Chrome 的协议、长度限制、跨域和清理证据见 [R2 验收记录](image-studio-r2-acceptance-20261018.md)。它不等同于尚未发布的新网页全链路、弱网性能对照或真实模型出图验收。

## 生产交付仍需完成

1. 完成基础设施强制检查，保留无关工作，提交本次精确源版本，安装并自检精确控制器，同步应用 pin。
2. 在现有规范 v9 上完成兼容版本的两次正常发布，形成均能处理 v9/v10 的实际活动、回退和 writer 组合。全局新受理开关保持关闭。
3. 已获 v9→v10 迁移及全用户上线授权，按 schema runbook 创建并验证快照，执行仅增加三张附属表的 v10 迁移，核对精确版本与摘要。
4. 候选业务验收、记录快照功能验收、10%/50%/100% 各至少 120 秒 canary、正式晋级、延迟状态与错误窗口、回退验证。
5. 发布前端并验证真实上传→202→后台出图→资产及计费一致，再对所有已有 Image Studio 权限的用户打开 `image_studio.async_submission_enabled`。不添加 4249 白名单。

完整设计、协议限制和验收矩阵见 [实施方案](image-studio-524-optimization-plan.md)。线上 schema 迁移、全局启用和真实模型成功率仍是未完成项，不能用本地通过代替。

## 分阶段不可变版本

首次迁移前必须完成两次真实 bridge 晋级，才能让两个槽位和 writer 都具备 v9/v10 兼容能力。A 提交包含后端及恢复、排空、计费的完整兼容实现，默认运行契约暂保持 `(9,10,9)`；B 提交接入新网页，后端仍构建 bridge；独立观察到 v10 后，C 提交才把默认 feature 契约收紧至 `(10,10,10)`。每个精确源码提交只构建一个不可变后端版本，未迁移前保持全局异步受理关闭。

A 兼容性复核同时移除了历史 v5 bridge 的视频分类降级分支：新的 bridge 下限是 v9，必须保留已上线分类功能。`go test ./common ./service -run '^TestConsoleContract$|^TestVideoSample' -count=1`、`go test ./cmd/kkai-migrate -count=1` 及各自带 `-tags kkai_bridge` 的版本均通过。

快照验收门禁已补齐 v10 staged candidate 专用路径，绑定候选版本、镜像和容器及原 writer 身份，不重复获取发布锁；apply 仍禁止候选存在。最终 `make policy` 全部通过（日志 `/tmp/kkai-image524-policy-release.log`），包括新候选绑定测试、只读检查、source-size、蓝绿/回退本地演练及 shellcheck。基础设施精确提交为 `c9f65acad5864276a4c4297e90e16233d4d34660`。

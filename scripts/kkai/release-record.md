# 发布计时与安全恢复

构建和 stage 入口现在自动在产物目录生成独立的私有 JSON 记录，通过 stderr 输出
`RELEASE_RECORD=/absolute/path/...json`。不改变 artifact、metadata 或控制器协议。

记录包括精确来源、产物摘要/字节数、实际资源参数和各阶段 UTC/耗时/退出码。
耗时使用单调时钟；BuildKit 的编译、缓存及镜像导出暂作为一个整体阶段，不猜测分项耗时或缓存命中率。
每次调用使用单独记录，原子更新、权限 0600，不存储命令参数、正文、凭据或原始输出。
保留小型记录即可；禁止把 sidecar 打进部署产物。

记录初始化失败会在构建/上传前停止；初始化后的计时写入故障只告警，不改写真实命令结果。
`running`、未结束阶段或缺少记录都表示证据不完整，不代表远端操作未发生。
记录的 `passed` 只表示被调用命令退出 0，不等于业务验收或整次上线完成。

## 构建资源

默认 `--resource-profile conservative` 保持现有 2 CPU / 3g。
下一次已授权构建可显式尝试 `--resource-profile balanced`，使用 4 CPU / 4g：

```bash
scripts/kkai/build-manual-release.sh \
  --schema-contract "${verified_schema_profile}" \
  --frontend-mode external --resource-profile balanced
```

这不是当前生产 schema 的选择示例；`verified_schema_profile` 必须先按主手册取得证据。
不为比较档位重复构建同一不可变 release。先记录相近正常发布的缓存冷热、耗时、OOM/交换和桌面响应。
Go 并行度 4、媒体并行度 2 和重型步骤串行保持不变。
资源优先级为 profile 默认值 < `KKAI_BUILDX_CPU_QUOTA`/`KKAI_BUILDX_MEMORY` < 显式参数，
profile 和覆盖参数的顺序不影响结果；`--no-resource-limits` 保留原有命令行顺序语义。
metadata 不记录这些新字段，sidecar 记录最终实际参数。

## 上传实验

以下两个选项默认均为 0，只在已授权 stage 中显式启用并对比实际耗时：

```bash
KKAI_RELEASE_SSH_MULTIPLEX=1 \
  scripts/kkai/deploy-manual-release.sh --stage "${verified_metadata}"
```

连接复用使用每次调用独立的短路径私有 socket，结束后只关闭该连接并清理该目录；
不复用候选 tunnel。SSH/SCP 同用这些选项，不修改 SSH alias 或全局配置。
压缩仅发生在 SSH 传输层，远端仍收到同一个原始 `.tar`，执行原有 SHA 和契约校验。
需要单独试压缩时，改用 `KKAI_RELEASE_SSH_COMPRESSION=1`，不同时设置复用开关，
避免同时改两个变量后无法归因。
结果不明时仍只查询一次 candidate-status，不自动重传或重放 stage。

## 人工阶段计时

用薄包装器计时现有命令，命令原输出继续显示，但不写入 sidecar：

```bash
python3 scripts/kkai/release-record.py run \
  --metadata "${verified_metadata}" --phase status-immediate \
  -- make -C "${infra_root}" newapi-status
```

可选 phase 包括 `candidate-acceptance`、`canary-10`、`canary-50`、`canary-100`、
`handoff-ready`、`promote`、`status-immediate`、`status-delayed`、`error-window`、
`frontend-prepare`、`frontend-activate`、`public-acceptance`。
对于正式灰度及 promote，补上 `--slot "${target_slot}" --rollout-id "${rollout_id}"`，
二者必须一起提供。由操作者核对这些坐标，包装器不替代控制器身份检查，也不提供发布授权。

不通过脚本自动跳过观察或批准业务结果；检查和证据整理可放在既有等待窗口内。
TERM/INT/HUP 会转发给直接子命令并等待退出；远端事务可能仍已完成，必须先查询官方状态。

## 中断后只读核对

```bash
scripts/kkai/release-resume-status.sh --metadata "${verified_metadata}" \
  --slot "${recorded_target_slot}" --rollout-id "${recorded_rollout_id}"
```

该入口只调用官方 candidate-status 和 rollout status；拒绝不匹配的版本/slot/rollout ID、
路由漂移及异常状态，不上传、不推进、不回滚、不清理。
仅在远端没有 rollout state 时可只提供 metadata 并返回成功；存在当前或历史 state 时，
必须给出本次精确 slot/rollout ID。旧发布的 finalized/rolled_back state 不能冒充本次恢复证据。
没有 candidate 或本地记录成功
都不能推断已经 promote；仍按主手册核对精确产物、当前安装契约、即时/延迟 status、
错误窗口和 rollback ready。完成检查后再按原手册恢复，不盲目重放写操作。

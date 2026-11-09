# 生产交付证据（2026-11-09）

结果与限制见[生产记录](../../membership-reservation-production-20261109.md)。本目录保存 A/B 灰度、业务与错误窗口、备份、v21 首次失败和已完成只读重放、控制模式、账务快照及清理回执。

`manifest.json` 登记每个证据文件的 SHA256；大日志以确定性 gzip 保留。仅将已完成灰度交接 token 替换为 `[REDACTED]`，登记文件标注该变换。命令参数与返回码在 `*.result.json`，没有密码或用户级凭据。

关键证据：`final-status.log`、`final-release-snapshot.log`、`recovery-apply.log`、`recovery-status-current.log`、`billing-apply-repeat-check.log`、`recovery-final-rounds.log`、`final-acceptance.json`。首次迁移失败不能删除：`v21-apply.log`、`v21-failed-audit.log`、`v21-after-failure-status.log`、`v21-completed-replay.log`、`migration-disposition.json` 描述其真实恢复过程。

自然恢复终态样本为0，固定空指纹不是生产 exactly-once 证明；账务快照和 worker 汇总仅证明本次观察结果。原始本地详细证据仍保留在任务临时目录。

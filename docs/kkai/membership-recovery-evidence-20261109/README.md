# 会员暂占恢复本地证据

这些记录来自本机隔离数据库及合成请求，不是生产发布回执。

- `linux-root-proof.json`：Linux ARM64 CLI / PG18，9 项真实凭证、DDL 回滚和重放验收；运行进程身份为合成数据。
- `measurements.json`：HTTP 每格汇总，包含修复前失败、修复后及预算调整后的样本，未用这些数据宣称性能全面通过。
- `*.jsonl.gz`：对应逐请求原始样本；解压后 SHA256 在汇总中。每项报告的 `scope`、worker 实际时长、样本量及错误字段界定适用范围。

`http-mysql57-matrix` 因测试日志列类型、既有多用户预占死锁和180秒整体上限而无效；不得用其成功子集作性能结论。

`http-mysql57-fixed-functional` 的七格共 280 次请求通过，最后一格预热因隔离容器 OOM 中断。`http-mysql57-final-cell-recheck` 在恢复容器后单独通过该格 40 次请求。八种功能场景都已覆盖，但不是一次完整命令通过，也不构成性能验收。OOM 状态、恢复结果与真实 MySQL 4 项预占回归的最终日志一并保留。

详见 [实施与验证记录](../membership-reservation-implementation-20261109.md)。

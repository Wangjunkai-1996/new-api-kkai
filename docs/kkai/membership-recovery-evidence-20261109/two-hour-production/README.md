# 2 小时规则生产证据

本目录证据属于 `kkai-prod-20261109.1794239898-a552996db`，不是之前72小时版本的验证结果。仅包含发布回执、固定只读聚合、精确构建信息和操作者归因；不包含凭据、原始业务日志或单个客户账单标识。

`*.result.json` 记录实际命令、开始/结束和退出码；同名日志是该操作输出。`B-canary-*-status.json` 和 `*-http.log` 记录10/50/100%精确版本、槽位、观察时间与HTTP聚合。`canary-*-errors.log` 是各档前60秒与发布前等长60秒的官方模型事件聚合，并已等待日志60秒写入保护。HTTP聚合覆盖完整120秒，两个口径不可当同一个样本集。

`canary-*-review.json` 是主执行者复核决定，错误来源无法完全确诊的限制保留。`release-review-notes.md` 记录前台路径、性能、测试失败/修正和资金口径。

`billing-*.log` 为只读RR账务快照：所有scope账户/账本对账，终态金额公式、归属、非负和Token退款检查。自动返还按 `natural_recovery_terminals.refund_quota` 计算，不采用worker的adjusted_quota；固定终态指纹只提供跨轮次观察证据。

`production-cleanup-inventory.json` 和 `local-cleanup-receipt.json` 记录本次盘点及实际清理。schema snapshot冻结保护的后端和30天保留期内前端均不是垃圾，不手工删除。

归档日志去除行尾空白，失败测试日志另将tab展开为空格；变动文件原始SHA在 `archive-format.json`，运行目录保留原件。runtime scope以JSON转义保存精确diff，便于通过仓库空白检查。

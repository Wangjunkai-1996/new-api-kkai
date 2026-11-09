# 租约保护最小修复验证

对应提交：`c6fc071f87b81c886935cf9e3ac806aca645cfe0`。验证时基于 `cc99ff13494538bacfad06d215415d761013988d` 加入这份单文件修复；源文件 SHA256 与 c6fc 提交逐字匹配。验证结果由 SRE 子任务产生，本次只复制、复核原日志，不重新运行。

`RequireMembershipRecoveryLeaderTx` 仅在 SQLite 保留 `updated_at = updated_at` 来取得写锁；PostgreSQL/MySQL 继续使用原 `FOR UPDATE`。holder、fence、数据库时钟与事务起始/提交双重校验保持原样。

三次命令均退出 0：

```sh
go test -p 1 ./model ./service -run 'TestMembershipRecovery(ScanCannotWriteWithLostLeaderFence|FenceLossRollsBackEvidenceAndFunds|WorkerModesKeepEarlyEvidenceAndAgedFallbackSeparate)|TestLeaderLease(AllowsSingleHolderAndCleanTakeover|ConcurrentAcquisitionHasOneWinner|UsesDatabaseClock)' -count=1 -timeout=120s -v

# 真实 PostgreSQL18：运行器启用会员并从0600文件注入独立本机测试DSN，凭据不归档。
go test -p 1 ./service -run '^TestMembershipRecoveryRealDatabaseAcceptance$/^postgres$' -count=1 -timeout=90s -v

KKAI_MEMBERSHIP_ENABLED=true GOMAXPROCS=2 go test -p 1 ./service -run '^TestMembershipRecoveryWorkerModesKeepEarlyEvidenceAndAgedFallbackSeparate$' -count=1 -timeout=60s -v
```

第一次定向运行整体通过，但未启用会员环境变量，四个 mode 子项实际 **SKIP**；不能将其计为模式覆盖。第三条命令明确启用会员后，off/observe/apply/paused 四项全部通过。无验证失败记录，三份原日志均保留。

真实 PostgreSQL 四项通过：前台与后台只结算一次、真实行锁取消、延期不锁用户、过期 leader 禁止结算。使用独立 `kkai_test_membership_leader_guard` 数据库和隔离 schema，SRE 完成后删除该独立数据库。未触碰生产，未为这次微小收窄重跑 MySQL；全仓测试跳过。性能结果另见父目录，功能测试不替代性能结论。

`result.json` 保存精确命令、源哈希和原日志哈希；本目录按 basename 加 `.gz` 无损归档对应日志，保留原始 CRLF 和行末空格，避免 Git 文本规范化改变证据哈希。绝对 TEMP 路径是来源记录，不是要求重新读取该机器的凭据。

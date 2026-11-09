# 同步 Relay 暂占 2 小时补偿上线记录

2026-11-10 北京时间 00:25 完成上线验收与清理。无可信计费证据的同步 Relay 暂占，满2小时且请求已停止或记录的派发截止时间含宽限已到后，由后台执行平台补偿。已知费用仍正常结算；运行中、证据冲突、日志查询失败及异步任务不因满2小时直接退款。迟到凭证不自动追扣。

## 生产结果

| 项目 | 结果 |
| --- | --- |
| 源码 | `a552996db2adb65d9e2074dbfc2e97962d0ef9bd`，`production/kkrich` |
| 发布版本 | `kkai-prod-20261109.1794239898-a552996db` |
| 镜像 | `sha256:2b8f3b00ada10d0a259f108d8e34f3129fe59b88978c51c34e73426f35d76c76` |
| 生产 active / writer | blue / 独立 writer，均为本次版本 |
| 回退版本 | green，`kkai-prod-20261109.1794233284-fc46400f3`，rollback ready |
| 恢复开关 | `apply`，generation `290edbfaf2474792b6f2beee231ad890` |
| 控制器及应用 pin | `29e9a8e927ea35df939e912baa381e663591f4a2`，未重装 |
| 数据库 | 现有 canonical21，feature runtime兼容14至21；本次未迁移 |
| 前端 | 现有 format2，current/previous路径与manifest hash保持一致 |

只把四处后台年龄判断统一为7200秒，并更新审计文案。正常请求预占、上游调用和结算没有增加同步SQL或首内容前操作；一分钟调度、每轮25笔aged资金调整预算保持原值。2小时是补偿资格时间，不是第120分钟整点完成承诺，历史积压仍需后台分批处理。

## 实际返还与账务

截至北京时间 **00:23:39**，本次启用后累计自动补偿 **150笔**，均为无证据补偿、终态released；可信费用自动结算样本为0，不能据此推断全部历史请求无费用。

| 资金来源 | 已返还笔数 | 实际返还 quota |
| --- | ---: | ---: |
| 会员额度 | 86 | 4,279,944 |
| 普通钱包 | 64 | 12,544,023 |
| 合计 | 150 | 16,823,967 |

金额取数据库终态的 `sum(reserved_quota - charged_quota)`，不是worker的adjusted_quota或全部待核查金额。恢复自然运行6轮，每轮释放25笔、刷新25笔缓存，failures均为0。

发布前、首轮、两次固定样本及最终共5个只读快照，全scope会员账户reserved/used与账本汇总一致；负金额、余额越界、归属错误、终态公式错误、Token退款遗漏和恢复表一致性异常均为0。固定T0=1794239950、T1=1794241132的首批25笔，在后续轮次金额指纹保持 `15ede1b82575d6f053a0e123a327c8a1`，未观察到重复资金变化；这不是形式化exactly-once证明。

最终快照仍有会员751笔、钱包499笔满2小时暂占待后台判断；此数包含仍需证据和资格检查的记录，不能全部宣布应退。普通业务同时产生新暂占，库存变化不等于退款金额。后台apply继续运行，没有提高批量预算或人工绕过账务事务。

## 发布与业务验收

本机构建一次，stage仅替换idle槽。候选会员/钱包各一次最小真实SSE均输出完整、返回200，会员唯一结算且来源与金额公式正确，钱包消费日志存在且非负。公网切换后及实际后台退款进行中，各再验证一次同样的会员/钱包流程，全部通过；没有在生产注入失败账单。

灰度10%、50%、100%每档120秒，精确rollout ID为 `7d656e43a7c645ad8a956f543b4e4d69`。各档目标完成HTTP样本28/197/551，HTTP5xx为0/3/1；后两档是relay_chat错误，未单独确诊其根因。独立精确runtime diff仅后台年龄条件，灰度时恢复持久paused，未改变前台/provider共享代码；结合候选业务结果，复核未发现本次特有回归。不能声称全站零错误。

16:16:41 UTC promotion成功；即时16:17:10、延迟16:22:19及清理后16:24:51的正式status均HEALTHY、精确版本匹配、rollback ready。16:18:29 UTC正式恢复apply。

发布前五分钟777个模型事件/20错误，发布后固定五分钟430/3（404两次、502一次），已等待额外60秒日志保护并审阅。状态码在发布前已有，重复签名仅作辅助；同时确认后台6轮无失败、资金对账和公网流程正常。模型事件率不是最终用户失败率，未以降低的错误率单独证明无回归。普通HTTP响应头和全请求耗时也不等同首内容时间；本次没有重做性能压测，不承诺数学意义上的零延迟变化。

## 验证命令

初次定向命令：service PASS；model因新测试错误调用了“传输已经停止”标记而失败。只修正测试fixture，未修改生产逻辑迎合测试。原失败日志保留。

```sh
KKAI_MEMBERSHIP_ENABLED=true GOMAXPROCS=2 go test -p 1 ./model ./service \
  -run '^TestMembership(Recovery|RelayReconcile|BillingReview|EvidenceSuccessAddsNoSynchronousSQL)' \
  -count=1 -timeout=120s
```

修正后仅重跑相关model范围，PASS：

```sh
KKAI_MEMBERSHIP_ENABLED=true GOMAXPROCS=2 go test -p 1 ./model \
  -run '^TestMembership(Recovery|RelayReconcile|BillingReview)' \
  -count=1 -timeout=120s
git diff --check
```

应用全套未运行；本次基础设施、schema、配置和控制器未修改，不重复此前已获授权并通过的infra完整check。实际构建、stage、rollout、promote、正式status、错误窗口及只读账务命令的精确参数、起止时间、返回码均在[证据目录](membership-recovery-evidence-20261109/two-hour-production/README.md)的 `*.result.json` 中。

## 清理、回退与限制

本机盘点本次确认的app/infra目录，仅有本次94,277,632字节发布tar，按SHA256 `421984c31eef75fc2bc9d41a71d18f30b272411e7b9038bf6b654b59780802cd`核验后删除；保留metadata及证据，本机无该类镜像标签残留。远端上传tar、候选状态/代理容器、已知隧道/socket均无残留。173个managed后端release和95个镜像受现有v10 schema snapshot冻结保护，按控制器提示保留；117个前端目录均受指针或30天窗口保护，无到期项。本次没有清理失败或无归属删除项，未手删线上release/image/frontend。

清理后current/rollback、三应用进程身份和前端hash与晋级后快照一致。回退使用已安装控制器和兼容21的旧版本，操作先持久暂停恢复；回退不撤销已经完成的返还，也不删除恢复表或还原旧业务库。源码同步在生产完成后独立进行，不影响本次运行镜像。

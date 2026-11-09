# 会员暂占恢复：B 性能验收与修复证据

这是本机隔离 PostgreSQL 和固定合成上游的证据，不是生产发布回执。最终候选业务源码为 `fc46400f3fed2647fafadd974ea1fc8d005d6015`。

## 最终裁决

建议最终候选进入正式构建与受控 apply 验收。已修复的积压争用有实际查询下降佐证；最终固定压力/生产节拍跟进没有主指标或辅助 P99 的双轮冻结边界越界。不能将其表述为数学意义的零开销，也不能替代生产候选、灰度、错误窗口和回退门禁。

最终跟进共12格、2,400请求：候选8格/1,600请求，历史同窗对照4格/800请求。全部成功，SQL错误、未结算账本、连接池等待均为0；501笔已知冲突保留资金。真实控制轮询16次，worker52轮（包含历史对照）。没有为最终SHA重跑不相关完整矩阵。

## 版本与证据关系

| 源码 | 覆盖 | 结果与处置 |
| --- | --- | --- |
| `cc99ff13494538bacfad06d215415d761013988d` | 完整32场景×2次，共64格/12,800候选请求 | 功能全通过；压力下1个重复主指标、2个辅助P99越界。原结果保留。 |
| `c6fc071f87b81c886935cf9e3ac806aca645cfe0` | 去除PG/MySQL lease无效UPDATE后，4压力格+8生产节拍ABBA，共12格/2,400请求 | 功能通过，虽然无冻结双轮flag，501同窗完成P95仍+15%、吞吐−9%；未据此启用apply。 |
| `fc46400f3fed2647fafadd974ea1fc8d005d6015` | terminal-only conflict与aged批量状态查询后，原样复用上述12格 | 功能通过，积压额外SQL从203条降至4条，重大争用消除；允许受控上线。 |

最终源码只针对有证据的两个场景补验：会员、4用户、SSE、unlimited令牌、0/501冲突。其它会员/钱包、单/多用户、SSE/JSON和finite/unlimited完整覆盖来自cc99，不伪称最终SHA完整重跑。相关行为/模式/并发保护的单元与真实DB验证由对应实现任务记录。

## 固定方法与边界

每格200请求、并发4、连接池8、GOMAXPROCS4、4次预热。走真实TextHelper、adapter、scanner和账务路径，首SSE和实际首内容独立记录。鉴权、渠道分发、输入分词、真实上游和生产规模不在覆盖范围；Redis在隔离夹具中关闭。

正式压力工具每100ms尝试启动真实worker，轮次不重叠；启动频率上限为生产每60秒一次的600倍，实际轮数在原始报告中。B独立1秒控制轮询真实执行，未mock。生产节拍诊断只改变测试工具调度：请求开始时立即运行一轮真实worker，下轮60秒后；短约1秒的200请求窗口完整覆盖启动轮，不可把该窗口差异外推为整分钟持续损失。生产节拍二进制的业务代码保持精确Git源码，只有测试调度overlay。

基线在候选执行前冻结，三次历史运行的各格min/max不再放宽。两次候选都劣于冻结最坏边界时触发调查；P99仅200样本，作为辅助，不夸大精度。同窗ABBA和SQL/worker证据参与技术判断，不能仅机械凭flag通过。

## 最终性能与查询证据

| 场景 | 历史同窗首内容P50 ms | 候选首内容P50 ms | 历史完成P95 ms | 候选完成P95 ms |
| --- | --- | --- | --- | --- |
| production-501 | 11.699 / 11.792 | 11.876 / 11.799 | 23.031 / 22.680 | 24.158 / 22.425 |
| production-0 | 11.844 / 11.881 | 11.847 / 11.808 | 27.333 / 22.892 | 22.726 / 27.431 |

501生产节拍同窗：首内容P50中位差约+0.092ms/+0.78%，完成P95中位约+0.435ms/+1.91%，没有同向尾延迟回退。吞吐中位仍比同窗历史低1.87%，如实保留；候选自身501/0积压吞吐中位约191.512/191.105请求/秒，几乎一致，而历史两场景约195.167/191.278，本身有约2%的变化。该结果不证明全部差值都属于噪声，但不再支持原先明显的积压持续拖慢。

实际SQL（含后台）同样支持修复效果：c6fc的501/0为8,659/8,456；fc464为8,460/8,456，积压额外查询203→4（每200请求）。501单轮worker活跃时间由150–197ms降至44–47ms。原因是停止反复延期未变化reserved冲突，并把aged页100次逐行状态查询改为一次批量读取；资金预算、fence和事务终态重验保留。

最终100ms压力下501首内容P50为12.351/12.668ms（原边界13.164），完成P95为26.232/24.744ms，吞吐184.177/182.972请求/秒；SQL8,990，真实worker每格11轮。0积压SQL8,966。所有原慢样本、失败pilot及未接受版本证据均保留。

## 精确命令与结果

完整cc99：以下四条分别16格/3,200请求，全部退出0。DSN仅由本地0600凭据文件注入；archive中的脚本不含凭据。

```sh
python3 run-batch-B.py candidate-B candidate-B-membership-1 --scope '^TestMembershipRecoveryHTTPMeasurement/postgres/membership/users-(1|4)/conflicts-(0|501)/apply/(sse|json)/(finite|unlimited)$' --samples 200
python3 run-batch-B.py candidate-B candidate-B-wallet-1 --scope '^TestMembershipRecoveryHTTPMeasurement/postgres/wallet/users-(1|4)/conflicts-(0|501)/apply/(sse|json)/(finite|unlimited)$' --samples 200
python3 run-batch-B.py candidate-B candidate-B-membership-2 --scope '^TestMembershipRecoveryHTTPMeasurement/postgres/membership/users-(1|4)/conflicts-(0|501)/apply/(sse|json)/(finite|unlimited)$' --samples 200
python3 run-batch-B.py candidate-B candidate-B-wallet-2 --scope '^TestMembershipRecoveryHTTPMeasurement/postgres/wallet/users-(1|4)/conflicts-(0|501)/apply/(sse|json)/(finite|unlimited)$' --samples 200
```

c6fc与fc464各自的固定12格命令，均退出0；每序列固定4格，未重测直到通过。

```sh
python3 run-final-sequence.py stress
python3 run-final-sequence.py production-501
python3 run-final-sequence.py production-0
python3 run-fc464-sequence.py stress
python3 run-fc464-sequence.py production-501
python3 run-fc464-sequence.py production-0
```

编译只使用带指定build tag的`go test -c -mod=readonly ... ./service`，精确参数、时长与测试二进制SHA256见各`*-build.json`和`candidate-B-source-manifest.json`。全仓测试未运行。生产镜像由发布任务单独构建，此处.test二进制不是生产镜像。

## 归档和复现

`manifest.json`记录每个源文件和压缩文件SHA256；`.gz`可逆解压后即原样JSONL/日志。每条请求样本、mode、worker/poll轮数、错误、SQL与资源字段均保存。原A完整证据位于同级`performance-A/`；冻结基线SHA256为`59a8a735c193f540bfa8bd5d643be5e1ac21fff444be0940de7e9e54a67af8ae`。

`B-production-diagnostic-plan.json`是cc99发现明确代码问题之前准备但未执行的计划；随后c6fc/fc464执行的是`B-followup-protocol.json`，fc464适用记录见`fc464-followup-protocol.json`。`candidate-B-production`临时源副本从未测量；不要把它算入验收。

先按来源Git提交导出源码。压力测试使用提交内工具；历史baseline使用performance-A中冻结的四份测试overlay；生产节拍工具另用本目录`tool-overlay/`的匹配HTTP测试文件。运行时仍需本机独立测试数据库/端口和外部0600凭据文件。不要复用生产数据库。

## 租约保护最小修复附件

[c6fc 租约保护验证](leader-lock-verification/README.md)保留精确命令、源文件和日志哈希。首次四模式实际跳过的边界与随后启用会员补跑通过的结果分别记录，不合并成一次完整通过。

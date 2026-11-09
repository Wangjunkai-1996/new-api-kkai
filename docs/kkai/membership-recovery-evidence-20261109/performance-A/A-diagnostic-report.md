# A 固定版本性能诊断与灰度裁决

记录时间：2026-11-09T13:04:01.759796+00:00。候选 A：`2648161aba4755a4cc7f75a10098deb3cbbd1d4a`，固定 `paused`；历史基线：`922db66baa46c07b5f36cdce8191a52d9af519db`。

## 裁决

建议允许 A 固定 paused 进入项目规定的受控灰度。功能验收通过，原重复越界在预先固定的同源/跨源诊断中没有复现，未发现会使正常前台额外进行 SQL 或后台资金调整的具体路径。此裁决不将原性能异常改写为 PASS，也不宣称零开销。B 必须独立验收真实持久控制轮询与恢复 worker。

A 保持 paused、按现有发布门禁检查首内容/完成 P95、吞吐、SQL/结算错误、未结清额度年龄、数据库锁等待和资源。出现可重复性能恶化或任何账务错误即停止放量并回滚。完整鉴权、分发、输入分词、真实上游和生产规模不在本机 HTTP 试验覆盖范围。

## 原始验收记录

历史基线 96 格、19,200/19,200 请求成功；A 64 格、12,800/12,800 请求成功。SQL 错误与未结算账本均为零。每格 200 请求、并发 4、池 8、GOMAXPROCS 4。首内容与首 SSE 独立统计。正常 worker 每 60 秒运行，试验将 worker 加速为每 100ms 启动一次且不重叠。

基线各格先执行三次并冻结 min/max，候选执行两次；两次均比冻结最坏边界差的主指标触发调查。以下 8 个 flag 保留，不放宽边界：

| 场景 | 指标 | 冻结边界 | 原 A 两次 | 相对边界变化 |
| --- | --- | ---: | --- | --- |
| membership|4|0|sse|False | complete_p95_ms | 29.114 | 30.856 / 123.149 | +5.98% / +322.99% |
| membership|4|0|sse|True | content_p50_ms | 12.971 | 13.463 / 16.004 | +3.79% / +23.38% |
| membership|4|0|sse|True | complete_p50_ms | 21.981 | 22.855 / 26.902 | +3.97% / +22.39% |
| membership|4|0|sse|True | requests_per_second | 173.897 | 170.039 / 134.692 | -2.22% / -22.54% |
| wallet|1|501|sse|True | complete_p95_ms | 54.416 | 464.735 / 54.809 | +754.04% / +0.72% |
| wallet|1|501|json|False | content_p95_ms | 58.541 | 85.334 / 58.617 | +45.77% / +0.13% |
| wallet|1|501|json|False | complete_p95_ms | 58.541 | 85.334 / 58.617 | +45.77% / +0.13% |
| wallet|1|501|json|False | requests_per_second | 85.503 | 68.420 / 81.347 | -19.98% / -4.86% |

## 固定诊断

执行前写入 `diagnostic-plan.json`。仅诊断上述四场景，逐场采用 baseline → A → A → baseline（连续 worker），随后 baseline → A（worker none）。共 24 格、4,800/4,800 请求成功，SQL 错误 0、未结算 0，所有 501 笔冲突保留资金。未扩大样本、未重跑到通过、未更改历史二进制或冻结边界。加入相同 50ms PostgreSQL 等待/阻塞采样及容器 CPU/IO 计数，诊断结果与原正式试验分开。

| 场景 | 历史诊断完成 P95 ms | A 诊断完成 P95 ms | 首内容 P50 中位变化 | 吞吐中位变化 |
| --- | --- | --- | ---: | ---: |
| member4-sse-finite | 23.464 / 24.136 | 23.129 / 23.611 | +0.76% | +0.58% |
| member4-sse-unlimited | 23.742 / 23.741 | 24.636 / 24.607 | +2.25% | -1.28% |
| wallet1-conflict-sse-unlimited | 47.948 / 46.789 | 41.156 / 40.515 | -5.55% | +8.08% |
| wallet1-conflict-json-finite | 46.381 / 45.814 | 38.679 / 45.235 | -4.57% | +5.77% |

四个场景的 continuous A 两次均在原冻结范围内。会员 unlimited 仍可见小幅同向差异（首内容 P50 +2.25%、完成 P50 +1.51%、吞吐 −1.28%）；不把它描述为绝对没有开销。无 worker 对照每 200 请求 SQL 完全一致：会员 8,400，钱包 7,400。无 worker 的会员 finite 单次 A 完成 P95 为31.803ms、对照23.343ms；该单次差异亦保留，不能据一次快/慢结果下总体结论。

## 资源与根因边界

24 格容器 CPU cgroup 的 throttled_usec 增量全部为 0，无连接池等待、无新 OOM。采样见 ClientRead、WALSync 和同用户资金锁竞争；多用户场景阻塞观测很少。CPU 与分配量未与原数倍尾延迟成比例增长，前台 SQL 数量无增加，因此原异常更符合等待/环境敏感性，未证明由某一新增代码路径造成。

采样覆盖整个测试进程（含建表/种子数据/清理），且未捕获原正式试验的 outlier，不能据此把原慢请求精确归因于磁盘、锁、宿主调度或网络。当前证据支持“未复现持续性退化，可以受控灰度观察”，不支持“根因已经完全证明为环境噪声”。

## 精确诊断命令与边界

以下四条全部退出 0，各命令硬上限270秒、单个子命令40秒；未运行全仓测试。凭据仅从权限0600的本地文件注入，不进入报告。

```sh
python3 diagnose.py member4-sse-finite
python3 diagnose.py member4-sse-unlimited
python3 diagnose.py wallet1-conflict-sse-unlimited
python3 diagnose.py wallet1-conflict-json-finite
python3 summarize-diagnostics.py
```

脚本、固定方案、原 JSONL、逐请求样本、PG等待采样、每格回执、原A比较和冻结边界均保留。冻结文件 SHA-256：`59a8a735c193f540bfa8bd5d643be5e1ac21fff444be0940de7e9e54a67af8ae`。A 的源导出与测试二进制保持不变。

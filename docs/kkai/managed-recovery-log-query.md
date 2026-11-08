# 受管恢复效果：操作员查询

无需统计接口或页面。数据保存在现有 NewAPI `logs.other.admin_info`：

- `managed_recovery_entry`：普通请求已握手 `handshake_succeeded`、无 `previous_response_id` 密文请求已握手 `encrypted_input_opaque`、密文与非空 ID 并存时排除 `excluded_encrypted_continuation`、JSON/不可重放体排除、Sub2 能力降级，以及握手前传输/HTTP/协议错误。旧版本的 `excluded_encrypted_input` 仍可查询。
- `managed_recovery`：逻辑终态、`same_account_retries`、`account_switches`、`stop_reason` 和 `protocol_error`。历史日志没有这些计数时只能标为未知，不能当作零次重试。

生产查询前按 NewAPI 发布入口确认当前 checkout，并从同级基础设施 checkout 执行一次 `make newapi-status`。随后从本仓库执行下面的只读 PostgreSQL 查询；替换 UTC 时间、渠道和分组，窗口最多六小时：

```sh
ssh -o BatchMode=yes -o ConnectTimeout=8 sys1 \
  "sudo -n docker exec -i kkai-postgres psql -X -qAt -v ON_ERROR_STOP=1 -U postgres -d newapi \
  --set=start_utc=2026-11-06T08:07:55Z --set=end_utc=2026-11-06T10:46:30Z \
  --set=channel=31 --set=group=default" \
  < scripts/kkai/query-managed-recovery.sql
```

脚本使用 `BEGIN READ ONLY`、`newapi_standby` 和八秒超时，只输出固定分类与数量，不输出客户正文、账号或请求 ID。分组按精确值匹配；带 shell 特殊字符的值需正确引用，不能直接拼接不可信输入。

查询包含消费/错误/取消日志，时间为 `[start, end)`。同一 `request_id` 只取窗口内最新一行；没有请求 ID 的历史行按单条日志计数，无法证明彼此是否属于同一请求。结果含义：

| 字段 | 含义 |
| --- | --- |
| `total_requests` / `observed` / `managed` | 去重总数 / 有新增或既有诊断 / 已完成受管握手的请求数 |
| `counter_known` / `counter_unknown` | 受管请求中两项计数有效 / 缺失或无效的数量 |
| `retried_request_count` | 已知发生过同号重试或切号的请求数 |
| `same_account_retries` / `account_switches` | 已知的同号重试次数 / 切号次数 |
| `recovered` | 经重试或切号后，最新日志确实是消费日志（type=2）、终态为 `completed` 且 `protocol_error` 不是 `true` 的请求数；不把普通成功、错误日志或协议出错后的完成算成救回 |
| `failed` / `tool_stop` | 受管失败数 / 因已输出工具事件而停止恢复的失败数 |
| `entry_reasons` / `stop_reasons` | 请求进入或排除原因 / 受管失败终止原因的去重计数 |

密文恢复的停止原因包括：`route_unavailable`（原路线不再可用）、
`opaque_context_rejected`（上下文不支持安全恢复）及 `invalid_encrypted_content`（上游拒绝密文）。
`encrypted_input_opaque` 已握手但缺少终态计数的请求仍计入 `counter_unknown`。

`counter_unknown > 0` 表示这些请求的救回效果不能精确统计。没有受管诊断也不等于未重试：非受管路径仍可能使用 Sub2 既有重试；必要时用 NewAPI `upstream_request_id` 与 Sub2 请求 ID 的 SHA-256 关联，并只输出聚合。Sub2 ops 中的最终 `upstream_status_code` 可能是合成值，不能当作提供商原始 HTTP 状态。

非流式和流前失败的受管内部响应头（`X-KKAI-Recovery-Diagnostics`）应在 NewAPI 入口转换到同一 `admin_info.managed_recovery` 结构；客户端不会看到该头。若 Sub2 断开且没有 terminal，Sub2 会记录 `openai.recovery_incomplete`，并保留已输出工具事件的守卫结果。查询时这类记录没有 `final_outcome=completed`，应留在 `counter_unknown`/失败诊断中，不能算作救回。

# KKAI Group Status

`GET /api/status/groups` returns health information only for groups visible to
the authenticated user. Supported windows are `now`, `15m`, `1h`, `6h`, and
`24h`.

Each relay sample updates fork-owned minute and hour aggregates. Redis stores
the shared multi-instance aggregates and a bounded recent-event stream; the
same sample is kept in a bounded local fallback. Redis queries are authoritative
when available. If Redis is disabled or unavailable, the endpoint continues
with local signals and persisted `perf_metrics` rows instead of failing.

Polled asynchronous tasks, including Seedance videos, add one sample when the
task first reaches success or failure. Only the winning persisted transition
records it; pending states, transient polling errors, repeated polls, and
terminal metadata updates do not add samples. This also covers batch/Suno
polling and terminal polling failures/timeouts. Tasks do not enter the
text-request cache rate.

The selected window controls health, success rate, latency, TTFT, and staleness.
It does not filter `recent_events`: every visible group returns its latest 60
requests, ordered oldest to newest, even when those requests predate the selected
window. Recent-event streams and local fallbacks are retained by bounded count
rather than by age. Redis and local events share stable IDs, so a Redis recovery
can merge locally buffered outage events without duplicating successful writes.
Background maintenance removes legacy TTLs and keeps streams at the same
60-event bound, including after rollback traffic from an older release;
steady-state status reads remain read-only. A group that has never received a
request returns an empty `recent_events` array.

Long windows de-duplicate database and Redis aggregates per group and hour.
When both sources cover the same hour, the larger aggregate is retained and
the latest live sample timestamp is preserved. This avoids double-counting and
prevents a partial Redis bucket after a restart from replacing a more complete
persisted bucket.

`sampled_at` and `updated_at` represent the latest observed request timestamp,
not response generation time. Old samples are marked `stale` and classified as
`unknown` even when their historical success rate was high. This prevents an
idle or disconnected group from appearing healthy indefinitely.

The `auto` group is an aggregate of the configured auto-group members that are
also visible to the user. It never exposes metrics for a hidden group.

Cache hit rate always uses the last 24 hours, independently of the selected
health window. It is the share of eligible successful text requests with
reported, non-estimated usage whose cached input exceeds 30% of the normalized
prompt context. Only non-streaming client requests with zero cache-read tokens
are excluded from the eligible samples. Streaming requests with zero cache-read
tokens remain in the denominator; non-streaming requests with positive cache-read
tokens are included, even when their cached input does not exceed the 30% hit
threshold. The original client stream flag determines whether a request is
non-streaming, including when the relay streams internally. All requests still
contribute to health and request counts.

The existing Redis v5 cache counters are retained without resetting accumulated
samples. Buckets written before this correction contain streaming samples only,
so that portion of the 24-hour window lacks eligible non-streaming requests;
those samples cannot be reconstructed from the aggregates and age out normally.
Earlier v4 counters remain ignored. Cache hit rate is hidden when Redis is
unavailable or there are no hits.

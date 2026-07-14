# KKAI Group Status

`GET /api/status/groups` returns health information only for groups visible to
the authenticated user. Supported windows are `now`, `15m`, `1h`, `6h`, and
`24h`.

Each relay sample updates fork-owned minute and hour aggregates. Redis stores
the shared multi-instance aggregates and a bounded recent-event stream; the
same sample is kept in a bounded local fallback. Redis queries are authoritative
when available. If Redis is disabled or unavailable, the endpoint continues
with local signals and persisted `perf_metrics` rows instead of failing.

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

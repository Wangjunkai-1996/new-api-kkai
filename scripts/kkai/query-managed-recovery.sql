-- PostgreSQL operator query. Required psql variables: start_utc, end_utc,
-- channel (positive integer), group (exact group name). No request data is emitted.
\set ON_ERROR_STOP on
BEGIN READ ONLY;
SET LOCAL ROLE newapi_standby;
SET LOCAL statement_timeout = '8s';
SET LOCAL lock_timeout = '3s';
SELECT 1 / CASE WHEN :'end_utc'::timestamptz > :'start_utc'::timestamptz
  AND :'end_utc'::timestamptz - :'start_utc'::timestamptz <= interval '6 hours'
  AND :'channel'::integer > 0 AND :'group' <> '' THEN 1 ELSE 0 END AS valid_window;

WITH latest AS MATERIALIZED (
  SELECT DISTINCT ON (coalesce(nullif(request_id, ''), 'log:' || id::text))
    type AS log_type,
    CASE WHEN pg_input_is_valid(other, 'jsonb') THEN other::jsonb ELSE '{}'::jsonb END AS j
  FROM public.logs
  WHERE created_at >= extract(epoch FROM :'start_utc'::timestamptz)
    AND created_at < extract(epoch FROM :'end_utc'::timestamptz)
    AND channel_id = :'channel'::integer AND "group" = :'group'
    AND type IN (2, 5, 8)
  ORDER BY coalesce(nullif(request_id, ''), 'log:' || id::text), created_at DESC, id DESC
), raw AS MATERIALIZED (
  SELECT j#>>'{admin_info,managed_recovery_entry}' AS entry,
    j#>'{admin_info,managed_recovery}' AS m,
    log_type
  FROM latest
), facts AS MATERIALIZED (
  SELECT coalesce(entry <> '' OR jsonb_typeof(m) = 'object', false) AS observed,
    coalesce(entry IN ('handshake_succeeded', 'encrypted_input_opaque') OR jsonb_typeof(m) = 'object', false) AS managed,
    CASE WHEN entry IN ('handshake_succeeded', 'excluded_non_replayable_body',
      'excluded_json_output', 'excluded_encrypted_input', 'excluded_encrypted_continuation', 'encrypted_input_opaque',
      'capability_fallback',
      'upstream_non_2xx', 'upstream_transport_error', 'protocol_header_missing') THEN entry
      WHEN coalesce(entry, '') = '' THEN 'legacy_unrecorded' ELSE 'other_or_unknown' END AS entry_reason,
    m->>'final_outcome' AS outcome,
    log_type = 2 AS final_consume,
    coalesce(m->>'protocol_error', '') <> 'true' AS protocol_ok,
    CASE WHEN jsonb_typeof(m->'same_account_retries') = 'number'
      AND m->>'same_account_retries' ~ '^[0-9]{1,2}$'
      THEN (m->>'same_account_retries')::integer END AS same_retries,
    CASE WHEN jsonb_typeof(m->'account_switches') = 'number'
      AND m->>'account_switches' ~ '^[0-9]{1,2}$'
      THEN (m->>'account_switches')::integer END AS switches,
    CASE WHEN m->>'stop_reason' IN ('tool_output_started', 'retry_not_allowed',
      'no_viable_account', 'logical_deadline', 'delivery_reserve',
      'retry_budget_exhausted', 'dispatch_limit', 'upstream_error',
      'route_unavailable', 'opaque_context_rejected', 'invalid_encrypted_content') THEN m->>'stop_reason'
      WHEN coalesce(m->>'stop_reason', '') = '' THEN 'missing' ELSE 'other_or_unknown' END AS stop_reason
  FROM raw
), classified AS MATERIALIZED (
  SELECT *, coalesce(same_retries BETWEEN 0 AND 10 AND switches BETWEEN 0 AND 10
    AND same_retries + switches <= 10, false) AS counters_known
  FROM facts
)
SELECT jsonb_build_object(
  'total_requests', count(*),
  'observed', count(*) FILTER (WHERE observed),
  'managed', count(*) FILTER (WHERE managed),
  'counter_known', count(*) FILTER (WHERE managed AND counters_known),
  'counter_unknown', count(*) FILTER (WHERE managed AND NOT counters_known),
  'retried_request_count', count(*) FILTER (WHERE managed AND counters_known AND same_retries + switches > 0),
  'same_account_retries', coalesce(sum(same_retries) FILTER (WHERE managed AND counters_known), 0),
  'account_switches', coalesce(sum(switches) FILTER (WHERE managed AND counters_known), 0),
  'recovered', count(*) FILTER (WHERE managed AND counters_known AND final_consume
    AND protocol_ok AND outcome = 'completed' AND same_retries + switches > 0),
  'failed', count(*) FILTER (WHERE managed AND outcome = 'failed'),
  'tool_stop', count(*) FILTER (WHERE managed AND outcome = 'failed' AND stop_reason = 'tool_output_started'),
  'entry_reasons', (SELECT coalesce(jsonb_object_agg(entry_reason, n), '{}'::jsonb)
    FROM (SELECT entry_reason, count(*) AS n FROM classified WHERE observed GROUP BY entry_reason) AS entries),
  'stop_reasons', (SELECT coalesce(jsonb_object_agg(stop_reason, n), '{}'::jsonb)
    FROM (SELECT stop_reason, count(*) AS n FROM classified WHERE managed AND outcome = 'failed' GROUP BY stop_reason) AS stops)
)
FROM classified;
ROLLBACK;

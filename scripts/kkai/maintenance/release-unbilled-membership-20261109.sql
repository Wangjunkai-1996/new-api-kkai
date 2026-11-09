\set ON_ERROR_STOP on
\if :{?apply}
\else
\set apply 0
\endif
\if :{?account_filter}
\else
\set account_filter 0
\endif
\if :{?zero_usage}
\else
\set zero_usage 0
\endif
\if :{?operation_id}
\else
\echo operation_id is required
\quit 3
\endif

BEGIN;
SET LOCAL statement_timeout = '90s';
SET LOCAL lock_timeout = '5s';
SET LOCAL kkai.operation_id = :'operation_id';

CREATE TEMP TABLE membership_release_candidates ON COMMIT DROP AS
WITH pending AS MATERIALIZED (
  SELECT DISTINCT ON (b.request_id)
    b.request_id, b.user_id, b.account_id, b.token_id,
    b.token_reserved, b.token_quota_debited, b.reserved_quota,
    l.upstream_request_id,
    CASE WHEN l.other::jsonb->>'status_code' ~ '^[0-9]{3}$'
      THEN (l.other::jsonb->>'status_code')::integer END AS newapi_status
  FROM kkai_membership_billing b
  JOIN logs l ON l.request_id = b.request_id AND l.type = 5
    AND l.channel_id = b.review_channel_id AND l.upstream_request_id <> ''
  JOIN channels c ON c.id = b.review_channel_id AND c.base_url LIKE '%sub2api-router%'
  JOIN kkai_membership_accounts a ON a.id = b.account_id
    AND a.user_id = b.user_id AND a.status = 'active'
  JOIN kkai_membership_users s ON s.user_id = b.user_id
    AND s.current_account_id = a.id
  WHERE b.source = 'membership' AND b.billing_scope = 'relay'
    AND b.state = 'reserved' AND b.review_started_at > 0
    AND b.created_at < extract(epoch FROM now()) - 86400
    AND (:account_filter = 0 OR b.account_id = :account_filter)
  ORDER BY b.request_id, l.id DESC
), ids AS (
  SELECT DISTINCT upstream_request_id FROM pending
), usage_rows AS MATERIALIZED (
  SELECT * FROM dblink(
    'dbname=sub2api options=-cdefault_transaction_read_only=on',
    'SELECT request_id, count(*), bool_and(' ||
      'coalesce(input_tokens,0)=0 AND coalesce(output_tokens,0)=0 ' ||
      'AND coalesce(cache_creation_tokens,0)=0 AND coalesce(cache_read_tokens,0)=0 ' ||
      'AND coalesce(image_count,0)=0 AND coalesce(video_count,0)=0 ' ||
      'AND coalesce(image_output_tokens,0)=0 AND coalesce(image_input_tokens,0)=0 ' ||
      'AND actual_cost=0 AND total_cost=0) ' ||
      'FROM usage_logs WHERE request_id IN (' ||
      (SELECT string_agg(quote_literal('client:' || upstream_request_id), ',') FROM ids) ||
      ') GROUP BY request_id'
  ) AS u(request_id text, usage_count bigint, zero_usage boolean)
), billed_rows AS MATERIALIZED (
  SELECT * FROM dblink(
    'dbname=sub2api options=-cdefault_transaction_read_only=on',
    'SELECT DISTINCT request_id FROM usage_billing_dedup WHERE request_id IN (' ||
      (SELECT string_agg(quote_literal('client:' || upstream_request_id), ',') FROM ids) || ')'
  ) AS d(request_id text)
), errors AS MATERIALIZED (
  SELECT * FROM dblink(
    'dbname=sub2api options=-cdefault_transaction_read_only=on',
    'SELECT client_request_id, min(status_code), max(status_code), max(created_at) '
      || 'FROM ops_error_logs WHERE client_request_id IN (' ||
      (SELECT string_agg(quote_literal(upstream_request_id), ',') FROM ids) ||
      ') GROUP BY client_request_id'
  ) AS e(client_request_id text, min_status integer, max_status integer, created_at timestamptz)
), coverage AS (
  SELECT oldest FROM dblink(
    'dbname=sub2api options=-cdefault_transaction_read_only=on',
    'SELECT min(created_at) FROM usage_logs'
  ) AS c(oldest timestamptz)
)
SELECT p.request_id, p.user_id, p.account_id, p.token_id,
  p.token_reserved, p.token_quota_debited, p.reserved_quota,
  p.upstream_request_id, e.max_status AS upstream_status
FROM pending p
JOIN errors e ON e.client_request_id = p.upstream_request_id
CROSS JOIN coverage c
LEFT JOIN usage_rows u ON u.request_id = 'client:' || p.upstream_request_id
LEFT JOIN billed_rows d ON d.request_id = 'client:' || p.upstream_request_id
WHERE p.newapi_status BETWEEN 400 AND 599
  AND e.min_status BETWEEN 400 AND 599 AND e.max_status BETWEEN 400 AND 599
  AND e.created_at > c.oldest + interval '5 minutes'
  AND ((:zero_usage = 0 AND u.request_id IS NULL AND d.request_id IS NULL)
    OR (:zero_usage = 1 AND u.usage_count = 1 AND u.zero_usage));

DO $$
DECLARE n integer;
BEGIN
  SELECT count(*) INTO n FROM membership_release_candidates;
  IF n < 1 OR n > 800 OR n <> (SELECT count(DISTINCT request_id) FROM membership_release_candidates)
    OR n <> (SELECT count(DISTINCT upstream_request_id) FROM membership_release_candidates) THEN
    RAISE EXCEPTION 'invalid reconciliation candidate count: %', n;
  END IF;
  IF EXISTS (SELECT 1 FROM membership_release_candidates
             WHERE token_quota_debited OR token_reserved < 0 OR reserved_quota < 0
                OR token_reserved > reserved_quota) THEN
    RAISE EXCEPTION 'candidate requires unsupported token adjustment';
  END IF;
END $$;

SELECT count(*) AS requests, count(DISTINCT user_id) AS users,
  round(sum(reserved_quota)::numeric / 500000, 2) AS refund_usd
FROM membership_release_candidates;

SELECT count(*) AS locked_users FROM (
  SELECT id FROM users WHERE id IN (SELECT user_id FROM membership_release_candidates)
  ORDER BY id FOR UPDATE
) locked;
SELECT count(*) AS locked_membership_states FROM (
  SELECT user_id FROM kkai_membership_users
  WHERE user_id IN (SELECT user_id FROM membership_release_candidates)
  ORDER BY user_id FOR UPDATE
) locked;
SELECT count(*) AS locked_billings FROM (
  SELECT request_id FROM kkai_membership_billing
  WHERE request_id IN (SELECT request_id FROM membership_release_candidates)
  ORDER BY request_id FOR UPDATE
) locked;
SELECT count(*) AS locked_accounts FROM (
  SELECT id FROM kkai_membership_accounts
  WHERE id IN (SELECT account_id FROM membership_release_candidates)
  ORDER BY id FOR UPDATE
) locked;
SELECT count(*) AS locked_tokens FROM (
  SELECT id FROM tokens WHERE id IN (
    SELECT token_id FROM membership_release_candidates WHERE token_reserved > 0
  ) ORDER BY id FOR UPDATE
) locked;

DO $$
DECLARE n integer;
BEGIN
  SELECT count(*) INTO n FROM membership_release_candidates;
  IF n <> (SELECT count(*) FROM kkai_membership_billing b
           JOIN membership_release_candidates c ON c.request_id = b.request_id
           WHERE b.state = 'reserved' AND b.source = 'membership'
             AND b.user_id = c.user_id AND b.account_id = c.account_id
             AND b.reserved_quota = c.reserved_quota
             AND b.token_reserved = c.token_reserved
             AND b.review_started_at > 0) THEN
    RAISE EXCEPTION 'billing changed during reconciliation';
  END IF;
  IF EXISTS (
    SELECT 1 FROM (
      SELECT account_id, user_id, sum(reserved_quota) AS quota
      FROM membership_release_candidates GROUP BY account_id, user_id
    ) c JOIN kkai_membership_accounts a ON a.id = c.account_id
    JOIN kkai_membership_users s ON s.user_id = c.user_id
    WHERE a.user_id <> c.user_id OR s.current_account_id <> a.id
       OR a.amount_reserved < c.quota OR a.amount_used > a.amount_total
  ) THEN
    RAISE EXCEPTION 'membership account balance changed';
  END IF;
  IF EXISTS (
    SELECT 1 FROM (
      SELECT token_id, user_id, sum(token_reserved) AS quota
      FROM membership_release_candidates WHERE token_reserved > 0
      GROUP BY token_id, user_id
    ) c LEFT JOIN tokens t ON t.id = c.token_id AND t.user_id = c.user_id
    WHERE t.id IS NULL OR t.used_quota < c.quota
  ) THEN
    RAISE EXCEPTION 'token usage changed or token was removed';
  END IF;
END $$;

WITH totals AS (
  SELECT account_id, sum(reserved_quota) AS quota
  FROM membership_release_candidates GROUP BY account_id
), changed AS (
  UPDATE kkai_membership_accounts a
  SET amount_reserved = a.amount_reserved - t.quota,
    status = CASE WHEN a.status = 'active' AND a.expires_at <= extract(epoch FROM now())
      THEN 'expired' ELSE a.status END
  FROM totals t WHERE a.id = t.account_id RETURNING a.id
)
SELECT count(*) AS accounts_updated FROM changed;

WITH totals AS (
  SELECT token_id, sum(token_reserved) AS quota
  FROM membership_release_candidates WHERE token_reserved > 0 GROUP BY token_id
), changed AS (
  UPDATE tokens t SET used_quota = t.used_quota - x.quota
  FROM totals x WHERE t.id = x.token_id RETURNING t.id
)
SELECT count(*) AS tokens_updated FROM changed;

WITH changed AS (
  UPDATE kkai_membership_billing b
  SET state = 'released', actual_quota = 0, charged_quota = 0, excess_quota = 0,
    settled_at = extract(epoch FROM now())::bigint,
    token_used_refund_unapplied = 0, token_refund_unapplied = 0,
    review_resolved_at = extract(epoch FROM now())::bigint,
    review_resolution = jsonb_build_object(
      'operation_id', :'operation_id', 'actor_id', 0, 'action', 'release',
      'actual_quota', 0,
      'reason', CASE WHEN :zero_usage = 1
        THEN 'Sub2API terminal error; sole usage receipt has zero usage and zero charge'
        ELSE 'Sub2API terminal error; no retained usage or billing receipt after 24 hours' END,
      'operator', 'codex-direct-db', 'upstream_request_id', c.upstream_request_id,
      'upstream_status', c.upstream_status,
      'at', extract(epoch FROM now())::bigint
    )::text
  FROM membership_release_candidates c WHERE b.request_id = c.request_id
  RETURNING b.request_id
)
SELECT count(*) AS billings_released FROM changed;

INSERT INTO audit_logs (
  event_id, user_id, username, actor_role, created_at, category, action,
  auth_method, method, route, status, success, content, other
)
SELECT md5(:'operation_id'), 0, 'codex-direct-db', 0,
  extract(epoch FROM now())::bigint, 'operation', 'membership_bulk_release',
  'direct_db', 'SQL', 'membership_reconciliation', 200, true,
  CASE WHEN :zero_usage = 1
    THEN 'Released zero-charge membership reservations'
    ELSE 'Released verified unbilled membership reservations' END,
  jsonb_build_object('operation_id', :'operation_id',
    'requests', count(*), 'quota', sum(reserved_quota),
    'evidence', CASE WHEN :zero_usage = 1
      THEN 'NewAPI and Sub2API terminal errors; one zero-usage zero-charge receipt'
      ELSE 'NewAPI error plus Sub2API terminal error; no usage or billing dedup' END)::text
FROM membership_release_candidates;

DO $$
DECLARE n integer;
BEGIN
  SELECT count(*) INTO n FROM membership_release_candidates;
  IF n <> (SELECT count(*) FROM kkai_membership_billing b
           JOIN membership_release_candidates c ON c.request_id = b.request_id
           WHERE b.state = 'released' AND b.actual_quota = 0
             AND b.charged_quota = 0 AND b.review_resolved_at > 0
             AND b.review_resolution::jsonb->>'operation_id' = current_setting('kkai.operation_id')) THEN
    RAISE EXCEPTION 'billing release verification failed';
  END IF;
  IF EXISTS (
    SELECT 1 FROM kkai_membership_accounts a
    WHERE a.id IN (SELECT account_id FROM membership_release_candidates)
      AND a.amount_reserved <> coalesce((
        SELECT sum(b.reserved_quota) FROM kkai_membership_billing b
        WHERE b.account_id = a.id AND b.source = 'membership' AND b.state = 'reserved'
      ), 0)
  ) THEN
    RAISE EXCEPTION 'account reservation ledger mismatch after release';
  END IF;
END $$;

SELECT :'operation_id' AS operation_id,
  count(*) AS verified_released,
  round(sum(reserved_quota)::numeric / 500000, 2) AS released_usd
FROM membership_release_candidates;

\if :apply
COMMIT;
\echo COMMITTED
\else
ROLLBACK;
\echo DRY_RUN_ROLLED_BACK
\endif

\set ON_ERROR_STOP on
\if :{?apply}
\else
\set apply 0
\endif
\if :{?operation_id}
\else
\echo operation_id is required
\quit 3
\endif
\if :{?cutoff_epoch}
\else
\echo cutoff_epoch is required
\quit 3
\endif
\if :{?expected_count}
\else
\echo expected_count is required
\quit 3
\endif
\if :{?review_log_mode}
\else
\set review_log_mode 0
\endif

BEGIN;
SET LOCAL statement_timeout = '90s';
SET LOCAL lock_timeout = '5s';
SET LOCAL kkai.operation_id = :'operation_id';
SET LOCAL kkai.expected_count = :'expected_count';
SET LOCAL kkai.review_log_mode = :'review_log_mode';

CREATE TEMP TABLE membership_compensation_candidates ON COMMIT DROP AS
WITH pending AS MATERIALIZED (
  SELECT b.request_id, b.user_id, b.account_id, b.token_id,
    b.token_reserved, b.token_quota_debited, b.reserved_quota,
    b.created_at, l.type AS log_type, l.upstream_request_id,
    c.base_url AS channel_url
  FROM kkai_membership_billing b
  JOIN kkai_membership_accounts a ON a.id = b.account_id
    AND a.user_id = b.user_id AND a.status = 'active'
  JOIN kkai_membership_users s ON s.user_id = b.user_id
    AND s.current_account_id = a.id
  LEFT JOIN LATERAL (
    SELECT type, upstream_request_id, channel_id, request_id
    FROM logs WHERE request_id = b.request_id ORDER BY id DESC LIMIT 1
  ) l ON true
  LEFT JOIN channels c ON c.id = b.review_channel_id
  WHERE b.state = 'reserved' AND b.source = 'membership'
    AND b.billing_scope = 'relay' AND b.review_started_at > 0
    AND b.created_at < :cutoff_epoch
    AND ((:review_log_mode = 0 AND l.request_id IS NULL)
      OR (:review_log_mode = 1 AND l.type IN (5, 8)
        AND l.channel_id = b.review_channel_id
        AND (SELECT count(*) FROM logs other WHERE other.request_id = b.request_id) = 1))
), ids AS (
  SELECT DISTINCT upstream_request_id FROM pending
  WHERE channel_url LIKE '%sub2api-router%' AND upstream_request_id <> ''
), usage_rows AS MATERIALIZED (
  SELECT * FROM dblink(
    'dbname=sub2api options=-cdefault_transaction_read_only=on',
    'SELECT request_id, count(*), bool_and(' ||
      'coalesce(input_tokens,0)=0 AND coalesce(output_tokens,0)=0 ' ||
      'AND coalesce(cache_creation_tokens,0)=0 AND coalesce(cache_read_tokens,0)=0 ' ||
      'AND coalesce(image_count,0)=0 AND coalesce(video_count,0)=0 ' ||
      'AND coalesce(image_output_tokens,0)=0 AND coalesce(image_input_tokens,0)=0 ' ||
      'AND coalesce(actual_cost,0)=0 AND coalesce(total_cost,0)=0) ' ||
      'FROM usage_logs WHERE request_id IN (' ||
      coalesce((SELECT string_agg(quote_literal('client:' || upstream_request_id), ',') FROM ids), 'NULL') ||
      ') GROUP BY request_id'
  ) AS u(request_id text, usage_count bigint, zero_usage boolean)
)
SELECT p.request_id, p.user_id, p.account_id, p.token_id,
  p.token_reserved, p.token_quota_debited, p.reserved_quota,
  p.created_at, p.log_type, p.upstream_request_id
FROM pending p
LEFT JOIN usage_rows u ON u.request_id = 'client:' || p.upstream_request_id
WHERE :review_log_mode = 0
  OR p.channel_url NOT LIKE '%sub2api-router%'
  OR p.upstream_request_id = '' OR u.request_id IS NULL OR u.zero_usage
ORDER BY p.created_at, p.request_id
LIMIT 200;

DO $$
DECLARE n integer;
BEGIN
  SELECT count(*) INTO n FROM membership_compensation_candidates;
  IF n <> current_setting('kkai.expected_count')::integer OR n < 1 OR n > 200
    OR n <> (SELECT count(DISTINCT request_id) FROM membership_compensation_candidates) THEN
    RAISE EXCEPTION 'unexpected compensation candidate count: %', n;
  END IF;
  IF EXISTS (SELECT 1 FROM audit_logs WHERE event_id = md5(current_setting('kkai.operation_id'))) THEN
    RAISE EXCEPTION 'operation ID was already committed';
  END IF;
  IF EXISTS (SELECT 1 FROM membership_compensation_candidates
             WHERE reserved_quota < 0 OR token_reserved < 0
               OR token_reserved > reserved_quota
               OR (token_quota_debited AND token_reserved = 0)) THEN
    RAISE EXCEPTION 'invalid funding reservation';
  END IF;
END $$;

SELECT count(*) requests, count(DISTINCT user_id) users,
  sum(reserved_quota) quota,
  round(sum(reserved_quota)::numeric / 500000, 4) refunded_usd
FROM membership_compensation_candidates;

SELECT count(*) locked_users FROM (
  SELECT id FROM users WHERE id IN (SELECT user_id FROM membership_compensation_candidates)
  ORDER BY id FOR UPDATE
) locked;
SELECT count(*) locked_membership_states FROM (
  SELECT user_id FROM kkai_membership_users
  WHERE user_id IN (SELECT user_id FROM membership_compensation_candidates)
  ORDER BY user_id FOR UPDATE
) locked;
SELECT count(*) locked_billings FROM (
  SELECT request_id FROM kkai_membership_billing
  WHERE request_id IN (SELECT request_id FROM membership_compensation_candidates)
  ORDER BY request_id FOR UPDATE
) locked;
SELECT count(*) locked_accounts FROM (
  SELECT id FROM kkai_membership_accounts
  WHERE id IN (SELECT account_id FROM membership_compensation_candidates)
  ORDER BY id FOR UPDATE
) locked;
SELECT count(*) locked_tokens FROM (
  SELECT id FROM tokens WHERE id IN (
    SELECT token_id FROM membership_compensation_candidates WHERE token_reserved > 0
  ) ORDER BY id FOR UPDATE
) locked;

DO $$
DECLARE n integer;
BEGIN
  SELECT count(*) INTO n FROM membership_compensation_candidates;
  IF n <> (SELECT count(*) FROM kkai_membership_billing b
           JOIN membership_compensation_candidates c ON c.request_id = b.request_id
           WHERE b.state = 'reserved' AND b.source = 'membership'
             AND b.user_id = c.user_id AND b.account_id = c.account_id
             AND b.reserved_quota = c.reserved_quota
             AND b.token_reserved = c.token_reserved
             AND b.created_at = c.created_at AND b.review_started_at > 0
             AND ((current_setting('kkai.review_log_mode')::integer = 0 AND NOT EXISTS (
               SELECT 1 FROM logs l WHERE l.request_id = b.request_id))
               OR (current_setting('kkai.review_log_mode')::integer = 1 AND (SELECT count(*) FROM logs l
                 WHERE l.request_id = b.request_id AND l.type = c.log_type
                   AND l.upstream_request_id = c.upstream_request_id) = 1
                 AND (SELECT count(*) FROM logs l WHERE l.request_id = b.request_id) = 1))) THEN
    RAISE EXCEPTION 'billing or evidence changed during compensation';
  END IF;
  IF EXISTS (
    SELECT 1 FROM (
      SELECT account_id, user_id, sum(reserved_quota) quota
      FROM membership_compensation_candidates GROUP BY account_id, user_id
    ) c JOIN kkai_membership_accounts a ON a.id = c.account_id
    JOIN kkai_membership_users s ON s.user_id = c.user_id
    WHERE a.user_id <> c.user_id OR s.current_account_id <> a.id
      OR a.status <> 'active' OR a.amount_reserved < c.quota
      OR a.amount_used > a.amount_total
  ) THEN
    RAISE EXCEPTION 'membership account balance changed';
  END IF;
  IF EXISTS (
    SELECT 1 FROM (
      SELECT token_id, user_id, sum(token_reserved) quota,
        sum(token_reserved) FILTER (WHERE token_quota_debited) debited
      FROM membership_compensation_candidates WHERE token_reserved > 0
      GROUP BY token_id, user_id
    ) c LEFT JOIN tokens t ON t.id = c.token_id AND t.user_id = c.user_id
    WHERE t.id IS NULL OR t.used_quota < c.quota
      OR (coalesce(c.debited, 0) > 0 AND t.remain_quota > 2147483647 - c.debited)
  ) THEN
    RAISE EXCEPTION 'token balance changed or token was removed';
  END IF;
END $$;

WITH totals AS (
  SELECT account_id, sum(reserved_quota) quota
  FROM membership_compensation_candidates GROUP BY account_id
), changed AS (
  UPDATE kkai_membership_accounts a
  SET amount_reserved = a.amount_reserved - x.quota,
    status = CASE WHEN a.status = 'active' AND a.expires_at <= extract(epoch FROM now())
      THEN 'expired' ELSE a.status END
  FROM totals x WHERE a.id = x.account_id RETURNING a.id
)
SELECT count(*) accounts_updated FROM changed;

WITH totals AS (
  SELECT token_id, sum(token_reserved) quota,
    sum(token_reserved) FILTER (WHERE token_quota_debited) debited
  FROM membership_compensation_candidates WHERE token_reserved > 0
  GROUP BY token_id
), changed AS (
  UPDATE tokens t
  SET used_quota = t.used_quota - x.quota,
    remain_quota = t.remain_quota + coalesce(x.debited, 0)
  FROM totals x WHERE t.id = x.token_id RETURNING t.id
)
SELECT count(*) tokens_updated FROM changed;

WITH changed AS (
  UPDATE kkai_membership_billing b
  SET state = 'released', actual_quota = 0, charged_quota = 0, excess_quota = 0,
    settled_at = extract(epoch FROM now())::bigint,
    token_used_refund_unapplied = 0, token_refund_unapplied = 0,
    review_resolved_at = extract(epoch FROM now())::bigint,
    review_resolution = jsonb_build_object(
      'operation_id', :'operation_id', 'actor_id', 0, 'action', 'release',
      'actual_quota', 0,
      'reason', CASE WHEN :review_log_mode = 1
        THEN 'Platform compensation: failed or cancelled request without attributable positive usage after 72 hours'
        ELSE 'Platform compensation: no retained NewAPI request log after 72 hours; upstream cost cannot be attributed' END,
      'operator', 'codex-direct-db',
      'evidence', CASE WHEN :review_log_mode = 1
        THEN 'one NewAPI error or cancellation log; no attributable positive billing receipt'
        ELSE 'no NewAPI request log of any type' END,
      'log_type', c.log_type, 'upstream_request_id', c.upstream_request_id,
      'created_at', c.created_at, 'at', extract(epoch FROM now())::bigint
    )::text
  FROM membership_compensation_candidates c WHERE b.request_id = c.request_id
  RETURNING b.request_id
)
SELECT count(*) billings_released FROM changed;

INSERT INTO audit_logs (
  event_id, user_id, username, actor_role, created_at, category, action,
  auth_method, method, route, status, success, content, other
)
SELECT md5(:'operation_id'), 0, 'codex-direct-db', 0,
  extract(epoch FROM now())::bigint, 'operation', 'membership_bulk_compensation',
  'direct_db', 'SQL', 'membership_reconciliation', 200, true,
  'Released membership reservations with no attributable billing evidence',
  jsonb_build_object('operation_id', :'operation_id',
    'requests', count(*), 'quota', sum(reserved_quota),
    'policy', 'platform absorbs unverifiable upstream cost after 72 hours',
    'evidence', CASE WHEN :review_log_mode = 1
      THEN 'one NewAPI error or cancellation log; no attributable positive billing receipt'
      ELSE 'no retained NewAPI request log of any type' END)::text
FROM membership_compensation_candidates;

DO $$
DECLARE n integer;
BEGIN
  SELECT count(*) INTO n FROM membership_compensation_candidates;
  IF n <> (SELECT count(*) FROM kkai_membership_billing b
           JOIN membership_compensation_candidates c ON c.request_id = b.request_id
           WHERE b.state = 'released' AND b.actual_quota = 0
             AND b.charged_quota = 0 AND b.review_resolved_at > 0
             AND b.review_resolution::jsonb->>'operation_id' = current_setting('kkai.operation_id')) THEN
    RAISE EXCEPTION 'billing compensation verification failed';
  END IF;
  IF EXISTS (
    SELECT 1 FROM kkai_membership_accounts a
    WHERE a.id IN (SELECT account_id FROM membership_compensation_candidates)
      AND a.amount_reserved <> coalesce((
        SELECT sum(b.reserved_quota) FROM kkai_membership_billing b
        WHERE b.account_id = a.id AND b.source = 'membership' AND b.state = 'reserved'
      ), 0)
  ) THEN
    RAISE EXCEPTION 'account reservation ledger mismatch after compensation';
  END IF;
END $$;

SELECT :'operation_id' operation_id, count(*) verified_released,
  round(sum(reserved_quota)::numeric / 500000, 4) refunded_usd
FROM membership_compensation_candidates;

\if :apply
COMMIT;
\echo COMMITTED
\else
ROLLBACK;
\echo DRY_RUN_ROLLED_BACK
\endif

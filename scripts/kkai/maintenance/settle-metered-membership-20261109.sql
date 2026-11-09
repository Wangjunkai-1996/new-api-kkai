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

BEGIN;
SET LOCAL statement_timeout = '90s';
SET LOCAL lock_timeout = '5s';
SET LOCAL kkai.operation_id = :'operation_id';

CREATE TEMP TABLE membership_settle_candidates ON COMMIT DROP AS
WITH pending AS MATERIALIZED (
  SELECT DISTINCT ON (b.request_id)
    b.request_id, b.user_id, b.account_id, b.token_id, b.token_reserved,
    b.token_quota_debited, b.reserved_quota, b.review_channel_id,
    b.snapshot::jsonb AS snap, l.upstream_request_id, l.other::jsonb AS log_other
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
  ORDER BY b.request_id, l.id DESC
), ids AS (
  SELECT DISTINCT upstream_request_id FROM pending
), usage_rows AS MATERIALIZED (
  SELECT * FROM dblink(
    'dbname=sub2api options=-cdefault_transaction_read_only=on',
    'SELECT request_id, count(*), max(model), max(inbound_endpoint), max(upstream_endpoint), ' ||
      'max(input_tokens), max(output_tokens), max(cache_creation_tokens), ' ||
      'max(cache_read_tokens), max(cache_creation_5m_tokens), max(cache_creation_1h_tokens), ' ||
      'max(coalesce(image_count,0)), max(coalesce(video_count,0)), ' ||
      'max(coalesce(image_output_tokens,0)), max(coalesce(image_input_tokens,0)), ' ||
      'sum(actual_cost) FROM usage_logs WHERE request_id IN (' ||
      (SELECT string_agg(quote_literal('client:' || upstream_request_id), ',') FROM ids) ||
      ') GROUP BY request_id'
  ) AS u(request_id text, usage_count bigint, model text, inbound text, upstream text,
    input_tokens integer, output_tokens integer, cache_creation integer,
    cache_read integer, cache_5m integer, cache_1h integer,
    image_count integer, video_count integer, image_output integer, image_input integer,
    actual_cost numeric)
), priced AS (
  SELECT p.*, u.actual_cost AS upstream_cost,
    CASE
      WHEN p.snap->'tiered' IS NULL AND p.snap->>'model_name' LIKE 'claude-%'
        AND u.upstream = '/v1/messages'
        AND p.snap->'price'->>'UsePrice' = 'false'
        THEN trunc((
          u.input_tokens
          + u.cache_read * (p.snap->'price'->>'CacheRatio')::numeric
          + greatest(u.cache_creation-u.cache_5m-u.cache_1h,0)
            * (p.snap->'price'->>'CacheCreationRatio')::numeric
          + u.cache_5m * (p.snap->'price'->>'CacheCreation5mRatio')::numeric
          + u.cache_1h * (p.snap->'price'->>'CacheCreation1hRatio')::numeric
          + u.output_tokens * (p.snap->'price'->>'CompletionRatio')::numeric
        ) * (p.snap->'price'->>'ModelRatio')::numeric
          * (p.snap->'price'->'GroupRatioInfo'->>'GroupRatio')::numeric)
      WHEN p.snap->>'model_name' = 'gpt-6-astra'
        AND p.snap->'tiered'->>'expr_string' =
          'v1:len <= 272000 ? tier("standard", p * 10 + c * 50 + cr * 2 + cc * 12.5) : tier("long_context", p * 20 + c * 100 + cr * 2 + cc * 25)'
        AND u.inbound = '/v1/responses' AND u.upstream = '/v1/responses'
        AND u.input_tokens + u.cache_read <= 272000 AND u.cache_creation = 0
        AND (p.snap->'tiered'->>'quota_per_unit')::numeric = 500000
        THEN round((u.input_tokens*10 + u.output_tokens*50 + u.cache_read*2)
          * (p.snap->'tiered'->>'quota_per_unit')::numeric / 1000000
          * (p.snap->'tiered'->>'group_ratio')::numeric)
      WHEN p.snap->>'model_name' = 'gpt-6.1-sol'
        AND p.snap->'tiered'->>'expr_string' =
          'v1:len <= 272000 ? tier("standard", p * 2.5 + c * 15 + cr * 0.6 + cc * 3.125) : tier("long_context", p * 5 + c * 22.5 + cr * 0.5 + cc * 6.25)'
        AND u.inbound = '/v1/responses' AND u.upstream = '/v1/responses'
        AND u.input_tokens + u.cache_read <= 272000 AND u.cache_creation = 0
        AND (p.snap->'tiered'->>'quota_per_unit')::numeric = 500000
        THEN round((u.input_tokens*2.5 + u.output_tokens*15 + u.cache_read*0.6)
          * (p.snap->'tiered'->>'quota_per_unit')::numeric / 1000000
          * (p.snap->'tiered'->>'group_ratio')::numeric)
    END::bigint AS actual_quota
  FROM pending p
  JOIN usage_rows u ON u.request_id = 'client:' || p.upstream_request_id
  WHERE u.usage_count = 1 AND u.actual_cost > 0
    AND u.model = p.snap->>'model_name'
    AND u.input_tokens >= 0 AND u.output_tokens >= 0
    AND u.cache_creation >= 0 AND u.cache_read >= 0
    AND u.cache_5m >= 0 AND u.cache_1h >= 0
    AND u.cache_5m + u.cache_1h <= u.cache_creation
    AND u.image_count = 0 AND u.video_count = 0
    AND u.image_output = 0 AND u.image_input = 0
    AND p.snap->'other_ratios' IS NULL
    AND p.snap->'tool_call_limits' IS NULL
    AND coalesce(p.log_other->>'error_code','') NOT LIKE 'violation_fee.%'
)
SELECT request_id, user_id, account_id, token_id, token_reserved,
  token_quota_debited, reserved_quota, review_channel_id, upstream_request_id,
  upstream_cost, actual_quota, least(actual_quota,reserved_quota) AS charged_quota,
  reserved_quota - least(actual_quota,reserved_quota) AS refunded_quota
FROM priced WHERE actual_quota IS NOT NULL;

DO $$
DECLARE n integer;
BEGIN
  SELECT count(*) INTO n FROM membership_settle_candidates;
  IF n < 1 OR n > 40 OR n <> (SELECT count(DISTINCT request_id) FROM membership_settle_candidates)
    OR n <> (SELECT count(DISTINCT upstream_request_id) FROM membership_settle_candidates) THEN
    RAISE EXCEPTION 'invalid settlement candidate count: %', n;
  END IF;
  IF EXISTS (SELECT 1 FROM membership_settle_candidates
             WHERE actual_quota < 0 OR actual_quota > reserved_quota
               OR token_reserved < 0 OR reserved_quota < 0
               OR (token_reserved > 0 AND (token_reserved <> reserved_quota OR token_quota_debited))) THEN
    RAISE EXCEPTION 'settlement candidate requires unsupported adjustment';
  END IF;
END $$;

SELECT count(*) requests, count(DISTINCT user_id) users,
  round(sum(reserved_quota)::numeric/500000,4) reserved_usd,
  round(sum(charged_quota)::numeric/500000,4) charged_usd,
  round(sum(refunded_quota)::numeric/500000,4) refunded_usd
FROM membership_settle_candidates;

SELECT count(*) locked_users FROM (
  SELECT id FROM users WHERE id IN (SELECT user_id FROM membership_settle_candidates)
  ORDER BY id FOR UPDATE
) locked;
SELECT count(*) locked_membership_states FROM (
  SELECT user_id FROM kkai_membership_users
  WHERE user_id IN (SELECT user_id FROM membership_settle_candidates)
  ORDER BY user_id FOR UPDATE
) locked;
SELECT count(*) locked_billings FROM (
  SELECT request_id FROM kkai_membership_billing
  WHERE request_id IN (SELECT request_id FROM membership_settle_candidates)
  ORDER BY request_id FOR UPDATE
) locked;
SELECT count(*) locked_accounts FROM (
  SELECT id FROM kkai_membership_accounts
  WHERE id IN (SELECT account_id FROM membership_settle_candidates)
  ORDER BY id FOR UPDATE
) locked;
SELECT count(*) locked_tokens FROM (
  SELECT id FROM tokens WHERE id IN (
    SELECT token_id FROM membership_settle_candidates WHERE token_reserved > 0
  ) ORDER BY id FOR UPDATE
) locked;

DO $$
DECLARE n integer;
BEGIN
  SELECT count(*) INTO n FROM membership_settle_candidates;
  IF n <> (SELECT count(*) FROM kkai_membership_billing b
           JOIN membership_settle_candidates c ON c.request_id = b.request_id
           WHERE b.state = 'reserved' AND b.source = 'membership'
             AND b.user_id = c.user_id AND b.account_id = c.account_id
             AND b.reserved_quota = c.reserved_quota
             AND b.token_reserved = c.token_reserved
             AND b.review_started_at > 0) THEN
    RAISE EXCEPTION 'billing changed during settlement';
  END IF;
  IF EXISTS (
    SELECT 1 FROM (
      SELECT account_id, user_id, sum(reserved_quota) reserved, sum(charged_quota) charged
      FROM membership_settle_candidates GROUP BY account_id,user_id
    ) c JOIN kkai_membership_accounts a ON a.id = c.account_id
    JOIN kkai_membership_users s ON s.user_id = c.user_id
    WHERE a.user_id <> c.user_id OR s.current_account_id <> a.id
      OR a.status <> 'active' OR a.amount_reserved < c.reserved
      OR a.amount_used > a.amount_total - c.charged
  ) THEN
    RAISE EXCEPTION 'membership account balance changed';
  END IF;
  IF EXISTS (
    SELECT 1 FROM (
      SELECT token_id,user_id,sum(refunded_quota) refunded
      FROM membership_settle_candidates WHERE token_reserved > 0
      GROUP BY token_id,user_id
    ) c LEFT JOIN tokens t ON t.id = c.token_id AND t.user_id = c.user_id
    WHERE t.id IS NULL OR t.used_quota < c.refunded
  ) THEN
    RAISE EXCEPTION 'token usage changed or token was removed';
  END IF;
END $$;

WITH totals AS (
  SELECT account_id,sum(reserved_quota) reserved,sum(charged_quota) charged
  FROM membership_settle_candidates GROUP BY account_id
), changed AS (
  UPDATE kkai_membership_accounts a
  SET amount_reserved = a.amount_reserved - x.reserved,
    amount_used = a.amount_used + x.charged,
    status = CASE
      WHEN a.expires_at <= extract(epoch FROM now()) THEN 'expired'
      WHEN a.amount_used + x.charged = a.amount_total THEN 'exhausted'
      ELSE a.status END
  FROM totals x WHERE a.id = x.account_id RETURNING a.id
)
SELECT count(*) accounts_updated FROM changed;

WITH totals AS (
  SELECT user_id,sum(charged_quota) charged,count(*) requests
  FROM membership_settle_candidates GROUP BY user_id
), changed AS (
  UPDATE users u SET used_quota = u.used_quota + x.charged,
    request_count = u.request_count + x.requests
  FROM totals x WHERE u.id = x.user_id RETURNING u.id
)
SELECT count(*) users_updated FROM changed;

WITH totals AS (
  SELECT review_channel_id channel_id,sum(charged_quota) charged
  FROM membership_settle_candidates GROUP BY review_channel_id
), changed AS (
  UPDATE channels c SET used_quota = c.used_quota + x.charged
  FROM totals x WHERE c.id = x.channel_id RETURNING c.id
)
SELECT count(*) channels_updated FROM changed;

WITH totals AS (
  SELECT token_id,sum(refunded_quota) refunded
  FROM membership_settle_candidates WHERE token_reserved > 0 GROUP BY token_id
), changed AS (
  UPDATE tokens t SET used_quota = t.used_quota - x.refunded
  FROM totals x WHERE t.id = x.token_id RETURNING t.id
)
SELECT count(*) tokens_updated FROM changed;

WITH changed AS (
  UPDATE kkai_membership_billing b
  SET state = 'settled', actual_quota = c.actual_quota,
    charged_quota = c.charged_quota, excess_quota = 0,
    settled_at = extract(epoch FROM now())::bigint,
    token_used_refund_unapplied = 0, token_refund_unapplied = 0,
    review_resolved_at = extract(epoch FROM now())::bigint,
    review_resolution = jsonb_build_object(
      'operation_id', :'operation_id', 'actor_id', 0, 'action', 'settle',
      'actual_quota', c.actual_quota,
      'reason', 'Sub2API metered usage priced with frozen NewAPI membership snapshot',
      'operator', 'codex-direct-db', 'upstream_request_id', c.upstream_request_id,
      'upstream_cost', c.upstream_cost,
      'at', extract(epoch FROM now())::bigint
    )::text
  FROM membership_settle_candidates c WHERE b.request_id = c.request_id
  RETURNING b.request_id
)
SELECT count(*) billings_settled FROM changed;

INSERT INTO audit_logs (
  event_id, user_id, username, actor_role, created_at, category, action,
  auth_method, method, route, status, success, content, other
)
SELECT md5(:'operation_id'), 0, 'codex-direct-db', 0,
  extract(epoch FROM now())::bigint, 'operation', 'membership_bulk_settle',
  'direct_db', 'SQL', 'membership_reconciliation', 200, true,
  'Settled verified metered membership reservations',
  jsonb_build_object('operation_id', :'operation_id',
    'requests', count(*), 'reserved_quota', sum(reserved_quota),
    'charged_quota', sum(charged_quota), 'refunded_quota', sum(refunded_quota),
    'evidence', 'Sub2API single positive-cost usage receipt; frozen NewAPI pricing')::text
FROM membership_settle_candidates;

DO $$
DECLARE n integer;
BEGIN
  SELECT count(*) INTO n FROM membership_settle_candidates;
  IF n <> (SELECT count(*) FROM kkai_membership_billing b
           JOIN membership_settle_candidates c ON c.request_id = b.request_id
           WHERE b.state = 'settled' AND b.actual_quota = c.actual_quota
             AND b.charged_quota = c.charged_quota AND b.review_resolved_at > 0
             AND b.review_resolution::jsonb->>'operation_id' = current_setting('kkai.operation_id')) THEN
    RAISE EXCEPTION 'billing settlement verification failed';
  END IF;
  IF EXISTS (
    SELECT 1 FROM kkai_membership_accounts a
    WHERE a.id IN (SELECT account_id FROM membership_settle_candidates)
      AND a.amount_reserved <> coalesce((
        SELECT sum(b.reserved_quota) FROM kkai_membership_billing b
        WHERE b.account_id = a.id AND b.source = 'membership' AND b.state = 'reserved'
      ), 0)
  ) THEN
    RAISE EXCEPTION 'account reservation ledger mismatch after settlement';
  END IF;
END $$;

SELECT :'operation_id' operation_id,count(*) verified_settled,
  round(sum(charged_quota)::numeric/500000,4) charged_usd,
  round(sum(refunded_quota)::numeric/500000,4) refunded_usd
FROM membership_settle_candidates;

\if :apply
COMMIT;
\echo COMMITTED
\else
ROLLBACK;
\echo DRY_RUN_ROLLED_BACK
\endif

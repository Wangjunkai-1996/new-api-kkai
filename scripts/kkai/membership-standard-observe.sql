-- Run with psql -X -v ON_ERROR_STOP=1 -d newapi -f membership-standard-observe.sql.
-- Window is [from_utc, until_utc). In-flight amounts include older open rows.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '20s';
SET LOCAL lock_timeout = '2s';
SET LOCAL TIME ZONE 'UTC';

WITH bounds AS (
    SELECT floor(extract(epoch FROM transaction_timestamp()))::bigint AS until_ts
), period AS (
    SELECT until_ts - 86400 AS from_ts, until_ts FROM bounds
), recent AS (
    SELECT b.*, p.from_ts, p.until_ts
    FROM kkai_membership_billing b CROSS JOIN period p
    WHERE b.source = 'membership' AND b.billing_scope = 'relay'
      AND (b.created_at >= p.from_ts OR b.settled_at >= p.from_ts
           OR b.review_resolved_at >= p.from_ts OR b.state = 'reserved')
), marked AS (
    SELECT b.*,
           CASE WHEN strpos(snapshot, '"standard_full_reserved_quota"') > 0 THEN 'standard'
                ELSE 'pilot' END AS policy,
           snapshot::jsonb AS pricing
    FROM recent b
    WHERE strpos(snapshot, '"full_reserved_quota"') > 0
       OR strpos(snapshot, '"standard_full_reserved_quota"') > 0
), holds AS (
    SELECT m.*,
           (pricing ->> CASE policy WHEN 'standard' THEN 'standard_full_reserved_quota'
                                      ELSE 'full_reserved_quota' END)::numeric AS full_quota,
           nullif((pricing ->> 'quota_per_unit')::numeric, 0) AS units_per_usd
    FROM marked m
), reductions AS (
    SELECT h.*, ceil((full_quota - reserved_quota) * 1000000 / units_per_usd) / 1000000 AS reduction_usd,
           least(actual_quota, full_quota) - charged_quota AS known_loss_quota
    FROM holds h WHERE full_quota > reserved_quota AND units_per_usd > 0
), early AS (
    SELECT CASE b.review_reason WHEN 'standard_early_no_evidence_compensation' THEN 'standard'
                                ELSE 'pilot' END AS policy,
           ceil(b.reserved_quota::numeric * 1000000 / nullif((b.snapshot::jsonb ->> 'quota_per_unit')::numeric, 0)) / 1000000 AS face_usd
    FROM recent b
    WHERE b.state = 'released'
      AND b.review_reason IN ('standard_early_no_evidence_compensation', 'pilot_early_no_evidence_compensation')
      AND b.review_resolved_at >= b.from_ts AND b.review_resolved_at < b.until_ts
), late_evidence AS (
    SELECT CASE b.review_reason WHEN 'standard_early_no_evidence_compensation' THEN 'standard'
                                ELSE 'pilot' END AS policy
    FROM kkai_membership_billing_recovery r
    JOIN kkai_membership_billing b USING (request_id)
    CROSS JOIN period p
    WHERE r.last_error_code = 'late_terminal_after_completion' AND r.evidence_kind = 'final_usage'
      AND b.billing_scope = 'relay' AND b.source = 'membership' AND b.state = 'released'
      AND b.review_reason IN ('standard_early_no_evidence_compensation', 'pilot_early_no_evidence_compensation')
      AND (r.evidence_payload::jsonb ->> 'assessed_quota')::numeric > 0
      AND (r.evidence_payload::jsonb ->> 'observed_at')::bigint >= p.from_ts
      AND (r.evidence_payload::jsonb ->> 'observed_at')::bigint < p.until_ts
), hold_totals AS (
    SELECT policy,
           count(*) FILTER (WHERE created_at >= from_ts AND created_at < until_ts) AS reduced_24h,
           count(*) FILTER (WHERE state = 'reserved') AS open_reduced,
           sum(reduction_usd) FILTER (WHERE state = 'reserved') AS open_reduction_usd,
           count(*) FILTER (WHERE state = 'settled' AND settled_at >= from_ts AND settled_at < until_ts
                             AND known_loss_quota > 0) AS known_shortfall_24h,
           sum(known_loss_quota / units_per_usd) FILTER
               (WHERE state = 'settled' AND settled_at >= from_ts AND settled_at < until_ts
                AND known_loss_quota > 0) AS known_shortfall_usd_24h,
           count(*) FILTER (WHERE state = 'released' AND review_resolved_at >= from_ts
                             AND review_resolved_at < until_ts AND review_reason <> 'trusted_terminal_evidence')
               AS waived_reduced_24h,
           sum(reduction_usd) FILTER (WHERE state = 'released' AND review_resolved_at >= from_ts
                                       AND review_resolved_at < until_ts AND review_reason <> 'trusted_terminal_evidence')
               AS waived_reduction_limit_usd_24h
    FROM reductions GROUP BY policy
), early_totals AS (
    SELECT policy, count(*) AS releases, sum(face_usd) AS face_usd FROM early GROUP BY policy
), late_totals AS (
    SELECT policy, count(*) AS verified_positive FROM late_evidence GROUP BY policy
)
SELECT to_timestamp(p.from_ts) AS from_utc, to_timestamp(p.until_ts) AS until_utc, policies.policy,
       (SELECT count(*) FROM recent WHERE created_at >= from_ts AND created_at < until_ts) AS membership_relay_created_24h,
       coalesce(h.reduced_24h, 0) AS reduced_24h,
       coalesce(h.open_reduced, 0) AS open_reduced,
       round(coalesce(h.open_reduction_usd, 0), 6) AS open_reduction_usd,
       coalesce(h.known_shortfall_24h, 0) AS known_shortfall_24h,
       round(coalesce(h.known_shortfall_usd_24h, 0), 6) AS known_shortfall_usd_24h,
       coalesce(h.waived_reduced_24h, 0) AS waived_reduced_24h,
       round(coalesce(h.waived_reduction_limit_usd_24h, 0), 6) AS waived_reduction_limit_usd_24h,
       coalesce(e.releases, 0) AS early_releases_24h,
       round(coalesce(e.face_usd, 0), 6) AS early_release_face_usd_24h,
       coalesce(l.verified_positive, 0) AS early_releases_with_late_positive_evidence_24h
FROM (VALUES ('standard'), ('pilot')) AS policies(policy)
CROSS JOIN period p
LEFT JOIN hold_totals h USING (policy)
LEFT JOIN early_totals e USING (policy)
LEFT JOIN late_totals l USING (policy)
ORDER BY policies.policy;

-- The relay count is the same denominator on both rows. Only a captured full
-- authorization proves that a reduction happened. The waived amount is the
-- foregone hold on a reviewed release with unconfirmed cost. Early-release
-- face is the amount actually returned. Both may refer to the same request.
-- The late-evidence count is based on a retained positive terminal and its
-- observation time. No reason for an ordinary full hold is persisted.
ROLLBACK;

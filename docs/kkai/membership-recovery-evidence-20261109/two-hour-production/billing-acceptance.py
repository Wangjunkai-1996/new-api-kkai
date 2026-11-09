"""Post-v21 read-only acceptance aggregates; no row identifiers leave the host.

Use a fixed --since across phases. After natural recovery completions, choose
--terminal-cutoff from a prior snapshot_epoch, then reuse that cutoff to compare
the financial sample. The checksum is an observation, not an exactly-once proof.
"""
import argparse
import json
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("phase")
parser.add_argument("--since", required=True, type=int)
parser.add_argument("--terminal-cutoff", required=True, type=int)
args = parser.parse_args()
if not 0 < args.since <= args.terminal_cutoff:
    parser.error("require 0 < since <= terminal-cutoff")

sql = r"""
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL ROLE newapi_standby;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '100ms';
WITH
membership_totals AS (
  SELECT account_id,
    coalesce(sum(reserved_quota) FILTER (WHERE state = 'reserved'), 0) AS reserved,
    coalesce(sum(charged_quota) FILTER (WHERE state = 'settled'), 0) AS used
  FROM kkai_membership_billing WHERE source = 'membership' GROUP BY account_id
),
window_terminals AS (
  SELECT b.*, r.state AS recovery_state, r.evidence_kind
  FROM kkai_membership_billing b
  LEFT JOIN kkai_membership_billing_recovery r USING (request_id)
  WHERE b.billing_scope = 'relay' AND b.state IN ('settled', 'released')
    AND b.review_reason IN ('trusted_terminal_evidence', 'no_evidence_compensation')
    AND b.review_resolved_at >= :since
),
fixed_terminals AS (
  SELECT * FROM window_terminals WHERE settled_at < :terminal_cutoff
),
terminal_sample AS (
  SELECT * FROM fixed_terminals ORDER BY settled_at DESC, request_id DESC LIMIT 1000
)
SELECT json_build_object(
  'snapshot_epoch', floor(extract(epoch FROM transaction_timestamp()))::bigint,
  'since', :since,
  'terminal_cutoff', :terminal_cutoff,
  'accounts_all_scopes', (
    SELECT json_build_object(
      'missing_accounts', count(*) FILTER (WHERE a.id IS NULL),
      'reserved_mismatches', count(*) FILTER (WHERE a.id IS NULL OR a.amount_reserved <> coalesce(t.reserved, 0)),
      'used_mismatches', count(*) FILTER (WHERE a.id IS NULL OR a.amount_used <> coalesce(t.used, 0)),
      'invalid_balances', count(*) FILTER (WHERE a.amount_total < 0 OR a.amount_used < 0 OR a.amount_reserved < 0
        OR a.amount_used::numeric + a.amount_reserved::numeric > a.amount_total),
      'account_reserved', coalesce(sum(a.amount_reserved), 0),
      'ledger_reserved', coalesce(sum(t.reserved), 0),
      'account_used', coalesce(sum(a.amount_used), 0),
      'ledger_used', coalesce(sum(t.used), 0))
    FROM kkai_membership_accounts a FULL JOIN membership_totals t ON t.account_id = a.id
  ),
  'ledger_all_scopes', (
    SELECT json_build_object(
      'negative_money', count(*) FILTER (WHERE reserved_quota < 0 OR actual_quota < 0 OR charged_quota < 0 OR excess_quota < 0),
      'unknown_state', count(*) FILTER (WHERE state NOT IN ('reserved', 'settled', 'released')),
      'unknown_source', count(*) FILTER (WHERE source NOT IN ('membership', 'wallet')),
      'settled_math_errors', count(*) FILTER (WHERE state = 'settled'
        AND (charged_quota <> least(actual_quota, reserved_quota) OR excess_quota <> actual_quota - charged_quota)),
      'released_money_errors', count(*) FILTER (WHERE state = 'released'
        AND (actual_quota <> 0 OR charged_quota <> 0 OR excess_quota <> 0)),
      'token_refund_exceptions', count(*) FILTER (WHERE token_refund_unapplied <> 0 OR token_used_refund_unapplied <> 0),
      'token_refund_unapplied', coalesce(sum(token_refund_unapplied), 0),
      'token_used_refund_unapplied', coalesce(sum(token_used_refund_unapplied), 0))
    FROM kkai_membership_billing
  ),
  'membership_owner_mismatches', (
    SELECT count(*) FROM kkai_membership_billing b
    LEFT JOIN kkai_membership_accounts a ON a.id = b.account_id
    WHERE b.source = 'membership' AND (a.id IS NULL OR b.user_id <> a.user_id)
  ),
  'relay_reserved', (
    SELECT coalesce(json_agg(x), '[]'::json) FROM (
      SELECT source, count(*) AS records, coalesce(sum(reserved_quota), 0) AS reserved_quota,
        min(created_at) AS oldest_created_at,
        count(*) FILTER (WHERE created_at <= extract(epoch FROM now()) - 7200) AS aged_2h,
        coalesce(sum(reserved_quota) FILTER (WHERE created_at <= extract(epoch FROM now()) - 7200), 0) AS aged_2h_quota,
        count(*) FILTER (WHERE review_started_at > 0) AS needs_review
      FROM kkai_membership_billing WHERE billing_scope = 'relay' AND state = 'reserved'
      GROUP BY source ORDER BY source
    ) x
  ),
  'recovery_states', (
    SELECT coalesce(json_agg(x), '[]'::json) FROM (
      SELECT r.state, r.evidence_kind, b.state AS ledger_state, count(*) AS records,
        count(*) FILTER (WHERE r.next_attempt_at <= extract(epoch FROM now())) AS due,
        min(r.updated_at) AS oldest_updated_at,
        coalesce(sum(b.reserved_quota) FILTER (WHERE b.state = 'reserved'), 0) AS reserved_quota
      FROM kkai_membership_billing_recovery r
      LEFT JOIN kkai_membership_billing b USING (request_id)
      GROUP BY r.state, r.evidence_kind, b.state ORDER BY r.state, r.evidence_kind, b.state
    ) x
  ),
  'recovery_invariants', (
    SELECT json_build_object(
      'missing_ledgers', count(*) FILTER (WHERE b.request_id IS NULL),
      'wrong_scope', count(*) FILTER (WHERE b.billing_scope <> 'relay'),
      'terminal_sidecar_without_terminal_ledger', count(*) FILTER (
        WHERE r.state IN ('cache_pending', 'resolved') AND b.state NOT IN ('settled', 'released')),
      'resolved_without_time', count(*) FILTER (WHERE r.state = 'resolved' AND r.resolved_at <= 0),
      'resolved_in_window', count(*) FILTER (WHERE r.state = 'resolved' AND r.resolved_at >= :since),
      'late_evidence_markers', count(*) FILTER (WHERE r.last_error_code = 'late_terminal_after_completion'))
    FROM kkai_membership_billing_recovery r LEFT JOIN kkai_membership_billing b USING (request_id)
  ),
  'natural_recovery_terminals', (
    SELECT coalesce(json_agg(x), '[]'::json) FROM (
      SELECT source, state, review_reason, evidence_kind, recovery_state,
        count(*) AS records,
        coalesce(sum(reserved_quota), 0) AS reservation_closed,
        coalesce(sum(actual_quota), 0) AS assessed_quota,
        coalesce(sum(charged_quota), 0) AS charged_quota,
        coalesce(sum(reserved_quota - charged_quota), 0) AS refund_quota,
        coalesce(sum(excess_quota), 0) AS platform_excess_quota,
        count(*) FILTER (WHERE review_reason = 'no_evidence_compensation'
          AND (state <> 'released' OR settled_at - created_at < 7200)) AS compensation_policy_errors,
        coalesce(sum(token_refund_unapplied), 0) AS token_refund_unapplied,
        coalesce(sum(token_used_refund_unapplied), 0) AS token_used_refund_unapplied
      FROM window_terminals
      GROUP BY source, state, review_reason, evidence_kind, recovery_state
      ORDER BY source, state, review_reason, evidence_kind, recovery_state
    ) x
  ),
  'fixed_terminal_financial_sample', (
    SELECT json_build_object(
      'eligible_records', (SELECT count(*) FROM fixed_terminals),
      'sample_limit', 1000, 'sample_records', count(*),
      'reservation_closed', coalesce(sum(reserved_quota), 0),
      'charged_quota', coalesce(sum(charged_quota), 0),
      'refund_quota', coalesce(sum(reserved_quota - charged_quota), 0),
      'cache_states', (SELECT coalesce(json_agg(s), '[]'::json) FROM (
        SELECT recovery_state, count(*) AS records FROM terminal_sample
        GROUP BY recovery_state ORDER BY recovery_state
      ) s),
      'financial_fingerprint', md5(coalesce(string_agg(md5(row(
        request_id, user_id, token_id, account_id, account_revision, source, state,
        reserved_quota, actual_quota, charged_quota, excess_quota, token_reserved,
        token_quota_debited, token_refund_unapplied, token_used_refund_unapplied, settled_at
      )::text), '' ORDER BY settled_at DESC, request_id DESC), '')))
    FROM terminal_sample
  ),
  'scan_progress', (
    SELECT coalesce(json_agg(x), '[]'::json) FROM (
      SELECT lane, updated_at FROM kkai_membership_billing_scan_progress ORDER BY lane
    ) x
  )
);
ROLLBACK;
"""
result = subprocess.run(
    ["docker", "exec", "-i", "--user", "999:999", "kkai-postgres", "psql",
     "--username=postgres", "--dbname=newapi", "--no-psqlrc", "--tuples-only",
     "--no-align", "--quiet", "--set=ON_ERROR_STOP=1", "--file=-",
     f"--set=since={args.since}", f"--set=terminal_cutoff={args.terminal_cutoff}"],
    input=sql, text=True, capture_output=True, timeout=15,
)
if result.returncode:
    raise SystemExit("read-only billing acceptance failed; no database error text emitted")
row = json.loads(result.stdout.strip())
if not isinstance(row, dict):
    raise SystemExit("read-only billing acceptance returned an unexpected result shape")
row["phase"] = args.phase
print(json.dumps(row, sort_keys=True))

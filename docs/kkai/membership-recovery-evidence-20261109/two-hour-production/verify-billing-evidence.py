"""Validate aggregate accounting invariants and the same completed financial sample."""
import json
from pathlib import Path

root = Path(__file__).resolve().parent
snapshots = []
for path in sorted(root.glob('billing-*.log')):
    row = json.loads(path.read_text().splitlines()[0])
    for key in ('missing_accounts', 'reserved_mismatches', 'used_mismatches', 'invalid_balances'):
        assert row['accounts_all_scopes'][key] == 0, (path.name, key)
    assert all(value == 0 for value in row['ledger_all_scopes'].values()), path.name
    assert row['membership_owner_mismatches'] == 0, path.name
    for key in ('missing_ledgers', 'wrong_scope', 'terminal_sidecar_without_terminal_ledger', 'resolved_without_time'):
        assert row['recovery_invariants'][key] == 0, (path.name, key)
    for terminal in row['natural_recovery_terminals']:
        for key in ('compensation_policy_errors', 'token_refund_unapplied', 'token_used_refund_unapplied'):
            assert terminal[key] == 0, (path.name, key)
    snapshots.append({'file': path.name, 'snapshot_epoch': row['snapshot_epoch'], 'invariants_passed': True})
fixed = []
for label in ('a', 'b'):
    path = root / f'billing-fixed-{label}.log'
    if path.exists():
        row = json.loads(path.read_text().splitlines()[0])
        fixed.append(row['fixed_terminal_financial_sample'])
if len(fixed) == 2:
    assert fixed[0]['sample_records'] > 0
    assert fixed[0] == fixed[1], 'completed financial sample changed'
result = {'snapshots': snapshots, 'fixed_sample_count': fixed[0]['sample_records'] if fixed else 0,
          'fixed_sample_unchanged': len(fixed) == 2, 'proof_limit': 'observed invariants and bounded sample, not formal exactly-once proof'}
(root / 'billing-verification.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps(result))

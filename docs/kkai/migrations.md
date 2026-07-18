# KKAI Schema Migrations

KKAI-owned database objects are managed only by `cmd/kkai-migrate`. NewAPI
startup verifies the required version but never creates or changes these
tables.

## Current Versions

| Version | Name | Objects |
| --- | --- | --- |
| 1 | `risk_incidents_and_outbox` | `kkai_policy_incidents`, `kkai_outbox` |
| 2 | `internal_balance_ledger` | `kkai_internal_balance_adjustments` |
| 3 | `background_job_leases` | `kkai_job_leases` |

Applied versions are recorded in `kkai_schema_migrations` with an immutable
SHA-256 checksum. A checksum mismatch or unknown future version stops both the
migrator and application startup.

## Commands

Build the migration binary on the external build machine:

```bash
go build -trimpath -o kkai-migrate ./cmd/kkai-migrate
go build -trimpath -o kkai-schema-observe ./cmd/kkai-schema-observe
```

Use `KKAI_MIGRATION_DSN`, `SQL_DSN`, or `--dsn-stdin`. Release automation uses
stdin so the DSN never appears in a process argument, container environment,
or release manifest. The command never prints the DSN.

```bash
./kkai-migrate --dry-run
./kkai-migrate
./kkai-migrate --check --min-version 3
```

The production image contains both tools built from the same source revision as
`/new-api`. `/kkai-migrate` owns describe, check, dry-run, bootstrap and apply
operations. Routine release observation uses `/kkai-schema-observe`, which only
accepts `--current` or `--check-upstream-baseline` with canonical JSON output
and has no migration or bootstrap command surface:

```bash
./kkai-schema-observe --current --json --dsn-stdin
./kkai-schema-observe --check-upstream-baseline --json \
  --source-revision "$SOURCE_REVISION" --dsn-stdin
```

Application startup verifies the KKAI schema version and never applies KKAI
migrations implicitly.

`--dry-run` is schema-read-only. If the migration metadata table does not
exist, dry-run still makes no database changes.

## Legacy Import

The first execution detects the old fork tables when present:

- `policy_incident_events` rows are copied as historical audit records. Token
  names and raw content are omitted, and historical actions are not replayed.
- `internal_balance_adjustments` rows are copied by `operation_id`. Quota
  changes are not replayed.

Legacy tables remain untouched for rollback compatibility. Removing them is a
separate post-stability operation and is not part of the candidate rollout.

## Rollout Rules

1. Back up PostgreSQL and record the pre-migration schema hash.
2. Run dry-run against the isolated production clone.
3. Apply migrations to the clone and compare schema plus row counts.
4. Reject any generated diff that drops a table/column, changes a column type,
   or rewrites an upstream-owned table.
5. Apply the same migration binary and checksums to production before starting
   the candidate application.
6. Run `--check` from the release workflow and record its output in the release
   manifest.

All migrations must be additive and idempotent. MySQL DDL is intentionally
executed outside the legacy-data transaction because MySQL implicitly commits
DDL; every DDL and index operation is safe to retry. PostgreSQL and SQLite use
transactional DDL.

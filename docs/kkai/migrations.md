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
| 4 | `outbox_event_key_mysql57_compat` | MySQL only: shrink `kkai_outbox.event_key` to 191 bytes |

The production database is PostgreSQL. Its required runtime version is 3 and
the release contract is `migration_kind=none`; PostgreSQL must not execute
version 4. SQLite and MySQL follow the same normal runtime contract. The generic
application migrator keeps version 4 only as explicit MySQL 5.7 compatibility
maintenance; ordinary `Apply` never selects it. A PostgreSQL ledger that already
contains the known version-4 record is accepted as a compatible legacy state
and is never downgraded.

Applied versions are recorded in `kkai_schema_migrations` with an immutable
SHA-256 checksum. A checksum mismatch or unknown future version stops both the
migrator and application startup.

## Commands

Build the migration binary on the external build machine:

```bash
go build -trimpath -o kkai-migrate ./cmd/kkai-migrate
```

Use `KKAI_MIGRATION_DSN`, `SQL_DSN`, or `--dsn-stdin`. Release automation uses
stdin so the DSN never appears in a process argument, container environment,
or release manifest. The command never prints the DSN.

```bash
./kkai-migrate --dry-run
./kkai-migrate
./kkai-migrate --check
./kkai-migrate --observe --current --json --dsn-stdin
./kkai-migrate --describe-contract --dialect postgres --json
```

The production image contains `/kkai-migrate` built from the same source
revision as `/new-api`. Release automation runs it as a read-only,
capability-free, one-shot container on the private data network. Application
startup verifies the KKAI schema version and never applies KKAI migrations
implicitly. Formal images are compiled with immutable
`schema_management=external`, which disables upstream GORM `AutoMigrate`
regardless of runtime role or environment drift. Production schema changes
belong in a reviewed one-shot migration path, not application startup.

The six `com.kkai.schema.*` OCI labels and the immutable schema-management label are generated from
`--describe-contract`, not hand-maintained in infrastructure. The read-only
`--observe --current --json` command returns the exact validated database
prefix and dialect-specific migration-set digest.

`compatible_prefixes` is serialized with Go's canonical JSON map-key ordering
before it becomes an OCI label. Consumers parse it as a JSON object and compare
the prefix-to-digest mapping semantically; they do not depend on raw key order.

`--observe` validates the migration ledger and physical shape of the versioned
KKAI schema. It also validates the unversioned main application tables and
columns from the exact model registry used by `migrateDB`, rather than
maintaining a second model list. On PostgreSQL this includes the canonical
`kkai_outbox.event_key` shape for the observed ledger version,
`tokens.model_limits` as `TEXT`, and `subscription_plans.price_amount` as
`NUMERIC(10,6)`. All checks use read-only schema metadata; observation never
runs GORM `AutoMigrate` or changes database state. Candidate health, pricing,
and protocol preflight continue to cover runtime semantics beyond schema shape.

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

For the current PostgreSQL contract, release automation runs observation and
minimum-version checks only. It must not execute migration version 4 or add a
transitional schema release.

If a future PostgreSQL contract explicitly declares an `expand` migration:

1. Back up PostgreSQL and record the pre-migration schema hash.
2. Run dry-run against the isolated production clone.
3. Apply migrations to the clone and compare schema plus row counts.
4. Reject any generated diff that drops a table/column, changes a column type,
   or rewrites an upstream-owned table without an independently reviewed plan.
5. Apply the same migration binary and checksums through the infrastructure
   transaction before starting the candidate application.
6. Run `--check` and `--observe` from the release workflow and record their
   outputs in the release manifest.

Normal runtime migrations must be additive and idempotent. The dormant MySQL v4
column shrink is an explicit compatibility maintenance exception and requires
its own reviewed rehearsal. MySQL DDL is executed outside the legacy-data
transaction because MySQL implicitly commits DDL; every DDL and index operation
must remain safe to retry. PostgreSQL and SQLite use transactional DDL.

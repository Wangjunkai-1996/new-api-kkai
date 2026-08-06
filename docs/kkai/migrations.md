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
| 4 | `outbox_event_key_mysql57_compat` | Cross-dialect bridge: normalize `kkai_outbox.event_key` to 191 characters |
| 5 | `video_studio` | Six additive Video Studio tables |
| 6 | `video_sample_category` | Nullable `kkai_video_samples.category` column |
| 7 | `image_studio` | Four additive Image Studio tables |
| 8 | `stateless_authentication` | Authentication columns, session/flow/identity tables, and legacy identity backfill |

Version 4 is an explicit bridge on every supported dialect. MySQL 5.7 and
PostgreSQL alter `kkai_outbox.event_key` to `VARCHAR(191)`; SQLite records the
same immutable migration as a physical no-op. Keeping one v4 ledger prefix
across SQLite, MySQL, and PostgreSQL makes the v5 rollout and rollback contract
unambiguous. The current `kkai_bridge` profile is the v7-to-v8 transition
contract `(7,8,7)`; the untagged feature profile remains exact v8 `(8,8,8)`.
Neither application profile changes the schema during startup.

Version 5 is an additive expand migration. It creates exactly these tables and
does not modify or replace `tasks`:

- `kkai_video_model_profiles`
- `kkai_video_samples`
- `kkai_video_generations`
- `kkai_video_assets`
- `kkai_video_task_assets`
- `kkai_idempotency_keys`

Version 6 is a separate additive expand migration. It adds only the nullable
`category VARCHAR(32)` column to `kkai_video_samples`; it does not modify the
v5 migration or its checksum. New samples store one fixed category. Historical
`NULL` or empty values are interpreted as `other` by the application.

Version 7 is a separate additive expand migration. It creates exactly these
Image Studio-owned tables and does not modify Video Studio or upstream-owned
tables:

- `kkai_image_model_profiles`
- `kkai_image_samples`
- `kkai_image_generations`
- `kkai_image_assets`

Image Studio reuses the v1 outbox and the v5 idempotency table; it does not
create parallel copies of either shared primitive.

Version 8 is a separate additive expand migration. It adds nullable
`users.auth_version` and `tokens.auto_groups` columns, creates
`user_sessions`, `auth_flows`, and `external_identity_claims`, initializes
every user authentication version to at least 1, and imports non-empty legacy
Telegram bindings into the single-owner identity table. Ambiguous subject or
user ownership aborts the migration instead of preserving an unsafe login
mapping.

Applied versions are recorded in `kkai_schema_migrations` with an immutable
SHA-256 checksum. A checksum mismatch or unknown future version stops both the
migrator and application startup.

## Commands

Build the migration binary on the external build machine:

```bash
go build -trimpath -o kkai-migrate ./cmd/kkai-migrate
go build -trimpath -tags kkai_bridge -o kkai-migrate-bridge ./cmd/kkai-migrate
```

The untagged binary is the final v8 feature profile. The `kkai_bridge` tag
builds the reviewed v7-to-v8 transition profile with runtime range v7 through
v8 and migration target v7.

Use `KKAI_MIGRATION_DSN`, `SQL_DSN`, or `--dsn-stdin`. Prefer stdin for an
operator-run migration so the DSN does not appear in a process argument. The
command never prints the DSN.

```bash
./kkai-migrate --dry-run
./kkai-migrate
./kkai-migrate --check
./kkai-migrate --check --min-version 8
./kkai-migrate --observe --current --json --dsn-stdin
./kkai-migrate --describe-contract --dialect postgres --json
```

The production image contains `/kkai-migrate` built from the same source
revision as `/new-api`. Ordinary application delivery does not run it.
Application startup verifies the KKAI schema version and never applies KKAI
migrations implicitly. Formal images compile `common.SchemaManagementMode` as
`external`, which disables upstream GORM `AutoMigrate` regardless of runtime
role or environment drift. Database maintenance remains separate from ordinary
application delivery.

The read-only `--observe --current --json` command returns the exact validated
database prefix and dialect-specific migration-set digest.

`--observe` validates the migration ledger and physical shape of the versioned
KKAI schema. It also validates the unversioned main application tables and
columns from the exact model registry used by `migrateDB`, rather than
maintaining a second model list. On PostgreSQL this includes the canonical
`kkai_outbox.event_key` shape for the observed ledger version,
`tokens.model_limits` as `TEXT`, and `subscription_plans.price_amount` as
`NUMERIC(10,6)`. All checks use read-only schema metadata; observation never
runs GORM `AutoMigrate` or changes database state.

`--dry-run` is schema-read-only. If the migration metadata table does not
exist, dry-run still makes no database changes.

## Historical Studio Bridge And Expands

The procedure below documents the completed v3-to-v7 rollout and applies only
to an audited pre-rc23 v7-compatible bridge binary. The current bridge profile
is only for the exact v7-to-v8 transition and reports
`runtime_min_version=7`, `runtime_max_version=8`, and
`migration_target_version=7`.

The bridge release contract is `runtime_min_version=3`,
`runtime_max_version=7`, and `migration_target_version=3`. Verify it for the
actual database dialect before rollout:

```bash
./kkai-migrate --describe-contract --dialect sqlite --json
./kkai-migrate --describe-contract --dialect mysql --json
./kkai-migrate --describe-contract --dialect postgres --json
```

Build production bridge images by opting in explicitly. The selected profile
is written to the local release metadata and the image's
`io.kkrich.schema-contract` label. The staging client validates the metadata
profile and passes it to the production controller for image verification:

```bash
scripts/kkai/build-manual-release.sh --schema-contract bridge
```

Ship the bridge through both the current and rollback slots before changing the
database. Both slots must advertise `runtime_max_version=7`. The ordinary
migration command without `--target` stops at v3; v4, v5, v6, and v7 are separate,
operator-invoked maintenance gates.

Run v4 independently, using the same reviewed binary that produced the bridge
contract:

```bash
./kkai-migrate --target 4 --dry-run --dsn-stdin
./kkai-migrate --target 4 --dsn-stdin
./kkai-migrate --check --min-version 4 --dsn-stdin
./kkai-migrate --observe --current --json --dsn-stdin
```

Confirm that observation reports `current_version: 4` and the exact v4
compatible-prefix digest from `--describe-contract`. Then run the v5 expand as
a second gate. The migrator rejects `--target 5` until the validated v4 prefix
already exists, so the bridge observation cannot be skipped:

```bash
./kkai-migrate --target 5 --dry-run --dsn-stdin
./kkai-migrate --target 5 --dsn-stdin
./kkai-migrate --check --min-version 5 --dsn-stdin
./kkai-migrate --observe --current --json --dsn-stdin
```

After v5 is observed and validated, run the v6 category expand as a third
independent gate. The migrator rejects `--target 6` until the complete v5
prefix and physical Video Studio schema have passed validation:

```bash
./kkai-migrate --target 6 --dry-run --dsn-stdin
./kkai-migrate --target 6 --dsn-stdin
./kkai-migrate --check --min-version 6 --dsn-stdin
./kkai-migrate --observe --current --json --dsn-stdin
```

On the bridge binary, the default `--check` validates v3. It does not prove
that the category schema exists; use `--min-version 6` for that gate.

After v6 is observed and validated, run the v7 Image Studio expand as a fourth
independent gate. The migrator rejects `--target 7` until the complete v6
prefix and physical Video Studio schema have passed validation:

```bash
./kkai-migrate --target 7 --dry-run --dsn-stdin
./kkai-migrate --target 7 --dsn-stdin
./kkai-migrate --check --min-version 7 --dsn-stdin
./kkai-migrate --observe --current --json --dsn-stdin
```

Confirm that observation reports `current_version: 7` and the exact v7
compatible-prefix digest from `--describe-contract`. `--observe` additionally
validates all four physical Image Studio tables, their columns, and the
immutable migration prefix. Keep the bridge binary for the explicit v4, v5,
v6, and v7 operator gates; do not replace those gates with an unqualified
feature-profile migration command.

Only after both current and rollback slots are v7-compatible, v4 through v7 pass
`--check`/`--observe`, and the candidate has been validated may a feature
release be built and staged:

```bash
scripts/kkai/build-manual-release.sh --schema-contract feature
```

That pre-rc23 feature contract was `runtime_min_version=7`,
`runtime_max_version=7`, and `migration_target_version=7`. There is no
automatic down migration: never delete the v5, v6, or v7 ledger rows, drop
their objects, or rewrite their checksums to make an older image start.

## Authentication V8 Gate

Do not apply v8 until both production slots and the rollback image have been
replaced with two unique builds of the reviewed `kkai_bridge` v7-to-v8
transition profile. After the transition slots are verified, run v8 as its own
operator gate:

```bash
./kkai-migrate --target 8 --dry-run --dsn-stdin
./kkai-migrate --target 8 --dsn-stdin
./kkai-migrate --check --min-version 8 --dsn-stdin
./kkai-migrate --observe --current --json --dsn-stdin
```

Confirm `current_version: 8`, the reviewed v8 prefix digest, the three new
authentication tables and their unique indexes, `users.auth_version >= 1`, and
the legacy Telegram ownership backfill. Only then may this rc.23 feature image
be staged. Until that gate passes, deploy this source only with the bridge
profile.

## Legacy Import

The first execution detects the old fork tables when present:

- `policy_incident_events` rows are copied as historical audit records. Token
  names and raw content are omitted, and historical actions are not replayed.
- `internal_balance_adjustments` rows are copied by `operation_id`. Quota
  changes are not replayed.

Legacy tables remain untouched for rollback compatibility. Removing them is a
separate post-stability operation and is not part of ordinary delivery.

## Operator Migration Rules

Ordinary release automation does not run schema observation or migrations. It
does not execute migration version 4, 5, 6, 7, or 8.

If a future PostgreSQL migration is needed:

1. Back up PostgreSQL and record the pre-migration schema hash.
2. Run dry-run against the isolated production clone.
3. Apply migrations to the clone and compare schema plus row counts.
4. Reject any generated diff that drops a table/column, changes a column type,
   or rewrites an upstream-owned table without an independently reviewed plan.
5. Apply the migration separately from ordinary application delivery.
6. Run `--check` and `--observe` before releasing the application.

Normal runtime migrations must be additive and idempotent. The v4 column
normalization is an explicit compatibility maintenance operation and requires
its own reviewed rehearsal on SQLite, MySQL, and PostgreSQL. MySQL DDL is
executed outside the legacy-data transaction because MySQL implicitly commits
DDL; every DDL and index operation must remain safe to retry. PostgreSQL and
SQLite use transactional DDL.

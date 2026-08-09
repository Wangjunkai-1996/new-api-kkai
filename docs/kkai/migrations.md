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
| 8 | `stateless_authentication` | Session, one-time auth flow, external identity, user auth version, and token auto-group schema |

Version 4 is an explicit bridge on every supported dialect. MySQL 5.7 and
PostgreSQL alter `kkai_outbox.event_key` to `VARCHAR(191)`; SQLite records the
same immutable migration as a physical no-op. Keeping one v4 ledger prefix
across SQLite, MySQL, and PostgreSQL makes the v5 rollout and rollback contract
unambiguous. That historical bridge was pinned to version 3 while accepting
versions through 7. The current bridge accepts versions 7 through 8 and keeps
its migration target at 7; the current feature build requires version 8.
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

Version 8 is a separate additive authentication expand. It adds
`users.auth_version`, `tokens.auto_groups`, and these tables:

- `user_sessions` for refresh rotation, expiry, and revocation state;
- `auth_flows` for short-lived one-time authentication operations;
- `external_identity_claims` for unique provider-subject ownership.

The v8 backfill initializes invalid or missing user auth versions and migrates
unambiguous legacy Telegram identities into the ownership table. It rejects an
ambiguous subject without printing that subject and does not drop or rewrite
existing authentication columns.

Applied versions are recorded in `kkai_schema_migrations` with an immutable
SHA-256 checksum. A checksum mismatch or unknown future version stops both the
migrator and application startup.

## Commands

Build the migration binary on the external build machine:

```bash
go build -trimpath -o kkai-migrate ./cmd/kkai-migrate
go build -trimpath -tags kkai_bridge -o kkai-migrate-bridge ./cmd/kkai-migrate
```

The untagged binary is the final feature profile. The `kkai_bridge` tag is a
compile-time-only bridge profile; there is no runtime environment switch that
can weaken an already-built feature image.

Use `KKAI_MIGRATION_DSN`, `SQL_DSN`, or `--dsn-stdin`. Prefer stdin for an
operator-run migration so the DSN does not appear in a process argument. The
command never prints the DSN.

```bash
./kkai-migrate --dry-run
./kkai-migrate
./kkai-migrate --check
./kkai-migrate --check --min-version 7
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

The historical v4-through-v7 bridge release contract is `runtime_min_version=3`,
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

That historical feature contract was `(7,7,7)` and failed closed on pre-v7
databases. It is not the current untagged feature profile.

## Current V8 Authentication Expand

The current bridge contract is `(7,8,7)` with only the canonical v7 and v8
prefixes. For an ordinary application release on live schema v7, build this
profile and leave the database unchanged:

```bash
scripts/kkai/build-manual-release.sh --schema-contract bridge
```

The current feature contract is `(8,8,8)` with only the canonical v8 prefix.
It cannot start on v7. Use it only after the separately authorized v8 procedure
has completed and `current_version: 8` has been independently observed:

```bash
scripts/kkai/build-manual-release.sh --schema-contract feature
```

The v8 migration requires the reviewed bridge in both effective slots, a safe
authentication precheck, a verified snapshot, explicit apply confirmation,
and post-apply check/observe. Ordinary application delivery and generic deploy
preflight do not run or prove any of those gates. There is no automatic down
migration; retain the reviewed bridge and rollback evidence under the
infrastructure runbook's retention rules.

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

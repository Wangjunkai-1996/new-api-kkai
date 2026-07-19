# KKAI Background Jobs

NewAPI has one scheduling boundary for recurring application work:
`BackgroundJobRegistry`. Production startup must not create independent
infinite-loop goroutines for database-writing maintenance.

## Node Roles

`KKAI_NODE_ROLE` accepts three values:

| Role | Serves requests | Read-only sync | Write jobs | Runtime AutoMigrate |
| --- | --- | --- | --- | --- |
| `standby-readonly` | Yes, when routed directly for canary checks | Yes | No | No |
| `serving` | Yes | Yes | No | No |
| `leader` | Yes | Yes | Only while holding the global lease | No |

For compatibility, `NODE_TYPE=slave` maps to `standby-readonly` when
`KKAI_NODE_ROLE` is unset. `DISABLE_BACKGROUND_TASKS=true` disables write jobs
only; options, channel cache, authorization policy, and pricing continue to
refresh on every node. Generic development builds retain upstream AutoMigrate.
Formal images are compiled with immutable `schema_management=external`, so
production startup never runs GORM AutoMigrate regardless of role or environment
drift.

## Registry Rules

- Every job has a stable name and positive interval.
- A job is either read-only or a writer. Every writer must declare that it
  requires the leader lease.
- Read-only jobs run on all roles.
- Only a `leader` role with write jobs enabled attempts the lease.
- Lease loss cancels the leadership context before another acquisition attempt.
- Shutdown flushes run before the holder releases a lease it still owns.
- `BATCH_UPDATE_ENABLED=true` is rejected because process-local quota buffers
  cannot be transferred safely during a leader handoff.

The lease is stored in `kkai_job_leases`. Acquisition and expired takeover are
atomic, renewals require the current holder, releases are holder-safe, and the
monotonic `fence` value records ownership generations. Lease expiry is computed
from database time, not container clocks. Business jobs must honor context
cancellation; the fence is not a substitute for cancellation inside a
long-running external request or database transaction.

During a blue/green handoff, a temporary `serving` process from the candidate
image carries public traffic without running background writers. The previous
leader is retired before the candidate starts as leader, so the first upgrade
from a legacy image never depends on that legacy process honoring the lease.
After the candidate acquires the single database lease and the selected release
passes final validation, the temporary handoff is removed.

## Standby Safety

The standby GORM callback rejects ORM writes and allows only a conservative set
of raw read statements. This is defense in depth, not the primary permission
boundary. Production standby credentials must use a read-only PostgreSQL role
with `default_transaction_read_only=on`.

At startup a standby performs only reads after the database connection opens:
KKAI migration verification, authorization load, setup detection, options,
channel cache, pricing, and custom provider load. It never runs schema
migrations or implicit ability repair.

## Registered Jobs

The application registry contains these classes:

- Read-only: runtime options, channel cache, authorization policy, pricing.
- Leader writes: system instance reporting, system task maintenance, Codex
  credential refresh, subscription maintenance, quota dashboard flush,
  performance metric flush, optional channel balance refresh, KKAI risk stream
  consumption, and durable outbox delivery.

Adding a recurring writer requires a registry entry, context-aware execution,
and a regression test proving it cannot run without leader capability.

# KKAI Background Jobs

NewAPI has one scheduling boundary for recurring application work:
`BackgroundJobRegistry`. Production startup must not create independent
infinite-loop goroutines for database-writing maintenance.

## Node Roles

`KKAI_NODE_ROLE` accepts three values. Request-local flushes persist data that
exists only inside the process handling the request; they are not global
maintenance ownership.

| Role | Serves requests | Read-only sync | Request-local flushes | Global write jobs | Runtime AutoMigrate |
| --- | --- | --- | --- | --- | --- |
| `standby-readonly` | Yes, for direct health checks | Yes | No | No | No |
| `serving` | Yes | Yes | Yes | No | No |
| `leader` | Yes | Yes | Yes | Only while holding the global lease | No |

For compatibility, `NODE_TYPE=slave` maps to `standby-readonly` when
`KKAI_NODE_ROLE` is unset. `DISABLE_BACKGROUND_TASKS=true` disables global write
jobs only; a request-capable node still flushes state accumulated by its own
requests. Options, channel cache, authorization policy, and pricing continue to
refresh on every node. Generic development builds retain upstream AutoMigrate.
Formal images are compiled with immutable `schema_management=external`, so
production startup never runs GORM AutoMigrate regardless of role or environment
drift.

## Registry Rules

- Every job has a stable name and positive interval.
- A job is read-only, a request-local state flush, or a global writer.
- A request-local state flush must declare `FlushesProcessLocalState`; it runs
  only on `serving` and `leader` roles and never on standby.
- Every other writer must declare that it requires the leader lease.
- Read-only jobs run on all roles.
- Only a `leader` role with write jobs enabled attempts the lease.
- Lease loss cancels the leadership context before another acquisition attempt.
- Request-local and leader-owned shutdown flushes run before process exit; the
  leader releases a lease only after its own shutdown flushes.
- `BATCH_UPDATE_ENABLED=true` is rejected because process-local quota buffers
  cannot be transferred safely during a leader change.

The lease is stored in `kkai_job_leases`. Acquisition and expired takeover are
atomic, renewals require the current holder, releases are holder-safe, and the
monotonic `fence` value records ownership generations. Lease expiry is computed
from database time, not container clocks. Business jobs must honor context
cancellation; the fence is not a substitute for cancellation inside a
long-running external request or database transaction.

During a `router-v3-staged` release, the public active slot and the dedicated
`newapi-writer` remain unchanged while the candidate starts in the idle
application slot as `serving`, with global background tasks disabled. It uses
the production writer database identity, so candidate acceptance requests can
write production business data even though the private candidate proxy is
traffic-isolated. Promotion is a separate controller action: it switches the
stable router, updates the dedicated writer to the promoted release, and
demotes the previous application slot to `standby-readonly`. Only
`newapi-writer` may run as `leader`; the previous release remains the symmetric
rollback target.

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
- Request-local writes: quota dashboard flush.
- Leader writes: system instance reporting, system task maintenance, Codex
  credential refresh, subscription maintenance, performance metric flush,
  optional channel balance refresh, KKAI risk stream consumption, and durable
  outbox delivery.

Adding a recurring writer requires a registry entry, context-aware execution,
and a regression test proving that it runs only within its declared request-local
or leader-owned scope.

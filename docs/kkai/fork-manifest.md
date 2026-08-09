# KKAI Fork Manifest

This document defines the owned surface of the KKAI fork. It is the source of
truth for deciding what is migrated, tested, released, or deliberately left to
upstream.

## Immutable Baseline

- Upstream repository: `github.com/QuantumNous/new-api`
- Upstream commit: `7c28993f6bd9e92616f3f578212577f8b7c40b45`
- Upstream label: `v1.0.0-rc.21` plus the pricing-page fix in `7c28993f`
- Rebuild branch: `rebuild/kkai-fork-v2-20260714`
- Production branch: `production/kkrich`
- Archived production head: `archive/production-kkrich-537501c5`

The rebuild branch must remain a descendant of the pinned upstream commit.
Quality checks compare the working tree with that commit using only local Git
objects. They never require a network code-hosting service, SSH, VPN, proxy, or
production-server state.

The immutable 206-path legacy snapshot is in `legacy-fork-files.txt`; its
port/rewrite/drop decisions are in `legacy-port-plan.md`.

## Scope

| Capability | Fork ownership | Legacy source | Rebuild status |
| --- | --- | --- | --- |
| FRT upstream response timing | Backend relay and log metadata | `d84a322e` and patch guard | Complete |
| Policy Incident Guard | Evidence, public errors, durable actions, audit | `828998d1` through `7ca9c8bc` | Complete; local relay and signed edge events share one durable action service |
| Invitation rebate and balance adjustments | API, idempotent ledger, admin/user UI | invitation commit series through `656e79e6` | Complete |
| Dynamic billing expressions | KKAI model ratios, tier variables, tests | production fork ratio changes | Complete; exact configured completion ratios override official fallbacks |
| Cache token billing | Unified cache read/write accounting on upstream converter | upstream `48068ce9` plus KKAI expressions | Complete; upstream implementation retained with fork acceptance tests |
| Standby configuration synchronization | Read-only options and channel cache refresh | `0f8616b9` | Complete; PostgreSQL dual-process verification included |
| Group status monitoring | Read API, aggregation, default frontend | `6f931ccf` through `c6ce2a85` | Complete |
| CC Switch import | One-time ticket flow, default and classic UI | `c63c41df` through `574ef743` | Approved exclusion: CC Switch `c8b0d60c` rejects remote `configUrl` exchange; unsafe URI credentials are forbidden |
| Waffo and wallet customization | Payment adapters and recharge display | production fork | Complete; upstream Waffo retained and fork UI restored |
| Classic frontend customization | KKAI-compatible classic build and UI, excluding CC Switch | production fork | Complete; build compatibility and recharge-pricing default restored |
| Blue/green release control | Slot identity, leader role, rollback manifest | `kkai-infra` | Simple read-only idle-slot deployment |
| Risk guard edge service | Detection only; no direct database writes | legacy `ops/ai-risk-guard` | Implementation complete; edge activation remains separate from application delivery |
| Signed internal attribution | Exact origin allowlist, HMAC, timestamp, nonce contract | legacy private-IP headers | Complete |

## Explicit Exclusions

- Upstream defects that are unrelated to a KKAI-owned capability.
- Broad cleanup or reformatting of upstream files.
- Translation completeness work for this remediation.
- Floating upgrades beyond the pinned upstream commit.
- Direct edits to `production/kkrich` outside the normal reviewed merge flow.
- Builds on the production server.

An upstream defect may only be changed when it blocks a documented KKAI
capability. Such a change must be isolated, tested, and recorded as fork-owned
compatibility behavior rather than presented as general upstream cleanup.

## Architecture Boundaries

1. NewAPI is the only writer for durable policy actions, invitation balances,
   and other KKAI business state.
2. Edge risk detection publishes authenticated events to Redis Streams. It
   does not write PostgreSQL or mutate users directly.
3. Internal attribution uses an exact origin allowlist plus HMAC, timestamp,
   and nonce replay protection.
4. Background jobs are registered by name and write-capability. A leader lease
   gates write jobs; standby instances continue read-only option and channel
   cache refreshes.
5. Standby database credentials are read-only. Application flags are a second
   guard, not the primary permission boundary.
6. Fork-owned schema changes use versioned, forward-only migrations. Startup
   model auto-migration is not used for KKAI tables.
7. Blue and green slots may run different immutable image digests. Slot
   replacement is independent from active-slot switching.
8. CC Switch URLs carry a short-lived one-time ticket, never a reusable API
   key.
9. The new idle-slot instance always uses read-only database credentials and runs
   no background writers while its health and version are checked.
10. Release-link changes and systemd restarts are an infrastructure-owned
    transaction. Application delivery never stops a slot or changes traffic;
    after the switch, only the selected release may own the stable alias and
    writer role, while the previous release remains available for rollback.

## Migration Rules

- Database maintenance is separate from ordinary blue/green application
  delivery; the release path neither observes nor changes the schema.
- Destructive schema changes require their own explicit operator plan.
- Every fork migration must have an idempotency test and a production-clone
  smoke test on PostgreSQL.
- SQLite, MySQL 8, and PostgreSQL 18 startup coverage remains mandatory even
  when production uses PostgreSQL.

## Development Quality Checks

`scripts/kkai/check-fork-quality.sh` enforces the following:

- the pinned upstream commit is an ancestor of the checked commit;
- new fork-owned feature source files stay at or below 250 lines and other new
  fork-owned source files stay at or below 500 lines, excluding generated code;
- changes to existing upstream source add at most 100 lines per file, reduced
  to 25 lines once the upstream file has 800 lines and 10 lines once it has
  1200 lines; modified upstream feature files have a 50-line ceiling;
- the source-size gate runs its own regression suite so additions, modifications,
  oversized upstream files, and generated-file exemptions cannot silently drift;
- changed Go files are formatted and changed shell scripts parse;
- default typecheck and both frontend builds succeed;
- changed frontend files are formatted;
- default frontend lint diagnostics do not increase over upstream by
  file/rule/severity;
- Go vet diagnostics do not increase over upstream by file/message;
- full mode runs the Go test suite.

The baseline is computed from a temporary detached worktree at the pinned
commit. Existing upstream warnings remain visible but are not attributed to
KKAI. Any additional warning or error introduced by the fork fails the gate.
These checks run during review and development workflows; they do not run beside
or block the production image workflow.

## Commit and Release Policy

- Keep commits separated by concern: baseline/tooling, backend capability,
  risk pipeline, standby/infra, frontend, and verification documentation.
- The production workflow builds one Linux AMD64 image from each runtime push
  to `production/kkrich` and publishes version and source-SHA tags for the same
  digest.
- The workflow signs the digest and dispatches only its source SHA, version, and
  digest to `kkai-infra`.
- Infrastructure verifies and deploys that exact digest through the read-only
  idle-slot path. Database maintenance is not part of application delivery.

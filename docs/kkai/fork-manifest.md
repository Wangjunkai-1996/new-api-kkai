# KKAI Fork Manifest

This document defines the owned surface of the KKAI fork. It is the source of
truth for deciding what is migrated, tested, released, or deliberately left to
upstream.

## Immutable Baseline

- Upstream repository: `github.com/QuantumNous/new-api`
- Upstream commit: `0ab02020603d22e5613bc4cf46bfab06f8567769`
- Upstream label: `v1.0.0-rc.23`
- Rebuild branch: `rebuild/kkai-fork-v2-20260714`
- Production branch: `production/kkrich`
- Archived production head: `archive/production-kkrich-537501c5`

The rebuild branch must remain a descendant of the pinned upstream commit.
Quality checks compare the working tree with that commit. They do not require
the current commit to equal `origin/production/kkrich`, and they never use SSH,
VPN, proxy, or production-server state.

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
| CC Switch import | One-time ticket flow and frontend UI | `c63c41df` through `574ef743` | Approved exclusion: CC Switch `c8b0d60c` rejects remote `configUrl` exchange; unsafe URI credentials are forbidden |
| Waffo and wallet customization | Payment adapters and recharge display | production fork | Complete; upstream Waffo retained and fork UI restored |
| Unified frontend customization | KKAI-compatible web UI, excluding CC Switch | production fork | Complete; rc.23 single-frontend migration retained the recharge-pricing default |
| Blue/green release control | Slot identity, leader role, rollback manifest | `kkai-infra` | `router-v3-staged` candidate acceptance and explicit promotion |
| Risk guard edge service | Detection only; no direct database writes | legacy `ops/ai-risk-guard` | Implementation complete; edge activation remains separate from application delivery |
| Signed internal attribution | Exact origin allowlist, HMAC, timestamp, nonce contract | legacy private-IP headers | Complete |

## Explicit Exclusions

- Upstream defects that are unrelated to a KKAI-owned capability.
- Broad cleanup or reformatting of upstream files.
- Translation completeness work for this remediation.
- Floating upgrades beyond the pinned upstream commit.
- Unreviewed or unverified changes to `production/kkrich`. When the user asks
  to commit or push, run the required local checks, then commit and push verified
  changes directly to that branch. Create a branch, worktree, or pull request
  only when the user explicitly requests one. If repository rules reject the
  direct push, report the exact blocker and stop instead of automatically
  switching to a pull request.
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
9. A `router-v3-staged` candidate runs in `serving` mode with background tasks
   disabled and the production writer database identity. Its candidate proxy is
   traffic-isolated, but candidate test requests can still change production
   business data while health, version, and feature behavior are checked.
10. Only the infrastructure-owned manual controller may change release links,
    router traffic, or slot lifecycle. `stage` does not switch traffic;
    `promote` switches the existing router, drains the previous active slot,
    updates the standby and writer, and preserves the previous release for
    rollback. Neither operation reloads `kkai-newapi.service`.

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
- frontend tests, i18n checks, typecheck, and production build succeed;
- changed frontend files are formatted;
- frontend lint diagnostics do not increase over upstream by
  file/rule/severity;
- Go vet diagnostics do not increase over upstream by file/message;
- full mode runs the Go test suite.

The baseline is computed from a temporary detached worktree at the pinned
commit. Existing upstream warnings remain visible but are not attributed to
KKAI. Any additional warning or error introduced by the fork fails the gate.
These checks run during review and development workflows; they do not run as
part of or block the manual production image build.

## Commit and Release Policy

- Keep commits separated by concern: baseline/tooling, backend capability,
  risk pipeline, standby/infra, frontend, and verification documentation.
- When the user asks to commit or push, run the required local checks, then
  commit and push verified application changes directly to `production/kkrich`.
  Create a branch, worktree, or pull request only when the user explicitly
  requests one. If repository rules reject the direct push, report the exact
  blocker and stop instead of changing workflows automatically.
- GitHub Actions must not build or deploy KKAI production images. An operator
  builds one Linux AMD64 image from a clean local `production/kkrich` checkout
  and stages its checksummed archive through the documented
  `router-v3-staged` manual controller. Promotion remains a separate explicit
  operator action after candidate acceptance.
- Database maintenance is not part of application delivery.

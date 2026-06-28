# KKAI Production Branch and Release Governance

## Source of Truth

The fixed production branch for the KKAI/Kkrich fork is:

```text
production/kkrich
```

This branch is the only normal source for production artifacts. Feature, fix, hotfix, upgrade, and release-candidate branches may be used for development and review, but production is not complete until the deployed commit exists on `production/kkrich` and has been pushed to `github-kkai`.

## Branch Taxonomy

Use these branch families consistently:

| Branch | Purpose | Production rule |
| --- | --- | --- |
| `production/kkrich` | Stable deployed release line | Only normal build source |
| `feature/kkai-<scope>-YYYYMMDD` | New product behavior | Merge into `production/kkrich` before deployment completion |
| `fix/kkai-<scope>-YYYYMMDD` | Non-emergency bug fixes | Merge into `production/kkrich` before deployment completion |
| `hotfix/kkai-<scope>-YYYYMMDD` | Emergency production patches | Start from `production/kkrich`, merge back immediately |
| `upgrade/kkai-upstream-YYYYMMDD` | Official upstream sync | Merge official changes, preserve fork patches, then merge into `production/kkrich` |
| `release/kkai-YYYYMMDD-<scope>` | Optional temporary release candidate | Short-lived only; must be merged or fast-forwarded into `production/kkrich` |

Do not use changing feature branch names as the identity of production. A feature branch can explain where work started; `production/kkrich` records what actually shipped.

## Version and Image Tags

Use production-oriented image tags:

```text
kkai-prod-YYYYMMDD.N-<shortsha>
```

Examples:

```text
kkai-prod-20260628.1-d5883b65
kkai-prod-20260628.2-a1b2c3d4
```

Avoid tags that make production look tied to a temporary feature branch, such as `kkai-YYYYMMDD-channel-monitoring-*`, after this governance rule is in place.

## Required Workflow

### 1. Start from production

```bash
git fetch github-kkai --prune
git switch production/kkrich
git pull --ff-only github-kkai production/kkrich
```

For new work:

```bash
git switch -c feature/kkai-<scope>-YYYYMMDD
```

For emergency work:

```bash
git switch -c hotfix/kkai-<scope>-YYYYMMDD
```

### 2. Integrate back into production

After review and validation:

```bash
git switch production/kkrich
git pull --ff-only github-kkai production/kkrich
git merge --no-ff <feature-or-fix-branch>
```

Fast-forward is acceptable when the branch is linear and already reviewed:

```bash
git merge --ff-only <feature-or-fix-branch>
```

### 3. Run mandatory checks

At minimum, before building a production artifact:

```bash
env PATH="/Users/tokk/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH" bun run build:check
scripts/check-frt-header-patch.sh
go test ./service ./model ./middleware ./controller ./relay -run 'PolicyIncident|TokenDisable|Policy|RelayTask|GenerateTextOtherInfo' -count=1
```

Run additional focused tests for the feature being shipped. For example, channel monitoring changes should also run:

```bash
go test ./model ./pkg/perf_metrics ./service ./controller -run 'GroupStatus|QueryLastEventsByGroup|QueryRecentEventsByGroup|RecentGroupRequestSignals' -count=1
```

### 4. Push production before rollout is considered complete

```bash
git push github-kkai production/kkrich
```

If a build was made before the final push for operational reasons, the rollout is not complete until the exact deployed commit is pushed to `github-kkai/production/kkrich`.

### 5. Build and deploy blue-green

Current production uses Docker blue-green app containers behind OpenResty:

```text
new-api-green -> 127.0.0.1:6781
new-api-blue  -> 127.0.0.1:6782
```

Either color can be active. Always verify the real active target from OpenResty instead of assuming by color name:

```bash
grep -R "127.0.0.1:678[12]" -n /opt/1panel/www/conf.d/api.kkrich.ltd.conf /opt/1panel/www/conf.d/api-origin.kkrich.ltd.conf
```

Deploy into the idle color first, validate it locally, then switch OpenResty. Keep the previous active color as the rollback slot.

### 6. Post-rollout verification

Run these checks after switching traffic:

```bash
curl -sS -D - https://api.kkrich.ltd/api/status -o /tmp/status.json | grep -iE 'http/|x-new-api-version'
docker ps --filter 'name=new-api' --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
docker logs --since 10m --tail 300 <active-new-api-container> | grep -Ei 'panic|fatal|SQLSTATE|ERROR|ERR' || true
```

Expected:

- `HTTP/2 200` from `/api/status`.
- `x-new-api-version` matches the intended production image tag.
- OpenResty points at the intended active color.
- Exactly two NewAPI app containers are kept: active and rollback.
- Obsolete NewAPI containers, unused images, temporary tarballs, and deployment backup files are removed.

## Upstream Sync Workflow

Do not update production by pulling official upstream directly into a server checkout.

Use this flow:

1. Start from clean `production/kkrich`.
2. Create `upgrade/kkai-upstream-YYYYMMDD`.
3. Fetch and merge the selected official target from `official/main` or a release tag.
4. Resolve conflicts while preserving fork-only production patches.
5. Run patch guards and focused tests.
6. Merge the upgrade branch into `production/kkrich`.
7. Push `production/kkrich`.
8. Build and deploy through blue-green.

Fork-only patches that must be preserved:

- FRT header display behavior documented in `docs/ops/frt-header-patch.md`.
- Policy incident guard behavior documented in `docs/ops/policy-incident-patch.md`.
- CC Switch one-click import behavior documented in `docs/ops/cc-switch-import-patch.md`.
- KKAI/Kkrich production UI and payment/invitation customizations already present on `production/kkrich`.

## AI Assistant Rules

Any Codex, Claude, or other code-assistant session working on this fork must:

- Fetch `github-kkai` before planning production work.
- Treat `production/kkrich` as the release source of truth.
- Create work branches from `production/kkrich`, not from stale feature branches.
- Never claim a rollout is complete while the deployed commit only exists on a temporary branch.
- Preserve fork-only production patches during upstream syncs.
- Avoid building on the small production server unless explicitly approved.
- Leave the server with exactly two NewAPI blue-green app containers after deployment cleanup.

## Why This Exists

Earlier rollouts used branch names such as `feature/kkai-channel-monitoring-20260627` directly in build and image tags. That made production look like it moved from feature branch to feature branch, even when the commit graph was correct. The operational risk is real: a future rebuild could start from the wrong branch and accidentally omit fork-only patches.

This governance makes the production line boring on purpose:

```text
all reviewed work -> production/kkrich -> kkai-prod-* image -> blue-green deploy
```

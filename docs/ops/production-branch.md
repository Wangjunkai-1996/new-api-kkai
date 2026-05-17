# Production Branch Workflow

## Purpose

This fork uses a fixed production branch so the running container, GitHub source, and server checkout do not drift apart.

Production branch:

```text
production/kkrich
```

## Contract

- `production/kkrich` is the source of truth for deployed KKAI/Kkrich code.
- Production artifacts must be built from `production/kkrich` or from a temporary release branch that is merged or fast-forwarded into `production/kkrich` before the rollout is considered complete.
- `/root/new-api` on the server should normally be checked out to `production/kkrich`.
- Temporary clean worktrees are allowed for safety, but they must not become the only place where deployed source exists.
- Do not deploy an official upstream image directly. Preserve the fork-only patches documented in:
  - `docs/ops/frt-header-patch.md`
  - `docs/ops/policy-incident-patch.md`

## Pre-Build Check

Run from the repository used for the build:

```bash
git fetch origin
git status --short --branch
git rev-parse --short HEAD
scripts/check-frt-header-patch.sh
go test ./service ./model ./middleware ./controller ./relay -run 'PolicyIncident|TokenDisable|Policy|RelayTask|GenerateTextOtherInfo' -count=1
```

Expected:

- branch is `production/kkrich` or an explicitly named release branch;
- worktree is clean except for generated frontend `dist` output when building locally;
- FRT patch guard passes;
- focused policy tests pass.

## Official Upstream Upgrade Workflow

Do not update production by running `git pull upstream main` directly in `/root/new-api`.

Use this flow instead:

1. Start from a clean `production/kkrich`.
2. Create a temporary upgrade branch:

   ```bash
   git checkout production/kkrich
   git pull --ff-only origin production/kkrich
   git checkout -b upgrade/upstream-YYYYMMDD
   ```

3. Merge the selected official target:

   ```bash
   git fetch upstream --tags --prune
   git merge upstream/main
   ```

   Use `upstream/main` when a needed fix has landed after the latest release tag. Use a release tag such as `v1.0.0-rc.6` when minimizing change scope matters more.

4. Resolve conflicts while preserving:
   - `docs/ops/frt-header-patch.md`
   - `docs/ops/policy-incident-patch.md`
   - `docs/ops/production-branch.md`
   - `scripts/check-frt-header-patch.sh`
   - FRT header display behavior
   - policy incident guard behavior
   - production-specific pricing display behavior

5. Run guards and focused tests:

   ```bash
   scripts/check-frt-header-patch.sh
   go test ./service ./model ./middleware ./controller ./relay -run 'PolicyIncident|TokenDisable|Policy|RelayTask|GenerateTextOtherInfo' -count=1
   ```

6. Build with the local Go+Bun rollout workflow in `docs/ops/local-go-bun-rollout.md`.
7. Deploy with canary first, then replace only the `new-api` app container.
8. Fast-forward or merge the verified upgrade result back into `production/kkrich`.
9. Push `production/kkrich` and make sure `/root/new-api` is checked out to the same production commit.

## Post-Rollout Check

After the container is live:

```bash
curl -sS http://127.0.0.1:3000/api/status
curl -I https://kkrich.ltd/
podman ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
git -C /root/new-api status --short --branch
git -C /root/new-api rev-parse --short HEAD
```

Expected:

- status/version matches the deployed image tag;
- `x-new-api-version` matches the intended version;
- `/root/new-api` is on `production/kkrich` at the deployed source commit;
- Postgres and Redis remain untouched unless a migration plan explicitly says otherwise.

## Why This Exists

The 2026-05-17 policy incident rollout was safely built from a clean temporary worktree to avoid an unrelated dirty file in `/root/new-api`. The running container was correct, but `/root/new-api` stayed behind the deployed commit. That created a future risk: rebuilding directly from `/root/new-api` could overwrite the policy incident guard patch.

This workflow prevents that class of drift by making `production/kkrich` the stable release line and requiring the server checkout to be reconciled after every rollout.

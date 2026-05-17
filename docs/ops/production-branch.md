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

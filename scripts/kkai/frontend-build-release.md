# Independent Frontend Artifact

`frontend-build-release.sh` builds the modern `default` Rsbuild bundle as a
separate, immutable production artifact. It does not upload files, change
`current` or `previous`, restart services, or invoke a deployment controller.

## Normal production format (2)

Run from the clean local `production/kkrich` checkout:

```bash
scripts/kkai/frontend-build-release.sh --theme default
```

The builder exports the exact source commit with `git archive` into an ordinary
temporary directory; it does not create a Git branch or worktree. Dependencies,
source, legal notices, lockfile, packaging helper and compatibility metadata
come from that same commit. Ignored local `.env` files and existing
`node_modules` are excluded. Later changes in the original checkout do not
change the archived inputs or block the build; metadata retains the chosen SHA.
An isolated temporary Git index rejects tracked-file changes and non-ignored
new files written by install/build hooks, without changing the live Git index.

Production requires the Bun version declared by `KKAI_BUN_VERSION` in that
commit's `docs/kkai/upstream-baseline.env`, which is also used by the fork quality
gate. Metadata records the real `bun --version`; a metadata-only environment
override cannot change it. The builder installs only the selected workspace:

```text
bun install --frozen-lockfile --network-concurrency=1 --concurrent-scripts=1 --filter ./default
bun run build -- --dist-path <temporary-directory>
```

The normal Bun global package cache remains available between builds; only the
snapshot's dependency tree is materialized afresh. The script does not borrow
or copy a potentially mismatched live `node_modules`, clear the package cache,
or guarantee that an uncached dependency can be installed without network access.
Real installation/build duration is not measured by the mocked regression test.

Compatibility comes from the same commit's `web/default/console-contract.json`.
Format 2 accepts no backend identity, schema profile or arbitrary API-contract
override. Compatible backend updates preserve the frontend artifact and its
current/previous pointers; a backend-only release does not rebuild this bundle.

## Prepare, activate and verify

Use format 2 only after infrastructure runbook 21's controller, Edge and backend
contract migration has been installed and accepted. Source capabilities and
historical records do not prove the installed capability. Confirm the current
backend with the selected infrastructure checkout's `make newapi-status` and
its immutable manifest, then follow runbook 21's frontend release procedure:

1. Upload the exact generated metadata and archive over the documented private
   SSH route. `prepare` verifies and retains the artifact without changing
   public selection.
2. Use `activate --release-id ... --expected-current-release ...` with the
   verified current backend manifest. The controller rechecks actual runtime
   compatibility, loaded Edge routes and selection identity under its locks.
   Frontend activation runs outside backend candidate/rollout transactions.
3. Verify the public entry, versioned JS/CSS, affected pages/APIs, error handling
   and relevant SSE flows. If present, verify the public `assets/kikai-home.html`
   checksum and `no-store` caching as required by runbook 21.

Frontend-only delivery does not build, stage, canary or promote a backend. A
compatible installed historical frontend can be selected through `activate`
without reuploading its archive. For backend rollback, use `rollback-plan` to
find a safe order; a previous frontend pointer alone does not establish backend
rollback readiness. Retain release trees, receipts and referenced metadata until
the controller's recovery and browser asset support requirements permit cleanup.

The browser artifact always uses relative API URLs. The production edge must
proxy NewAPI paths and route `/invitations/api/` to the independent KKAI
Invitations service, rewriting the public prefix to `/api/` upstream. For local
Rsbuild development, `VITE_INVITATIONS_API_URL` defaults to
`http://localhost:6212` and only configures the dev proxy.

## Output and integrity

```text
frontend-releases/<release-id>/
  default/
  LICENSE
  NOTICE
  THIRD-PARTY-LICENSES.md
  frontend.json
  manifest.sha256
<release-id>.tar.gz
<release-id>.json
```

`frontend.json` records frontend source SHA, API/capability requirements, the
immutable asset URL, actual Bun version and lockfile SHA-256. It contains no
backend release, source, image or schema pairing. `manifest.sha256` covers every
release file except itself, including legal notices. Outer metadata also records
the archive and manifest digests. Format 2 has no `release-pair.json`.

The release tree, archive and outer metadata are prepared in a temporary sibling
and moved into place only after a successful build. Existing release IDs are
rejected; failure removes incomplete outputs. Only the pinned infrastructure
controller changes live selection pointers.

## Local phase record

A real build writes a separate JSON timing/identity record beside its output;
`RELEASE_RECORD=<absolute-path>` is printed to stderr. Recording starts before
Bun installation and fails before that work if the record cannot be initialized.
It covers source export, Bun validation, dependency installation when enabled,
bundling, source validation, checksums, archive creation and publication. The
exit trap retains the original build exit status and records a failed phase
when a command fails.

The record includes source/release identity, actual Bun and lockfile versions,
archive checksum and byte count. It is excluded from the immutable artifact and
deployment metadata; existing controller interfaces and checksums are unchanged.
`--dry-run` does not create a timing record. Mocked timings validate recording
behavior only; use records from the next authorized real build to find bottlenecks.

## Local experiments and focused verification

`--allow-dirty` builds the live source tree for explicitly local experiments.
`--allow-non-production` permits another source branch. Both permit an unpinned
local Bun executable, whose actual version is still recorded. `--skip-install`
requires one of these explicit local flags and uses that checkout's existing
dependencies; it is forbidden for a formal production build. `classic` and
`both` remain local-only legacy compatibility themes.

`--source-root`, `--source-sha`, executable overrides and `--lock-dir` support
local fixtures. `--release-id` and `--build-timestamp` make metadata stable.
`--dry-run` validates the plan without installing, building or writing output.
Run the seconds-scale regression with mocked Bun:

```bash
scripts/kkai/frontend-build-release_test.sh
```

It covers packaging, legal-file checksums, frozen filtered installs, format 2,
local legacy themes, duplicate rejection, dry-run and failed-build cleanup,
production Bun pinning, ignored local input isolation, hook mutation rejection,
and clean HEAD changes during a build. It does not install dependencies, compile a real bundle, contact
production, or replace release acceptance.

## Legacy format 1 only

Existing format 1 installations and explicit migrations retain exact
frontend/backend pairing. They require `--legacy-pair`, the verified backend
release/source/image coordinates, and the matching `--schema-contract` and
`--api-contract`. Their artifacts include `release-pair.json`; do not edit old
metadata to convert them to format 2. Legacy `install` changes public selection
immediately, unlike format 2's normal prepare/activate sequence.

Use `docs/runbooks/20-newapi-frontend.md` in the confirmed infrastructure checkout
for that legacy procedure and initial Edge adoption. Use runbook 21 for format 2
daily delivery and controlled format transitions. A routine compatible release
does not repeat controller installation, ACME setup or first Edge adoption.

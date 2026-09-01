# KKAI production image

This directory contains the Dockerfile and runtime helpers used for the KKAI
New API image.

Production images are built manually from a clean `production/kkrich` checkout
with `scripts/kkai/build-manual-release.sh`. The script emits one Linux AMD64
Docker archive and a metadata file under `.local-releases/`.

There is no production-safe profile default for every live schema. Production
commands must pass `--schema-contract` explicitly after the live schema has
been established:

- `bridge` is the current `(7,8,7)` profile. Use it for an application-only
  release while the database remains at v7.
- `feature` is the current `(8,8,8)` profile. Use it only after schema v8 has
  been independently observed.

Without exact v8 evidence, select `bridge`; do not omit the explicit profile or
use generic deployment preflight to infer the live schema.

Build the v7-to-v8 bridge profile with:

```bash
scripts/kkai/build-manual-release.sh --schema-contract bridge
```

The manual build selects and bootstraps the configured `kkai-mirror-builder`
Buildx instance by default. It verifies the `docker-container` driver, Linux
AMD64 support, and a local Docker endpoint (normally the `desktop-linux`
context); a remote builder is rejected because production images must be built
on this Mac. Override the builder name with `--builder NAME` (or
`BUILDX_BUILDER`) only when the alternate name still resolves to that local
endpoint.

The Dockerfile uses a locked BuildKit cache mount as a builder-local semaphore
around the seven CPU/memory-heavy install and compile steps (the shared web
install, two web bundles, two backend variants, runtime tools, and media
packaging). BuildKit can
otherwise start the frontend, Go, and media stages at the same time and
exhaust the Docker VM even when each individual command has a limit. The gate
keeps those steps one-at-a-time; package-level parallelism inside each step is
still controlled separately.

Go package compilation is capped at four workers by default; tune it with
`--go-build-parallelism N` or `KKAI_GO_BUILD_PARALLELISM=N` when the workstation
has more or less headroom. The FFmpeg/x264 media stage is capped at two `make`
workers by default; tune it with `--media-build-parallelism N` or
`KKAI_MEDIA_BUILD_PARALLELISM=N` when that stage needs more or less headroom.
On Apple Silicon, the backend and runtime-tool stages use the pinned
multi-platform Go image on the native build platform and cross-compile the
same `linux/amd64` binaries. The final runtime image and the FFmpeg stages stay
on their pinned AMD64 images, so this optimization does not change the
production architecture or runtime base.
The shared frontend dependency install uses one lifecycle-script worker and
one network request at a time. This is intentional: Bun otherwise defaults to
two lifecycle jobs per visible CPU, which is a large burst on a 10-core Docker
VM.

The script also applies conservative Buildx limits to each Linux `RUN`
container by default: `cpu-quota=200000` (about two CPU cores per active step)
and `memory=3g`. These are requested per-step limits, not a global limit for
the whole build; the locked gate above limits the heavy steps in this
Dockerfile, while the overall cap still comes from Docker Desktop's VM
settings.
Override them with `--resource cpu-quota=...`, `--resource memory=...`, or the
`KKAI_BUILDX_CPU_QUOTA` and `KKAI_BUILDX_MEMORY` environment variables. Use
`--no-resource-limits` only for a measured, high-headroom build.

Only one local New API build is allowed at a time. A stale lock is reported with
its exact path instead of being removed implicitly, so verify the old process
before clearing it.

Use `--schema-contract feature` only after the v8 schema gate passes. The
profile is recorded in release metadata and in the image's
`io.kkrich.schema-contract` label. The staging client validates the metadata
value and forwards it to the production controller, which must match it against
the loaded image before accepting the candidate. That generic preflight does
not observe the database schema, so its `ready` result is not compatibility
evidence. The profile cannot be changed at runtime.

The application and `/kkai-migrate` are compiled with
`common.SchemaManagementMode=external`. The image therefore cannot run GORM
AutoMigrate when it starts in the read-only idle slot. Database maintenance is
separate from ordinary application delivery.

## Frontend modes

`build-manual-release.sh` defaults to `--frontend-mode embedded`. The mode is a
release property and must be selected explicitly for an external split:

```bash
# Self-contained image; embeds both web/default and web/classic.
scripts/kkai/build-manual-release.sh \
  --schema-contract bridge \
  --frontend-mode embedded

# Backend-only image; the edge serves the separately published frontend.
scripts/kkai/build-manual-release.sh \
  --schema-contract bridge \
  --frontend-mode external
```

The embedded path selects the `runtime-embedded`/`backend` targets and copies
both built web bundles into the image. The external path selects
`runtime-external`/`backend-external`, enables the `external_frontend` Go build
tag (plus `kkai_bridge` for the bridge schema), and does not depend on
`web-deps`, `web-default`, or `web-classic`. This keeps a backend-only build
from loading or compiling frontend stages.

Every release must carry one consistent mode in all four places: the metadata
`frontend_mode` field, the image label
`io.kkrich.frontend-mode`, the image environment entry
`FRONTEND_MODE=embedded|external`, and the controller request. The deploy
client rejects missing or invalid metadata and the controller rejects an image
whose label or environment disagrees with the request. Do not change
`FRONTEND_MODE` by editing a running container; build a new immutable image.

The external image is safe to stage only after the matching frontend artifact
and edge controller are installed and verified. The `external_frontend` tag
supplies empty embed symbols so the backend can run without a frontend tree;
the edge must provide the actual `default` or `classic` SPA files.

For a local compile-only check (no Docker image or deployment is produced):

```bash
make test-backend-only
make build-backend-only
```

`build-backend-only` writes the binary to
`.local-releases/backend-only/new-api`. The default `make`/Dockerfile targets
are unchanged and continue to require and embed the built frontend assets.
When running that binary directly, set `FRONTEND_MODE=external`; the Docker
development image and compose service set it automatically.

The standalone frontend artifact is built locally with
`scripts/kkai/frontend-build-release.sh`. It uses the same source commit as
the backend, runs a frozen Bun install by default, and emits an immutable
theme directory, `manifest.sha256`, `frontend.json`, `release-pair.json`, and
a `.tar.gz` archive. Artifact builds force a relative API base even when a local
`.env.production` contains `VITE_REACT_APP_SERVER_URL`; the edge must therefore
proxy API requests on the same origin. The script does not switch a live
frontend pointer or deploy anything. Use `--skip-install` only for isolated
tests with a prepared dependency tree.

```bash
scripts/kkai/frontend-build-release.sh \
  --schema-contract bridge \
  --backend-source-sha "$(git rev-parse HEAD)" \
  --backend-release-id "${BACKEND_RELEASE_ID:?set the backend release ID from backend metadata}" \
  --api-contract "${API_CONTRACT:?set the API contract accepted by the edge controller}"
```

For a real release pair, pass the exact backend release ID emitted by
`build-manual-release.sh`; the script's same-ID fallback is intended only for
local builds that deliberately use one shared release identifier. The API
contract has no implicit default; use the integer registered by the backend and
edge controller for the pair.

The artifact intentionally has no `frontend_mode` field. Mode belongs to the
backend/edge contract: pair an embedded artifact install with a backend release
whose metadata, image label, and `FRONTEND_MODE` all say `embedded`; pair an
external artifact install with a backend built using `--frontend-mode external`
and an edge manifest already set to `external`. The artifact controller checks
the exact backend source/release/image/schema/API coordinates before install;
recheck those coordinates after installation and before changing either mode.

The installed edge controller verifies both the archive checksum and every
entry in `manifest.sha256` before publishing an immutable theme path. It selects
the theme reported by the backend's `/api/status` response (or the artifact's
`default_theme` during bootstrap), serves that theme's `index.html` for
non-API SPA routes, and proxies these NewAPI API prefixes to the backend:
`/api`, `/v1`, `/v1beta`, `/pg`, `/mj`, `/:mode/mj`, `/suno`, `/kling`, and
`/jimeng`. It proxies `/invitations/api/` to the independent KKAI Invitations
service and rewrites that prefix to `/api/` upstream (for example,
`/invitations/api/invitations/status` becomes `/api/invitations/status`); never
send the public prefix to NewAPI. During local Rsbuild development, set
`VITE_INVITATIONS_API_URL` to that service (the default is
`http://localhost:6212`); the dev proxy applies the same rewrite. The backend
returns a plain 404 for an unmatched web request in external mode, so a missing
edge fallback is visible instead of silently serving an empty embedded page.

For `embedded` to `external`, install and verify the matching artifact first,
then set the platform edge manifest to `external` and verify the static/API
paths, and only afterward stage and promote the external backend. The NewAPI
planner enforces this edge-first order. If the switch fails, restore the backend
to `embedded` first, then restore the edge mode, and finally point the frontend
controller at the verified `previous` artifact. Use controller operations for
all pointer changes; do not edit symlinks or release files by hand. See
`scripts/kkai/frontend-build-release.md` for the complete artifact contract and
focused regression command.

When Docker build stages require the operator workstation proxy, pass it
explicitly without changing the image definition. The port below is the
current local `kkai-mirror-builder` setting; verify it with
`docker buildx inspect` before copying this example to another Mac:

```bash
BUILD_HTTP_PROXY=http://host.docker.internal:7897 \
BUILD_HTTPS_PROXY=http://host.docker.internal:7897 \
BUILDX_BUILDER=kkai-mirror-builder \
scripts/kkai/build-manual-release.sh --schema-contract bridge
```

The normal invocation keeps progress output in Docker's TTY-friendly default.
For a measured low-noise build, tune compiler or media parallelism while keeping
the resource limits enabled:

```bash
BUILDX_BUILDER=kkai-mirror-builder \
KKAI_GO_BUILD_PARALLELISM=4 \
KKAI_MEDIA_BUILD_PARALLELISM=2 \
scripts/kkai/build-manual-release.sh --schema-contract bridge
```

Set `BUILDKIT_PROGRESS=plain` only while diagnosing a failed build; it prints
every step and can add noticeable terminal overhead.

On a 16 GiB Mac, keep Docker Desktop's VM around 6 CPU / 6 GiB memory so the
host retains headroom for the editor and browser. The script limits individual
build steps, while this Docker Desktop setting provides the overall VM cap.

The BuildKit cache is shared by local builds (including any other workload that
uses this builder) and is intentionally retained for repeat releases. Check its
size before maintenance:

```bash
docker buildx du --builder kkai-mirror-builder
```

After confirming that no build or rollback depends on the cache, reclaim only
old entries from this builder (never run a workspace-wide Docker prune):

```bash
docker buildx prune --builder kkai-mirror-builder --filter until=168h
```

Stage the metadata file explicitly with:

```bash
scripts/kkai/deploy-manual-release.sh --stage .local-releases/<version>.json
```

If the remote stage exits nonzero or its response is incomplete, the client
preserves stdout/stderr and performs one read-only `candidate-status` query.
Treat that result as authoritative and do not retry stage or rebuild the same
commit until the controller state has been routed.

This replaces only the inactive blue/green slot and exposes the candidate on
the production host's loopback interface. It does not switch public traffic;
promotion remains a separate operator action after acceptance. Production
images are built and deployed only through this local/manual path.

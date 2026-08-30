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
around the six CPU/memory-heavy install and compile steps. BuildKit can
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

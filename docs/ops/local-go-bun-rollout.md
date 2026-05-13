# Local Go+Bun Rollout

## Purpose

This document records the proven low-risk rollout path used on this fork when we want to ship a small frontend/backend change without building on the production server.

The key idea:

- build the production frontend assets and Linux backend binary on the local Mac;
- upload only the built binary to the server;
- create a new application image on the server by replacing `/new-api` inside the current known-good image;
- validate with a side container first;
- replace only the `new-api` app container;
- do not restart Postgres or Redis.

This workflow is especially useful on the current small production server where local source builds can spike CPU and hurt live traffic.

## When To Use

Use this workflow when all of the following are true:

- production is already running in a container;
- the change is limited to the NewAPI app layer;
- database schema does not need migration;
- we want to avoid building source code on the production server.

Typical examples:

- classic/default frontend display changes;
- backend logging changes;
- fork-only patch refreshes;
- upstream syncs that have already been validated locally.

## Preconditions

Local Mac must have:

- `go`
- `bun`

Current production assumptions:

- app container name is `new-api`
- runtime is Podman-compatible (`docker` command may be aliased to `podman`)
- Postgres and Redis are separate running containers and must stay untouched

## Local Build Workflow

Repository root below means the local source checkout, for example:

```bash
cd /path/to/new-api-src
```

### 1. Rebuild the needed frontend

If only classic frontend changed:

```bash
cd web/classic
bun run build
cd ../..
```

If both frontends changed:

```bash
cd web/default
DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="${VERSION_VALUE}" bun run build
cd ../classic
VITE_REACT_APP_VERSION="${VERSION_VALUE}" bun run build
cd ../..
```

Notes:

- this project embeds frontend build output into the Go binary via `//go:embed`
- the frontend `dist` directories must exist before the final backend build

### 2. Build the Linux amd64 backend binary

Pick a clear version string first:

```bash
VERSION_VALUE="v1.0.0-rc.5-frt-header-bf02cc7"
OUTPUT="../artifacts/new-api-pricing-toggle-bf02cc7-linux-amd64"
```

Build:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build -trimpath \
  -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=${VERSION_VALUE}'" \
  -o "${OUTPUT}"
```

### 3. Verify the local artifact

```bash
file "${OUTPUT}"
shasum -a 256 "${OUTPUT}"
strings "${OUTPUT}" | grep -m 1 "${VERSION_VALUE}"
```

Optional fork guard for this repo:

```bash
scripts/check-frt-header-patch.sh
```

## Server Rollout Workflow

The example below assumes:

- uploaded binary path: `/root/new-api-pricing-toggle-bf02cc7-linux-amd64`
- current live image: `localhost/new-api:frt-header-20260513-6c06a38`
- target image tag: `localhost/new-api:pricing-toggle-bf02cc7`

### 1. Upload the built binary

From the local Mac:

```bash
scp ../artifacts/new-api-pricing-toggle-bf02cc7-linux-amd64 aliyun-guanghe:/root/
```

### 2. Create a new image by replacing `/new-api`

On the server:

```bash
cd /root
chmod 755 /root/new-api-pricing-toggle-bf02cc7-linux-amd64

TMP_CID=$(podman create localhost/new-api:frt-header-20260513-6c06a38)
podman cp /root/new-api-pricing-toggle-bf02cc7-linux-amd64 "${TMP_CID}:/new-api"
podman commit "${TMP_CID}" localhost/new-api:pricing-toggle-bf02cc7
podman rm "${TMP_CID}"
```

This keeps the base filesystem, entrypoint, and packages aligned with the last known-good app image while replacing only the embedded application binary.

### 3. Validate with a side container first

Run a canary on port `3001` with the same network, volumes, and environment:

```bash
podman rm -f new-api-canary >/dev/null 2>&1 || true

podman run -d \
  --name new-api-canary \
  --replace \
  --network new-api-runtime \
  -p 127.0.0.1:3001:3000 \
  --restart no \
  -v new-api_new_api_data:/data:rw \
  -v new-api_new_api_logs:/app/logs:rw \
  -e SQL_DSN=postgresql://...@postgres:5432/newapi \
  -e REDIS_CONN_STRING=redis://...@redis:6379/0 \
  -e APP_HTTP_BIND=0.0.0.0:3000 \
  -e TZ=Asia/Shanghai \
  -e BATCH_UPDATE_ENABLED=true \
  -e GENERATE_DEFAULT_TOKEN=false \
  -e STREAMING_TIMEOUT=300 \
  -e STREAM_SCANNER_MAX_BUFFER_MB=64 \
  -e MAX_REQUEST_BODY_MB=32 \
  localhost/new-api:pricing-toggle-bf02cc7 --log-dir /app/logs
```

Then verify:

```bash
curl http://127.0.0.1:3001/api/status
podman logs --tail 100 new-api-canary
```

Expected result:

- `/api/status` returns success
- reported `version` matches the new local build version

### 4. Replace only the live app container

Keep the old app container as rollback insurance:

```bash
OLD_NAME="new-api-old-$(date +%Y%m%d%H%M%S)"
podman rename new-api "${OLD_NAME}"
```

Important:

- renaming alone does not free port `127.0.0.1:3000`
- the old container must be stopped before the new live container binds the old port

Launch the new live container:

```bash
podman stop "${OLD_NAME}"

podman run -d \
  --name new-api \
  --replace \
  --network new-api-runtime \
  -p 127.0.0.1:3000:3000 \
  --restart unless-stopped \
  -v new-api_new_api_data:/data:rw \
  -v new-api_new_api_logs:/app/logs:rw \
  -e SQL_DSN=postgresql://...@postgres:5432/newapi \
  -e REDIS_CONN_STRING=redis://...@redis:6379/0 \
  -e APP_HTTP_BIND=127.0.0.1:3000 \
  -e TZ=Asia/Shanghai \
  -e BATCH_UPDATE_ENABLED=true \
  -e GENERATE_DEFAULT_TOKEN=false \
  -e STREAMING_TIMEOUT=300 \
  -e STREAM_SCANNER_MAX_BUFFER_MB=64 \
  -e MAX_REQUEST_BODY_MB=32 \
  localhost/new-api:pricing-toggle-bf02cc7 --log-dir /app/logs
```

There may be a very short switchover window where `127.0.0.1:3000` briefly refuses connections. That is expected during the stop/start handoff.

### 5. Live verification

Check local status:

```bash
curl http://127.0.0.1:3000/api/status
podman ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
podman logs --since 5m new-api | grep -Ei 'panic|fatal|SQLSTATE|stream scanner error'
```

Check public response:

```bash
curl -I https://kkrich.ltd/
```

Expected result:

- `x-new-api-version` header matches the new version string
- `new-api` is healthy
- Postgres and Redis remain untouched

## Rollback

This workflow does not modify schema. Rollback is app-container only.

If the new app is bad:

1. stop and remove the new `new-api` container;
2. stop and remove any failed canary;
3. restart the preserved old container;
4. if desired, rename it back to `new-api`.

Example:

```bash
podman rm -f new-api
podman start new-api-old-YYYYMMDDHHMMSS
podman rename new-api-old-YYYYMMDDHHMMSS new-api
```

## Proven Example

Verified in this workspace on 2026-05-14:

- local artifact:
  - `artifacts/new-api-pricing-toggle-bf02cc7-linux-amd64`
- live image:
  - `localhost/new-api:pricing-toggle-bf02cc7`
- live version header:
  - `v1.0.0-rc.5-frt-header-bf02cc7`

This release preserved the fork-only FRT header display patch and added the classic pricing-page default toggle change (`showWithRecharge: true` by default).

# FRT Header Display Patch

## Purpose

This fork intentionally changes how NewAPI displays first token latency in log `other.frt`.

The production goal is to make user-facing `frt` closer to the upstream NewAPI dashboard timing convention by using upstream HTTP response header latency, while preserving the real first SSE arrival latency for administrator troubleshooting.

## Behavior Contract

- Displayed `other.frt` uses `UpstreamHeaderTime - StartTime` when `UpstreamHeaderTime` exists.
- `other.first_sse_ms` stores the original real first SSE latency from `FirstResponseTime - StartTime`.
- If `UpstreamHeaderTime` is missing, `other.frt` falls back to the old `FirstResponseTime - StartTime` behavior.
- The patch must not fake SSE chunks, alter billing, change quotas, affect channel selection, or change retry/stream forwarding behavior.
- No database schema migration is required. The change only extends the JSON stored in `logs.other`.

## Patch Touch Points

Keep these files in mind during upstream syncs:

- `relay/common/relay_info.go`
  - `RelayInfo.UpstreamHeaderTime time.Time`
  - `RelayInfo.SetUpstreamHeaderTime()`
- `relay/channel/api_request.go`
  - call `info.SetUpstreamHeaderTime()` immediately after successful `client.Do(req)` returns a non-nil response
- `service/log_info_generate.go`
  - `frt` display helper based on upstream header time
  - `first_sse_ms` helper preserving real first SSE timing
- `service/log_info_generate_test.go`
  - regression tests for header-time `frt`, fallback behavior, and `first_sse_ms` omission when no valid SSE timing exists

Original patch commit on this fork:

- `d84a322e fix: display frt from upstream response headers`

CI/image workflow commit:

- `6c06a38e ci: build frt header image on ghcr`

## Safe Upstream Upgrade Workflow

Do not deploy the official upstream image directly. Official images do not include this fork-only patch.

Recommended workflow:

1. Fetch official upstream.
2. Create an upgrade branch from the desired upstream version.
3. Re-apply or preserve commit `d84a322e` on top of that upstream version.
4. Resolve conflicts carefully in the touch-point files above.
5. Run the patch guard:

   ```bash
   scripts/check-frt-header-patch.sh
   ```

6. Run focused tests:

   ```bash
   go test ./service -run TestGenerateTextOtherInfo -count=1
   go test ./relay/common ./relay/channel -run '^$' -count=1
   ```

7. Build the custom image with GitHub Actions/GHCR or with an external build machine.
8. On production, only pull the fixed-tag custom image and replace the `new-api` app container. Do not build on the production server.
9. Keep Postgres and Redis running. Do not restart them for this patch.

If GitHub Actions or external image build is unavailable, this fork also has a proven fallback rollout path using a local Mac build with `go` + `bun`, then replacing only `/new-api` inside the current app image on the server. See:

- `docs/ops/local-go-bun-rollout.md`

## Production Verification

After deployment, verify:

```bash
curl -I http://127.0.0.1:3000/
podman inspect new-api --format '{{.State.Health.Status}}'
podman logs --since 5m new-api | grep -Ei 'panic|fatal|SQLSTATE|stream scanner error'
```

Database spot check on PostgreSQL deployments:

```sql
select id,
       model_name,
       is_stream,
       other::jsonb->>'frt' as frt,
       other::jsonb->>'first_sse_ms' as first_sse_ms,
       other::jsonb->>'request_path' as path
from logs
where is_stream = true and other::jsonb ? 'first_sse_ms'
order by id desc
limit 10;
```

Expected result: recent streaming logs should keep both `frt` and `first_sse_ms`. `frt` should usually be lower than or close to `first_sse_ms`.

## Rollback

This patch does not change database schema. Rollback is application-container only:

1. Stop the new `new-api` container.
2. Restore the preserved previous `new-api` container or rerun the previous fixed image tag.
3. Keep Postgres and Redis untouched.

## Future-Agent Prompt

When working in a new Codex/AI window, start with:

> Read `AGENTS.md` and `docs/ops/frt-header-patch.md` first. Upgrade NewAPI from official upstream while preserving the fork-only FRT Header Display Patch. Run `scripts/check-frt-header-patch.sh` before build or deploy.

# Policy Incident Guard Patch

## Purpose

This fork intentionally adds a production safety guard for high-confidence upstream policy incidents. When an upstream NewAPI-like provider reports safety-policy failures such as `cyber_policy` or a permanently disabled upstream API key, the fork must stop retry fan-out, isolate the affected upstream key/channel, and block the implicated client token.

The goal is to avoid replaying the same risky request across additional channels while keeping enough append-only audit evidence for administrator review.

## Behavior Contract

- Detect high-confidence policy incident errors from normal relay and task relay paths.
- Mark the request context as no-retry after detection.
- Set short-lived breaker state for the client token and upstream key when Redis is available.
- Persistently disable the implicated client token by default through `policy_incident_setting.disable_client_token_persistently`.
- Auto-disable the implicated upstream channel/key through the existing channel status update path.
- Insert an append-only `policy_incident_events` audit record.
- Store upstream keys only as fingerprints and redact sensitive text from metadata.
- Notify the root user once per locked incident.
- Do not store raw request prompts or raw upstream keys in the incident event.
- Do not restart Postgres or Redis just for this patch.

## Patch Touch Points

Keep these files in mind during upstream syncs:

- `constant/context_key.go`
  - policy incident no-retry and breaker context keys
- `controller/relay.go`
  - policy incident handling in relay retry flow
- `middleware/auth.go`
  - disabled-token and policy breaker enforcement
- `middleware/distributor.go`
  - channel/key context needed by incident handling
- `model/channel.go`
  - upstream key/channel status isolation helpers
- `model/main.go`
  - `PolicyIncidentEvent` auto-migration registration
- `model/policy_incident_event.go`
  - append-only audit model, fingerprinting, redaction, metadata normalization
- `model/token.go`
  - persistent client token disable helper
- `relay/relay_task.go`
  - task relay policy incident handling
- `service/policy_incident.go`
  - classification, no-retry marking, token disable, upstream isolation, audit event, notification
- `setting/operation_setting/policy_incident_setting.go`
  - persistent token disable setting, enabled by default
- `types/error.go`
  - policy incident error typing

Original patch commits on this fork:

- `828998d1 fix: guard upstream cyber policy incidents`
- `91259f4a fix: enforce policy incident client token ban`

## Safe Upstream Upgrade Workflow

Do not deploy the official upstream image directly. Official images do not include this fork-only patch.

Recommended workflow:

1. Fetch official upstream.
2. Create an upgrade branch from the desired upstream version.
3. Re-apply or preserve commits `828998d1` and `91259f4a` on top of that upstream version.
4. Resolve conflicts carefully in the touch-point files above.
5. Run the FRT patch guard too:

   ```bash
   scripts/check-frt-header-patch.sh
   ```

6. Run focused policy tests:

   ```bash
   go test ./service -run PolicyIncident -count=1
   go test ./model -run 'PolicyIncident|TokenDisable' -count=1
   go test ./middleware -run Policy -count=1
   go test ./controller -run Policy -count=1
   go test ./relay -run RelayTask -count=1
   ```

7. Build on a local Mac or external builder. Do not build on the small production server.
8. Back up the production database before first rollout because this patch adds the `policy_incident_events` table through app auto-migration.
9. Validate with a side container before replacing the live app container.
10. Keep Postgres and Redis running during rollout and rollback.

## Production Verification

After deployment, verify:

```bash
curl http://127.0.0.1:3000/api/status
podman ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
podman logs --since 5m new-api | grep -Ei 'panic|fatal|SQLSTATE|policy|stream scanner error'
```

Database spot check on PostgreSQL deployments:

```sql
select id,
       request_id,
       user_id,
       token_id,
       channel_id,
       upstream_key_fingerprint,
       evidence_level,
       causality,
       action_taken,
       action_result,
       created_at
from policy_incident_events
order by id desc
limit 10;
```

Expected result: the table exists after app startup. It may be empty until a real policy incident occurs.

## Rollback

This patch adds a table but does not require destructive schema migration. Rollback is normally application-container only:

1. Stop and remove the new `new-api` container.
2. Restart the preserved previous app container or rerun the previous fixed image tag.
3. Keep Postgres and Redis untouched.
4. Leave `policy_incident_events` in the database unless a separate maintenance window explicitly decides to remove it.

## Future-Agent Prompt

When working in a new Codex/AI window, start with:

> Read `AGENTS.md`, `docs/ops/frt-header-patch.md`, and `docs/ops/policy-incident-patch.md` first. Upgrade NewAPI from official upstream while preserving both fork-only patches: FRT Header Display and Policy Incident Guard. Run `scripts/check-frt-header-patch.sh` plus focused policy tests before build or deploy.

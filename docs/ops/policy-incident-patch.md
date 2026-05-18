# Policy Incident Guard Patch

## Purpose

This fork intentionally adds a production safety guard for high-confidence upstream policy incidents. When an upstream NewAPI-like provider reports safety-policy failures such as `cyber_policy` or a permanently disabled upstream API key, the fork must stop retry fan-out, isolate the affected upstream key/channel, and decide whether client-token action is allowed from the incident causality.

The goal is to avoid replaying the same risky request across additional channels while keeping enough append-only audit evidence for administrator review. Client tokens may be blocked only when the incident is attributable to the client request, not when the only proven cause is an upstream key ban or permanent-disable state.

## Behavior Contract

- Detect high-confidence policy incident errors from normal relay and task relay paths.
- Mark the request context as no-retry after detection.
- Classify incident causality before taking client-token actions.
- Set short-lived breaker state for the upstream key when Redis is available.
- Set client-token breaker state and persistently disable the implicated client token only when causality is `client_policy_request` and `ClientTokenActionAllowed` is true.
- Never set client-token breaker state and never persistently disable the client token when causality is `upstream_key_encountered`; record `token_breaker_skipped`, `token_db_disable_skipped`, and `client_attribution_missing` instead.
- Auto-disable the implicated upstream channel/key through the existing channel status update path.
- Insert an append-only `policy_incident_events` audit record.
- Store `causality` and `client_token_action_allowed` so operators can audit why client-token action was or was not taken.
- Store upstream keys only as fingerprints and redact sensitive text from metadata.
- Notify the root user once per locked incident.
- Do not store raw request prompts or raw upstream keys in the incident event.
- Do not restart Postgres or Redis just for this patch.

## Causality Rules

| Causality | Meaning | Client token action | Upstream action | Required audit markers |
| --- | --- | --- | --- | --- |
| `client_policy_request` | The upstream policy error is attributable to the client's request, such as a clear `cyber_policy` request-policy hit without an upstream-key permanent-disable marker. | Allowed. Set token breaker and, if enabled by config, persistently disable the client token. | Isolate the implicated upstream channel/key. | `client_token_action_allowed=true`; action includes `token_breaker_set`; persistent disable may be `token_disabled`, `token_unchanged`, or `token_db_disable_skipped` depending on config/state. |
| `upstream_key_encountered` | The event proves the upstream key encountered a policy/permanent-disable state, such as `当前 API key 已永久禁用`; it does not prove the current client caused it. | Forbidden. Do not set token breaker and do not persistently disable the client token. | Isolate the implicated upstream channel/key. | `client_token_action_allowed=false`; action includes `token_breaker_skipped,token_db_disable_skipped`; result includes `client_attribution_missing`. |

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
- `ce78d522 fix: avoid client bans for upstream key policy incidents`

## Safe Upstream Upgrade Workflow

Do not deploy the official upstream image directly. Official images do not include this fork-only patch.

Recommended workflow:

1. Fetch official upstream.
2. Create an upgrade branch from the desired upstream version.
3. Re-apply or preserve commits `828998d1`, `91259f4a`, and `ce78d522` on top of that upstream version.
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
       metadata ->> 'client_token_action_allowed' as client_token_action_allowed,
       action_taken,
       action_result,
       created_at
from policy_incident_events
order by id desc
limit 10;
```

Expected result: the table exists after app startup. It may be empty until a real policy incident occurs. When events exist, `upstream_key_encountered` rows must show `client_token_action_allowed=false`, `token_breaker_skipped`, `token_db_disable_skipped`, and `client_attribution_missing`. `client_policy_request` rows may show client-token breaker or disable actions according to `policy_incident_setting.disable_client_token_persistently`.

## Rollback

This patch adds a table but does not require destructive schema migration. Rollback is normally application-container only:

1. Stop and remove the new `new-api` container.
2. Restart the preserved previous app container or rerun the previous fixed image tag.
3. Keep Postgres and Redis untouched.
4. Leave `policy_incident_events` in the database unless a separate maintenance window explicitly decides to remove it.

## Future-Agent Prompt

When working in a new Codex/AI window, start with:

> Read `AGENTS.md`, `docs/ops/frt-header-patch.md`, and `docs/ops/policy-incident-patch.md` first. Upgrade NewAPI from official upstream while preserving fork-only patches: FRT Header Display and the causality-aware Policy Incident Guard. Preserve commits `828998d1`, `91259f4a`, and `ce78d522`; never disable client tokens for `upstream_key_encountered` incidents. Run `scripts/check-frt-header-patch.sh` plus focused policy tests before build or deploy.

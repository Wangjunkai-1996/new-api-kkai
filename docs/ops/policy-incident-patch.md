# Policy Incident Guard Patch

## Purpose

This fork adds a safety guard for high-confidence upstream policy incidents. It stops retry fan-out, classifies causality before client-token action, and records one append-only database audit event.

## Behavior Contract

- Detect high-confidence incidents in normal and task relay paths and mark the request as no-retry.
- Classify `client_policy_request`, `upstream_key_encountered`, and `ambiguous_mixed_attribution` before taking action.
- Only `client_policy_request` may set the client-token breaker or use the optional persistent token/user disable.
- Persistent client disable and upstream isolation both default off.
- When persistent disable is enabled, token/user changes and the audit event commit in one main-database transaction; caches invalidate after commit.
- Mixed or upstream-only attribution never disables the client token or user.
- Upstream isolation uses the fixed reason `policy_incident_upstream_isolated`; no upstream free-form error text enters status fields.
- Persist the append-only `policy_incident_events` event before root notification.
- Store upstream keys only as SHA-256 fingerprints. Store a fixed internal error code/message, never provider text.

## Database-Only Audit

The application does not write Policy Incident files. There is no evidence directory, cleanup job, retention setting, file lock, marker, or `policy_incident_evidence_outcomes` table.

`policy_incident_events.metadata` is enforced at the model boundary and accepts only:

- `case_id`: generated `policy-<unix_millis>-<16 lowercase hex>` identifier
- `request_body_sha256`: complete lowercase SHA-256, when body streaming succeeds
- `request_body_bytes`: non-negative byte count paired with the digest
- `client_token_action_allowed`: boolean

Unknown fields, free-form strings, nested values, incomplete digest pairs, and invalid types are rejected. Causality remains in the typed `causality` column. The request body is streamed only to calculate its digest and byte count; it is never materialized or persisted by this subsystem.

Legacy disk files are ignored. Application startup and request handling do not inspect, move, or delete them. Operators may back up and remove a former policy evidence directory in a separate maintenance action after deciding whether it must be retained. `NEW_API_POLICY_EVIDENCE_DIR` has no runtime effect and should be removed from deployment configuration.

Policy settings are published as one immutable snapshot. Single and bulk option updates persist before publishing. Removed evidence options are rejected and are not persisted.

## Causality Rules

| Causality | Client action | Upstream action |
| --- | --- | --- |
| `client_policy_request` | Token breaker allowed; persistent token/user disable only when explicitly enabled. | Isolation only when explicitly enabled. |
| `upstream_key_encountered` | Never disable or break the client token/user. | Isolation only when explicitly enabled. |
| `ambiguous_mixed_attribution` | Never disable or break the client token/user. | Isolation only when explicitly enabled. |

## Patch Touch Points

- `controller/relay.go`: relay retry handling
- `middleware/auth.go`: disabled-token and breaker enforcement
- `middleware/distributor.go`: channel/key context
- `model/channel.go`: fixed-reason upstream isolation
- `model/main.go`: `PolicyIncidentEvent` auto-migration
- `model/policy_incident_event.go`: append-only audit and strict metadata whitelist
- `model/token.go`: persistent client-token disable
- `relay/relay_task.go`: task relay handling
- `service/policy_incident.go`: classification, actions, audit, and notification
- `service/policy_incident_audit.go`: in-memory case ID plus streaming request digest only; no disk I/O
- `setting/operation_setting/policy_incident_setting.go`: atomic safety-setting snapshot

Original patch commits on this fork:

- `828998d1 fix: guard upstream cyber policy incidents`
- `91259f4a fix: enforce policy incident client token ban`
- `ce78d522 fix: avoid client bans for upstream key policy incidents`

## Verification

```bash
go test ./service -run PolicyIncident -count=1
go test ./model -run 'PolicyIncident|TokenDisable' -count=1
go test ./setting/operation_setting -run PolicyIncident -count=1
go test ./middleware -run Policy -count=1
go test ./controller -run Policy -count=1
go test ./relay -run RelayTask -count=1
go test -race ./service ./model ./setting/operation_setting -run PolicyIncident -count=1
go vet ./...
scripts/check-frt-header-patch.sh
```

## Production Verification

Check recent events:

```sql
select id,
       request_id,
       user_id,
       token_id,
       channel_id,
       upstream_key_fingerprint,
       causality,
       metadata ->> 'case_id' as case_id,
       metadata ->> 'request_body_sha256' as request_body_sha256,
       metadata ->> 'request_body_bytes' as request_body_bytes,
       metadata ->> 'client_token_action_allowed' as client_token_action_allowed,
       action_taken,
       action_result,
       created_at
from policy_incident_events
order by id desc
limit 10;
```

With default settings, rows must show upstream breaker/isolation skipped with `config_disabled`. Mixed and upstream-only incidents must show client actions skipped. Metadata must contain no keys outside the four documented fields.

## Rollback

Rollback is application-container only. Keep Postgres and Redis running and leave `policy_incident_events` intact unless a separate maintenance window explicitly decides otherwise. Legacy disk files are outside application rollback and remain operator-managed.

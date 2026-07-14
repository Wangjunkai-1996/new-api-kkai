# KKAI Policy Incident Verification

This note records the accepted Policy Incident Guard behavior on the pinned
upstream baseline.

## Ownership

- NewAPI is the only writer for incidents, token/user/channel status, and the
  durable outbox.
- Local relay and task-relay policy errors call the same `RiskActionService`
  used by the signed Redis Stream consumer.
- Edge detection remains a separate pending workstream. It may publish signed
  events but never receives business-database credentials.
- Cache invalidation and root notification run after commit through the outbox.

## Detection And Evidence

- Exact client-policy markers, upstream-key state markers, and mixed evidence
  are classified separately.
- Detection stops normal and task retry fan-out before another channel is
  selected.
- Incidents store request/user/token/channel identifiers, fingerprints, rule
  version, the error-evidence digest, and request-body digest/size.
- Raw request bodies, client tokens, and upstream keys are never stored in the
  incident or outbox.
- Reusing an event ID with a different normalized input is rejected as an
  idempotency conflict.
- Token and channel actions require the locked database row to match the event
  ID/owner and current key fingerprint; a stale or mismatched fingerprint rolls
  back the complete transaction.

## Action Policy

| Causality | Default decision | Durable action |
| --- | --- | --- |
| `client_token` | `disable` | Disable the matching current-fingerprint token and non-privileged user in the incident transaction |
| `upstream_key` | `reject` | Record only; shared channel remains enabled |
| `ambiguous` | `reject` | Record only; token, user, and channel remain enabled |

An upstream channel may be disabled only when a signed event explicitly sets
`upstream_action_allowed=true`. Confirmed evidence alone is insufficient.
Official generic channel auto-ban is skipped for classified policy incidents so
it cannot bypass this rule.

## Public Contract

- Client-attributed policy errors return HTTP 403 with
  `request blocked by policy` and code `policy_blocked`.
- Upstream-key and ambiguous incidents return HTTP 503 with
  `upstream unavailable` and code `upstream_unavailable`.
- OpenAI, Claude, realtime, and task response paths share the same
  classification and redaction behavior.
- Policy text, credentials, provider links, advertisements, sensitive params,
  metadata, and unsafe task data are not returned to clients.
- OpenAI-compatible responses include the incident case ID in safe metadata;
  all protocols retain the normal request ID correlation in server logs.

## Structure

Fork-owned policy files are split by responsibility: classification, guard
orchestration, decision policy, input normalization, transaction records,
state mutations, public response mapping, and redaction. No new file exceeds
250 lines. The model mutation layer uses the shared cross-database
`lockForUpdate` helper.

## Verification Commands

```bash
go test ./model ./service ./controller
go test ./pkg/kkaimigrate ./service \
  -run 'Test(.*Migration.*|.*Risk.*|.*Policy.*|.*Outbox.*)' -count=1
go test -race ./model ./service ./controller \
  -run 'Test(.*KKAI.*|.*Risk.*|.*Policy.*|.*Public.*)'
```

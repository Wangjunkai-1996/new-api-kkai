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
| `client_token` | `reject` | Record the incident and apply a 60-second key cooldown; never disable the token, user, or channel |
| `upstream_key` | `reject` | Record only; shared channel remains enabled |
| `ambiguous` | `reject` | Record only; token, user, and channel remain enabled |

Every `upstream_policy` event is non-disabling. Durable disable flags are rejected
at both the decision and transaction-input boundaries. Confirmed bearer-token Cyber
incidents receive only a fixed 60-second cooldown after token and channel identity
validation. Official generic channel auto-ban is skipped for classified policy
incidents so it cannot bypass this rule.

## Public Contract

- A client-attributed Cyber warning is recognized when a concrete upstream HTTP
  status accompanies the exact structured `cyber_policy` code. Before response
  headers are committed, the triggering request returns HTTP 403 with code
  `request_policy_warning`, warns against jailbreak/abuse, and is not retried.
  If an SSE response or WebSocket upgrade has already started, HTTP status is
  immutable; the relay terminates the active stream with the equivalent
  `request_policy_warning` error frame instead.
- `upstream_key` and `ambiguous` incidents return HTTP 503 with code
  `upstream_unavailable` and a generic service-unavailable message.
- Local sensitive-word matches return HTTP 400 with code `prompt_blocked`; only
  the current request is stopped and no cumulative cooldown is recorded.
- Existing ordinary rate-limit and cooldown behavior is unchanged.
- OpenAI, Claude, realtime, and task response paths share the same
  classification and redaction behavior.
- Policy text, credentials, provider links, advertisements, sensitive params,
  metadata, and unsafe task data are not returned to clients.
- OpenAI-compatible responses include the incident case ID in safe metadata;
  all protocols retain the normal request ID correlation in server logs.

## API Key Cooldown

- Cooldown scope is derived only from the authenticated positive `token_id`.
  Redis keys use a versioned, domain-separated HMAC. User ID, channel identity,
  IP address, request content, message history, and custom conversation headers
  are never part of the scope.
- An identity-validated `client_token` Cyber incident records a fixed 60-second
  cooldown, even if a later audit/outbox write fails. Every later confirmed
  incident resets the cooldown to 60 seconds; it never escalates automatically
  and never disables the user or key.
- `CyberPolicyKeyCooldownEnabled` controls only the 60-second cooldown and is
  enabled by default. Disabling it immediately bypasses cooldown reads and writes;
  Cyber requests are still rejected with HTTP 403 and incidents are still audited.
- The same incident event is idempotent. Requests made while blocked do not create
  incidents or extend the cooldown.
- Cooldown checks run after token authentication and before request rate limiting
  or channel distribution. A blocked request returns HTTP 429, code
  `key_cooldown`, and an integer `Retry-After` containing the actual remaining
  seconds. It does not select a channel, reach a provider, or pre-consume quota.
- Redis Lua performs check and record transitions atomically using Redis time so
  all instances observe one state. Redis unavailability is logged and fails closed
  with a local one-minute emergency cooldown.
  No cooldown state is written to business database tables, and Redis stores no
  raw token, provider key, request body, credential, or raw event ID.
- Signed asynchronous Risk Stream events are audit inputs and do not establish
  request-local cooldown state. The synchronous relay guard owns Cyber cooldowns
  after validating the authenticated bearer token and selected upstream key.

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

# CC Switch One-Time Ticket Blocker

## Current decision — 2026-11-03

The user accepted the existing CC Switch import protocol for the default
console's Usage Guide and existing API Key import entry. These user-triggered
flows may put the API key in the local `ccswitch://` URI, subject to the
credential-handling requirements in
[Usage Guide development, section 2.2](usage-guide-development.md#22-cc-switch-协议决策).
This is a scoped product decision accepting the residual risk of the browser
and operating-system protocol handoff, not evidence that credential leakage
has occurred or that the handoff is risk-free.

One-time ticket exchange remains a possible future enhancement. It is not a
prerequisite for this module. The historical contract and client limitation
below are retained as rationale; they must not be used to block the accepted
standard-protocol implementation or to claim ticket support has shipped.
No application code, client capability, deployment or security test is changed
or completed by this documentation decision.

## Historical Required Contract (superseded for the scoped flow above)

KKAI may only provide one-click CC Switch imports when all of the following are
true:

- the deep link contains a random, short-lived ticket and no API key or
  reversibly encoded API key;
- the ticket expires within 60 seconds;
- the CC Switch client exchanges it over HTTPS exactly once;
- replay, expiry, ownership mismatch, and malformed payloads are rejected;
- ticket values and exchanged credentials are excluded from application and
  proxy logs.

## Verified Client Limitation

CC Switch commit `c8b0d60c2d796c214ed76a2461494d3bac06094c` documents a
`configUrl` provider parameter, but its current implementation cannot consume
it. In `src-tauri/src/deeplink/provider.rs`, `parse_and_merge_config` returns
`Remote config URL is not yet supported. Use inline config instead.` whenever
`config_url` is present.

The supported alternatives are not safe for this contract:

- `apiKey` places the reusable key directly in the custom URI;
- inline `config` only base64-encodes the reusable key in the URI;
- a short-lived ticket used as the provider API key becomes unusable after
  redemption and cannot update the key stored by CC Switch;
- a long-lived ticket or relay credential is another reusable secret and does
  not satisfy single-use semantics.

The production fork's legacy usage-script patch therefore must not be ported:
it places the same raw key in both `apiKey` and `usageApiKey`.

## Future Ticket Implementation Conditions

CC Switch must first implement remote provider configuration retrieval for
`configUrl` or an equivalent credential-exchange field. The client behavior
must be covered by tests proving one fetch, HTTPS-only redirects, bounded
response size, timeout handling, and no persistence of the ticket after the
provider configuration is saved.

Once that client contract is released and a ticket implementation is requested,
NewAPI can add a fork-owned ticket service backed by Redis atomic consume
semantics, plus builders that put only the exchange URL and ticket in the deep
link. Ticket support is not complete. The scoped standard-protocol flow above
is allowed independently and must not be reported as ticket support.

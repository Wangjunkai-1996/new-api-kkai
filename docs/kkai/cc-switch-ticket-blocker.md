# CC Switch One-Time Ticket Blocker

## Required Contract

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

## Unblock Conditions

CC Switch must first implement remote provider configuration retrieval for
`configUrl` or an equivalent credential-exchange field. The client behavior
must be covered by tests proving one fetch, HTTPS-only redirects, bounded
response size, timeout handling, and no persistence of the ticket after the
provider configuration is saved.

Once that client contract is released, NewAPI can add a fork-owned ticket
service backed by Redis atomic consume semantics, plus default/classic builders
that put only the exchange URL and ticket in the deep link. Until then, the
existing upstream import remains unchanged and this manifest item is blocked,
not complete.

# KKAI Internal Attribution

NewAPI attaches authenticated request attribution only when the final upstream
origin is explicitly allowlisted. Private address ranges, localhost, and DNS
suffixes are not trusted implicitly.

## Sender Configuration

Both variables must be set together:

- `KKAI_ATTRIBUTION_ORIGINS`: comma-separated absolute origins.
- `KKAI_ATTRIBUTION_SECRET`: a shared secret of at least 32 bytes.

Example:

```text
KKAI_ATTRIBUTION_ORIGINS=https://guard.internal.example:8443,wss://guard.internal.example:9443
KKAI_ATTRIBUTION_SECRET=<random secret with at least 32 bytes>
```

Origins are matched by normalized scheme, host, and effective port. Paths,
queries, fragments, and user information are forbidden in allowlist entries.
`https` and `wss` are distinct schemes. An omitted default port is equivalent
to its explicit form, such as `https://host` and `https://host:443`.

When both variables are unset, attribution is disabled and any forged
attribution headers are still removed. A partial configuration, invalid origin,
or weak secret stops NewAPI during startup.

## Signed Envelope

The envelope contains request ID, numeric user/token/channel identifiers,
multi-key index, model, source, version, Unix timestamp, random nonce, and an
HMAC-SHA256 signature. It never contains the token name, API key, request body,
or upstream credential.

The signature binds the HTTP method, exact origin, escaped path, raw query, and
all attribution claims. A `Host` override that differs from the allowlisted URL
origin is rejected.

## Receiver Contract

Receivers must use `pkg/kkaiattribution.Verifier` or implement the same
canonical envelope. The provided `NonceStore` must reserve each nonce atomically
until the signed envelope leaves its accepted time window. A process-local map
is insufficient when more than one receiver instance is running.

Every redirect strips all current and legacy attribution headers before the
next request. Redirected requests are not re-signed, so an allowlisted service
must expose its final endpoint directly.

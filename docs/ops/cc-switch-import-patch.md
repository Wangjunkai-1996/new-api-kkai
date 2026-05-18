# KKAI CC Switch One-Click Import Patch

This fork carries a KKAI/Kkrich-only patch for the token table's CC Switch one-click import.

## Purpose

Official upstream New API imports a provider into CC Switch with endpoint, API key, model, homepage, and enabled state only. That is not enough for KKAI because CC Switch will not automatically display token balance unless the import deep link also includes usage-query configuration.

The KKAI patch makes new CC Switch imports work as expected:

- Provider name defaults to `KKAI`.
- Provider endpoint is `https://api.kkrich.ltd/v1`.
- CC Switch receives a usage script during import.
- The usage script queries the imported token's own usage through `/api/usage/token/`.
- Users do not need to generate or paste a New API access token.
- Users do not need to know their New API user ID.

## Files

Keep the behavior in both frontend variants:

```text
web/classic/src/components/table/tokens/modals/CCSwitchModal.jsx
web/default/src/features/keys/components/dialogs/cc-switch-dialog.tsx
```

## Required Deep Link Fields

The generated `ccswitch://v1/import` URL must include the normal provider fields plus:

```text
usageEnabled=true
usageScript=<base64 JavaScript usage script>
usageBaseUrl=<server root address, such as https://kkrich.ltd>
usageApiKey=<the imported sk-token>
usageAutoInterval=30
```

Do not use the CC Switch built-in NewAPI template for one-click token import. That template queries `/api/user/self` and requires a New API user access token plus user ID, which ordinary users may not have generated.

## Usage Script Contract

The usage script must:

- Send `GET {{baseUrl}}/api/usage/token/`.
- Authenticate with `Authorization: Bearer {{apiKey}}`.
- Parse `response.data.total_available`, `response.data.total_granted`, `response.data.total_used`, and `response.data.unlimited_quota`.
- Convert quota units using `500000` quota per displayed unit.
- Handle unlimited tokens without treating negative `total_available` as a failure.

## Upgrade Guard

Before building or deploying after an upstream merge, verify:

```bash
grep -R "CC_SWITCH_TOKEN_USAGE_SCRIPT" \
  web/classic/src/components/table/tokens/modals/CCSwitchModal.jsx \
  web/default/src/features/keys/components/dialogs/cc-switch-dialog.tsx

grep -R "https://api.kkrich.ltd/v1" \
  web/classic/src/components/table/tokens/modals/CCSwitchModal.jsx \
  web/default/src/features/keys/components/dialogs/cc-switch-dialog.tsx

grep -R "/api/usage/token/" \
  web/classic/src/components/table/tokens/modals/CCSwitchModal.jsx \
  web/default/src/features/keys/components/dialogs/cc-switch-dialog.tsx
```

Also run:

```bash
git diff --check
```

If a frontend build is part of the rollout, build from `production/kkrich` or a verified temporary release branch that is merged back into `production/kkrich` before the rollout is considered complete.

## Rollback Notes

Reverting this patch does not affect existing New API tokens or server-side usage APIs. It only changes future CC Switch one-click imports. Existing CC Switch providers keep whatever usage script was already imported into the local CC Switch database.

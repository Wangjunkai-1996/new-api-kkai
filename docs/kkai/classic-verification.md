# KKAI Classic Frontend Verification

## Restored Scope

The pinned upstream classic frontend remains the implementation baseline. KKAI
adds only the following owned behavior:

- the production build resolves the shared `date-fns` dependency consistently;
- model pricing starts with recharge prices visible;
- resetting pricing filters restores that same recharge-price default.

The pricing default and reset policy live under `web/classic/src/kkai`. Existing
upstream components contain only the imports and call sites needed to attach
that policy.

## Deliberately Excluded

The legacy production fork changed `CCSwitchModal.jsx` to place the reusable
provider key in both `apiKey` and `usageApiKey` deep-link parameters. That code
is not migrated. CC Switch remains a separate blocked capability governed by
`cc-switch-ticket-blocker.md`; it is not part of the completed classic frontend
status.

The legacy one-line change in `helpers/utils.jsx` only changed the pricing reset
default. Its behavior is restored by the KKAI-owned pricing policy instead of
editing that large upstream helper file.

## Acceptance

- fork-owned classic changes pass the changed-file Prettier check against the
  pinned upstream baseline;
- `bun run build` produces the production bundle;
- the full fork quality gate checks formatting and rebuilds classic;
- no raw or reversibly encoded API key is added to a CC Switch URI.

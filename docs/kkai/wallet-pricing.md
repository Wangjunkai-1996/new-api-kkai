# KKAI Wallet And Pricing

## Ownership

Waffo and Waffo Pancake payment processing are provided by the pinned upstream
baseline. KKAI does not fork their controller, webhook, settlement, model, or
configuration code. Payment-provider validation and callback behavior remain
owned by upstream.

The KKAI-owned surface is limited to:

- preferring Invitations statistics in the wallet when that service is
  available, while failing closed to the upstream referral card;
- linking the wallet rebate card to the dedicated Invitations workflow;
- defaulting public model prices to the recharge-price view in both frontend
  variants while preserving an explicit user opt-out.

## Code Boundaries

- `web/default/src/features/kkai-wallet` owns wallet adaptation.
- `web/default/src/features/kkai-pricing` owns default-frontend pricing policy.
- `web/classic/src/kkai` owns classic-frontend pricing policy.
- Upstream wallet and pricing files contain only the minimum imports and call
  sites required to attach those policies.

The Invitations status and statistics requests reuse the feature query keys
from the dedicated Invitations UI. If the status request fails, the service is
disabled, or statistics are unavailable, the upstream referral card remains
the authoritative fallback.

## Verification

- The disabled-query regression test confirms that a disabled Invitations
  statistics query cannot leave the wallet on a permanent loading skeleton.
- Pricing tests confirm that recharge display is the default and that an
  explicit `false` route value remains effective.
- Default typecheck/lint/build and the classic production build cover the two
  integration points.
- `classic-verification.md` records the legacy-diff audit and keeps the unsafe
  CC Switch deep-link customization outside this completed scope.

# KKAI Invitations Balance Adjustments

`POST /api/internal/balance-adjustments` is the only supported path for the
KKAI Invitations service to change a NewAPI user balance. The Invitations
service must never write `users.quota` directly.

The endpoint accepts only `Authorization: Bearer <secret>` using a shared
`INVITATIONS_INTERNAL_SECRET` of at least 32 non-whitespace characters. User
sessions, access tokens, and `New-API-User` headers are ignored. Read-only
standby nodes reject the operation with `503`.

Credits use reason `invitation_reward` with a positive delta. Reversals use
`invitation_reward_reversal`, a negative delta, and the exact original
operation ID. A reversal must target the same user and exactly negate the
original credit.

The operation ID is the idempotency key. The first committed application
returns `201`; an identical replay returns the stored result with `200` and
`replayed: true`. Reusing the operation ID with a different normalized payload
returns `409`. The user balance and immutable
`kkai_internal_balance_adjustments` row commit in one transaction.

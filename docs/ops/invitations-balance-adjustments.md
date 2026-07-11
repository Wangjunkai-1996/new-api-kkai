# Invitations Balance Adjustments

`POST /api/internal/balance-adjustments` is the only supported path for the
Invitations service to change a NewAPI user's quota. The Invitations service
must not write `users.quota` directly.

## Authentication

Set the same random secret of at least 32 characters in both services:

```text
INVITATIONS_INTERNAL_SECRET=<random secret>
```

Send it only as `Authorization: Bearer <secret>`. This endpoint ignores
`New-API-User` and all session or user access tokens.

## Request

```json
{
  "operation_id": "01J...stable-operation-id",
  "user_id": 123,
  "delta": 500000,
  "reason": "invitation_reward",
  "metadata": {
    "rebate_record_id": 42,
    "payout_id": 99
  }
}
```

`reason` is either `invitation_reward` with a positive `delta`, or
`invitation_reward_reversal` with a negative `delta`. A reversal must include
`metadata.original_operation_id`, must target the same user, and must exactly
reverse the original credit. Metadata accepts only `rebate_record_id`,
`payout_id`, and `original_operation_id`.

The first application returns `201`. Replaying the same normalized payload and
`operation_id` returns the stored result with `200` and `replayed: true`. Reusing
an operation ID with another payload returns `409`. Balance changes and the
idempotency ledger row commit in one database transaction.

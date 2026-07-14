/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  createApproveAndPayCommand,
  createRebatePayoutCommand,
  payoutCommandHeaders,
} from './payout-command'

describe('invitation payout commands', () => {
  test('keeps a stable key for every retry of the same operation', () => {
    const command = createRebatePayoutCommand(42, 'pay', 'fixed-nonce')

    assert.equal(command.idempotencyKey, 'rebate-payout:42:pay:fixed-nonce')
    assert.deepEqual(payoutCommandHeaders(command), {
      'Idempotency-Key': 'rebate-payout:42:pay:fixed-nonce',
      'X-KKAI-Payout-Action': 'pay',
    })
    assert.deepEqual(
      payoutCommandHeaders(command),
      payoutCommandHeaders(command)
    )
  })

  test('separates approve-and-pay operations by request scope', () => {
    const first = createApproveAndPayCommand('request:7', 'first-nonce')
    const second = createApproveAndPayCommand('batch', 'second-nonce')

    assert.equal(
      first.idempotencyKey,
      'rebate-approve-pay:request:7:first-nonce'
    )
    assert.equal(second.idempotencyKey, 'rebate-approve-pay:batch:second-nonce')
  })
})

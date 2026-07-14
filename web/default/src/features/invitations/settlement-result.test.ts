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
  summarizeApproveAndPay,
  summarizePayoutAction,
} from './settlement-result'

describe('invitation settlement results', () => {
  test('reports queued payout work as pending instead of success', () => {
    assert.deepEqual(summarizePayoutAction({ outcome: 'pending' }), {
      level: 'warning',
      messageKey: 'Payout is pending',
    })
  })

  test('reports partial approve-and-pay failures explicitly', () => {
    assert.deepEqual(
      summarizeApproveAndPay({
        paidCount: 2,
        alreadyPaidCount: 1,
        pendingCount: 0,
        failedCount: 1,
      }),
      {
        level: 'error',
        messageKey:
          'Settlement: {{paid}} paid, {{alreadyPaid}} already paid, {{pending}} pending, {{failed}} failed',
        values: { paid: 2, alreadyPaid: 1, pending: 0, failed: 1 },
      }
    )
  })

  test('keeps mixed paid and queued records visible as pending', () => {
    assert.equal(
      summarizeApproveAndPay({
        paidCount: 2,
        alreadyPaidCount: 0,
        pendingCount: 3,
        failedCount: 0,
      }).level,
      'warning'
    )
  })
})

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
  derivePayoutActions,
  registrationRewardAction,
  isApproveAndPayEligible,
} from './admin-action-policy'
import type { RebateRecord } from './types'

const record = (payoutStatus?: string): RebateRecord => ({
  id: 1,
  inviterId: 2,
  source: 'order',
  orderType: 'voucher',
  orderAmount: 100,
  rebateAmount: 10,
  status: 'approved',
  payoutStatus,
  unlockRequired: false,
  createdAt: '2026-07-14T00:00:00Z',
  updatedAt: '2026-07-14T00:00:00Z',
})

describe('invitation admin action policy', () => {
  test('does not infer pay permission from an unknown payout state', () => {
    assert.deepEqual(derivePayoutActions(record(), 'order'), {
      pay: false,
      reverse: false,
      revokeSignup: false,
    })
  })

  test('keeps order payout and signup reward actions separated', () => {
    assert.deepEqual(derivePayoutActions(record('failed'), 'order'), {
      pay: true,
      reverse: false,
      revokeSignup: false,
    })
    assert.deepEqual(
      derivePayoutActions(
        { ...record('paid'), source: 'signup', orderType: 'invite_inviter' },
        'signup'
      ),
      { pay: false, reverse: false, revokeSignup: true }
    )
  })

  test('prevents completed signup rewards from being revoked', () => {
    assert.equal(registrationRewardAction(false, null), 'generate')
    assert.equal(registrationRewardAction(true, 'pending'), 'revoke')
    assert.equal(registrationRewardAction(true, 'completed'), null)
  })

  test('allows both pending and approved requests into payout settlement', () => {
    assert.equal(isApproveAndPayEligible('pending'), true)
    assert.equal(isApproveAndPayEligible('approved'), true)
    assert.equal(isApproveAndPayEligible('completed'), false)
  })
})

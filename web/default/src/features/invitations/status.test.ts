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

import { describe, expect, test } from 'vitest'

import {
  DISABLED_INVITATION_STATUS,
  deriveInvitationAccess,
  normalizeInvitationStatus,
} from './status'

describe('invitation feature status', () => {
  test('fails closed when the external service response is unavailable', () => {
    assert.deepEqual(
      normalizeInvitationStatus(undefined),
      DISABLED_INVITATION_STATUS
    )
    assert.deepEqual(
      normalizeInvitationStatus({ success: false }),
      DISABLED_INVITATION_STATUS
    )
  })

  test('keeps admin access separate from the user-facing feature switch', () => {
    const status = normalizeInvitationStatus({
      success: true,
      data: {
        available: true,
        userInvitationRebateEnabled: false,
        orderRebateEnabled: true,
        invitationSignupRewardEnabled: true,
        rebateToBalanceEnabled: true,
      },
    })

    assert.deepEqual(deriveInvitationAccess(status), {
      salesVisible: false,
      salesActive: false,
      adminVisible: true,
      userVisible: false,
      rebateRecordsVisible: false,
      rebateManagementVisible: false,
    })
  })

  test('only exposes rebate actions enabled by the external service', () => {
    const status = normalizeInvitationStatus({
      success: true,
      data: {
        available: true,
        userInvitationRebateEnabled: true,
        orderRebateEnabled: false,
        invitationSignupRewardEnabled: true,
        rebateToBalanceEnabled: true,
      },
    })

    assert.deepEqual(deriveInvitationAccess(status), {
      salesVisible: false,
      salesActive: false,
      adminVisible: true,
      userVisible: true,
      rebateRecordsVisible: true,
      rebateManagementVisible: true,
    })
  })
})

test('sales access stays independent from ordinary transfer settings and preserves inactive history', () => {
  const status = {
    ...DISABLED_INVITATION_STATUS,
    available: true,
    orderRebateEnabled: true,
  }
  expect(
    deriveInvitationAccess(status, { registered: true, active: true })
  ).toMatchObject({
    userVisible: true,
    salesVisible: true,
    salesActive: true,
    rebateManagementVisible: true,
  })
  expect(
    deriveInvitationAccess(status, { registered: true, active: false })
  ).toMatchObject({
    userVisible: true,
    salesVisible: true,
    salesActive: false,
    rebateManagementVisible: true,
  })
  expect(
    deriveInvitationAccess(DISABLED_INVITATION_STATUS, {
      registered: true,
      active: true,
    })
  ).toMatchObject({
    userVisible: false,
    salesVisible: false,
    salesActive: false,
  })
})

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
  normalizeInvitationSystemConfig,
  prepareInvitationSystemConfig,
} from './system-config'

describe('invitation system config compatibility', () => {
  test('expands the legacy signup reward amount into both recipient fields', () => {
    const config = normalizeInvitationSystemConfig({
      invitationSignupRewardAmount: 250,
    })

    assert.equal(config.invitationSignupInviterRewardAmount, 250)
    assert.equal(config.invitationSignupInviteeRewardAmount, 250)
  })

  test('derives the legacy aggregate field before saving', () => {
    const config = normalizeInvitationSystemConfig({})
    config.invitationSignupInviterRewardAmount = 200
    config.invitationSignupInviteeRewardAmount = 350

    assert.equal(
      prepareInvitationSystemConfig(config).invitationSignupRewardAmount,
      350
    )
  })
})

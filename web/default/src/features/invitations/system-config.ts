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
import type { SystemConfig } from './types'

export const INVITATION_SYSTEM_CONFIG_DEFAULTS: SystemConfig = {
  minRebateRequestAmount: 0,
  rebateRequestFrequencyDays: 0,
  userInvitationRebateEnabled: false,
  orderRebateEnabled: false,
  invitationSignupRewardEnabled: false,
  invitationSignupRewardAmount: 0,
  invitationSignupInviterRewardAmount: 0,
  invitationSignupInviteeRewardAmount: 0,
  invitationSignupRewardReviewRequired: false,
  invitationSignupInviterRewardRequiresPaidOrder: false,
  invitationSignupInviteeRewardRequiresPaidOrder: false,
  rebateToBalanceEnabled: false,
}

export const normalizeInvitationSystemConfig = (
  value: Partial<SystemConfig>
): SystemConfig => {
  const legacySignupReward = value.invitationSignupRewardAmount ?? 0
  return {
    ...INVITATION_SYSTEM_CONFIG_DEFAULTS,
    ...value,
    invitationSignupInviterRewardAmount:
      value.invitationSignupInviterRewardAmount ?? legacySignupReward,
    invitationSignupInviteeRewardAmount:
      value.invitationSignupInviteeRewardAmount ?? legacySignupReward,
  }
}

export const prepareInvitationSystemConfig = (
  value: SystemConfig
): SystemConfig => ({
  ...value,
  invitationSignupRewardAmount:
    value.invitationSignupInviterRewardAmount === 0 &&
    value.invitationSignupInviteeRewardAmount === 0
      ? 0
      : Math.max(
          value.invitationSignupInviterRewardAmount,
          value.invitationSignupInviteeRewardAmount
        ),
})

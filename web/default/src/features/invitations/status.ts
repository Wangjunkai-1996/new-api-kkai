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
import type { ApiResponse, InvitationFeatureStatus } from './types'

export const DISABLED_INVITATION_STATUS: InvitationFeatureStatus = {
  available: false,
  userInvitationRebateEnabled: false,
  orderRebateEnabled: false,
  invitationSignupRewardEnabled: false,
  rebateToBalanceEnabled: false,
}

export const normalizeInvitationStatus = (
  response?: ApiResponse<InvitationFeatureStatus>
): InvitationFeatureStatus => {
  if (!response?.success || response.data?.available !== true) {
    return DISABLED_INVITATION_STATUS
  }

  return {
    available: true,
    userInvitationRebateEnabled:
      response.data.userInvitationRebateEnabled === true,
    orderRebateEnabled: response.data.orderRebateEnabled === true,
    invitationSignupRewardEnabled:
      response.data.invitationSignupRewardEnabled === true,
    rebateToBalanceEnabled: response.data.rebateToBalanceEnabled === true,
  }
}

export const deriveInvitationAccess = (status: InvitationFeatureStatus) => {
  const userVisible = status.available && status.userInvitationRebateEnabled
  const rebateRecordsVisible =
    userVisible &&
    (status.orderRebateEnabled || status.invitationSignupRewardEnabled)

  return {
    adminVisible: status.available,
    userVisible,
    rebateRecordsVisible,
    rebateManagementVisible:
      rebateRecordsVisible && status.rebateToBalanceEnabled,
  }
}

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
export type InvitationFeatureStatus = {
  available: boolean
  userInvitationRebateEnabled: boolean
  orderRebateEnabled: boolean
  invitationSignupRewardEnabled: boolean
  rebateToBalanceEnabled: boolean
}

export type InvitationStats = {
  invitationCode: string
  invitedCount: number
  totalRebate: number
  completedRebate: number
  pendingRebate: number
  confirmingRebate: number
}

export type RebateStatus =
  | 'initializing'
  | 'pending'
  | 'requested'
  | 'approved'
  | 'completed'
  | 'rejected'

export type RebateRequestStatus =
  | 'pending'
  | 'approved'
  | 'rejected'
  | 'completed'

export type RebateRecord = {
  id: number
  inviterId: number
  inviterName?: string | null
  userId?: number | null
  userName?: string | null
  source?: 'order' | 'signup' | string
  orderType: string
  orderId?: number | string | null
  orderNo?: string | null
  orderAmount: number
  rebateAmount: number
  rebateRatio?: number | null
  status: RebateStatus
  displayStatus?: string
  payoutStatus?: string | null
  payout?: RebatePayoutStatus | null
  payoutId?: number | string | null
  canPay?: boolean | null
  canReverse?: boolean | null
  payoutPaidAt?: string | null
  payoutReversedAt?: string | null
  unlockRequired: boolean
  unlockedAt?: string | null
  effectiveAt?: string
  createdAt: string
  updatedAt: string
}

export type RebatePayoutStatus = {
  id?: number | string | null
  recordId?: number | null
  status?: string | null
  amount?: number | null
  canPay?: boolean | null
  canReverse?: boolean | null
  paidAt?: string | null
  reversedAt?: string | null
  updatedAt?: string | null
  message?: string | null
}

export type RebateRequest = {
  id: number
  userId: number
  amount: number
  status: RebateRequestStatus
  rebateRecordIds: number[]
  createdAt: string
  updatedAt: string
  approvedAt?: string
  completedAt?: string
  rejectedAt?: string
  reviewNote?: string
  rejectReason?: string
}

export type RebateRequestFormData = {
  amount: number
  rebateRecordIds: number[]
}

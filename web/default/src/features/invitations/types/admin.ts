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
import type { RebateRequest } from './user'

export const ALL_USER_GROUP = '__all__'

export type RebateRule = {
  id: number
  user_group: string
  rule_type: 'subscription' | 'topup'
  rebate_rate: string
  created_at: number
  updated_at: number
}

export type RebateRuleFormData = Omit<
  RebateRule,
  'id' | 'created_at' | 'updated_at'
>

export type UserGroup = {
  name: string
  user_count: number
}

export type SystemConfig = {
  minRebateRequestAmount: number
  rebateRequestFrequencyDays: number
  userInvitationRebateEnabled: boolean
  orderRebateEnabled: boolean
  invitationSignupRewardEnabled: boolean
  invitationSignupRewardAmount: number
  invitationSignupInviterRewardAmount: number
  invitationSignupInviteeRewardAmount: number
  invitationSignupRewardReviewRequired: boolean
  invitationSignupInviterRewardRequiresPaidOrder: boolean
  invitationSignupInviteeRewardRequiresPaidOrder: boolean
  rebateToBalanceEnabled: boolean
}

export type RebateRequestAdmin = RebateRequest & {
  userName: string
  source?: 'order' | 'signup' | string
  payoutManaged?: boolean | null
  usesPayoutService?: boolean | null
  includesOrderRebates?: boolean | null
}

export type RebateStats = {
  total_rebate: number
  completed_rebate: number
  pending_rebate: number
  requested_rebate: number
  approved_rebate: number
  total_invitations: number
}

export type AdminRebateOrderRecord = {
  orderType: 'topup' | 'subscription'
  orderId: number
  inviterId: number
  inviterName?: string | null
  inviteeId: number
  inviteeName?: string | null
  userGroup: string
  orderAmount: number
  rebateAmount: number
  rebateRatio?: number | null
  status: string
  ruleMissing: boolean
  localRebateRecordId?: number | null
  orderTime: string
  effectiveAt: string
  initializationEndsAt: string
  adminAdjusted: boolean
  canModify: boolean
  canClose: boolean
  canReopen: boolean
  canEndInitialization: boolean
  canExtendInitialization: boolean
}

export type AdminInvitationRegistration = {
  id: number
  inviterId: number
  inviterName?: string | null
  inviteeId: number
  inviteeName?: string | null
  userGroup: string
  invitedAt: string
  totalRewardAmount: number
  inviterRewardGenerated: boolean
  inviterRewardRecordId?: number | null
  inviterRewardAmount: number
  inviterRewardStatus?: string | null
  inviteeRewardGenerated: boolean
  inviteeRewardRecordId?: number | null
  inviteeRewardAmount: number
  inviteeRewardStatus?: string | null
}

export type RewardOperationResponse = {
  generated?: boolean
  revoked?: boolean
  recordId?: number | null
}

export type RebateOrderBatchResponse = { updated: number }

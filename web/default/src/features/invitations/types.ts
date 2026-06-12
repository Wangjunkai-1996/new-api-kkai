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

// 邀请统计
export interface InvitationStats {
  invitationCode: string
  invitedCount: number
  totalRebate: number
  completedRebate: number
  pendingRebate: number
}

// 邀请返利外挂后端可用状态
export interface InvitationFeatureStatus {
  available: boolean
  userInvitationRebateEnabled: boolean
  orderRebateEnabled: boolean
  invitationSignupRewardEnabled: boolean
  rebateToBalanceEnabled: boolean
}

// 返利记录状态
export type RebateStatus =
  | 'pending'
  | 'requested'
  | 'approved'
  | 'completed'
  | 'rejected'

// 订单类型
export type OrderType =
  | 'topup'
  | 'subscription'
  | 'invite_inviter'
  | 'invite_invitee'
  | 'other'

// 管理员返利记录展示状态
export type AdminRebateOrderStatus =
  | 'initializing'
  | 'estimated'
  | 'claimable'
  | 'paid'
  | 'closed'

export type RebateDisplayStatus =
  | 'estimated'
  | 'claimable'
  | 'paid'
  | 'waiting_unlock'

// 返利记录：用户侧不暴露被邀请客户身份
export interface RebateRecord {
  id: number
  inviterId: number
  orderType: OrderType
  orderAmount: number
  rebateAmount: number
  rebateRatio?: number | null
  status: RebateStatus
  displayStatus?: RebateDisplayStatus
  unlockRequired: boolean
  unlockedAt?: string | null
  effectiveAt?: string
  createdAt: string
  updatedAt: string
}

// 返利到余额申请状态
export type RebateRequestStatus =
  | 'pending'
  | 'approved'
  | 'rejected'
  | 'completed'

// 返利到余额申请
export interface RebateRequest {
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

// API 响应格式
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

// 分页参数
export interface PaginationParams {
  page?: number
  pageSize?: number
}

// 分页响应
export interface PaginatedResponse<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

// 返利到余额申请表单数据
export interface RebateRequestFormData {
  amount: number
  rebateRecordIds: number[]
}

// ============================================================================
// 管理员类型定义
// ============================================================================

export const ALL_USER_GROUP = '__all__'

// 返利规则
export interface RebateRule {
  id: number
  user_group: string
  rule_type: 'subscription' | 'topup'
  rebate_rate: string
  created_at: number
  updated_at: number
}

// 用户组
export interface UserGroup {
  name: string
  user_count: number
}

// 系统配置
export interface SystemConfig {
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

// 管理员返利申请
export interface RebateRequestAdmin extends RebateRequest {
  userName: string
}

// 返利统计
export interface RebateStats {
  total_rebate: number
  completed_rebate: number
  pending_rebate: number
  requested_rebate: number
  approved_rebate: number
  total_invitations: number
}

// 管理员返利订单记录
export interface AdminRebateOrderRecord {
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
  status: AdminRebateOrderStatus
  ruleMissing: boolean
  localRebateRecordId?: number | null
  localRebateStatus?: string | null
  orderTime: string
  effectiveAt: string
  scanStartedAt: string
  initializationEndsAt: string
  finalizedAt?: string | null
  closedAt?: string | null
  adminAdjusted: boolean
  canModify: boolean
  canClose: boolean
  canReopen: boolean
  canEndInitialization: boolean
  canExtendInitialization: boolean
}

// 管理员邀请注册记录
export interface AdminInvitationRegistration {
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

export interface InvitationRegistrationRewardResponse {
  generated: boolean
  recordId?: number | null
}

export interface InvitationRegistrationRewardRevokeResponse {
  revoked: boolean
  recordId?: number | null
}

export interface RebateOrderRecordBatchResponse {
  updated: number
}

export interface UpdateRebateOrderRecordsData {
  recordIds: number[]
  rebateAmount?: number
  rebateRatio?: number
}

export interface RebateOrderRecordIdsData {
  recordIds: number[]
}

export interface ExtendRebateInitializationData {
  recordIds: number[]
  initializationEndsAt: number
}

// 返利规则表单数据
export interface RebateRuleFormData {
  user_group: string
  rule_type: 'subscription' | 'topup'
  rebate_rate: string
}

// 返利审批操作数据
export interface RebateApprovalData {
  note?: string
}

export interface RebateRejectionData {
  reason: string
  note?: string
}

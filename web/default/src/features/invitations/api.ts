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
import { api, type ApiRequestConfig } from '@/lib/api'
import type {
  ApiResponse,
  InvitationFeatureStatus,
  InvitationStats,
  RebateRecord,
  RebateRequest,
  RebateRequestFormData,
  PaginationParams,
  PaginatedResponse,
  RebateStatus,
  AdminRebateOrderRecord,
  AdminInvitationRegistration,
  RebateOrderRecordBatchResponse,
  InvitationRegistrationRewardResponse,
  InvitationRegistrationRewardRevokeResponse,
  UpdateRebateOrderRecordsData,
  RebateOrderRecordIdsData,
  ExtendRebateInitializationData,
} from './types'

const BASE_PATH = '/invitations/api/invitations'

// ============================================================================
// 用户 API
// ============================================================================

/**
 * 获取邀请返利外挂后端状态。失败时调用方应默认隐藏界面。
 */
export async function getInvitationFeatureStatus(): Promise<
  ApiResponse<InvitationFeatureStatus>
> {
  const res = await api.get(`${BASE_PATH}/status`, {
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  return res.data
}

/**
 * 获取我的邀请码和统计信息
 */
export async function getMyCode(
  config?: ApiRequestConfig
): Promise<ApiResponse<InvitationStats>> {
  const res = await api.get(`${BASE_PATH}/my-code`, config)
  return res.data
}

/**
 * 获取返利记录列表
 */
export async function getRebateRecords(
  params?: PaginationParams & { status?: RebateStatus }
): Promise<ApiResponse<PaginatedResponse<RebateRecord>>> {
  const res = await api.get(`${BASE_PATH}/rebate-records`, { params })
  return res.data
}

/**
 * 获取可申请到余额的返利金额（尚未提交申请的返利总额）
 */
export async function getAvailableRebates(): Promise<
  ApiResponse<{ amount: number; recordIds: number[] }>
> {
  const res = await api.get(`${BASE_PATH}/available-rebates`)
  return res.data
}

/**
 * 申请返利到余额
 */
export async function requestRebate(
  data: RebateRequestFormData
): Promise<ApiResponse<RebateRequest>> {
  const res = await api.post(`${BASE_PATH}/rebate-requests`, data, {
    skipErrorHandler: true,
  })
  return res.data
}

/**
 * 获取我的返利到余额申请列表
 */
export async function getMyRebateRequests(
  params?: PaginationParams
): Promise<ApiResponse<PaginatedResponse<RebateRequest>>> {
  const res = await api.get(`${BASE_PATH}/rebate-requests`, { params })
  return res.data
}

// ============================================================================
// 管理员 API
// ============================================================================

const ADMIN_BASE_PATH = '/invitations/api/admin'

/**
 * 获取返利规则列表
 */
export async function getRebateRules(): Promise<
  ApiResponse<import('./types').RebateRule[]>
> {
  const res = await api.get(`${ADMIN_BASE_PATH}/rebate-rules`)
  return res.data
}

/**
 * 创建返利规则
 */
export async function createRebateRule(
  data: import('./types').RebateRuleFormData
): Promise<ApiResponse<import('./types').RebateRule>> {
  const res = await api.post(`${ADMIN_BASE_PATH}/rebate-rules`, data, {
    skipErrorHandler: true,
  })
  return res.data
}

/**
 * 更新返利规则
 */
export async function updateRebateRule(
  id: number,
  data: import('./types').RebateRuleFormData
): Promise<ApiResponse<import('./types').RebateRule>> {
  const res = await api.put(`${ADMIN_BASE_PATH}/rebate-rules/${id}`, data, {
    skipErrorHandler: true,
  })
  return res.data
}

/**
 * 删除返利规则
 */
export async function deleteRebateRule(id: number): Promise<ApiResponse<void>> {
  const res = await api.delete(`${ADMIN_BASE_PATH}/rebate-rules/${id}`, {
    skipErrorHandler: true,
  })
  return res.data
}

/**
 * 获取用户组列表
 */
export async function getUserGroups(): Promise<
  ApiResponse<import('./types').UserGroup[]>
> {
  const res = await api.get(`${ADMIN_BASE_PATH}/user-groups`)
  return res.data
}

/**
 * 获取系统配置
 */
export async function getSystemConfig(): Promise<
  ApiResponse<import('./types').SystemConfig>
> {
  const res = await api.get(`${ADMIN_BASE_PATH}/system-config`)
  return res.data
}

/**
 * 更新系统配置
 */
export async function updateSystemConfig(
  data: import('./types').SystemConfig
): Promise<ApiResponse<import('./types').SystemConfig>> {
  const res = await api.put(`${ADMIN_BASE_PATH}/system-config`, data, {
    skipErrorHandler: true,
  })
  return res.data
}

/**
 * 获取返利申请列表（管理员）
 */
export async function getRebateRequests(
  params?: PaginationParams & { status?: import('./types').RebateRequestStatus }
): Promise<
  ApiResponse<PaginatedResponse<import('./types').RebateRequestAdmin>>
> {
  const res = await api.get(`${ADMIN_BASE_PATH}/rebate-requests`, {
    params,
  })
  return res.data
}

/**
 * 通过返利申请
 */
export async function approveRebateRequest(
  id: number,
  data?: import('./types').RebateApprovalData
): Promise<ApiResponse<import('./types').RebateRequestAdmin>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/rebate-requests/${id}/approve`,
    data,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 拒绝返利申请
 */
export async function rejectRebateRequest(
  id: number,
  data: import('./types').RebateRejectionData
): Promise<ApiResponse<import('./types').RebateRequestAdmin>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/rebate-requests/${id}/reject`,
    data,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 撤回返利申请审核
 */
export async function resetRebateRequestReview(
  id: number
): Promise<ApiResponse<import('./types').RebateRequestAdmin>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/rebate-requests/${id}/reset`,
    undefined,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 标记返利处理完成
 */
export async function completeRebateRequest(
  id: number
): Promise<ApiResponse<import('./types').RebateRequestAdmin>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/rebate-requests/${id}/complete`,
    undefined,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 撤回返利处理完成状态
 */
export async function undoCompleteRebateRequest(
  id: number
): Promise<ApiResponse<import('./types').RebateRequestAdmin>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/rebate-requests/${id}/undo-complete`,
    undefined,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 获取返利统计
 */
export async function getRebateStats(): Promise<
  ApiResponse<import('./types').RebateStats>
> {
  const res = await api.get(`${ADMIN_BASE_PATH}/rebate-stats`)
  return res.data
}

/**
 * 获取管理员返利记录列表（不受用户侧邀请返利开关影响）
 */
export async function getAdminRebateRecords(
  params?: PaginationParams & {
    status?: RebateStatus
    source?: 'order' | 'signup'
  }
): Promise<ApiResponse<PaginatedResponse<RebateRecord>>> {
  const res = await api.get(`${ADMIN_BASE_PATH}/rebate-records`, {
    params,
  })
  return res.data
}

/**
 * 按返利记录撤回邀请注册奖励
 */
export async function revokeAdminSignupRewardRecord(
  id: number
): Promise<ApiResponse<InvitationRegistrationRewardRevokeResponse>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/rebate-records/${id}/revoke-signup-reward`,
    undefined,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 获取管理员返利订单记录
 */
export async function getAdminRebateOrderRecords(
  params?: PaginationParams & { orderType?: 'topup' | 'subscription' }
): Promise<ApiResponse<PaginatedResponse<AdminRebateOrderRecord>>> {
  const res = await api.get(`${ADMIN_BASE_PATH}/rebate-order-records`, {
    params,
  })
  return res.data
}

/**
 * 批量修改返利订单记录的返利金额或比例
 */
export async function updateAdminRebateOrderRecords(
  data: UpdateRebateOrderRecordsData
): Promise<ApiResponse<RebateOrderRecordBatchResponse>> {
  const res = await api.patch(`${ADMIN_BASE_PATH}/rebate-order-records`, data, {
    skipErrorHandler: true,
  })
  return res.data
}

/**
 * 批量关闭返利订单记录
 */
export async function closeAdminRebateOrderRecords(
  data: RebateOrderRecordIdsData
): Promise<ApiResponse<RebateOrderRecordBatchResponse>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/rebate-order-records/close`,
    data,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 批量开启已关闭的返利订单记录
 */
export async function reopenAdminRebateOrderRecords(
  data: RebateOrderRecordIdsData
): Promise<ApiResponse<RebateOrderRecordBatchResponse>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/rebate-order-records/reopen`,
    data,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 提前结束返利订单初始化
 */
export async function endAdminRebateOrderInitialization(
  data: RebateOrderRecordIdsData
): Promise<ApiResponse<RebateOrderRecordBatchResponse>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/rebate-order-records/end-initialization`,
    data,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 延长返利订单初始化时间
 */
export async function extendAdminRebateOrderInitialization(
  data: ExtendRebateInitializationData
): Promise<ApiResponse<RebateOrderRecordBatchResponse>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/rebate-order-records/extend-initialization`,
    data,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 获取管理员邀请注册列表
 */
export async function getAdminInvitationRegistrations(
  params?: PaginationParams
): Promise<ApiResponse<PaginatedResponse<AdminInvitationRegistration>>> {
  const res = await api.get(`${ADMIN_BASE_PATH}/invitation-registrations`, {
    params,
  })
  return res.data
}

/**
 * 为邀请人生成邀请注册奖励
 */
export async function generateAdminInvitationInviterReward(
  id: number
): Promise<ApiResponse<InvitationRegistrationRewardResponse>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/invitation-registrations/${id}/inviter-reward`,
    undefined,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 为被邀请人生成邀请注册奖励
 */
export async function generateAdminInvitationInviteeReward(
  id: number
): Promise<ApiResponse<InvitationRegistrationRewardResponse>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/invitation-registrations/${id}/invitee-reward`,
    undefined,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 撤回邀请人邀请注册奖励
 */
export async function revokeAdminInvitationInviterReward(
  id: number
): Promise<ApiResponse<InvitationRegistrationRewardRevokeResponse>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/invitation-registrations/${id}/inviter-reward/revoke`,
    undefined,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 撤回被邀请人邀请注册奖励
 */
export async function revokeAdminInvitationInviteeReward(
  id: number
): Promise<ApiResponse<InvitationRegistrationRewardRevokeResponse>> {
  const res = await api.post(
    `${ADMIN_BASE_PATH}/invitation-registrations/${id}/invitee-reward/revoke`,
    undefined,
    { skipErrorHandler: true }
  )
  return res.data
}

/**
 * 获取用户返利详情
 */
export async function getUserRebateDetails(
  userId: number,
  params?: PaginationParams
): Promise<ApiResponse<PaginatedResponse<RebateRecord>>> {
  const res = await api.get(
    `${ADMIN_BASE_PATH}/users/${userId}/rebate-details`,
    { params }
  )
  return res.data
}

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
import { api } from '@/lib/api'

import { payoutCommandHeaders } from '../payout-command'
import type {
  AdminInvitationRegistration,
  AdminRebateOrderRecord,
  ApiResponse,
  PaginatedResponse,
  PaginationParams,
  RebateOrderBatchResponse,
  RebatePayoutActionResponse,
  RebatePayoutCommand,
  RebatePayoutStatus,
  RebateRecord,
  RebateStatus,
  RewardOperationResponse,
} from '../types'
import { INVITATIONS_ADMIN_PATH } from './paths'

export const getAdminRebateRecords = async (
  params?: PaginationParams & {
    status?: RebateStatus
    source?: 'order' | 'signup'
  }
): Promise<ApiResponse<PaginatedResponse<RebateRecord>>> => {
  const response = await api.get(`${INVITATIONS_ADMIN_PATH}/rebate-records`, {
    params,
  })
  return response.data
}

export const getAdminRebatePayoutStatus = async (
  recordId: number
): Promise<ApiResponse<RebatePayoutStatus>> => {
  const response = await api.get(
    `${INVITATIONS_ADMIN_PATH}/rebate-payouts/${recordId}/status`,
    { skipErrorHandler: true }
  )
  return response.data
}

export const executeAdminRebatePayout = async (
  command: RebatePayoutCommand
): Promise<ApiResponse<RebatePayoutActionResponse>> => {
  const response = await api.post(
    `${INVITATIONS_ADMIN_PATH}/rebate-payouts/${command.recordId}/${command.action}`,
    undefined,
    {
      headers: payoutCommandHeaders(command),
      skipErrorHandler: true,
    }
  )
  return response.data
}

export const revokeAdminSignupRewardRecord = async (
  recordId: number
): Promise<ApiResponse<RewardOperationResponse>> => {
  const response = await api.post(
    `${INVITATIONS_ADMIN_PATH}/rebate-records/${recordId}/revoke-signup-reward`,
    undefined,
    { skipErrorHandler: true }
  )
  return response.data
}

export const getAdminRebateOrderRecords = async (
  params?: PaginationParams & { orderType?: 'topup' | 'subscription' }
): Promise<ApiResponse<PaginatedResponse<AdminRebateOrderRecord>>> => {
  const response = await api.get(
    `${INVITATIONS_ADMIN_PATH}/rebate-order-records`,
    { params }
  )
  return response.data
}

const postOrderRecordAction = async (
  action: 'close' | 'reopen' | 'end-initialization',
  recordIds: number[]
): Promise<ApiResponse<RebateOrderBatchResponse>> => {
  const response = await api.post(
    `${INVITATIONS_ADMIN_PATH}/rebate-order-records/${action}`,
    { recordIds },
    { skipErrorHandler: true }
  )
  return response.data
}

export const closeAdminRebateOrderRecords = (recordIds: number[]) =>
  postOrderRecordAction('close', recordIds)

export const reopenAdminRebateOrderRecords = (recordIds: number[]) =>
  postOrderRecordAction('reopen', recordIds)

export const endAdminRebateOrderInitialization = (recordIds: number[]) =>
  postOrderRecordAction('end-initialization', recordIds)

export const updateAdminRebateOrderRecords = async (data: {
  recordIds: number[]
  rebateAmount?: number
  rebateRatio?: number
}): Promise<ApiResponse<RebateOrderBatchResponse>> => {
  const response = await api.patch(
    `${INVITATIONS_ADMIN_PATH}/rebate-order-records`,
    data,
    { skipErrorHandler: true }
  )
  return response.data
}

export const extendAdminRebateOrderInitialization = async (
  recordIds: number[],
  initializationEndsAt: number
): Promise<ApiResponse<RebateOrderBatchResponse>> => {
  const response = await api.post(
    `${INVITATIONS_ADMIN_PATH}/rebate-order-records/extend-initialization`,
    { recordIds, initializationEndsAt },
    { skipErrorHandler: true }
  )
  return response.data
}

export const getAdminInvitationRegistrations = async (
  params?: PaginationParams
): Promise<ApiResponse<PaginatedResponse<AdminInvitationRegistration>>> => {
  const response = await api.get(
    `${INVITATIONS_ADMIN_PATH}/invitation-registrations`,
    { params }
  )
  return response.data
}

export const runRegistrationRewardAction = async (
  registrationId: number,
  recipient: 'inviter' | 'invitee',
  action: 'generate' | 'revoke'
): Promise<ApiResponse<RewardOperationResponse>> => {
  const suffix = action === 'revoke' ? '/revoke' : ''
  const response = await api.post(
    `${INVITATIONS_ADMIN_PATH}/invitation-registrations/${registrationId}/${recipient}-reward${suffix}`,
    undefined,
    { skipErrorHandler: true }
  )
  return response.data
}

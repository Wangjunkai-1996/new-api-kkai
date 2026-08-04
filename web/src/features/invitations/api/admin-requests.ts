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

import type {
  ApiResponse,
  ApproveAndPayCommand,
  ApproveAndPayResponse,
  BatchApproveAndPayResponse,
  PaginatedResponse,
  PaginationParams,
  RebateRequestAdmin,
  RebateRequestStatus,
} from '../types'
import { INVITATIONS_ADMIN_PATH } from './paths'

export const getAdminRebateRequests = async (
  params?: PaginationParams & { status?: RebateRequestStatus }
): Promise<ApiResponse<PaginatedResponse<RebateRequestAdmin>>> => {
  const response = await api.get(`${INVITATIONS_ADMIN_PATH}/rebate-requests`, {
    params,
  })
  return response.data
}

export const approveRebateRequest = async (
  id: number,
  note?: string
): Promise<ApiResponse<RebateRequestAdmin>> => {
  const response = await api.post(
    `${INVITATIONS_ADMIN_PATH}/rebate-requests/${id}/approve`,
    { note },
    { skipErrorHandler: true }
  )
  return response.data
}

export const approveAndPayRebateRequest = async (
  id: number,
  command: ApproveAndPayCommand,
  note?: string
): Promise<ApiResponse<ApproveAndPayResponse>> => {
  const response = await api.post(
    `${INVITATIONS_ADMIN_PATH}/rebate-requests/${id}/approve-and-pay`,
    { note },
    {
      headers: { 'Idempotency-Key': command.idempotencyKey },
      skipErrorHandler: true,
    }
  )
  return response.data
}

export const batchApproveAndPayRebateRequests = async (
  requestIds: number[],
  command: ApproveAndPayCommand,
  note?: string
): Promise<ApiResponse<BatchApproveAndPayResponse>> => {
  const response = await api.post(
    `${INVITATIONS_ADMIN_PATH}/rebate-requests/approve-and-pay-batch`,
    { requestIds, note },
    {
      headers: { 'Idempotency-Key': command.idempotencyKey },
      skipErrorHandler: true,
    }
  )
  return response.data
}

export const rejectRebateRequest = async (
  id: number,
  reason: string,
  note?: string
): Promise<ApiResponse<RebateRequestAdmin>> => {
  const response = await api.post(
    `${INVITATIONS_ADMIN_PATH}/rebate-requests/${id}/reject`,
    { reason, note },
    { skipErrorHandler: true }
  )
  return response.data
}

const postRequestAction = async (
  id: number,
  action: 'reset' | 'complete' | 'undo-complete'
): Promise<ApiResponse<RebateRequestAdmin>> => {
  const response = await api.post(
    `${INVITATIONS_ADMIN_PATH}/rebate-requests/${id}/${action}`,
    undefined,
    { skipErrorHandler: true }
  )
  return response.data
}

export const resetRebateRequestReview = (id: number) =>
  postRequestAction(id, 'reset')

export const completeRebateRequest = (id: number) =>
  postRequestAction(id, 'complete')

export const undoCompleteRebateRequest = (id: number) =>
  postRequestAction(id, 'undo-complete')

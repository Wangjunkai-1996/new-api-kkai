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
  InvitationStats,
  PaginatedResponse,
  PaginationParams,
  RebateRecord,
  RebateRequest,
  RebateRequestFormData,
  RebateStatus,
} from '../types'
import { INVITATIONS_PATH } from './paths'

export const getMyInvitationCode = async (
  config?: ApiRequestConfig
): Promise<ApiResponse<InvitationStats>> => {
  const response = await api.get(`${INVITATIONS_PATH}/my-code`, config)
  return response.data
}

export const getRebateRecords = async (
  params?: PaginationParams & { status?: RebateStatus }
): Promise<ApiResponse<PaginatedResponse<RebateRecord>>> => {
  const response = await api.get(`${INVITATIONS_PATH}/rebate-records`, {
    params,
  })
  return response.data
}

export const getAvailableRebates = async (): Promise<
  ApiResponse<{ amount: number; recordIds: number[] }>
> => {
  const response = await api.get(`${INVITATIONS_PATH}/available-rebates`)
  return response.data
}

export const requestRebateTransfer = async (
  data: RebateRequestFormData
): Promise<ApiResponse<RebateRequest>> => {
  const response = await api.post(`${INVITATIONS_PATH}/rebate-requests`, data, {
    skipErrorHandler: true,
  })
  return response.data
}

export const getMyRebateRequests = async (
  params?: PaginationParams
): Promise<ApiResponse<PaginatedResponse<RebateRequest>>> => {
  const response = await api.get(`${INVITATIONS_PATH}/rebate-requests`, {
    params,
  })
  return response.data
}

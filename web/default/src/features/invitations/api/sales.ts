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
  PaginationParams,
  PaginatedResponse,
  SalesAccountsResponse,
  SalesCustomers,
  SalesRecharge,
} from '../types'
import { INVITATIONS_ADMIN_PATH, INVITATIONS_PATH } from './paths'

export const getSalesProfile = async () => {
  const response = await api.get<
    ApiResponse<{ active: boolean; registered: boolean }>
  >(`${INVITATIONS_PATH}/sales-profile`, { skipErrorHandler: true })
  return response.data
}

export const getSalesAccounts = async (params: PaginationParams) => {
  const response = await api.get<ApiResponse<SalesAccountsResponse>>(
    `${INVITATIONS_ADMIN_PATH}/sales-accounts`,
    { params }
  )
  return response.data
}

export const updateSalesAccount = async (input: {
  userId: number
  active: boolean
  note?: string
}) => {
  const response = await api.put<
    ApiResponse<{ active: boolean; registered: boolean }>
  >(
    `${INVITATIONS_ADMIN_PATH}/sales-accounts/${input.userId}`,
    { active: input.active, note: input.note },
    { skipErrorHandler: true }
  )
  return response.data
}

export const getSalesCustomers = async (params: PaginationParams) => {
  const response = await api.get<ApiResponse<SalesCustomers>>(
    `${INVITATIONS_PATH}/sales-customers`,
    { params }
  )
  return response.data
}

export const getSalesCustomerRecharges = async (
  userId: number,
  params: PaginationParams
) => {
  const response = await api.get<ApiResponse<PaginatedResponse<SalesRecharge>>>(
    `${INVITATIONS_PATH}/sales-customers/${userId}/recharges`,
    { params }
  )
  return response.data
}

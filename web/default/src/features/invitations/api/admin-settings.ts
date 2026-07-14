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
  RebateRule,
  RebateRuleFormData,
  RebateStats,
  SystemConfig,
  UserGroup,
} from '../types'
import { INVITATIONS_ADMIN_PATH } from './paths'

export const getRebateRules = async (): Promise<ApiResponse<RebateRule[]>> => {
  const response = await api.get(`${INVITATIONS_ADMIN_PATH}/rebate-rules`)
  return response.data
}

export const createRebateRule = async (
  data: RebateRuleFormData
): Promise<ApiResponse<RebateRule>> => {
  const response = await api.post(
    `${INVITATIONS_ADMIN_PATH}/rebate-rules`,
    data,
    { skipErrorHandler: true }
  )
  return response.data
}

export const updateRebateRule = async (
  id: number,
  data: RebateRuleFormData
): Promise<ApiResponse<RebateRule>> => {
  const response = await api.put(
    `${INVITATIONS_ADMIN_PATH}/rebate-rules/${id}`,
    data,
    { skipErrorHandler: true }
  )
  return response.data
}

export const deleteRebateRule = async (
  id: number
): Promise<ApiResponse<void>> => {
  const response = await api.delete(
    `${INVITATIONS_ADMIN_PATH}/rebate-rules/${id}`,
    { skipErrorHandler: true }
  )
  return response.data
}

export const getInvitationUserGroups = async (): Promise<
  ApiResponse<UserGroup[]>
> => {
  const response = await api.get(`${INVITATIONS_ADMIN_PATH}/user-groups`)
  return response.data
}

export const getInvitationSystemConfig = async (): Promise<
  ApiResponse<SystemConfig>
> => {
  const response = await api.get(`${INVITATIONS_ADMIN_PATH}/system-config`)
  return response.data
}

export const updateInvitationSystemConfig = async (
  data: SystemConfig
): Promise<ApiResponse<SystemConfig>> => {
  const response = await api.put(
    `${INVITATIONS_ADMIN_PATH}/system-config`,
    data,
    { skipErrorHandler: true }
  )
  return response.data
}

export const getInvitationRebateStats = async (): Promise<
  ApiResponse<RebateStats>
> => {
  const response = await api.get(`${INVITATIONS_ADMIN_PATH}/rebate-stats`)
  return response.data
}

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
import type { ApiResponse, PaginatedResponse } from '../types'

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === 'object'

const isNonNegativeInteger = (value: unknown): value is number =>
  typeof value === 'number' && Number.isSafeInteger(value) && value >= 0

const isPositiveInteger = (value: unknown): value is number =>
  typeof value === 'number' && Number.isSafeInteger(value) && value > 0

export const requireInvitationData = <T>(
  response: ApiResponse<T>,
  fallbackMessage: string
): T => {
  if (
    response.success &&
    response.data !== undefined &&
    response.data !== null
  ) {
    return response.data
  }
  throw new Error(response.message || fallbackMessage)
}

export const requireInvitationPage = <T>(
  response: ApiResponse<PaginatedResponse<T>>,
  fallbackMessage: string,
  itemGuard?: (value: unknown) => value is T
): PaginatedResponse<T> => {
  const data = requireInvitationData(response, fallbackMessage)
  const isValidItem = (value: unknown) =>
    itemGuard ? itemGuard(value) : isRecord(value)
  if (
    !isRecord(data) ||
    !Array.isArray(data.items) ||
    !data.items.every(isValidItem) ||
    !isNonNegativeInteger(data.total) ||
    !isPositiveInteger(data.page) ||
    !isPositiveInteger(data.pageSize)
  ) {
    throw new Error(fallbackMessage)
  }
  return data
}

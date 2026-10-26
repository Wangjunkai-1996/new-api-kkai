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
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  MembershipBillingReview,
  MembershipCatalog,
  MembershipCheckout,
  MembershipOrder,
  MembershipOrderRequest,
  MembershipSelf,
} from './types'

interface MembershipResponse<T> {
  success: boolean
  message?: string
  data: T
}

export async function getMembershipCatalog(): Promise<MembershipCatalog> {
  const response = await api.get<MembershipResponse<MembershipCatalog>>(
    '/api/membership/catalog'
  )
  return requireServerSuccess(response.data).data
}

export async function getMembershipSelf(): Promise<MembershipSelf> {
  const response = await api.get<MembershipResponse<MembershipSelf>>(
    '/api/membership/self'
  )
  return requireServerSuccess(response.data).data
}

export async function getMembershipOrders(
  admin = false
): Promise<MembershipOrder[]> {
  const path = admin ? '/api/membership/admin/orders' : '/api/membership/orders'
  const response = await api.get<MembershipResponse<MembershipOrder[]>>(path)
  return requireServerSuccess(response.data).data
}

export async function getMembershipOrder(
  tradeNo: string
): Promise<MembershipOrder> {
  const response = await api.get<MembershipResponse<MembershipOrder>>(
    `/api/membership/orders/${encodeURIComponent(tradeNo)}`
  )
  return requireServerSuccess(response.data).data
}

export async function createMembershipOrder(
  request: MembershipOrderRequest
): Promise<MembershipCheckout> {
  const response = await api.post<MembershipResponse<MembershipCheckout>>(
    '/api/membership/orders',
    request
  )
  return requireServerSuccess(response.data).data
}

export async function resumeMembershipPayment(
  tradeNo: string
): Promise<MembershipCheckout> {
  const response = await api.post<MembershipResponse<MembershipCheckout>>(
    `/api/membership/orders/${encodeURIComponent(tradeNo)}/pay`
  )
  return requireServerSuccess(response.data).data
}

export async function resolveMembershipOrder(request: {
  trade_no: string
  action: 'grant' | 'dismiss'
  reason: string
  operation_id: string
}): Promise<void> {
  const { trade_no, ...data } = request
  const response = await api.post<MembershipResponse<unknown>>(
    `/api/membership/admin/orders/${encodeURIComponent(trade_no)}/resolve`,
    data
  )
  requireServerSuccess(response.data)
}

export async function getMembershipAdminConfig(): Promise<MembershipCatalog> {
  const response = await api.get<MembershipResponse<MembershipCatalog>>(
    '/api/membership/admin/config'
  )
  return requireServerSuccess(response.data).data
}

export async function publishMembershipCatalog(
  catalog: MembershipCatalog,
  expectedReleaseId: number
): Promise<MembershipCatalog> {
  const response = await api.put<MembershipResponse<MembershipCatalog>>(
    '/api/membership/admin/config',
    {
      catalog,
      expected_release_id: expectedReleaseId,
    }
  )
  return requireServerSuccess(response.data).data
}

export async function getMembershipBillingReviews(): Promise<
  MembershipBillingReview[]
> {
  const response = await api.get<MembershipResponse<MembershipBillingReview[]>>(
    '/api/membership/admin/billing-reviews'
  )
  return requireServerSuccess(response.data).data
}
export async function resolveMembershipBillingReview(request: {
  request_id: string
  operation_id: string
  action: 'settle' | 'release'
  actual_quota: number
  reason: string
}): Promise<void> {
  const { request_id, ...body } = request
  const response = await api.post<MembershipResponse<unknown>>(
    `/api/membership/admin/billing-reviews/${encodeURIComponent(request_id)}/resolve`,
    body
  )
  requireServerSuccess(response.data)
}

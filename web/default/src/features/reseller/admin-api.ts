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

import type { ResellerWithdrawal } from './api'

export interface ResellerApplication {
  id: number
  applicant_user_id: number
  tenant_id: number
  business_name: string
  desired_domain_prefix?: string
  contact_name: string
  contact_email: string
  notes: string
  status: 'pending' | 'approved' | 'rejected'
  rejection_reason: string
  created_at: string
  domains?: Array<{ hostname: string; status: string }>
}
export interface ApplicationInput {
  business_name: string
  desired_domain_prefix: string
  contact_name: string
  contact_email: string
  notes: string
}
export interface ManagedTenant {
  id: number
  display_name: string
  status: 'active' | 'disabled' | 'provisioning'
  domains: Array<{ hostname: string; status: string }>
  activation?: {
    domain_ready: boolean
    ready: boolean
  }
}
export interface ManagedWithdrawal extends ResellerWithdrawal {
  tenant_id: number
  alipay_account: string
  alipay_name: string
  reviewed_by: number
  reviewed_at: number
  paid_at: number
  timeline: string
}
export interface WithdrawalReview {
  status: 'approved' | 'paid' | 'rejected' | 'failed'
  payment_ref: string
  note: string
}
export async function getApplications(): Promise<ResellerApplication[]> {
  const response = await api.get('/api/reseller/applications')
  return requireServerSuccess(response.data).data ?? []
}
export async function submitApplication(
  input: ApplicationInput
): Promise<ResellerApplication> {
  const response = await api.post('/api/reseller/applications', input, {
    skipErrorHandler: true,
  })
  return requireServerSuccess(response.data).data
}
export async function getManagedResellers(): Promise<{
  applications: ResellerApplication[]
  tenants: ManagedTenant[]
  root_domain?: string
}> {
  const response = await api.get('/api/admin/resellers')
  const data = requireServerSuccess(response.data).data
  const tenants: ManagedTenant[] = data.tenants ?? []
  return {
    applications: data.applications ?? [],
    tenants: tenants.map((tenant) => ({
      ...tenant,
      domains: tenant.domains ?? [],
    })),
    root_domain: data.root_domain,
  }
}
export async function reviewApplication(
  id: number,
  status: 'approved' | 'rejected',
  reason: string
): Promise<void> {
  const response = await api.patch(`/api/admin/resellers/applications/${id}`, {
    status,
    reason,
  })
  requireServerSuccess(response.data)
}
export async function updateTenantStatus(
  id: number,
  status: 'active' | 'disabled'
): Promise<void> {
  const response = await api.patch(`/api/admin/resellers/${id}/status`, {
    status,
  })
  requireServerSuccess(response.data)
}
export async function provisionDomain(
  id: number,
  hostname: string
): Promise<void> {
  const response = await api.post(`/api/admin/resellers/${id}/domain`, {
    hostname,
  })
  requireServerSuccess(response.data)
}
export async function getManagedWithdrawals(
  page: number,
  pageSize: number
): Promise<{ items: ManagedWithdrawal[]; total: number }> {
  const response = await api.get('/api/admin/resellers/withdrawals', {
    params: { p: page, page_size: pageSize },
  })
  const data = requireServerSuccess(response.data).data
  return { items: data.items ?? [], total: data.total }
}
export async function reviewWithdrawal(
  id: number,
  review: WithdrawalReview
): Promise<void> {
  const response = await api.patch(
    `/api/admin/resellers/withdrawals/${id}`,
    review
  )
  requireServerSuccess(response.data)
}

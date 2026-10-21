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
import { getCurrentHost } from '@/lib/host-scope'
import { requireServerSuccess } from '@/lib/server-error-message'

export interface ResellerProfile {
  site_name: string
  logo_url: string
  contact: string
  announcement: string
  homepage_content: string
}
export interface ResellerMe {
  tenant: { id: number; status: string; display_name: string }
  profile: ResellerProfile
  domains: Array<{ hostname: string; status: string }>
}
export interface CatalogEntry {
  sales_group: string
  model: string
  status: 'enabled' | 'disabled'
  is_default: boolean
  sort_order: number
}
export interface SupplierEntry {
  sales_group: string
  model: string
  supplier_price_minor: number
  minimum_multiplier?: string
  price_unit?: string
}
export interface GroupPrice {
  id: number
  group: string
  sales_multiplier: string
  platform_multiplier: string
  minimum_multiplier: string
  markup_percent: string
  pricing_mode: 'markup' | 'legacy'
  version: number
  inherited: boolean
}
export interface ResellerCustomer {
  id: number
  username: string
  display_name: string
  status: number
  created_at: number
  quota: number
  used_quota: number
  total_credited_quota: number
  total_refunded_quota: number
}
export interface ResellerPage<T> {
  items: T[]
  total: number
}
export interface ResellerWallet {
  pending_minor: number
  available_minor: number
  frozen_minor: number
  debt_minor: number
}
export interface ResellerLedgerEntry {
  id: number
  kind: string
  amount_minor: number
  source_reference: string
  available_at: number
  created_at: number
  sale_minor?: number
  supplier_minor_exact?: string
  payment_fee_minor?: number
}
export interface ResellerEarnings {
  wallet: ResellerWallet
  ledger: ResellerLedgerEntry[]
  ledger_total?: number
  estimated_profit_minor: number | null
  total_profit_minor?: number
  withdrawal_fee_minor?: number
}
export interface ResellerWithdrawal {
  id: number
  amount_minor: number
  fee_minor: number
  net_minor: number
  status: 'requested' | 'approved' | 'paid' | 'rejected' | 'failed'
  review_note: string
  payment_ref: string
  created_at: number
  updated_at: number
  timeline?: string
}
export interface WithdrawalInput {
  amount_minor: number
  idempotency_key: string
  alipay_account: string
  alipay_name: string
}

export function resellerQueryKey(userId: number | undefined, resource: string) {
  return ['reseller', getCurrentHost(), userId, resource] as const
}

export async function getResellerMe(): Promise<ResellerMe> {
  const response = await api.get('/api/reseller/me', { skipErrorHandler: true })
  return requireServerSuccess(response.data).data as ResellerMe
}
export async function getResellerProfile(): Promise<ResellerProfile> {
  const response = await api.get('/api/reseller/profile')
  return requireServerSuccess(response.data).data as ResellerProfile
}
export async function updateResellerProfile(
  profile: Omit<ResellerProfile, 'logo_url'>
): Promise<ResellerProfile> {
  const response = await api.patch('/api/reseller/profile', profile)
  return requireServerSuccess(response.data).data as ResellerProfile
}
export async function uploadResellerLogo(
  file: File
): Promise<{ logo_url: string }> {
  const form = new FormData()
  form.append('logo', file)
  const response = await api.post('/api/reseller/brand/logo', form)
  return requireServerSuccess(response.data).data as { logo_url: string }
}
export async function getResellerCatalog(): Promise<CatalogEntry[]> {
  const response = await api.get('/api/reseller/catalog')
  return (
    (requireServerSuccess(response.data).data as CatalogEntry[] | null) ?? []
  )
}
export async function getResellerSupply(): Promise<SupplierEntry[]> {
  const response = await api.get('/api/reseller/supply')
  return (
    (requireServerSuccess(response.data).data as SupplierEntry[] | null) ?? []
  )
}
export async function updateResellerCatalog(
  entries: CatalogEntry[]
): Promise<CatalogEntry[]> {
  const response = await api.patch('/api/reseller/catalog', { entries })
  return (
    (requireServerSuccess(response.data).data as CatalogEntry[] | null) ?? []
  )
}
export async function getResellerPrices(): Promise<GroupPrice[]> {
  const response = await api.get('/api/reseller/prices', {
    params: { scope: 'group' },
  })
  return (requireServerSuccess(response.data).data as GroupPrice[] | null) ?? []
}
export async function updateResellerPrices(
  prices: Array<
    { sales_group: string; expected_version: number } & (
      | { markup_percent: string }
      | { convert_legacy: true; expected_platform_multiplier: string }
    )
  >
): Promise<GroupPrice[]> {
  const response = await api.patch('/api/reseller/prices', { prices })
  return (requireServerSuccess(response.data).data as GroupPrice[] | null) ?? []
}
export async function getResellerCustomers(
  page = 1,
  pageSize = 20
): Promise<ResellerPage<ResellerCustomer>> {
  const response = await api.get('/api/reseller/customers', {
    params: { p: page, page_size: pageSize },
  })
  const result = requireServerSuccess(response.data)
  const items = (result.data as ResellerCustomer[] | null) ?? []
  return {
    items:
      result.total == null
        ? items.slice((page - 1) * pageSize, page * pageSize)
        : items,
    total: result.total ?? items.length,
  }
}
export async function getResellerEarnings(
  page = 1,
  pageSize = 20
): Promise<ResellerEarnings> {
  const response = await api.get('/api/reseller/earnings', {
    params: { p: page, page_size: pageSize },
  })
  const data = requireServerSuccess(response.data).data as ResellerEarnings
  const ledger = data.ledger ?? []
  return {
    ...data,
    ledger:
      data.ledger_total == null
        ? ledger.slice((page - 1) * pageSize, page * pageSize)
        : ledger,
    ledger_total: data.ledger_total ?? ledger.length,
  }
}
export async function getResellerWithdrawals(
  page = 1,
  pageSize = 20
): Promise<ResellerPage<ResellerWithdrawal>> {
  const response = await api.get('/api/reseller/withdrawals', {
    params: { p: page, page_size: pageSize },
  })
  const result = requireServerSuccess(response.data)
  const items = (result.data as ResellerWithdrawal[] | null) ?? []
  return {
    items:
      result.total == null
        ? items.slice((page - 1) * pageSize, page * pageSize)
        : items,
    total: result.total ?? items.length,
  }
}
export async function requestResellerWithdrawal(
  input: WithdrawalInput
): Promise<ResellerWithdrawal> {
  const response = await api.post('/api/reseller/withdrawals', input)
  return requireServerSuccess(response.data).data as ResellerWithdrawal
}

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
export interface PriceVersion {
  id: number
  model: string
  group: string
  supplier_price_minor: number
  sales_multiplier: string
  version: number
}
export interface ResellerCustomer {
  id: number
  username: string
  display_name: string
  status: number
  created_at: number
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
export async function getResellerPrices(): Promise<PriceVersion[]> {
  const response = await api.get('/api/reseller/prices')
  return (
    (requireServerSuccess(response.data).data as PriceVersion[] | null) ?? []
  )
}
export async function updateResellerPrices(
  prices: Array<{
    sales_group: string
    model: string
    sale_multiplier: string
    expected_version: number
  }>
): Promise<PriceVersion[]> {
  const response = await api.patch('/api/reseller/prices', { prices })
  return (
    (requireServerSuccess(response.data).data as PriceVersion[] | null) ?? []
  )
}
export async function getResellerCustomers(): Promise<ResellerCustomer[]> {
  const response = await api.get('/api/reseller/customers')
  return (
    (requireServerSuccess(response.data).data as ResellerCustomer[] | null) ??
    []
  )
}
export async function getResellerEarnings(): Promise<ResellerEarnings> {
  const response = await api.get('/api/reseller/earnings')
  const data = requireServerSuccess(response.data).data as ResellerEarnings
  return { ...data, ledger: data.ledger ?? [] }
}
export async function getResellerWithdrawals(): Promise<ResellerWithdrawal[]> {
  const response = await api.get('/api/reseller/withdrawals')
  return (
    (requireServerSuccess(response.data).data as ResellerWithdrawal[] | null) ??
    []
  )
}
export async function requestResellerWithdrawal(
  input: WithdrawalInput
): Promise<ResellerWithdrawal> {
  const response = await api.post('/api/reseller/withdrawals', input)
  return requireServerSuccess(response.data).data as ResellerWithdrawal
}

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
export type MembershipStyle = 'gold' | 'diamond' | 'king'

export interface MembershipPaymentMethod {
  provider: string
  method: string
  name: string
}

export interface MembershipTier {
  id: string
  name: string
  rank: number
  style: MembershipStyle
  price_minor: number
  currency: 'USD' | 'CNY'
  quota: number
  enabled: boolean
  discounts: Record<string, number>
  payment_products?: Record<string, string>
  eligibility?: {
    action: 'purchase' | 'upgrade' | 'current' | 'unavailable'
    reason?: string
  }
  payment_methods?: MembershipPaymentMethod[]
}

export interface MembershipGroup {
  key: string
  display_name: string
  enabled: boolean
  published: boolean
}

export interface MembershipCatalog {
  runtime_enabled?: boolean
  schema_ready?: boolean
  release_id: number
  sales_enabled: boolean
  topup_discounts_disabled?: boolean
  model_limits?: Record<string, MembershipModelLimit> | null
  tiers: MembershipTier[]
  groups: MembershipGroup[]
  group_quotes?: MembershipCatalogGroupQuote[]
}

export interface MembershipTierQuote {
  coefficient: number
  membership_ratio: number
  savings: number
}

export interface MembershipCatalogGroupQuote {
  group_key: string
  display_name: string
  description: string
  base_ratio: number
  wallet_ratio: number
  tier_quotes: Record<string, MembershipTierQuote>
}

export interface MembershipAccount {
  id: number
  user_id: number
  tier_id: string
  rank: number
  release_id: number
  amount_total: number
  amount_used: number
  amount_reserved: number
  expires_at: number
  revision: number
  status: 'active' | 'expired' | 'exhausted' | 'cancelled'
}

export interface MembershipGroupQuote {
  group_key: string
  display_name: string
  description?: string
  base_ratio?: number
  savings?: number
  coefficient: number
  wallet_ratio: number
  membership_ratio: number
  enabled: boolean
  published: boolean
}

export interface MembershipOrder {
  id: number
  trade_no: string
  tier_id: string
  tier_name: string
  status: string
  price_minor: number
  currency: string
  quota: number
  account_id?: number
  created_at: number
  paid_at?: number
  checkout_state?: 'creating' | 'ready'
  payments?: Array<{
    receipt_id: string
    status: string
    reason?: string
    amount_minor: number
    currency: string
  }>
  exception_reason?: string
  resolution_note?: string
  user_id?: number
}

export interface MembershipSelf {
  runtime_enabled?: boolean
  server_time: number
  account: MembershipAccount | null
  tier: MembershipTier | null
  discounts: MembershipGroupQuote[]
  pending_order: MembershipOrder | null
}

export interface MembershipCheckout {
  order: MembershipOrder
  trade_no: string
  status: string
  payment_url?: string
  submit_url?: string
  form_fields?: Record<string, string>
}

export interface MembershipOrderRequest {
  tier_id: string
  payment_provider: string
  payment_method: string
  release_id: number
  expected_account_id: number
  expected_revision: number
}

export interface MembershipModelLimit {
  max_input_tokens: number
  max_output_tokens: number
  max_tool_calls: number
  task_usage_limits?: Record<string, number>
  evidence: string
}

export interface MembershipBillingReview {
  request_id: string
  user_id: number
  source: string
  group_key: string
  reserved_quota: number
  actual_quota: number
  charged_quota: number
  excess_quota: number
  state: string
  snapshot?: string
  review_reason: string
  review_started_at: number
  review_resolved_at: number
}

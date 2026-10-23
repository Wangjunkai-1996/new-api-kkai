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
export interface MoneyDTO {
  amount_minor: string
  currency: string
  exponent: number
}
export type PaymentDisplayStatus =
  | 'creating'
  | 'pending'
  | 'payment_processing'
  | 'cancel_processing'
  | 'expiry_processing'
  | 'review_required'
  | 'credit_review'
  | 'success'
  | 'cancelled'
  | 'expired'
  | 'ended'
  | 'timed_out'
  | 'failed'
  | 'unavailable'
export type PaymentStatusGroup =
  | 'all'
  | 'pending'
  | 'processing'
  | 'review'
  | 'completed'
  | 'closed'
export interface PaymentPricing {
  expected_payment: MoneyDTO | null
  pricing_mode: 'fixed' | 'channel_adjusted' | 'unknown'
  payable_payment: MoneyDTO | null
  pricing_notice_code: string | null
  credited_quota: string | null
  credit_display: { amount: string; unit: string } | null
}
export interface PaymentOrder extends PaymentPricing {
  trade_no: string
  version: string
  display_status: PaymentDisplayStatus
  status_group: PaymentStatusGroup
  allowed_actions: (
    | 'resume'
    | 'cancel'
    | 'end'
    | 'reconcile'
    | 'contact_support'
    | 'acknowledge'
  )[]
  payment_provider: string
  payment_method: string
  created_at: number
  deadline_at: number | null
  deadline_source: 'provider' | 'local_policy' | 'unknown'
  close_reason: 'user_cancel' | 'timeout' | null
  closed_at: number | null
  local_end_reason?: 'user_ended' | 'timeout' | null
  local_ended_at?: number | null
  completed_at: number | null
  last_checked_at: number | null
  reason_code: string | null
  received_payment: MoneyDTO | null
  review: {
    reason_code: string
    owner_label: string
    last_checked_at: number | null
  } | null
  legacy_display: { amount: string | null; money: string | null } | null
  timeline?: { event_type: string; occurred_at: number; summary_code: string }[]
  user_id?: number
  review_owner?: string | null
  receipts?: PaymentReceipt[]
}
export interface PaymentReceipt {
  id: string
  payment: MoneyDTO
  paid_at: number | null
  received_at: number
  verification_status: string
  processing_status: string
  exception_reason: string | null
  review_owner: string | null
  evidence_source: string
}
export interface WalletState {
  flow: {
    id: string | null
    revision: string
    phase: 'idle' | 'creating' | 'order' | 'processing' | 'review' | 'result'
    order: PaymentOrder | null
  }
  can_create: boolean
  create_block_reason_code: string | null
  other_unfinished_count: number
  server_time: number
}
export interface PaymentQuoteRequest {
  payment_provider: string
  payment_method: string
  amount?: string
  product_id?: string
  waffo_pay_method_type?: string
  waffo_pay_method_name?: string
}
export interface PaymentQuote extends PaymentPricing {
  quote_token: string
  expires_at: number
  payment_provider: string
  payment_method: string
  server_time: number
}
export type PaymentCheckout =
  | { kind: 'url'; url: string }
  | {
      kind: 'form'
      method: 'POST'
      action: string
      fields: Record<string, string>
    }
export interface PaymentOrderResult {
  wallet: WalletState
  order: PaymentOrder
}
export interface PaymentHistory {
  items: PaymentOrder[]
  total: number
  counts: Record<PaymentStatusGroup, number>
  query_scope: string
  server_time: number
  today_payment_total?: number
  today_payment_date?: string
}

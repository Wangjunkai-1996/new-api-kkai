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
import type {
  PaymentOrder,
  PaymentQuote,
  WalletState,
} from './payment-flow-types'

export function paymentOrder(
  overrides: Partial<PaymentOrder> = {}
): PaymentOrder {
  return {
    trade_no: 'ORDER-1',
    version: '1',
    display_status: 'pending',
    status_group: 'pending',
    allowed_actions: ['resume', 'end', 'reconcile'],
    expected_payment: { amount_minor: '1000', currency: 'CNY', exponent: 2 },
    payable_payment: { amount_minor: '1000', currency: 'CNY', exponent: 2 },
    pricing_mode: 'fixed',
    pricing_notice_code: null,
    credited_quota: '10000',
    credit_display: { amount: '10', unit: 'USD' },
    payment_provider: 'epay',
    payment_method: 'alipay',
    created_at: 1_790_000_000,
    deadline_at: null,
    deadline_source: 'unknown',
    close_reason: null,
    closed_at: null,
    completed_at: null,
    last_checked_at: null,
    reason_code: null,
    received_payment: null,
    review: null,
    legacy_display: null,
    ...overrides,
  }
}
export function paymentWallet(
  order: PaymentOrder | null = null,
  overrides: Partial<WalletState> = {}
): WalletState {
  return {
    flow: {
      id: order ? 'FLOW-1' : null,
      revision: '1',
      phase: order ? 'order' : 'idle',
      order,
    },
    can_create: !order,
    create_block_reason_code: order ? 'CURRENT_FLOW_ACTIVE' : null,
    other_unfinished_count: 0,
    server_time: 1_790_000_000,
    ...overrides,
  }
}
export function paymentQuote(): PaymentQuote {
  const {
    expected_payment,
    payable_payment,
    pricing_mode,
    pricing_notice_code,
    credited_quota,
    credit_display,
    payment_provider,
    payment_method,
  } = paymentOrder()
  return {
    expected_payment,
    payable_payment,
    pricing_mode,
    pricing_notice_code,
    credited_quota,
    credit_display,
    payment_provider,
    payment_method,
    quote_token: 'QUOTE-1',
    expires_at: 4_000_000_000,
    server_time: 1_790_000_000,
  }
}

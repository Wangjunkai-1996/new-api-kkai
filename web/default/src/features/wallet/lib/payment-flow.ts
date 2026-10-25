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
import { toIntlLocale } from '@/i18n/languages'

import type {
  MoneyDTO,
  PaymentOrder,
  PaymentCheckout,
  PaymentQuote,
  WalletState,
} from '../payment-flow-types'

export const PAYMENT_STATUS_LABELS = {
  creating: 'Creating payment',
  pending: 'Awaiting payment',
  payment_processing: 'Payment processing',
  cancel_processing: 'Cancellation processing',
  expiry_processing: 'Expiry processing',
  review_required: 'Payment under review',
  credit_review: 'Credit under review',
  success: 'Payment completed',
  cancelled: 'Order cancelled',
  expired: 'Order timed out',
  ended: 'Top-up ended',
  timed_out: 'Top-up timed out',
  failed: 'Payment failed',
  unavailable: 'Payment status unavailable',
} as const
export const PAYMENT_BLOCK_LABELS: Record<string, string> = {
  PAYMENT_DISABLED: 'Online top-up is currently disabled.',
  CHANNEL_UNAVAILABLE: 'No payment channel is currently available.',
  QUOTA_CAPACITY_EXCEEDED:
    'Your credit balance has reached the supported limit.',
  CURRENT_FLOW_ACTIVE: 'Finish the current order before starting another.',
  RECOVERABLE_ORDER_EXISTS:
    'Restore your unfinished order before starting another.',
}
export function isLocallyEnded(
  order: Pick<PaymentOrder, 'display_status' | 'local_ended_at'>
): boolean {
  if (
    order.display_status === 'success' ||
    order.display_status === 'credit_review'
  ) {
    return false
  }
  return (
    order.display_status === 'ended' ||
    order.display_status === 'timed_out' ||
    !!order.local_ended_at
  )
}
export function formatPaymentMoney(
  money: MoneyDTO | null,
  locale = 'en'
): string {
  if (
    !money ||
    !/^\d+$/.test(money.amount_minor) ||
    !Number.isInteger(money.exponent) ||
    money.exponent < 0 ||
    money.exponent > 12
  ) {
    return '—'
  }
  const digits = money.amount_minor.padStart(money.exponent + 1, '0')
  const integer = money.exponent ? digits.slice(0, -money.exponent) : digits
  const fraction = money.exponent ? digits.slice(-money.exponent) : ''
  const formatted = new Intl.NumberFormat(toIntlLocale(locale), {
    minimumFractionDigits: money.exponent,
    maximumFractionDigits: money.exponent,
  })
    .formatToParts(BigInt(integer))
    .map((part) => (part.type === 'fraction' ? fraction : part.value))
    .join('')
  return `${money.currency} ${formatted}`
}
export function mergeWalletState(
  previous: WalletState | undefined,
  next: WalletState
): WalletState {
  if (!previous) return next
  const oldRevision = BigInt(previous.flow.revision),
    nextRevision = BigInt(next.flow.revision)
  if (nextRevision < oldRevision) return previous
  if (nextRevision === oldRevision && previous.flow.id !== next.flow.id) {
    return previous
  }
  if (
    previous.flow.order?.trade_no === next.flow.order?.trade_no &&
    previous.flow.order &&
    next.flow.order &&
    BigInt(next.flow.order.version) < BigInt(previous.flow.order.version)
  ) {
    return previous
  }
  return next
}
export function hasSamePaymentTerms(
  previous: PaymentQuote,
  next: PaymentQuote
): boolean {
  return (
    JSON.stringify([
      previous.expected_payment,
      previous.payable_payment,
      previous.credited_quota,
      previous.credit_display,
      previous.payment_provider,
      previous.payment_method,
      previous.pricing_mode,
      previous.pricing_notice_code,
    ]) ===
    JSON.stringify([
      next.expected_payment,
      next.payable_payment,
      next.credited_quota,
      next.credit_display,
      next.payment_provider,
      next.payment_method,
      next.pricing_mode,
      next.pricing_notice_code,
    ])
  )
}
export interface PaymentWindow {
  window: Window | null
  navigated: boolean
}
export function preparePaymentWindow(): PaymentWindow {
  try {
    const paymentWindow = window.open(
      'about:blank',
      `wallet-payment-${crypto.randomUUID()}`
    )
    if (paymentWindow) paymentWindow.opener = null
    return { window: paymentWindow, navigated: false }
  } catch {
    return { window: null, navigated: false }
  }
}
export function closePaymentPlaceholder(target: PaymentWindow): void {
  if (target.window && !target.navigated && !target.window.closed) {
    target.window.close()
  }
}
export function openPaymentCheckout(
  checkout: PaymentCheckout,
  target: PaymentWindow
): boolean {
  const raw = checkout.kind === 'url' ? checkout.url : checkout.action
  const url = new URL(raw)
  if (
    url.username ||
    url.password ||
    (url.protocol !== 'https:' &&
      !(
        url.protocol === 'http:' &&
        ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
      ))
  ) {
    throw new Error('Unsafe payment destination')
  }
  if (!target.window || target.window.closed) return false
  if (checkout.kind === 'url') target.window.location.replace(url.href)
  else {
    const form = target.window.document.createElement('form')
    form.method = 'POST'
    form.action = url.href
    form.target = '_self'
    form.rel = 'noreferrer'
    for (const [name, value] of Object.entries(checkout.fields)) {
      const input = target.window.document.createElement('input')
      input.type = 'hidden'
      input.name = name
      input.value = value
      form.appendChild(input)
    }
    target.window.document.body.appendChild(form)
    form.submit()
    form.remove()
  }
  target.navigated = true
  return true
}

import dayjs from 'dayjs'
import timezone from 'dayjs/plugin/timezone'
import utc from 'dayjs/plugin/utc'

import { toIntlLocale } from '@/i18n/languages'

import type { InvoiceOrder, InvoiceStatus } from '../types'

dayjs.extend(utc)
dayjs.extend(timezone)

export const invoiceStatuses: InvoiceStatus[] = [
  'pending_review',
  'approved_for_issue',
  'delivered',
  'confirmed',
  'disputed',
  'awaiting_reconfirmation',
  'finance_hold',
  'rejected',
]
export const statusKeys: Record<InvoiceStatus, string> = {
  pending_review: 'Pending review',
  approved_for_issue: 'Awaiting invoice',
  delivered: 'Awaiting confirmation',
  confirmed: 'Confirmed',
  disputed: 'Dispute in progress',
  awaiting_reconfirmation: 'Awaiting reconfirmation',
  finance_hold: 'Finance review required',
  rejected: 'Rejected',
}
export const reasonKeys: Record<string, string> = {
  PAYMENT_UNVERIFIED: 'Payment evidence needs verification',
  PAYMENT_ALIAS_UNRESOLVED: 'Payment source needs verification',
  ORDER_CLAIMED: 'Already included in an invoice request',
  UNPAID: 'Payment not completed',
  REFUNDED_OR_REVERSED: 'Refunded or reversed payment',
  PAYMENT_EXCEPTION: 'Payment needs review',
  WALLET_PURCHASE: 'Paid from wallet. Invoice the original top-up.',
  GRANT_NOT_CASH: 'Gifts and credits are not cash payments',
  CURRENCY_UNSUPPORTED: 'Currency not supported for invoicing',
  SELLER_UNCONFIGURED: 'Seller invoicing is not configured',
  SOURCE_DISABLED: 'This payment source is not available yet',
}
export function invoiceLocale(locale: string): string {
  return toIntlLocale(locale.replaceAll('_', '-')) || 'en'
}
export function formatInvoiceMoney(
  minor: string | null,
  currency: string,
  exponent = 2,
  locale = 'en'
): string {
  if (minor === null || !/^-?\d+$/.test(minor)) return '—'
  const negative = minor.startsWith('-')
  const value = BigInt(minor)
  const absolute = value < 0n ? -value : value
  const scale = 10n ** BigInt(exponent)
  const integer = absolute / scale
  const fraction = (absolute % scale).toString().padStart(exponent, '0')
  const pieces = new Intl.NumberFormat(invoiceLocale(locale), {
    style: 'currency',
    currency,
    minimumFractionDigits: exponent,
    maximumFractionDigits: exponent,
  }).formatToParts(integer)
  return (
    (negative ? '−' : '') +
    pieces
      .map((part) => (part.type === 'fraction' ? fraction : part.value))
      .join('')
  )
}
export function sumInvoiceOrders(orders: InvoiceOrder[]): string {
  return orders
    .reduce((sum, order) => sum + BigInt(order.amount_minor ?? '0'), 0n)
    .toString()
}
export function invoiceMoneyTotals(orders: InvoiceOrder[]) {
  const totals = new Map<
    string,
    { currency: string; currency_exponent: number; amount_minor: string }
  >()
  for (const order of orders) {
    const key = `${order.currency}:${order.currency_exponent}`
    const current = totals.get(key)
    totals.set(key, {
      currency: order.currency,
      currency_exponent: order.currency_exponent,
      amount_minor: (
        BigInt(current?.amount_minor || '0') + BigInt(order.amount_minor || '0')
      ).toString(),
    })
  }
  return [...totals.values()]
}
export function invoiceMinorFromDecimal(
  value: string,
  exponent: number
): string | null {
  if (!Number.isInteger(exponent) || exponent < 0 || exponent > 6) return null
  if (!/^\d+(?:\.\d+)?$/.test(value.trim())) return null
  const [whole, fraction = ''] = value.trim().split('.')
  if (fraction.length > exponent) return null
  return (
    BigInt(whole) * 10n ** BigInt(exponent) +
    BigInt(fraction.padEnd(exponent, '0') || '0')
  ).toString()
}
export function invoiceOrderKey(order: {
  source_type: string
  order_id: string
}) {
  return `${order.source_type}:${order.order_id}`
}
export function invoiceDate(value: string | undefined, locale: string): string {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleString(invoiceLocale(locale))
}
export function dateFilter(
  value: string,
  end = false,
  zone = 'Asia/Shanghai'
): string | undefined {
  if (!value) return undefined
  const day = end ? dayjs.utc(value).add(1, 'day').format('YYYY-MM-DD') : value
  return dayjs.tz(day, zone).toISOString()
}
export function invoiceFilterDate(
  value: string | undefined,
  end = false,
  zone = 'Asia/Shanghai'
) {
  if (!value) return ''
  const day = dayjs(value).tz(zone)
  return (end ? day.subtract(1, 'day') : day).format('YYYY-MM-DD')
}

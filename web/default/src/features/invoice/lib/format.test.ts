import { describe, expect, it } from 'vitest'

import type { InvoiceOrder } from '../types'
import {
  dateFilter,
  formatInvoiceMoney,
  invoiceDate,
  invoiceFilterDate,
  invoiceLocale,
  invoiceMinorFromDecimal,
  invoiceMoneyTotals,
} from './format'

const order = (currency: string, amount_minor: string, currency_exponent = 2) =>
  ({ currency, amount_minor, currency_exponent }) as InvoiceOrder

describe('invoice money and date contracts', () => {
  it('formats int64 money exactly above the JavaScript safe integer limit', () => {
    expect(formatInvoiceMoney('900719925474099312', 'CNY', 2, 'en-US')).toBe(
      'CN¥9,007,199,254,740,993.12'
    )
    expect(formatInvoiceMoney('12345', 'JPY', 0, 'en-US')).toBe('¥12,345')
    expect(formatInvoiceMoney('12345', '', 2, 'en-US')).toBe('123.45')
    expect(formatInvoiceMoney(null, 'CNY')).toBe('—')
  })
  it('normalizes existing project locales without crashing dates or money', () => {
    expect(invoiceLocale('zhCN')).toBe('zh-CN')
    expect(invoiceLocale('zhTW')).toBe('zh-TW')
    expect(invoiceLocale('en_US')).toBe('en-US')
    expect(invoiceLocale('not a locale')).toBe('en')
    expect(formatInvoiceMoney('12000', 'CNY', 2, 'zhCN')).toContain('120.00')
    expect(invoiceDate('2026-10-22T00:00:00Z', 'zhCN')).not.toBe('—')
  })
  it('keeps currency totals separate and exact', () => {
    expect(
      invoiceMoneyTotals([
        order('CNY', '900719925474099312'),
        order('USD', '105'),
        order('CNY', '88'),
      ])
    ).toEqual([
      {
        currency: 'CNY',
        currency_exponent: 2,
        amount_minor: '900719925474099400',
      },
      { currency: 'USD', currency_exponent: 2, amount_minor: '105' },
    ])
  })
  it('uses configured calendar boundaries and an exclusive end across DST', () => {
    expect(dateFilter('2026-10-22', false, 'Asia/Shanghai')).toBe(
      '2026-10-21T16:00:00.000Z'
    )
    expect(dateFilter('2026-03-08', false, 'America/New_York')).toBe(
      '2026-03-08T05:00:00.000Z'
    )
    expect(dateFilter('2026-03-08', true, 'America/New_York')).toBe(
      '2026-03-09T04:00:00.000Z'
    )
    expect(
      invoiceFilterDate('2026-03-09T04:00:00.000Z', true, 'America/New_York')
    ).toBe('2026-03-08')
    expect(dateFilter('')).toBeUndefined()
  })
  it('parses displayed tax amounts without floating point rounding', () => {
    expect(invoiceMinorFromDecimal('9007199254740993.12', 2)).toBe(
      '900719925474099312'
    )
    expect(invoiceMinorFromDecimal('12.3', 2)).toBe('1230')
    expect(invoiceMinorFromDecimal('1.234', 2)).toBeNull()
    expect(invoiceMinorFromDecimal('-1', 2)).toBeNull()
    expect(invoiceMinorFromDecimal('NaN', 2)).toBeNull()
  })
})

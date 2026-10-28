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
import { describe, expect, it } from 'vitest'

import {
  parsePrice,
  parseShopInteger,
  priceInput,
  safeCheckoutUrl,
  safeShopImage,
} from './lib'
import { moneySchema, policySchema } from './schemas'

describe('shop money boundaries', () => {
  it('preserves exact int64 minor units without floating point conversion', () => {
    const money = parsePrice('92233720368547758.07', 'CNY', 2)
    expect(money?.amount_minor).toBe('9223372036854775807')
    expect(money && priceInput(money)).toBe('92233720368547758.07')
    expect(parsePrice('92233720368547758.08', 'CNY', 2)).toBeNull()
    expect(parsePrice('0.001', 'KWD', 3)?.amount_minor).toBe('1')
    expect(parsePrice('10', 'JPY', 0)?.amount_minor).toBe('10')
  })
  it.each(['-1', 'NaN', 'Infinity', '1e3', '01', '1.001', '', ' 1'])(
    'rejects invalid price %s',
    (value) => {
      expect(parsePrice(value, 'CNY', 2)).toBeNull()
    }
  )
  it('rejects unknown currency precision and oversized numbers', () => {
    expect(parsePrice('1', 'CNY', -1)).toBeNull()
    expect(parsePrice('1', 'CNY', 4)).toBeNull()
    expect(parsePrice('1', '', 2)).toBeNull()
    expect(
      moneySchema.safeParse({
        amount_minor: '9223372036854775808',
        currency: 'CNY',
        exponent: 2,
      }).success
    ).toBe(false)
    expect(() => parseShopInteger('2147483648')).toThrow()
    expect(() => parseShopInteger('1e2')).toThrow()
    expect(parseShopInteger('0')).toBe(0)
  })
  it.each(['invalid', '1.2', '-1', '', '9'.repeat(100)])(
    'rejects malformed server money without throwing: %s',
    (amount_minor) => {
      expect(
        moneySchema.safeParse({ amount_minor, currency: 'CNY', exponent: 2 })
          .success
      ).toBe(false)
    }
  )
  it('accepts an incomplete SLA draft without inventing a schedule', () => {
    const policy = policySchema.parse({
      sla: {
        clock: '',
        duration_minutes: 0,
        timezone: '',
        pause_on_missing_input: false,
        weekly_windows: null,
      },
    })
    expect(policy.sla?.clock).toBe('')
    expect(policy.sla?.weekly_windows).toEqual([])
  })
})

describe('shop links', () => {
  it('accepts HTTPS payment URLs without embedded credentials only', () => {
    expect(safeCheckoutUrl('https://pay.example/checkout?id=1')).toBe(
      'https://pay.example/checkout?id=1'
    )
    expect(safeCheckoutUrl('javascript:alert(1)')).toBeUndefined()
    expect(safeCheckoutUrl('http://pay.example')).toBeUndefined()
    expect(safeCheckoutUrl('https://user:secret@pay.example')).toBeUndefined()
    expect(safeCheckoutUrl('/pay')).toBeUndefined()
    expect(safeShopImage('https://other.example/image.webp')).toBeUndefined()
  })
})

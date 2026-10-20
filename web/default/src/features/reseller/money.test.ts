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
import { describe, expect, test } from 'vitest'

import { formatExactMinor, formatMinor, parseCnyMinor } from './money'

describe('withdrawal input', () => {
  test.each([
    ['100', 10000],
    ['100.01', 10001],
    ['0.1', 10],
    [' 124.99 ', 12499],
  ])('parses %s as integer cents', (value, expected) => {
    expect(parseCnyMinor(value)).toBe(expected)
  })
  test.each([
    '-1',
    '1e5',
    '1.001',
    'Infinity',
    'NaN',
    '',
    '99999999999999999999',
    '00100',
  ])('rejects invalid amount %s', (value) => {
    expect(parseCnyMinor(value)).toBeNull()
  })
})

test.each([
  ['zhCN', '¥100.01', '¥ 1.234567'],
  ['zhTW', 'CN¥100.01', 'CN¥ 1.234567'],
])(
  'Chinese interface amounts render without losing sub-cent precision (%s)',
  (language, amount, exactAmount) => {
    expect(formatMinor(10001, language)).toBe(amount)
    expect(formatExactMinor('123.4567', language)).toBe(exactAmount)
  }
)

test('exact settlement costs retain sub-cent precision', () => {
  expect(formatExactMinor('123.4567', 'en')).toBe('CN¥ 1.234567')
  expect(formatExactMinor('0.000001', 'en')).toBe('CN¥ 0.00000001')
  expect(formatExactMinor('100', 'fr')).toBe('CNY 1,00')
  expect(formatExactMinor('invalid', 'en')).toBe('—')
})

test('rational costs are rounded for display with an approximation mark', () => {
  expect(formatExactMinor('20000/3', 'en')).toBe('≈CN¥ 66.66666667')
  expect(formatExactMinor('-1/3', 'fr')).toBe('≈-CNY 0,00333333')
  expect(formatExactMinor('300/3', 'en')).toBe('CN¥ 1.00')
  expect(formatExactMinor('1/0', 'en')).toBe('—')
  expect(formatExactMinor('1/-3', 'en')).toBe('—')
})

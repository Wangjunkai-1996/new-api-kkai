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
import { expect, it } from 'vitest'

import { formatWalletCash } from './funding-summary'

it.each([
  ['CNY', 2, '368000', 'CN¥3,680.00'],
  ['USD', 2, '2000', '$20.00'],
  ['JPY', 0, '1234', '¥1,234'],
  ['KWD', 3, '1234', 'KWD\u00a01.234'],
  ['USD', 2, '900719925474099301', '$9,007,199,254,740,993.01'],
  ['USD', 2, '-50', '-$0.50'],
] as const)(
  'preserves %s amounts and their original minor units',
  (currency, exponent, amount_minor, expected) => {
    expect(formatWalletCash({ currency, exponent, amount_minor }, 'en')).toBe(
      expected
    )
  }
)

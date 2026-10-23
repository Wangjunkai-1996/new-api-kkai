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

import type { MoneyDTO } from '../payment-flow-types'

export function formatWalletCash(money: MoneyDTO, locale: string): string {
  const negative = money.amount_minor.startsWith('-')
  const digits = money.amount_minor
    .replace(/^-/, '')
    .padStart(money.exponent + 1, '0')
  const integer = BigInt(
    money.exponent ? digits.slice(0, -money.exponent) : digits
  )
  const fraction = money.exponent ? digits.slice(-money.exponent) : ''
  let value: bigint | number = integer
  if (negative) value = integer === 0n ? -0 : -integer
  return new Intl.NumberFormat(toIntlLocale(locale), {
    style: 'currency',
    currency: money.currency,
    currencyDisplay: 'symbol',
    minimumFractionDigits: money.exponent,
    maximumFractionDigits: money.exponent,
  })
    .formatToParts(value)
    .map((part) => (part.type === 'fraction' ? fraction : part.value))
    .join('')
}

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
import { moneySchema, type Money } from './schemas'

export function parsePrice(
  value: string,
  currency: string,
  exponent: number
): Money | null {
  if (
    !Number.isInteger(exponent) ||
    exponent < 0 ||
    exponent > 3 ||
    !/^(0|[1-9]\d*)(\.\d+)?$/.test(value)
  ) {
    return null
  }
  const [whole, fraction = ''] = value.split('.')
  if (fraction.length > exponent) return null
  const result = moneySchema.safeParse({
    amount_minor: BigInt(
      `${whole}${fraction.padEnd(exponent, '0')}`
    ).toString(),
    currency,
    exponent,
  })
  return result.success ? result.data : null
}

export function priceInput(money: Money) {
  const digits = money.amount_minor.padStart(money.exponent + 1, '0')
  return money.exponent
    ? `${digits.slice(0, -money.exponent)}.${digits.slice(-money.exponent)}`
    : digits
}

export function parseShopInteger(
  value: string,
  minimum = 0,
  maximum = 2147483647
) {
  if (!/^(0|[1-9]\d*)$/.test(value)) throw new Error('invalid integer')
  const parsed = Number(value)
  if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) {
    throw new Error('invalid integer')
  }
  return parsed
}

export function safeShopImage(value?: string) {
  if (!value) return undefined
  try {
    const url = new URL(value, window.location.origin)
    return url.origin === window.location.origin &&
      url.protocol === window.location.protocol
      ? url.href
      : undefined
  } catch {
    return undefined
  }
}

export function safeCheckoutUrl(value?: string) {
  if (!value) return undefined
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && !url.username && !url.password
      ? url.href
      : undefined
  } catch {
    return undefined
  }
}

export async function shopDigest(value: string) {
  const bytes = await crypto.subtle.digest(
    'SHA-256',
    new TextEncoder().encode(value)
  )
  return Array.from(new Uint8Array(bytes), (byte) =>
    byte.toString(16).padStart(2, '0')
  ).join('')
}

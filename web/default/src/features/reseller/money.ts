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
/** Convert a user-entered CNY amount without floating-point rounding. */
export function parseCnyMinor(value: string): number | null {
  const match = /^(0|[1-9]\d{0,10})(?:\.(\d{1,2}))?$/.exec(value.trim())
  if (!match) return null
  const minor =
    BigInt(match[1]) * 100n + BigInt((match[2] ?? '').padEnd(2, '0'))
  if (minor > BigInt(Number.MAX_SAFE_INTEGER)) return null
  return Number(minor)
}

/** Floating point is only used for display; ledger values remain integer cents. */
export function formatMinor(value: number, locale: string): string {
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: 'CNY',
  }).format(value / 100)
}

/** Preserve decimals; mark rounded displays of exact rational costs. */
export function formatExactMinor(value: string, locale: string): string {
  if (value.length > 128) return '—'
  let approximate = false
  const rational = /^(-?)(\d+)\/([1-9]\d*)$/.exec(value)
  if (rational) {
    const numerator = BigInt(rational[2])
    const denominator = BigInt(rational[3])
    // Eight CNY decimal places; the stored fraction remains exact on the server.
    const scaled = numerator * 1000000n
    const quotient = scaled / denominator
    const remainder = scaled % denominator
    approximate = remainder !== 0n
    const rounded = quotient + (remainder * 2n >= denominator ? 1n : 0n)
    const digits = rounded.toString().padStart(7, '0')
    value = `${rational[1]}${digits.slice(0, -6)}.${digits.slice(-6)}`
  }
  const match = /^(-?)(\d+)(?:\.(\d+))?$/.exec(value)
  if (!match) return '—'
  const rawFraction = match[3] ?? ''
  const scale = rawFraction.length + 2
  const cents = `${match[2]}${rawFraction}`.padStart(scale + 1, '0')
  const yuan = BigInt(cents.slice(0, -scale))
  const fraction = cents.slice(-scale).replace(/0+$/, '').padEnd(2, '0')
  const decimalSeparator =
    new Intl.NumberFormat(locale)
      .formatToParts(1.1)
      .find((part) => part.type === 'decimal')?.value ?? '.'
  const currency =
    new Intl.NumberFormat(locale, { style: 'currency', currency: 'CNY' })
      .formatToParts(0)
      .find((part) => part.type === 'currency')?.value ?? 'CNY'
  return `${approximate ? '≈' : ''}${match[1]}${currency} ${new Intl.NumberFormat(locale).format(yuan)}${decimalSeparator}${fraction}`
}

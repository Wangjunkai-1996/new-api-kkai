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
import { formatCurrencyUSD } from '@/lib/format'

const GENERIC_AXIOS_ERROR = /^Request failed with status code \d+$/

export const formatRebateAmount = (amountCents: number): string =>
  formatCurrencyUSD(amountCents / 100)

export const formatRebateRate = (
  value: unknown,
  language?: string
): string | null => {
  if (
    typeof value !== 'number' ||
    !Number.isFinite(value) ||
    value < 0 ||
    value > 1
  ) {
    return null
  }
  return new Intl.NumberFormat(toIntlLocale(language), {
    style: 'percent',
    maximumSignificantDigits: 15,
  }).format(value === 0 ? 0 : value)
}

export const formatInvitationDate = (value?: string | null): string => {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

export const getInvitationErrorMessage = (
  error: unknown,
  fallback: string
): string => {
  const responseMessage = (
    error as { response?: { data?: { message?: unknown } } }
  )?.response?.data?.message
  if (typeof responseMessage === 'string' && responseMessage.trim()) {
    return responseMessage
  }

  const message = (error as { message?: unknown })?.message
  if (
    typeof message === 'string' &&
    message.trim() &&
    !GENERIC_AXIOS_ERROR.test(message)
  ) {
    return message
  }
  return fallback
}

export const formatSalesAmount = (amount: number, currency: string): string =>
  new Intl.NumberFormat(
    undefined,
    currency === 'usd_credit_v1'
      ? { style: 'currency', currency: 'USD' }
      : { minimumFractionDigits: 2, maximumFractionDigits: 2 }
  ).format(amount / 100)

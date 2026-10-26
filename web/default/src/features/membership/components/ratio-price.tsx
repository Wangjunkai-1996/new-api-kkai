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
import { useTranslation } from 'react-i18next'

import { toIntlLocale } from '@/i18n/languages'

import type { MembershipTierQuote } from '../types'

export function RatioValue(props: { value?: number }) {
  const { t, i18n } = useTranslation()
  if (props.value === undefined) return <span>—</span>
  if (props.value === 0) return <span>{t('Free of charge')}</span>
  return (
    <span className='[overflow-wrap:anywhere] tabular-nums'>
      {new Intl.NumberFormat(toIntlLocale(i18n.language), {
        maximumSignificantDigits: 12,
      }).format(props.value)}
      <span aria-hidden='true'>×</span>
    </span>
  )
}

export function MembershipRatioPrice(props: {
  quote?: MembershipTierQuote
  accentClassName?: string
}) {
  const { t, i18n } = useTranslation()
  if (!props.quote) {
    return (
      <span className='text-muted-foreground text-xs'>{t('Unavailable')}</span>
    )
  }
  let note = t('No extra membership discount')
  if (props.quote.membership_ratio === 0) note = t('Free group')
  else if (props.quote.savings > 0) {
    note = t('Save {{percent}} vs wallet', {
      percent: new Intl.NumberFormat(toIntlLocale(i18n.language), {
        style: 'percent',
        maximumFractionDigits: 2,
      }).format(props.quote.savings),
    })
  } else if (props.quote.coefficient < 1) note = t('Existing discount retained')

  return (
    <>
      <span
        className={`block text-lg font-semibold ${props.accentClassName ?? 'text-[#876321] dark:text-[#dec387]'}`}
      >
        <RatioValue value={props.quote.membership_ratio} />
      </span>
      <span
        className={
          props.quote.savings > 0
            ? 'mt-1 block text-xs text-emerald-700 dark:text-emerald-300'
            : 'text-muted-foreground mt-1 block text-xs'
        }
      >
        {note}
      </span>
    </>
  )
}

export function MembershipRatioCalculation(props: {
  baseRatio?: number
  walletRatio: number
  quotes: Array<{ id: string; name: string; quote?: MembershipTierQuote }>
}) {
  const { t, i18n } = useTranslation()
  const percent = new Intl.NumberFormat(toIntlLocale(i18n.language), {
    style: 'percent',
    maximumFractionDigits: 2,
  })
  return (
    <div className='text-muted-foreground space-y-3 text-xs leading-relaxed'>
      <p>
        {t('Standard ratio')}: <RatioValue value={props.baseRatio} />
        <span className='mx-2' aria-hidden='true'>
          ·
        </span>
        {t('My wallet ratio')}: <RatioValue value={props.walletRatio} />
      </p>
      {props.quotes.map((item) => {
        if (!item.quote) return null
        return (
          <div key={item.id} className='space-y-1'>
            <p className='text-foreground font-medium'>
              {item.name} · {t('Membership coefficient')}:{' '}
              {percent.format(item.quote.coefficient)}
              <span className='mx-2' aria-hidden='true'>
                →
              </span>
              <RatioValue value={item.quote.membership_ratio} />
            </p>
            <p>
              {item.quote.coefficient < 1
                ? t(
                    'The lower price between your wallet and the discounted standard ratio applies. Discounts are not stacked.'
                  )
                : t(
                    'Without an additional membership discount, your existing wallet ratio is retained.'
                  )}
            </p>
          </div>
        )
      })}
    </div>
  )
}

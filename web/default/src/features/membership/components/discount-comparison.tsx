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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'

import type { MembershipCatalog, MembershipGroupQuote } from '../types'

export function MembershipDiscounts(props: {
  catalog: MembershipCatalog
  currentDiscounts: MembershipGroupQuote[]
  active: boolean
}) {
  const { t, i18n } = useTranslation()
  const [search, setSearch] = useState('')
  const tiers = [...props.catalog.tiers]
    .filter((tier) => tier.enabled)
    .sort((a, b) => a.rank - b.rank)
  const visibleGroups = props.catalog.groups.filter((group) =>
    `${group.display_name} ${group.key}`
      .toLocaleLowerCase()
      .includes(search.toLocaleLowerCase())
  )
  const discounts = new Map(
    props.currentDiscounts.map((quote) => [quote.group_key, quote])
  )
  const formatCoefficient = (coefficient: number) =>
    new Intl.NumberFormat(toIntlLocale(i18n.language), {
      style: 'percent',
      maximumFractionDigits: 2,
    }).format(coefficient)

  return (
    <section
      className='space-y-3'
      aria-labelledby='membership-discounts-heading'
    >
      <div className='flex flex-col justify-between gap-3 sm:flex-row sm:items-center'>
        <div>
          <h2
            id='membership-discounts-heading'
            className='text-base font-semibold'
          >
            {t('Group discounts')}
          </h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              'Compare the latest published discounts for each membership tier.'
            )}
          </p>
        </div>
        <Input
          className='h-9 sm:max-w-64'
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={t('Search groups')}
          aria-label={t('Search groups')}
        />
      </div>
      {visibleGroups.length === 0 && (
        <p className='text-muted-foreground border-y py-4 text-sm'>
          {t('No matching groups')}
        </p>
      )}
      {visibleGroups.length > 0 && (
        <div className='divide-y border-y'>
          {visibleGroups.map((group) => {
            const current = discounts.get(group.key)
            return (
              <div
                key={group.key}
                className={cn(
                  'grid min-w-0 gap-x-5 gap-y-3 py-4 lg:items-center',
                  props.active
                    ? 'lg:grid-cols-[minmax(10rem,1.1fr)_minmax(10rem,0.9fr)_minmax(0,2fr)]'
                    : 'lg:grid-cols-[minmax(10rem,1fr)_minmax(0,3fr)]'
                )}
              >
                <div className='min-w-0'>
                  <h3 className='text-sm font-medium'>
                    {group.display_name || group.key}
                  </h3>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {group.enabled ? t('Billing group') : t('Group disabled')}
                  </p>
                </div>
                {props.active && (
                  <div className='border-primary/50 bg-primary/5 min-w-0 border-l-2 py-1 pl-3'>
                    <p className='text-muted-foreground text-xs'>
                      {t('My current discount')}
                    </p>
                    <p className='mt-0.5 text-sm font-semibold tabular-nums'>
                      {current?.published
                        ? formatCoefficient(current.coefficient)
                        : t('No extra discount')}
                    </p>
                    {current && (
                      <p className='text-muted-foreground mt-0.5 text-xs'>
                        {t('Current billing ratio')}: {current.membership_ratio}{' '}
                        · {t('Wallet')}: {current.wallet_ratio}
                      </p>
                    )}
                  </div>
                )}
                <dl className='grid min-w-0 grid-cols-2 gap-3 sm:grid-cols-3'>
                  {tiers.map((tier) => (
                    <div key={tier.id} className='min-w-0'>
                      <dt className='text-muted-foreground text-xs'>
                        {tier.name} · {t('Group discounts')}
                      </dt>
                      <dd className='mt-1 text-sm font-semibold tabular-nums'>
                        {group.published
                          ? formatCoefficient(tier.discounts[group.key] ?? 1)
                          : t('Not yet published')}
                      </dd>
                    </div>
                  ))}
                </dl>
              </div>
            )
          })}
        </div>
      )}
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {t(
          'Percentages apply to the group base ratio. Your existing group price is retained when lower; discounts are not stacked. Wallet payments do not receive the membership discount.'
        )}
      </p>
    </section>
  )
}

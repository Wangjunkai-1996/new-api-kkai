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
      className='space-y-4'
      aria-labelledby='membership-discounts-heading'
    >
      <div className='flex flex-col justify-between gap-3 sm:flex-row sm:items-center'>
        <div>
          <h2
            id='membership-discounts-heading'
            className='text-lg font-semibold'
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
          className='sm:max-w-64'
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={t('Search groups')}
          aria-label={t('Search groups')}
        />
      </div>
      {visibleGroups.length === 0 && (
        <p className='text-muted-foreground rounded-xl border p-5 text-sm'>
          {t('No matching groups')}
        </p>
      )}
      <div className='overflow-hidden rounded-2xl border'>
        {visibleGroups.map((group) => {
          const current = discounts.get(group.key)
          return (
            <div
              key={group.key}
              className='bg-card flex flex-col gap-4 border-b p-5 last:border-b-0 lg:flex-row lg:items-center'
            >
              <div className='lg:w-44 lg:shrink-0'>
                <h3 className='font-medium'>
                  {group.display_name || group.key}
                </h3>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {group.enabled ? t('Billing group') : t('Group disabled')}
                </p>
              </div>
              {props.active && (
                <div className='bg-muted/60 rounded-lg p-3 lg:w-52 lg:shrink-0'>
                  <p className='text-muted-foreground text-xs'>
                    {t('My current discount')}
                  </p>
                  <p className='mt-1 font-semibold'>
                    {current?.published
                      ? formatCoefficient(current.coefficient)
                      : t('No extra discount')}
                  </p>
                  {current && (
                    <p className='text-muted-foreground mt-1 text-xs'>
                      {t('Current billing ratio')}: {current.membership_ratio} ·{' '}
                      {t('Wallet')}: {current.wallet_ratio}
                    </p>
                  )}
                </div>
              )}
              <dl className='grid flex-1 grid-cols-1 gap-3 sm:grid-cols-3'>
                {tiers.map((tier) => (
                  <div key={tier.id}>
                    <dt className='text-muted-foreground text-xs'>
                      {tier.name} · {t('Group discounts')}
                    </dt>
                    <dd className='mt-1 font-semibold'>
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
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {t(
          'Percentages apply to the group base ratio. Your existing group price is retained when lower; discounts are not stacked. Wallet payments do not receive the membership discount.'
        )}
      </p>
    </section>
  )
}

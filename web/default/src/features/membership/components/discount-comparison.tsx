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
import { BadgeCheck, Search, ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

import { isMembershipTierQuote, membershipEmblems } from '../lib'
import type {
  MembershipCatalog,
  MembershipGroupQuote,
  MembershipTier,
} from '../types'
import { MembershipRatioPrice, RatioValue } from './ratio-price'

const appearances = {
  gold: {
    accent: 'text-amber-800 dark:text-[#e2c17d]',
    current:
      'border-amber-300/60 bg-amber-50/80 dark:border-[#665130] dark:bg-[#302a20]',
    badge: 'bg-amber-100 text-amber-900 dark:bg-[#4a3b24] dark:text-[#e2c17d]',
  },
  diamond: {
    accent: 'text-sky-800 dark:text-[#a2d5f5]',
    current:
      'border-sky-300/60 bg-sky-50/80 dark:border-[#365d75] dark:bg-[#202e39]',
    badge: 'bg-sky-100 text-sky-900 dark:bg-[#29465a] dark:text-[#a2d5f5]',
  },
  king: {
    accent: 'text-violet-800 dark:text-[#d6b9f5]',
    current:
      'border-violet-300/60 bg-violet-50/80 dark:border-[#654b7e] dark:bg-[#2c2636]',
    badge:
      'bg-violet-100 text-violet-900 dark:bg-[#463456] dark:text-[#d6b9f5]',
  },
}

export function MembershipDiscounts(props: {
  catalog: MembershipCatalog
  currentDiscounts: MembershipGroupQuote[]
  currentTierId?: string
  currentTier?: MembershipTier | null
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const tiers = [...props.catalog.tiers].filter((tier) => tier.enabled)
  if (
    props.currentTierId &&
    props.currentTier?.id === props.currentTierId &&
    !tiers.some((tier) => tier.id === props.currentTierId)
  ) {
    tiers.push(props.currentTier)
  }
  tiers.sort((a, b) => a.rank - b.rank)
  const groups = props.catalog.group_quotes
  const discounts = new Map(
    props.currentDiscounts.map((quote) => [quote.group_key, quote])
  )
  const quotesAvailable =
    Array.isArray(groups) &&
    groups.every(
      (group) =>
        group &&
        typeof group.group_key === 'string' &&
        typeof group.display_name === 'string' &&
        typeof group.description === 'string' &&
        Number.isFinite(group.wallet_ratio) &&
        group.wallet_ratio >= 0 &&
        tiers.every((tier) => {
          if (tier.id === props.currentTierId) {
            const current = discounts.get(group.group_key)
            return current?.enabled && isMembershipTierQuote(current)
          }
          return isMembershipTierQuote(group.tier_quotes?.[tier.id])
        })
    )
  const visibleGroups = quotesAvailable
    ? (groups ?? []).filter((group) =>
        `${group.display_name} ${group.description} ${group.group_key}`
          .toLocaleLowerCase()
          .includes(search.trim().toLocaleLowerCase())
      )
    : []
  // A larger dynamic catalog uses the same stacked group cards as mobile.
  const wide = tiers.length > 3

  return (
    <section
      className='bg-card overflow-hidden rounded-2xl border'
      aria-labelledby='membership-discounts-heading'
    >
      <div className='flex flex-col justify-between gap-4 p-5 sm:flex-row sm:items-center sm:px-6'>
        <div>
          <h2
            id='membership-discounts-heading'
            className='text-lg font-semibold tracking-tight'
          >
            {t('Group discounts')}
          </h2>
          <p className='text-muted-foreground mt-1 text-xs leading-relaxed'>
            {t(
              'A lower ratio means a lower request price. Membership discounts apply to the standard group ratio, while a lower existing wallet price is retained.'
            )}
          </p>
        </div>
        <div className='relative w-full sm:w-60'>
          <Search
            className='text-muted-foreground pointer-events-none absolute top-3 left-3 size-4'
            aria-hidden='true'
          />
          <Input
            type='search'
            className='h-10 pl-9'
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('Search groups')}
            aria-label={t('Search groups')}
          />
        </div>
      </div>
      {!quotesAvailable && (
        <Alert variant='destructive' className='mx-5 mb-5 w-auto sm:mx-6'>
          <AlertDescription>
            {t(
              'Membership information could not be loaded. Please refresh before purchasing.'
            )}
          </AlertDescription>
        </Alert>
      )}
      {quotesAvailable && visibleGroups.length === 0 && (
        <p className='text-muted-foreground border-t px-6 py-10 text-center text-sm'>
          {t('No matching groups')}
        </p>
      )}
      {quotesAvailable && visibleGroups.length > 0 && (
        <div className='px-4 pb-1 sm:px-6'>
          <table
            role='table'
            className={cn(
              'block w-full table-fixed border-separate border-spacing-0 text-sm @4xl/content:table',
              wide && '@4xl/content:block'
            )}
          >
            <caption className='sr-only'>{t('Group discounts')}</caption>
            <thead
              role='rowgroup'
              className={cn(
                'hidden @4xl/content:table-header-group',
                wide && '@4xl/content:hidden'
              )}
            >
              <tr role='row'>
                <th
                  role='columnheader'
                  scope='col'
                  className='text-muted-foreground w-[34%] border-b py-5 pr-6 text-left text-xs font-normal'
                >
                  {t('Billing group')}
                </th>
                {tiers.map((tier) => {
                  const current = tier.id === props.currentTierId
                  const style = appearances[tier.style] ?? appearances.gold
                  return (
                    <th
                      role='columnheader'
                      key={tier.id}
                      scope='col'
                      aria-current={current ? 'true' : undefined}
                      className={cn(
                        'border-y border-transparent px-4 py-5 text-center align-middle first:rounded-tl-xl last:rounded-tr-xl',
                        current && `${style.current} rounded-t-xl border-x`
                      )}
                    >
                      <div className='flex items-center justify-center gap-2'>
                        <img
                          src={membershipEmblems[tier.style]}
                          width={36}
                          height={36}
                          alt=''
                          className='size-9 object-contain'
                        />
                        <span
                          className={cn(
                            'font-semibold break-words',
                            style.accent
                          )}
                        >
                          {tier.name}
                        </span>
                      </div>
                      <span
                        className={cn(
                          'mt-2 inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-normal',
                          current ? style.badge : 'text-muted-foreground'
                        )}
                      >
                        {current && (
                          <BadgeCheck className='size-3' aria-hidden='true' />
                        )}
                        {current
                          ? t('Current membership')
                          : t('Group discounts')}
                      </span>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody
              role='rowgroup'
              className={cn(
                'block space-y-3 @4xl/content:table-row-group @4xl/content:space-y-0',
                wide && '@4xl/content:block @4xl/content:space-y-3'
              )}
            >
              {visibleGroups.map((group) => (
                <tr
                  role='row'
                  key={group.group_key}
                  className={cn(
                    'group grid rounded-xl border @4xl/content:table-row',
                    {
                      'grid-cols-1': tiers.length === 1,
                      'grid-cols-2': tiers.length === 2,
                      'grid-cols-3': tiers.length > 2,
                    },
                    wide && '@4xl/content:grid'
                  )}
                >
                  <th
                    role='rowheader'
                    scope='row'
                    className={cn(
                      'col-span-full min-w-0 border-b p-4 text-left font-normal @4xl/content:table-cell @4xl/content:py-5 @4xl/content:pl-0 @4xl/content:pr-6',
                      wide && '@4xl/content:block @4xl/content:px-4'
                    )}
                  >
                    <span className='block font-medium break-words'>
                      {group.display_name || group.group_key}
                    </span>
                    <span className='text-muted-foreground mt-1 block text-xs leading-relaxed break-words'>
                      {group.description || t('Billing group')}
                    </span>
                    <span className='text-muted-foreground mt-2 block text-xs'>
                      {t('My wallet ratio')}{' '}
                      <RatioValue value={group.wallet_ratio} />
                    </span>
                  </th>
                  {tiers.map((tier) => {
                    const current = tier.id === props.currentTierId
                    const style = appearances[tier.style] ?? appearances.gold
                    const quote = current
                      ? discounts.get(group.group_key)
                      : group.tier_quotes[tier.id]
                    return (
                      <td
                        role='cell'
                        key={tier.id}
                        className={cn(
                          'min-w-0 border-transparent p-2.5 text-center align-middle @4xl/content:border-b @4xl/content:px-4 @4xl/content:py-5 motion-safe:transition-colors',
                          current
                            ? `${style.current} border-x`
                            : 'group-hover:bg-muted/30 @4xl/content:border-border'
                        )}
                      >
                        <span
                          className={cn(
                            'mb-2 block text-xs font-medium break-words @4xl/content:hidden',
                            style.accent,
                            wide && '@4xl/content:block'
                          )}
                        >
                          {tier.name}
                        </span>
                        <MembershipRatioPrice
                          quote={
                            isMembershipTierQuote(quote) ? quote : undefined
                          }
                          accentClassName={style.accent}
                        />
                        {current && (
                          <span
                            className={cn(
                              'mt-2 inline-flex max-w-full items-center justify-center gap-1 text-[10px] @4xl/content:hidden',
                              style.accent,
                              wide && '@4xl/content:inline-flex'
                            )}
                          >
                            <BadgeCheck
                              className='size-3 shrink-0'
                              aria-hidden='true'
                            />
                            {t('Current membership')}
                          </span>
                        )}
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div className='text-muted-foreground bg-muted/40 m-4 flex items-start gap-2.5 rounded-xl p-3.5 text-xs leading-relaxed sm:mx-6 sm:mb-5'>
        <ShieldCheck className='mt-0.5 size-4 shrink-0' aria-hidden='true' />
        <p>
          {t(
            'Savings are compared with your wallet price. Existing group discounts and membership discounts are not stacked.'
          )}
        </p>
      </div>
    </section>
  )
}

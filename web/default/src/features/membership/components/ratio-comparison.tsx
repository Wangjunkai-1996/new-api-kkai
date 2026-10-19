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
import { ArrowDown01Icon, Search01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Fragment, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import type { MembershipTierQuote } from '../types'
import {
  MembershipRatioCalculation,
  MembershipRatioPrice,
  RatioValue,
} from './ratio-price'

export interface MembershipRatioRow {
  group_key: string
  display_name: string
  description?: string
  base_ratio?: number
  wallet_ratio: number
  quotes: Record<string, MembershipTierQuote>
}

export interface MembershipRatioColumn {
  id: string
  name: string
}

export function MembershipRatioComparison(props: {
  rows: MembershipRatioRow[]
  columns: MembershipRatioColumn[]
  current: boolean
}) {
  const { t } = useTranslation()
  const id = useId()
  const [search, setSearch] = useState('')
  const [onlySavings, setOnlySavings] = useState(false)
  const [expanded, setExpanded] = useState<string | null>(null)
  const query = search.trim().toLocaleLowerCase()
  const rows = props.rows.filter((row) => {
    const matches =
      `${row.display_name} ${row.description ?? ''} ${row.group_key}`
        .toLocaleLowerCase()
        .includes(query)
    return (
      matches &&
      (!onlySavings ||
        props.columns.some(
          (column) => (row.quotes[column.id]?.savings ?? 0) > 0
        ))
    )
  })
  // Keep each tier readable when an installation offers many dynamic plans.
  const wide = props.columns.length > 3

  return (
    <>
      <div className='flex flex-col gap-4 px-5 py-5 sm:flex-row sm:items-center sm:justify-between sm:px-6'>
        <div className='relative sm:w-64'>
          <HugeiconsIcon
            icon={Search01Icon}
            size={16}
            className='text-muted-foreground pointer-events-none absolute top-3 left-3'
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
        <label className='text-muted-foreground flex cursor-pointer items-center gap-2 text-xs'>
          <Switch checked={onlySavings} onCheckedChange={setOnlySavings} />
          {t('Only show savings')}
        </label>
      </div>
      <div className='px-5 sm:px-6'>
        <table
          role='table'
          className={cn(
            'block w-full border-separate border-spacing-0 text-sm @5xl/content:table @5xl/content:table-fixed',
            wide && '@5xl/content:block'
          )}
        >
          <caption className='sr-only'>{t('Group ratio comparison')}</caption>
          <thead
            role='rowgroup'
            className={cn(
              'hidden @5xl/content:table-header-group',
              wide && '@5xl/content:hidden'
            )}
          >
            <tr role='row'>
              <th
                scope='col'
                className='text-muted-foreground w-[30%] border-y py-4 pr-4 text-left text-xs font-normal'
              >
                {t('Billing group')}
              </th>
              <th
                scope='col'
                className='text-muted-foreground w-[16%] border-y px-3 py-4 text-right text-xs font-normal'
              >
                {t('Standard ratio')}
              </th>
              <th
                scope='col'
                className='text-muted-foreground w-[19%] border-y px-3 py-4 text-right text-xs font-normal'
              >
                {t('My wallet ratio')}
              </th>
              {props.columns.map((column) => (
                <th
                  key={column.id}
                  scope='col'
                  className='border-y border-[#e8d9b7] bg-[#fbf7ed] px-4 py-3 text-right font-medium break-words first:rounded-tl-lg last:rounded-tr-lg dark:border-[#4a4132] dark:bg-[#2d2a24]'
                >
                  <span className='text-[#876321] dark:text-[#dec387]'>
                    {column.name}
                  </span>
                  <span className='text-muted-foreground mt-1 block text-xs font-normal'>
                    {props.current
                      ? t('My locked benefits')
                      : t('New purchase / upgrade')}
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody
            role='rowgroup'
            className={cn(
              'block space-y-4 @5xl/content:table-row-group @5xl/content:space-y-0',
              wide && '@5xl/content:block @5xl/content:space-y-4'
            )}
          >
            {rows.map((row, index) => {
              const open = expanded === row.group_key
              const detailId = `${id}-${index}`
              return (
                <Fragment key={row.group_key}>
                  <tr
                    role='row'
                    className={cn(
                      'block rounded-xl border p-4 @5xl/content:table-row @5xl/content:rounded-none @5xl/content:border-0 @5xl/content:p-0',
                      wide &&
                        '@5xl/content:block @5xl/content:rounded-xl @5xl/content:border @5xl/content:p-4'
                    )}
                  >
                    <th
                      scope='row'
                      role='rowheader'
                      className={cn(
                        'block pb-4 text-left font-normal @5xl/content:table-cell @5xl/content:border-b @5xl/content:py-5 @5xl/content:pr-4',
                        wide &&
                          '@5xl/content:block @5xl/content:border-0 @5xl/content:pt-0'
                      )}
                    >
                      <button
                        type='button'
                        className='focus-visible:ring-ring flex w-full cursor-pointer items-start gap-2 rounded-sm text-left outline-none focus-visible:ring-2'
                        aria-expanded={open}
                        aria-controls={detailId}
                        onClick={() => setExpanded(open ? null : row.group_key)}
                      >
                        <HugeiconsIcon
                          icon={ArrowDown01Icon}
                          size={14}
                          aria-hidden='true'
                          className={cn(
                            'text-muted-foreground mt-1 shrink-0 -rotate-90 transition-transform',
                            open && 'rotate-0'
                          )}
                        />
                        <span className='min-w-0'>
                          <span className='block font-medium break-words'>
                            {row.display_name || row.group_key}
                          </span>
                          <span className='text-muted-foreground mt-1 block text-xs leading-relaxed break-words'>
                            {row.description || t('Billing group')}
                          </span>
                        </span>
                      </button>
                    </th>
                    <td
                      role='cell'
                      className={cn(
                        'flex items-center justify-between gap-3 border-t py-3 @5xl/content:table-cell @5xl/content:border-t-0 @5xl/content:border-b @5xl/content:px-3 @5xl/content:py-5 @5xl/content:text-right',
                        wide &&
                          '@5xl/content:flex @5xl/content:border-t @5xl/content:border-b-0 @5xl/content:px-0 @5xl/content:py-3'
                      )}
                    >
                      <span
                        className={cn(
                          'text-muted-foreground text-xs @5xl/content:hidden',
                          wide && '@5xl/content:block'
                        )}
                      >
                        {t('Standard ratio')}
                      </span>
                      <span className='text-muted-foreground'>
                        <RatioValue value={row.base_ratio} />
                      </span>
                    </td>
                    <td
                      role='cell'
                      className={cn(
                        'flex items-center justify-between gap-3 py-3 @5xl/content:table-cell @5xl/content:border-b @5xl/content:px-3 @5xl/content:py-5 @5xl/content:text-right',
                        wide &&
                          '@5xl/content:flex @5xl/content:border-b-0 @5xl/content:px-0 @5xl/content:py-3'
                      )}
                    >
                      <span
                        className={cn(
                          'text-muted-foreground text-xs @5xl/content:hidden',
                          wide && '@5xl/content:block'
                        )}
                      >
                        {t('My wallet ratio')}
                      </span>
                      <RatioValue value={row.wallet_ratio} />
                    </td>
                    {props.columns.map((column) => (
                      <td
                        role='cell'
                        key={column.id}
                        className={cn(
                          'mt-2 flex items-center justify-between gap-4 rounded-lg bg-[#fbf7ed] px-3 py-3 @5xl/content:mt-0 @5xl/content:table-cell @5xl/content:rounded-none @5xl/content:border-b @5xl/content:border-[#e8d9b7] @5xl/content:px-4 @5xl/content:py-5 @5xl/content:text-right dark:bg-[#2d2a24] dark:@5xl/content:border-[#4a4132]',
                          wide &&
                            '@5xl/content:mt-2 @5xl/content:flex @5xl/content:rounded-lg @5xl/content:border-0 @5xl/content:px-3 @5xl/content:py-3'
                        )}
                      >
                        <span
                          className={cn(
                            'min-w-0 text-xs break-words text-[#876321] @5xl/content:hidden dark:text-[#dec387]',
                            wide && '@5xl/content:block'
                          )}
                        >
                          {column.name}
                        </span>
                        <span className='min-w-0 text-right'>
                          <MembershipRatioPrice quote={row.quotes[column.id]} />
                        </span>
                      </td>
                    ))}
                  </tr>
                  {open && (
                    <tr
                      role='row'
                      id={detailId}
                      className={cn(
                        'block @5xl/content:table-row',
                        wide && '@5xl/content:block'
                      )}
                    >
                      <td
                        role='cell'
                        colSpan={3 + props.columns.length}
                        className='bg-muted/45 block rounded-xl border p-4 @5xl/content:table-cell @5xl/content:rounded-none @5xl/content:border-x-0 @5xl/content:border-t-0 @5xl/content:px-5'
                      >
                        <MembershipRatioCalculation
                          baseRatio={row.base_ratio}
                          walletRatio={row.wallet_ratio}
                          quotes={props.columns.map((column) => ({
                            id: column.id,
                            name: column.name,
                            quote: row.quotes[column.id],
                          }))}
                        />
                      </td>
                    </tr>
                  )}
                </Fragment>
              )
            })}
          </tbody>
        </table>
      </div>
      {rows.length === 0 && (
        <p className='text-muted-foreground px-6 py-12 text-center text-sm'>
          {t('No matching groups')}
        </p>
      )}
      <p
        className='text-muted-foreground px-5 pt-4 text-xs sm:px-6'
        role='status'
      >
        {t('Showing {{count}} groups', { count: rows.length })}
      </p>
      <p className='text-muted-foreground px-5 py-5 text-xs leading-relaxed sm:px-6'>
        {t(
          'Savings are compared with your wallet price. Existing group discounts and membership discounts are not stacked.'
        )}
      </p>
    </>
  )
}

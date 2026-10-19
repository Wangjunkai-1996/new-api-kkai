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
import { ArrowUpRight01Icon, CrownIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button, buttonVariants } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'

import {
  MembershipRatioComparison,
  type MembershipRatioColumn,
  type MembershipRatioRow,
} from './components/ratio-comparison'
import { useMembership } from './hooks'

export function MembershipRatiosPage() {
  const { t, i18n } = useTranslation()
  const membership = useMembership()
  const [view, setView] = useState('offers')
  const catalog = membership.catalog.data
  const self = membership.self.data
  const account = self?.account
  const loading = membership.catalog.isPending || membership.self.isPending
  const error = membership.catalog.isError || membership.self.isError
  const runtimeEnabled =
    catalog?.runtime_enabled !== false && self?.runtime_enabled !== false
  const active =
    runtimeEnabled &&
    account?.status === 'active' &&
    account.expires_at > membership.now &&
    account.amount_used < account.amount_total
  const current = view === 'current'
  const tiers = [...(catalog?.tiers ?? [])]
    .filter((tier) => tier.enabled)
    .sort((a, b) => a.rank - b.rank)
  const currentColumn = {
    id: 'current',
    name: self?.tier?.name ?? t('Current membership'),
  }
  const columns: MembershipRatioColumn[] = current ? [currentColumn] : tiers
  const saleRows: MembershipRatioRow[] = (catalog?.group_quotes ?? []).map(
    (quote) => ({ ...quote, quotes: quote.tier_quotes })
  )
  const currentRows: MembershipRatioRow[] = active
    ? (self?.discounts ?? [])
        .filter((quote) => quote.enabled)
        .map((quote) => ({
          ...quote,
          quotes: {
            current: {
              coefficient: quote.coefficient,
              membership_ratio: quote.membership_ratio,
              savings: quote.savings ?? 0,
            },
          },
        }))
    : []
  const rows = current ? currentRows : saleRows
  const maximumSaving = rows.reduce(
    (highest, row) =>
      Math.max(
        highest,
        ...columns.map((column) => row.quotes[column.id]?.savings ?? 0)
      ),
    0
  )
  const saving = new Intl.NumberFormat(toIntlLocale(i18n.language), {
    style: 'percent',
    maximumFractionDigits: 2,
  }).format(maximumSaving)

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Membership ratios')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          onClick={() => void membership.refresh()}
          disabled={membership.catalog.isFetching || membership.self.isFetching}
        >
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-6xl space-y-7 pb-8'>
          <header className='flex flex-col justify-between gap-5 pt-2 sm:flex-row sm:items-end'>
            <div>
              <p className='text-xs font-medium tracking-widest text-[#876321] dark:text-[#dec387]'>
                {t('MEMBERSHIP')}
              </p>
              <h1 className='mt-2 text-3xl font-semibold tracking-tight'>
                {t('Membership ratios')}
              </h1>
              <p className='text-muted-foreground mt-3 text-sm leading-relaxed'>
                {t(
                  'Compare group prices and find the membership that fits your usage.'
                )}
              </p>
            </div>
            <Link
              to='/membership'
              className={buttonVariants({
                className:
                  'h-10 self-start bg-blue-50 px-4 text-blue-800 hover:bg-blue-100 sm:self-auto dark:bg-blue-400/10 dark:text-blue-200 dark:hover:bg-blue-400/20',
              })}
            >
              {t('View membership plans')}
              <HugeiconsIcon
                icon={ArrowUpRight01Icon}
                size={16}
                aria-hidden='true'
              />
            </Link>
          </header>
          {loading && <Skeleton className='h-96 w-full rounded-2xl' />}
          {error && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t(
                  'Membership information could not be loaded. Please refresh before purchasing.'
                )}
              </AlertDescription>
            </Alert>
          )}
          {!loading && !error && !runtimeEnabled && (
            <Alert>
              <AlertDescription>
                {t('Memberships are not available yet.')}
              </AlertDescription>
            </Alert>
          )}
          {!loading && !error && runtimeEnabled && catalog && self && (
            <>
              <section
                aria-label={t('Membership benefits summary')}
                className='bg-card flex flex-col gap-5 rounded-2xl border px-5 py-5 md:flex-row md:items-center md:gap-8 md:px-6'
              >
                <div className='flex min-w-0 flex-1 items-center gap-3 md:border-r md:pr-8'>
                  <span className='flex size-10 shrink-0 items-center justify-center rounded-xl bg-[#fbf7ed] text-[#876321] dark:bg-[#2d2a24] dark:text-[#dec387]'>
                    <HugeiconsIcon
                      icon={CrownIcon}
                      size={20}
                      aria-hidden='true'
                    />
                  </span>
                  <div className='min-w-0'>
                    <p className='text-muted-foreground text-xs'>
                      {t('My membership')}
                    </p>
                    <p className='mt-1 font-semibold break-words'>
                      {active ? currentColumn.name : t('No active membership')}
                    </p>
                  </div>
                </div>
                <dl className='grid flex-1 grid-cols-3 gap-3 border-t pt-4 md:border-t-0 md:pt-0'>
                  <div>
                    <dd className='text-xl font-semibold tabular-nums'>
                      {catalog.sales_enabled ? tiers.length : 0}
                    </dd>
                    <dt className='text-muted-foreground mt-1 text-xs'>
                      {t('Plans on sale')}
                    </dt>
                  </div>
                  <div>
                    <dd className='text-xl font-semibold tabular-nums'>
                      {saleRows.length}
                    </dd>
                    <dt className='text-muted-foreground mt-1 text-xs'>
                      {t('Available groups')}
                    </dt>
                  </div>
                  <div>
                    <dd className='text-xl font-semibold text-[#876321] tabular-nums dark:text-[#dec387]'>
                      {saving}
                    </dd>
                    <dt className='text-muted-foreground mt-1 text-xs'>
                      {t('Maximum savings vs wallet')}
                    </dt>
                  </div>
                </dl>
              </section>
              {!catalog.sales_enabled && (
                <Alert>
                  <AlertDescription>
                    {t(
                      'New membership purchases are temporarily unavailable. Existing benefits remain valid.'
                    )}
                  </AlertDescription>
                </Alert>
              )}
              <Tabs
                value={view}
                onValueChange={setView}
                className='bg-card gap-0 overflow-hidden rounded-2xl border'
              >
                <div className='flex flex-col justify-between gap-4 px-5 pt-6 sm:flex-row sm:items-center sm:px-6'>
                  <div>
                    <h2 className='text-lg font-semibold'>
                      {t('Group ratio comparison')}
                    </h2>
                    <p className='text-muted-foreground mt-1 text-xs'>
                      {t('Only billing groups available to you are shown.')}
                    </p>
                  </div>
                  <TabsList
                    aria-label={t('Quote scope')}
                    className='h-9 max-w-full self-start sm:self-auto'
                  >
                    <TabsTrigger value='offers' className='px-3'>
                      {t('New purchase offers')}
                    </TabsTrigger>
                    <TabsTrigger value='current' className='px-3'>
                      {t('My benefits')}
                    </TabsTrigger>
                  </TabsList>
                </div>
                <TabsContent value={view}>
                  <p className='mx-5 mt-5 rounded-lg bg-[#fbf7ed] px-3 py-3 text-xs leading-relaxed text-[#876321] sm:mx-6 dark:bg-[#2d2a24] dark:text-[#dec387]'>
                    {current
                      ? t(
                          'My benefits use your locked membership rules. New purchase offers may differ, even for the same plan.'
                        )
                      : t(
                          'These are the latest offers for new purchases and upgrades. Your current benefits are shown separately.'
                        )}
                  </p>
                  {current && !active ? (
                    <div className='px-6 py-12 text-center'>
                      <h3 className='font-medium'>
                        {t('No active membership benefits')}
                      </h3>
                      <p className='text-muted-foreground mx-auto mt-2 max-w-lg text-sm leading-relaxed'>
                        {t(
                          'No membership discount is active. Requests use your wallet at its existing group price.'
                        )}
                      </p>
                    </div>
                  ) : (
                    <MembershipRatioComparison
                      key={view}
                      rows={rows}
                      columns={columns}
                      current={current}
                    />
                  )}
                </TabsContent>
              </Tabs>
              <details className='bg-card group rounded-xl border'>
                <summary className='focus-visible:ring-ring cursor-pointer rounded-xl px-5 py-4 text-sm font-medium outline-none focus-visible:ring-2'>
                  {t('How membership ratios work')}
                </summary>
                <div className='text-muted-foreground space-y-3 px-5 pb-5 text-sm leading-relaxed'>
                  <p>
                    {t(
                      'A lower ratio means a lower request price. Membership discounts apply to the standard group ratio, while a lower existing wallet price is retained.'
                    )}
                  </p>
                  <p>
                    {t(
                      'Membership discounts apply only to subscription quota. After expiry or quota exhaustion, requests automatically use your wallet at its existing price.'
                    )}
                  </p>
                  <p>
                    {t(
                      'Subscription quota is used first. If it cannot cover the full request reservation, the entire request uses your wallet at your existing group price.'
                    )}
                  </p>
                  <p>
                    {t(
                      'Purchased memberships lock their discount coefficients. Final ratios follow current group base prices. New purchase offers and active benefits can differ.'
                    )}
                  </p>
                </div>
              </details>
            </>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

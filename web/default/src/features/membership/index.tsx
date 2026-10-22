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
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button, buttonVariants } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

import { CurrentMembership } from './components/current-membership'
import { MembershipDiscounts } from './components/discount-comparison'
import { MembershipOrders } from './components/orders'
import { MembershipPurchaseDialog } from './components/purchase-dialog'
import { MembershipTierCard } from './components/tier-card'
import { useMembership } from './hooks'
import type { MembershipTier } from './types'

export function MembershipCenter() {
  const { t } = useTranslation()
  const membership = useMembership()
  const [selectedTier, setSelectedTier] = useState<MembershipTier | null>(null)
  const catalog = membership.catalog.data
  const self = membership.self.data
  const loading = membership.catalog.isPending || membership.self.isPending
  const error = membership.catalog.isError || membership.self.isError
  const tiers = [...(catalog?.tiers ?? [])]
    .filter((tier) => tier.enabled)
    .sort((a, b) => a.rank - b.rank)
  const runtimeEnabled =
    catalog?.runtime_enabled !== false && self?.runtime_enabled !== false
  const active =
    self?.account?.status === 'active' &&
    self.account.expires_at > membership.now

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Membership center')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Link
          to='/membership/ratios'
          className={buttonVariants({ variant: 'outline' })}
        >
          {t('Membership ratios')}
        </Link>
        <Button
          variant='outline'
          onClick={() => void membership.refresh()}
          disabled={membership.self.isFetching || membership.catalog.isFetching}
        >
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-7 pb-8'>
          <header className='max-w-2xl pt-2'>
            <p className='text-muted-foreground text-xs font-medium tracking-widest'>
              {t('MEMBERSHIP')}
            </p>
            <h1 className='mt-3 text-3xl font-semibold tracking-tight sm:text-4xl'>
              {t('A better price for every call.')}
            </h1>
            <p className='text-muted-foreground mt-3 text-sm leading-relaxed'>
              {t(
                'Prepay a shared quota, enjoy group-specific discounts, and keep your wallet ready for continuous use.'
              )}
            </p>
          </header>
          {loading && (
            <>
              <Skeleton className='h-44 w-full rounded-2xl' />
              <div className='grid gap-5 md:grid-cols-3'>
                {['one', 'two', 'three'].map((key) => (
                  <Skeleton key={key} className='h-80 rounded-2xl' />
                ))}
              </div>
            </>
          )}
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
              <CurrentMembership membership={self} now={membership.now} />
              {!catalog.sales_enabled && (
                <Alert>
                  <AlertDescription>
                    {t(
                      'New membership purchases are temporarily unavailable. Existing benefits remain valid.'
                    )}
                  </AlertDescription>
                </Alert>
              )}
              <section
                aria-label={t('Membership plans')}
                className='grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3'
              >
                {tiers.map((tier) => (
                  <MembershipTierCard
                    key={tier.id}
                    tier={tier}
                    salesEnabled={catalog.sales_enabled}
                    onPurchase={setSelectedTier}
                  />
                ))}
              </section>
              <MembershipDiscounts
                catalog={catalog}
                currentDiscounts={self.discounts}
                active={active}
              />
              <details className='bg-card rounded-xl border p-5'>
                <summary className='cursor-pointer font-medium'>
                  {t('How membership billing works')}
                </summary>
                <div className='text-muted-foreground mt-4 space-y-3 text-sm leading-relaxed'>
                  <p>
                    {t(
                      'Subscription quota is used first. If it cannot cover the full request reservation, the entire request uses your wallet at your existing group price.'
                    )}
                  </p>
                  <p>
                    {t(
                      'Unused subscription quota stays available for smaller requests. Wallet and subscription quota are never mixed for one request.'
                    )}
                  </p>
                  <p>
                    {t(
                      'Quota expires after 30 days and is not reset or renewed automatically. A failed request returns unused reserved quota to its original source without extending validity.'
                    )}
                  </p>
                </div>
              </details>
              <MembershipOrders />
            </>
          )}
        </div>
        {selectedTier && self && catalog && (
          <MembershipPurchaseDialog
            key={selectedTier.id}
            tier={
              catalog.tiers.find((tier) => tier.id === selectedTier.id) ??
              selectedTier
            }
            membership={self}
            releaseId={catalog.release_id}
            salesEnabled={catalog.sales_enabled && !error}
            onClose={() => setSelectedTier(null)}
            onRefresh={() => void membership.refresh()}
            onOrderCreated={() => void membership.refresh()}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

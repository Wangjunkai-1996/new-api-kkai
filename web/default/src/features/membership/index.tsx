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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Info, RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { getWalletBalance } from '@/features/wallet/api'
import { useWalletPaymentIdentity } from '@/features/wallet/hooks/use-wallet-payment-flow'
import { formatQuota } from '@/lib/format'

import { getMembershipOrder } from './api'
import { CurrentMembership } from './components/current-membership'
import { MembershipDiscounts } from './components/discount-comparison'
import { MembershipOrders } from './components/orders'
import { MembershipPurchaseDialog } from './components/purchase-dialog'
import { MembershipSuccessDialog } from './components/success-dialog'
import { MembershipTierCard } from './components/tier-card'
import { useMembership } from './hooks'
import type { MembershipOrder, MembershipTier } from './types'

import '@/styles/membership-page.css'

export function MembershipCenter(props: {
  initialTradeNo?: string
  onPaymentReturnHandled?: () => void
}) {
  const { t } = useTranslation()
  const membership = useMembership()
  const client = useQueryClient()
  const refetchSelf = membership.self.refetch
  const refetchCatalog = membership.catalog.refetch
  const [selectedTier, setSelectedTier] = useState<MembershipTier | null>(null)
  const [confirmedOrder, setConfirmedOrder] = useState<MembershipOrder | null>(
    null
  )
  const [returnRefreshFailed, setReturnRefreshFailed] = useState(false)
  const [returnRefreshing, setReturnRefreshing] = useState(false)
  const [view, setView] = useState<'overview' | 'orders'>('overview')
  const walletIdentity = useWalletPaymentIdentity()
  const wallet = useQuery({
    queryKey: ['wallet-payment', ...walletIdentity, 'balance'],
    enabled: walletIdentity.every((part) => part !== undefined && part !== ''),
    queryFn: async ({ signal }) => {
      const response = await getWalletBalance(signal)
      if (!response.success || !response.data) {
        throw new Error('Wallet balance unavailable')
      }
      return response.data
    },
    retry: false,
    meta: { errorToast: false },
  })
  const walletQuota = wallet.data?.quota
  const handledReturn = useRef(false)
  const returnedOrder = useQuery({
    queryKey: [
      'membership',
      membership.userId,
      'return-order',
      props.initialTradeNo,
    ],
    queryFn: () => getMembershipOrder(props.initialTradeNo ?? ''),
    enabled: !!props.initialTradeNo,
    refetchInterval: (query) =>
      query.state.data?.status === 'pending' ? 5000 : false,
    refetchIntervalInBackground: false,
  })
  const confirmReturnedOrder = useCallback(
    async (order: MembershipOrder) => {
      setReturnRefreshFailed(false)
      setReturnRefreshing(true)
      try {
        const [selfResult, catalogResult] = await Promise.all([
          refetchSelf(),
          refetchCatalog(),
        ])
        const self = selfResult.data
        const account = self?.account
        if (
          selfResult.isError ||
          catalogResult.isError ||
          !catalogResult.data ||
          !account ||
          !order.account_id ||
          account.id !== order.account_id ||
          account.status !== 'active' ||
          account.expires_at <= self.server_time
        ) {
          setReturnRefreshFailed(true)
          return
        }
        void client.invalidateQueries({ queryKey: ['pricing'] })
        setConfirmedOrder(order)
      } catch {
        setReturnRefreshFailed(true)
      } finally {
        setReturnRefreshing(false)
      }
    },
    [client, refetchCatalog, refetchSelf]
  )
  useEffect(() => {
    const order = returnedOrder.data
    if (order?.status !== 'success' || handledReturn.current) return
    handledReturn.current = true
    void confirmReturnedOrder(order)
  }, [returnedOrder.data, confirmReturnedOrder])
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
    self.account.expires_at > membership.now &&
    self.account.amount_used < self.account.amount_total
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='text-2xl font-semibold'>{t('Membership center')}</span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {view === 'overview' && (
          <a
            href='#membership-billing-guide'
            className='text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 text-xs'
            onClick={() => {
              const guide = document.querySelector<HTMLDetailsElement>(
                '#membership-billing-guide'
              )
              if (guide) guide.open = true
            }}
          >
            <Info className='size-4' aria-hidden='true' />
            {t('How membership billing works')}
          </a>
        )}
        <Button
          variant='ghost'
          size='icon'
          aria-label={t('Refresh')}
          title={t('Refresh')}
          onClick={() => {
            void membership.refresh()
            void wallet.refetch()
          }}
          disabled={membership.self.isFetching || membership.catalog.isFetching}
        >
          <RefreshCw aria-hidden='true' />
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='membership-page-enter flex w-full flex-col gap-6 pb-8'>
          <header>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Your membership status, available quota, and pricing benefits.'
              )}
            </p>
            <nav
              className='mt-5 flex gap-6 border-b text-sm'
              aria-label={t('Membership center')}
            >
              <button
                type='button'
                className={`-mb-px border-b-2 pb-3 ${view === 'overview' ? 'border-foreground font-medium' : 'text-muted-foreground hover:text-foreground border-transparent'}`}
                aria-current={view === 'overview' ? 'page' : undefined}
                onClick={() => setView('overview')}
              >
                {t('Membership overview')}
              </button>
              <Link
                to='/usage-logs/$section'
                params={{ section: 'common' }}
                className='text-muted-foreground hover:text-foreground -mb-px border-b-2 border-transparent pb-3'
              >
                {t('Usage logs')}
              </Link>
              <button
                type='button'
                className={`-mb-px border-b-2 pb-3 ${view === 'orders' ? 'border-foreground font-medium' : 'text-muted-foreground hover:text-foreground border-transparent'}`}
                aria-current={view === 'orders' ? 'page' : undefined}
                onClick={() => setView('orders')}
              >
                {t('Membership orders')}
              </button>
            </nav>
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
          {returnRefreshFailed && (
            <Alert variant='destructive'>
              <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
                <span>
                  {t(
                    'Payment confirmed, but your membership status could not be verified.'
                  )}
                </span>
                <Button
                  variant='outline'
                  size='sm'
                  disabled={returnRefreshing}
                  onClick={() => {
                    if (returnedOrder.data?.status === 'success') {
                      void confirmReturnedOrder(returnedOrder.data)
                    }
                  }}
                >
                  {t('Refresh')}
                </Button>
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
          {!loading &&
            !error &&
            !returnRefreshFailed &&
            runtimeEnabled &&
            catalog &&
            self && (
              <>
                {props.initialTradeNo && returnedOrder.isPending && (
                  <Alert>
                    <AlertDescription>
                      {t('Checking the returned payment order...')}
                    </AlertDescription>
                  </Alert>
                )}
                {props.initialTradeNo && returnedOrder.isError && (
                  <Alert variant='destructive'>
                    <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
                      <span>{t('Could not load membership orders.')}</span>
                      <Button
                        variant='outline'
                        size='sm'
                        onClick={() => void returnedOrder.refetch()}
                      >
                        {t('Refresh')}
                      </Button>
                    </AlertDescription>
                  </Alert>
                )}
                {returnedOrder.data?.status === 'pending' && (
                  <Alert>
                    <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
                      <span>{t('Awaiting payment confirmation')}</span>
                      <Button
                        variant='outline'
                        size='sm'
                        onClick={() => void returnedOrder.refetch()}
                      >
                        {t('Refresh')}
                      </Button>
                    </AlertDescription>
                  </Alert>
                )}
                {(returnedOrder.data?.status === 'paid_exception' ||
                  returnedOrder.data?.status === 'closed') && (
                  <Alert
                    variant={
                      returnedOrder.data.status === 'paid_exception'
                        ? 'destructive'
                        : 'default'
                    }
                  >
                    <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
                      <span>
                        {returnedOrder.data.status === 'paid_exception'
                          ? t('Payment received, needs review')
                          : t('Order closed')}
                      </span>
                      <Button
                        variant='outline'
                        size='sm'
                        onClick={props.onPaymentReturnHandled}
                      >
                        {t('Close')}
                      </Button>
                    </AlertDescription>
                  </Alert>
                )}
                {view === 'overview' && (
                  <>
                    <CurrentMembership membership={self} now={membership.now} />
                    <div className='text-muted-foreground flex flex-wrap items-center justify-between gap-2 px-1 text-xs'>
                      <span>
                        {t('Wallet')}
                        {walletQuota !== undefined
                          ? ` ${formatQuota(walletQuota)}`
                          : ''}
                        {' · '}
                        {t(
                          'Your existing group price is retained when the wallet pays.'
                        )}
                      </span>
                      <Link
                        to='/membership/ratios'
                        className='hover:text-foreground'
                      >
                        {t('Membership ratios')} →
                      </Link>
                    </div>
                  </>
                )}
                {view === 'overview' && !catalog.sales_enabled && (
                  <Alert>
                    <AlertDescription>
                      {t(
                        'New membership purchases are temporarily unavailable. Existing benefits remain valid.'
                      )}
                    </AlertDescription>
                  </Alert>
                )}
                {view === 'overview' && (
                  <>
                    <section
                      className='space-y-4'
                      aria-label={t('Membership plans')}
                    >
                      <div className='flex flex-wrap items-center justify-between gap-2'>
                        <h2 className='text-base font-semibold'>
                          {t('Membership plans')}
                        </h2>
                        <Link
                          to='/membership/ratios'
                          className='text-muted-foreground hover:text-foreground text-xs'
                        >
                          {t('Membership ratios')} →
                        </Link>
                      </div>
                      <div className='grid grid-cols-1 gap-3 md:grid-cols-3'>
                        {tiers.map((tier) => (
                          <MembershipTierCard
                            key={tier.id}
                            tier={tier}
                            salesEnabled={catalog.sales_enabled}
                            onPurchase={setSelectedTier}
                          />
                        ))}
                      </div>
                    </section>
                    <MembershipDiscounts
                      catalog={catalog}
                      currentDiscounts={self.discounts}
                      currentTier={active ? self.tier : null}
                      currentTierId={active ? self.account?.tier_id : undefined}
                    />
                    <details
                      id='membership-billing-guide'
                      className='border-t pt-4'
                    >
                      <summary className='cursor-pointer text-sm font-medium'>
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
                  </>
                )}
                {view === 'orders' && <MembershipOrders />}
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
            onRefresh={() => {
              void membership.refresh()
              void wallet.refetch()
            }}
            onOrderCreated={(order) => {
              void membership.refresh()
              if (!order.payment_url && !order.submit_url) setView('orders')
            }}
          />
        )}
        {confirmedOrder && self && (
          <MembershipSuccessDialog
            order={confirmedOrder}
            membership={self}
            onClose={() => {
              setConfirmedOrder(null)
              props.onPaymentReturnHandled?.()
            }}
            onViewMembership={() => {
              setView('overview')
              setConfirmedOrder(null)
              props.onPaymentReturnHandled?.()
            }}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

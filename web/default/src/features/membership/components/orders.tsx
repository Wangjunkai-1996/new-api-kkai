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
import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useSiteConfig } from '@/features/site-config'
import { toIntlLocale } from '@/i18n/languages'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  checkMembershipPayment,
  getMembershipOrders,
  resumeMembershipPayment,
} from '../api'
import {
  formatMembershipPrice,
  isMembershipOrderPending,
  openMembershipCheckout,
} from '../lib'
import type { MembershipOrder } from '../types'
import { ResolveMembershipOrderDialog } from './resolve-order-dialog'

export function MembershipOrderStatus(props: { order: MembershipOrder }) {
  const { t } = useTranslation()
  let label = t('Payment status unknown')
  if (props.order.payment_state === 'processing') {
    label = t('Payment confirmed, activating membership')
  } else if (props.order.payment_state === 'review') {
    label = t('Payment verification needs review')
  } else if (props.order.status === 'success') {
    label = t('Membership activated')
  } else if (
    props.order.status === 'pending' ||
    props.order.payment_state === 'checking'
  ) {
    label = t('Awaiting payment confirmation')
  } else if (props.order.status === 'closed') label = t('Order closed')
  else if (props.order.status === 'paid_exception') {
    label = t('Payment received, needs review')
  }
  return (
    <Badge
      variant={
        props.order.status === 'paid_exception' ||
        props.order.payment_state === 'review'
          ? 'destructive'
          : 'secondary'
      }
    >
      {label}
    </Badge>
  )
}

export function MembershipOrders(props: { admin?: boolean }) {
  const { t, i18n } = useTranslation()
  const { data: site } = useSiteConfig()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const client = useQueryClient()
  const previous = useRef<string | null>(null)
  const [reviewing, setReviewing] = useState<MembershipOrder | null>(null)
  const [adminView, setAdminView] = useState<'recent' | 'exceptions'>('recent')
  const status =
    props.admin && adminView === 'exceptions' ? 'paid_exception' : undefined
  const check = useMutation({
    mutationFn: (tradeNo: string) =>
      checkMembershipPayment(tradeNo, !!props.admin),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['membership', userId] })
      toast.success(
        t('Payment check requested. Confirmation will update automatically.')
      )
    },
    onError: (error) => handleServerError(error, t('Could not check payment.')),
  })
  const resume = useMutation({
    mutationFn: resumeMembershipPayment,
    onSuccess: (checkout) => {
      openMembershipCheckout(checkout)
    },
    onError: (error) => handleServerError(error, t('Payment request failed')),
  })
  const queryKey = [
    'membership',
    userId,
    'orders',
    !!props.admin,
    status ?? 'recent',
  ]
  const orders = useInfiniteQuery({
    queryKey,
    initialPageParam: undefined as number | undefined,
    queryFn: async ({ pageParam }) => {
      if (!props.admin) return getMembershipOrders(false)
      const page = await getMembershipOrders(true, status, pageParam)
      let previousId = pageParam ?? Infinity
      for (const order of page) {
        if (
          !Number.isSafeInteger(order.id) ||
          order.id <= 0 ||
          order.id >= previousId
        ) {
          return null
        }
        previousId = order.id
      }
      return page
    },
    getNextPageParam: (lastPage) =>
      props.admin && lastPage?.length === 50 ? lastPage.at(-1)?.id : undefined,
    refetchInterval: (query) =>
      query.state.data?.pages.some((page) =>
        page?.some(isMembershipOrderPending)
      )
        ? 5000
        : false,
    refetchIntervalInBackground: false,
  })
  const orderList = orders.data?.pages.flatMap((page) => page ?? []) ?? []
  const hasInvalidPage = orders.data?.pages.at(-1) === null
  const states = orderList
    .map((order) => `${order.id}:${order.status}`)
    .join(',')
  useEffect(() => {
    if (!states) return
    if (previous.current !== null && previous.current !== states) {
      void client.invalidateQueries({
        queryKey: ['membership', userId, 'self'],
      })
      void client.invalidateQueries({
        queryKey: ['membership', userId, 'catalog'],
      })
      void client.invalidateQueries({ queryKey: ['pricing'] })
    }
    previous.current = states
  }, [states, client, userId])
  return (
    <section className='space-y-4'>
      <div className='flex items-center justify-between gap-3'>
        <h2 className='text-lg font-semibold'>
          {props.admin ? t('Membership orders') : t('My membership orders')}
        </h2>
        <Button
          variant='outline'
          size='sm'
          onClick={() => void client.resetQueries({ queryKey, exact: true })}
          disabled={orders.isFetching}
        >
          {t('Refresh')}
        </Button>
      </div>
      {props.admin && (
        <div
          className='flex gap-2'
          role='group'
          aria-label={t('Membership orders')}
        >
          <Button
            type='button'
            size='sm'
            variant={adminView === 'recent' ? 'default' : 'outline'}
            aria-pressed={adminView === 'recent'}
            onClick={() => setAdminView('recent')}
          >
            {t('Recent')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant={adminView === 'exceptions' ? 'default' : 'outline'}
            aria-pressed={adminView === 'exceptions'}
            onClick={() => setAdminView('exceptions')}
          >
            {t('Payment exceptions')}
          </Button>
        </div>
      )}
      {orders.isPending && <Skeleton className='h-24 w-full' />}
      {hasInvalidPage && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Pagination')}: {t('Unavailable')}
          </AlertDescription>
        </Alert>
      )}
      {orders.isError && (
        <Alert variant='destructive'>
          <AlertDescription>
            <div className='flex items-center justify-between gap-3'>
              <span>{t('Could not load membership orders.')}</span>
              <Button
                size='sm'
                variant='outline'
                onClick={() =>
                  void (orders.isFetchNextPageError
                    ? orders.fetchNextPage()
                    : orders.refetch())
                }
                disabled={orders.isFetching}
              >
                {t('Retry')}
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}
      {!orders.isPending &&
        !orders.isError &&
        !hasInvalidPage &&
        !orderList.length && (
          <p className='text-muted-foreground rounded-xl border p-5 text-sm'>
            {status ? t('No orders found') : t('No membership orders yet.')}
          </p>
        )}
      <div className='space-y-3'>
        {orderList.map((order) => (
          <article key={order.id} className='bg-card rounded-xl border p-4'>
            <div className='flex flex-wrap items-center justify-between gap-3'>
              <div>
                <p className='font-medium'>
                  {order.tier_name || order.tier_id}
                </p>
                <p className='text-muted-foreground mt-1 text-xs break-all'>
                  {order.trade_no}
                </p>
              </div>
              <MembershipOrderStatus order={order} />
            </div>
            <div className='text-muted-foreground mt-3 flex flex-wrap justify-between gap-2 text-sm'>
              <span>
                {formatMembershipPrice(
                  order.price_minor,
                  order.currency,
                  i18n.language
                )}
              </span>
              <span>
                {new Date(order.created_at * 1000).toLocaleString(
                  toIntlLocale(i18n.language)
                )}
              </span>
            </div>
            {order.status === 'paid_exception' &&
              order.payment_state !== 'review' &&
              order.payment_state !== 'processing' && (
                <p className='mt-3 text-sm'>
                  {t(
                    'Payment is recorded. An administrator will review this order; do not pay again.'
                  )}
                </p>
              )}
            {order.checkout_state === 'creating' &&
              order.status === 'pending' && (
                <p className='text-muted-foreground mt-2 text-sm'>
                  {t(
                    'Payment confirmation is pending. Please wait before paying again.'
                  )}
                </p>
              )}
            {props.admin && (
              <p className='text-muted-foreground mt-2 text-xs'>
                {t('User ID')}: {order.user_id} {order.exception_reason}
              </p>
            )}
            {order.resolution_note && (
              <p className='text-muted-foreground mt-2 text-xs'>
                {order.resolution_note}
              </p>
            )}
            {!props.admin && site?.tenant_id === 0 && (
              <Button
                variant='outline'
                size='sm'
                className='mt-3'
                render={
                  <Link
                    to='/invoices/new'
                    search={{
                      source_type: 'membership',
                      order_id: order.trade_no,
                    }}
                  />
                }
              >
                {t('Apply for invoice')}
              </Button>
            )}
            {order.can_check_payment && (
              <Button
                variant='outline'
                size='sm'
                className='mt-3'
                disabled={check.isPending || resume.isPending}
                onClick={() => check.mutate(order.trade_no)}
              >
                {t('Check payment')}
              </Button>
            )}
            {!props.admin && order.status === 'pending' && (
              <Button
                variant='outline'
                size='sm'
                className='mt-3'
                disabled={
                  resume.isPending ||
                  check.isPending ||
                  order.checkout_state === 'creating' ||
                  order.payment_state === 'processing' ||
                  order.payment_state === 'review'
                }
                onClick={() => resume.mutate(order.trade_no)}
              >
                {t('Continue payment')}
              </Button>
            )}
            {props.admin &&
              (order.status === 'paid_exception' ||
                order.payments?.some(
                  (payment) => payment.status === 'paid_exception'
                )) && (
                <Button
                  variant='outline'
                  size='sm'
                  className='mt-3'
                  onClick={() => setReviewing(order)}
                >
                  {t('Review payment')}
                </Button>
              )}
          </article>
        ))}
      </div>
      {props.admin && orders.hasNextPage && !orders.isFetchNextPageError && (
        <Button
          variant='outline'
          onClick={() => void orders.fetchNextPage()}
          disabled={orders.isFetching}
        >
          {orders.isFetchingNextPage ? t('Loading...') : t('Load more')}
        </Button>
      )}
      {reviewing && (
        <ResolveMembershipOrderDialog
          key={reviewing.id}
          order={reviewing}
          onClose={() => setReviewing(null)}
        />
      )}
    </section>
  )
}

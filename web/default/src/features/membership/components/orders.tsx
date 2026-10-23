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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useSiteConfig } from '@/features/site-config'
import { toIntlLocale } from '@/i18n/languages'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { getMembershipOrders, resumeMembershipPayment } from '../api'
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
  if (props.order.status === 'pending') {
    label = t('Awaiting payment confirmation')
  } else if (props.order.status === 'success') label = t('Membership activated')
  else if (props.order.status === 'closed') label = t('Order closed')
  else if (props.order.status === 'paid_exception') {
    label = t('Payment received, needs review')
  }
  return (
    <Badge
      variant={
        props.order.status === 'paid_exception' ? 'destructive' : 'secondary'
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
  const resume = useMutation({
    mutationFn: resumeMembershipPayment,
    onSuccess: (checkout) => {
      openMembershipCheckout(checkout)
    },
    onError: (error) => handleServerError(error, t('Payment request failed')),
  })
  const orders = useQuery({
    queryKey: ['membership', userId, 'orders', !!props.admin],
    queryFn: () => getMembershipOrders(props.admin),
    refetchInterval: (query) =>
      query.state.data?.some(isMembershipOrderPending) ? 5000 : false,
    refetchIntervalInBackground: false,
  })
  const states = orders.data
    ?.map((order) => `${order.id}:${order.status}`)
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
          onClick={() => void orders.refetch()}
          disabled={orders.isFetching}
        >
          {t('Refresh')}
        </Button>
      </div>
      {orders.isPending && <Skeleton className='h-24 w-full' />}
      {orders.isError && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Could not load membership orders.')}
          </AlertDescription>
        </Alert>
      )}
      {!orders.isPending && !orders.isError && !orders.data?.length && (
        <p className='text-muted-foreground rounded-xl border p-5 text-sm'>
          {t('No membership orders yet.')}
        </p>
      )}
      <div className='space-y-3'>
        {orders.data?.map((order) => (
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
            {order.status === 'paid_exception' && (
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
            {!props.admin && order.status === 'pending' && (
              <Button
                variant='outline'
                size='sm'
                className='mt-3'
                disabled={
                  resume.isPending || order.checkout_state === 'creating'
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

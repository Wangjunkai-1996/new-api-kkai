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
import { ShoppingBag01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { formatPaymentMoney } from '@/features/wallet/lib/payment-flow'
import { toIntlLocale } from '@/i18n/languages'

import { ShopError } from '../api'
import { moneySchema, type Money, type Order } from '../schemas'

export function ShopHeading(props: {
  eyebrow?: string
  title: string
  description?: string
  children?: ReactNode
}) {
  return (
    <header className='shop-heading'>
      <div>
        {props.eyebrow && <p className='shop-eyebrow'>{props.eyebrow}</p>}
        <h1>{props.title}</h1>
        {props.description && <p className='shop-muted'>{props.description}</p>}
      </div>
      {props.children}
    </header>
  )
}
export function MoneyText(props: { money?: Money | null; className?: string }) {
  const { i18n } = useTranslation()
  return (
    <span className={props.className ?? 'shop-money'}>
      {formatPaymentMoney(
        moneySchema.safeParse(props.money).success
          ? (props.money ?? null)
          : null,
        i18n.language
      )}
    </span>
  )
}
export function ShopTime(props: { value?: string | null }) {
  const { i18n } = useTranslation()
  if (!props.value || !Number.isFinite(Date.parse(props.value))) {
    return <span>—</span>
  }
  return (
    <time dateTime={props.value}>
      {new Intl.DateTimeFormat(toIntlLocale(i18n.language), {
        dateStyle: 'medium',
        timeStyle: 'short',
        timeZone: 'UTC',
      }).format(new Date(props.value))}{' '}
      UTC
    </time>
  )
}
export function ShopErrorNotice(props: { error: unknown; retry?: () => void }) {
  const { t } = useTranslation()
  if (!props.error) return null
  const code = props.error instanceof ShopError ? props.error.code : ''
  let message = t('Unable to load this information. Please try again.')
  if (code === 'SHOP_PRODUCT_CHANGED' || code === 'SHOP_VERSION_CONFLICT') {
    message = t(
      'This information has changed. Refresh and review it before continuing.'
    )
  }
  if (code === 'SHOP_PAYMENT_UNRESOLVED') {
    message = t(
      'The previous request is unresolved. Keep the original request and do not pay again.'
    )
  }
  if (
    code === 'SHOP_IDEMPOTENCY_CONFLICT' ||
    code === 'SHOP_REQUEST_UNRESOLVED'
  ) {
    message = t(
      'The previous operation is unresolved. Check its result or retry with the original information.'
    )
  }
  if (code === 'SHOP_REVEAL_EXPIRED') {
    message = t('The viewing period has ended.')
  }
  if (code === 'SHOP_REVEAL_BLOCKED') {
    message = t(
      'Delivery details are temporarily unavailable. Check the order for the next step.'
    )
  }
  if (code === 'SHOP_OUT_OF_STOCK' || code === 'SHOP_CAPACITY_FULL') {
    message = t('This product is currently unavailable.')
  }
  if (code === 'SHOP_NOT_FOUND') {
    message = t('This item does not exist or is not available to you.')
  }
  if (code === 'SHOP_VALIDATION_FAILED') {
    message = t(
      'Check the required fields and try again. Sensitive values are not shown in errors.'
    )
  }
  if (code === 'SHOP_NEW_ORDERS_PAUSED') {
    message = t(
      'New purchases are paused. Your existing orders remain available.'
    )
  }
  if (
    code === 'SHOP_CHANNEL_UNAVAILABLE' ||
    code === 'SHOP_PAYMENT_UNAVAILABLE'
  ) {
    message = t('No payment method is currently available.')
  }
  if (props.error instanceof ShopError && props.error.status === 403) {
    message = t(
      'This action requires an authorized session and current verification.'
    )
  }
  return (
    <Alert variant='destructive' role='alert'>
      <AlertTitle>{t('Unable to complete this action')}</AlertTitle>
      <AlertDescription>
        {message}
        {props.retry && (
          <Button variant='outline' onClick={props.retry}>
            {t('Refresh')}
          </Button>
        )}
      </AlertDescription>
    </Alert>
  )
}
export function ShopLoading() {
  return (
    <div className='shop-loading' aria-busy='true'>
      <Skeleton className='h-10 w-1/2' />
      <Skeleton className='h-64 w-full' />
    </div>
  )
}
export function ShopEmpty(props: { title?: string; description?: string }) {
  const { t } = useTranslation()
  return (
    <Empty>
      <EmptyMedia variant='icon'>
        <HugeiconsIcon icon={ShoppingBag01Icon} aria-hidden='true' />
      </EmptyMedia>
      <EmptyHeader>
        <EmptyTitle>{props.title ?? t('Nothing here yet')}</EmptyTitle>
        <EmptyDescription>
          {props.description ??
            t('Your items will appear here when they are available.')}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}
export function OrderStatus(props: { order: Pick<Order, 'display_status'> }) {
  const { t } = useTranslation()
  const labels = {
    awaiting_payment: t('Awaiting payment'),
    payment_verifying: t('Verifying payment'),
    closed: t('Closed'),
    pending_delivery: t('Awaiting delivery'),
    processing: t('Processing'),
    awaiting_input: t('Additional information required'),
    delivery_exception: t('Delivery needs attention'),
    delivered: t('Delivered'),
    aftersale: t('After-sales support'),
    refund_pending: t('Refund being processed'),
    refund_failed: t('Refund needs attention'),
    refunded: t('Refunded'),
  }
  return <Badge variant='secondary'>{labels[props.order.display_status]}</Badge>
}
export function ShopState(props: { state?: string }) {
  const { t } = useTranslation()
  const labels: Record<string, string> = {
    available: t('Available'),
    reserved: t('Reserved'),
    sold: t('Delivered'),
    delivered: t('Delivered'),
    quarantined: t('Quarantined'),
    released: t('Released'),
    pending: t('Pending'),
    review: t('Awaiting review'),
    matched: t('Matched'),
    extra: t('Additional payments'),
    registered: t('Registered'),
    ready: t('Ready'),
    unknown: t('Unknown'),
    approved: t('Approved'),
    cancelled: t('Cancelled'),
    failed: t('Failed'),
    succeeded: t('Succeeded'),
    processing: t('Processing'),
    closed: t('Closed'),
    paid: t('Paid'),
    unpaid: t('Unpaid'),
    disputed: t('Disputed'),
    rejected: t('Rejected'),
  }
  return <span>{labels[props.state ?? ''] ?? t('Unknown')}</span>
}
export function ShopEventSummary(props: {
  event: { event_type: string; public_summary: string }
}) {
  const { t } = useTranslation()
  const labels: Record<string, string> = {
    order_created: t('Order created'),
    order_closed: t('Order closed'),
    order_expired: t('Order expired'),
    payment_attempt_created: t('Payment started'),
    payment_received: t('Payment received'),
    delivered: t('Delivered'),
    delivery_exception: t('Delivery needs attention'),
    buyer_input_updated: t('Buyer information updated'),
    claim_order: t('Order claimed'),
    request_input: t('Additional information required'),
    retry_delivery: t('Delivery retry scheduled'),
    aftersale_created: t('Support request'),
    aftersale_message: t('After-sales support'),
    aftersale_accept: t('After-sales support'),
    aftersale_resolve: t('Resolved'),
    aftersale_reject: t('Rejected'),
    aftersale_close: t('Closed'),
    refund_approved: t('Refund approved'),
    refund_progress: t('Refund being processed'),
  }
  const summary = props.event.public_summary.trim()
  return (
    <span>
      {summary && props.event.event_type !== 'delivery_exception'
        ? summary
        : (labels[props.event.event_type] ?? t('Order updated'))}
    </span>
  )
}
export function Pagination(props: {
  page: number
  total: number
  pageSize?: number
  onPage: (page: number) => void
}) {
  const { t } = useTranslation()
  return (
    <nav className='shop-pagination' aria-label={t('Pagination')}>
      <Button
        variant='outline'
        disabled={props.page <= 1}
        onClick={() => props.onPage(props.page - 1)}
      >
        {t('Previous')}
      </Button>
      <span>{t('Page {{page}}', { page: props.page })}</span>
      <Button
        variant='outline'
        disabled={props.page * (props.pageSize ?? 20) >= props.total}
        onClick={() => props.onPage(props.page + 1)}
      >
        {t('Next')}
      </Button>
    </nav>
  )
}

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
import { Copy, ExternalLink, RefreshCw } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'

import { formatTimestamp } from '../lib/billing'
import {
  formatPaymentMoney,
  isLocallyEnded,
  PAYMENT_STATUS_LABELS,
} from '../lib/payment-flow'
import type { PaymentOrder, WalletState } from '../payment-flow-types'
import { PaymentAmounts } from './payment-amounts'

export function CurrentTopupOrder(props: {
  wallet: WalletState
  busy: boolean
  notice?: string
  onResume: () => void
  onEnd: () => void
  onReconcile: () => void
  onAcknowledge: () => void
  onHistory: () => void
  onUnfinishedHistory?: () => void
}) {
  const { t } = useTranslation()
  const [confirmEnd, setConfirmEnd] = useState(false)
  const [deadlineReached, setDeadlineReached] = useState(false)
  const heading = useRef<HTMLHeadingElement>(null)
  const order = props.wallet.flow.order
  useEffect(() => {
    heading.current?.focus()
  }, [order?.trade_no, props.wallet.flow.phase])
  const deadline = order?.deadline_at
  const resumable = order?.allowed_actions.includes('resume') ?? false
  useEffect(() => {
    if (!deadline || !resumable) return
    const timeout = window.setTimeout(
      () => setDeadlineReached(true),
      Math.max(0, (deadline - props.wallet.server_time) * 1000)
    )
    return () => window.clearTimeout(timeout)
  }, [deadline, resumable, props.wallet.server_time])
  if (!order) return null
  const final =
    props.wallet.flow.phase === 'result' &&
    order.allowed_actions.includes('acknowledge')
  const ended = isLocallyEnded(order)
  const received =
    order.display_status === 'success' ||
    order.display_status === 'credit_review'
  return (
    <section
      className='bg-card mx-auto w-full max-w-2xl rounded-xl border p-5 shadow-sm sm:p-8'
      aria-busy={props.busy}
      aria-labelledby='current-payment-title'
    >
      <p className='text-muted-foreground mb-2 text-sm'>
        {t('Current top-up order')}
      </p>
      <h2
        ref={heading}
        tabIndex={-1}
        id='current-payment-title'
        className='mb-6 text-2xl font-semibold outline-none'
      >
        {t(
          PAYMENT_STATUS_LABELS[order.display_status] ??
            'Payment status unavailable'
        )}
      </h2>
      <PaymentOrderDetails
        order={order}
        serverTime={props.wallet.server_time}
      />
      <div className='mt-5 space-y-3' aria-live='polite'>
        {order.display_status === 'timed_out' && (
          <p>
            {t(
              'This top-up has timed out on this site. Return to top-up to start a new order.'
            )}
          </p>
        )}
        {order.display_status === 'ended' && (
          <p>
            {t(
              'This top-up has ended on this site. Return to top-up to start a new order.'
            )}
          </p>
        )}
        {order.display_status === 'expired' && (
          <p>
            {t(
              'This order timed out and was automatically cancelled. It can no longer be paid.'
            )}
          </p>
        )}
        {order.display_status === 'success' && (
          <p>{t('Payment received and credits added to your wallet.')}</p>
        )}
        {order.display_status === 'cancelled' && (
          <p>
            {t('The order was cancelled and its payment session is closed.')}
          </p>
        )}
        {order.display_status === 'pending' && (
          <p className='text-muted-foreground text-sm'>
            {t(
              'Closing the payment page does not end this top-up. Continue payment before its deadline or end this top-up.'
            )}
          </p>
        )}
        {[
          'creating',
          'payment_processing',
          'cancel_processing',
          'expiry_processing',
          'review_required',
          'credit_review',
        ].includes(order.display_status) && (
          <p className='text-muted-foreground text-sm'>
            {t(
              'We are verifying this order. Please do not pay again or create another order.'
            )}
          </p>
        )}
        {props.notice && (
          <p role='status' className='text-sm'>
            {props.notice === 'PAYMENT_WINDOW_UNAVAILABLE'
              ? t(
                  'The order is saved. If the payment page did not open, select Continue payment.'
                )
              : t(
                  'The request could not be confirmed. Refresh this order and try again.'
                )}
          </p>
        )}
      </div>
      <div className='mt-6 flex flex-wrap gap-3'>
        {final && (
          <Button
            className='min-h-11 w-full sm:w-auto'
            disabled={props.busy}
            onClick={props.onAcknowledge}
          >
            {order.display_status === 'success'
              ? t('Top up again')
              : t('Return to top-up')}
          </Button>
        )}
        {!final &&
          !ended &&
          !received &&
          order.allowed_actions.includes('resume') && (
            <Button
              className='min-h-11 w-full sm:w-auto'
              disabled={props.busy || deadlineReached}
              onClick={props.onResume}
            >
              <ExternalLink aria-hidden='true' />
              {t('Continue payment')}
            </Button>
          )}
        {!final &&
          !ended &&
          !received &&
          order.allowed_actions.includes('end') && (
            <Button
              variant='outline'
              disabled={props.busy}
              aria-expanded={confirmEnd}
              aria-controls='end-current-payment'
              onClick={() => setConfirmEnd((value) => !value)}
            >
              {t('End this top-up')}
            </Button>
          )}
        {!ended && order.allowed_actions.includes('reconcile') && (
          <Button
            variant='outline'
            disabled={props.busy}
            onClick={props.onReconcile}
          >
            <RefreshCw aria-hidden='true' />
            {t('Check payment')}
          </Button>
        )}
        <Button variant='ghost' onClick={props.onHistory}>
          {t('Order History')}
        </Button>
      </div>
      {confirmEnd &&
        !ended &&
        !received &&
        order.allowed_actions.includes('end') && (
          <div
            id='end-current-payment'
            className='bg-muted/40 mt-4 space-y-3 rounded-lg border p-4'
          >
            <h3 className='font-medium'>{t('End this top-up?')}</h3>
            <p className='text-sm break-all'>{order.trade_no}</p>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Ending this top-up lets you start a new order after returning to top-up. It does not guarantee that the previous payment page is closed. Any payment actually received will still be verified and credited.'
              )}
            </p>
            <div className='flex flex-wrap gap-2'>
              <Button
                disabled={props.busy}
                onClick={() => {
                  setConfirmEnd(false)
                  props.onEnd()
                }}
              >
                {t('Confirm ending')}
              </Button>
              <Button
                variant='outline'
                disabled={props.busy}
                onClick={() => setConfirmEnd(false)}
              >
                {t('Back to order')}
              </Button>
            </div>
          </div>
        )}
      {props.wallet.other_unfinished_count > 0 && (
        <Button
          variant='link'
          className='mt-4 h-auto text-left whitespace-normal'
          onClick={props.onUnfinishedHistory ?? props.onHistory}
        >
          {t('View other unfinished orders ({{count}})', {
            count: props.wallet.other_unfinished_count,
          })}
        </Button>
      )}
    </section>
  )
}

export function PaymentOrderDetails(props: {
  order: PaymentOrder
  serverTime: number
}) {
  const { t, i18n } = useTranslation()
  const { copyToClipboard, copiedText } = useCopyToClipboard({ notify: false })
  const order = props.order
  const [remaining, setRemaining] = useState<number | null>(null)
  const canResume =
    order.display_status === 'pending' &&
    !isLocallyEnded(order) &&
    order.allowed_actions.includes('resume')
  useEffect(() => {
    if (!order.deadline_at || !canResume) return
    const deadline = order.deadline_at
    const started = Date.now()
    const tick = () =>
      setRemaining(
        Math.max(
          0,
          Math.floor(
            deadline - props.serverTime - (Date.now() - started) / 1000
          )
        )
      )
    const timer = window.setInterval(tick, 1000)
    return () => window.clearInterval(timer)
  }, [order.deadline_at, props.serverTime, canResume])
  let remainingLabel = ''
  if (remaining !== null) {
    remainingLabel = `${Math.floor(remaining / 60)}:${String(remaining % 60).padStart(2, '0')}`
  }
  return (
    <div className='space-y-5'>
      <PaymentAmounts pricing={order} />
      {isLocallyEnded(order) && order.display_status !== 'success' && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Do not pay through the previous payment page. If a payment was actually received, it will still be verified and credited.'
          )}
        </p>
      )}
      {order.allowed_actions.includes('contact_support') && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Contact the site support team with the order number copied below and your payment receipt. Do not share payment links or tokens.'
          )}
        </p>
      )}

      <dl className='space-y-3 border-t pt-4 text-sm'>
        <div className='flex flex-wrap justify-between gap-2'>
          <dt className='text-muted-foreground'>{t('Payment Method')}</dt>
          <dd>{order.payment_method}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Order number')}</dt>
          <dd className='mt-1 flex items-start gap-2'>
            <span className='min-w-0 flex-1 font-mono break-all'>
              {order.trade_no}
            </span>
            <Button
              variant='ghost'
              size='icon'
              aria-label={t('Copy order number')}
              onClick={() => void copyToClipboard(order.trade_no)}
            >
              <Copy aria-hidden='true' />
            </Button>
          </dd>
          {copiedText === order.trade_no && <p role='status'>{t('Copied')}</p>}
        </div>
        <div className='flex flex-wrap justify-between gap-2'>
          <dt className='text-muted-foreground'>{t('Created')}</dt>
          <dd>{formatTimestamp(order.created_at)}</dd>
        </div>
        {order.deadline_at && (
          <div className='flex flex-wrap justify-between gap-2'>
            <dt className='text-muted-foreground'>
              {order.deadline_source === 'local_policy'
                ? t('Top-up deadline')
                : t('Payment deadline')}
            </dt>
            <dd>
              {formatTimestamp(order.deadline_at)}
              {canResume && (
                <span className='ml-2 tabular-nums'>{remainingLabel}</span>
              )}
            </dd>
          </div>
        )}
        {order.received_payment && (
          <div className='flex flex-wrap justify-between gap-2'>
            <dt className='text-muted-foreground'>{t('Payment received')}</dt>
            <dd>{formatPaymentMoney(order.received_payment, i18n.language)}</dd>
          </div>
        )}
        {order.completed_at && (
          <div className='flex flex-wrap justify-between gap-2'>
            <dt className='text-muted-foreground'>{t('Completed')}</dt>
            <dd>{formatTimestamp(order.completed_at)}</dd>
          </div>
        )}
        {order.closed_at && (
          <div className='flex flex-wrap justify-between gap-2'>
            <dt className='text-muted-foreground'>{t('Closed')}</dt>
            <dd>{formatTimestamp(order.closed_at)}</dd>
          </div>
        )}
        {order.local_ended_at && (
          <div className='flex flex-wrap justify-between gap-2'>
            <dt className='text-muted-foreground'>{t('Top-up ended at')}</dt>
            <dd>{formatTimestamp(order.local_ended_at)}</dd>
          </div>
        )}
        {order.last_checked_at && (
          <div className='flex flex-wrap justify-between gap-2'>
            <dt className='text-muted-foreground'>{t('Last checked')}</dt>
            <dd>{formatTimestamp(order.last_checked_at)}</dd>
          </div>
        )}
      </dl>
      {order.review && (
        <div className='bg-muted/40 rounded-md border p-3 text-sm'>
          <p>{t('Payment under review')}</p>
          <p>
            {t('Responsible team')}: {order.review.owner_label}
          </p>
          <p>
            {t(
              'Keep your order number and payment receipt when contacting support.'
            )}
          </p>
        </div>
      )}
      {order.legacy_display && (
        <p className='text-muted-foreground text-sm'>
          {t('Original record (not verified)')}:{' '}
          {order.legacy_display.money ?? '—'} /{' '}
          {order.legacy_display.amount ?? '—'}
        </p>
      )}
      {!!order.timeline?.length && (
        <details>
          <summary className='cursor-pointer text-sm'>
            {t('Order timeline')}
          </summary>
          <ol className='mt-3 space-y-2 text-sm'>
            {order.timeline.map((event) => (
              <li
                key={`${event.event_type}-${event.occurred_at}-${event.summary_code}`}
              >
                <time>{formatTimestamp(event.occurred_at)}</time>
                <span className='ml-2'>
                  {t(
                    PAYMENT_STATUS_LABELS[
                      event.summary_code as keyof typeof PAYMENT_STATUS_LABELS
                    ] ?? 'Order updated'
                  )}
                </span>
              </li>
            ))}
          </ol>
        </details>
      )}
    </div>
  )
}

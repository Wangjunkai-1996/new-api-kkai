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
import { ChevronLeft, ChevronRight, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useSiteConfig } from '@/features/site-config'
import { hasPermission } from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { useBillingHistory } from '../../hooks/use-billing-history'
import { useWalletPaymentIdentity } from '../../hooks/use-wallet-payment-flow'
import { formatTimestamp, getPaymentMethodName } from '../../lib/billing'
import { formatWalletCash } from '../../lib/funding-summary'
import { isLocallyEnded, PAYMENT_STATUS_LABELS } from '../../lib/payment-flow'
import {
  actOnWalletOrder,
  getWalletOrder,
  WalletPaymentError,
} from '../../payment-flow-api'
import type {
  PaymentOrder,
  PaymentStatusGroup,
  WalletState,
} from '../../payment-flow-types'
import { PaymentOrderDetails } from '../current-topup-order'
import { PaymentAmounts } from '../payment-amounts'
import { PaymentEvidence } from '../payment-evidence'
import { WalletFundingSummary } from '../wallet-funding-summary'

const GROUPS: Record<PaymentStatusGroup, string> = {
  all: 'All',
  pending: 'Awaiting payment',
  processing: 'Processing',
  review: 'Under review',
  completed: 'Completed',
  closed: 'Closed',
}
interface BillingHistoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  scope?: 'self' | 'admin'
  unfinished?: boolean
  onWalletChange?: (wallet: WalletState) => void
  onRestore?: (tradeNo: string) => void
}
export function BillingHistoryDialog(props: BillingHistoryDialogProps) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const identity = useWalletPaymentIdentity()
  const user = useAuthStore((state) => state.auth.user)
  const admin = props.scope === 'admin'
  const permitted =
    !admin ||
    ((user?.role ?? 0) >= ROLE.ADMIN && hasPermission(user, 'billing', 'read'))
  const history = useBillingHistory({
    enabled: props.open && permitted,
    scope: props.scope ?? 'self',
    unfinished: props.unfinished,
  })
  let description = t('Recent 30-day records and all unfinished orders.')
  if (props.unfinished) description = t('Unfinished orders from all dates.')
  else if (history.exact) description = t('Exact order number, any date')
  else if (history.period === 'all') {
    description = t('All-time top-up records and unfinished orders.')
  }
  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={admin ? t('All top-up orders') : t('Order History')}
      description={description}
      contentClassName='max-sm:h-dvh max-sm:w-screen max-sm:max-w-none max-sm:rounded-none sm:max-w-[940px]'
      bodyClassName='space-y-4'
    >
      {!permitted ? (
        <p role='alert'>{t('Admin access required')}</p>
      ) : (
        <>
          {!admin && <WalletFundingSummary enabled={props.open} />}
          <div
            className='flex flex-wrap gap-2'
            role='group'
            aria-label={t('Order status filter')}
          >
            {Object.entries(GROUPS).map(([group, label]) => (
              <Button
                key={group}
                variant={history.status === group ? 'default' : 'outline'}
                size='sm'
                aria-pressed={history.status === group}
                onClick={() =>
                  history.handleStatusChange(group as PaymentStatusGroup)
                }
              >
                {t(label)}
                {history.counts && (
                  <span className='ml-1 tabular-nums'>
                    {history.counts[group as PaymentStatusGroup]}
                  </span>
                )}
              </Button>
            ))}
          </div>
          <div className='flex flex-wrap gap-2'>
            <Input
              aria-label={t('Search by order number...')}
              placeholder={t('Search by order number...')}
              value={history.keyword}
              onChange={(event) => history.handleSearch(event.target.value)}
              className='min-w-0 flex-1 basis-48'
            />
            {!props.unfinished && (
              <select
                aria-label={t('Order time range')}
                className='bg-background min-h-9 rounded-md border px-2 text-sm'
                value={history.period}
                onChange={(event) =>
                  history.handlePeriodChange(
                    event.target.value as 'recent' | 'all'
                  )
                }
              >
                <option value='recent'>{t('Last 30 days')}</option>
                <option value='all'>{t('All time')}</option>
              </select>
            )}
            <Button
              variant='outline'
              disabled={history.loading}
              onClick={() => {
                void history.refresh()
                if (!admin) {
                  void client.invalidateQueries({
                    queryKey: [
                      'wallet-payment',
                      ...identity,
                      'funding-summary',
                    ],
                  })
                }
              }}
            >
              <RefreshCw aria-hidden='true' />
              {t('Refresh')}
            </Button>
          </div>
          <label className='flex items-center gap-2 text-sm'>
            <input
              type='checkbox'
              checked={history.exact}
              onChange={(event) =>
                history.handleExactChange(event.target.checked)
              }
            />
            {t('Exact order number, any date')}
          </label>
          {history.loading && <p role='status'>{t('Loading...')}</p>}
          {history.error && (
            <p role='alert'>{t('Failed to load billing history')}</p>
          )}
          {!history.loading && !history.error && !history.records.length && (
            <p className='py-8 text-center'>{t('No orders found')}</p>
          )}
          <div className='space-y-3' aria-busy={history.loading}>
            {history.records.map((order) => (
              <HistoryOrder
                key={order.trade_no}
                order={order}
                serverTime={history.serverTime}
                admin={admin}
                onWalletChange={props.onWalletChange}
                onRestore={props.onRestore}
              />
            ))}
          </div>
          <div className='flex flex-wrap items-center justify-between gap-3 text-sm'>
            <label className='flex items-center gap-2'>
              {t('Page size')}
              <select
                className='bg-background rounded-md border p-2'
                value={history.pageSize}
                onChange={(event) =>
                  history.handlePageSizeChange(Number(event.target.value))
                }
              >
                {[10, 20, 50, 100].map((size) => (
                  <option key={size} value={size}>
                    {size}
                  </option>
                ))}
              </select>
            </label>
            <span>{t('{{count}} orders', { count: history.total })}</span>
            <div className='flex items-center gap-2'>
              <Button
                size='icon'
                variant='outline'
                aria-label={t('Previous page')}
                disabled={history.page <= 1 || history.loading}
                onClick={() => history.handlePageChange(history.page - 1)}
              >
                <ChevronLeft aria-hidden='true' />
              </Button>
              <span>
                {history.page} /{' '}
                {Math.max(1, Math.ceil(history.total / history.pageSize))}
              </span>
              <Button
                size='icon'
                variant='outline'
                aria-label={t('Next page')}
                disabled={
                  history.page * history.pageSize >= history.total ||
                  history.loading
                }
                onClick={() => history.handlePageChange(history.page + 1)}
              >
                <ChevronRight aria-hidden='true' />
              </Button>
            </div>
          </div>
        </>
      )}
    </Dialog>
  )
}
function HistoryOrder(props: {
  order: PaymentOrder
  serverTime: number
  admin: boolean
  onWalletChange?: (wallet: WalletState) => void
  onRestore?: (tradeNo: string) => void
}) {
  const { t, i18n } = useTranslation()
  const identity = useWalletPaymentIdentity()
  const client = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const { data: site } = useSiteConfig()
  const [expanded, setExpanded] = useState(false)
  const [confirm, setConfirm] = useState<
    'end' | 'resume' | 'admin-reconcile' | null
  >(null)
  const [reason, setReason] = useState('')
  const [feedback, setFeedback] = useState(false)
  const details = useQuery({
    queryKey: [
      'wallet-payment',
      ...identity,
      'detail',
      props.admin,
      props.order.trade_no,
    ],
    queryFn: ({ signal }) =>
      getWalletOrder(props.order.trade_no, props.admin, signal),
    enabled: expanded,
    retry: false,
  })
  const order = details.data?.order ?? props.order
  const ended = isLocallyEnded(order)
  const received =
    order.display_status === 'success' ||
    order.display_status === 'credit_review'
  const mutation = useMutation({
    mutationFn: async (action: 'end' | 'reconcile' | 'admin-reconcile') =>
      actOnWalletOrder(order, action, reason),
    retry: false,
    onSuccess: (result) => {
      props.onWalletChange?.(result.wallet)
      setConfirm(null)
      setFeedback(true)
      client.setQueryData(
        ['wallet-payment', ...identity, 'detail', props.admin, order.trade_no],
        result
      )
    },
    onError: (error) => {
      if (error instanceof WalletPaymentError && error.wallet) {
        props.onWalletChange?.(error.wallet)
      }
    },
    onSettled: () => {
      void client.invalidateQueries({
        queryKey: ['wallet-payment', ...identity, 'history'],
      })
      void client.invalidateQueries({
        queryKey: ['wallet-payment', ...identity, 'current'],
      })
      if (!props.admin) {
        void client.invalidateQueries({
          queryKey: ['wallet-payment', ...identity, 'funding-summary'],
        })
      }
    },
  })
  const canOperate =
    !ended &&
    order.allowed_actions.includes('reconcile') &&
    props.admin &&
    hasPermission(user, 'billing', 'write')
  const awaitingPayment =
    !order.received_payment &&
    !ended &&
    ['creating', 'pending', 'payment_processing'].includes(order.display_status)
  const payment = awaitingPayment
    ? (order.payable_payment ?? order.expected_payment)
    : order.received_payment
  let credited = t('Unconfirmed')
  if (awaitingPayment || order.display_status === 'credit_review') {
    credited = t('Not yet credited')
  } else if (order.display_status === 'success' && order.credit_display) {
    credited = `${order.credit_display.amount} ${order.credit_display.unit}`
  }
  let statusClass = 'bg-muted text-muted-foreground'
  if (order.status_group === 'completed') {
    statusClass = 'bg-success/10 text-success'
  } else if (order.status_group === 'pending') {
    statusClass = 'bg-warning/10 text-warning'
  }
  return (
    <article className='rounded-lg border px-4 py-3.5'>
      <div className='flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
        <span className={`rounded px-2 py-0.5 font-medium ${statusClass}`}>
          {t(
            PAYMENT_STATUS_LABELS[order.display_status] ??
              'Payment status unavailable'
          )}
        </span>
        <span>
          {order.created_at > 0
            ? formatTimestamp(order.created_at)
            : t('Not available')}
        </span>
        <span className='text-muted-foreground'>
          {getPaymentMethodName(order.payment_method, t)}
        </span>
      </div>
      <p className='text-muted-foreground mt-1 min-w-0 font-mono text-[11px] break-all'>
        {order.trade_no}
      </p>
      {props.admin && order.user_id !== undefined && (
        <p className='mb-2 text-sm'>
          {t('User ID')}: {order.user_id}
        </p>
      )}
      <div className='mt-3 flex flex-wrap items-center gap-x-6 gap-y-3'>
        <dl className='flex flex-wrap gap-x-6 gap-y-2 text-xs max-[440px]:w-full'>
          <div className='flex flex-wrap items-baseline gap-2'>
            <dt className='text-muted-foreground'>
              {awaitingPayment ? t('Amount due') : t('Actual payment')}
            </dt>
            <dd className='text-[15px] font-medium break-all tabular-nums'>
              {payment
                ? formatWalletCash(payment, i18n.language)
                : t('Unverified')}
            </dd>
          </div>
          <div className='flex flex-wrap items-baseline gap-2'>
            <dt className='text-muted-foreground'>{t('Credited Amount')}</dt>
            <dd className='text-[15px] font-medium break-all tabular-nums'>
              {credited}
            </dd>
          </div>
        </dl>
        <div className='ml-auto flex flex-wrap gap-2 max-[440px]:ml-0 max-[440px]:w-full'>
          <Button
            variant='outline'
            size='sm'
            aria-expanded={expanded}
            onClick={() => setExpanded((value) => !value)}
          >
            {t('Order details')}
          </Button>
          {!props.admin &&
            !ended &&
            !received &&
            order.allowed_actions.includes('resume') &&
            props.onRestore && (
              <Button
                variant='outline'
                size='sm'
                onClick={() => setConfirm('resume')}
              >
                {t('Continue payment')}
              </Button>
            )}
          {!props.admin &&
            !ended &&
            !received &&
            order.allowed_actions.includes('end') && (
              <Button
                variant='outline'
                size='sm'
                disabled={mutation.isPending}
                onClick={() => setConfirm('end')}
              >
                {t('End this top-up')}
              </Button>
            )}
          {!props.admin &&
            !ended &&
            order.allowed_actions.includes('reconcile') && (
              <Button
                variant='outline'
                size='sm'
                disabled={mutation.isPending}
                onClick={() => mutation.mutate('reconcile')}
              >
                {t('Check payment')}
              </Button>
            )}
          {canOperate && (
            <Button
              variant='outline'
              size='sm'
              disabled={mutation.isPending}
              onClick={() => {
                setExpanded(true)
                setConfirm('admin-reconcile')
              }}
            >
              {t('Verify payment')}
            </Button>
          )}
          {!props.admin &&
            site?.tenant_id === 0 &&
            order.display_status === 'success' && (
              <Button
                variant='outline'
                size='sm'
                render={
                  <Link
                    to='/invoices/new'
                    search={{ source_type: 'topup', order_id: order.trade_no }}
                  />
                }
              >
                {t('Apply for invoice')}
              </Button>
            )}
        </div>
      </div>
      {feedback && (
        <p role='status' className='mt-3 text-sm'>
          {t(
            'The request was received. The displayed order state is the latest confirmed result.'
          )}
        </p>
      )}
      {mutation.isError && (
        <p role='alert' className='mt-3 text-sm'>
          {t(
            'The request could not be confirmed. Refresh this order and try again.'
          )}
        </p>
      )}
      {expanded && (
        <div className='mt-4 border-t pt-4'>
          {details.isPending && <p role='status'>{t('Loading...')}</p>}
          {details.isError && (
            <p role='alert'>{t('Could not load order details.')}</p>
          )}
          <PaymentOrderDetails order={order} serverTime={props.serverTime} />
          {props.admin && order.receipts && (
            <PaymentEvidence receipts={order.receipts} />
          )}
        </div>
      )}
      {confirm &&
        !ended &&
        (confirm === 'admin-reconcile' ? canOperate : !received) && (
          <section
            className='bg-muted/30 mt-4 space-y-3 rounded-lg border p-4'
            aria-label={t('Confirm order action')}
          >
            <h3 className='font-semibold'>
              {confirm === 'end'
                ? t('End this top-up?')
                : t('Confirm order action')}
            </h3>
            <p className='text-sm break-all'>{order.trade_no}</p>
            <PaymentAmounts pricing={order} />
            {confirm === 'end' && (
              <p className='text-sm'>
                {t(
                  'Ending this top-up does not guarantee that the previous payment page is closed. Any payment actually received will still be verified and credited. Your current order will not be replaced.'
                )}
              </p>
            )}
            {confirm === 'resume' && (
              <p className='text-sm'>
                {t(
                  'Restore this original order in your wallet to reopen its existing payment session.'
                )}
              </p>
            )}
            {confirm === 'admin-reconcile' && (
              <label className='block space-y-2 text-sm'>
                {t('Reason for verification')}
                <Input
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  maxLength={500}
                />
                <span className='text-muted-foreground block'>
                  {t(
                    'Credits are added only after payment evidence is verified. This action does not manually mark an order as paid.'
                  )}
                </span>
              </label>
            )}
            <div className='flex flex-wrap gap-2'>
              <Button
                disabled={
                  mutation.isPending ||
                  (confirm === 'admin-reconcile' && !reason.trim())
                }
                onClick={() => {
                  if (confirm === 'resume') props.onRestore?.(order.trade_no)
                  else if (confirm) mutation.mutate(confirm)
                }}
              >
                {confirm === 'end' ? t('Confirm ending') : t('Confirm')}
              </Button>
              <Button
                variant='outline'
                disabled={mutation.isPending}
                onClick={() => setConfirm(null)}
              >
                {t('Back to order')}
              </Button>
            </div>
          </section>
        )}
    </article>
  )
}

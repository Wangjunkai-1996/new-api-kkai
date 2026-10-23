import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuthStore } from '@/stores/auth-store'

import { getInvoiceOrders, invoiceKey } from '../api'
import { useInvoiceDraft } from '../lib/draft'
import {
  formatInvoiceMoney,
  invoiceDate,
  invoiceOrderKey,
  reasonKeys,
} from '../lib/format'
import type { InvoiceFilters, InvoiceOrder } from '../types'
import { InvoiceFiltersBar } from './filters'
import { InvoiceEmpty, InvoiceError, InvoicePagination } from './shared'

export function InvoiceOrderSelector(props: {
  selected: InvoiceOrder[]
  onChange: (orders: InvoiceOrder[]) => void
  unavailable?: boolean
}) {
  const { t, i18n } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const { policy } = useInvoiceDraft()
  const maxOrders = policy?.limits.max_orders || 50
  const [filters, setFilters] = useState<InvoiceFilters>({
    p: 1,
    page_size: 20,
  })
  const [unavailable, setUnavailable] = useState(false)
  const hasFilters = !!(
    filters.q ||
    filters.source_type ||
    filters.currency ||
    filters.from ||
    filters.to
  )
  const query = useQuery({
    queryKey: [...invoiceKey(userId), 'orders', filters, props.unavailable],
    queryFn: () =>
      getInvoiceOrders({
        ...filters,
        eligibility: props.unavailable ? 'unavailable' : 'eligible',
      }),
    retry: false,
    gcTime: 0,
  })
  const pageOrders = query.data?.items.filter((order) => order.eligible) || []
  const selectedKeys = new Set(props.selected.map(invoiceOrderKey))
  const checkedCount = pageOrders.filter((order) =>
    selectedKeys.has(invoiceOrderKey(order))
  ).length
  function toggle(order: InvoiceOrder, checked: boolean) {
    if (checked) props.onChange([...props.selected, order])
    else {
      props.onChange(
        props.selected.filter(
          (item) => invoiceOrderKey(item) !== invoiceOrderKey(order)
        )
      )
    }
  }
  return (
    <div className='space-y-4'>
      <div className='invoice-panel'>
        <div className='invoice-panel-head'>
          <h2 className='font-medium'>
            {props.unavailable
              ? t('Orders not currently invoiceable')
              : t('Select paid orders')}
          </h2>
          <p className='text-muted-foreground mt-1 text-xs'>
            {props.unavailable
              ? t(
                  'These orders cannot be selected. Review the reason shown for each order.'
                )
              : t(
                  'Only verified cash payments are included. Each payment can be invoiced once.'
                )}
          </p>
        </div>
        <div className='p-4'>
          <InvoiceFiltersBar
            value={filters}
            onChange={setFilters}
            timezone={policy?.timezone}
            currencies={policy?.currencies}
          />
        </div>
        <InvoiceError
          error={query.error}
          onRetry={() => void query.refetch()}
        />
        {query.isPending && (
          <div className='space-y-3 p-4'>
            <Skeleton className='h-16' />
            <Skeleton className='h-16' />
          </div>
        )}
        {!props.unavailable && !!pageOrders.length && (
          <label className='invoice-label text-muted-foreground border-t px-[18px] py-3'>
            <Checkbox
              checked={checkedCount === pageOrders.length}
              indeterminate={
                checkedCount > 0 && checkedCount < pageOrders.length
              }
              onCheckedChange={(checked) => {
                const pageKeys = new Set(pageOrders.map(invoiceOrderKey))
                const remaining = props.selected.filter(
                  (order) => !pageKeys.has(invoiceOrderKey(order))
                )
                props.onChange(
                  checked
                    ? [...remaining, ...pageOrders].slice(0, maxOrders)
                    : remaining
                )
              }}
            />
            {t('Select this page')}
            <span className='ml-auto text-xs'>
              {t('{{count}} selected', { count: props.selected.length })}
            </span>
          </label>
        )}
        {query.data?.items.length === 0 && (
          <div className='p-4'>
            <InvoiceEmpty>
              <p>
                {hasFilters || props.unavailable
                  ? t('No orders match these filters.')
                  : t('No invoiceable orders yet.')}
              </p>
              {!props.unavailable && (
                <>
                  <p className='mx-auto mt-2 max-w-md text-xs leading-relaxed'>
                    {t(
                      'Select an invoiceable order to continue. If a paid order is missing, check its invoice eligibility below.'
                    )}
                  </p>
                  {!unavailable && (
                    <Button
                      type='button'
                      size='sm'
                      variant='outline'
                      className='mt-4 max-w-full whitespace-normal'
                      onClick={() => setUnavailable(true)}
                      aria-expanded={unavailable}
                    >
                      {t('View unavailable orders and reasons')}
                    </Button>
                  )}
                </>
              )}
            </InvoiceEmpty>
          </div>
        )}
        {query.data?.items.map((order) => (
          <div
            key={invoiceOrderKey(order)}
            className='invoice-order'
            data-selected={selectedKeys.has(invoiceOrderKey(order))}
          >
            {order.eligible && !props.unavailable ? (
              <Checkbox
                aria-label={t('Select order {{order}}', {
                  order: order.order_no,
                })}
                checked={selectedKeys.has(invoiceOrderKey(order))}
                disabled={
                  !selectedKeys.has(invoiceOrderKey(order)) &&
                  props.selected.length >= maxOrders
                }
                onCheckedChange={(checked) => toggle(order, checked)}
              />
            ) : (
              <span
                aria-hidden='true'
                className='text-muted-foreground text-center'
              >
                —
              </span>
            )}
            <div className='min-w-0'>
              <p className='flex flex-wrap items-center gap-2 text-[13px] font-medium'>
                <span>
                  {order.source_type === 'topup'
                    ? t('Top-up')
                    : t('Subscription payment')}
                </span>
                <span className='bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-xs font-normal'>
                  {order.currency || t('Unknown currency')}
                </span>
              </p>
              <p className='text-muted-foreground mt-1 text-xs wrap-anywhere'>
                {order.order_no}
              </p>
              <p className='text-muted-foreground mt-1 text-xs'>
                {invoiceDate(order.paid_at, i18n.language)} ·{' '}
                {order.payment_label}
              </p>
              {order.reason_codes?.map((code) => (
                <p
                  key={code}
                  className='mt-1 text-xs text-amber-700 dark:text-amber-300'
                >
                  {t(reasonKeys[code] || 'Payment evidence needs verification')}
                </p>
              ))}
              {order.claimed_request_public_id && (
                <Link
                  to='/invoices/$invoiceId'
                  params={{ invoiceId: order.claimed_request_public_id }}
                  className='text-primary mt-1 inline-block text-xs underline'
                >
                  {t('View request')}
                </Link>
              )}
            </div>
            <div className='text-right'>
              <p className='text-sm font-medium whitespace-nowrap tabular-nums'>
                {order.amount_minor === null
                  ? t('Needs verification')
                  : formatInvoiceMoney(
                      order.amount_minor,
                      order.currency,
                      order.currency_exponent,
                      i18n.language
                    )}
              </p>
              {order.eligible && (
                <p className='mt-1 text-xs text-emerald-700 dark:text-emerald-300'>
                  {t('Available')}
                </p>
              )}
            </div>
          </div>
        ))}
        <div className='px-4 pb-4'>
          <InvoicePagination
            page={filters.p || 1}
            total={query.data?.total || 0}
            onChange={(p) => setFilters({ ...filters, p })}
          />
        </div>
      </div>
      {!props.unavailable && (
        <>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => setUnavailable(!unavailable)}
            aria-expanded={unavailable}
          >
            {t('Why are some orders unavailable?')}
          </Button>
          {unavailable && (
            <InvoiceOrderSelector
              selected={props.selected}
              onChange={props.onChange}
              unavailable
            />
          )}
        </>
      )}
    </div>
  )
}

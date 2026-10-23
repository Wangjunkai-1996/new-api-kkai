import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuthStore } from '@/stores/auth-store'

import { getInvoice, getInvoices, invoiceKey } from './api'
import { InvoiceDetail } from './components/detail'
import { InvoiceFiltersBar } from './components/filters'
import {
  InvoiceEmpty,
  InvoiceError,
  InvoicePagination,
  InvoiceStatusBadge,
} from './components/shared'
import { useInvoiceDraft } from './lib/draft'
import { formatInvoiceMoney, invoiceDate } from './lib/format'
import type { InvoiceFilters, InvoiceRequest } from './types'

export function InvoiceRecords(props: {
  filters?: InvoiceFilters
  selectedId?: string
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const { policy } = useInvoiceDraft()
  const filters: InvoiceFilters = {
    p: 1,
    page_size: 20,
    group: 'all',
    ...props.filters,
  }
  const setFilters = (value: InvoiceFilters) =>
    void navigate({ to: '/invoices', search: { ...value, tab: 'records' } })
  const setSelected = (selected: string) =>
    void navigate({
      to: '/invoices',
      search: { ...filters, tab: 'records', selected: selected || undefined },
    })
  const navigate = useNavigate()
  const query = useQuery({
    queryKey: [...invoiceKey(userId), 'records', filters],
    queryFn: () => getInvoices(filters),
    retry: false,
    gcTime: 0,
  })
  const current =
    query.data?.items.find((item) => item.public_id === props.selectedId)
      ?.public_id ||
    query.data?.items[0]?.public_id ||
    ''
  const detail = useQuery({
    queryKey: [...invoiceKey(userId), 'detail', current],
    queryFn: () => getInvoice(current),
    enabled: !!current,
    retry: false,
    gcTime: 0,
  })
  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap gap-2'>
        {[
          ['all', 'All'],
          ['processing', 'In progress'],
          ['awaiting_confirmation', 'Awaiting confirmation'],
          ['completed', 'Completed'],
        ].map(([value, label]) => (
          <Button
            key={value}
            size='sm'
            variant={filters.group === value ? 'secondary' : 'ghost'}
            onClick={() => {
              setFilters({ ...filters, group: value, p: 1 })
            }}
          >
            {t(label)}
          </Button>
        ))}
      </div>
      <InvoiceFiltersBar
        status
        timezone={policy?.timezone}
        currencies={policy?.currencies}
        value={filters}
        onChange={(value) => {
          setFilters(value)
        }}
      />
      <InvoiceError error={query.error} onRetry={() => void query.refetch()} />
      {query.isPending && <Skeleton className='h-80' />}
      {query.data?.items.length === 0 && (
        <InvoiceEmpty>
          {t('No invoice requests match these filters.')}
        </InvoiceEmpty>
      )}
      {!!query.data?.items.length && (
        <div className='invoice-master-detail'>
          <div>
            <div className='invoice-panel overflow-hidden'>
              {query.data.items.map((invoice) => (
                <InvoiceRecordRow
                  key={invoice.public_id}
                  invoice={invoice}
                  selected={current === invoice.public_id}
                  onSelect={() => {
                    const pane = document.querySelector('.invoice-record-pane')
                    if (!pane || getComputedStyle(pane).display === 'none') {
                      void navigate({
                        to: '/invoices/$invoiceId',
                        params: { invoiceId: invoice.public_id },
                        search: filters,
                      })
                    } else setSelected(invoice.public_id)
                  }}
                />
              ))}
            </div>
            <InvoicePagination
              page={filters.p || 1}
              pageSize={filters.page_size || 20}
              total={query.data.total}
              onChange={(p) => {
                setFilters({ ...filters, p })
              }}
            />
          </div>
          <div className='invoice-record-pane space-y-3'>
            <InvoiceError
              error={detail.error}
              onRetry={() => void detail.refetch()}
            />
            {detail.isPending && <Skeleton className='h-96' />}
            {detail.data && (
              <>
                <Link
                  to='/invoices/$invoiceId'
                  params={{ invoiceId: current }}
                  className='text-primary mb-2 inline-block text-xs underline'
                >
                  {t('Open request details')}
                </Link>
                <InvoiceDetail invoice={detail.data} />
              </>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
export function InvoiceRecordRow(props: {
  invoice: InvoiceRequest
  admin?: boolean
  selected: boolean
  onSelect: () => void
}) {
  const { t, i18n } = useTranslation()
  const invoice = props.invoice
  return (
    <button
      type='button'
      className='invoice-request-row hover:bg-muted/40 focus-visible:ring-primary focus-visible:ring-2'
      aria-current={props.selected}
      onClick={props.onSelect}
    >
      <span className='min-w-0'>
        <span className='block text-xs font-medium wrap-anywhere'>
          {invoice.display_no}
        </span>
        {props.admin && (
          <span
            className='text-muted-foreground mt-1 block text-xs wrap-anywhere'
            title={invoice.username || String(invoice.user_id || '')}
          >
            {t('User')}: {invoice.username || invoice.user_id}
          </span>
        )}
        <span className='text-muted-foreground mt-1 block text-xs wrap-anywhere'>
          {invoice.profile.title}
        </span>
        <span className='mt-2 block'>
          <InvoiceStatusBadge status={invoice.status} />
        </span>
        <span className='text-muted-foreground mt-2 block text-xs'>
          {invoice.source_summary
            ?.map((source) =>
              source === 'topup' ? t('Top-up') : t('Subscription payment')
            )
            .join(' + ')}{' '}
          · {invoice.items_count}
        </span>
      </span>
      <span className='shrink-0 text-right'>
        <span className='block text-sm font-medium tabular-nums'>
          {formatInvoiceMoney(
            invoice.amount_minor,
            invoice.currency,
            invoice.currency_exponent,
            i18n.language
          )}
        </span>
        <span className='text-muted-foreground mt-2 block text-[11px]'>
          {invoiceDate(invoice.created_at, i18n.language).split(' ')[0]}
        </span>
      </span>
    </button>
  )
}
export function InvoiceDetailPage(props: {
  invoiceId: string
  filters?: InvoiceFilters
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const detail = useQuery({
    queryKey: [...invoiceKey(userId), 'detail', props.invoiceId],
    queryFn: () => getInvoice(props.invoiceId),
    retry: false,
    gcTime: 0,
  })
  return (
    <div className='mx-auto max-w-3xl space-y-4'>
      <Link
        to='/invoices'
        search={{ ...props.filters, tab: 'records' }}
        className='text-primary text-sm underline'
      >
        {t('Back to invoice requests')}
      </Link>
      <InvoiceError
        error={detail.error}
        onRetry={() => void detail.refetch()}
      />
      {detail.isPending && <Skeleton className='h-96' />}
      {detail.data && (
        <InvoiceDetail key={props.invoiceId} invoice={detail.data} />
      )}
    </div>
  )
}

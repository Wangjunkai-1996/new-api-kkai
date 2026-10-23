import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Main } from '@/components/layout/components/main'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuthStore } from '@/stores/auth-store'

import {
  getInvoice,
  getInvoiceAdminSummary,
  getInvoicePolicy,
  getInvoices,
  invoiceKey,
} from './api'
import { InvoiceAdminActions } from './components/admin-actions'
import { InvoiceDetail } from './components/detail'
import { InvoiceFiltersBar } from './components/filters'
import { InvoiceNotifications } from './components/notifications'
import {
  InvoiceEmpty,
  InvoiceError,
  InvoicePagination,
} from './components/shared'
import { InvoiceRecordRow } from './records'
import type { InvoiceFilters } from './types'

import './invoice.css'

export function InvoiceAdminPage(props: {
  invoiceId?: string
  selectedId?: string
  filters?: InvoiceFilters
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const navigate = useNavigate()
  const policy = useQuery({
    queryKey: [...invoiceKey(userId), 'policy'],
    queryFn: getInvoicePolicy,
    retry: false,
    gcTime: 0,
  })
  const filters: InvoiceFilters = {
    p: 1,
    page_size: 20,
    queue: 'pending_review',
    ...props.filters,
  }
  const setFilters = (value: InvoiceFilters) =>
    void navigate({ to: '/invoice-admin', search: value })
  const setSelected = (selected: string) =>
    void navigate({
      to: '/invoice-admin',
      search: { ...filters, selected: selected || undefined },
    })
  const query = useQuery({
    queryKey: [...invoiceKey(userId, true), 'records', filters],
    queryFn: () => getInvoices(filters, true),
    retry: false,
    gcTime: 0,
  })
  const summary = useQuery({
    queryKey: [
      ...invoiceKey(userId, true),
      'summary',
      filters.q,
      filters.from,
      filters.to,
      filters.source_type,
      filters.currency,
    ],
    queryFn: () =>
      getInvoiceAdminSummary({
        q: filters.q,
        from: filters.from,
        to: filters.to,
        currency: filters.currency,
        source_type: filters.source_type,
      }),
    retry: false,
    gcTime: 0,
  })
  const current =
    props.invoiceId ||
    query.data?.items.find((item) => item.public_id === props.selectedId)
      ?.public_id ||
    query.data?.items[0]?.public_id ||
    ''
  const detail = useQuery({
    queryKey: [...invoiceKey(userId, true), 'detail', current],
    queryFn: () => getInvoice(current, true),
    enabled: !!current,
    retry: false,
    gcTime: 0,
  })
  const queues: {
    id: 'pending_review' | 'approved_for_issue' | 'disputed' | 'all'
    label: string
  }[] = [
    { id: 'pending_review', label: 'Pending review' },
    { id: 'approved_for_issue', label: 'Awaiting invoice' },
    { id: 'disputed', label: 'Disputes' },
    { id: 'all', label: 'All requests' },
  ]
  return (
    <Main className='bg-muted/20 overflow-y-auto'>
      <div className='invoice-content'>
        <div className='invoice-root space-y-5'>
          <header className='flex flex-wrap items-start justify-between gap-3'>
            <div>
              <h1 className='text-2xl font-medium tracking-tight md:text-[26px]'>
                {t('Invoice management')}
              </h1>
              <p className='text-muted-foreground mt-2 text-[13px]'>
                {t(
                  'Review payment evidence, deliver invoices and resolve feedback.'
                )}
              </p>
            </div>
            <div className='flex gap-2'>
              <InvoiceNotifications admin />
              <Button
                variant='outline'
                size='sm'
                onClick={() => {
                  void query.refetch()
                  void detail.refetch()
                  void summary.refetch()
                }}
              >
                {t('Refresh')}
              </Button>
            </div>
          </header>
          <nav
            className='flex flex-wrap gap-2'
            aria-label={t('Invoice queues')}
          >
            {queues.map((queue) => (
              <Button
                key={queue.id}
                size='sm'
                variant={filters.queue === queue.id ? 'secondary' : 'ghost'}
                onClick={() => {
                  setFilters({
                    ...filters,
                    queue: queue.id,
                    p: 1,
                    status: undefined,
                  })
                }}
              >
                {t(queue.label)}{' '}
                <span className='bg-muted ml-1 rounded px-1.5 text-xs'>
                  {summary.data?.[queue.id] ?? '—'}
                </span>
              </Button>
            ))}
          </nav>
          {!!summary.data?.finance_hold && (
            <Button
              variant='outline'
              size='sm'
              onClick={() => {
                setFilters({
                  ...filters,
                  queue: 'all',
                  status: 'finance_hold',
                  p: 1,
                })
              }}
            >
              {t('Finance review required')} · {summary.data.finance_hold}
            </Button>
          )}
          {!props.invoiceId && (
            <InvoiceFiltersBar
              admin
              status
              timezone={policy.data?.timezone}
              currencies={policy.data?.currencies}
              value={filters}
              onChange={(value) => {
                setFilters(value)
              }}
            />
          )}
          <InvoiceError
            error={query.error || summary.error}
            onRetry={() => {
              void query.refetch()
              void summary.refetch()
            }}
          />
          {props.invoiceId ? (
            <div className='mx-auto max-w-3xl space-y-4'>
              <Link
                to='/invoice-admin'
                search={filters}
                className='text-primary text-sm underline'
              >
                {t('Back to invoice queue')}
              </Link>
              <InvoiceError
                error={detail.error}
                onRetry={() => void detail.refetch()}
              />
              {detail.isPending && <Skeleton className='h-96' />}
              {detail.data && (
                <InvoiceDetail invoice={detail.data} admin>
                  <InvoiceAdminActions key={current} invoice={detail.data} />
                </InvoiceDetail>
              )}
            </div>
          ) : (
            <>
              {query.isPending && <Skeleton className='h-80' />}
              {query.data?.items.length === 0 && (
                <InvoiceEmpty>{t('No requests in this queue.')}</InvoiceEmpty>
              )}
              {!!query.data?.items.length && (
                <div className='invoice-master-detail'>
                  <div>
                    <div className='invoice-panel overflow-hidden'>
                      {query.data.items.map((invoice) => (
                        <InvoiceRecordRow
                          key={invoice.public_id}
                          admin
                          invoice={invoice}
                          selected={current === invoice.public_id}
                          onSelect={() => {
                            const pane = document.querySelector(
                              '.invoice-record-pane'
                            )
                            if (
                              !pane ||
                              getComputedStyle(pane).display === 'none'
                            ) {
                              void navigate({
                                to: '/invoice-admin/$invoiceId',
                                params: { invoiceId: invoice.public_id },
                                search: filters,
                              })
                            } else {
                              setSelected(invoice.public_id)
                            }
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
                      <InvoiceDetail invoice={detail.data} admin>
                        <InvoiceAdminActions
                          key={current}
                          invoice={detail.data}
                        />
                      </InvoiceDetail>
                    )}
                  </div>
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </Main>
  )
}

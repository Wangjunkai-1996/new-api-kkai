import {
  AddInvoiceIcon,
  InformationCircleIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { Link, Outlet, useBlocker, useLocation } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Main } from '@/components/layout/components/main'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { getCurrentHost } from '@/lib/host-scope'
import { useAuthStore } from '@/stores/auth-store'

import { getInvoicePolicy, getInvoiceSummary, invoiceKey } from './api'
import { InvoiceNotifications } from './components/notifications'
import { InvoiceError } from './components/shared'
import { DraftContext, freshInvoiceDraft } from './lib/draft'
import { formatInvoiceMoney, invoiceOrderKey } from './lib/format'
import { useInvoicePaymentSync } from './lib/use-invoice-payment-sync'

import './invoice.css'

export function InvoiceWorkspace() {
  const user = useAuthStore((state) => state.auth.user)
  const sessionId = useAuthStore((state) => state.auth.session?.sid)
  return (
    <InvoiceWorkspaceContent
      key={`${getCurrentHost()}:${user?.id}:${sessionId}`}
    />
  )
}
function InvoiceWorkspaceContent() {
  const { t, i18n } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const [draft, setDraft] = useState(() => freshInvoiceDraft(user?.email))
  const [rules, setRules] = useState(false)
  const path = useLocation({ select: (location) => location.pathname })
  const search = useLocation({
    select: (location) => location.search as { tab?: string },
  })
  const policy = useQuery({
    queryKey: [...invoiceKey(user?.id), 'policy'],
    queryFn: getInvoicePolicy,
    retry: false,
    gcTime: 0,
  })
  const summary = useQuery({
    queryKey: [...invoiceKey(user?.id), 'summary'],
    queryFn: getInvoiceSummary,
    retry: false,
    gcTime: 0,
  })
  const paymentSync = useInvoicePaymentSync(policy.data?.enabled === true)
  const syncingPayments =
    policy.isPending ||
    (policy.data?.enabled && paymentSync.isIdle) ||
    paymentSync.isPending
  const activeTab = path.endsWith('/new') ? 'new' : search.tab || 'records'
  useBlocker({
    shouldBlockFn: ({ next }) =>
      draft.orders.length > 0 &&
      !next.pathname.startsWith('/invoices') &&
      !window.confirm(
        t('Leave this page? Your unsaved invoice draft will be lost.')
      ),
    enableBeforeUnload: draft.orders.length > 0,
  })
  return (
    <DraftContext.Provider value={{ draft, setDraft, policy: policy.data }}>
      <Main className='bg-muted/20 overflow-y-auto'>
        <div className='invoice-content'>
          <div className='invoice-root space-y-5'>
            <header className='flex flex-wrap items-start justify-between gap-4'>
              <div>
                <h1 className='text-2xl leading-tight font-medium tracking-tight md:text-[26px]'>
                  {t('Invoice center')}
                </h1>
                <p className='text-muted-foreground mt-2 text-[13px]'>
                  {t(
                    'Request and manage invoices for top-ups and memberships.'
                  )}
                </p>
              </div>
              <div className='flex flex-wrap gap-2'>
                <InvoiceNotifications />
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => setRules(true)}
                >
                  <HugeiconsIcon icon={InformationCircleIcon} size={16} />
                  {t('Invoice guide')}
                </Button>
                {activeTab !== 'new' && (
                  <Button size='sm' render={<Link to='/invoices/new' />}>
                    <HugeiconsIcon icon={AddInvoiceIcon} size={16} />
                    {t('Apply for invoice')}
                  </Button>
                )}
              </div>
            </header>
            <InvoiceError
              error={policy.error || summary.error}
              onRetry={() => {
                void policy.refetch()
                void summary.refetch()
              }}
            />
            {paymentSync.isPending && (
              <p role='status' className='text-muted-foreground text-sm'>
                {t('Updating your payment records automatically…')}
              </p>
            )}
            <InvoiceError
              error={paymentSync.error}
              message={
                paymentSync.isError
                  ? t(
                      'Some payment records could not be updated. Please try again later.'
                    )
                  : undefined
              }
              onRetry={() => paymentSync.mutate()}
            />
            {paymentSync.failures.length > 0 && (
              <details className='bg-muted/30 rounded-lg border p-3 text-sm'>
                <summary className='cursor-pointer'>
                  {t(
                    'Payments still unverified: {{count}}. Verified payments remain available.',
                    { count: paymentSync.failures.length }
                  )}
                </summary>
                <ul className='mt-3 space-y-3'>
                  {paymentSync.failures.map((failure) => (
                    <li key={invoiceOrderKey(failure.order)}>
                      <p className='mb-1 font-medium break-all'>
                        {failure.order.order_no}
                      </p>
                      <InvoiceError error={failure.error} />
                    </li>
                  ))}
                </ul>
                {!paymentSync.isError && (
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    className='mt-3'
                    disabled={paymentSync.isPending}
                    onClick={() => paymentSync.mutate()}
                  >
                    {t('Retry')}
                  </Button>
                )}
              </details>
            )}
            <div className='bg-card grid grid-cols-1 divide-y rounded-xl border sm:grid-cols-3 sm:divide-x sm:divide-y-0'>
              <div className='p-5'>
                <p className='text-muted-foreground text-xs'>
                  {t('Available to invoice')}
                </p>
                {summary.isPending || syncingPayments ? (
                  <Skeleton className='mt-2 h-8 w-32' />
                ) : (
                  <div className='mt-2 text-[26px] font-medium tabular-nums'>
                    {summary.data?.totals?.length
                      ? summary.data.totals.map((total) => (
                          <p key={total.currency}>
                            {formatInvoiceMoney(
                              total.amount_minor,
                              total.currency,
                              total.currency_exponent,
                              i18n.language
                            )}
                          </p>
                        ))
                      : '—'}
                  </div>
                )}
                <p className='text-muted-foreground mt-1 text-xs'>
                  {syncingPayments
                    ? t('Updating your payment records automatically…')
                    : t('{{count}} verified payments', {
                        count: summary.data?.eligible_count || 0,
                      })}
                </p>
              </div>
              <div className='p-5'>
                <p className='text-muted-foreground text-xs'>
                  {t('Invoice requests in progress')}
                </p>
                <p className='mt-2 text-[26px] font-medium tabular-nums'>
                  {summary.data?.processing_count ?? '—'}
                </p>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t('Review, issuance or dispute handling')}
                </p>
              </div>
              <div className='p-5'>
                <p className='text-muted-foreground text-xs'>
                  {t('Awaiting confirmation')}
                </p>
                <p className='mt-2 text-[26px] font-medium tabular-nums'>
                  {summary.data?.awaiting_confirmation_count ?? '—'}
                </p>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t('Review your invoice and confirm or report a problem')}
                </p>
              </div>
            </div>
            <nav
              className='flex gap-5 overflow-x-auto border-b text-sm'
              aria-label={t('Invoice center')}
            >
              {(
                [
                  ['new', 'Apply for invoice'],
                  ['records', 'Invoice requests'],
                  ['profiles', 'Saved invoice titles'],
                ] as const
              ).map(([tab, label]) => (
                <Link
                  key={tab}
                  to={tab === 'new' ? '/invoices/new' : '/invoices'}
                  search={tab === 'new' ? {} : { tab }}
                  className={`shrink-0 border-b-2 px-1 pb-3 ${activeTab === tab ? 'border-primary text-primary font-medium' : 'text-muted-foreground border-transparent'}`}
                  aria-current={activeTab === tab ? 'page' : undefined}
                >
                  {t(label)}
                </Link>
              ))}
            </nav>
            <Outlet />
            <p className='text-muted-foreground pt-2 text-center text-xs'>
              {t(
                'Invoice confirmation does not waive your right to request corrections.'
              )}
            </p>
            <Dialog open={rules} onOpenChange={setRules}>
              <DialogContent className='sm:max-w-lg'>
                <DialogHeader>
                  <DialogTitle>{t('Invoice guide')}</DialogTitle>
                  <DialogDescription>
                    {t('Invoices are based on verified cash payments.')}
                  </DialogDescription>
                </DialogHeader>
                <div className='space-y-4 text-sm leading-relaxed'>
                  <p>
                    {t(
                      'Verified top-ups and directly paid memberships can be invoiced. Expired membership benefits do not affect paid order eligibility.'
                    )}
                  </p>
                  <p>
                    {t(
                      'Orders must share the same seller, currency and tax rules. Gifts and wallet purchases do not create new invoiceable payments.'
                    )}
                  </p>
                  <p>
                    {t(
                      'Minimum per request: {{amount}}. Eligible payments can be combined.',
                      {
                        amount: formatInvoiceMoney(
                          policy.data?.minimum_amount_minor || '20000',
                          'CNY',
                          2,
                          i18n.language
                        ),
                      }
                    )}
                  </p>
                  <p>
                    {t(
                      'Approval starts invoice preparation. Your invoice is available only after an administrator delivers the issued file.'
                    )}
                  </p>
                  <p>
                    {t(
                      'If something is incorrect, report a problem here. Feedback does not automatically refund payments or release orders.'
                    )}
                  </p>
                  {policy.data?.sla && <p>{policy.data.sla}</p>}
                </div>
              </DialogContent>
            </Dialog>
          </div>
        </div>
      </Main>
    </DraftContext.Provider>
  )
}

import { ArrowLeft01Icon, ArrowRight01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { getServerErrorSources } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { statusKeys } from '../lib/format'
import type { InvoiceStatus } from '../types'

export function InvoiceStatusBadge(props: { status: InvoiceStatus }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'inline-flex max-w-full items-center gap-1.5 rounded-full px-2 py-1 text-xs',
        'bg-muted text-muted-foreground',
        ['confirmed', 'delivered'].includes(props.status) &&
          'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
        ['pending_review', 'approved_for_issue', 'finance_hold'].includes(
          props.status
        ) && 'bg-amber-500/10 text-amber-800 dark:text-amber-300',
        ['rejected', 'disputed'].includes(props.status) &&
          'bg-red-500/10 text-red-700 dark:text-red-300'
      )}
    >
      <span aria-hidden='true'>●</span>
      {t(statusKeys[props.status] ?? 'Unknown status')}
    </span>
  )
}
export function InvoiceError(props: {
  error?: unknown
  message?: string
  onRetry?: () => void
}) {
  const { t } = useTranslation()
  if (!props.error && !props.message) return null
  const code =
    getServerErrorSources(props.error)
      .map((source) => source.code)
      .find(
        (value) => typeof value === 'string' && !value.startsWith('ERR_')
      ) || (props.error instanceof Error ? props.error.message : undefined)
  let message =
    props.message ||
    t('The request could not be completed. Your input is preserved.')
  if (
    ['VERSION_CONFLICT', 'INVALID_STATE', 'DELIVERY_VERSION_CHANGED'].includes(
      String(code)
    )
  ) {
    message = t(
      'This request was updated. Review the latest version before continuing.'
    )
  }
  if (
    [
      'ORDER_ALREADY_CLAIMED',
      'PAYMENT_CHANGED',
      'POLICY_CHANGED',
      'ORDER_UNAVAILABLE',
    ].includes(String(code))
  ) {
    message = t(
      'Payment eligibility has changed. Review the selected orders again.'
    )
  }
  if (code === 'INVOICE_BELOW_MINIMUM') {
    message = t('Add eligible payments to reach the minimum invoice amount.')
  }
  if (code === 'PAYMENT_VERIFICATION_UNAVAILABLE') {
    message = t(
      'Payment verification is temporarily unavailable. Try again later.'
    )
  }
  if (code === 'PAYMENT_VERIFICATION_FAILED') {
    message = t(
      'This payment could not be verified. Check its payment status and try again, or contact support.'
    )
  }
  if (code === 'PAYMENT_EXCEPTION') {
    message = t('This payment needs manual review. Contact support.')
  }
  if (code === 'NOT_FOUND') {
    message = t(
      'The requested record is no longer available. Refresh and try again.'
    )
  }
  if (
    [
      'SCHEMA_NOT_READY',
      'INVOICE_SCHEMA_NOT_READY',
      'FEATURE_DISABLED',
    ].includes(String(code))
  ) {
    message = t(
      'Invoicing is not available yet. Existing requests remain accessible.'
    )
  }
  return (
    <div
      role='alert'
      className='border-destructive/25 bg-destructive/5 text-destructive rounded-lg border p-3 text-sm'
    >
      <p>{message}</p>
      {props.onRetry && (
        <Button
          type='button'
          variant='outline'
          size='sm'
          className='mt-2'
          onClick={props.onRetry}
        >
          {t('Retry')}
        </Button>
      )}
    </div>
  )
}
export function InvoiceEmpty(props: { children?: React.ReactNode }) {
  const { t } = useTranslation()
  return (
    <div className='text-muted-foreground rounded-xl border border-dashed p-8 text-center text-sm'>
      {props.children || t('No matching records')}
    </div>
  )
}
export function InvoicePagination(props: {
  page: number
  total: number
  pageSize?: number
  onChange: (page: number) => void
}) {
  const { t } = useTranslation()
  const totalPages = Math.max(
    1,
    Math.ceil(props.total / (props.pageSize || 20))
  )
  if (totalPages <= 1) return null
  return (
    <nav
      className='mt-4 flex items-center justify-between gap-3 text-xs'
      aria-label={t('Pagination')}
    >
      <span className='text-muted-foreground'>
        {t('Page {{page}} of {{total}}', {
          page: props.page,
          total: totalPages,
        })}
      </span>
      <div className='flex gap-2'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.page <= 1}
          onClick={() => props.onChange(props.page - 1)}
          aria-label={t('Previous page')}
        >
          <HugeiconsIcon icon={ArrowLeft01Icon} size={16} />
        </Button>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.page >= totalPages}
          onClick={() => props.onChange(props.page + 1)}
          aria-label={t('Next page')}
        >
          <HugeiconsIcon icon={ArrowRight01Icon} size={16} />
        </Button>
      </div>
    </nav>
  )
}

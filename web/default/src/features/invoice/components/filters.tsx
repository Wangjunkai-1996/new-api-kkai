import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'

import {
  dateFilter,
  invoiceFilterDate,
  invoiceStatuses,
  statusKeys,
} from '../lib/format'
import type { InvoiceFilters } from '../types'

export function InvoiceFiltersBar(props: {
  value: InvoiceFilters
  onChange: (value: InvoiceFilters) => void
  status?: boolean
  admin?: boolean
  timezone?: string
  currencies?: { currency: string }[]
}) {
  const { t } = useTranslation()
  const update = (field: keyof InvoiceFilters, value: string) =>
    props.onChange({ ...props.value, [field]: value || undefined, p: 1 })
  return (
    <div className='invoice-filters'>
      <Input
        aria-label={
          props.admin
            ? t('Search request, order, username or account email')
            : t('Search request or order number')
        }
        placeholder={
          props.admin
            ? t('Search request, order, username or account email')
            : t('Search request or order number')
        }
        maxLength={128}
        value={props.value.q || ''}
        onChange={(event) => update('q', event.target.value)}
        className='min-w-40 flex-1'
      />
      <NativeSelect
        aria-label={t('Order source')}
        value={props.value.source_type || ''}
        onChange={(event) => update('source_type', event.target.value)}
      >
        <option value=''>{t('All sources')}</option>
        <option value='topup'>{t('Top-up')}</option>
        <option value='membership'>{t('Subscription payment')}</option>
        <option value='legacy_subscription'>{t('Legacy subscription')}</option>
      </NativeSelect>
      <NativeSelect
        aria-label={t('Currency')}
        value={props.value.currency || ''}
        onChange={(event) => update('currency', event.target.value)}
      >
        <option value=''>{t('All currencies')}</option>
        {(props.currencies || [{ currency: 'CNY' }]).map(({ currency }) => (
          <option value={currency} key={currency}>
            {currency}
          </option>
        ))}
      </NativeSelect>
      {props.status && (
        <NativeSelect
          aria-label={t('Status')}
          value={props.value.status || ''}
          onChange={(event) => update('status', event.target.value)}
        >
          <option value=''>{t('All statuses')}</option>
          {invoiceStatuses.map((status) => (
            <option key={status} value={status}>
              {t(statusKeys[status])}
            </option>
          ))}
        </NativeSelect>
      )}
      <div className='flex min-w-0 items-center gap-2'>
        <Input
          aria-label={t('From date')}
          type='date'
          className='w-36'
          value={invoiceFilterDate(props.value.from, false, props.timezone)}
          onChange={(event) =>
            update(
              'from',
              dateFilter(event.target.value, false, props.timezone) || ''
            )
          }
        />
        <span aria-hidden='true'>–</span>
        <Input
          aria-label={t('To date')}
          type='date'
          className='w-36'
          value={invoiceFilterDate(props.value.to, true, props.timezone)}
          onChange={(event) =>
            update(
              'to',
              dateFilter(event.target.value, true, props.timezone) || ''
            )
          }
        />
      </div>
      <Button
        type='button'
        size='sm'
        variant='ghost'
        onClick={() =>
          props.onChange({
            p: 1,
            page_size: 20,
            group: props.value.group,
            queue: props.value.queue,
          })
        }
      >
        {t('Reset filters')}
      </Button>
    </div>
  )
}

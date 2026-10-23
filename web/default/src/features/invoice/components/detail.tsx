import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import {
  formatInvoiceMoney,
  invoiceDate,
  invoiceOrderKey,
  statusKeys,
} from '../lib/format'
import type { InvoiceRequest } from '../types'
import { InvoiceFileButton, InvoiceFileCard } from './files'
import { InvoiceStatusBadge } from './shared'
import { InvoiceUserActions } from './user-actions'

const eventKeys: Record<string, string> = {
  submitted: 'Invoice request submitted',
  approve: 'Request approved',
  approved: 'Request approved',
  reject: 'Request rejected',
  rejected: 'Request rejected',
  delivered: 'Invoice delivered',
  delivery: 'Invoice delivered',
  confirm: 'Invoice confirmed',
  confirmed: 'Invoice confirmed',
  dispute: 'Problem reported',
  disputed: 'Problem reported',
  message: 'Additional information received',
  reply: 'Administrator replied',
  reissue: 'Corrected invoice delivered',
  hold: 'Finance review required',
  resume: 'Invoice processing resumed',
}
const disputeCategoryKeys: Record<string, string> = {
  title_tax: 'Incorrect title or tax ID',
  amount: 'Incorrect amount',
  attachment: 'Attachment cannot be opened',
  incomplete: 'Incomplete invoice information',
  other: 'Other',
}
export function InvoiceDetail(props: {
  invoice: InvoiceRequest
  admin?: boolean
  children?: React.ReactNode
}) {
  const { t, i18n } = useTranslation()
  const invoice = props.invoice
  let notificationMessage = t(
    'Your invoice is available securely in your account.'
  )
  if (invoice.notification_delivery_state === 'retrying') {
    notificationMessage = t(
      'Invoice delivered. Email notification will be retried.'
    )
  }
  if (invoice.notification_delivery_state === 'failed') {
    notificationMessage = t(
      'Invoice delivered. Email notification could not be sent. You can access the invoice here.'
    )
  }
  return (
    <section className='invoice-panel'>
      <div className='invoice-panel-head flex flex-wrap items-start justify-between gap-3'>
        <div>
          <p className='text-muted-foreground text-xs wrap-anywhere'>
            {invoice.display_no}
          </p>
          <h2 className='mt-2 text-[28px] font-medium tracking-tight tabular-nums'>
            {formatInvoiceMoney(
              invoice.amount_minor,
              invoice.currency,
              invoice.currency_exponent,
              i18n.language
            )}
          </h2>
        </div>
        <InvoiceStatusBadge status={invoice.status} />
      </div>
      <div className='invoice-panel-body space-y-5'>
        {invoice.reason && (
          <div className='rounded-lg bg-amber-500/10 p-3 text-sm leading-relaxed'>
            {invoice.reason}
          </div>
        )}
        {invoice.status === 'finance_hold' && (
          <p className='rounded-lg bg-amber-500/10 p-3 text-sm'>
            {t(
              'Processing is paused for finance review. Delivered files remain available.'
            )}{' '}
            {invoice.hold_from_status &&
              t(statusKeys[invoice.hold_from_status])}
          </p>
        )}
        {!props.admin && (
          <InvoiceUserActions key={invoice.public_id} invoice={invoice} />
        )}
        <section>
          <h3 className='mb-2 text-sm font-medium'>
            {t('Invoice information')}
          </h3>
          {props.admin && (
            <div className='invoice-data-row'>
              <span>{t('User')}</span>
              <span>{invoice.username || invoice.user_id}</span>
            </div>
          )}
          <div className='invoice-data-row'>
            <span>{t('Seller')}</span>
            <span>{invoice.seller_name}</span>
          </div>
          <div className='invoice-data-row'>
            <span>{t('Invoice title')}</span>
            <span>{invoice.profile.title}</span>
          </div>
          {invoice.profile.kind === 'company' && (
            <div className='invoice-data-row'>
              <span>{t('Tax ID')}</span>
              <span>{invoice.profile.tax_id}</span>
            </div>
          )}
          <div className='invoice-data-row'>
            <span>{t('Invoice type')}</span>
            <span>{invoice.invoice_type}</span>
          </div>
          <div className='invoice-data-row'>
            <span>{t('Invoice email')}</span>
            <span>{invoice.profile.email}</span>
          </div>
          {(['address', 'phone', 'bank_name', 'bank_account'] as const).map(
            (field) =>
              invoice.profile[field] && (
                <div className='invoice-data-row' key={field}>
                  <span>
                    {t(
                      {
                        address: 'Address',
                        phone: 'Phone',
                        bank_name: 'Bank name',
                        bank_account: 'Bank account',
                      }[field]
                    )}
                  </span>
                  <span>{invoice.profile[field]}</span>
                </div>
              )
          )}
          {invoice.note && (
            <div className='invoice-data-row'>
              <span>{t('Additional note')}</span>
              <span>{invoice.note}</span>
            </div>
          )}
        </section>
        <section>
          <h3 className='mb-3 text-sm font-medium'>
            {t('Related orders')}{' '}
            <span className='text-muted-foreground font-normal'>
              {invoice.items_count}
            </span>
          </h3>
          {invoice.items?.map((order) => (
            <div key={invoiceOrderKey(order)} className='invoice-data-row'>
              <div className='min-w-0! shrink!'>
                <p className='text-foreground wrap-anywhere'>
                  {order.order_no}
                </p>
                <p className='mt-1 text-xs'>
                  {order.source_type === 'topup'
                    ? t('Top-up')
                    : t('Subscription payment')}{' '}
                  · {invoiceDate(order.paid_at, i18n.language)}
                </p>
                <p className='mt-1 text-xs'>{order.payment_label}</p>
              </div>
              <span className='shrink-0 tabular-nums'>
                {formatInvoiceMoney(
                  order.amount_minor,
                  order.currency,
                  order.currency_exponent,
                  i18n.language
                )}
              </span>
            </div>
          ))}
        </section>
        {invoice.current_delivery && (
          <section className='space-y-3'>
            <h3 className='text-sm font-medium'>{t('Current invoice')}</h3>
            <InvoiceFileCard
              requestId={invoice.public_id}
              delivery={invoice.current_delivery}
              current
              exponent={invoice.currency_exponent}
              admin={props.admin}
            />
            {invoice.notification_delivery_state && (
              <p className='text-muted-foreground text-xs'>
                {notificationMessage}
              </p>
            )}
          </section>
        )}
        {!!invoice.delivered_versions?.filter(
          (delivery) =>
            delivery.delivery_version !== invoice.current_delivery_version
        ).length && (
          <details>
            <summary className='cursor-pointer text-sm font-medium'>
              {t('Invoice history')}
            </summary>
            <div className='mt-3 space-y-3'>
              {invoice.delivered_versions
                .filter(
                  (delivery) =>
                    delivery.delivery_version !==
                    invoice.current_delivery_version
                )
                .map((delivery) => (
                  <InvoiceFileCard
                    key={delivery.delivery_version}
                    requestId={invoice.public_id}
                    delivery={delivery}
                    current={false}
                    exponent={invoice.currency_exponent}
                    admin={props.admin}
                  />
                ))}
            </div>
          </details>
        )}
        {invoice.active_dispute && (
          <section className='space-y-3 rounded-lg border border-amber-500/25 bg-amber-500/5 p-4'>
            <h3 className='text-sm font-medium'>
              {t('Invoice feedback')} ·{' '}
              {t('Version {{version}}', {
                version: invoice.active_dispute.delivery_version,
              })}
            </h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                disputeCategoryKeys[invoice.active_dispute.category] || 'Other'
              )}{' '}
              · {invoiceDate(invoice.active_dispute.created_at, i18n.language)}
            </p>
            <p className='text-sm leading-relaxed whitespace-pre-wrap'>
              {invoice.active_dispute.description}
            </p>
            {invoice.active_dispute.evidence && (
              <InvoiceFileButton
                requestId={invoice.public_id}
                file={invoice.active_dispute.evidence}
                admin={props.admin}
              />
            )}
            {invoice.active_dispute.reply && (
              <div className='border-t pt-3'>
                <p className='text-muted-foreground text-xs'>
                  {t('Administrator reply')}
                </p>
                <p className='mt-2 text-sm leading-relaxed whitespace-pre-wrap'>
                  {invoice.active_dispute.reply}
                </p>
              </div>
            )}
          </section>
        )}
        {props.children}
        <section>
          <h3 className='mb-4 text-sm font-medium'>{t('Invoice timeline')}</h3>
          <ol className='ml-1 space-y-0 border-l'>
            {invoice.events?.map((event) => (
              <li key={event.id} className='relative pb-5 pl-5 last:pb-0'>
                <span className='border-background bg-primary absolute top-1 -left-1 size-2 rounded-full border' />
                <p className='text-[13px] font-medium'>
                  {t(eventKeys[event.kind] || statusKeys[event.status])}
                </p>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {invoiceDate(event.created_at, i18n.language)}
                </p>
                {event.description && (
                  <p className='text-muted-foreground mt-2 text-xs leading-relaxed whitespace-pre-wrap'>
                    {event.description}
                  </p>
                )}
                {event.attachment && (
                  <div className='mt-2'>
                    <InvoiceFileButton
                      requestId={invoice.public_id}
                      file={event.attachment}
                      admin={props.admin}
                    />
                  </div>
                )}
              </li>
            ))}
          </ol>
        </section>
        {invoice.status === 'rejected' && !props.admin && (
          <Link
            to='/invoices/new'
            className='text-primary inline-block text-sm underline'
          >
            {t('Create a new invoice request')}
          </Link>
        )}
      </div>
    </section>
  )
}

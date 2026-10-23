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
import { useTranslation } from 'react-i18next'

import { formatTimestamp } from '../lib/billing'
import { formatPaymentMoney } from '../lib/payment-flow'
import type { PaymentReceipt } from '../payment-flow-types'

const RECEIPT_STATUS_LABELS: Record<string, string> = {
  verified: 'Verified',
  pending: 'Pending',
  review: 'Under review',
  credited: 'Credits added',
}
export function PaymentEvidence(props: { receipts: PaymentReceipt[] }) {
  const { t, i18n } = useTranslation()
  return (
    <section
      className='mt-5 space-y-3 border-t pt-4'
      aria-label={t('Payment receipts')}
    >
      <h4 className='font-medium'>{t('Payment receipts')}</h4>
      {props.receipts.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t('No verified payment receipt is recorded for this order.')}
        </p>
      )}
      {props.receipts.map((receipt) => (
        <dl
          key={receipt.id}
          className='space-y-2 rounded-md border p-3 text-sm'
        >
          <div>
            <dt className='text-muted-foreground'>{t('Receipt ID')}</dt>
            <dd className='break-all'>{receipt.id}</dd>
          </div>
          <div>
            <dt className='text-muted-foreground'>{t('Payment received')}</dt>
            <dd>{formatPaymentMoney(receipt.payment, i18n.language)}</dd>
          </div>
          <div>
            <dt className='text-muted-foreground'>
              {t('Verification status')}
            </dt>
            <dd>
              {t(
                RECEIPT_STATUS_LABELS[receipt.verification_status] ??
                  'Status unavailable'
              )}
            </dd>
          </div>
          <div>
            <dt className='text-muted-foreground'>{t('Processing status')}</dt>
            <dd>
              {t(
                RECEIPT_STATUS_LABELS[receipt.processing_status] ??
                  'Status unavailable'
              )}
            </dd>
          </div>
          {receipt.paid_at && (
            <div>
              <dt className='text-muted-foreground'>{t('Payment time')}</dt>
              <dd>{formatTimestamp(receipt.paid_at)}</dd>
            </div>
          )}
          <div>
            <dt className='text-muted-foreground'>{t('Receipt recorded')}</dt>
            <dd>{formatTimestamp(receipt.received_at)}</dd>
          </div>
          <div>
            <dt className='text-muted-foreground'>{t('Evidence source')}</dt>
            <dd className='break-all'>
              <code>{receipt.evidence_source}</code>
            </dd>
          </div>
          {receipt.exception_reason && (
            <div>
              <dt className='text-muted-foreground'>{t('Exception reason')}</dt>
              <dd className='break-all'>
                <code>{receipt.exception_reason}</code>
              </dd>
            </div>
          )}
          {receipt.review_owner && (
            <div>
              <dt className='text-muted-foreground'>{t('Responsible team')}</dt>
              <dd className='break-all'>{receipt.review_owner}</dd>
            </div>
          )}
        </dl>
      ))}
    </section>
  )
}

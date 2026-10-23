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

import { formatPaymentMoney } from '../lib/payment-flow'
import type { PaymentPricing } from '../payment-flow-types'

export function PaymentAmounts(props: { pricing: PaymentPricing }) {
  const { t, i18n } = useTranslation()
  const pricing = props.pricing
  const payable = pricing.payable_payment
  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-baseline justify-between gap-2'>
        <span className='text-muted-foreground text-sm'>
          {payable ? t('Amount due') : t('Order amount')}
        </span>
        <strong className='text-2xl font-semibold break-all tabular-nums'>
          {formatPaymentMoney(
            payable ?? pricing.expected_payment,
            i18n.language
          )}
        </strong>
      </div>
      {pricing.pricing_mode === 'channel_adjusted' && !payable && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'The final amount includes any valid discounts and taxes shown on the payment page.'
          )}
        </p>
      )}
      {pricing.pricing_mode === 'unknown' && (
        <p className='text-muted-foreground text-sm'>
          {t('The original payment amount is being verified.')}
        </p>
      )}
      <div className='flex flex-wrap justify-between gap-2 text-sm'>
        <span className='text-muted-foreground'>{t('Credited Amount')}</span>
        <span className='font-medium break-all'>
          {pricing.credit_display
            ? `${pricing.credit_display.amount} ${pricing.credit_display.unit}`
            : t('Not available')}
        </span>
      </div>
    </div>
  )
}

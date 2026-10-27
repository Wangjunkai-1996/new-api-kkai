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
import { motion, useReducedMotion } from 'motion/react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { MOTION_TRANSITION } from '@/lib/motion'

import { formatPaymentMoney } from '../lib/payment-flow'
import type { MoneyDTO, PaymentPricing } from '../payment-flow-types'

function getDiscountBreakdown(
  payment: MoneyDTO | null,
  discountRate?: number
): { original: MoneyDTO; savings: MoneyDTO } | null {
  if (!payment || !discountRate || discountRate <= 0 || discountRate >= 1) {
    return null
  }
  const discountedMinor = Number(payment.amount_minor)
  if (!Number.isSafeInteger(discountedMinor) || discountedMinor <= 0) {
    return null
  }
  const originalMinor = Math.round(discountedMinor / discountRate)
  if (
    !Number.isSafeInteger(originalMinor) ||
    originalMinor <= discountedMinor
  ) {
    return null
  }
  return {
    original: { ...payment, amount_minor: String(originalMinor) },
    savings: {
      ...payment,
      amount_minor: String(originalMinor - discountedMinor),
    },
  }
}

export function PaymentAmounts(props: {
  pricing: PaymentPricing
  discountRate?: number
  variant?: 'default' | 'wallet'
}) {
  const { t, i18n } = useTranslation()
  const reduceMotion = useReducedMotion()
  const pricing = props.pricing
  const payable = pricing.payable_payment
  const displayedPayment = payable ?? pricing.expected_payment
  const discount = getDiscountBreakdown(displayedPayment, props.discountRate)
  if (props.variant === 'wallet') {
    const formattedPayment = formatPaymentMoney(displayedPayment, i18n.language)
    const currency =
      formattedPayment !== '—' ? displayedPayment?.currency : undefined
    return (
      <div className='flex flex-col gap-3'>
        <div className='border-b pb-[22px]'>
          <span className='text-muted-foreground text-xs'>
            {payable ? t('Amount due') : t('Order amount')}
          </span>
          <div
            className='mt-2 flex flex-wrap items-baseline gap-x-2'
            aria-live='polite'
          >
            {currency && (
              <span className='text-muted-foreground text-[13px]'>
                {currency}
              </span>
            )}
            <motion.strong
              key={formattedPayment}
              initial={reduceMotion ? false : { opacity: 0, y: 5 }}
              animate={{ opacity: 1, y: 0 }}
              transition={MOTION_TRANSITION.fast}
              className='min-w-0 text-[35px] leading-tight font-semibold tracking-tight break-all tabular-nums'
            >
              {currency
                ? formattedPayment.slice(currency.length + 1)
                : formattedPayment}
            </motion.strong>
          </div>
          {discount && (
            <Badge
              variant='secondary'
              className='mt-2 h-auto max-w-full rounded-[5px] bg-[var(--wallet-success-soft)] px-2 py-1 text-[11px] font-normal whitespace-normal text-[var(--wallet-success)]'
            >
              {t('You save')}{' '}
              {formatPaymentMoney(discount.savings, i18n.language)}
            </Badge>
          )}
        </div>
        <div className='flex justify-between gap-3 text-xs'>
          <span className='text-muted-foreground'>{t('Credited Amount')}</span>
          <span className='min-w-0 text-right font-medium break-all tabular-nums'>
            {pricing.credit_display
              ? `${pricing.credit_display.amount} ${pricing.credit_display.unit}`
              : t('Not available')}
          </span>
        </div>
        {discount && (
          <div className='flex justify-between gap-3 text-xs'>
            <span className='text-muted-foreground'>{t('Original Price')}</span>
            <span className='min-w-0 text-right font-medium break-all tabular-nums line-through'>
              {formatPaymentMoney(discount.original, i18n.language)}
            </span>
          </div>
        )}
        {pricing.pricing_mode === 'channel_adjusted' && !payable && (
          <p className='text-muted-foreground text-xs'>
            {t(
              'The final amount includes any valid discounts and taxes shown on the payment page.'
            )}
          </p>
        )}
        {pricing.pricing_mode === 'unknown' && (
          <p className='text-muted-foreground text-xs'>
            {t('The original payment amount is being verified.')}
          </p>
        )}
      </div>
    )
  }
  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-baseline justify-between gap-2'>
        <span className='text-muted-foreground text-sm'>
          {payable ? t('Amount due') : t('Order amount')}
        </span>
        <strong className='text-2xl font-semibold break-all tabular-nums'>
          {formatPaymentMoney(displayedPayment, i18n.language)}
        </strong>
      </div>
      {discount && (
        <>
          <div className='flex flex-wrap justify-between gap-2 text-sm'>
            <span className='text-muted-foreground'>{t('Original Price')}</span>
            <span className='text-muted-foreground font-medium break-all tabular-nums line-through'>
              {formatPaymentMoney(discount.original, i18n.language)}
            </span>
          </div>
          <div className='flex flex-wrap justify-between gap-2 text-sm'>
            <span className='text-muted-foreground'>{t('You save')}</span>
            <span className='font-medium break-all text-emerald-700 tabular-nums dark:text-emerald-300'>
              {formatPaymentMoney(discount.savings, i18n.language)}
            </span>
          </div>
        </>
      )}
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

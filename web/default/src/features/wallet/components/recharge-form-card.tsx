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
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  ChevronDown,
  Loader2,
  Plus,
  Receipt,
  Ticket,
} from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { getCurrencyDisplay } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { MOTION_TRANSITION } from '@/lib/motion'
import { cn } from '@/lib/utils'

import { PAYMENT_TYPES, resolveTopupLink } from '../constants'
import {
  formatTopupCreditAmount,
  formatWalletPaymentAmount,
  getMinTopupAmount,
  getPaymentIcon,
} from '../lib'
import type { PaymentQuote } from '../payment-flow-types'
import type {
  CreemProduct,
  PaymentMethod,
  PresetAmount,
  TopupInfo,
  WaffoPayMethod,
} from '../types'
import { CreemProductsSection } from './creem-products-section'
import { PaymentAmounts } from './payment-amounts'

interface RechargeFormCardProps {
  quote?: PaymentQuote
  confirmation?: ReactNode
  purchaseDisabled?: boolean
  topupInfo: TopupInfo | null
  presetAmounts: PresetAmount[]
  selectedPreset: number | null
  onSelectPreset: (preset: PresetAmount) => void
  topupAmount: number
  onTopupAmountChange: (amount: number) => void
  paymentAmount: number
  calculating: boolean
  paymentQuoteValid: boolean
  selectedPaymentMethod?: PaymentMethod
  selectedWaffoMethodIndex?: number | null
  onPaymentMethodSelect: (method: PaymentMethod) => void
  onPaymentSubmit: () => void
  paymentLoading: string | null
  redemptionCode: string
  onRedemptionCodeChange: (code: string) => void
  onRedeem: () => void
  redeeming: boolean
  topupLink?: string
  loading?: boolean
  creemProducts?: CreemProduct[]
  enableCreemTopup?: boolean
  onCreemProductSelect?: (product: CreemProduct) => void
  enableWaffoTopup?: boolean
  waffoPayMethods?: WaffoPayMethod[]
  waffoMinTopup?: number
  onWaffoMethodSelect?: (method: WaffoPayMethod, index: number) => void
  enableWaffoPancakeTopup?: boolean
}

export function RechargeFormCard(props: RechargeFormCardProps) {
  const { t } = useTranslation()
  const [customOpen, setCustomOpen] = useState(false)
  const [localAmount, setLocalAmount] = useState('')
  const [redemptionOpen, setRedemptionOpen] = useState(true)
  const reduceMotion = useReducedMotion()
  const customActive = customOpen || props.selectedPreset === null

  const redemptionEnabled = props.topupInfo?.enable_redemption !== false
  const standardMethods = (props.topupInfo?.pay_methods ?? []).filter(
    (method) => {
      if (method.type === PAYMENT_TYPES.STRIPE) {
        return !!props.topupInfo?.enable_stripe_topup
      }
      if (method.type === PAYMENT_TYPES.WAFFO_PANCAKE) {
        return !!props.enableWaffoPancakeTopup
      }
      return !!props.topupInfo?.enable_online_topup
    }
  )
  if (
    props.topupInfo?.enable_stripe_topup &&
    !standardMethods.some((method) => method.type === PAYMENT_TYPES.STRIPE)
  ) {
    standardMethods.push({
      name: 'Stripe',
      type: PAYMENT_TYPES.STRIPE,
      min_topup: props.topupInfo.stripe_min_topup,
    })
  }
  if (
    props.enableWaffoPancakeTopup &&
    !standardMethods.some(
      (method) => method.type === PAYMENT_TYPES.WAFFO_PANCAKE
    )
  ) {
    standardMethods.push({
      name: 'Waffo Pancake',
      type: PAYMENT_TYPES.WAFFO_PANCAKE,
      min_topup: props.topupInfo?.waffo_pancake_min_topup,
    })
  }
  const hasWaffoMethods =
    !!props.enableWaffoTopup &&
    !!props.waffoPayMethods?.length &&
    !!props.onWaffoMethodSelect
  const minimumForMethod = (method: PaymentMethod) => {
    if (method.type === PAYMENT_TYPES.STRIPE) {
      return Math.max(
        1,
        method.min_topup || 0,
        props.topupInfo?.stripe_min_topup || 0
      )
    }
    if (method.type === PAYMENT_TYPES.WAFFO_PANCAKE) {
      return Math.max(
        1,
        method.min_topup || 0,
        props.topupInfo?.waffo_pancake_min_topup || 0
      )
    }
    return Math.max(1, method.min_topup || 0, props.topupInfo?.min_topup || 0)
  }
  const availableMinimums = standardMethods.map(minimumForMethod)
  if (hasWaffoMethods) {
    availableMinimums.push(Math.max(1, props.waffoMinTopup || 1))
  }
  const minTopup =
    availableMinimums.length > 0
      ? Math.min(...availableMinimums)
      : getMinTopupAmount(props.topupInfo)
  const hasConfigurableTopup =
    !!props.topupInfo?.enable_online_topup ||
    !!props.topupInfo?.enable_stripe_topup ||
    !!props.enableWaffoTopup ||
    !!props.enableWaffoPancakeTopup
  const hasCreem =
    !!props.enableCreemTopup &&
    !!props.creemProducts?.length &&
    !!props.onCreemProductSelect
  let methodMin = minTopup
  if (props.selectedPaymentMethod?.type === PAYMENT_TYPES.WAFFO) {
    methodMin = Math.max(1, props.waffoMinTopup || 1)
  } else if (props.selectedPaymentMethod) {
    methodMin = minimumForMethod(props.selectedPaymentMethod)
  }
  const validAmount =
    Number.isSafeInteger(props.topupAmount) && props.topupAmount >= methodMin
  const quoteReady =
    validAmount &&
    props.paymentQuoteValid &&
    !props.calculating &&
    props.paymentAmount > 0
  const discountRate = props.topupInfo?.discount?.[props.topupAmount] ?? 1
  const hasDiscount =
    !props.quote &&
    discountRate > 0 &&
    discountRate < 1 &&
    props.selectedPaymentMethod?.type !== PAYMENT_TYPES.STRIPE
  const originalPrice =
    quoteReady && hasDiscount
      ? props.paymentAmount / discountRate
      : props.paymentAmount
  const discount =
    quoteReady && hasDiscount
      ? Math.max(0, originalPrice - props.paymentAmount)
      : 0
  const tokenDisplay = getCurrencyDisplay().config.quotaDisplayType === 'TOKENS'
  const creditAmount = (amount: number) =>
    formatTopupCreditAmount(amount, t('Tokens'))

  const handleCustomToggle = () => {
    if (customActive && props.presetAmounts.length > 0) {
      setCustomOpen(false)
      props.onSelectPreset(props.presetAmounts[0])
      return
    }
    setCustomOpen(true)
    setLocalAmount('')
    props.onTopupAmountChange(0)
  }

  const handleAmountChange = (value: string) => {
    setCustomOpen(true)
    setLocalAmount(value)
    const amount = /^\d+$/.test(value) ? Number(value) : 0
    props.onTopupAmountChange(Number.isSafeInteger(amount) ? amount : 0)
  }

  if (props.loading) {
    return (
      <section className='space-y-5' aria-label={t('Add Funds')}>
        <Skeleton className='h-6 w-32' />
        <div className='grid gap-6 lg:grid-cols-[minmax(0,1fr)_18rem]'>
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
            {Array.from({ length: 6 }, (_, index) => (
              <Skeleton key={index} className='h-[74px] rounded-md' />
            ))}
          </div>
          <Skeleton className='h-64 rounded-md' />
        </div>
      </section>
    )
  }

  return (
    <section className='min-w-0' aria-labelledby='wallet-recharge-heading'>
      <div className='mb-4 flex items-end gap-5 sm:mb-5'>
        <h2
          id='wallet-recharge-heading'
          className='shrink-0 text-lg font-semibold'
        >
          {t('Online Top-up')}
        </h2>
        <span className='bg-border mb-1 h-px flex-1' aria-hidden='true' />
      </div>

      {hasConfigurableTopup && (
        <div className='grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_18rem] lg:gap-7'>
          <div className='min-w-0'>
            <div className='mb-3 flex items-start justify-between gap-3'>
              <div>
                <h3 className='text-sm font-medium'>
                  {t('Select credit amount')}
                </h3>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t('For API usage')}
                </p>
              </div>
              <span className='text-muted-foreground text-xs'>
                {tokenDisplay ? t('Tokens') : 'USD · $'}
              </span>
            </div>

            {props.presetAmounts.length > 0 && (
              <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
                {props.presetAmounts.map((preset) => {
                  const presetDiscount =
                    preset.discount ??
                    props.topupInfo?.discount?.[preset.value] ??
                    1
                  const selected =
                    props.selectedPreset === preset.value && !customActive
                  let discountLabel = ''
                  if (selected && hasDiscount && discount > 0) {
                    discountLabel = `${t('You save')} ${formatWalletPaymentAmount(
                      discount,
                      props.selectedPaymentMethod?.type
                    )}`
                  } else if (
                    presetDiscount > 0 &&
                    presetDiscount < 1 &&
                    props.selectedPaymentMethod?.type !== PAYMENT_TYPES.STRIPE
                  ) {
                    discountLabel = `${t('Discount')} ${Math.round((1 - presetDiscount) * 100)}%`
                  }
                  return (
                    <button
                      key={preset.value}
                      type='button'
                      aria-pressed={selected}
                      onClick={() => {
                        setCustomOpen(false)
                        props.onSelectPreset(preset)
                      }}
                      className={cn(
                        'group relative flex min-h-[72px] min-w-0 flex-col justify-between gap-2 rounded-md border bg-card px-3 py-2.5 text-left transition-[transform,border-color,box-shadow,background-color] duration-200 hover:-translate-y-0.5 hover:border-primary/60 hover:shadow-sm active:scale-[0.98] motion-reduce:transition-none motion-reduce:hover:transform-none motion-reduce:active:transform-none',
                        selected &&
                          'border-primary bg-primary/[0.06] shadow-[inset_0_0_0_1px_var(--primary)]'
                      )}
                    >
                      <span className='flex w-full items-start justify-between gap-1'>
                        <strong className='min-w-0 text-base leading-none font-semibold break-all tabular-nums sm:text-xl'>
                          {tokenDisplay ? '' : '$'}
                          {formatNumber(preset.value)}
                        </strong>
                        {selected && (
                          <Check
                            className='text-primary size-3.5 shrink-0'
                            aria-hidden='true'
                          />
                        )}
                      </span>
                      {discountLabel ? (
                        <span className='inline-flex min-h-5 w-fit max-w-full items-center rounded-sm bg-emerald-600 px-1.5 py-1 text-[11px] leading-tight font-semibold break-words text-white tabular-nums dark:bg-emerald-500 dark:text-emerald-950'>
                          {discountLabel}
                        </span>
                      ) : (
                        <span className='h-5' aria-hidden='true' />
                      )}
                    </button>
                  )
                })}
              </div>
            )}

            <div className='mt-3 flex flex-wrap items-center justify-between gap-x-3 gap-y-1'>
              <button
                type='button'
                onClick={handleCustomToggle}
                aria-expanded={customActive}
                aria-controls='wallet-custom-amount'
                className='text-primary inline-flex items-center gap-1 text-xs font-medium hover:underline'
              >
                <Plus className='size-3.5' aria-hidden='true' />{' '}
                {t('Custom Amount')}
              </button>
              <span className='text-muted-foreground text-xs'>
                {t('Minimum topup amount: {{amount}}', {
                  amount: creditAmount(minTopup),
                })}
              </span>
            </div>
            {customActive && (
              <div id='wallet-custom-amount' className='mt-3'>
                <label
                  htmlFor='topup-amount'
                  className='text-muted-foreground mb-1.5 block text-xs'
                >
                  {t('Custom Amount')}
                </label>
                <Input
                  id='topup-amount'
                  type='number'
                  inputMode='numeric'
                  min={methodMin}
                  step='1'
                  value={
                    customOpen ? localAmount : String(props.topupAmount || '')
                  }
                  onChange={(event) => handleAmountChange(event.target.value)}
                  className='h-10'
                />
                {!validAmount && (
                  <p className='text-destructive mt-1.5 text-xs' role='alert'>
                    {t('Enter an amount of at least {{amount}}', {
                      amount: creditAmount(methodMin),
                    })}
                  </p>
                )}
              </div>
            )}

            <div className='mt-5 border-t pt-5 sm:mt-7'>
              <h3 className='mb-3 text-sm font-medium'>
                {t('Payment Method')}
              </h3>
              <div className='grid grid-cols-1 gap-2 sm:grid-cols-2'>
                {standardMethods.map((method) => {
                  const minimum = minimumForMethod(method)
                  const disabled = props.topupAmount < minimum
                  const selected =
                    props.selectedPaymentMethod?.type === method.type
                  return (
                    <button
                      key={method.type}
                      type='button'
                      disabled={disabled}
                      title={
                        disabled
                          ? t('Minimum topup amount: {{amount}}', {
                              amount: creditAmount(minimum),
                            })
                          : undefined
                      }
                      aria-pressed={selected}
                      onClick={() => props.onPaymentMethodSelect(method)}
                      className={cn(
                        'flex h-12 min-w-0 items-center gap-2.5 rounded-md border bg-card px-3 text-left text-sm transition-[transform,border-color,box-shadow,background-color] duration-200 hover:-translate-y-0.5 hover:border-primary/60 hover:shadow-sm disabled:cursor-not-allowed disabled:opacity-50 motion-reduce:transition-none motion-reduce:hover:transform-none',
                        selected && 'border-primary bg-primary/[0.07]'
                      )}
                    >
                      {getPaymentIcon(
                        method.type,
                        'size-5 shrink-0',
                        method.icon,
                        method.name
                      )}
                      <span className='min-w-0 flex-1 truncate'>
                        {method.name}
                      </span>
                      {selected && (
                        <Check
                          className='text-primary size-4 shrink-0'
                          aria-hidden='true'
                        />
                      )}
                    </button>
                  )
                })}
                {hasWaffoMethods &&
                  props.waffoPayMethods?.map((method, index) => {
                    const selected =
                      props.selectedPaymentMethod?.type ===
                        PAYMENT_TYPES.WAFFO &&
                      props.selectedWaffoMethodIndex === index
                    const waffoMinimum = Math.max(1, props.waffoMinTopup || 1)
                    const disabled = props.topupAmount < waffoMinimum
                    return (
                      <button
                        key={`${method.payMethodType ?? ''}-${method.payMethodName ?? ''}-${method.name}`}
                        type='button'
                        disabled={disabled}
                        title={
                          disabled
                            ? t('Minimum topup amount: {{amount}}', {
                                amount: creditAmount(waffoMinimum),
                              })
                            : undefined
                        }
                        aria-pressed={selected}
                        onClick={() =>
                          props.onWaffoMethodSelect?.(method, index)
                        }
                        className={cn(
                          'flex h-12 min-w-0 items-center gap-2.5 rounded-md border bg-card px-3 text-left text-sm transition-[transform,border-color,box-shadow,background-color] duration-200 hover:-translate-y-0.5 hover:border-primary/60 hover:shadow-sm disabled:cursor-not-allowed disabled:opacity-50 motion-reduce:transition-none motion-reduce:hover:transform-none',
                          selected && 'border-primary bg-primary/[0.07]'
                        )}
                      >
                        {method.icon ? (
                          <img
                            src={method.icon}
                            alt=''
                            className='size-5 shrink-0 object-contain'
                          />
                        ) : (
                          getPaymentIcon(PAYMENT_TYPES.WAFFO, 'size-5 shrink-0')
                        )}
                        <span className='min-w-0 flex-1 truncate'>
                          {method.name}
                        </span>
                        {selected && (
                          <Check
                            className='text-primary size-4 shrink-0'
                            aria-hidden='true'
                          />
                        )}
                      </button>
                    )
                  })}
              </div>
              {standardMethods.length === 0 && !hasWaffoMethods && (
                <Alert className='mt-2'>
                  <AlertDescription>
                    {t(
                      'No payment methods available. Please contact administrator.'
                    )}
                  </AlertDescription>
                </Alert>
              )}
            </div>
          </div>

          <aside
            className='bg-card border-t-primary rounded-md border border-t-2 p-5 shadow-sm transition-shadow duration-200 hover:shadow-md motion-reduce:transition-none lg:sticky lg:top-5'
            aria-label={t('Payment Summary')}
          >
            <div className='mb-6 flex items-center justify-between text-sm font-medium'>
              <span>{t('Payment Summary')}</span>
              <Receipt
                className='text-muted-foreground size-4'
                aria-hidden='true'
              />
            </div>
            {props.quote ? (
              <PaymentAmounts pricing={props.quote} />
            ) : (
              <>
                <div className='space-y-4 text-xs'>
                  <div className='flex justify-between gap-3'>
                    <span className='text-muted-foreground'>
                      {t('Credited Amount')}
                    </span>
                    <strong className='text-right font-medium tabular-nums'>
                      {validAmount ? creditAmount(props.topupAmount) : '-'}
                    </strong>
                  </div>
                  {hasDiscount && quoteReady && (
                    <div className='flex justify-between gap-3'>
                      <span className='text-muted-foreground'>
                        {t('Original Price')}
                      </span>
                      <strong className='text-muted-foreground text-right font-medium tabular-nums line-through'>
                        {formatWalletPaymentAmount(
                          originalPrice,
                          props.selectedPaymentMethod?.type
                        )}
                      </strong>
                    </div>
                  )}
                </div>
                {discount > 0 && (
                  <div className='mt-5 flex items-center justify-between gap-3 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2.5 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-300'>
                    <span className='font-medium'>{t('You save')}</span>
                    <strong className='font-semibold tabular-nums'>
                      {formatWalletPaymentAmount(
                        discount,
                        props.selectedPaymentMethod?.type
                      )}
                    </strong>
                  </div>
                )}
                <div className='mt-5 flex items-end justify-between gap-3 border-t pt-5'>
                  <span className='shrink-0 text-xs'>{t('You Pay')}</span>
                  <motion.strong
                    key={`${props.selectedPaymentMethod?.type}-${quoteReady ? props.paymentAmount : 'pending'}`}
                    initial={reduceMotion ? false : { opacity: 0, y: 5 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={MOTION_TRANSITION.fast}
                    className='min-w-0 text-right text-3xl leading-none font-semibold break-all tabular-nums'
                  >
                    {quoteReady
                      ? formatWalletPaymentAmount(
                          props.paymentAmount,
                          props.selectedPaymentMethod?.type
                        )
                      : '-'}
                  </motion.strong>
                </div>
              </>
            )}
            <Button
              className='mt-5 h-11 w-full shadow-sm transition-[transform,box-shadow] duration-200 hover:-translate-y-0.5 hover:shadow-md motion-reduce:transition-none motion-reduce:hover:transform-none'
              disabled={
                props.purchaseDisabled ||
                !quoteReady ||
                !props.selectedPaymentMethod ||
                !!props.paymentLoading
              }
              onClick={props.onPaymentSubmit}
            >
              {props.calculating || props.paymentLoading ? (
                <Loader2 className='size-4 animate-spin' aria-hidden='true' />
              ) : null}
              {t('Review and Pay')}{' '}
              <ArrowRight className='size-4' aria-hidden='true' />
            </Button>
            {props.confirmation}
          </aside>
        </div>
      )}

      {hasCreem && (
        <div className={cn('mt-7', hasConfigurableTopup && 'border-t pt-6')}>
          <h3 className='mb-3 text-sm font-medium'>{t('Creem Payment')}</h3>
          <CreemProductsSection
            disabled={props.purchaseDisabled}
            products={props.creemProducts ?? []}
            onProductSelect={(product) => props.onCreemProductSelect?.(product)}
          />
        </div>
      )}

      {!hasConfigurableTopup && !hasCreem && (
        <Alert>
          <AlertDescription>
            {t('No payment methods available. Please contact administrator.')}
          </AlertDescription>
        </Alert>
      )}

      <div className='bg-muted/25 mt-8 border-y px-4 sm:px-5'>
        {redemptionEnabled ? (
          <details
            className='group'
            open={redemptionOpen}
            onToggle={(event) => setRedemptionOpen(event.currentTarget.open)}
          >
            <summary className='flex cursor-pointer list-none items-center justify-between gap-3 py-4 [&::-webkit-details-marker]:hidden'>
              <span className='flex min-w-0 items-center gap-2.5'>
                <Ticket
                  className='text-primary size-4 shrink-0'
                  aria-hidden='true'
                />
                <strong className='text-sm font-medium'>
                  {t('Redeem with a code')}
                </strong>
                <span className='text-muted-foreground hidden text-xs sm:inline'>
                  {t('Backup option')}
                </span>
              </span>
              <ChevronDown
                className='text-muted-foreground size-4 shrink-0 transition-transform group-open:rotate-180'
                aria-hidden='true'
              />
            </summary>
            <div className='pb-5'>
              <label
                htmlFor='redemption-code'
                className='text-muted-foreground mb-2 block text-xs'
              >
                {t('Redemption Code')}
              </label>
              <div className='flex flex-wrap gap-2'>
                <Input
                  id='redemption-code'
                  value={props.redemptionCode}
                  onChange={(event) =>
                    props.onRedemptionCodeChange(event.target.value)
                  }
                  placeholder={t('Enter your redemption code')}
                  autoComplete='off'
                  className='h-10 min-w-0 flex-1 basis-52'
                />
                <Button
                  variant='outline'
                  className='h-10'
                  onClick={props.onRedeem}
                  disabled={props.redeeming || !props.redemptionCode.trim()}
                >
                  {props.redeeming && (
                    <Loader2
                      className='size-4 animate-spin'
                      aria-hidden='true'
                    />
                  )}
                  {t('Redeem')}
                </Button>
              </div>
              <a
                href={resolveTopupLink(props.topupLink)}
                target='_blank'
                rel='noopener noreferrer'
                className='border-primary/35 bg-primary/10 text-primary hover:bg-primary/15 focus-visible:ring-ring mt-4 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border px-4 text-sm font-semibold shadow-sm transition-[transform,box-shadow,background-color] duration-200 hover:-translate-y-0.5 hover:shadow-md focus-visible:ring-2 focus-visible:outline-none motion-reduce:transition-none motion-reduce:hover:transform-none'
              >
                <Ticket className='size-4' aria-hidden='true' />
                {t('Buy a redemption code')}
                <ArrowUpRight className='size-4' aria-hidden='true' />
              </a>
            </div>
          </details>
        ) : (
          <p className='text-muted-foreground py-4 text-xs'>
            {t(
              'Redemption codes are disabled until the administrator confirms compliance terms.'
            )}
          </p>
        )}
      </div>
    </section>
  )
}

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
import { ArrowUpRight, Receipt } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { MembershipWalletSummary } from '@/features/membership/components/wallet-summary'
import { useSiteConfig } from '@/features/site-config'
import { getSelf } from '@/lib/api'
import { MOTION_TRANSITION } from '@/lib/motion'

import { BillingHistoryDialog } from './components/dialogs/billing-history-dialog'
import { CreemConfirmDialog } from './components/dialogs/creem-confirm-dialog'
import { PaymentConfirmDialog } from './components/dialogs/payment-confirm-dialog'
import { RechargeFormCard } from './components/recharge-form-card'
import { SubscriptionPlansCard } from './components/subscription-plans-card'
import { WalletStatsCard } from './components/wallet-stats-card'
import { DEFAULT_DISCOUNT_RATE, PAYMENT_TYPES } from './constants'
import {
  useTopupInfo,
  usePayment,
  useRedemption,
  useCreemPayment,
  useWaffoPayment,
  useWaffoPancakePayment,
} from './hooks'
import { getMinTopupAmount, dispatchSelectedPayment } from './lib'
import type {
  UserWalletData,
  PaymentMethod,
  PresetAmount,
  CreemProduct,
  WaffoPayMethod,
  TopupInfo,
} from './types'

interface WalletProps {
  initialShowHistory?: boolean
}

function paymentMinimum(method: PaymentMethod, topupInfo: TopupInfo): number {
  if (method.type === PAYMENT_TYPES.STRIPE) {
    return Math.max(1, method.min_topup || 0, topupInfo.stripe_min_topup || 0)
  }
  if (method.type === PAYMENT_TYPES.WAFFO_PANCAKE) {
    return Math.max(
      1,
      method.min_topup || 0,
      topupInfo.waffo_pancake_min_topup || 0
    )
  }
  if (method.type === PAYMENT_TYPES.WAFFO) {
    return Math.max(1, topupInfo.waffo_min_topup || 0)
  }
  return Math.max(1, method.min_topup || 0, topupInfo.min_topup || 0)
}

export function Wallet(props: WalletProps) {
  const { data: site } = useSiteConfig()
  const showPlatformBenefits = site?.tenant_id === 0
  const { t } = useTranslation()
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [userLoading, setUserLoading] = useState(true)
  const [topupAmount, setTopupAmount] = useState(0)
  const [selectedPreset, setSelectedPreset] = useState<number | null>(null)
  const [selectedPaymentMethod, setSelectedPaymentMethod] =
    useState<PaymentMethod>()
  const [selectedWaffoMethodIndex, setSelectedWaffoMethodIndex] = useState<
    number | null
  >(null)
  const [paymentQuote, setPaymentQuote] = useState<{
    amount: number
    paymentType: string
    value: number
  } | null>(null)
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false)
  const [billingDialogOpen, setBillingDialogOpen] = useState(
    !!props.initialShowHistory
  )
  const [redemptionCode, setRedemptionCode] = useState('')
  const [creemDialogOpen, setCreemDialogOpen] = useState(false)
  const [selectedCreemProduct, setSelectedCreemProduct] =
    useState<CreemProduct | null>(null)

  const { topupInfo, presetAmounts, loading: topupLoading } = useTopupInfo()
  const reduceMotion = useReducedMotion()
  const { processing, calculatePaymentAmount, processPayment } = usePayment()
  const { redeeming, redeemCode } = useRedemption()
  const { processing: creemProcessing, processCreemPayment } = useCreemPayment()
  const { processing: waffoProcessing, processWaffoPayment } = useWaffoPayment()
  const { processing: pancakeProcessing, processWaffoPancakePayment } =
    useWaffoPancakePayment()

  const paymentMethods = useMemo(() => {
    if (!topupInfo) return []
    const methods = topupInfo.pay_methods.filter((method) => {
      if (method.type === PAYMENT_TYPES.STRIPE) {
        return topupInfo.enable_stripe_topup
      }
      if (method.type === PAYMENT_TYPES.WAFFO_PANCAKE) {
        return !!topupInfo.enable_waffo_pancake_topup
      }
      return topupInfo.enable_online_topup
    })
    if (
      topupInfo.enable_stripe_topup &&
      !methods.some((method) => method.type === PAYMENT_TYPES.STRIPE)
    ) {
      methods.push({
        name: 'Stripe',
        type: PAYMENT_TYPES.STRIPE,
        min_topup: topupInfo.stripe_min_topup,
      })
    }
    if (
      topupInfo.enable_waffo_pancake_topup &&
      !methods.some((method) => method.type === PAYMENT_TYPES.WAFFO_PANCAKE)
    ) {
      methods.push({
        name: 'Waffo Pancake',
        type: PAYMENT_TYPES.WAFFO_PANCAKE,
        min_topup: topupInfo.waffo_pancake_min_topup,
      })
    }
    return methods
  }, [topupInfo])

  const availableMethod = useMemo(() => {
    if (!topupInfo) return undefined
    const standardMethod = paymentMethods.find(
      (method) => topupAmount >= paymentMinimum(method, topupInfo)
    )
    const waffoMethod = topupInfo.waffo_pay_methods?.[0]
    const canUseWaffo =
      topupInfo.enable_waffo_topup &&
      waffoMethod &&
      topupAmount >= Math.max(1, topupInfo.waffo_min_topup || 0)

    if (selectedPaymentMethod?.type === PAYMENT_TYPES.WAFFO && canUseWaffo) {
      const index = selectedWaffoMethodIndex ?? 0
      if (topupInfo.waffo_pay_methods?.[index]) return selectedPaymentMethod
    }
    if (
      selectedPaymentMethod &&
      paymentMethods.some(
        (method) =>
          method.type === selectedPaymentMethod.type &&
          topupAmount >= paymentMinimum(method, topupInfo)
      )
    ) {
      return selectedPaymentMethod
    }
    if (standardMethod) return standardMethod
    if (canUseWaffo) {
      return {
        name: waffoMethod.name,
        type: PAYMENT_TYPES.WAFFO,
        icon: waffoMethod.icon,
      }
    }
    return undefined
  }, [
    paymentMethods,
    selectedPaymentMethod,
    selectedWaffoMethodIndex,
    topupAmount,
    topupInfo,
  ])

  const activeWaffoMethodIndex =
    availableMethod?.type === PAYMENT_TYPES.WAFFO
      ? (selectedWaffoMethodIndex ?? 0)
      : null
  const activePaymentType = availableMethod?.type
  const currentQuote =
    paymentQuote?.amount === topupAmount &&
    paymentQuote.paymentType === activePaymentType
      ? paymentQuote
      : null
  const paymentAmount = currentQuote?.value ?? 0
  const calculating = !!availableMethod && topupAmount > 0 && !currentQuote
  const paymentQuoteValid =
    !!availableMethod &&
    !!currentQuote &&
    Number.isInteger(topupAmount) &&
    !!topupInfo &&
    topupAmount >= paymentMinimum(availableMethod, topupInfo) &&
    Number.isFinite(paymentAmount) &&
    paymentAmount > 0

  // Fetch and refresh user data
  const fetchUser = useCallback(async () => {
    try {
      const response = await getSelf()
      if (response.success && response.data) {
        setUser(response.data as UserWalletData)
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch user data:', error)
    } finally {
      setUserLoading(false)
    }
  }, [])

  useEffect(() => {
    // eslint-disable-next-line react/set-state-in-effect -- Loading the current user is an external request.
    fetchUser()
  }, [fetchUser])

  useEffect(() => {
    if (props.initialShowHistory) {
      window.history.replaceState({}, '', window.location.pathname)
    }
  }, [props.initialShowHistory])

  // Initialize topup amount when topup info is loaded
  const topupAmountInitializedRef = useRef(false)
  useEffect(() => {
    if (topupInfo && !topupAmountInitializedRef.current) {
      topupAmountInitializedRef.current = true
      const minimums = paymentMethods.map((method) =>
        paymentMinimum(method, topupInfo)
      )
      if (topupInfo.enable_waffo_topup && topupInfo.waffo_pay_methods?.length) {
        minimums.push(Math.max(1, topupInfo.waffo_min_topup || 0))
      }
      const minTopup = minimums.length
        ? Math.min(...minimums)
        : getMinTopupAmount(topupInfo)
      const preset =
        presetAmounts.find(
          (item) =>
            item.value >= minTopup &&
            (paymentMethods.some(
              (method) => item.value >= paymentMinimum(method, topupInfo)
            ) ||
              (topupInfo.enable_waffo_topup &&
                !!topupInfo.waffo_pay_methods?.length &&
                item.value >= Math.max(1, topupInfo.waffo_min_topup || 0)))
        ) ?? presetAmounts.find((item) => item.value >= minTopup)
      setTopupAmount(preset?.value ?? minTopup)
      setSelectedPreset(preset?.value ?? null)
    }
  }, [topupInfo, presetAmounts, paymentMethods])

  useEffect(() => {
    if (!activePaymentType || topupAmount <= 0) return
    let cancelled = false
    calculatePaymentAmount(topupAmount, activePaymentType)
      .then((value) => {
        if (!cancelled) {
          setPaymentQuote({
            amount: topupAmount,
            paymentType: activePaymentType,
            value,
          })
        }
      })
      .catch(() => {
        if (!cancelled) {
          setPaymentQuote({
            amount: topupAmount,
            paymentType: activePaymentType,
            value: 0,
          })
        }
      })
    return () => {
      cancelled = true
    }
  }, [activePaymentType, topupAmount, calculatePaymentAmount])

  // Handle preset selection
  const handleSelectPreset = (preset: PresetAmount) => {
    if (preset.value !== topupAmount) setPaymentQuote(null)
    setTopupAmount(preset.value)
    setSelectedPreset(preset.value)
  }

  // Handle topup amount change
  const handleTopupAmountChange = (amount: number) => {
    if (amount !== topupAmount) setPaymentQuote(null)
    setTopupAmount(amount)
    setSelectedPreset(null)
  }

  const handlePaymentMethodSelect = (method: PaymentMethod) => {
    if (method.type !== activePaymentType) setPaymentQuote(null)
    setSelectedPaymentMethod(method)
    setSelectedWaffoMethodIndex(null)
  }

  const handlePaymentSubmit = () => {
    if (paymentQuoteValid) setConfirmDialogOpen(true)
  }

  // Handle payment confirmation
  const handlePaymentConfirm = async () => {
    if (!availableMethod || !paymentQuoteValid) return

    const success = await dispatchSelectedPayment(
      availableMethod,
      topupAmount,
      activeWaffoMethodIndex,
      {
        regular: processPayment,
        waffo: processWaffoPayment,
        waffoPancake: processWaffoPancakePayment,
      }
    )

    if (success) {
      setConfirmDialogOpen(false)
      await fetchUser()
    }
  }

  // Handle redemption
  const handleRedeem = async () => {
    if (!redemptionCode) return

    const success = await redeemCode(redemptionCode)
    if (success) {
      setRedemptionCode('')
      await fetchUser()
    }
  }

  // Handle Creem product selection
  const handleCreemProductSelect = (product: CreemProduct) => {
    setSelectedCreemProduct(product)
    setCreemDialogOpen(true)
  }

  // Handle Creem payment confirmation
  const handleCreemConfirm = async () => {
    if (!selectedCreemProduct) return

    const success = await processCreemPayment(selectedCreemProduct.productId)
    if (success) {
      setCreemDialogOpen(false)
      setSelectedCreemProduct(null)
      await fetchUser()
    }
  }

  const handleWaffoMethodSelect = (method: WaffoPayMethod, index: number) => {
    if (activePaymentType !== PAYMENT_TYPES.WAFFO) setPaymentQuote(null)
    setSelectedPaymentMethod({
      name: method.name,
      type: PAYMENT_TYPES.WAFFO,
      icon: method.icon,
    })
    setSelectedWaffoMethodIndex(index)
  }

  // Get discount rate for current topup amount
  const getDiscountRate = useCallback(() => {
    return topupInfo?.discount?.[topupAmount] || DEFAULT_DISCOUNT_RATE
  }, [topupInfo, topupAmount])

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Wallet')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            className='border-border bg-card hover:border-primary/40 h-10 gap-2 px-3 shadow-sm transition-[border-color,box-shadow,transform] duration-200 hover:-translate-y-0.5 hover:shadow-md motion-reduce:transition-none motion-reduce:hover:transform-none'
            onClick={() => setBillingDialogOpen(true)}
          >
            <Receipt aria-hidden='true' />
            {t('Order History')}
            <ArrowUpRight className='size-3.5' aria-hidden='true' />
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <motion.div
            initial={reduceMotion ? false : { opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={MOTION_TRANSITION.default}
            className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-7'
          >
            <WalletStatsCard user={user} loading={userLoading} />

            {showPlatformBenefits && <MembershipWalletSummary />}

            <div id='wallet-add-funds' className='scroll-mt-4'>
              <RechargeFormCard
                topupInfo={topupInfo}
                presetAmounts={presetAmounts}
                selectedPreset={selectedPreset}
                onSelectPreset={handleSelectPreset}
                topupAmount={topupAmount}
                onTopupAmountChange={handleTopupAmountChange}
                paymentAmount={paymentAmount}
                calculating={calculating}
                onPaymentMethodSelect={handlePaymentMethodSelect}
                selectedPaymentMethod={availableMethod}
                selectedWaffoMethodIndex={activeWaffoMethodIndex}
                onPaymentSubmit={handlePaymentSubmit}
                paymentQuoteValid={paymentQuoteValid}
                paymentLoading={null}
                redemptionCode={redemptionCode}
                onRedemptionCodeChange={setRedemptionCode}
                onRedeem={handleRedeem}
                redeeming={redeeming}
                topupLink={topupInfo?.topup_link}
                loading={topupLoading}
                creemProducts={topupInfo?.creem_products}
                enableCreemTopup={topupInfo?.enable_creem_topup}
                onCreemProductSelect={handleCreemProductSelect}
                enableWaffoTopup={topupInfo?.enable_waffo_topup}
                waffoPayMethods={topupInfo?.waffo_pay_methods}
                waffoMinTopup={topupInfo?.waffo_min_topup}
                onWaffoMethodSelect={handleWaffoMethodSelect}
                enableWaffoPancakeTopup={topupInfo?.enable_waffo_pancake_topup}
              />
            </div>

            {showPlatformBenefits && (
              <SubscriptionPlansCard
                topupInfo={topupInfo}
                userQuota={user?.quota}
                onPurchaseSuccess={fetchUser}
              />
            )}
          </motion.div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <PaymentConfirmDialog
        open={confirmDialogOpen}
        onOpenChange={setConfirmDialogOpen}
        onConfirm={handlePaymentConfirm}
        topupAmount={topupAmount}
        paymentAmount={paymentAmount}
        paymentMethod={availableMethod}
        calculating={calculating}
        processing={processing || waffoProcessing || pancakeProcessing}
        discountRate={
          availableMethod?.type === PAYMENT_TYPES.STRIPE ? 1 : getDiscountRate()
        }
      />

      <BillingHistoryDialog
        open={billingDialogOpen}
        onOpenChange={setBillingDialogOpen}
      />

      <CreemConfirmDialog
        open={creemDialogOpen}
        onOpenChange={setCreemDialogOpen}
        onConfirm={handleCreemConfirm}
        product={selectedCreemProduct}
        processing={creemProcessing}
      />
    </>
  )
}

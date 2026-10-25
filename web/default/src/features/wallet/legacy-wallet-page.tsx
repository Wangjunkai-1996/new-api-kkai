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
import { useState, useEffect, useCallback, useMemo, useRef } from 'react'

import { MembershipWalletSummary } from '@/features/membership/components/wallet-summary'
import { useSiteConfig } from '@/features/site-config'
import { MOTION_TRANSITION } from '@/lib/motion'

import { CreemConfirmDialog } from './components/dialogs/creem-confirm-dialog'
import { LegacyBillingHistoryDialog } from './components/dialogs/legacy-billing-history-dialog'
import { PaymentConfirmDialog } from './components/dialogs/payment-confirm-dialog'
import { RechargeFormCard } from './components/recharge-form-card'
import { SubscriptionPlansCard } from './components/subscription-plans-card'
import { DEFAULT_DISCOUNT_RATE, PAYMENT_TYPES } from './constants'
import {
  usePayment,
  useRedemption,
  useCreemPayment,
  useWaffoPayment,
  useWaffoPancakePayment,
} from './hooks'
import { getMinTopupAmount, dispatchSelectedPayment } from './lib'
import type {
  PaymentMethod,
  PresetAmount,
  CreemProduct,
  WaffoPayMethod,
  TopupInfo,
  WalletPageProps,
} from './types'

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

export function LegacyWalletPage(props: WalletPageProps) {
  const { data: site } = useSiteConfig()
  const showPlatformBenefits = site?.tenant_id === 0
  const user = props.user
  const fetchUser = props.onRefreshUser
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
  const [redemptionCode, setRedemptionCode] = useState('')
  const [creemDialogOpen, setCreemDialogOpen] = useState(false)
  const [selectedCreemProduct, setSelectedCreemProduct] =
    useState<CreemProduct | null>(null)

  const topupInfo = props.topupInfo
  const presetAmounts = props.presetAmounts
  const topupLoading = props.topupLoading
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
    if (!props.writesEnabled || !activePaymentType || topupAmount <= 0) return
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
  }, [
    activePaymentType,
    topupAmount,
    calculatePaymentAmount,
    props.writesEnabled,
  ])

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
    if (props.writesEnabled && paymentQuoteValid) setConfirmDialogOpen(true)
  }

  // Handle payment confirmation
  const handlePaymentConfirm = async () => {
    if (!props.writesEnabled || !availableMethod || !paymentQuoteValid) return
    props.onLegacyPaymentStarted?.()

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
    if (!props.writesEnabled || !redemptionCode) return

    const success = await redeemCode(redemptionCode)
    if (success) {
      setRedemptionCode('')
      await fetchUser()
    }
  }

  // Handle Creem product selection
  const handleCreemProductSelect = (product: CreemProduct) => {
    if (!props.writesEnabled) return
    setSelectedCreemProduct(product)
    setCreemDialogOpen(true)
  }

  // Handle Creem payment confirmation
  const handleCreemConfirm = async () => {
    if (!props.writesEnabled || !selectedCreemProduct) return
    props.onLegacyPaymentStarted?.()

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
      <motion.div
        initial={reduceMotion ? false : { opacity: 0, y: 10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={MOTION_TRANSITION.default}
        className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-7'
      >
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
            paymentQuoteValid={props.writesEnabled && paymentQuoteValid}
            purchaseDisabled={!props.writesEnabled}
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
            writesEnabled={props.writesEnabled}
            onPurchaseStarted={props.onLegacyPaymentStarted}
          />
        )}
      </motion.div>

      <PaymentConfirmDialog
        open={confirmDialogOpen && props.writesEnabled}
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

      <LegacyBillingHistoryDialog
        scope={props.historyScope ?? 'self'}
        open={props.historyOpen}
        onOpenChange={props.onHistoryOpenChange}
      />

      <CreemConfirmDialog
        open={creemDialogOpen && props.writesEnabled}
        onOpenChange={setCreemDialogOpen}
        onConfirm={handleCreemConfirm}
        product={selectedCreemProduct}
        processing={creemProcessing}
      />
    </>
  )
}

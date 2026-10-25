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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Receipt } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { MembershipWalletSummary } from '@/features/membership/components/wallet-summary'
import { useSiteConfig } from '@/features/site-config'
import { useDebounce } from '@/hooks/use-debounce'
import { getSelf } from '@/lib/api'

import {
  CurrentTopupOrder,
  PaymentOrderDetails,
} from './components/current-topup-order'
import { BillingHistoryDialog } from './components/dialogs/billing-history-dialog'
import { PaymentAmounts } from './components/payment-amounts'
import { RechargeFormCard } from './components/recharge-form-card'
import { SubscriptionPlansCard } from './components/subscription-plans-card'
import { WalletRequestError } from './components/wallet-request-error'
import { WalletStatsCard } from './components/wallet-stats-card'
import { PAYMENT_TYPES } from './constants'
import { useRedemption } from './hooks'
import {
  useWalletPaymentFlow,
  useWalletPaymentIdentity,
} from './hooks/use-wallet-payment-flow'
import { getMinTopupAmount } from './lib'
import {
  closePaymentPlaceholder,
  hasSamePaymentTerms,
  isLocallyEnded,
  PAYMENT_BLOCK_LABELS,
  PAYMENT_STATUS_LABELS,
  preparePaymentWindow,
} from './lib/payment-flow'
import { quoteWalletPayment } from './payment-flow-api'
import type { PaymentQuoteRequest } from './payment-flow-types'
import type {
  UserWalletData,
  PaymentMethod,
  CreemProduct,
  TopupInfo,
  WalletPageProps,
} from './types'

function paymentMinimum(method: PaymentMethod, info: TopupInfo): number {
  if (method.type === PAYMENT_TYPES.STRIPE) {
    return Math.max(1, method.min_topup ?? 0, info.stripe_min_topup ?? 0)
  }
  if (method.type === PAYMENT_TYPES.WAFFO_PANCAKE) {
    return Math.max(1, method.min_topup ?? 0, info.waffo_pancake_min_topup ?? 0)
  }
  if (method.type === PAYMENT_TYPES.WAFFO) {
    return Math.max(1, info.waffo_min_topup ?? 0)
  }
  return Math.max(1, method.min_topup ?? 0, info.min_topup ?? 0)
}
export function LifecycleWalletPage(props: WalletPageProps) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const { data: site } = useSiteConfig()
  const identity = useWalletPaymentIdentity()
  const flow = useWalletPaymentFlow(props.initialTradeNo)
  const topupInfo = props.topupInfo
  const presetAmounts = props.presetAmounts
  const topupLoading = props.topupLoading
  const userQuery = useQuery({
    queryKey: ['wallet-payment', ...identity, 'balance'],
    queryFn: async () => {
      const response = await getSelf()
      if (!response.success || !response.data) {
        throw new Error('Wallet balance unavailable')
      }
      return response.data as UserWalletData
    },
    retry: false,
    meta: { errorToast: false },
  })
  const paymentStatus = flow.wallet?.flow.order?.display_status
  const returnStatus = flow.returnedOrder?.display_status
  useEffect(() => {
    if (
      ![paymentStatus, returnStatus].some(
        (status) => status === 'success' || status === 'credit_review'
      )
    ) {
      return
    }
    for (const scope of ['balance', 'funding-summary', 'history']) {
      void client.invalidateQueries({
        queryKey: ['wallet-payment', ...identity, scope],
      })
    }
  }, [client, identity, paymentStatus, returnStatus])
  const user = userQuery.data ?? null
  const userLoading = userQuery.isPending
  const [amount, setAmount] = useState(0)
  const [selectedPreset, setSelectedPreset] = useState<number | null>(null)
  const [selectedMethod, setSelectedMethod] = useState<PaymentMethod>()
  const [waffoIndex, setWaffoIndex] = useState<number | null>(null)
  const [product, setProduct] = useState<CreemProduct | null>(null)
  const [historyOpen, setHistoryOpen] = useState(!!props.initialShowHistory)
  const [historyUnfinished, setHistoryUnfinished] = useState(false)
  const [redemptionCode, setRedemptionCode] = useState('')
  const { redeeming, redeemCode } = useRedemption()
  const initialized = useRef(false)
  const methods = useMemo(() => {
    if (!topupInfo) return []
    const list = topupInfo.pay_methods.filter((method) => {
      if (method.type === PAYMENT_TYPES.STRIPE) {
        return topupInfo.enable_stripe_topup
      }
      if (method.type === PAYMENT_TYPES.WAFFO_PANCAKE) {
        return topupInfo.enable_waffo_pancake_topup
      }
      return topupInfo.enable_online_topup
    })
    if (
      topupInfo.enable_stripe_topup &&
      !list.some((method) => method.type === PAYMENT_TYPES.STRIPE)
    ) {
      list.push({ name: 'Stripe', type: PAYMENT_TYPES.STRIPE })
    }
    if (
      topupInfo.enable_waffo_pancake_topup &&
      !list.some((method) => method.type === PAYMENT_TYPES.WAFFO_PANCAKE)
    ) {
      list.push({ name: 'Waffo Pancake', type: PAYMENT_TYPES.WAFFO_PANCAKE })
    }
    return list
  }, [topupInfo])
  useEffect(() => {
    if (!topupInfo || initialized.current) return
    initialized.current = true
    const minimums = methods.map((method) => paymentMinimum(method, topupInfo))
    if (topupInfo.enable_waffo_topup && topupInfo.waffo_pay_methods?.length) {
      minimums.push(Math.max(1, topupInfo.waffo_min_topup ?? 0))
    }
    const minimum = minimums.length
      ? Math.min(...minimums)
      : getMinTopupAmount(topupInfo)
    const preset = presetAmounts.find((item) => item.value >= minimum)
    setAmount(preset?.value ?? minimum)
    setSelectedPreset(preset?.value ?? null)
  }, [topupInfo, methods, presetAmounts])
  const availableMethod = useMemo(() => {
    if (!topupInfo) return undefined
    if (
      selectedMethod?.type === PAYMENT_TYPES.WAFFO &&
      topupInfo.enable_waffo_topup &&
      topupInfo.waffo_pay_methods?.[waffoIndex ?? 0] &&
      amount >= paymentMinimum(selectedMethod, topupInfo)
    ) {
      return selectedMethod
    }
    if (
      selectedMethod &&
      methods.some((method) => method.type === selectedMethod.type) &&
      amount >= paymentMinimum(selectedMethod, topupInfo)
    ) {
      return selectedMethod
    }
    const standard = methods.find(
      (method) => amount >= paymentMinimum(method, topupInfo)
    )
    if (standard) return standard
    const waffo = topupInfo.waffo_pay_methods?.[0]
    if (
      topupInfo.enable_waffo_topup &&
      waffo &&
      amount >= Math.max(1, topupInfo.waffo_min_topup ?? 0)
    ) {
      return { name: waffo.name, type: PAYMENT_TYPES.WAFFO, icon: waffo.icon }
    }
    return undefined
  }, [topupInfo, selectedMethod, waffoIndex, methods, amount])
  const activeWaffoIndex =
    availableMethod?.type === PAYMENT_TYPES.WAFFO ? (waffoIndex ?? 0) : null
  const showingReturned = !!(
    flow.returnedOrder ||
    flow.returnedLoading ||
    flow.returnedError
  )
  const idle =
    !showingReturned && flow.wallet?.flow.phase === 'idle' && !flow.creating
  const canBuy = idle && !flow.readError && flow.wallet?.can_create === true
  const quoteRequest = useMemo<PaymentQuoteRequest | null>(() => {
    if (product) {
      return {
        payment_provider: 'creem',
        payment_method: 'creem',
        product_id: product.productId,
      }
    }
    if (
      !availableMethod ||
      !topupInfo ||
      !Number.isSafeInteger(amount) ||
      amount < paymentMinimum(availableMethod, topupInfo)
    ) {
      return null
    }
    let provider = 'epay'
    if (['stripe', 'waffo', 'waffo_pancake'].includes(availableMethod.type)) {
      provider = availableMethod.type
    }
    const request: PaymentQuoteRequest = {
      payment_provider: provider,
      payment_method: availableMethod.type,
      amount: String(amount),
    }
    if (provider === 'waffo') {
      const method = topupInfo.waffo_pay_methods?.[activeWaffoIndex ?? 0]
      request.waffo_pay_method_type = method?.payMethodType
      request.waffo_pay_method_name = method?.payMethodName
    }
    return request
  }, [product, availableMethod, amount, topupInfo, activeWaffoIndex])
  const debouncedQuoteRequest = useDebounce(quoteRequest, 300)
  const quoteSettled = debouncedQuoteRequest === quoteRequest
  const quote = useQuery({
    queryKey: ['wallet-payment', ...identity, 'quote', debouncedQuoteRequest],
    queryFn: ({ signal }) => {
      if (!debouncedQuoteRequest) {
        throw new Error('Payment quote input unavailable')
      }
      return quoteWalletPayment(debouncedQuoteRequest, signal)
    },
    enabled: canBuy && !!debouncedQuoteRequest && quoteSettled,
    retry: false,
    staleTime: (query) =>
      Math.min(
        2_147_483_646,
        Math.max(
          0,
          (query.state.data?.expires_at ?? 0) * 1000 - query.state.dataUpdatedAt
        )
      ),
    meta: { errorToast: false },
    refetchOnWindowFocus: false,
  })
  const currentQuote = quoteSettled ? quote.data : undefined
  const [quoteChanged, setQuoteChanged] = useState(false)
  const [refreshingQuote, setRefreshingQuote] = useState(false)
  const quoteRefreshBusy = useRef(false)
  const activeQuoteRequest = useRef(quoteRequest)
  useEffect(() => {
    activeQuoteRequest.current = quoteRequest
  }, [quoteRequest])
  const submitPayment = async () => {
    if (
      !canBuy ||
      !currentQuote ||
      quote.isFetching ||
      quoteRefreshBusy.current
    ) {
      return
    }
    setQuoteChanged(false)
    if (currentQuote.expires_at > Date.now() / 1000) {
      flow.create(currentQuote)
      return
    }
    quoteRefreshBusy.current = true
    setRefreshingQuote(true)
    const target = preparePaymentWindow()
    const request = quoteRequest
    try {
      const result = await quote.refetch()
      if (
        result.error ||
        !result.data ||
        result.data.expires_at <= Date.now() / 1000 ||
        activeQuoteRequest.current !== request
      ) {
        closePaymentPlaceholder(target)
        return
      }
      if (!hasSamePaymentTerms(currentQuote, result.data)) {
        closePaymentPlaceholder(target)
        setQuoteChanged(true)
        return
      }
      flow.create(result.data, target)
    } finally {
      quoteRefreshBusy.current = false
      setRefreshingQuote(false)
    }
  }
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Wallet')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          onClick={() => {
            setHistoryUnfinished(false)
            setHistoryOpen(true)
          }}
        >
          <Receipt aria-hidden='true' />
          {t('Order History')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-5'>
          <WalletStatsCard
            user={user}
            loading={userLoading}
            fundingSummaryEnabled
          />
          {props.invalidPaymentReturn && (
            <div
              role='alert'
              className='bg-muted rounded-lg border p-4 text-sm'
            >
              {t(
                'We could not identify this payment return. Check Order History for the latest status.'
              )}
              <Button
                variant='link'
                onClick={() => {
                  setHistoryUnfinished(false)
                  setHistoryOpen(true)
                }}
              >
                {t('Order History')}
              </Button>
            </div>
          )}
          {!!flow.operationError && (
            <WalletRequestError
              error={flow.operationError}
              onRetry={flow.refresh}
              retryLabel={t('Refresh')}
              message={
                !flow.wallet?.flow.order
                  ? t(
                      'The request could not be confirmed. Refresh this order and try again.'
                    )
                  : undefined
              }
            />
          )}
          {userQuery.isError && (
            <WalletRequestError
              error={userQuery.error}
              onRetry={() => void userQuery.refetch()}
              message={t(
                'We could not refresh your wallet balance. The displayed balance may be out of date.'
              )}
            />
          )}
          {!!flow.readError && (
            <WalletRequestError
              error={flow.readError}
              onRetry={flow.refresh}
              message={
                !flow.wallet
                  ? t(
                      'We could not restore your wallet payment state. New payments are temporarily unavailable.'
                    )
                  : undefined
              }
            />
          )}
          {flow.returnedLoading && (
            <p role='status'>{t('Checking the returned payment order...')}</p>
          )}
          {!!flow.returnedError && (
            <WalletRequestError
              error={flow.returnedError}
              onRetry={flow.refreshReturned}
              message={t(
                'The request could not be confirmed. Refresh this order and try again.'
              )}
            />
          )}
          {flow.returnedOrder && (
            <section className='bg-card mx-auto w-full max-w-2xl space-y-5 rounded-xl border p-6'>
              <p className='text-muted-foreground text-sm'>
                {t('Returned payment order')}
              </p>
              <h2 className='text-2xl font-semibold'>
                {t(PAYMENT_STATUS_LABELS[flow.returnedOrder.display_status])}
              </h2>
              <PaymentOrderDetails
                order={flow.returnedOrder}
                serverTime={
                  flow.returnedServerTime ?? flow.wallet?.server_time ?? 0
                }
              />
              {flow.returnedOrder.display_status === 'success' && (
                <p>{t('Payment received and credits added to your wallet.')}</p>
              )}
              <div className='flex flex-wrap gap-3'>
                {!flow.wallet?.flow.order &&
                  !flow.readError &&
                  flow.returnedOrder.display_status === 'pending' &&
                  !isLocallyEnded(flow.returnedOrder) &&
                  flow.returnedOrder.allowed_actions.includes('resume') && (
                    <Button disabled={flow.busy} onClick={flow.resumeReturned}>
                      {t('Continue payment')}
                    </Button>
                  )}
                <Button variant='outline' onClick={flow.refreshReturned}>
                  {t('Refresh')}
                </Button>
              </div>
            </section>
          )}
          {showingReturned && (
            <Button variant='outline' onClick={flow.dismissReturned}>
              {flow.wallet?.flow.order
                ? t('Return to current order')
                : t('Back to wallet')}
            </Button>
          )}
          {flow.pollingPaused &&
            (['creating', 'order', 'processing', 'review'].includes(
              flow.wallet?.flow.phase ?? ''
            ) ||
              (flow.returnedOrder &&
                !['completed', 'closed'].includes(
                  flow.returnedOrder.status_group
                ))) && (
              <p role='status' className='text-muted-foreground text-sm'>
                {t(
                  'Automatic checking is paused. You can check again or view this order later in history.'
                )}
              </p>
            )}
          {flow.loading && (
            <div className='bg-card rounded-lg border p-6' role='status'>
              {t('Restoring your top-up order...')}
            </div>
          )}
          {!showingReturned && flow.wallet?.flow.order && (
            <CurrentTopupOrder
              key={flow.wallet.flow.id}
              wallet={flow.wallet}
              busy={flow.busy}
              notice={flow.notice}
              onResume={flow.resume}
              onEnd={() => {
                if (flow.wallet?.flow.order) {
                  flow.act(flow.wallet.flow.order, 'end')
                }
              }}
              onReconcile={() => {
                if (flow.wallet?.flow.order) {
                  flow.act(flow.wallet.flow.order, 'reconcile')
                }
              }}
              onAcknowledge={flow.acknowledge}
              onHistory={() => {
                setHistoryUnfinished(false)
                setHistoryOpen(true)
              }}
              onUnfinishedHistory={() => {
                setHistoryUnfinished(true)
                setHistoryOpen(true)
              }}
            />
          )}
          {flow.creating && !flow.wallet?.flow.order && (
            <section
              className='bg-card mx-auto w-full max-w-2xl space-y-4 rounded-lg border p-6'
              aria-busy={flow.busy}
              aria-live='polite'
            >
              <h2 className='text-xl font-semibold'>{t('Creating payment')}</h2>
              <p>
                {t(
                  'We are verifying this order. Please do not pay again or create another order.'
                )}
              </p>
              {!flow.busy && (
                <Button onClick={flow.retryCreate}>
                  {t('Retry original request')}
                </Button>
              )}
            </section>
          )}
          {idle && (
            <>
              {site?.tenant_id === 0 && <MembershipWalletSummary />}
              {!canBuy && (
                <p
                  className='bg-muted rounded-lg border p-4 text-sm'
                  role='status'
                >
                  {t(
                    PAYMENT_BLOCK_LABELS[
                      flow.wallet?.create_block_reason_code ?? ''
                    ] ?? 'Online top-up is currently unavailable.'
                  )}{' '}
                  <Button
                    variant='link'
                    disabled={flow.busy}
                    onClick={
                      flow.recoverable ? flow.retryRecovery : flow.refresh
                    }
                  >
                    {flow.recoverable
                      ? t('Restore unfinished order')
                      : t('Refresh')}
                  </Button>
                </p>
              )}
              {flow.notice && (
                <p role='alert'>
                  {t(
                    'The request could not be confirmed. Refresh this order and try again.'
                  )}
                </p>
              )}
              {quote.isError && quoteSettled && canBuy && (
                <WalletRequestError
                  error={quote.error}
                  onRetry={() => void quote.refetch()}
                  message={t(
                    'We could not obtain a valid payment quote. Please try again.'
                  )}
                />
              )}
              {quoteChanged && (
                <p role='status'>
                  {t(
                    'The payment quote has changed. Review the updated amounts before paying.'
                  )}
                </p>
              )}
              <RechargeFormCard
                topupInfo={topupInfo}
                presetAmounts={presetAmounts}
                selectedPreset={selectedPreset}
                onSelectPreset={(preset) => {
                  setProduct(null)
                  setAmount(preset.value)
                  setSelectedPreset(preset.value)
                }}
                topupAmount={amount}
                onTopupAmountChange={(value) => {
                  setProduct(null)
                  setAmount(value)
                  setSelectedPreset(null)
                }}
                paymentAmount={currentQuote ? 1 : 0}
                calculating={
                  !quoteSettled || quote.isFetching || refreshingQuote
                }
                paymentQuoteValid={canBuy && !!currentQuote && !product}
                selectedPaymentMethod={availableMethod}
                selectedWaffoMethodIndex={activeWaffoIndex}
                onPaymentMethodSelect={(method) => {
                  setProduct(null)
                  setSelectedMethod(method)
                  setWaffoIndex(null)
                }}
                onPaymentSubmit={submitPayment}
                paymentLoading={null}
                redemptionCode={redemptionCode}
                onRedemptionCodeChange={setRedemptionCode}
                onRedeem={async () => {
                  if (await redeemCode(redemptionCode)) {
                    setRedemptionCode('')
                    await userQuery.refetch()
                  }
                }}
                redeeming={redeeming}
                topupLink={topupInfo?.topup_link}
                loading={topupLoading}
                creemProducts={topupInfo?.creem_products}
                enableCreemTopup={topupInfo?.enable_creem_topup}
                onCreemProductSelect={(value) => {
                  if (canBuy) {
                    setProduct(value)
                  }
                }}
                enableWaffoTopup={topupInfo?.enable_waffo_topup}
                waffoPayMethods={topupInfo?.waffo_pay_methods}
                waffoMinTopup={topupInfo?.waffo_min_topup}
                onWaffoMethodSelect={(_, index) => {
                  setProduct(null)
                  setSelectedMethod({
                    name:
                      topupInfo?.waffo_pay_methods?.[index]?.name ?? 'Waffo',
                    type: PAYMENT_TYPES.WAFFO,
                  })
                  setWaffoIndex(index)
                }}
                enableWaffoPancakeTopup={topupInfo?.enable_waffo_pancake_topup}
                quote={!product ? currentQuote : undefined}
                purchaseDisabled={!canBuy}
              />
              {product && (
                <section className='bg-card space-y-4 rounded-lg border p-5'>
                  <h3 className='font-semibold'>{product.name}</h3>
                  {currentQuote && <PaymentAmounts pricing={currentQuote} />}
                  <p className='text-muted-foreground text-sm'>
                    {t(
                      'Payment opens in a new tab. Your order remains available in this wallet.'
                    )}
                  </p>
                  <div className='flex gap-2'>
                    <Button
                      disabled={
                        !canBuy ||
                        !currentQuote ||
                        quote.isFetching ||
                        refreshingQuote
                      }
                      onClick={submitPayment}
                    >
                      {t('Review and Pay')}
                    </Button>
                    <Button variant='outline' onClick={() => setProduct(null)}>
                      {t('Back to edit')}
                    </Button>
                  </div>
                </section>
              )}
              {site?.tenant_id === 0 && (
                <SubscriptionPlansCard
                  topupInfo={topupInfo}
                  userQuota={user?.quota}
                  onPurchaseSuccess={() => {
                    void userQuery.refetch()
                  }}
                />
              )}
            </>
          )}
        </div>
        <BillingHistoryDialog
          key={`${props.historyScope ?? 'self'}:${historyUnfinished}`}
          unfinished={historyUnfinished}
          open={historyOpen}
          onOpenChange={setHistoryOpen}
          scope={props.historyScope ?? 'self'}
          onWalletChange={flow.apply}
          onOrderAction={async (result, action) => {
            if (await flow.acceptOrderAction(result, action)) {
              setHistoryOpen(false)
            }
          }}
          onRestore={(tradeNo) => {
            setHistoryOpen(false)
            flow.restore(tradeNo)
          }}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

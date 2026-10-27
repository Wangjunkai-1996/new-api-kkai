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
import { useQuery } from '@tanstack/react-query'
import axios from 'axios'
import { History } from 'lucide-react'
import { lazy, Suspense, useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Main } from '@/components/layout'
import { Button } from '@/components/ui/button'

import '@/styles/wallet.css'

import { getWalletBalance } from './api'
import { WalletRequestError } from './components/wallet-request-error'
import { WalletStatsCard } from './components/wallet-stats-card'
import { useTopupInfo } from './hooks/use-topup-info'
import { useWalletPaymentIdentity } from './hooks/use-wallet-payment-flow'
import { walletRequestRetryAt } from './payment-flow-api'
import type { WalletPageProps } from './types'

let legacyModule:
  | Promise<{ default: typeof import('./legacy-wallet-page').LegacyWalletPage }>
  | undefined
let lifecycleModule:
  | Promise<{
      default: typeof import('./lifecycle-wallet-page').LifecycleWalletPage
    }>
  | undefined
const loadLegacy = () =>
  (legacyModule ??= import('./legacy-wallet-page').then((module) => ({
    default: module.LegacyWalletPage,
  })))
const loadLifecycle = () =>
  (lifecycleModule ??= import('./lifecycle-wallet-page').then((module) => ({
    default: module.LifecycleWalletPage,
  })))
const LegacyWalletPage = lazy(loadLegacy)
const LifecycleWalletPage = lazy(loadLifecycle)
type WalletProps = Pick<
  WalletPageProps,
  | 'initialShowHistory'
  | 'initialTradeNo'
  | 'invalidPaymentReturn'
  | 'historyScope'
>
type WalletMode = 'legacy' | 'lifecycle'

function canRefreshWalletBalance(query: {
  state: { error: unknown; errorUpdatedAt: number }
}): boolean {
  const error = query.state.error
  if (!axios.isAxiosError(error) || error.response?.status !== 429) return true
  const retryAt = walletRequestRetryAt(error, query.state.errorUpdatedAt)
  return retryAt !== undefined && Date.now() >= retryAt
}

export function Wallet(props: WalletProps) {
  const identity = useWalletPaymentIdentity()
  return <WalletPage key={JSON.stringify(identity)} {...props} />
}

function WalletPage(props: WalletProps) {
  const { t } = useTranslation()
  const identity = useWalletPaymentIdentity()
  const topup = useTopupInfo()
  const balance = useQuery({
    queryKey: ['wallet-payment', ...identity, 'balance'],
    enabled: topup.identityReady,
    queryFn: async ({ signal }) => {
      const response = await getWalletBalance(signal)
      if (!response.success || !response.data) {
        throw new Error('Wallet balance unavailable')
      }
      return response.data
    },
    retryOnMount: false,
    refetchOnMount: (query) =>
      canRefreshWalletBalance(query) ? 'always' : false,
    refetchOnWindowFocus: canRefreshWalletBalance,
    refetchOnReconnect: canRefreshWalletBalance,
    networkMode: 'always',
    retry: false,
    meta: { errorToast: false },
  })
  const [mode, setMode] = useState<WalletMode | null>(null)
  const [legacyPaymentStarted, setLegacyPaymentStarted] = useState(false)
  const [history, setHistory] = useState({
    open: !!props.initialShowHistory,
    unfinished: false,
  })
  // A confirmed lifecycle page keeps its payment attempt across config changes.
  let nextMode = mode
  if (mode !== 'lifecycle' && !legacyPaymentStarted) {
    if (topup.confirmedLifecycle) nextMode = 'lifecycle'
    else if (topup.capability === 'legacy' && topup.fresh) nextMode = 'legacy'
  }
  if (nextMode !== mode) setMode(nextMode)

  useEffect(() => {
    if (topup.capability === 'legacy' && !topup.confirmedLifecycle) {
      void loadLegacy().catch(() => undefined)
    } else {
      void loadLifecycle().catch(() => undefined)
    }
  }, [topup.capability, topup.confirmedLifecycle])
  const refreshBalance = balance.refetch
  const refreshUser = useCallback(async () => {
    await refreshBalance()
  }, [refreshBalance])
  const openHistory = useCallback((open: boolean, unfinished = false) => {
    setHistory({ open, unfinished })
  }, [])
  const writesEnabled =
    mode === 'lifecycle'
      ? topup.capability === 'lifecycle'
      : mode === 'legacy' && topup.capability === 'legacy' && topup.fresh
  const capabilityMismatch =
    topup.capability === 'invalid' ||
    (mode === 'lifecycle' && topup.capability !== 'lifecycle') ||
    (mode === 'legacy' && topup.capability === 'lifecycle')
  const pageProps: WalletPageProps = {
    ...props,
    topupInfo: topup.topupInfo,
    presetAmounts: topup.presetAmounts,
    topupLoading: topup.loading,
    user: balance.data ?? null,
    onRefreshUser: refreshUser,
    historyOpen: history.open,
    historyUnfinished: history.unfinished,
    onHistoryOpenChange: openHistory,
    writesEnabled,
    onLegacyPaymentStarted: () => setLegacyPaymentStarted(true),
  }
  const loading = (
    <div className='bg-card rounded-lg border p-6' role='status'>
      {t('Loading wallet payment options...')}
    </div>
  )
  return (
    <Main className='wallet-page'>
      <div className='min-h-0 flex-1 overflow-auto'>
        <div className='wallet-content mx-auto flex w-full max-w-[1760px] flex-col gap-5'>
          <header className='flex flex-wrap items-center justify-between gap-x-4 gap-y-3'>
            <div className='min-w-40 flex-1'>
              <h1 className='text-2xl font-semibold tracking-tight sm:text-[26px]'>
                {t('Wallet')}
              </h1>
              <p className='text-muted-foreground mt-1 text-xs'>
                {t('Manage your credits for every API call.')}
              </p>
            </div>
            <Button
              variant='outline'
              className='ms-auto h-10 shrink-0 gap-2 rounded-[9px] px-3'
              onClick={() => openHistory(true)}
            >
              <History aria-hidden='true' />
              {t('Order History')}
            </Button>
          </header>
          <WalletStatsCard
            user={balance.data ?? null}
            loading={balance.isPending}
            balanceRefreshing={balance.isFetching}
            balanceError={balance.error}
            balanceErrorUpdatedAt={balance.errorUpdatedAt}
            balanceUpdatedAt={balance.dataUpdatedAt}
            onBalanceRetry={() => void balance.refetch()}
            fundingSummaryEnabled={mode === 'lifecycle'}
          />
          {capabilityMismatch && (
            <WalletRequestError
              error={undefined}
              onRetry={() => void topup.refetch()}
              message={t(
                'Payment availability changed. Your current order is preserved, and payment actions are temporarily paused.'
              )}
            />
          )}
          {!!topup.error && (
            <WalletRequestError
              error={topup.error}
              errorUpdatedAt={topup.errorUpdatedAt}
              onRetry={() => void topup.refetch()}
              message={
                mode === 'lifecycle'
                  ? t(
                      'Payment options could not be refreshed. The displayed options may be out of date.'
                    )
                  : t(
                      'Payment options are unavailable. New payments are temporarily unavailable.'
                    )
              }
            />
          )}
          {!mode && history.open && (
            <div className='bg-card rounded-lg border p-4' role='status'>
              {t(
                'Order history will load after payment availability is confirmed.'
              )}
            </div>
          )}
          <Suspense fallback={loading}>
            {mode === 'lifecycle' && <LifecycleWalletPage {...pageProps} />}
            {mode === 'legacy' && <LegacyWalletPage {...pageProps} />}
            {!mode && !topup.error && !capabilityMismatch && loading}
          </Suspense>
        </div>
      </div>
    </Main>
  )
}

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
import { lazy, Suspense, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'

import { useTopupInfo } from './hooks/use-topup-info'
import { useWalletPaymentIdentity } from './hooks/use-wallet-payment-flow'
import type { WalletPageProps } from './types'

const LegacyWalletPage = lazy(() =>
  import('./legacy-wallet-page').then((module) => ({
    default: module.LegacyWalletPage,
  }))
)
const LifecycleWalletPage = lazy(() =>
  import('./lifecycle-wallet-page').then((module) => ({
    default: module.LifecycleWalletPage,
  }))
)
type WalletProps = Omit<
  WalletPageProps,
  'topupInfo' | 'presetAmounts' | 'topupLoading'
>

export function Wallet(props: WalletProps) {
  const identity = useWalletPaymentIdentity()
  return <WalletPage key={identity.join(':')} {...props} />
}

function WalletPage(props: WalletProps) {
  const { t } = useTranslation()
  const topup = useTopupInfo()
  const ready = topup.topupInfo?.wallet_lifecycle_ready
  const refetch = topup.refetch
  useEffect(() => {
    const refresh = () => {
      if (!document.hidden) void refetch(true)
    }
    window.addEventListener('focus', refresh)
    const interval =
      ready === true ? undefined : window.setInterval(refresh, 30_000)
    return () => {
      window.removeEventListener('focus', refresh)
      window.clearInterval(interval)
    }
  }, [ready, refetch])

  const loading = (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Wallet')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='bg-card rounded-lg border p-6' role='status'>
          {t('Restoring your top-up order...')}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
  if (topup.loading && !topup.topupInfo) return loading
  if (!topup.topupInfo || (ready !== undefined && typeof ready !== 'boolean')) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Wallet')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='bg-card space-y-4 rounded-lg border p-6' role='alert'>
            <p>
              {t(
                'We could not restore your wallet payment state. New payments are temporarily unavailable.'
              )}
            </p>
            <Button variant='outline' onClick={() => void refetch()}>
              {t('Retry')}
            </Button>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }
  const pageProps: WalletPageProps = {
    ...props,
    topupInfo: topup.topupInfo,
    presetAmounts: topup.presetAmounts,
    topupLoading: topup.loading,
  }
  return (
    <Suspense fallback={loading}>
      {ready === true ? (
        <LifecycleWalletPage {...pageProps} />
      ) : (
        <LegacyWalletPage {...pageProps} />
      )}
    </Suspense>
  )
}

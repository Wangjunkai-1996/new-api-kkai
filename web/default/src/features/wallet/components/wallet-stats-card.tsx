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
import { CircleHelp } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuota } from '@/lib/format'

import { useWalletFundingSummary } from '../hooks/use-wallet-funding-summary'
import type { UserWalletData } from '../types'
import { WalletCashAmounts } from './wallet-cash-amounts'
import { WalletRequestError } from './wallet-request-error'

interface WalletStatsCardProps {
  user: UserWalletData | null
  loading?: boolean
  balanceRefreshing?: boolean
  balanceError?: unknown
  balanceErrorUpdatedAt?: number
  balanceUpdatedAt?: number
  onBalanceRetry?: () => void
  fundingSummaryEnabled?: boolean
}

export function WalletStatsCard(props: WalletStatsCardProps) {
  const { t, i18n } = useTranslation()
  const funding = useWalletFundingSummary(props.fundingSummaryEnabled === true)
  const summary = props.fundingSummaryEnabled ? funding.data : undefined
  const balanceLoading = props.loading && !props.user
  const summaryLoading =
    props.fundingSummaryEnabled && funding.isPending && !summary
  const balanceValue = props.user
    ? formatQuota(props.user.quota)
    : t('Not available')
  const consumedValue = props.user
    ? formatQuota(props.user.used_quota)
    : t('Not available')
  const requestCount = props.user
    ? props.user.request_count.toLocaleString()
    : t('Not available')

  return (
    <section
      aria-label={t('Wallet amount statistics')}
      className='wallet-overview wallet-enter bg-card rounded-2xl border'
    >
      <section
        aria-label={t('Current Balance')}
        aria-busy={balanceLoading || props.balanceRefreshing}
        className='wallet-balance min-w-0'
      >
        <div className='text-muted-foreground flex items-center gap-2 text-xs'>
          <span
            className='bg-success size-1.5 shrink-0 rounded-full'
            aria-hidden='true'
          />
          {t('Current Balance')}
        </div>
        <div className='wallet-balance-value mt-2.5 leading-tight font-semibold tracking-[-0.045em] break-all tabular-nums'>
          {balanceLoading ? (
            <Skeleton
              aria-hidden='true'
              className='h-9 w-64 max-w-full motion-reduce:animate-none'
            />
          ) : (
            balanceValue
          )}
        </div>
        <p className='text-muted-foreground mt-2 text-xs'>
          {t('Includes top-ups, rebates and gifts')}
        </p>
        {(balanceLoading || props.balanceRefreshing) && (
          <p role='status' className='text-muted-foreground mt-2 text-xs'>
            {balanceLoading
              ? t('Loading wallet balance...')
              : t('Updating wallet balance...')}
          </p>
        )}
        {props.user && !!props.balanceUpdatedAt && (
          <p className='text-muted-foreground mt-2 text-xs'>
            {t('Last updated:')}{' '}
            <time dateTime={new Date(props.balanceUpdatedAt).toISOString()}>
              {new Date(props.balanceUpdatedAt).toLocaleTimeString(
                toIntlLocale(i18n.language)
              )}
            </time>
          </p>
        )}
        {!!props.balanceError && (
          <WalletRequestError
            error={props.balanceError}
            errorUpdatedAt={props.balanceErrorUpdatedAt}
            onRetry={props.onBalanceRetry}
            retryDisabled={props.balanceRefreshing}
            retryLabel={t('Retry balance')}
            message={
              props.user
                ? t(
                    'We could not refresh your wallet balance. The displayed balance may be out of date.'
                  )
                : t('We could not load your wallet balance. Try again.')
            }
          />
        )}
      </section>
      <section
        aria-label={t('Total paid top-ups')}
        aria-busy={props.fundingSummaryEnabled && funding.isFetching}
        className='wallet-metric min-w-0'
      >
        <div className='text-muted-foreground mb-1.5 flex items-center gap-1.5 text-xs'>
          <span>{t('Total paid top-ups')}</span>
          <Tooltip>
            <TooltipTrigger
              aria-label={t('Payment total details')}
              className='inline-flex size-6 shrink-0 items-center justify-center sm:size-4'
            >
              <CircleHelp className='size-3.5' aria-hidden='true' />
            </TooltipTrigger>
            <TooltipContent className='max-w-72' align='start'>
              <div className='space-y-1'>
                <p>
                  {t(
                    'Only confirmed top-up payments are counted, in their original currencies. Refunds have not been deducted.'
                  )}
                </p>
                {summary && summary.unknown_payment_count > 0 && (
                  <p>
                    {t(
                      '{{count}} historical orders have unverified payments and are not included.',
                      { count: summary.unknown_payment_count }
                    )}
                  </p>
                )}
              </div>
            </TooltipContent>
          </Tooltip>
        </div>
        {summaryLoading ? (
          <Skeleton
            aria-hidden='true'
            className='h-7 w-32 max-w-full motion-reduce:animate-none'
          />
        ) : (
          <WalletCashAmounts
            payments={summary?.cash_payments}
            className='text-lg font-semibold'
          />
        )}
        <p className='text-muted-foreground mt-1.5 text-[11px]'>
          {t('Excludes rebates and gifts · Before refunds')}
        </p>
        {props.fundingSummaryEnabled && funding.isFetching && (
          <p role='status' className='text-muted-foreground mt-2 text-xs'>
            {summaryLoading
              ? t('Loading payment totals...')
              : t('Updating payment totals...')}
          </p>
        )}
        {summary && !!funding.dataUpdatedAt && (
          <p className='text-muted-foreground mt-2 text-xs'>
            {t('Last updated:')}{' '}
            <time dateTime={new Date(funding.dataUpdatedAt).toISOString()}>
              {new Date(funding.dataUpdatedAt).toLocaleTimeString(
                toIntlLocale(i18n.language)
              )}
            </time>
          </p>
        )}
        {props.fundingSummaryEnabled && funding.isError && (
          <WalletRequestError
            error={funding.error}
            errorUpdatedAt={funding.errorUpdatedAt}
            onRetry={() => void funding.refetch()}
            retryDisabled={funding.isFetching}
            retryLabel={t('Retry payment totals')}
            message={
              summary
                ? t(
                    'We could not refresh your payment totals. The displayed totals may be out of date.'
                  )
                : t('We could not load your payment totals. Try again.')
            }
          />
        )}
      </section>
      <div className='wallet-metric min-w-0'>
        <span className='text-muted-foreground text-xs'>
          {t('Total consumed credits')}
        </span>
        <div className='mt-2 text-lg font-semibold break-all tabular-nums'>
          {balanceLoading ? (
            <Skeleton
              aria-hidden='true'
              className='h-6 w-28 max-w-full motion-reduce:animate-none'
            />
          ) : (
            consumedValue
          )}
        </div>
        <p className='text-muted-foreground mt-1.5 text-[11px]'>
          {t('Deducted for actual usage')}
        </p>
      </div>
      <div className='wallet-metric wallet-request-count min-w-0'>
        <span className='text-muted-foreground text-xs'>
          {t('API Requests')}
        </span>
        <div className='mt-2 text-lg font-semibold break-all tabular-nums'>
          {balanceLoading ? (
            <Skeleton
              aria-hidden='true'
              className='h-6 w-20 max-w-full motion-reduce:animate-none'
            />
          ) : (
            requestCount
          )}
        </div>
        <p className='text-muted-foreground mt-1.5 text-[11px]'>
          {t('Total API calls')}
        </p>
      </div>
    </section>
  )
}

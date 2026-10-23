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
import { formatQuota } from '@/lib/format'

import { useWalletFundingSummary } from '../hooks/use-wallet-funding-summary'
import type { UserWalletData } from '../types'
import { WalletCashAmounts } from './wallet-cash-amounts'

interface WalletStatsCardProps {
  user: UserWalletData | null
  loading?: boolean
  fundingSummaryEnabled?: boolean
}

export function WalletStatsCard(props: WalletStatsCardProps) {
  const { t } = useTranslation()
  const funding = useWalletFundingSummary(props.fundingSummaryEnabled === true)
  const summary =
    props.fundingSummaryEnabled && !funding.isError ? funding.data : undefined

  if (props.loading) {
    return (
      <div className='bg-muted/25 grid grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)] items-center gap-x-3 gap-y-4 border-y px-3 py-4 sm:gap-5 sm:px-5 sm:py-5 md:grid-cols-[minmax(0,1.1fr)_minmax(0,1.43fr)_minmax(0,0.85fr)_minmax(0,0.72fr)]'>
        <div className='min-w-0'>
          <Skeleton className='h-4 w-24' />
          <Skeleton className='mt-2 h-9 w-64 max-w-full' />
          <Skeleton className='mt-2 h-3 w-28 max-w-full' />
        </div>
        {['payments', 'usage', 'requests'].map((key) => (
          <div
            key={key}
            className='min-w-0 border-l pl-3 sm:pl-5 [&:nth-child(3)]:border-0 [&:nth-child(3)]:pl-0 md:[&:nth-child(3)]:border-l md:[&:nth-child(3)]:pl-5'
          >
            <Skeleton className='h-3 w-20 max-w-full' />
            <Skeleton className='mt-2 h-5 w-28 max-w-full' />
            <Skeleton className='mt-2 h-3 w-24 max-w-full' />
          </div>
        ))}
      </div>
    )
  }

  return (
    <section
      aria-label={t('Wallet amount statistics')}
      className='bg-muted/25 grid grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)] items-center gap-x-3 gap-y-4 border-y px-3 py-4 sm:gap-5 sm:px-5 sm:py-5 md:grid-cols-[minmax(0,1.1fr)_minmax(0,1.43fr)_minmax(0,0.85fr)_minmax(0,0.72fr)]'
    >
      <div className='min-w-0'>
        <div className='text-muted-foreground flex items-center gap-2 text-xs'>
          <span
            className='bg-success size-1.5 shrink-0 rounded-full'
            aria-hidden='true'
          />
          {t('Current Balance')}
        </div>
        <div className='mt-2 text-[29px] leading-tight font-semibold break-all tabular-nums sm:text-[34px]'>
          {props.user ? formatQuota(props.user.quota) : t('Not available')}
        </div>
        <p className='text-muted-foreground mt-1.5 text-[11px]'>
          {t('Includes top-ups, rebates and gifts')}
        </p>
      </div>
      <div className='min-w-0 border-l pl-3 sm:pl-5'>
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
        <WalletCashAmounts
          payments={summary?.cash_payments}
          loading={props.fundingSummaryEnabled && funding.isPending}
          className='text-[19px] sm:text-[21px]'
        />
        <p className='text-muted-foreground mt-1.5 text-[11px]'>
          {t('Excludes rebates and gifts · Before refunds')}
        </p>
      </div>
      <div className='min-w-0 md:border-l md:pl-5'>
        <span className='text-muted-foreground text-xs'>
          {t('Total consumed credits')}
        </span>
        <strong className='mt-1.5 block text-[17px] font-medium break-all tabular-nums'>
          {props.user ? formatQuota(props.user.used_quota) : t('Not available')}
        </strong>
        <p className='text-muted-foreground mt-1.5 text-[11px]'>
          {t('Deducted for actual usage')}
        </p>
      </div>
      <div className='min-w-0 border-l pl-3 sm:pl-5'>
        <span className='text-muted-foreground text-xs'>
          {t('API Requests')}
        </span>
        <strong className='mt-1.5 block text-[17px] font-medium break-all tabular-nums'>
          {props.user
            ? props.user.request_count.toLocaleString()
            : t('Not available')}
        </strong>
        <p className='text-muted-foreground mt-1.5 text-[11px]'>
          {t('Total API calls')}
        </p>
      </div>
    </section>
  )
}

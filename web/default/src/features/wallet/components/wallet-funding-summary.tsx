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

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

import { useWalletFundingSummary } from '../hooks/use-wallet-funding-summary'
import { formatWalletCash } from '../lib/funding-summary'
import { WalletCashAmounts } from './wallet-cash-amounts'

export function WalletFundingSummary(props: { enabled: boolean }) {
  const { t, i18n } = useTranslation()
  const funding = useWalletFundingSummary(props.enabled)
  const summary = funding.isError ? undefined : funding.data
  let rebate = ''
  if (summary) {
    const credit = summary.rebate_credit_display
    rebate = `${credit.amount} ${t('Tokens')}`
    if (credit.unit === 'USD') {
      const [integer, fraction = ''] = credit.amount.split('.')
      const minorFraction = fraction.padEnd(2, '0')
      rebate = formatWalletCash(
        {
          amount_minor: `${integer}${minorFraction}`,
          currency: 'USD',
          exponent: minorFraction.length,
        },
        i18n.language
      )
    }
  }
  return (
    <section
      className='bg-muted/25 space-y-1.5 border-y px-4 py-3.5'
      aria-label={t('My total payments · All time')}
      aria-busy={funding.isPending}
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <span className='text-muted-foreground text-xs'>
          {t('My total payments · All time')}
        </span>
        <WalletCashAmounts
          payments={summary?.cash_payments}
          loading={funding.isPending}
        />
      </div>
      {funding.isPending && <Skeleton className='h-3 w-72 max-w-full' />}
      {funding.isError && (
        <p className='text-muted-foreground text-xs' role='status'>
          {t('Payment totals unavailable')}{' '}
          <Button
            variant='link'
            size='sm'
            className='h-auto p-0 text-xs'
            onClick={() => void funding.refetch()}
          >
            {t('Retry')}
          </Button>
        </p>
      )}
      {summary && (
        <>
          <p className='text-muted-foreground text-[11px]'>
            {t(
              '{{count}} confirmed payments · Original currencies, excluding rebates, gifts and redemption codes. Before refunds.',
              { count: summary.confirmed_payment_count }
            )}
          </p>
          <p className='text-muted-foreground text-[11px]'>
            {summary.unknown_payment_count > 0 && (
              <>
                {t(
                  '{{count}} historical orders have unverified payments and are not included.',
                  { count: summary.unknown_payment_count }
                )}{' '}
              </>
            )}
            {t('Verified net rebates credited: {{amount}} credits.', {
              amount: rebate,
            })}
          </p>
        </>
      )}
    </section>
  )
}

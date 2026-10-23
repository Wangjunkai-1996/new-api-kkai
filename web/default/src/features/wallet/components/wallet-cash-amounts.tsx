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

import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import { formatWalletCash } from '../lib/funding-summary'
import type { MoneyDTO } from '../payment-flow-types'

export function WalletCashAmounts(props: {
  payments?: MoneyDTO[]
  loading?: boolean
  className?: string
}) {
  const { t, i18n } = useTranslation()
  if (props.loading) return <Skeleton className='h-7 w-32 max-w-full' />
  if (!props.payments) {
    return (
      <span className='text-muted-foreground text-sm'>
        {t('Not available')}
      </span>
    )
  }
  if (!props.payments.length) {
    return (
      <span className='text-muted-foreground text-sm'>
        {t('No confirmed payments')}
      </span>
    )
  }
  return (
    <div
      className={cn(
        'flex flex-wrap items-baseline gap-x-3 gap-y-1 text-xl font-semibold tracking-tight tabular-nums',
        props.className
      )}
    >
      {props.payments.map((payment, index) => {
        return (
          <span
            key={`${payment.currency}:${payment.exponent}`}
            className={cn(
              'break-all',
              index > 0 && 'text-[13px] font-medium tracking-normal'
            )}
          >
            {formatWalletCash(payment, i18n.language)}
          </span>
        )
      })}
    </div>
  )
}

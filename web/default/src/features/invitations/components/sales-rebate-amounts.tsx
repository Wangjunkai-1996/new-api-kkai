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

import { formatSalesAmount } from '../format'
import type { SalesRebates } from '../types'

export const SalesRebateAmounts = (props: {
  rebates: SalesRebates
  currency: string
  compact?: boolean
}) => {
  const { t } = useTranslation()
  const amounts = [
    [t('Cooling Down'), props.rebates.cooling],
    [t('Available to Request'), props.rebates.available],
    [t('Awaiting Review'), props.rebates.pendingApproval],
    [t('Approved, Awaiting Issue'), props.rebates.approved],
    [t('Issue Confirmed'), props.rebates.issued],
    [t('Locked Rebate'), props.rebates.locked],
    [t('Closed Rebate'), props.rebates.closed],
  ] as const
  return (
    <div className='space-y-2'>
      <dl
        className={
          props.compact
            ? 'grid grid-cols-2 gap-x-4 gap-y-2 text-xs'
            : 'grid grid-cols-2 gap-4 border-t pt-4 sm:grid-cols-3 lg:grid-cols-5'
        }
      >
        {amounts.map(([label, amount]) => (
          <div key={label}>
            <dt className='text-muted-foreground'>{label}</dt>
            <dd className='font-semibold tabular-nums'>
              {formatSalesAmount(amount, props.currency)}
            </dd>
          </div>
        ))}
      </dl>
      {props.rebates.pendingCalculationCount > 0 && (
        <p className='text-muted-foreground text-xs'>
          {t('{{count}} recharges awaiting rebate calculation', {
            count: props.rebates.pendingCalculationCount,
          })}
        </p>
      )}
    </div>
  )
}

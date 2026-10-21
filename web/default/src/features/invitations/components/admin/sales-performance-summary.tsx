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

import { formatSalesAmount } from '../../format'
import type { SalesCreditCurrency, SalesPerformance } from '../../types'
import { SalesRebateAmounts } from '../sales-rebate-amounts'

export const SalesPerformanceSummary = (props: {
  summary: SalesPerformance
  currency?: SalesCreditCurrency
}) => {
  const { t } = useTranslation()
  return (
    <section
      aria-label={t('Sales Performance')}
      className='space-y-4 rounded-lg border p-4'
    >
      <h3 className='font-semibold'>{t('Sales Performance')}</h3>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Lifetime totals cover all sales accounts, including inactive accounts and activity before sales access was enabled. Pagination does not change these totals.'
        )}
      </p>
      <dl className='grid grid-cols-2 gap-4 lg:grid-cols-3'>
        <div>
          <dt className='text-muted-foreground text-sm'>
            {t('Invited Registrations')}
          </dt>
          <dd className='text-2xl font-semibold tabular-nums'>
            {props.summary.invitedCount}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-sm'>
            {t('Effective Customers')}
          </dt>
          <dd className='text-2xl font-semibold tabular-nums'>
            {props.summary.effectiveInvitedCount}
          </dd>
        </div>
        <div className='col-span-2 lg:col-span-1'>
          <dt className='text-muted-foreground text-sm'>
            {t('Confirmed Credited Recharges')}
          </dt>
          <dd className='text-2xl font-semibold tabular-nums'>
            {props.currency
              ? formatSalesAmount(props.summary.rechargeAmount, props.currency)
              : '—'}
          </dd>
        </div>
      </dl>
      {props.currency && (
        <SalesRebateAmounts
          rebates={props.summary.rebates}
          currency={props.currency}
        />
      )}
      <p className='text-muted-foreground text-sm'>
        {t(
          'A customer counts as effective after a successful paid wallet recharge, even if the balance is later spent.'
        )}
      </p>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Rebate totals include recorded customer rewards. Recharges without a rebate record may still be syncing.'
        )}
      </p>
      {props.summary.unverifiedRechargeCount > 0 && (
        <p className='text-sm' role='status'>
          {t('{{count}} historical recharge amounts need verification', {
            count: props.summary.unverifiedRechargeCount,
          })}
        </p>
      )}
    </section>
  )
}

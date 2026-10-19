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
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useAuthStore } from '@/stores/auth-store'

import { getResellerEarnings, resellerQueryKey } from './api'
import { formatMinor, formatExactMinor } from './money'

export function EarningsPanel() {
  const { t, i18n } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const query = useQuery({
    queryKey: resellerQueryKey(userId, 'earnings'),
    queryFn: getResellerEarnings,
  })
  if (query.isPending) return <Skeleton className='h-64 w-full' />
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load data')}
        description={query.error.message}
        onRetry={() => void query.refetch()}
      />
    )
  }
  const earnings = query.data
  const amounts = [
    { label: t('Estimated Earnings'), value: earnings.estimated_profit_minor },
    { label: t('Pending Earnings'), value: earnings.wallet.pending_minor },
    {
      label: t('Available for Withdrawal'),
      value: earnings.wallet.available_minor,
    },
    { label: t('Frozen Amount'), value: earnings.wallet.frozen_minor },
    { label: t('Outstanding Debt'), value: earnings.wallet.debt_minor },
    { label: t('Lifetime Earnings'), value: earnings.total_profit_minor },
  ]
  return (
    <div className='space-y-6'>
      {earnings.wallet.debt_minor > 0 && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Withdrawals are paused until outstanding debt is cleared.')}
          </AlertDescription>
        </Alert>
      )}
      <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
        {amounts.map((item) => (
          <Card key={item.label}>
            <CardHeader>
              <CardTitle>{item.label}</CardTitle>
            </CardHeader>
            <CardContent className='text-2xl font-semibold tabular-nums'>
              {item.value == null
                ? t('Not available')
                : formatMinor(item.value, i18n.language)}
            </CardContent>
          </Card>
        ))}
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Estimates use remaining paid customer balances and current prices. Only settled earnings become withdrawable after 24 hours.'
        )}
      </p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Created At')}</TableHead>
            <TableHead>{t('Reference')}</TableHead>
            <TableHead>{t('Earnings')}</TableHead>
            <TableHead>{t('Customer Spend')}</TableHead>
            <TableHead>{t('Supply Cost')}</TableHead>
            <TableHead>{t('Payment Fee')}</TableHead>
            <TableHead>{t('Available At')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {earnings.ledger.map((entry) => (
            <TableRow key={entry.id}>
              <TableCell>
                {new Date(entry.created_at * 1000).toLocaleString(
                  i18n.language
                )}
              </TableCell>
              <TableCell className='max-w-48 truncate'>
                {entry.source_reference}
              </TableCell>
              <TableCell>
                {formatMinor(entry.amount_minor, i18n.language)}
              </TableCell>
              <TableCell>
                {entry.sale_minor == null
                  ? '—'
                  : formatMinor(entry.sale_minor, i18n.language)}
              </TableCell>
              <TableCell>
                {entry.supplier_minor_exact == null
                  ? '—'
                  : formatExactMinor(entry.supplier_minor_exact, i18n.language)}
              </TableCell>
              <TableCell>
                {entry.payment_fee_minor == null
                  ? '—'
                  : formatMinor(entry.payment_fee_minor, i18n.language)}
              </TableCell>
              <TableCell>
                {entry.available_at > 0
                  ? new Date(entry.available_at * 1000).toLocaleString(
                      i18n.language
                    )
                  : '—'}
              </TableCell>
            </TableRow>
          ))}
          {earnings.ledger.length === 0 && (
            <TableRow>
              <TableCell colSpan={7}>{t('No data available')}</TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  )
}

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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { PaginationControls } from '@/features/invitations/components/pagination-controls'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuota } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { getResellerCustomers, resellerQueryKey } from './api'

export function CustomersPanel() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage ?? i18n.language)
  const userId = useAuthStore((state) => state.auth.user?.id)
  useSystemConfigStore((state) => state.config.currency)
  const [page, setPage] = useState(1)
  const pageSize = 20
  const query = useQuery({
    queryKey: [...resellerQueryKey(userId, 'customers'), page, pageSize],
    queryFn: () => getResellerCustomers(page, pageSize),
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
  return (
    <div>
      <p className='text-muted-foreground mb-3 text-xs'>
        {t(
          'Available balance excludes active reservations; total used includes settled charges only.'
        )}
      </p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Username')}</TableHead>
            <TableHead>{t('Display Name')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead className='text-right'>
              {t('Available Balance')}
            </TableHead>
            <TableHead className='text-right'>
              {t('Total credited quota')}
            </TableHead>
            <TableHead className='text-right'>{t('Total Used')}</TableHead>
            <TableHead>{t('Created At')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {query.data.items.map((customer) => (
            <TableRow key={customer.id}>
              <TableCell>{customer.username}</TableCell>
              <TableCell>{customer.display_name}</TableCell>
              <TableCell>
                {customer.status === 1 ? t('Active') : t('Disabled')}
              </TableCell>
              <TableCell className='text-right tabular-nums'>
                {formatQuota(customer.quota)}
              </TableCell>
              <TableCell className='text-right tabular-nums'>
                {formatQuota(customer.total_credited_quota)}
                {customer.total_refunded_quota > 0 && (
                  <div className='text-muted-foreground text-xs'>
                    {t('Refunded quota')}:{' '}
                    {formatQuota(customer.total_refunded_quota)}
                  </div>
                )}
              </TableCell>
              <TableCell className='text-right tabular-nums'>
                {formatQuota(customer.used_quota)}
              </TableCell>
              <TableCell>
                {new Date(customer.created_at * 1000).toLocaleString(locale)}
              </TableCell>
            </TableRow>
          ))}
          {query.data.items.length === 0 && (
            <TableRow>
              <TableCell colSpan={7}>{t('No data available')}</TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <PaginationControls
        page={page}
        pageSize={pageSize}
        total={query.data.total}
        onPageChange={setPage}
      />
    </div>
  )
}

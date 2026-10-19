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

import { getResellerCustomers, resellerQueryKey } from './api'

export function CustomersPanel() {
  const { t, i18n } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const query = useQuery({
    queryKey: resellerQueryKey(userId, 'customers'),
    queryFn: getResellerCustomers,
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
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('Username')}</TableHead>
          <TableHead>{t('Display Name')}</TableHead>
          <TableHead>{t('Status')}</TableHead>
          <TableHead>{t('Created At')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {query.data.map((customer) => (
          <TableRow key={customer.id}>
            <TableCell>{customer.username}</TableCell>
            <TableCell>{customer.display_name}</TableCell>
            <TableCell>
              {customer.status === 1 ? t('Active') : t('Disabled')}
            </TableCell>
            <TableCell>
              {new Date(customer.created_at * 1000).toLocaleString(
                i18n.language
              )}
            </TableCell>
          </TableRow>
        ))}
        {query.data.length === 0 && (
          <TableRow>
            <TableCell colSpan={4}>{t('No data available')}</TableCell>
          </TableRow>
        )}
      </TableBody>
    </Table>
  )
}

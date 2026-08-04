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
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

import { getInvitationRebateStats } from '../../api'
import { requireInvitationData } from '../../api/result'
import { formatRebateAmount } from '../../format'

export const RebateStatistics = () => {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['kkai', 'invitations', 'admin', 'statistics'],
    queryFn: async () =>
      requireInvitationData(
        await getInvitationRebateStats(),
        'Failed to load rebate statistics'
      ),
  })

  if (query.isPending) return <Skeleton className='h-56 w-full' />
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load rebate statistics')}
        description={query.error.message}
        onRetry={() => void query.refetch()}
      />
    )
  }

  const stats = query.data
  const values = [
    [t('Total Rebate'), formatRebateAmount(stats.total_rebate)],
    [t('Completed Rebate'), formatRebateAmount(stats.completed_rebate)],
    [t('Pending Rebate'), formatRebateAmount(stats.pending_rebate)],
    [t('Requested Rebate'), formatRebateAmount(stats.requested_rebate)],
    [t('Approved Rebate'), formatRebateAmount(stats.approved_rebate)],
    [t('Total Invitations'), String(stats.total_invitations)],
  ]

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Statistics')}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className='grid border sm:grid-cols-2 lg:grid-cols-3'>
          {values.map(([label, value]) => (
            <div key={label} className='min-w-0 border-b p-4 sm:border-r'>
              <div className='text-muted-foreground text-sm'>{label}</div>
              <div className='mt-1 truncate text-2xl font-semibold tabular-nums'>
                {value}
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

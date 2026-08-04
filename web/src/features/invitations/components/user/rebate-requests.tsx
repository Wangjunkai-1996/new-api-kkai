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
import { ClipboardList } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { getMyRebateRequests } from '../../api'
import { requireInvitationData } from '../../api/result'
import { formatInvitationDate, formatRebateAmount } from '../../format'
import {
  rebateRequestStatusLabel,
  rebateStatusVariant,
} from '../../status-label'
import { PaginationControls } from '../pagination-controls'

const PAGE_SIZE = 10

export const RebateRequests = () => {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const query = useQuery({
    queryKey: ['kkai', 'invitations', 'rebate-requests', page],
    queryFn: async () =>
      requireInvitationData(
        await getMyRebateRequests({ page, pageSize: PAGE_SIZE }),
        'Failed to load rebate requests'
      ),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Rebate Requests')}</CardTitle>
      </CardHeader>
      <CardContent>
        {query.isPending && <Skeleton className='h-56 w-full' />}
        {query.isError && (
          <ErrorState
            title={t('Failed to load rebate requests')}
            description={query.error.message}
            onRetry={() => void query.refetch()}
          />
        )}
        {query.data?.items.length === 0 && (
          <EmptyState
            icon={ClipboardList}
            title={t('No rebate requests')}
            bordered
          />
        )}
        {query.data && query.data.items.length > 0 && (
          <div className='overflow-hidden rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Amount')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead>{t('Created At')}</TableHead>
                  <TableHead>{t('Review Note')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {query.data.items.map((request) => (
                  <TableRow key={request.id}>
                    <TableCell className='tabular-nums'>
                      {formatRebateAmount(request.amount)}
                    </TableCell>
                    <TableCell>
                      <Badge variant={rebateStatusVariant(request.status)}>
                        {t(rebateRequestStatusLabel(request.status))}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      {formatInvitationDate(request.createdAt)}
                    </TableCell>
                    <TableCell className='max-w-64 whitespace-normal'>
                      {request.rejectReason || request.reviewNote || '-'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        {query.data && (
          <PaginationControls
            page={page}
            pageSize={PAGE_SIZE}
            total={query.data.total}
            onPageChange={setPage}
          />
        )}
      </CardContent>
    </Card>
  )
}

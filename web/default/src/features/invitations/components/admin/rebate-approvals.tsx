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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'

import { getAdminRebateRequests } from '../../api'
import { requireInvitationData } from '../../api/result'
import { getInvitationErrorMessage } from '../../format'
import { createApproveAndPayCommand } from '../../payout-command'
import {
  summarizeApproveAndPay,
  type ApproveAndPayCounts,
} from '../../settlement-result'
import type { RebateRequestStatus } from '../../types'
import { PaginationControls } from '../pagination-controls'
import { RebateApprovalTable } from './rebate-approval-table'
import { runRequestAction } from './request-action'
import {
  RequestActionDialog,
  type RebateRequestAction,
} from './request-action-dialog'
import { showSettlementNotice } from './settlement-toast'

const PAGE_SIZE = 20
const REQUESTS_QUERY_KEY = ['kkai', 'invitations', 'admin', 'requests']

export const RebateApprovals = () => {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<RebateRequestStatus | 'all'>('pending')
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [action, setAction] = useState<RebateRequestAction | null>(null)
  const query = useQuery({
    queryKey: [...REQUESTS_QUERY_KEY, page, status],
    queryFn: async () =>
      requireInvitationData(
        await getAdminRebateRequests({
          page,
          pageSize: PAGE_SIZE,
          ...(status === 'all' ? {} : { status }),
        }),
        'Failed to load rebate requests'
      ),
  })
  const mutation = useMutation({
    mutationFn: runRequestAction,
    onSuccess: async (data, completedAction) => {
      if (
        completedAction.kind === 'approve-pay' ||
        completedAction.kind === 'batch-approve-pay'
      ) {
        showSettlementNotice(
          summarizeApproveAndPay(data as ApproveAndPayCounts),
          t
        )
      } else {
        toast.success(t('Rebate request updated'))
      }
      setAction(null)
      setSelectedIds([])
      await queryClient.invalidateQueries({
        queryKey: ['kkai', 'invitations'],
      })
    },
    onError: (error) =>
      toast.error(
        getInvitationErrorMessage(error, t('Failed to update rebate request'))
      ),
  })
  return (
    <Card>
      <CardHeader className='flex-row flex-wrap items-center justify-between gap-3'>
        <CardTitle>{t('Rebate Approvals')}</CardTitle>
        <div className='flex flex-wrap gap-2'>
          {selectedIds.length > 0 && (
            <Button
              size='sm'
              onClick={() =>
                setAction({
                  kind: 'batch-approve-pay',
                  requestIds: selectedIds,
                  command: createApproveAndPayCommand('batch'),
                })
              }
            >
              {t('Approve and Pay')} ({selectedIds.length})
            </Button>
          )}
          <Select
            value={status}
            onValueChange={(value) => {
              setStatus(value as RebateRequestStatus | 'all')
              setPage(1)
              setSelectedIds([])
            }}
          >
            <SelectTrigger className='w-36' aria-label={t('Filter by status')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All Status')}</SelectItem>
              <SelectItem value='pending'>{t('Pending')}</SelectItem>
              <SelectItem value='approved'>{t('Approved')}</SelectItem>
              <SelectItem value='rejected'>{t('Rejected')}</SelectItem>
              <SelectItem value='completed'>{t('Completed')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </CardHeader>
      <CardContent>
        {query.isPending && <Skeleton className='h-72 w-full' />}
        {query.isError && (
          <ErrorState
            title={t('Failed to load rebate requests')}
            description={query.error.message}
            onRetry={() => void query.refetch()}
          />
        )}
        {query.data?.items.length === 0 && (
          <EmptyState
            icon={CheckCircle2}
            title={t('No rebate requests')}
            bordered
          />
        )}
        {query.data && query.data.items.length > 0 && (
          <RebateApprovalTable
            requests={query.data.items}
            selectedIds={selectedIds}
            onSelectedIdsChange={setSelectedIds}
            onAction={setAction}
          />
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
      <RequestActionDialog
        action={action}
        pending={mutation.isPending}
        onActionChange={setAction}
        onConfirm={() => action && mutation.mutate(action)}
      />
    </Card>
  )
}

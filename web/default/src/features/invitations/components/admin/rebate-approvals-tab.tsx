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
import type { TFunction } from 'i18next'
import { WalletCards } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import {
  approveAndPayRebateRequest,
  batchApproveAndPayRebateRequests,
  getRebateRequests,
} from '../../api'
import { getInvitationErrorMessage } from '../../lib/error'
import type {
  ApproveAndPayRebateResponse,
  BatchApproveAndPayRebateResponse,
  RebateRequestAdmin,
  RebateRequestStatus,
} from '../../types'
import { ApproveAndPayDialog } from './approve-and-pay-dialog'
import { RebateRequestsTable } from './rebate-requests-table'

export function RebateApprovalsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState<RebateRequestStatus | 'all'>(
    'all'
  )
  const [approveAndPayRequests, setApproveAndPayRequests] = useState<
    RebateRequestAdmin[]
  >([])

  // 获取返利申请列表
  const { data: requestsData, isLoading } = useQuery({
    queryKey: ['adminRebateRequests', statusFilter],
    queryFn: async () => {
      const params = statusFilter === 'all' ? {} : { status: statusFilter }
      const response = await getRebateRequests(params)
      return response.data
    },
  })
  const requests = requestsData?.items ?? []
  const actionableRequests = requests.filter(isApproveAndPayEligible)

  const approveAndPayMutation = useMutation({
    mutationFn: async (selectedRequests: RebateRequestAdmin[]) => {
      if (selectedRequests.length === 1) {
        return approveAndPayRebateRequest(selectedRequests[0].id)
      }

      return batchApproveAndPayRebateRequests({
        requestIds: selectedRequests.map((request) => request.id),
      })
    },
    onSuccess: (response) => {
      const data = response.data
      toast.success(approveAndPaySuccessMessage(t, data))
      queryClient.invalidateQueries({ queryKey: ['adminRebateRequests'] })
      queryClient.invalidateQueries({ queryKey: ['rebateStats'] })
      queryClient.invalidateQueries({ queryKey: ['adminRebateRecords'] })
      setApproveAndPayRequests([])
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to approve and pay rebates'))
      )
    },
  })

  const openApproveAndPayDialog = (selectedRequests: RebateRequestAdmin[]) => {
    setApproveAndPayRequests(selectedRequests.filter(isApproveAndPayEligible))
  }

  const closeApproveAndPayDialog = () => {
    if (approveAndPayMutation.isPending) return
    setApproveAndPayRequests([])
  }

  return (
    <Card>
      <CardHeader>
        <div className='flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between'>
          <CardTitle>{t('Rebate Approvals')}</CardTitle>
          <div className='flex flex-wrap items-center gap-2'>
            <Button
              type='button'
              size='sm'
              disabled={
                actionableRequests.length === 0 ||
                approveAndPayMutation.isPending
              }
              onClick={() => openApproveAndPayDialog(actionableRequests)}
            >
              <WalletCards className='size-4' />
              {t('Approve and Pay All')}
            </Button>
            <Label htmlFor='status-filter' className='whitespace-nowrap'>
              {t('Status')}:
            </Label>
            <Select
              value={statusFilter}
              onValueChange={(value) =>
                setStatusFilter(value as RebateRequestStatus | 'all')
              }
            >
              <SelectTrigger id='status-filter' className='w-[180px]'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='all'>{t('All')}</SelectItem>
                <SelectItem value='pending'>{t('Pending')}</SelectItem>
                <SelectItem value='approved'>{t('Approved')}</SelectItem>
                <SelectItem value='rejected'>{t('Rejected')}</SelectItem>
                <SelectItem value='completed'>{t('Completed')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <RebateRequestsTable
          requests={requests}
          loading={isLoading}
          actionLoading={approveAndPayMutation.isPending}
          onApproveAndPay={(request) => openApproveAndPayDialog([request])}
        />
      </CardContent>
      <ApproveAndPayDialog
        open={approveAndPayRequests.length > 0}
        requests={approveAndPayRequests}
        loading={approveAndPayMutation.isPending}
        onClose={closeApproveAndPayDialog}
        onConfirm={() => approveAndPayMutation.mutate(approveAndPayRequests)}
      />
    </Card>
  )
}

function isApproveAndPayEligible(request: RebateRequestAdmin) {
  return request.status === 'pending' || request.status === 'approved'
}

function approveAndPaySuccessMessage(
  t: TFunction,
  data:
    | ApproveAndPayRebateResponse
    | BatchApproveAndPayRebateResponse
    | undefined
) {
  if (!data) {
    return t('Settlement request submitted; waiting for settlement')
  }

  const recordCounts = {
    pending: data.pendingCount ?? 0,
    paid: data.paidCount ?? 0,
    alreadyPaid: data.alreadyPaidCount ?? 0,
    failed: data.failedCount ?? 0,
  }

  if ('failedRequests' in data) {
    return t(
      'Settlement records: {{pending}} pending, {{paid}} paid, {{alreadyPaid}} already paid, {{failed}} failed; failed requests: {{failedRequests}}',
      {
        ...recordCounts,
        failedRequests: data.failedRequests ?? 0,
      }
    )
  }

  return t(
    'Settlement records: {{pending}} pending, {{paid}} paid, {{alreadyPaid}} already paid, {{failed}} failed',
    recordCounts
  )
}

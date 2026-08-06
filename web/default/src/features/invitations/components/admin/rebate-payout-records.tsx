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
import { Banknote } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'

import {
  executeAdminRebatePayout,
  getAdminRebateRecords,
  revokeAdminSignupRewardRecord,
} from '../../api'
import { requireInvitationData } from '../../api/result'
import { getInvitationErrorMessage } from '../../format'
import { summarizePayoutAction } from '../../settlement-result'
import type { RebatePayoutActionResponse } from '../../types'
import { PaginationControls } from '../pagination-controls'
import {
  PayoutActionDialog,
  type PayoutActionInput,
} from './payout-action-dialog'
import { PayoutRecordTable } from './payout-record-table'
import { showSettlementNotice } from './settlement-toast'

const PAGE_SIZE = 20
const PAYOUT_RECORDS_KEY = ['kkai', 'invitations', 'admin', 'payout-records']

export const RebatePayoutRecords = () => {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [source, setSource] = useState<'order' | 'signup'>('order')
  const [action, setAction] = useState<PayoutActionInput | null>(null)
  const query = useQuery({
    queryKey: [...PAYOUT_RECORDS_KEY, page, source],
    queryFn: async () =>
      requireInvitationData(
        await getAdminRebateRecords({
          page,
          pageSize: PAGE_SIZE,
          source,
        }),
        'Failed to load payout records'
      ),
  })
  const mutation = useMutation({
    mutationFn: async (input: PayoutActionInput) => {
      if (input.kind === 'payout') {
        return requireInvitationData(
          await executeAdminRebatePayout(input.command),
          'Failed to update payout'
        )
      }
      return requireInvitationData(
        await revokeAdminSignupRewardRecord(input.record.id),
        'Failed to revoke signup reward'
      )
    },
    onSuccess: async (data, completedAction) => {
      setAction(null)
      if (completedAction.kind === 'payout') {
        showSettlementNotice(
          summarizePayoutAction(data as RebatePayoutActionResponse),
          t
        )
      } else {
        toast.success(t('Signup reward revoked'))
      }
      await queryClient.invalidateQueries({
        queryKey: ['kkai', 'invitations'],
      })
    },
    onError: (error) =>
      toast.error(
        getInvitationErrorMessage(error, t('Failed to update payout'))
      ),
  })

  return (
    <Card>
      <CardHeader className='flex-row items-center justify-between gap-3'>
        <CardTitle>{t('Balance Payout Records')}</CardTitle>
        <Select
          value={source}
          onValueChange={(value) => {
            setSource(value as typeof source)
            setPage(1)
          }}
        >
          <SelectTrigger className='w-36' aria-label={t('Source')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='order'>{t('Order')}</SelectItem>
            <SelectItem value='signup'>{t('Signup')}</SelectItem>
          </SelectContent>
        </Select>
      </CardHeader>
      <CardContent>
        {query.isPending && <Skeleton className='h-72 w-full' />}
        {query.isError && (
          <ErrorState
            title={t('Failed to load payout records')}
            description={query.error.message}
            onRetry={() => void query.refetch()}
          />
        )}
        {query.data?.items.length === 0 && (
          <EmptyState icon={Banknote} title={t('No payout records')} bordered />
        )}
        {query.data && query.data.items.length > 0 && (
          <PayoutRecordTable
            records={query.data.items}
            mode={source}
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
      <PayoutActionDialog
        input={action}
        pending={mutation.isPending}
        onClose={() => setAction(null)}
        onConfirm={() => action && mutation.mutate(action)}
      />
    </Card>
  )
}

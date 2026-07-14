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
import { ReceiptText } from 'lucide-react'
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
  closeAdminRebateOrderRecords,
  endAdminRebateOrderInitialization,
  extendAdminRebateOrderInitialization,
  getAdminRebateOrderRecords,
  reopenAdminRebateOrderRecords,
  updateAdminRebateOrderRecords,
} from '../../api'
import { requireInvitationData } from '../../api/result'
import { getInvitationErrorMessage } from '../../format'
import { PaginationControls } from '../pagination-controls'
import {
  OrderRecordActionDialog,
  type OrderRecordActionInput,
} from './order-record-action-dialog'
import { RebateOrderRecordTable } from './rebate-order-record-table'

const PAGE_SIZE = 20
const ORDER_RECORDS_KEY = ['kkai', 'invitations', 'admin', 'order-records']

export const RebateOrderRecords = () => {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [orderType, setOrderType] = useState<'all' | 'topup' | 'subscription'>(
    'all'
  )
  const [action, setAction] = useState<OrderRecordActionInput | null>(null)
  const query = useQuery({
    queryKey: [...ORDER_RECORDS_KEY, page, orderType],
    queryFn: async () =>
      requireInvitationData(
        await getAdminRebateOrderRecords({
          page,
          pageSize: PAGE_SIZE,
          ...(orderType === 'all' ? {} : { orderType }),
        }),
        'Failed to load rebate order records'
      ),
  })
  const mutation = useMutation({
    mutationFn: runOrderRecordAction,
    onSuccess: async () => {
      setAction(null)
      toast.success(t('Rebate order record updated'))
      await queryClient.invalidateQueries({
        queryKey: ['kkai', 'invitations'],
      })
    },
    onError: (error) =>
      toast.error(
        getInvitationErrorMessage(error, t('Failed to update order record'))
      ),
  })

  return (
    <Card>
      <CardHeader className='flex-row items-center justify-between gap-3'>
        <CardTitle>{t('Order Rebate Records')}</CardTitle>
        <Select
          value={orderType}
          onValueChange={(value) => {
            setOrderType(value as typeof orderType)
            setPage(1)
          }}
        >
          <SelectTrigger className='w-40' aria-label={t('Order Type')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='all'>{t('All Types')}</SelectItem>
            <SelectItem value='topup'>{t('Top-up')}</SelectItem>
            <SelectItem value='subscription'>{t('Subscription')}</SelectItem>
          </SelectContent>
        </Select>
      </CardHeader>
      <CardContent>
        {query.isPending && <Skeleton className='h-72 w-full' />}
        {query.isError && (
          <ErrorState
            title={t('Failed to load rebate order records')}
            description={query.error.message}
            onRetry={() => void query.refetch()}
          />
        )}
        {query.data?.items.length === 0 && (
          <EmptyState
            icon={ReceiptText}
            title={t('No order records')}
            bordered
          />
        )}
        {query.data && query.data.items.length > 0 && (
          <RebateOrderRecordTable
            records={query.data.items}
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
      <OrderRecordActionDialog
        key={
          action
            ? `${action.record.orderType}:${action.record.orderId}:${action.action}`
            : 'closed'
        }
        input={action}
        pending={mutation.isPending}
        onClose={() => setAction(null)}
        onConfirm={(input) => mutation.mutate(input)}
      />
    </Card>
  )
}

const runOrderRecordAction = async (input: OrderRecordActionInput) => {
  const recordId = input.record.localRebateRecordId
  if (!recordId) throw new Error('Rebate record is not initialized')
  const ids = [recordId]
  if (input.action === 'modify') {
    return requireInvitationData(
      await updateAdminRebateOrderRecords({
        recordIds: ids,
        rebateAmount: input.rebateAmount,
        rebateRatio: input.rebateRatio,
      }),
      'Failed to update rebate amount'
    )
  }
  if (input.action === 'close') {
    return requireInvitationData(
      await closeAdminRebateOrderRecords(ids),
      'Failed to close rebate record'
    )
  }
  if (input.action === 'reopen') {
    return requireInvitationData(
      await reopenAdminRebateOrderRecords(ids),
      'Failed to reopen rebate record'
    )
  }
  if (input.action === 'end-initialization') {
    return requireInvitationData(
      await endAdminRebateOrderInitialization(ids),
      'Failed to end rebate initialization'
    )
  }
  if (!input.initializationEndsAt) throw new Error('Invalid initialization end')
  return requireInvitationData(
    await extendAdminRebateOrderInitialization(
      ids,
      Math.floor(input.initializationEndsAt / 1000)
    ),
    'Failed to extend rebate initialization'
  )
}

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
import type { PaginationState } from '@tanstack/react-table'
import { Loader2, RotateCcw, Wallet } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import {
  createRebatePayoutIdempotencyKey,
  getAdminRebateRecords,
  getAdminRebatePayoutStatus,
  payAdminRebatePayout,
  reverseAdminRebatePayout,
} from '../../api'
import { getInvitationErrorMessage } from '../../lib/error'
import { formatRebateAmount } from '../../lib/format'
import type {
  RebatePayoutAction,
  RebatePayoutStatusResponse,
  RebateRecord,
  RebateStatus,
} from '../../types'

type PayoutDialogState = {
  action: RebatePayoutAction
  record: RebateRecord
  status?: RebatePayoutStatusResponse | null
} | null

const REBATE_STATUS_COLORS: Record<RebateStatus, string> = {
  pending: 'bg-amber-500/10 text-amber-700 dark:text-amber-400',
  requested: 'bg-sky-500/10 text-sky-700 dark:text-sky-400',
  approved: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
  completed: 'bg-slate-500/10 text-slate-700 dark:text-slate-400',
  rejected: 'bg-red-500/10 text-red-700 dark:text-red-400',
}

function formatDateTime(value?: string | null): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString()
}

function rebateStatusLabel(
  t: ReturnType<typeof useTranslation>['t'],
  status: RebateStatus
): string {
  const labels: Record<RebateStatus, string> = {
    pending: t('Pending'),
    requested: t('Requested'),
    approved: t('Approved'),
    completed: t('Completed'),
    rejected: t('Rejected'),
  }
  return labels[status]
}

function rebateRecordTypeLabel(
  t: ReturnType<typeof useTranslation>['t'],
  orderType: string
): string {
  switch (orderType) {
    case 'topup':
      return t('Topup')
    case 'subscription':
      return t('Subscription')
    case 'redemption':
    case 'redemption_code':
    case 'redeem':
    case 'voucher':
    case 'other':
      return t('Voucher Rebate')
    default:
      return orderType || t('Voucher / Order')
  }
}

function payoutStatusValue(
  record: RebateRecord,
  status?: RebatePayoutStatusResponse | null
): string {
  return (
    status?.status ??
    record.payout?.status ??
    record.payoutStatus ??
    'unknown'
  ).toLowerCase()
}

function balanceLogId(
  record: RebateRecord,
  status?: RebatePayoutStatusResponse | null
): number | string | null | undefined {
  return (
    status?.balanceLogId ??
    status?.balanceLogID ??
    record.payout?.balanceLogId ??
    record.payout?.balanceLogID ??
    record.payoutBalanceLogId ??
    record.payoutBalanceLogID
  )
}

function payoutTimestamp(
  record: RebateRecord,
  status?: RebatePayoutStatusResponse | null
): string | null | undefined {
  return (
    status?.paidAt ??
    status?.reversedAt ??
    status?.updatedAt ??
    record.payout?.paidAt ??
    record.payout?.reversedAt ??
    record.payout?.updatedAt ??
    record.payoutPaidAt ??
    record.payoutReversedAt
  )
}

function payoutStatusLabel(
  t: ReturnType<typeof useTranslation>['t'],
  value: string
): string {
  switch (value) {
    case 'pending':
    case 'unpaid':
      return t('Pending payout')
    case 'processing':
      return t('Payout processing')
    case 'paid':
    case 'completed':
    case 'success':
      return t('Balance paid')
    case 'reversed':
    case 'reverted':
      return t('Reversed')
    case 'failed':
      return t('Payout failed')
    case 'not_found':
    case 'none':
      return t('No payout ledger')
    default:
      return t('Unknown payout status')
  }
}

function payoutStatusClass(value: string): string {
  switch (value) {
    case 'paid':
    case 'completed':
    case 'success':
      return 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400'
    case 'processing':
      return 'bg-sky-500/10 text-sky-700 dark:text-sky-400'
    case 'reversed':
    case 'reverted':
      return 'bg-purple-500/10 text-purple-700 dark:text-purple-400'
    case 'failed':
      return 'bg-red-500/10 text-red-700 dark:text-red-400'
    case 'pending':
    case 'unpaid':
      return 'bg-amber-500/10 text-amber-700 dark:text-amber-400'
    default:
      return 'bg-slate-500/10 text-slate-700 dark:text-slate-400'
  }
}

function canPayPayout(
  record: RebateRecord,
  status?: RebatePayoutStatusResponse | null
): boolean {
  if (status?.canPay != null) return status.canPay
  if (record.canPay != null) return record.canPay

  return ['pending', 'unpaid', 'failed', 'reversed', 'reverted'].includes(
    payoutStatusValue(record, status)
  )
}

function canReversePayout(
  record: RebateRecord,
  status?: RebatePayoutStatusResponse | null
): boolean {
  if (status?.canReverse != null) return status.canReverse
  if (record.canReverse != null) return record.canReverse

  return ['paid', 'completed', 'success'].includes(
    payoutStatusValue(record, status)
  )
}

function formatRebateUser(record: RebateRecord): string {
  const userId = record.userId ?? record.inviterId
  const userName = record.userName ?? record.inviterName
  return userName ? `${userName} (#${userId})` : `#${userId}`
}

export function VoucherRebatePayoutRecordsPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })
  const [payoutDialog, setPayoutDialog] = useState<PayoutDialogState>(null)

  const queryKey = [
    'adminRebateRecords',
    'order',
    pagination.pageIndex + 1,
    pagination.pageSize,
  ]

  const { data, isLoading, isFetching } = useQuery({
    queryKey,
    queryFn: async () => {
      const response = await getAdminRebateRecords({
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        source: 'order',
      })
      return response.data
    },
  })

  const payoutMutation = useMutation({
    mutationFn: async ({
      action,
      recordId,
      idempotencyKey,
    }: {
      action: RebatePayoutAction
      recordId: number
      idempotencyKey: string
    }) => {
      const payload = { idempotencyKey }
      return action === 'pay'
        ? payAdminRebatePayout(recordId, payload)
        : reverseAdminRebatePayout(recordId, payload)
    },
    onSuccess: (_response, variables) => {
      toast.success(t('Settlement request submitted; waiting for settlement'))
      queryClient.invalidateQueries({ queryKey: ['adminRebateRecords'] })
      queryClient.invalidateQueries({ queryKey: ['adminRebateRequests'] })
      queryClient.invalidateQueries({ queryKey: ['rebateStats'] })
      queryClient.invalidateQueries({
        queryKey: ['adminRebatePayoutStatus', variables.recordId],
      })
      setPayoutDialog(null)
    },
    onError: (error: unknown, variables) => {
      toast.error(
        getInvitationErrorMessage(
          error,
          variables.action === 'pay'
            ? t('Failed to pay voucher rebate')
            : t('Failed to reverse voucher rebate payout')
        )
      )
    },
  })

  const items = data?.items ?? []
  const total = data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pagination.pageSize))
  const currentPage = pagination.pageIndex + 1

  const goPrevious = () => {
    setPagination((current) => ({
      ...current,
      pageIndex: Math.max(0, current.pageIndex - 1),
    }))
  }

  const goNext = () => {
    setPagination((current) => ({
      ...current,
      pageIndex: Math.min(pageCount - 1, current.pageIndex + 1),
    }))
  }

  const submitPayoutAction = () => {
    if (!payoutDialog || payoutMutation.isPending) return

    payoutMutation.mutate({
      action: payoutDialog.action,
      recordId: payoutDialog.record.id,
      idempotencyKey: createRebatePayoutIdempotencyKey(
        payoutDialog.record.id,
        payoutDialog.action
      ),
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Voucher Rebate Payout Records')}</CardTitle>
        <CardDescription>
          {t(
            'Manage voucher rebate payouts and reversals through balance ledger entries'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading && !data ? (
          <div className='text-muted-foreground flex items-center justify-center py-8'>
            {t('Loading...')}
          </div>
        ) : (
          <>
            <div className='overflow-x-auto rounded-md border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Record ID')}</TableHead>
                    <TableHead>{t('Rebate User')}</TableHead>
                    <TableHead>{t('Reward Type')}</TableHead>
                    <TableHead>{t('Rebate Amount')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead>{t('Payout Status')}</TableHead>
                    <TableHead>{t('Balance Ledger')}</TableHead>
                    <TableHead>{t('Created At')}</TableHead>
                    <TableHead className='text-right'>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.length > 0 ? (
                    items.map((record) => (
                      <VoucherRebatePayoutRow
                        key={record.id}
                        record={record}
                        disabled={payoutMutation.isPending}
                        onAction={(action, status) =>
                          setPayoutDialog({ action, record, status })
                        }
                      />
                    ))
                  ) : (
                    <TableRow>
                      <TableCell colSpan={9} className='h-24 text-center'>
                        {t('No voucher rebate payout records found')}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>

            {data && data.total > 0 && (
              <div className='flex flex-col gap-3 px-2 py-4 sm:flex-row sm:items-center sm:justify-between'>
                <div className='text-muted-foreground text-sm'>
                  {isFetching
                    ? t('Refreshing payout status...')
                    : t('Showing {{from}} to {{to}} of {{total}} records', {
                        from: pagination.pageIndex * pagination.pageSize + 1,
                        to: Math.min(
                          (pagination.pageIndex + 1) * pagination.pageSize,
                          data.total
                        ),
                        total: data.total,
                      })}
                </div>
                <div className='flex items-center gap-2'>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={goPrevious}
                    disabled={currentPage <= 1}
                  >
                    {t('Previous')}
                  </Button>
                  <div className='min-w-24 text-center text-sm'>
                    {t('Page {{current}} of {{total}}', {
                      current: currentPage,
                      total: pageCount,
                    })}
                  </div>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={goNext}
                    disabled={currentPage >= pageCount}
                  >
                    {t('Next')}
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </CardContent>

      <PayoutActionDialog
        dialog={payoutDialog}
        loading={payoutMutation.isPending}
        onClose={() => setPayoutDialog(null)}
        onSubmit={submitPayoutAction}
      />
    </Card>
  )
}

function VoucherRebatePayoutRow({
  record,
  disabled,
  onAction,
}: {
  record: RebateRecord
  disabled: boolean
  onAction: (
    action: RebatePayoutAction,
    status?: RebatePayoutStatusResponse | null
  ) => void
}) {
  const { t } = useTranslation()
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['adminRebatePayoutStatus', record.id],
    queryFn: async () => {
      const response = await getAdminRebatePayoutStatus(record.id)
      return response.data
    },
    retry: false,
    staleTime: 15_000,
  })
  const statusLoading = isLoading && !data
  const value = payoutStatusValue(record, data)
  const ledgerId = balanceLogId(record, data)
  const actionsDisabled = disabled || statusLoading || isError
  const payDisabled = actionsDisabled || !canPayPayout(record, data)
  const reverseDisabled = actionsDisabled || !canReversePayout(record, data)
  let payoutStatusContent

  if (statusLoading) {
    payoutStatusContent = (
      <Badge variant='outline' className='text-muted-foreground'>
        {t('Loading...')}
      </Badge>
    )
  } else if (isError) {
    payoutStatusContent = (
      <Badge
        variant='outline'
        className='bg-red-500/10 text-red-700 dark:text-red-400'
        title={getInvitationErrorMessage(
          error,
          t('Failed to load payout status')
        )}
      >
        {t('Payout status unavailable')}
      </Badge>
    )
  } else {
    payoutStatusContent = (
      <div className='flex flex-col gap-1'>
        <Badge variant='outline' className={payoutStatusClass(value)}>
          {payoutStatusLabel(t, value)}
        </Badge>
        {payoutTimestamp(record, data) && (
          <span className='text-muted-foreground text-xs'>
            {formatDateTime(payoutTimestamp(record, data))}
          </span>
        )}
      </div>
    )
  }

  return (
    <TableRow>
      <TableCell className='font-mono'>#{record.id}</TableCell>
      <TableCell className='font-mono'>{formatRebateUser(record)}</TableCell>
      <TableCell>{rebateRecordTypeLabel(t, record.orderType)}</TableCell>
      <TableCell className='font-medium'>
        {formatRebateAmount(record.rebateAmount)}
      </TableCell>
      <TableCell>
        <Badge
          variant='outline'
          className={REBATE_STATUS_COLORS[record.status]}
        >
          {rebateStatusLabel(t, record.status)}
        </Badge>
      </TableCell>
      <TableCell>{payoutStatusContent}</TableCell>
      <TableCell>
        {ledgerId ? (
          <span
            className='font-mono text-sm'
            title={t('Balance ledger entry for voucher rebate payout')}
          >
            #{ledgerId}
          </span>
        ) : (
          <span className='text-muted-foreground'>-</span>
        )}
      </TableCell>
      <TableCell className='text-muted-foreground'>
        {formatDateTime(record.createdAt)}
      </TableCell>
      <TableCell className='text-right'>
        <div className='flex justify-end gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={payDisabled}
            title={t('Pay voucher rebate to balance')}
            onClick={() => onAction('pay', data)}
          >
            <Wallet className='size-4' />
            {t('Pay Rebate')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={reverseDisabled}
            title={t('Reverse voucher rebate balance payout')}
            onClick={() => onAction('reverse', data)}
          >
            <RotateCcw className='size-4' />
            {t('Reverse')}
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}

function PayoutActionDialog({
  dialog,
  loading,
  onClose,
  onSubmit,
}: {
  dialog: PayoutDialogState
  loading: boolean
  onClose: () => void
  onSubmit: () => void
}) {
  const { t } = useTranslation()

  if (!dialog) return null

  const isPay = dialog.action === 'pay'
  const statusValue = payoutStatusValue(dialog.record, dialog.status)
  const ledgerId = balanceLogId(dialog.record, dialog.status)

  return (
    <Dialog open={dialog != null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='sm:max-w-[520px]'>
        <DialogHeader>
          <DialogTitle>
            {isPay ? t('Pay Voucher Rebate') : t('Reverse Voucher Rebate')}
          </DialogTitle>
          <DialogDescription>
            {isPay
              ? t(
                  'This will pay the voucher rebate to balance through the payout service and create a balance ledger entry.'
                )
              : t(
                  'This will reverse the voucher rebate payout through the payout service and write a reversal ledger entry.'
                )}
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-3 text-sm'>
          <div className='flex justify-between gap-4'>
            <span className='text-muted-foreground'>{t('Record ID')}</span>
            <span className='font-mono'>#{dialog.record.id}</span>
          </div>
          <div className='flex justify-between gap-4'>
            <span className='text-muted-foreground'>{t('Rebate User')}</span>
            <span className='font-mono'>{formatRebateUser(dialog.record)}</span>
          </div>
          <div className='flex justify-between gap-4'>
            <span className='text-muted-foreground'>{t('Rebate Amount')}</span>
            <span className='font-medium'>
              {formatRebateAmount(dialog.record.rebateAmount)}
            </span>
          </div>
          <div className='flex justify-between gap-4'>
            <span className='text-muted-foreground'>{t('Payout Status')}</span>
            <span>{payoutStatusLabel(t, statusValue)}</span>
          </div>
          {ledgerId && (
            <div className='flex justify-between gap-4'>
              <span className='text-muted-foreground'>
                {t('Balance Ledger')}
              </span>
              <span className='font-mono'>#{ledgerId}</span>
            </div>
          )}
        </div>
        <div className='bg-muted rounded-md p-3 text-sm'>
          {isPay
            ? t(
                'Do not use the legacy Complete action for voucher rebates. This action keeps the balance ledger and payout status in sync.'
              )
            : t(
                'Use reversal only when the previous voucher rebate balance payout must be corrected.'
              )}
        </div>
        <DialogFooter>
          <Button type='button' variant='outline' onClick={onClose}>
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            variant={isPay ? 'default' : 'destructive'}
            disabled={loading}
            onClick={onSubmit}
          >
            {loading && <Loader2 className='size-4 animate-spin' />}
            {isPay ? t('Pay Rebate') : t('Reverse')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

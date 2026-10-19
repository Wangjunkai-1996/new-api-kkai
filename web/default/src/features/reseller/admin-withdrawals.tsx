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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  getManagedWithdrawals,
  reviewWithdrawal,
  type ManagedWithdrawal,
  type WithdrawalReview,
} from './admin-api'
import { resellerQueryKey } from './api'
import { formatMinor } from './money'
import { withdrawalStatusLabel } from './withdrawal-status'
import { WithdrawalTimeline } from './withdrawal-timeline'

export function AdminWithdrawals() {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [page, setPage] = useState(1)
  const pageSize = 20
  const [selection, setSelection] = useState<{
    withdrawal: ManagedWithdrawal
    status: WithdrawalReview['status']
  } | null>(null)
  const [paymentRef, setPaymentRef] = useState('')
  const [note, setNote] = useState('')
  const query = useQuery({
    queryKey: [...resellerQueryKey(userId, 'admin-withdrawals'), page],
    queryFn: () => getManagedWithdrawals(page, pageSize),
  })
  const mutation = useMutation({
    mutationFn: () => {
      if (!selection) throw new Error('No withdrawal selected')
      return reviewWithdrawal(selection.withdrawal.id, {
        status: selection.status,
        payment_ref: paymentRef.trim(),
        note: note.trim(),
      })
    },
    onSuccess: () => {
      setSelection(null)
      void client.invalidateQueries({
        queryKey: resellerQueryKey(userId, 'admin-withdrawals'),
      })
    },
    onError: (error) => handleServerError(error),
  })
  function choose(
    withdrawal: ManagedWithdrawal,
    status: WithdrawalReview['status']
  ) {
    setSelection({ withdrawal, status })
    setPaymentRef('')
    setNote('')
  }
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
  const requiresNote =
    selection?.status === 'rejected' || selection?.status === 'failed'
  return (
    <div className='space-y-4'>
      <Alert>
        <AlertDescription>
          {t(
            'Record confirmed manual payouts only. If payment is uncertain, check Alipay before changing the status or paying again.'
          )}
        </AlertDescription>
      </Alert>
      <Button
        variant='outline'
        disabled={query.isFetching}
        onClick={() => void query.refetch()}
      >
        {t('Refresh')}
      </Button>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Reference')}</TableHead>
            <TableHead>{t('Reseller')}</TableHead>
            <TableHead>{t('Alipay Account')}</TableHead>
            <TableHead>{t('Amount')}</TableHead>
            <TableHead>{t('Withdrawal Fee')}</TableHead>
            <TableHead>{t('Net Amount')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {query.data.items.map((withdrawal) => (
            <TableRow key={withdrawal.id}>
              <TableCell>#{withdrawal.id}</TableCell>
              <TableCell>#{withdrawal.tenant_id}</TableCell>
              <TableCell>
                <p>{withdrawal.alipay_name}</p>
                <p className='break-all'>{withdrawal.alipay_account}</p>
              </TableCell>
              <TableCell>
                {formatMinor(withdrawal.amount_minor, i18n.language)}
              </TableCell>
              <TableCell>
                {formatMinor(withdrawal.fee_minor, i18n.language)}
              </TableCell>
              <TableCell>
                {formatMinor(withdrawal.net_minor, i18n.language)}
              </TableCell>
              <TableCell>
                <p>{withdrawalStatusLabel(withdrawal.status, t)}</p>
                <WithdrawalTimeline withdrawal={withdrawal} />
                {withdrawal.payment_ref && <p>{withdrawal.payment_ref}</p>}
                {withdrawal.review_note && <p>{withdrawal.review_note}</p>}
              </TableCell>
              <TableCell>
                <div className='flex flex-wrap gap-2'>
                  {withdrawal.status === 'requested' && (
                    <>
                      <Button
                        size='sm'
                        onClick={() => choose(withdrawal, 'approved')}
                      >
                        {t('Approve')}
                      </Button>
                      <Button
                        size='sm'
                        variant='destructive'
                        onClick={() => choose(withdrawal, 'rejected')}
                      >
                        {t('Reject')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => choose(withdrawal, 'failed')}
                      >
                        {t('Mark Failed')}
                      </Button>
                    </>
                  )}
                  {withdrawal.status === 'approved' && (
                    <>
                      <Button
                        size='sm'
                        onClick={() => choose(withdrawal, 'paid')}
                      >
                        {t('Record Paid')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => choose(withdrawal, 'failed')}
                      >
                        {t('Mark Failed')}
                      </Button>
                    </>
                  )}
                </div>
              </TableCell>
            </TableRow>
          ))}
          {!query.data.items.length && (
            <TableRow>
              <TableCell colSpan={8}>{t('No data available')}</TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <span className='text-muted-foreground text-sm'>
          {t('Page {{page}} of {{pageCount}}', {
            page,
            pageCount: Math.max(1, Math.ceil(query.data.total / pageSize)),
          })}
        </span>
        <div className='flex gap-2'>
          <Button
            variant='outline'
            disabled={page <= 1 || query.isFetching}
            onClick={() => setPage((current) => current - 1)}
          >
            {t('Previous')}
          </Button>
          <Button
            variant='outline'
            disabled={page * pageSize >= query.data.total || query.isFetching}
            onClick={() => setPage((current) => current + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      </div>
      <ConfirmDialog
        open={Boolean(selection)}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setSelection(null)
        }}
        title={t('Review Withdrawal')}
        desc={
          selection
            ? `${selection.withdrawal.alipay_name} · ${selection.withdrawal.alipay_account} · ${formatMinor(selection.withdrawal.net_minor, i18n.language)}`
            : ''
        }
        disabled={
          (selection?.status === 'paid' && !paymentRef.trim()) ||
          (requiresNote && !note.trim())
        }
        isLoading={mutation.isPending}
        destructive={requiresNote}
        handleConfirm={() => mutation.mutate()}
      >
        {selection?.status === 'paid' && (
          <div className='space-y-2'>
            <Label htmlFor='withdrawal-payment-ref'>
              {t('Payment Reference')}
            </Label>
            <Input
              id='withdrawal-payment-ref'
              value={paymentRef}
              maxLength={192}
              onChange={(event) => setPaymentRef(event.target.value)}
            />
          </div>
        )}
        <div className='space-y-2'>
          <Label htmlFor='withdrawal-review-note'>{t('Review Note')}</Label>
          <Textarea
            id='withdrawal-review-note'
            value={note}
            maxLength={4096}
            onChange={(event) => setNote(event.target.value)}
          />
        </div>
      </ConfirmDialog>
    </div>
  )
}

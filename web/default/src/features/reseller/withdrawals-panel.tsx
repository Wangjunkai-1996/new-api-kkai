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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import axios from 'axios'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ErrorState } from '@/components/error-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldError,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  getResellerEarnings,
  getResellerWithdrawals,
  requestResellerWithdrawal,
  resellerQueryKey,
  type WithdrawalInput,
} from './api'
import { formatMinor, parseCnyMinor } from './money'
import { withdrawalStatusLabel } from './withdrawal-status'
import { WithdrawalTimeline } from './withdrawal-timeline'

const withdrawalSchema = z.object({
  amount: z.string().refine((value) => (parseCnyMinor(value) ?? 0) >= 10000),
  alipay_account: z.string().trim().min(1).max(128),
  alipay_name: z.string().trim().min(1).max(128),
})

export function WithdrawalsPanel() {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [pendingRequest, setPendingRequest] = useState<WithdrawalInput | null>(
    null
  )
  const form = useForm<z.infer<typeof withdrawalSchema>>({
    resolver: zodResolver(withdrawalSchema),
    defaultValues: { amount: '', alipay_account: '', alipay_name: '' },
  })
  const earnings = useQuery({
    queryKey: resellerQueryKey(userId, 'earnings'),
    queryFn: getResellerEarnings,
  })
  const withdrawals = useQuery({
    queryKey: resellerQueryKey(userId, 'withdrawals'),
    queryFn: getResellerWithdrawals,
  })
  const mutation = useMutation({
    mutationFn: requestResellerWithdrawal,
    onSuccess: async () => {
      setPendingRequest(null)
      form.reset()
      await Promise.all([
        client.invalidateQueries({
          queryKey: resellerQueryKey(userId, 'earnings'),
        }),
        client.invalidateQueries({
          queryKey: resellerQueryKey(userId, 'withdrawals'),
        }),
      ])
      toast.success(t('Withdrawal request submitted'))
    },
    onError: (error) => {
      if (
        axios.isAxiosError(error) &&
        [400, 403, 422].includes(error.response?.status ?? 0)
      ) {
        setPendingRequest(null)
      }
      handleServerError(error)
    },
  })
  const unconfirmed = mutation.isError && pendingRequest !== null
  const hasDebt = (earnings.data?.wallet.debt_minor ?? 0) > 0
  if (withdrawals.isPending || earnings.isPending) {
    return <Skeleton className='h-80 w-full' />
  }
  if (withdrawals.isError || earnings.isError) {
    return (
      <ErrorState
        title={t('Failed to load data')}
        description={withdrawals.error?.message ?? earnings.error?.message}
        onRetry={() => {
          void withdrawals.refetch()
          void earnings.refetch()
        }}
      />
    )
  }
  return (
    <div className='space-y-6'>
      <p>
        {t('Available for Withdrawal')}:{' '}
        <strong>
          {formatMinor(earnings.data.wallet.available_minor, i18n.language)}
        </strong>
      </p>
      {hasDebt && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Withdrawals are paused until outstanding debt is cleared.')}
          </AlertDescription>
        </Alert>
      )}
      {unconfirmed && (
        <Alert>
          <AlertDescription>
            {t(
              'The request result is unconfirmed. Retry the same request to avoid a duplicate withdrawal.'
            )}
          </AlertDescription>
        </Alert>
      )}
      <form
        className='max-w-xl'
        onSubmit={form.handleSubmit((values) => {
          if (pendingRequest) {
            mutation.mutate(pendingRequest)
            return
          }
          const amount = parseCnyMinor(values.amount)
          if (amount == null || amount > earnings.data.wallet.available_minor) {
            form.setError('amount', { type: 'validate' })
            return
          }
          const request: WithdrawalInput = {
            amount_minor: amount,
            alipay_account: values.alipay_account,
            alipay_name: values.alipay_name,
            idempotency_key: crypto.randomUUID(),
          }
          setPendingRequest(request)
          mutation.mutate(request)
        })}
      >
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor='withdrawal-amount'>
              {t('Withdrawal Amount (CNY)')}
            </FieldLabel>
            <Input
              id='withdrawal-amount'
              inputMode='decimal'
              disabled={mutation.isPending || unconfirmed}
              aria-invalid={Boolean(form.formState.errors.amount)}
              {...form.register('amount')}
            />
            {form.formState.errors.amount && (
              <FieldError>
                {t(
                  'Enter at least CNY 100 without exceeding your available balance.'
                )}
              </FieldError>
            )}
          </Field>
          <Field>
            <FieldLabel htmlFor='withdrawal-alipay'>
              {t('Alipay Account')}
            </FieldLabel>
            <Input
              id='withdrawal-alipay'
              maxLength={128}
              autoComplete='off'
              disabled={mutation.isPending || unconfirmed}
              aria-invalid={Boolean(form.formState.errors.alipay_account)}
              {...form.register('alipay_account')}
            />
            {form.formState.errors.alipay_account && (
              <FieldError>{t('Please enter a valid value.')}</FieldError>
            )}
          </Field>
          <Field>
            <FieldLabel htmlFor='withdrawal-name'>
              {t('Account Holder Name')}
            </FieldLabel>
            <Input
              id='withdrawal-name'
              maxLength={128}
              autoComplete='name'
              disabled={mutation.isPending || unconfirmed}
              aria-invalid={Boolean(form.formState.errors.alipay_name)}
              {...form.register('alipay_name')}
            />
            {form.formState.errors.alipay_name && (
              <FieldError>{t('Please enter a valid value.')}</FieldError>
            )}
          </Field>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Withdrawals are reviewed and paid manually via Alipay. The withdrawal fee is deducted from the requested amount.'
            )}
          </p>
          {earnings.data.withdrawal_fee_minor != null && (
            <p>
              {t('Withdrawal Fee')}:{' '}
              {formatMinor(earnings.data.withdrawal_fee_minor, i18n.language)}
            </p>
          )}
          <Button
            type='submit'
            disabled={
              mutation.isPending ||
              hasDebt ||
              earnings.data.wallet.available_minor < 10000
            }
          >
            {unconfirmed ? t('Retry') : t('Request Withdrawal')}
          </Button>
        </FieldGroup>
      </form>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Created At')}</TableHead>
            <TableHead>{t('Amount')}</TableHead>
            <TableHead>{t('Withdrawal Fee')}</TableHead>
            <TableHead>{t('Net Amount')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead>{t('Review Note')}</TableHead>
            <TableHead>{t('Payment Reference')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {withdrawals.data.map((item) => (
            <TableRow key={item.id}>
              <TableCell>
                {new Date(item.created_at * 1000).toLocaleString(i18n.language)}
              </TableCell>
              <TableCell>
                {formatMinor(item.amount_minor, i18n.language)}
              </TableCell>
              <TableCell>
                {formatMinor(item.fee_minor, i18n.language)}
              </TableCell>
              <TableCell>
                {formatMinor(item.net_minor, i18n.language)}
              </TableCell>
              <TableCell>
                <Badge variant='outline'>
                  {withdrawalStatusLabel(item.status, t)}
                </Badge>
                <WithdrawalTimeline withdrawal={item} />
              </TableCell>
              <TableCell>{item.review_note || '—'}</TableCell>
              <TableCell>{item.payment_ref || '—'}</TableCell>
            </TableRow>
          ))}
          {withdrawals.data.length === 0 && (
            <TableRow>
              <TableCell colSpan={7}>{t('No data available')}</TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  )
}

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
import { ArrowRightLeft } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

import { getAvailableRebates, requestRebateTransfer } from '../../api'
import { requireInvitationData } from '../../api/result'
import { formatRebateAmount, getInvitationErrorMessage } from '../../format'

export const RebateTransfer = () => {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const availableQuery = useQuery({
    queryKey: ['kkai', 'invitations', 'available-rebates'],
    queryFn: async () =>
      requireInvitationData(
        await getAvailableRebates(),
        'Failed to load available rebates'
      ),
  })
  const transferMutation = useMutation({
    mutationFn: async () => {
      if (!availableQuery.data) throw new Error('No rebates available')
      return requireInvitationData(
        await requestRebateTransfer({
          amount: availableQuery.data.amount,
          rebateRecordIds: availableQuery.data.recordIds,
        }),
        'Failed to submit rebate request'
      )
    },
    onSuccess: async () => {
      setConfirmOpen(false)
      toast.success(t('Rebate request submitted'))
      await queryClient.invalidateQueries({
        queryKey: ['kkai', 'invitations'],
      })
    },
    onError: (error) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to submit rebate request'))
      )
    },
  })
  const available = availableQuery.data
  const canTransfer =
    Boolean(available && available.amount > 0 && available.recordIds.length) &&
    !transferMutation.isPending
  let content = <Skeleton className='h-24 w-full' />

  if (availableQuery.isError) {
    content = (
      <ErrorState
        title={t('Failed to load available rebates')}
        description={availableQuery.error.message}
        onRetry={() => void availableQuery.refetch()}
      />
    )
  } else if (!availableQuery.isPending) {
    content = (
      <div className='flex flex-col justify-between gap-4 sm:flex-row sm:items-end'>
        <div>
          <div className='text-muted-foreground text-sm'>
            {t('Available Rebate')}
          </div>
          <div className='mt-1 text-3xl font-semibold tabular-nums'>
            {formatRebateAmount(available?.amount ?? 0)}
          </div>
          <div className='text-muted-foreground mt-1 text-sm tabular-nums'>
            {t('{{count}} eligible records', {
              count: available?.recordIds.length ?? 0,
            })}
          </div>
        </div>
        <Button disabled={!canTransfer} onClick={() => setConfirmOpen(true)}>
          <ArrowRightLeft aria-hidden='true' />
          {t('Transfer to Balance')}
        </Button>
      </div>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Transfer to Balance')}</CardTitle>
      </CardHeader>
      <CardContent>{content}</CardContent>
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Confirm rebate request')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('Submit {{amount}} to your account balance?', {
                amount: formatRebateAmount(available?.amount ?? 0),
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              disabled={transferMutation.isPending}
              onClick={(event) => {
                event.preventDefault()
                transferMutation.mutate()
              }}
            >
              {transferMutation.isPending ? t('Submitting...') : t('Confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}

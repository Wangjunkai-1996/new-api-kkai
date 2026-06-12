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
import { useState } from 'react'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getUserRebateDetails } from '../../api'
import { formatRebateAmount } from '../../lib/format'
import type { OrderType, RebateDisplayStatus, RebateRecord } from '../../types'

const formSchema = z.object({
  userId: z
    .string()
    .min(1, 'User ID is required')
    .refine((val) => !isNaN(Number(val)) && Number(val) > 0, {
      message: 'User ID must be a positive number',
    }),
})

type FormData = z.infer<typeof formSchema>

export function UserRebateDetails() {
  const { t } = useTranslation()
  const [records, setRecords] = useState<RebateRecord[]>([])

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormData>({
    resolver: zodResolver(formSchema),
    defaultValues: { userId: '' },
  })

  // 查询用户返利详情
  const queryMutation = useMutation({
    mutationFn: (userId: number) =>
      getUserRebateDetails(userId, { page: 1, pageSize: 1000 }),
    onSuccess: (response) => {
      setRecords(response.data?.items ?? [])
    },
    onError: () => {
      setRecords([])
    },
  })

  const onSubmit = (data: FormData) => {
    const userId = Number(data.userId)
    queryMutation.mutate(userId)
  }

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleString()
  }

  const getStatusBadge = (record: RebateRecord) => {
    const status: RebateDisplayStatus =
      record.displayStatus ??
      (record.status === 'pending' ? 'claimable' : 'paid')
    const labels: Record<RebateDisplayStatus, string> = {
      estimated: t('Estimated Rebate'),
      claimable: t('Claimable Rebate'),
      paid: t('Paid Rebate'),
      waiting_unlock: t(
        'Pending rebate (waiting for first top-up/subscription to unlock)'
      ),
    }
    const classes: Record<RebateDisplayStatus, string> = {
      estimated: 'bg-amber-500/10 text-amber-700 dark:text-amber-400',
      claimable: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
      paid: 'bg-gray-500/10 text-gray-700 dark:text-gray-400',
      waiting_unlock: 'bg-blue-500/10 text-blue-700 dark:text-blue-400',
    }
    return (
      <Badge
        variant='outline'
        className={`h-auto max-w-[14rem] justify-start py-1 text-left leading-tight whitespace-normal ${classes[status]}`}
      >
        {labels[status]}
      </Badge>
    )
  }

  const isInvitationSignupReward = (type: OrderType) =>
    type === 'invite_inviter' || type === 'invite_invitee'

  const formatOrderType = (type: OrderType) => {
    const types: Record<OrderType, string> = {
      topup: t('Top-up'),
      subscription: t('Subscription'),
      invite_inviter: t('Invitation Reward'),
      invite_invitee: t('New User Invitation Reward'),
      other: t('Other'),
    }
    return types[type] || type
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('User Rebate Details')}</CardTitle>
      </CardHeader>
      <CardContent className='space-y-4'>
        <form onSubmit={handleSubmit(onSubmit)} className='flex gap-2'>
          <div className='flex-1 space-y-2'>
            <Label htmlFor='userId' className='sr-only'>
              {t('User ID')}
            </Label>
            <Input
              id='userId'
              type='text'
              placeholder={t('Enter user ID')}
              {...register('userId')}
            />
            {errors.userId && (
              <p className='text-destructive text-sm'>
                {errors.userId.message}
              </p>
            )}
          </div>
          <Button type='submit' disabled={queryMutation.isPending}>
            <Search className='mr-2 size-4' />
            {queryMutation.isPending ? t('Querying...') : t('Query')}
          </Button>
        </form>

        {queryMutation.isSuccess && (
          <div className='overflow-x-auto'>
            {records.length === 0 ? (
              <div className='flex items-center justify-center py-8'>
                <div className='text-muted-foreground'>
                  {t('No rebate records found for this user')}
                </div>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Order Type')}</TableHead>
                    <TableHead>{t('Order Amount')}</TableHead>
                    <TableHead>{t('Rebate Amount')}</TableHead>
                    <TableHead>{t('Rebate Rate')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead>{t('Created At')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {records.map((record) => (
                    <TableRow key={record.id}>
                      <TableCell>{formatOrderType(record.orderType)}</TableCell>
                      <TableCell>
                        {isInvitationSignupReward(record.orderType)
                          ? '-'
                          : formatRebateAmount(record.orderAmount)}
                      </TableCell>
                      <TableCell className='font-medium'>
                        {formatRebateAmount(record.rebateAmount)}
                      </TableCell>
                      <TableCell>
                        {isInvitationSignupReward(record.orderType)
                          ? '-'
                          : record.rebateRatio == null
                            ? t('Not configured')
                            : `${(record.rebateRatio * 100).toFixed(2)}%`}
                      </TableCell>
                      <TableCell>{getStatusBadge(record)}</TableCell>
                      <TableCell className='text-muted-foreground'>
                        {formatDate(record.createdAt)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </div>
        )}

        {queryMutation.isError && (
          <div className='bg-destructive/10 text-destructive rounded-md p-4 text-sm'>
            {t(
              'Failed to query user rebate details. Please check the user ID and try again.'
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

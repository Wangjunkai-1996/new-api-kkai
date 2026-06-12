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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Gift, RotateCcw, UserRoundPlus } from 'lucide-react'
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  generateAdminInvitationInviteeReward,
  generateAdminInvitationInviterReward,
  getAdminInvitationRegistrations,
  revokeAdminInvitationInviteeReward,
  revokeAdminInvitationInviterReward,
} from '../../api'
import { getInvitationErrorMessage } from '../../lib/error'
import { formatRebateAmount } from '../../lib/format'
import type { AdminInvitationRegistration } from '../../types'

const DEFAULT_PAGE_SIZE = 20

function formatUser(id: number, name?: string | null): string {
  return name ? `${name} (#${id})` : `#${id}`
}

function formatDateTime(value?: string | null): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString()
}

function rewardStatusLabel(
  t: ReturnType<typeof useTranslation>['t'],
  status?: string | null
): string {
  const labels: Record<string, string> = {
    pending: t('Pending'),
    requested: t('Requested'),
    approved: t('Approved'),
    completed: t('Completed'),
    rejected: t('Rejected'),
    closed: t('Closed'),
  }
  return status ? (labels[status] ?? status) : t('Not generated')
}

function rewardBadge(
  t: ReturnType<typeof useTranslation>['t'],
  generated: boolean,
  status?: string | null
) {
  if (!generated) {
    return <Badge variant='secondary'>{t('Not generated')}</Badge>
  }

  return <Badge>{rewardStatusLabel(t, status)}</Badge>
}

function groupNameLabel(group: string): string {
  return group === '__all__' ? 'All Groups' : group || '-'
}

function canRevokeReward(generated: boolean, status?: string | null): boolean {
  return generated && status !== 'completed'
}

export function InvitationRegistrationsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [pageSize] = useState(DEFAULT_PAGE_SIZE)

  const queryKey = ['adminInvitationRegistrations', page, pageSize]
  const { data, isLoading } = useQuery({
    queryKey,
    queryFn: async () => {
      const response = await getAdminInvitationRegistrations({
        page,
        pageSize,
      })
      return response.data
    },
  })

  const invalidateList = () => {
    queryClient.invalidateQueries({
      queryKey: ['adminInvitationRegistrations'],
    })
    queryClient.invalidateQueries({ queryKey: ['rebateStats'] })
    queryClient.invalidateQueries({ queryKey: ['adminRebateRecords'] })
  }

  const inviterRewardMutation = useMutation({
    mutationFn: generateAdminInvitationInviterReward,
    onSuccess: (response) => {
      toast.success(
        response.data?.generated
          ? t('Inviter reward generated')
          : t('Inviter reward already exists')
      )
      invalidateList()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to generate inviter reward'))
      )
    },
  })

  const inviteeRewardMutation = useMutation({
    mutationFn: generateAdminInvitationInviteeReward,
    onSuccess: (response) => {
      toast.success(
        response.data?.generated
          ? t('Invitee reward generated')
          : t('Invitee reward already exists')
      )
      invalidateList()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to generate invitee reward'))
      )
    },
  })

  const revokeInviterRewardMutation = useMutation({
    mutationFn: revokeAdminInvitationInviterReward,
    onSuccess: (response) => {
      toast.success(
        response.data?.revoked
          ? t('Inviter reward revoked')
          : t('Inviter reward already revoked')
      )
      invalidateList()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to revoke inviter reward'))
      )
    },
  })

  const revokeInviteeRewardMutation = useMutation({
    mutationFn: revokeAdminInvitationInviteeReward,
    onSuccess: (response) => {
      toast.success(
        response.data?.revoked
          ? t('Invitee reward revoked')
          : t('Invitee reward already revoked')
      )
      invalidateList()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to revoke invitee reward'))
      )
    },
  })

  const total = data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pageSize))
  const items = data?.items ?? []
  const mutating =
    inviterRewardMutation.isPending ||
    inviteeRewardMutation.isPending ||
    revokeInviterRewardMutation.isPending ||
    revokeInviteeRewardMutation.isPending

  const renderRewardSummary = (
    generated: boolean,
    amount: number,
    status?: string | null
  ) => (
    <div className='flex min-w-40 flex-col gap-1'>
      <div>{amount > 0 ? formatRebateAmount(amount) : '-'}</div>
      <div>{rewardBadge(t, generated, status)}</div>
    </div>
  )

  const actionCellClass =
    'bg-popover sticky right-0 z-20 w-64 min-w-64 border-l shadow-[-8px_0_8px_-8px_rgb(0_0_0_/_0.2)]'

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Invitation Registrations')}</CardTitle>
        <CardDescription>
          {t('View inviter and invited user registration relationships')}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className='text-muted-foreground flex items-center justify-center py-8'>
            {t('Loading...')}
          </div>
        ) : (
          <>
            <div className='overflow-x-auto rounded-md border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className='min-w-44'>{t('Inviter')}</TableHead>
                    <TableHead className='min-w-44'>
                      {t('Invited User')}
                    </TableHead>
                    <TableHead className='min-w-32'>
                      {t('User Group')}
                    </TableHead>
                    <TableHead className='min-w-44'>
                      {t('Invited At')}
                    </TableHead>
                    <TableHead className='min-w-40'>
                      {t('Total Rewards')}
                    </TableHead>
                    <TableHead className='min-w-44'>
                      {t('Inviter Reward')}
                    </TableHead>
                    <TableHead className='min-w-44'>
                      {t('Invitee Reward')}
                    </TableHead>
                    <TableHead className={actionCellClass}>
                      {t('Actions')}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.length > 0 ? (
                    items.map((record: AdminInvitationRegistration) => (
                      <TableRow key={record.id}>
                        <TableCell>
                          {formatUser(record.inviterId, record.inviterName)}
                        </TableCell>
                        <TableCell>
                          {formatUser(record.inviteeId, record.inviteeName)}
                        </TableCell>
                        <TableCell>
                          <Badge variant='secondary'>
                            {t(groupNameLabel(record.userGroup))}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          {formatDateTime(record.invitedAt)}
                        </TableCell>
                        <TableCell>
                          {formatRebateAmount(record.totalRewardAmount)}
                        </TableCell>
                        <TableCell>
                          {renderRewardSummary(
                            record.inviterRewardGenerated,
                            record.inviterRewardAmount,
                            record.inviterRewardStatus
                          )}
                        </TableCell>
                        <TableCell>
                          {renderRewardSummary(
                            record.inviteeRewardGenerated,
                            record.inviteeRewardAmount,
                            record.inviteeRewardStatus
                          )}
                        </TableCell>
                        <TableCell className={actionCellClass}>
                          <div className='flex flex-col gap-2 xl:flex-row'>
                            {record.inviterRewardGenerated ? (
                              <Button
                                type='button'
                                variant='outline'
                                size='sm'
                                className='justify-start whitespace-nowrap'
                                disabled={
                                  mutating ||
                                  !canRevokeReward(
                                    record.inviterRewardGenerated,
                                    record.inviterRewardStatus
                                  )
                                }
                                onClick={() =>
                                  revokeInviterRewardMutation.mutate(record.id)
                                }
                              >
                                <RotateCcw className='size-4' />
                                {t('Revoke Inviter Reward')}
                              </Button>
                            ) : (
                              <Button
                                type='button'
                                variant='outline'
                                size='sm'
                                className='justify-start whitespace-nowrap'
                                disabled={mutating}
                                onClick={() =>
                                  inviterRewardMutation.mutate(record.id)
                                }
                              >
                                <Gift className='size-4' />
                                {t('Generate Inviter Reward')}
                              </Button>
                            )}
                            {record.inviteeRewardGenerated ? (
                              <Button
                                type='button'
                                variant='outline'
                                size='sm'
                                className='justify-start whitespace-nowrap'
                                disabled={
                                  mutating ||
                                  !canRevokeReward(
                                    record.inviteeRewardGenerated,
                                    record.inviteeRewardStatus
                                  )
                                }
                                onClick={() =>
                                  revokeInviteeRewardMutation.mutate(record.id)
                                }
                              >
                                <RotateCcw className='size-4' />
                                {t('Revoke Invitee Reward')}
                              </Button>
                            ) : (
                              <Button
                                type='button'
                                variant='outline'
                                size='sm'
                                className='justify-start whitespace-nowrap'
                                disabled={mutating}
                                onClick={() =>
                                  inviteeRewardMutation.mutate(record.id)
                                }
                              >
                                <UserRoundPlus className='size-4' />
                                {t('Generate Invitee Reward')}
                              </Button>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    ))
                  ) : (
                    <TableRow>
                      <TableCell colSpan={8} className='h-24 text-center'>
                        {t('No invitation registrations found')}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>

            {total > 0 && (
              <div className='flex flex-col gap-3 px-2 py-4 sm:flex-row sm:items-center sm:justify-between'>
                <div className='text-muted-foreground text-sm'>
                  {t('Showing {{from}} to {{to}} of {{total}} records', {
                    from: (page - 1) * pageSize + 1,
                    to: Math.min(page * pageSize, total),
                    total,
                  })}
                </div>
                <div className='flex items-center gap-2'>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={page <= 1}
                    onClick={() =>
                      setPage((current) => Math.max(1, current - 1))
                    }
                  >
                    {t('Previous')}
                  </Button>
                  <div className='min-w-24 text-center text-sm'>
                    {t('Page {{current}} of {{total}}', {
                      current: page,
                      total: pageCount,
                    })}
                  </div>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={page >= pageCount}
                    onClick={() =>
                      setPage((current) => Math.min(pageCount, current + 1))
                    }
                  >
                    {t('Next')}
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}

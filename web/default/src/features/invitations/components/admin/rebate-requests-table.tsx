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
import { CheckCircle, XCircle, Check, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatRebateAmount } from '../../lib/format'
import type { RebateRequestAdmin } from '../../types'
import { RebateActionDialog } from './rebate-action-dialog'

interface RebateRequestsTableProps {
  requests: RebateRequestAdmin[]
  loading: boolean
}

type ActionType = 'approve' | 'reject' | 'reset' | 'complete' | 'undoComplete'

export function RebateRequestsTable({
  requests,
  loading,
}: RebateRequestsTableProps) {
  const { t } = useTranslation()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [selectedRequest, setSelectedRequest] =
    useState<RebateRequestAdmin | null>(null)
  const [actionType, setActionType] = useState<ActionType>('approve')

  const handleAction = (request: RebateRequestAdmin, type: ActionType) => {
    setSelectedRequest(request)
    setActionType(type)
    setDialogOpen(true)
  }

  const formatUser = (request: RebateRequestAdmin) => {
    return request.userName
      ? `${request.userName} (#${request.userId})`
      : `#${request.userId}`
  }

  const formatDate = (dateString?: string) => {
    if (!dateString) return '-'
    return new Date(dateString).toLocaleString()
  }

  const getStatusBadge = (status: string) => {
    const variants: Record<
      string,
      'default' | 'secondary' | 'destructive' | 'outline'
    > = {
      pending: 'default',
      approved: 'secondary',
      rejected: 'destructive',
      completed: 'outline',
    }
    return (
      <Badge variant={variants[status] || 'default'}>
        {t(status.charAt(0).toUpperCase() + status.slice(1))}
      </Badge>
    )
  }

  if (loading) {
    return (
      <div className='flex items-center justify-center py-8'>
        <div className='text-muted-foreground'>{t('Loading...')}</div>
      </div>
    )
  }

  if (requests.length === 0) {
    return (
      <div className='flex items-center justify-center py-8'>
        <div className='text-muted-foreground'>
          {t('No rebate requests found')}
        </div>
      </div>
    )
  }

  return (
    <>
      <div className='overflow-x-auto'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Rebate User')}</TableHead>
              <TableHead>{t('Rebate Amount')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
              <TableHead>{t('Created At')}</TableHead>
              <TableHead>{t('Approved At')}</TableHead>
              <TableHead>{t('Completed At')}</TableHead>
              <TableHead>{t('Review Note')}</TableHead>
              <TableHead className='text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {requests.map((request) => (
              <TableRow key={request.id}>
                <TableCell className='font-mono'>
                  {formatUser(request)}
                </TableCell>
                <TableCell className='font-medium'>
                  {formatRebateAmount(request.amount)}
                </TableCell>
                <TableCell>{getStatusBadge(request.status)}</TableCell>
                <TableCell className='text-muted-foreground'>
                  {formatDate(request.createdAt)}
                </TableCell>
                <TableCell className='text-muted-foreground'>
                  {formatDate(request.approvedAt)}
                </TableCell>
                <TableCell className='text-muted-foreground'>
                  {formatDate(request.completedAt)}
                </TableCell>
                <TableCell className='text-muted-foreground'>
                  {request.reviewNote || request.rejectReason || '-'}
                </TableCell>
                <TableCell className='text-right'>
                  <div className='flex justify-end gap-2'>
                    {request.status !== 'completed' && (
                      <>
                        <Button
                          variant='ghost'
                          size='sm'
                          onClick={() => handleAction(request, 'approve')}
                          title={
                            request.status === 'approved'
                              ? t('Edit Approval')
                              : t('Approve')
                          }
                        >
                          <CheckCircle className='size-4 text-green-600' />
                        </Button>
                        <Button
                          variant='ghost'
                          size='sm'
                          onClick={() => handleAction(request, 'reject')}
                          title={
                            request.status === 'rejected'
                              ? t('Edit Rejection')
                              : t('Reject')
                          }
                        >
                          <XCircle className='size-4 text-red-600' />
                        </Button>
                        {request.status !== 'pending' && (
                          <Button
                            variant='ghost'
                            size='sm'
                            onClick={() => handleAction(request, 'reset')}
                            title={t('Reset Review')}
                          >
                            <RotateCcw className='size-4 text-amber-600' />
                          </Button>
                        )}
                      </>
                    )}
                    {request.status === 'approved' && (
                      <Button
                        variant='ghost'
                        size='sm'
                        onClick={() => handleAction(request, 'complete')}
                        title={t('Complete')}
                      >
                        <Check className='size-4 text-blue-600' />
                      </Button>
                    )}
                    {request.status === 'completed' && (
                      <Button
                        variant='ghost'
                        size='sm'
                        onClick={() => handleAction(request, 'undoComplete')}
                        title={t('Withdraw Completion')}
                      >
                        <RotateCcw className='size-4 text-amber-600' />
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <RebateActionDialog
        open={dialogOpen}
        onClose={() => {
          setDialogOpen(false)
          setSelectedRequest(null)
        }}
        request={selectedRequest}
        actionType={actionType}
      />
    </>
  )
}

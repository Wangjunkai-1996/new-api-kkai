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
import { useEffect, useMemo, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
  type PaginationState,
} from '@tanstack/react-table'
import { format } from 'date-fns'
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
import { Checkbox } from '@/components/ui/checkbox'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  getAvailableRebates,
  getMyRebateRequests,
  getRebateRecords,
  requestRebate,
} from '../api'
import { getInvitationErrorMessage } from '../lib/error'
import { formatRebateAmount } from '../lib/format'
import type {
  OrderType,
  RebateRecord,
  RebateRequest,
  RebateRequestStatus,
} from '../types'

const columnHelper = createColumnHelper<RebateRequest>()

// 状态徽章颜色映射
const STATUS_COLORS: Record<RebateRequestStatus, string> = {
  pending: 'bg-yellow-500/10 text-yellow-700 dark:text-yellow-400',
  approved: 'bg-green-500/10 text-green-700 dark:text-green-400',
  rejected: 'bg-red-500/10 text-red-700 dark:text-red-400',
  completed: 'bg-gray-500/10 text-gray-700 dark:text-gray-400',
}

function isInvitationSignupReward(orderType: OrderType): boolean {
  return orderType === 'invite_inviter' || orderType === 'invite_invitee'
}

export function RebateManagement() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [selectedRecordIds, setSelectedRecordIds] = useState<number[]>([])
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  })

  // 获取可申请到余额的返利金额
  const { data: availableData } = useQuery({
    queryKey: ['availableRebates'],
    queryFn: async () => {
      const response = await getAvailableRebates()
      return response.data
    },
  })

  const { data: pendingRecordsData, isLoading: claimableLoading } = useQuery({
    queryKey: ['claimableRebateRecords', availableData?.recordIds ?? []],
    enabled: Boolean(availableData?.recordIds?.length),
    queryFn: async () => {
      const response = await getRebateRecords({
        page: 1,
        pageSize: 1000,
        status: 'pending',
      })
      return response.data
    },
  })

  // 获取返利到余额申请列表
  const { data: requestsData, isLoading } = useQuery({
    queryKey: ['rebateRequests', pagination.pageIndex + 1, pagination.pageSize],
    queryFn: async () => {
      const response = await getMyRebateRequests({
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
      })
      return response.data
    },
  })

  const availableRecordIds = useMemo(
    () => new Set(availableData?.recordIds ?? []),
    [availableData?.recordIds]
  )

  const claimableRecords = useMemo(
    () =>
      (pendingRecordsData?.items ?? []).filter((record) =>
        availableRecordIds.has(record.id)
      ),
    [availableRecordIds, pendingRecordsData?.items]
  )

  useEffect(() => {
    setSelectedRecordIds(availableData?.recordIds ?? [])
  }, [availableData?.recordIds])

  const selectedRecordIdSet = useMemo(
    () => new Set(selectedRecordIds),
    [selectedRecordIds]
  )

  const selectedRecords = useMemo(
    () =>
      claimableRecords.filter((record) => selectedRecordIdSet.has(record.id)),
    [claimableRecords, selectedRecordIdSet]
  )

  const selectedAmount = selectedRecords.reduce(
    (sum, record) => sum + record.rebateAmount,
    0
  )

  const allClaimableSelected =
    claimableRecords.length > 0 &&
    selectedRecords.length === claimableRecords.length
  const someClaimableSelected =
    selectedRecords.length > 0 &&
    selectedRecords.length < claimableRecords.length

  const toggleRecord = (recordId: number, checked: boolean) => {
    setSelectedRecordIds((current) => {
      if (checked) return Array.from(new Set([...current, recordId]))
      return current.filter((id) => id !== recordId)
    })
  }

  const toggleAllRecords = (checked: boolean) => {
    setSelectedRecordIds(
      checked ? claimableRecords.map((record) => record.id) : []
    )
  }

  // 返利到余额申请 mutation
  const rebateRequestMutation = useMutation({
    mutationFn: async () => {
      if (selectedRecordIds.length === 0) {
        throw new Error(t('No available rebates to apply to balance'))
      }
      return requestRebate({
        amount: selectedAmount,
        rebateRecordIds: selectedRecordIds,
      })
    },
    onSuccess: () => {
      toast.success(t('Rebate balance request submitted successfully'))
      setSelectedRecordIds([])
      queryClient.invalidateQueries({ queryKey: ['availableRebates'] })
      queryClient.invalidateQueries({ queryKey: ['claimableRebateRecords'] })
      queryClient.invalidateQueries({ queryKey: ['rebateRequests'] })
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(
          error,
          t('Failed to submit rebate balance request')
        )
      )
    },
  })

  // 提交返利到余额申请
  const submitSelectedRebates = () => {
    rebateRequestMutation.mutate()
  }

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

  const formatDate = (value?: string) => {
    if (!value) return '-'
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? '-' : format(date, 'yyyy-MM-dd HH:mm')
  }

  // 定义表格列
  const columns = useMemo(
    () => [
      columnHelper.accessor('amount', {
        header: () => t('Rebate Amount'),
        cell: (info) => formatRebateAmount(info.getValue()),
      }),
      columnHelper.accessor('status', {
        header: () => t('Status'),
        cell: (info) => {
          const status = info.getValue()
          const statusLabels: Record<RebateRequestStatus, string> = {
            pending: t('Pending'),
            approved: t('Approved'),
            rejected: t('Rejected'),
            completed: t('Completed'),
          }
          return (
            <Badge variant='outline' className={STATUS_COLORS[status]}>
              {statusLabels[status]}
            </Badge>
          )
        },
      }),
      columnHelper.accessor('createdAt', {
        header: () => t('Created At'),
        cell: (info) => format(new Date(info.getValue()), 'yyyy-MM-dd HH:mm'),
      }),
      columnHelper.accessor('approvedAt', {
        header: () => t('Approved At'),
        cell: (info) => {
          const value = info.getValue()
          return value ? format(new Date(value), 'yyyy-MM-dd HH:mm') : '-'
        },
      }),
      columnHelper.accessor('completedAt', {
        header: () => t('Completed At'),
        cell: (info) => {
          const value = info.getValue()
          return value ? format(new Date(value), 'yyyy-MM-dd HH:mm') : '-'
        },
      }),
      columnHelper.accessor('rejectReason', {
        header: () => t('Reject Reason'),
        cell: (info) => info.getValue() || '-',
      }),
    ],
    [t]
  )

  // 创建表格实例
  const table = useReactTable({
    data: requestsData?.items ?? [],
    columns,
    pageCount: requestsData
      ? Math.ceil(requestsData.total / pagination.pageSize)
      : 0,
    state: {
      pagination,
    },
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
  })

  const availableAmount = availableData?.amount ?? 0

  return (
    <div className='space-y-6'>
      {/* 返利到余额申请表单 */}
      <Card>
        <CardHeader>
          <CardTitle>{t('Apply Rebate to Balance')}</CardTitle>
          <CardDescription>
            {t('Apply completed rebates to your account balance')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className='space-y-4'>
            <div className='grid gap-3 sm:grid-cols-3'>
              <div>
                <div className='text-muted-foreground text-sm'>
                  {t('Available Rebate Amount')}
                </div>
                <div className='text-2xl font-bold'>
                  {formatRebateAmount(availableAmount)}
                </div>
              </div>
              <div>
                <div className='text-muted-foreground text-sm'>
                  {t('Selected Rebate Amount')}
                </div>
                <div className='text-2xl font-bold'>
                  {formatRebateAmount(selectedAmount)}
                </div>
              </div>
              <div>
                <div className='text-muted-foreground text-sm'>
                  {t('Selected Records')}
                </div>
                <div className='text-2xl font-bold'>
                  {selectedRecords.length}/{claimableRecords.length}
                </div>
              </div>
            </div>

            <div className='overflow-x-auto rounded-md border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className='w-12'>
                      <Checkbox
                        checked={allClaimableSelected}
                        indeterminate={someClaimableSelected}
                        onCheckedChange={(value) => toggleAllRecords(!!value)}
                        aria-label={t('Select all')}
                      />
                    </TableHead>
                    <TableHead>{t('Order Type')}</TableHead>
                    <TableHead>{t('Order Amount')}</TableHead>
                    <TableHead>{t('Rebate Amount')}</TableHead>
                    <TableHead>{t('Created At')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {claimableLoading ? (
                    <TableRow>
                      <TableCell colSpan={5} className='h-20 text-center'>
                        {t('Loading...')}
                      </TableCell>
                    </TableRow>
                  ) : claimableRecords.length > 0 ? (
                    claimableRecords.map((record: RebateRecord) => (
                      <TableRow key={record.id}>
                        <TableCell>
                          <Checkbox
                            checked={selectedRecordIdSet.has(record.id)}
                            onCheckedChange={(value) =>
                              toggleRecord(record.id, !!value)
                            }
                            aria-label={t('Select row')}
                          />
                        </TableCell>
                        <TableCell>
                          {formatOrderType(record.orderType)}
                        </TableCell>
                        <TableCell>
                          {isInvitationSignupReward(record.orderType)
                            ? '-'
                            : formatRebateAmount(record.orderAmount)}
                        </TableCell>
                        <TableCell className='font-medium'>
                          {formatRebateAmount(record.rebateAmount)}
                        </TableCell>
                        <TableCell className='text-muted-foreground'>
                          {formatDate(record.createdAt)}
                        </TableCell>
                      </TableRow>
                    ))
                  ) : (
                    <TableRow>
                      <TableCell colSpan={5} className='h-20 text-center'>
                        {t('No available rebates to apply to balance')}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>

            <div className='flex justify-end'>
              <Button
                type='button'
                disabled={
                  rebateRequestMutation.isPending ||
                  selectedRecords.length === 0 ||
                  selectedAmount <= 0
                }
                onClick={submitSelectedRebates}
              >
                {rebateRequestMutation.isPending
                  ? t('Submitting...')
                  : t('Apply Selected to Balance')}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* 返利到余额申请列表 */}
      <Card>
        <CardHeader>
          <CardTitle>{t('Rebate Balance Requests')}</CardTitle>
          <CardDescription>
            {t('View your rebate-to-balance request history')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className='flex items-center justify-center py-8'>
              <div className='text-muted-foreground'>{t('Loading...')}</div>
            </div>
          ) : (
            <>
              <div className='rounded-md border'>
                <Table>
                  <TableHeader>
                    {table.getHeaderGroups().map((headerGroup) => (
                      <TableRow key={headerGroup.id}>
                        {headerGroup.headers.map((header) => (
                          <TableHead key={header.id}>
                            {header.isPlaceholder
                              ? null
                              : flexRender(
                                  header.column.columnDef.header,
                                  header.getContext()
                                )}
                          </TableHead>
                        ))}
                      </TableRow>
                    ))}
                  </TableHeader>
                  <TableBody>
                    {table.getRowModel().rows.length > 0 ? (
                      table.getRowModel().rows.map((row) => (
                        <TableRow key={row.id}>
                          {row.getVisibleCells().map((cell) => (
                            <TableCell key={cell.id}>
                              {flexRender(
                                cell.column.columnDef.cell,
                                cell.getContext()
                              )}
                            </TableCell>
                          ))}
                        </TableRow>
                      ))
                    ) : (
                      <TableRow>
                        <TableCell
                          colSpan={columns.length}
                          className='h-24 text-center'
                        >
                          {t('No rebate requests found')}
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </div>

              {/* 分页控制 */}
              {requestsData && requestsData.total > 0 && (
                <div className='flex items-center justify-between px-2 py-4'>
                  <div className='text-muted-foreground text-sm'>
                    {t('Showing {{from}} to {{to}} of {{total}} records', {
                      from: pagination.pageIndex * pagination.pageSize + 1,
                      to: Math.min(
                        (pagination.pageIndex + 1) * pagination.pageSize,
                        requestsData.total
                      ),
                      total: requestsData.total,
                    })}
                  </div>
                  <div className='flex items-center gap-2'>
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => table.previousPage()}
                      disabled={!table.getCanPreviousPage()}
                    >
                      {t('Previous')}
                    </Button>
                    <div className='text-sm'>
                      {t('Page {{current}} of {{total}}', {
                        current: pagination.pageIndex + 1,
                        total: table.getPageCount(),
                      })}
                    </div>
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => table.nextPage()}
                      disabled={!table.getCanNextPage()}
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
    </div>
  )
}

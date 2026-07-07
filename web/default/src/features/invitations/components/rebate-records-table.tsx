import { useQuery } from '@tanstack/react-query'
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
  type PaginationState,
} from '@tanstack/react-table'
import { format } from 'date-fns'
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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { getRebateRecords } from '../api'
import { formatRebateAmount } from '../lib/format'
import {
  deriveRebateDisplayStatus,
  getRebateDisplayStatusClass,
  getRebateDisplayStatusLabel,
} from '../lib/rebate-display-status'
import type { RebateRecord, RebateStatus, OrderType } from '../types'

const columnHelper = createColumnHelper<RebateRecord>()

function isInvitationSignupReward(orderType: OrderType): boolean {
  return orderType === 'invite_inviter' || orderType === 'invite_invitee'
}

export function RebateRecordsTable() {
  const { t } = useTranslation()
  const [statusFilter, setStatusFilter] = useState<RebateStatus | 'all'>('all')
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  })

  // 获取返利记录
  const { data, isLoading } = useQuery({
    queryKey: [
      'rebateRecords',
      pagination.pageIndex + 1,
      pagination.pageSize,
      statusFilter,
    ],
    queryFn: async () => {
      const params = {
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        ...(statusFilter !== 'all' && { status: statusFilter }),
      }
      const response = await getRebateRecords(params)
      return response.data
    },
  })

  // 定义表格列
  const columns = useMemo(
    () => [
      columnHelper.accessor('orderType', {
        header: () => t('Order Type'),
        cell: (info) => {
          const type = info.getValue() as OrderType
          const typeLabels: Record<OrderType, string> = {
            topup: t('Topup'),
            subscription: t('Subscription'),
            redemption: t('Voucher Rebate'),
            redemption_code: t('Voucher Rebate'),
            redeem: t('Voucher Rebate'),
            voucher: t('Voucher Rebate'),
            invite_inviter: t('Invitation Reward'),
            invite_invitee: t('New User Invitation Reward'),
            other: t('Other'),
          }
          return typeLabels[type] || type
        },
      }),
      columnHelper.accessor('orderAmount', {
        header: () => t('Order Amount'),
        cell: (info) =>
          isInvitationSignupReward(info.row.original.orderType)
            ? '-'
            : formatRebateAmount(info.getValue()),
      }),
      columnHelper.accessor('rebateAmount', {
        header: () => t('Rebate Amount'),
        cell: (info) => formatRebateAmount(info.getValue()),
      }),
      columnHelper.accessor('rebateRatio', {
        header: () => t('Rebate Ratio'),
        cell: (info) => {
          const ratio = info.getValue()
          if (isInvitationSignupReward(info.row.original.orderType)) {
            return '-'
          }
          return ratio == null
            ? t('Not configured')
            : `${(ratio * 100).toFixed(1)}%`
        },
      }),
      columnHelper.accessor('createdAt', {
        header: () => t('Created At'),
        cell: (info) => format(new Date(info.getValue()), 'yyyy-MM-dd HH:mm'),
      }),
      columnHelper.accessor('status', {
        header: () => t('Status'),
        cell: (info) => {
          const status =
            info.row.original.displayStatus ??
            deriveRebateDisplayStatus(info.getValue())
          return (
            <Badge
              variant='outline'
              className={`h-auto max-w-[14rem] justify-start py-1 text-left leading-tight whitespace-normal ${getRebateDisplayStatusClass(status)}`}
            >
              {getRebateDisplayStatusLabel(t, status)}
            </Badge>
          )
        },
      }),
    ],
    [t]
  )

  // 创建表格实例
  const table = useReactTable({
    data: data?.items ?? [],
    columns,
    pageCount: data ? Math.ceil(data.total / pagination.pageSize) : 0,
    state: {
      pagination,
    },
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
  })

  return (
    <Card>
      <CardHeader>
        <div className='flex items-center justify-between'>
          <div>
            <CardTitle>{t('Rebate Records')}</CardTitle>
            <CardDescription>{t('View your rebate history')}</CardDescription>
          </div>
          <Select
            value={statusFilter}
            onValueChange={(value) =>
              setStatusFilter(value as RebateStatus | 'all')
            }
          >
            <SelectTrigger className='w-[180px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All Status')}</SelectItem>
              <SelectItem value='pending'>{t('Pending')}</SelectItem>
              <SelectItem value='requested'>{t('Requested')}</SelectItem>
              <SelectItem value='approved'>{t('Approved')}</SelectItem>
              <SelectItem value='completed'>{t('Completed')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
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
                        {t('No records found')}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>

            {/* 分页控制 */}
            {data && data.total > 0 && (
              <div className='flex items-center justify-between px-2 py-4'>
                <div className='text-muted-foreground text-sm'>
                  {t('Showing {{from}} to {{to}} of {{total}} records', {
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
  )
}

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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
  type PaginationState,
  type RowSelectionState,
} from '@tanstack/react-table'
import {
  Ban,
  Edit,
  Loader2,
  LockOpen,
  RotateCcw,
  TimerOff,
  TimerReset,
} from 'lucide-react'
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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
import {
  closeAdminRebateOrderRecords,
  endAdminRebateOrderInitialization,
  getAdminRebateRecords,
  extendAdminRebateOrderInitialization,
  getAdminRebateOrderRecords,
  reopenAdminRebateOrderRecords,
  revokeAdminSignupRewardRecord,
  updateAdminRebateOrderRecords,
} from '../../api'
import { getInvitationErrorMessage } from '../../lib/error'
import { formatRebateAmount } from '../../lib/format'
import type {
  AdminRebateOrderRecord,
  AdminRebateOrderStatus,
  RebateRecord,
  RebateStatus,
} from '../../types'

type OrderTypeFilter = 'all' | 'topup' | 'subscription'
type DialogState =
  | { type: 'edit'; records: AdminRebateOrderRecord[] }
  | { type: 'close'; records: AdminRebateOrderRecord[] }
  | { type: 'reopen'; records: AdminRebateOrderRecord[] }
  | { type: 'end'; records: AdminRebateOrderRecord[] }
  | { type: 'extend'; records: AdminRebateOrderRecord[] }
  | null

const columnHelper = createColumnHelper<AdminRebateOrderRecord>()

const STATUS_COLORS: Record<AdminRebateOrderStatus, string> = {
  initializing: 'bg-sky-500/10 text-sky-700 dark:text-sky-400',
  estimated: 'bg-amber-500/10 text-amber-700 dark:text-amber-400',
  claimable: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
  paid: 'bg-slate-500/10 text-slate-700 dark:text-slate-400',
  closed: 'bg-red-500/10 text-red-700 dark:text-red-400',
}

const REBATE_STATUS_COLORS: Record<RebateStatus, string> = {
  pending: 'bg-amber-500/10 text-amber-700 dark:text-amber-400',
  requested: 'bg-sky-500/10 text-sky-700 dark:text-sky-400',
  approved: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
  completed: 'bg-slate-500/10 text-slate-700 dark:text-slate-400',
  rejected: 'bg-red-500/10 text-red-700 dark:text-red-400',
}

function formatRate(rate?: number | null): string {
  if (rate == null) return ''
  return `${(rate * 100).toFixed(2)}%`
}

function formatDateTime(value?: string | null): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString()
}

function formatUser(id: number, name?: string | null): string {
  return name ? `${name} (#${id})` : `#${id}`
}

function recordIds(records: AdminRebateOrderRecord[]): number[] {
  return records
    .map((record) => record.localRebateRecordId)
    .filter((id): id is number => typeof id === 'number' && id > 0)
}

function toDatetimeLocalValue(value?: string | null): string {
  const date = value ? new Date(value) : new Date()
  if (Number.isNaN(date.getTime())) return ''
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

function addDaysToDatetimeLocal(value: string, days: number): string {
  const date = value ? new Date(value) : new Date()
  date.setDate(date.getDate() + days)
  return toDatetimeLocalValue(date.toISOString())
}

function statusLabel(
  t: ReturnType<typeof useTranslation>['t'],
  status: AdminRebateOrderStatus
): string {
  const labels: Record<AdminRebateOrderStatus, string> = {
    initializing: t('Initializing'),
    estimated: t('Estimated Rebate'),
    claimable: t('Claimable Rebate'),
    paid: t('Paid Rebate'),
    closed: t('Closed'),
  }
  return labels[status]
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

function orderTypeLabel(
  t: ReturnType<typeof useTranslation>['t'],
  orderType: 'topup' | 'subscription'
): string {
  return orderType === 'topup' ? t('Topup') : t('Subscription')
}

function signupRewardTypeLabel(
  t: ReturnType<typeof useTranslation>['t'],
  orderType: string
): string {
  if (orderType === 'invite_inviter') return t('Inviter Reward')
  if (orderType === 'invite_invitee') return t('Invitee Reward')
  return orderType
}

function canRevokeSignupReward(record: RebateRecord): boolean {
  return (
    (record.orderType === 'invite_inviter' ||
      record.orderType === 'invite_invitee') &&
    record.status !== 'completed'
  )
}

function getRebateOrderTableColumnClass(columnId: string) {
  switch (columnId) {
    case 'select':
      return 'w-10 min-w-10'
    case 'actions':
      return 'bg-popover sticky right-0 z-20 w-48 min-w-48 border-l shadow-[-8px_0_8px_-8px_rgb(0_0_0_/_0.2)]'
    default:
      return undefined
  }
}

export function RebateOrderRecordsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [orderType, setOrderType] = useState<OrderTypeFilter>('all')
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({})
  const [dialog, setDialog] = useState<DialogState>(null)
  const [editAmount, setEditAmount] = useState('')
  const [editRatio, setEditRatio] = useState('')
  const [extendUntil, setExtendUntil] = useState('')

  const queryKey = [
    'adminRebateOrderRecords',
    orderType,
    pagination.pageIndex + 1,
    pagination.pageSize,
  ]

  const { data, isLoading } = useQuery({
    queryKey,
    queryFn: async () => {
      const response = await getAdminRebateOrderRecords({
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        ...(orderType !== 'all' && { orderType }),
      })
      return response.data
    },
  })

  const resetAfterMutation = () => {
    setDialog(null)
    setRowSelection({})
    queryClient.invalidateQueries({ queryKey: ['adminRebateOrderRecords'] })
    queryClient.invalidateQueries({ queryKey: ['rebateStats'] })
  }

  const updateMutation = useMutation({
    mutationFn: updateAdminRebateOrderRecords,
    onSuccess: () => {
      toast.success(t('Rebate records updated successfully'))
      resetAfterMutation()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to update rebate records'))
      )
    },
  })

  const closeMutation = useMutation({
    mutationFn: closeAdminRebateOrderRecords,
    onSuccess: () => {
      toast.success(t('Rebate records closed successfully'))
      resetAfterMutation()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to close rebate records'))
      )
    },
  })

  const reopenMutation = useMutation({
    mutationFn: reopenAdminRebateOrderRecords,
    onSuccess: () => {
      toast.success(t('Rebate records reopened successfully'))
      resetAfterMutation()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to reopen rebate records'))
      )
    },
  })

  const endInitializationMutation = useMutation({
    mutationFn: endAdminRebateOrderInitialization,
    onSuccess: () => {
      toast.success(t('Initialization ended successfully'))
      resetAfterMutation()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to end initialization'))
      )
    },
  })

  const extendInitializationMutation = useMutation({
    mutationFn: extendAdminRebateOrderInitialization,
    onSuccess: () => {
      toast.success(t('Initialization extended successfully'))
      resetAfterMutation()
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to extend initialization'))
      )
    },
  })

  const isMutating =
    updateMutation.isPending ||
    closeMutation.isPending ||
    reopenMutation.isPending ||
    endInitializationMutation.isPending ||
    extendInitializationMutation.isPending

  const openDialog = (
    type: NonNullable<DialogState>['type'],
    records: AdminRebateOrderRecord[]
  ) => {
    if (records.length === 0) return

    if (type === 'edit') {
      const record = records.length === 1 ? records[0] : null
      setEditAmount(record ? (record.rebateAmount / 100).toFixed(2) : '')
      setEditRatio(
        record?.rebateRatio != null ? (record.rebateRatio * 100).toFixed(2) : ''
      )
    }

    if (type === 'extend') {
      const record = records.length === 1 ? records[0] : null
      const baseValue = toDatetimeLocalValue(record?.initializationEndsAt)
      setExtendUntil(addDaysToDatetimeLocal(baseValue, 1))
    }

    setDialog({ type, records } as DialogState)
  }

  const submitEdit = () => {
    if (!dialog || dialog.type !== 'edit') return
    const ids = recordIds(dialog.records)
    const amountValue = editAmount.trim()
    const ratioValue = editRatio.trim()
    const payload: {
      recordIds: number[]
      rebateAmount?: number
      rebateRatio?: number
    } = { recordIds: ids }

    if (amountValue) {
      const amount = Number(amountValue)
      if (!Number.isFinite(amount) || amount < 0) {
        toast.error(t('Invalid rebate amount'))
        return
      }
      payload.rebateAmount = Math.round(amount * 100)
    }

    if (ratioValue) {
      const ratio = Number(ratioValue)
      if (!Number.isFinite(ratio) || ratio < 0 || ratio > 100) {
        toast.error(t('Invalid rebate rate'))
        return
      }
      payload.rebateRatio = ratio / 100
    }

    if (payload.rebateAmount == null && payload.rebateRatio == null) {
      toast.error(t('Enter rebate amount or rebate rate'))
      return
    }

    updateMutation.mutate(payload)
  }

  const submitClose = () => {
    if (!dialog || dialog.type !== 'close') return
    closeMutation.mutate({ recordIds: recordIds(dialog.records) })
  }

  const submitReopen = () => {
    if (!dialog || dialog.type !== 'reopen') return
    reopenMutation.mutate({ recordIds: recordIds(dialog.records) })
  }

  const submitEndInitialization = () => {
    if (!dialog || dialog.type !== 'end') return
    endInitializationMutation.mutate({ recordIds: recordIds(dialog.records) })
  }

  const submitExtendInitialization = () => {
    if (!dialog || dialog.type !== 'extend') return
    const date = new Date(extendUntil)
    if (!extendUntil || Number.isNaN(date.getTime())) {
      toast.error(t('Invalid initialization end time'))
      return
    }

    extendInitializationMutation.mutate({
      recordIds: recordIds(dialog.records),
      initializationEndsAt: Math.floor(date.getTime() / 1000),
    })
  }

  const columns = useMemo(
    () => [
      columnHelper.display({
        id: 'select',
        header: ({ table }) => (
          <Checkbox
            checked={table.getIsAllPageRowsSelected()}
            indeterminate={table.getIsSomePageRowsSelected()}
            onCheckedChange={(value) =>
              table.toggleAllPageRowsSelected(!!value)
            }
            aria-label={t('Select all')}
          />
        ),
        cell: ({ row }) => (
          <Checkbox
            checked={row.getIsSelected()}
            disabled={!row.getCanSelect()}
            onCheckedChange={(value) => row.toggleSelected(!!value)}
            aria-label={t('Select row')}
          />
        ),
      }),
      columnHelper.accessor('inviterId', {
        header: () => t('Inviter'),
        cell: (info) => (
          <span className='font-mono'>
            {formatUser(info.getValue(), info.row.original.inviterName)}
          </span>
        ),
      }),
      columnHelper.accessor('inviteeId', {
        header: () => t('Invited Customer'),
        cell: (info) => (
          <span className='font-mono'>
            {formatUser(info.getValue(), info.row.original.inviteeName)}
          </span>
        ),
      }),
      columnHelper.accessor('userGroup', {
        header: () => t('User Group'),
        cell: (info) => <Badge variant='secondary'>{info.getValue()}</Badge>,
      }),
      columnHelper.accessor('orderType', {
        header: () => t('Order Type'),
        cell: (info) => orderTypeLabel(t, info.getValue()),
      }),
      columnHelper.accessor('orderAmount', {
        header: () => t('Order Amount'),
        cell: (info) => formatRebateAmount(info.getValue()),
      }),
      columnHelper.accessor('rebateAmount', {
        header: () => t('Rebate Amount'),
        cell: (info) => {
          const record = info.row.original
          return (
            <div className='flex flex-col gap-1'>
              <span className='font-medium'>
                {record.ruleMissing && record.rebateAmount === 0
                  ? t('Not configured')
                  : formatRebateAmount(info.getValue())}
              </span>
              {record.adminAdjusted && (
                <Badge variant='outline' className='w-fit'>
                  {t('Manual')}
                </Badge>
              )}
            </div>
          )
        },
      }),
      columnHelper.accessor('rebateRatio', {
        header: () => t('Rebate Rate'),
        cell: (info) => (
          <div className='flex items-center gap-2'>
            <span>{formatRate(info.getValue()) || t('Not configured')}</span>
            {info.row.original.ruleMissing && (
              <Badge variant='outline' className='text-amber-700'>
                {t('Rule Missing')}
              </Badge>
            )}
          </div>
        ),
      }),
      columnHelper.accessor('status', {
        header: () => t('Status'),
        cell: (info) => {
          const status = info.getValue()
          return (
            <Badge variant='outline' className={STATUS_COLORS[status]}>
              {statusLabel(t, status)}
            </Badge>
          )
        },
      }),
      columnHelper.accessor('orderTime', {
        header: () => t('Order Time'),
        cell: (info) => (
          <span className='text-muted-foreground'>
            {formatDateTime(info.getValue())}
          </span>
        ),
      }),
      columnHelper.accessor('effectiveAt', {
        header: () => t('Effective At'),
        cell: (info) => (
          <span className='text-muted-foreground'>
            {formatDateTime(info.getValue())}
          </span>
        ),
      }),
      columnHelper.accessor('scanStartedAt', {
        header: () => t('Scan Time'),
        cell: (info) => (
          <span className='text-muted-foreground'>
            {formatDateTime(info.getValue())}
          </span>
        ),
      }),
      columnHelper.accessor('initializationEndsAt', {
        header: () => t('Initialization Ends'),
        cell: (info) => (
          <span className='text-muted-foreground'>
            {formatDateTime(info.getValue())}
          </span>
        ),
      }),
      columnHelper.display({
        id: 'actions',
        header: () => t('Actions'),
        cell: ({ row }) => {
          const record = row.original
          return (
            <div className='flex items-center gap-1'>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                title={t('Edit Rebate')}
                disabled={!record.canModify}
                onClick={() => openDialog('edit', [record])}
              >
                <Edit className='size-4' />
              </Button>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                title={t('End Initialization')}
                disabled={!record.canEndInitialization}
                onClick={() => openDialog('end', [record])}
              >
                <TimerOff className='size-4' />
              </Button>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                title={t('Extend Initialization')}
                disabled={!record.canExtendInitialization}
                onClick={() => openDialog('extend', [record])}
              >
                <TimerReset className='size-4' />
              </Button>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                title={t('Reopen Rebate')}
                disabled={!record.canReopen}
                onClick={() => openDialog('reopen', [record])}
              >
                <LockOpen className='size-4' />
              </Button>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                title={t('Close Rebate')}
                disabled={!record.canClose}
                onClick={() => openDialog('close', [record])}
              >
                <Ban className='size-4' />
              </Button>
            </div>
          )
        },
      }),
    ],
    [t]
  )

  const table = useReactTable({
    data: data?.items ?? [],
    columns,
    pageCount: data ? Math.ceil(data.total / pagination.pageSize) : 0,
    state: { pagination, rowSelection },
    onPaginationChange: setPagination,
    onRowSelectionChange: setRowSelection,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) =>
      String(row.localRebateRecordId ?? `${row.orderType}:${row.orderId}`),
    enableRowSelection: (row) => Boolean(row.original.localRebateRecordId),
    manualPagination: true,
  })

  const selectedRecords = table
    .getSelectedRowModel()
    .rows.map((row) => row.original)
  const hasSelected = selectedRecords.length > 0
  const selectedCanModify =
    hasSelected && selectedRecords.every((record) => record.canModify)
  const selectedCanClose =
    hasSelected && selectedRecords.every((record) => record.canClose)
  const selectedCanReopen =
    hasSelected && selectedRecords.every((record) => record.canReopen)
  const selectedCanEnd =
    hasSelected &&
    selectedRecords.every((record) => record.canEndInitialization)
  const selectedCanExtend =
    hasSelected &&
    selectedRecords.every((record) => record.canExtendInitialization)

  const handleOrderTypeChange = (value: OrderTypeFilter | null) => {
    if (!value) return
    setOrderType(value)
    setPagination((current) => ({ ...current, pageIndex: 0 }))
    setRowSelection({})
  }

  return (
    <div className='space-y-6'>
      <Card>
        <CardHeader>
          <CardTitle>{t('Rebate Records')}</CardTitle>
          <CardDescription>
            {t('View inviter and invited customer rebate order records')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div
            role='toolbar'
            aria-label={t('Actions')}
            className='bg-background/95 supports-[backdrop-filter]:bg-background/80 sticky top-[var(--app-header-height,3rem)] z-30 -mx-6 mb-4 border-y px-6 py-3 shadow-sm backdrop-blur'
          >
            <div className='flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between'>
              <div className='flex min-w-0 flex-col gap-1'>
                <div className='text-sm font-medium'>{t('Actions')}</div>
                <div className='text-muted-foreground min-h-5 text-sm'>
                  {hasSelected
                    ? t('Selected {{count}}', { count: selectedRecords.length })
                    : t('No records selected')}
                </div>
              </div>
              <div className='flex flex-col gap-2 xl:flex-row xl:items-center xl:justify-end'>
                <div className='grid grid-cols-1 gap-2 sm:grid-cols-2 xl:flex xl:flex-wrap xl:items-center xl:justify-end'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    className='justify-start whitespace-nowrap xl:justify-center'
                    disabled={!selectedCanModify}
                    onClick={() => openDialog('edit', selectedRecords)}
                  >
                    <Edit className='size-4' />
                    {t('Edit Selected')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    className='justify-start whitespace-nowrap xl:justify-center'
                    disabled={!selectedCanEnd}
                    onClick={() => openDialog('end', selectedRecords)}
                  >
                    <TimerOff className='size-4' />
                    {t('End Initialization')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    className='justify-start whitespace-nowrap xl:justify-center'
                    disabled={!selectedCanExtend}
                    onClick={() => openDialog('extend', selectedRecords)}
                  >
                    <TimerReset className='size-4' />
                    {t('Extend Initialization')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    className='justify-start whitespace-nowrap xl:justify-center'
                    disabled={!selectedCanReopen}
                    onClick={() => openDialog('reopen', selectedRecords)}
                  >
                    <LockOpen className='size-4' />
                    {t('Reopen Selected')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    className='justify-start whitespace-nowrap xl:justify-center'
                    disabled={!selectedCanClose}
                    onClick={() => openDialog('close', selectedRecords)}
                  >
                    <Ban className='size-4' />
                    {t('Close Selected')}
                  </Button>
                </div>
                <Select value={orderType} onValueChange={handleOrderTypeChange}>
                  <SelectTrigger className='w-full xl:w-[180px]'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='all'>{t('All Order Types')}</SelectItem>
                    <SelectItem value='topup'>{t('Topup')}</SelectItem>
                    <SelectItem value='subscription'>
                      {t('Subscription')}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
          </div>
          {isLoading ? (
            <div className='flex items-center justify-center py-8'>
              <div className='text-muted-foreground'>{t('Loading...')}</div>
            </div>
          ) : (
            <>
              <div className='overflow-x-auto rounded-md border'>
                <Table>
                  <TableHeader>
                    {table.getHeaderGroups().map((headerGroup) => (
                      <TableRow key={headerGroup.id}>
                        {headerGroup.headers.map((header) => (
                          <TableHead
                            key={header.id}
                            className={getRebateOrderTableColumnClass(
                              header.column.id
                            )}
                          >
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
                        <TableRow
                          key={row.id}
                          data-state={
                            row.getIsSelected() ? 'selected' : undefined
                          }
                        >
                          {row.getVisibleCells().map((cell) => (
                            <TableCell
                              key={cell.id}
                              className={getRebateOrderTableColumnClass(
                                cell.column.id
                              )}
                            >
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
                          {t('No rebate records found')}
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </div>

              {data && data.total > 0 && (
                <div className='flex flex-col gap-3 px-2 py-4 sm:flex-row sm:items-center sm:justify-between'>
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
                    <div className='min-w-24 text-center text-sm'>
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

        <Dialog
          open={dialog != null}
          onOpenChange={(open) => !open && setDialog(null)}
        >
          <DialogContent className='sm:max-w-[520px]'>
            {dialog?.type === 'edit' && (
              <>
                <DialogHeader>
                  <DialogTitle>{t('Edit Rebate Records')}</DialogTitle>
                  <DialogDescription>
                    {t('Editing {{count}} rebate records', {
                      count: dialog.records.length,
                    })}
                  </DialogDescription>
                </DialogHeader>
                <div className='space-y-4'>
                  <div className='space-y-2'>
                    <Label htmlFor='rebate-amount'>{t('Rebate Amount')}</Label>
                    <Input
                      id='rebate-amount'
                      type='number'
                      min='0'
                      step='0.01'
                      value={editAmount}
                      onChange={(event) => setEditAmount(event.target.value)}
                      placeholder='0.00'
                    />
                    {editAmount && Number.isFinite(Number(editAmount)) && (
                      <p className='text-muted-foreground text-sm'>
                        {formatRebateAmount(
                          Math.round(Number(editAmount) * 100)
                        )}
                      </p>
                    )}
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='rebate-ratio'>{t('Rebate Rate')}</Label>
                    <Input
                      id='rebate-ratio'
                      type='number'
                      min='0'
                      max='100'
                      step='0.01'
                      value={editRatio}
                      onChange={(event) => setEditRatio(event.target.value)}
                      placeholder={t('Enter percentage value (0-100)')}
                    />
                  </div>
                </div>
                <DialogFooter>
                  <Button variant='outline' onClick={() => setDialog(null)}>
                    {t('Cancel')}
                  </Button>
                  <Button onClick={submitEdit} disabled={isMutating}>
                    {isMutating && <Loader2 className='size-4 animate-spin' />}
                    {t('Save')}
                  </Button>
                </DialogFooter>
              </>
            )}

            {dialog?.type === 'close' && (
              <>
                <DialogHeader>
                  <DialogTitle>{t('Close Rebate')}</DialogTitle>
                  <DialogDescription>
                    {t(
                      'Close {{count}} rebate records so inviters cannot see them',
                      {
                        count: dialog.records.length,
                      }
                    )}
                  </DialogDescription>
                </DialogHeader>
                <DialogFooter>
                  <Button variant='outline' onClick={() => setDialog(null)}>
                    {t('Cancel')}
                  </Button>
                  <Button
                    variant='destructive'
                    onClick={submitClose}
                    disabled={isMutating}
                  >
                    {isMutating && <Loader2 className='size-4 animate-spin' />}
                    {t('Close')}
                  </Button>
                </DialogFooter>
              </>
            )}

            {dialog?.type === 'reopen' && (
              <>
                <DialogHeader>
                  <DialogTitle>{t('Reopen Rebate')}</DialogTitle>
                  <DialogDescription>
                    {t(
                      'Reopen {{count}} rebate records so inviters can see them again',
                      {
                        count: dialog.records.length,
                      }
                    )}
                  </DialogDescription>
                </DialogHeader>
                <DialogFooter>
                  <Button variant='outline' onClick={() => setDialog(null)}>
                    {t('Cancel')}
                  </Button>
                  <Button onClick={submitReopen} disabled={isMutating}>
                    {isMutating && <Loader2 className='size-4 animate-spin' />}
                    {t('Reopen')}
                  </Button>
                </DialogFooter>
              </>
            )}

            {dialog?.type === 'end' && (
              <>
                <DialogHeader>
                  <DialogTitle>{t('End Initialization')}</DialogTitle>
                  <DialogDescription>
                    {t('End initialization for {{count}} rebate records now', {
                      count: dialog.records.length,
                    })}
                  </DialogDescription>
                </DialogHeader>
                <DialogFooter>
                  <Button variant='outline' onClick={() => setDialog(null)}>
                    {t('Cancel')}
                  </Button>
                  <Button
                    onClick={submitEndInitialization}
                    disabled={isMutating}
                  >
                    {isMutating && <Loader2 className='size-4 animate-spin' />}
                    {t('End Initialization')}
                  </Button>
                </DialogFooter>
              </>
            )}

            {dialog?.type === 'extend' && (
              <>
                <DialogHeader>
                  <DialogTitle>{t('Extend Initialization')}</DialogTitle>
                  <DialogDescription>
                    {t('Extend initialization for {{count}} rebate records', {
                      count: dialog.records.length,
                    })}
                  </DialogDescription>
                </DialogHeader>
                <div className='space-y-2'>
                  <Label htmlFor='initialization-ends-at'>
                    {t('Initialization Ends')}
                  </Label>
                  <Input
                    id='initialization-ends-at'
                    type='datetime-local'
                    value={extendUntil}
                    onChange={(event) => setExtendUntil(event.target.value)}
                  />
                </div>
                <DialogFooter>
                  <Button variant='outline' onClick={() => setDialog(null)}>
                    {t('Cancel')}
                  </Button>
                  <Button
                    onClick={submitExtendInitialization}
                    disabled={isMutating}
                  >
                    {isMutating && <Loader2 className='size-4 animate-spin' />}
                    {t('Extend')}
                  </Button>
                </DialogFooter>
              </>
            )}
          </DialogContent>
        </Dialog>
      </Card>
      <SignupRewardRecordsPanel />
    </div>
  )
}

function SignupRewardRecordsPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })

  const { data, isLoading } = useQuery({
    queryKey: [
      'adminRebateRecords',
      'signup',
      pagination.pageIndex + 1,
      pagination.pageSize,
    ],
    queryFn: async () => {
      const response = await getAdminRebateRecords({
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        source: 'signup',
      })
      return response.data
    },
  })

  const revokeMutation = useMutation({
    mutationFn: revokeAdminSignupRewardRecord,
    onSuccess: (response) => {
      toast.success(
        response.data?.revoked
          ? t('Signup reward revoked')
          : t('Signup reward already revoked')
      )
      queryClient.invalidateQueries({ queryKey: ['adminRebateRecords'] })
      queryClient.invalidateQueries({
        queryKey: ['adminInvitationRegistrations'],
      })
      queryClient.invalidateQueries({ queryKey: ['adminRebateRequests'] })
      queryClient.invalidateQueries({ queryKey: ['rebateStats'] })
    },
    onError: (error: unknown) => {
      toast.error(
        getInvitationErrorMessage(error, t('Failed to revoke signup reward'))
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

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Invitation Signup Reward Records')}</CardTitle>
        <CardDescription>
          {t(
            'View generated invitation signup rewards and revoke uncompleted records'
          )}
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
                    <TableHead className='min-w-28'>{t('Record ID')}</TableHead>
                    <TableHead className='min-w-40'>
                      {t('Reward Type')}
                    </TableHead>
                    <TableHead className='min-w-32'>
                      {t('Reward User')}
                    </TableHead>
                    <TableHead className='min-w-36'>
                      {t('Rebate Amount')}
                    </TableHead>
                    <TableHead className='min-w-32'>{t('Status')}</TableHead>
                    <TableHead className='min-w-44'>
                      {t('Created At')}
                    </TableHead>
                    <TableHead className='bg-popover sticky right-0 z-20 w-44 min-w-44 border-l shadow-[-8px_0_8px_-8px_rgb(0_0_0_/_0.2)]'>
                      {t('Actions')}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.length > 0 ? (
                    items.map((record: RebateRecord) => {
                      const canRevoke = canRevokeSignupReward(record)
                      return (
                        <TableRow key={record.id}>
                          <TableCell className='font-mono'>
                            #{record.id}
                          </TableCell>
                          <TableCell>
                            {signupRewardTypeLabel(t, record.orderType)}
                          </TableCell>
                          <TableCell className='font-mono'>
                            #{record.inviterId}
                          </TableCell>
                          <TableCell>
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
                          <TableCell className='text-muted-foreground'>
                            {formatDateTime(record.createdAt)}
                          </TableCell>
                          <TableCell className='bg-popover sticky right-0 z-20 w-44 min-w-44 border-l shadow-[-8px_0_8px_-8px_rgb(0_0_0_/_0.2)]'>
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              className='justify-start whitespace-nowrap'
                              title={
                                canRevoke
                                  ? t('Revoke Reward')
                                  : t(
                                      'Completed signup rewards must undo completion before revoke'
                                    )
                              }
                              disabled={!canRevoke || revokeMutation.isPending}
                              onClick={() => revokeMutation.mutate(record.id)}
                            >
                              <RotateCcw className='size-4' />
                              {t('Revoke Reward')}
                            </Button>
                          </TableCell>
                        </TableRow>
                      )
                    })
                  ) : (
                    <TableRow>
                      <TableCell colSpan={7} className='h-24 text-center'>
                        {t('No signup reward records found')}
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
                    from: pagination.pageIndex * pagination.pageSize + 1,
                    to: Math.min(currentPage * pagination.pageSize, total),
                    total,
                  })}
                </div>
                <div className='flex items-center gap-2'>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={goPrevious}
                    disabled={pagination.pageIndex <= 0}
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
    </Card>
  )
}

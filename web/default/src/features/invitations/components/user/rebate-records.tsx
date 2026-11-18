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
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { History } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useAuthStore } from '@/stores/auth-store'

import { getRebateRecords } from '../../api'
import { requireInvitationPage } from '../../api/result'
import {
  formatInvitationDate,
  formatRebateAmount,
  formatRebateRate,
  formatSalesAmount,
} from '../../format'
import {
  rebateStatusLabel,
  rebateStatusVariant,
  salesRebateStatusLabel,
} from '../../status-label'
import type { PaginatedResponse, RebateRecord, RebateStatus } from '../../types'
import { PaginationControls } from '../pagination-controls'

const PAGE_SIZE = 10
const SIGNUP_REWARDS = new Set(['invite_inviter', 'invite_invitee'])
const REBATE_ORDERS = new Set(['topup', 'subscription', 'redemption'])
const UNCALCULATED_STATUSES = new Set([
  'confirming',
  'initializing',
  'pending_calculation',
  'not_generated',
  'estimated',
])

const isRebateRecord = (value: unknown): value is RebateRecord => {
  if (value === null || typeof value !== 'object') return false
  const record = value as Partial<RebateRecord>
  return (
    typeof record.id === 'number' &&
    Number.isSafeInteger(record.id) &&
    typeof record.orderType === 'string' &&
    typeof record.orderAmount === 'number' &&
    Number.isFinite(record.orderAmount) &&
    typeof record.rebateAmount === 'number' &&
    Number.isFinite(record.rebateAmount) &&
    typeof record.status === 'string' &&
    typeof record.createdAt === 'string' &&
    typeof record.updatedAt === 'string'
  )
}

export const RebateRecords = (props: { sales?: boolean }) => {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<RebateStatus | 'all'>('all')
  const query = useQuery({
    queryKey: ['kkai', 'invitations', 'records', userId, page, status],
    enabled: !!userId,
    queryFn: async ({ signal }) =>
      requireInvitationPage(
        await getRebateRecords(
          {
            page,
            pageSize: PAGE_SIZE,
            ...(status === 'all' ? {} : { status }),
          },
          signal
        ),
        'Failed to load rebate records',
        isRebateRecord
      ),
  })

  if (!userId) return null

  return (
    <Card>
      <CardHeader className='flex-row items-center justify-between gap-3'>
        <CardTitle>{t('Rebate Records')}</CardTitle>
        <Select
          value={status}
          onValueChange={(value) => {
            setStatus(value as RebateStatus | 'all')
            setPage(1)
          }}
        >
          <SelectTrigger className='w-40' aria-label={t('Filter by status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='all'>{t('All Status')}</SelectItem>
            <SelectItem value='initializing'>{t('Confirming')}</SelectItem>
            <SelectItem value='pending'>{t('Pending')}</SelectItem>
            <SelectItem value='requested'>{t('Requested')}</SelectItem>
            <SelectItem value='approved'>{t('Approved')}</SelectItem>
            <SelectItem value='completed'>{t('Completed')}</SelectItem>
            <SelectItem value='rejected'>{t('Rejected')}</SelectItem>
          </SelectContent>
        </Select>
      </CardHeader>
      <CardContent>
        {props.sales &&
          query.data?.items.some(
            (record) => record.creditCurrency !== 'usd_credit_v1'
          ) && (
            <p className='text-muted-foreground mb-3 text-sm'>
              {t('Legacy Site Credits')}
            </p>
          )}
        <RebateRecordsContent query={query} sales={props.sales} />
        {query.data && (
          <PaginationControls
            page={page}
            pageSize={PAGE_SIZE}
            total={query.data.total}
            onPageChange={setPage}
          />
        )}
      </CardContent>
    </Card>
  )
}

const RebateRecordsContent = (props: {
  query: UseQueryResult<PaginatedResponse<RebateRecord>, Error>
  sales?: boolean
}) => {
  const { t } = useTranslation()
  if (props.query.isPending) return <Skeleton className='h-72 w-full' />
  if (props.query.isError) {
    return (
      <ErrorState
        title={t('Failed to load rebate records')}
        description={props.query.error.message}
        onRetry={() => void props.query.refetch()}
      />
    )
  }
  if (!props.query.data?.items.length) {
    return <EmptyState icon={History} title={t('No rebate records')} bordered />
  }
  return (
    <RebateRecordList records={props.query.data.items} sales={props.sales} />
  )
}

const RebateRecordList = (props: {
  records: RebateRecord[]
  sales?: boolean
}) => {
  const { t } = useTranslation()
  return (
    <>
      <div className='hidden overflow-hidden rounded-md border md:block'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Type')}</TableHead>
              {!props.sales && <TableHead>{t('Order Amount')}</TableHead>}
              <TableHead>{t('Rebate Amount')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
              <TableHead>{t('Effective At')}</TableHead>
              <TableHead>{t('Created At')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.records.map((record) => (
              <TableRow key={record.id}>
                <TableCell>{record.orderType}</TableCell>
                {!props.sales && (
                  <TableCell>
                    {formatRebateAmount(record.orderAmount)}
                  </TableCell>
                )}
                <TableCell className='whitespace-normal'>
                  <RecordRebateAmount record={record} sales={props.sales} />
                </TableCell>
                <TableCell>
                  <RecordStatus record={record} />
                </TableCell>
                <TableCell>
                  {formatInvitationDate(record.effectiveAt)}
                </TableCell>
                <TableCell>{formatInvitationDate(record.createdAt)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <div className='space-y-3 md:hidden'>
        {props.records.map((record) => (
          <div key={record.id} className='rounded-md border p-3'>
            <div className='flex items-start justify-between gap-3'>
              <span className='font-medium'>{record.orderType}</span>
              <RecordStatus record={record} />
            </div>
            <dl className='mt-3 grid grid-cols-2 gap-2 text-sm'>
              {!props.sales && (
                <>
                  <dt className='text-muted-foreground'>{t('Order Amount')}</dt>
                  <dd className='text-right tabular-nums'>
                    {formatRebateAmount(record.orderAmount)}
                  </dd>
                </>
              )}
              <dt className='text-muted-foreground'>{t('Rebate Amount')}</dt>
              <dd className='min-w-0 text-right tabular-nums'>
                <RecordRebateAmount record={record} sales={props.sales} />
              </dd>
              <dt className='text-muted-foreground'>{t('Effective At')}</dt>
              <dd className='text-right'>
                {formatInvitationDate(record.effectiveAt)}
              </dd>
              <dt className='text-muted-foreground'>{t('Created At')}</dt>
              <dd className='text-right'>
                {formatInvitationDate(record.createdAt)}
              </dd>
            </dl>
          </div>
        ))}
      </div>
    </>
  )
}

const RecordRebateAmount = ({
  record,
  sales,
}: {
  record: RebateRecord
  sales?: boolean
}) => {
  const { t, i18n } = useTranslation()
  let note = t('No rate recorded')
  if (SIGNUP_REWARDS.has(record.orderType)) {
    note = t('Fixed reward (not percentage-based)')
  } else if (REBATE_ORDERS.has(record.orderType)) {
    const rate = formatRebateRate(
      record.rebateRatio,
      i18n.resolvedLanguage ?? i18n.language
    )
    if (rate !== null) {
      note = t('Rate for this record: {{rate}}', { rate })
    } else if (
      record.rebateRatio == null &&
      !record.issuedAt &&
      (record.status === 'pending' || record.status === 'initializing') &&
      UNCALCULATED_STATUSES.has(record.displayStatus ?? record.status)
    ) {
      note = t('Rate pending confirmation')
    }
  }
  return (
    <>
      {sales
        ? formatSalesAmount(
            record.rebateAmount,
            record.creditCurrency ?? 'legacy_075'
          )
        : formatRebateAmount(record.rebateAmount)}
      <span className='text-muted-foreground block text-xs wrap-break-word'>
        {note}
      </span>
    </>
  )
}

const RecordStatus = (props: { record: RebateRecord }) => {
  const { t } = useTranslation()
  const status =
    props.record.displayStatus === 'estimated' &&
    props.record.rebateRatio == null &&
    !SIGNUP_REWARDS.has(props.record.orderType)
      ? 'pending_calculation'
      : props.record.displayStatus
  return (
    <Badge
      variant={rebateStatusVariant(
        props.record.issuedAt ? 'completed' : props.record.status
      )}
    >
      {t(
        status
          ? salesRebateStatusLabel(status)
          : rebateStatusLabel(props.record.status)
      )}
    </Badge>
  )
}

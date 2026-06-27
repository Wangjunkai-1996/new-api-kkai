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
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  AlertCircle,
  CheckCircle2,
  Clock3,
  Gauge,
  RefreshCw,
  ServerCrash,
  Signal,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatNumber, formatPercent, formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'
import { getGroupStatus } from './api'
import type {
  GroupHealthConfidence,
  GroupHealthStatus,
  GroupStatusEntry,
  GroupStatusWindowHours,
} from './types'

const WINDOW_OPTIONS: GroupStatusWindowHours[] = [1, 6, 24]

const STATUS_ORDER: GroupHealthStatus[] = [
  'outage',
  'degraded',
  'busy',
  'operational',
  'unknown',
]

const STATUS_META: Record<
  GroupHealthStatus,
  { labelKey: string; variant: StatusVariant; icon: typeof CheckCircle2 }
> = {
  operational: {
    labelKey: 'Operational',
    variant: 'success',
    icon: CheckCircle2,
  },
  busy: {
    labelKey: 'Busy',
    variant: 'warning',
    icon: Clock3,
  },
  degraded: {
    labelKey: 'Degraded',
    variant: 'warning',
    icon: AlertCircle,
  },
  outage: {
    labelKey: 'Outage',
    variant: 'danger',
    icon: ServerCrash,
  },
  unknown: {
    labelKey: 'Unknown',
    variant: 'neutral',
    icon: Signal,
  },
}

const CONFIDENCE_LABELS: Record<GroupHealthConfidence, string> = {
  high: 'High',
  medium: 'Medium',
  low: 'Low',
}

const MESSAGE_LABELS: Record<string, string> = {
  'No routable models are currently enabled for this group.':
    'No routable models are currently enabled for this group.',
  'Not enough recent traffic to determine health.':
    'Not enough recent traffic to determine health.',
  'Recent requests are failing at a high rate.':
    'Recent requests are failing at a high rate.',
  'Recent success rate is below the healthy threshold.':
    'Recent success rate is below the healthy threshold.',
  'Requests are succeeding, but latency is elevated.':
    'Requests are succeeding, but latency is elevated.',
  'Recent traffic is healthy.': 'Recent traffic is healthy.',
}

export function GroupStatusPage() {
  const { t } = useTranslation()
  const [hours, setHours] = useState<GroupStatusWindowHours>(6)
  const query = useQuery({
    queryKey: ['group-status', hours],
    queryFn: () => getGroupStatus(hours),
    staleTime: 30 * 1000,
  })

  const result = query.data?.data
  const groups = result?.groups ?? []
  const summary = useMemo(() => buildSummary(groups), [groups])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Group Status')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('Recent health of the groups available to your account.')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Actions>
        <ButtonGroup aria-label={t('Status window')}>
          {WINDOW_OPTIONS.map((option) => (
            <Button
              key={option}
              type='button'
              variant={hours === option ? 'default' : 'outline'}
              size='sm'
              aria-pressed={hours === option}
              onClick={() => setHours(option)}
            >
              {t('{{hours}}h', { hours: option })}
            </Button>
          ))}
        </ButtonGroup>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={query.isFetching}
          onClick={() => query.refetch()}
        >
          <RefreshCw
            className={cn('size-3.5', query.isFetching && 'animate-spin')}
          />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-3 sm:space-y-4'>
          {query.isLoading && !result ? (
            <GroupStatusSkeleton />
          ) : query.isError ? (
            <ErrorState
              title={t('Failed to load group status')}
              description={t('Please retry after checking your session.')}
              onRetry={() => query.refetch()}
            />
          ) : groups.length === 0 ? (
            <EmptyState
              icon={Signal}
              title={t('No groups available')}
              description={t('No usable groups are currently assigned.')}
              bordered
            />
          ) : (
            <>
              <StatusSummary
                groups={groups}
                summary={summary}
                generatedAt={result?.generated_at}
              />
              <GroupStatusTable groups={groups} />
              <GroupStatusCards groups={groups} />
            </>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function StatusSummary(props: {
  groups: GroupStatusEntry[]
  summary: Record<GroupHealthStatus, number>
  generatedAt?: number
}) {
  const { t } = useTranslation()
  const totalRequests = props.groups.reduce(
    (sum, group) => sum + group.request_count,
    0
  )
  const availableModels = props.groups.reduce(
    (sum, group) => sum + group.available_model_count,
    0
  )

  return (
    <div className='grid grid-cols-2 gap-2 lg:grid-cols-4'>
      <SummaryCard
        icon={Signal}
        label={t('Groups')}
        value={formatNumber(props.groups.length)}
        detail={STATUS_ORDER.map((status) => {
          const count = props.summary[status]
          if (!count) return null
          return `${t(STATUS_META[status].labelKey)} ${count}`
        })
          .filter(Boolean)
          .join(' / ')}
      />
      <SummaryCard
        icon={Activity}
        label={t('Requests')}
        value={formatNumber(totalRequests)}
        detail={t('Selected window')}
      />
      <SummaryCard
        icon={Gauge}
        label={t('Available Models')}
        value={formatNumber(availableModels)}
        detail={t('Routable model coverage')}
      />
      <SummaryCard
        icon={Clock3}
        label={t('Updated At')}
        value={formatTimestampToDate(props.generatedAt)}
        detail={t('Read-only snapshot')}
      />
    </div>
  )
}

function SummaryCard(props: {
  icon: typeof Signal
  label: string
  value: string
  detail: string
}) {
  const Icon = props.icon
  return (
    <Card size='sm' className='rounded-lg'>
      <CardContent className='flex min-h-24 items-start gap-3'>
        <div className='bg-muted text-muted-foreground mt-0.5 rounded-md p-1.5'>
          <Icon className='size-4' />
        </div>
        <div className='min-w-0 space-y-1'>
          <div className='text-muted-foreground text-xs font-medium'>
            {props.label}
          </div>
          <div className='truncate text-xl font-semibold'>{props.value}</div>
          <div className='text-muted-foreground line-clamp-2 text-xs'>
            {props.detail || '-'}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function GroupStatusTable({ groups }: { groups: GroupStatusEntry[] }) {
  const { t } = useTranslation()

  return (
    <Card className='hidden rounded-lg md:block'>
      <CardHeader className='border-b'>
        <CardTitle>{t('Group Health')}</CardTitle>
      </CardHeader>
      <CardContent className='p-0'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='min-w-48'>{t('Group')}</TableHead>
              <TableHead className='min-w-40'>{t('Status')}</TableHead>
              <TableHead>{t('Confidence')}</TableHead>
              <TableHead className='text-right'>{t('Requests')}</TableHead>
              <TableHead className='text-right'>{t('Success Rate')}</TableHead>
              <TableHead className='text-right'>
                {t('Avg Latency')}
              </TableHead>
              <TableHead className='text-right'>{t('Avg TTFT')}</TableHead>
              <TableHead className='text-right'>
                {t('Available Models')}
              </TableHead>
              <TableHead className='min-w-52'>{t('Message')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {groups.map((group) => (
              <TableRow key={group.group}>
                <TableCell>
                  <div className='min-w-0'>
                    <div className='font-medium'>{group.group}</div>
                    <div className='text-muted-foreground max-w-56 truncate text-xs'>
                      {group.desc || '-'}
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <HealthStatusBadge status={group.status} />
                </TableCell>
                <TableCell>
                  {t(CONFIDENCE_LABELS[group.confidence])}
                </TableCell>
                <TableCell className='text-right'>
                  {formatNumber(group.request_count)}
                </TableCell>
                <TableCell className='text-right'>
                  {formatSuccessRate(group)}
                </TableCell>
                <TableCell className='text-right'>
                  {formatLatency(group.avg_latency_ms)}
                </TableCell>
                <TableCell className='text-right'>
                  {formatLatency(group.avg_ttft_ms)}
                </TableCell>
                <TableCell className='text-right'>
                  {formatNumber(group.available_model_count)}
                </TableCell>
                <TableCell>
                  <span className='text-muted-foreground line-clamp-2 whitespace-normal'>
                    {t(MESSAGE_LABELS[group.message] ?? group.message)}
                  </span>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function GroupStatusCards({ groups }: { groups: GroupStatusEntry[] }) {
  const { t } = useTranslation()

  return (
    <div className='space-y-2 md:hidden'>
      {groups.map((group) => (
        <Card key={group.group} size='sm' className='rounded-lg'>
          <CardHeader>
            <div className='flex min-w-0 items-start justify-between gap-3'>
              <div className='min-w-0'>
                <CardTitle className='truncate'>{group.group}</CardTitle>
                <div className='text-muted-foreground truncate text-xs'>
                  {group.desc || '-'}
                </div>
              </div>
              <HealthStatusBadge status={group.status} />
            </div>
          </CardHeader>
          <CardContent className='space-y-3'>
            <div className='grid grid-cols-2 gap-x-4 gap-y-2 text-sm'>
              <MobileMetric
                label={t('Confidence')}
                value={t(CONFIDENCE_LABELS[group.confidence])}
              />
              <MobileMetric
                label={t('Requests')}
                value={formatNumber(group.request_count)}
              />
              <MobileMetric
                label={t('Success Rate')}
                value={formatSuccessRate(group)}
              />
              <MobileMetric
                label={t('Available Models')}
                value={formatNumber(group.available_model_count)}
              />
              <MobileMetric
                label={t('Avg Latency')}
                value={formatLatency(group.avg_latency_ms)}
              />
              <MobileMetric
                label={t('Avg TTFT')}
                value={formatLatency(group.avg_ttft_ms)}
              />
            </div>
            <p className='text-muted-foreground text-sm'>
              {t(MESSAGE_LABELS[group.message] ?? group.message)}
            </p>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function MobileMetric(props: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='truncate font-medium'>{props.value}</div>
    </div>
  )
}

function HealthStatusBadge({ status }: { status: GroupHealthStatus }) {
  const { t } = useTranslation()
  const meta = STATUS_META[status]
  return (
    <StatusBadge
      copyable={false}
      icon={meta.icon}
      label={t(meta.labelKey)}
      variant={meta.variant}
    />
  )
}

function GroupStatusSkeleton() {
  return (
    <div className='space-y-3'>
      <div className='grid grid-cols-2 gap-2 lg:grid-cols-4'>
        {Array.from({ length: 4 }).map((_, index) => (
          <Card key={index} size='sm' className='rounded-lg'>
            <CardContent className='space-y-2'>
              <Skeleton className='h-4 w-20' />
              <Skeleton className='h-7 w-28' />
              <Skeleton className='h-3 w-full' />
            </CardContent>
          </Card>
        ))}
      </div>
      <Card className='rounded-lg'>
        <CardContent className='space-y-2'>
          {Array.from({ length: 5 }).map((_, index) => (
            <Skeleton key={index} className='h-10 w-full' />
          ))}
        </CardContent>
      </Card>
    </div>
  )
}

function buildSummary(
  groups: GroupStatusEntry[]
): Record<GroupHealthStatus, number> {
  return groups.reduce(
    (summary, group) => {
      summary[group.status] += 1
      return summary
    },
    {
      operational: 0,
      busy: 0,
      degraded: 0,
      outage: 0,
      unknown: 0,
    } as Record<GroupHealthStatus, number>
  )
}

function formatLatency(value: number): string {
  if (!value) return '-'
  if (value >= 1000) {
    return `${formatNumber(value / 1000)}s`
  }
  return `${formatNumber(value)}ms`
}

function formatSuccessRate(group: GroupStatusEntry): string {
  if (group.request_count <= 0) return '-'
  return formatPercent(group.success_rate)
}

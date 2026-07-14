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

import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, RefreshCw, Signal } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertAction, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { cn } from '@/lib/utils'

import { getGroupStatus } from './api'
import { GroupStatusList } from './components/group-status-list'
import { GroupStatusSkeleton } from './components/group-status-skeleton'
import { GroupStatusSummary } from './components/group-status-summary'
import { sortGroupStatuses } from './status'
import type {
  GroupStatusEntry,
  GroupStatusResult,
  GroupStatusWindow,
} from './types'

const WINDOW_OPTIONS: { value: GroupStatusWindow; labelKey: string }[] = [
  { value: 'now', labelKey: 'Now' },
  { value: '15m', labelKey: '15m' },
  { value: '1h', labelKey: '1h' },
  { value: '6h', labelKey: '6h' },
  { value: '24h', labelKey: '24h' },
]

export function GroupStatusPage() {
  const { t } = useTranslation()
  const [window, setWindow] = useState<GroupStatusWindow>('now')
  const query = useQuery({
    queryKey: ['kkai', 'group-status', window],
    queryFn: () => getGroupStatus(window),
    staleTime: window === 'now' ? 10_000 : 30_000,
    refetchInterval: window === 'now' ? 15_000 : false,
    refetchIntervalInBackground: false,
  })
  const groups = useMemo(
    () => sortGroupStatuses(query.data?.groups ?? []),
    [query.data?.groups]
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Group Status')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <ButtonGroup aria-label={t('Status window')}>
          {WINDOW_OPTIONS.map((option) => (
            <Button
              key={option.value}
              type='button'
              size='sm'
              variant={window === option.value ? 'default' : 'outline'}
              aria-pressed={window === option.value}
              onClick={() => setWindow(option.value)}
            >
              {t(option.labelKey)}
            </Button>
          ))}
        </ButtonGroup>
        <Button
          type='button'
          size='icon-sm'
          variant='outline'
          disabled={query.isFetching}
          aria-label={t('Refresh')}
          title={t('Refresh')}
          onClick={() => void query.refetch()}
        >
          <RefreshCw className={cn(query.isFetching && 'animate-spin')} />
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          {query.isError && query.data && (
            <Alert variant='destructive'>
              <AlertTriangle aria-hidden='true' />
              <AlertDescription>
                {t('Refresh failed. Showing the latest available data.')}
              </AlertDescription>
              <AlertAction>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-xs'
                  aria-label={t('Retry')}
                  title={t('Retry')}
                  onClick={() => void query.refetch()}
                >
                  <RefreshCw />
                </Button>
              </AlertAction>
            </Alert>
          )}
          <GroupStatusContent
            isLoading={query.isLoading && !query.data}
            error={query.error}
            result={query.data}
            groups={groups}
            onRetry={() => void query.refetch()}
          />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function GroupStatusContent(props: {
  isLoading: boolean
  error: Error | null
  result?: GroupStatusResult
  groups: GroupStatusEntry[]
  onRetry: () => void
}) {
  const { t } = useTranslation()

  if (props.isLoading) return <GroupStatusSkeleton />
  if (props.error && !props.result) {
    return (
      <ErrorState
        title={t('Failed to load group status')}
        description={props.error.message}
        onRetry={props.onRetry}
      />
    )
  }
  if (!props.result || props.groups.length === 0) {
    return (
      <EmptyState
        icon={Signal}
        title={t('No groups available')}
        description={t('No usable groups are currently assigned.')}
        bordered
      />
    )
  }
  return (
    <>
      <GroupStatusSummary groups={props.groups} result={props.result} />
      <GroupStatusList groups={props.groups} />
    </>
  )
}

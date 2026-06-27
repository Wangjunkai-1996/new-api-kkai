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
import { RefreshCw, Signal } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { getGroupStatus } from './api'
import { GroupStatusCards } from './group-status-cards'
import { GroupStatusSummary } from './group-status-summary'
import { GroupStatusTable } from './group-status-table'
import { sortGroupsForConfidence } from './status-display'
import { WINDOW_OPTIONS } from './status-meta'
import type { GroupStatusWindow } from './types'

export function GroupStatusPage() {
  const { t } = useTranslation()
  const [window, setWindow] = useState<GroupStatusWindow>('now')
  const query = useQuery({
    queryKey: ['group-status', window],
    queryFn: () => getGroupStatus(window),
    staleTime: window === 'now' ? 10 * 1000 : 30 * 1000,
    refetchInterval: window === 'now' ? 15 * 1000 : false,
  })

  const result = query.data?.data
  const sortedGroups = useMemo(
    () => sortGroupsForConfidence(result?.groups ?? []),
    [result?.groups]
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Group Flow')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('A confidence panel for choosing the smoothest group.')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Actions>
        <ButtonGroup aria-label={t('Status window')}>
          {WINDOW_OPTIONS.map((option) => (
            <Button
              key={option.value}
              type='button'
              variant={window === option.value ? 'default' : 'outline'}
              size='sm'
              aria-pressed={window === option.value}
              title={t(option.detailKey)}
              onClick={() => setWindow(option.value)}
            >
              {t(option.labelKey)}
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
          ) : sortedGroups.length === 0 ? (
            <EmptyState
              icon={Signal}
              title={t('No groups available')}
              description={t('No usable groups are currently assigned.')}
              bordered
            />
          ) : (
            <>
              <GroupStatusSummary
                groups={sortedGroups}
                window={result?.window ?? window}
                windowMinutes={result?.window_minutes}
                generatedAt={result?.generated_at}
              />
              <GroupStatusCards groups={sortedGroups} generatedAt={result?.generated_at} />
              <GroupStatusTable groups={sortedGroups} />
            </>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function GroupStatusSkeleton() {
  return (
    <div className='space-y-3'>
      <Card className='rounded-xl'>
        <CardContent className='space-y-3'>
          <Skeleton className='h-5 w-32' />
          <Skeleton className='h-9 w-72 max-w-full' />
          <Skeleton className='h-4 w-full max-w-xl' />
        </CardContent>
      </Card>
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

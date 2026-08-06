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
import { Film, LoaderCircle, RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { formatTimestampToDate } from '@/lib/format'

import { VideoStudioNav } from './components/video-studio-nav'
import { useVideoGenerations } from './queries'
import type { VideoGenerationStatus } from './types'
import {
  getVideoGenerationFailureMessageKey,
  getVideoProgress,
} from './video-domain'

const STATUS_VARIANTS: Record<VideoGenerationStatus, StatusVariant> = {
  queued: 'warning',
  processing: 'info',
  archiving: 'cyan',
  ready: 'success',
  failed: 'danger',
}

const STATUS_LABELS: Record<VideoGenerationStatus, string> = {
  queued: 'videoStudio.status.queued',
  processing: 'videoStudio.status.processing',
  archiving: 'videoStudio.status.archiving',
  ready: 'videoStudio.status.ready',
  failed: 'videoStudio.status.failed',
}

export function VideoTasksPage() {
  const { t } = useTranslation()
  const generationsQuery = useVideoGenerations({}, true)
  const generations = generationsQuery.items

  return (
    <main
      id='content'
      className='flex size-full min-h-0 flex-col overflow-hidden'
    >
      <VideoStudioNav
        action={
          <Button
            size='icon-sm'
            variant='ghost'
            onClick={() => void generationsQuery.refresh()}
            aria-label={t('videoStudio.refresh')}
          >
            <RotateCw
              className={
                generationsQuery.isFetching
                  ? 'animate-spin motion-reduce:animate-none'
                  : undefined
              }
              aria-hidden='true'
            />
          </Button>
        }
      />
      <div className='min-h-0 flex-1 overflow-y-auto p-3 sm:p-4'>
        {generationsQuery.isLoading && generations.length === 0 && (
          <div className='space-y-2'>
            {Array.from({ length: 7 }, (_, index) => (
              <Skeleton
                key={`task-skeleton-${index}`}
                className='h-24 w-full rounded-lg'
              />
            ))}
          </div>
        )}

        {generationsQuery.isError && generations.length === 0 && (
          <Empty className='min-h-72 border'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <Film aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>{t('videoStudio.tasksFailed')}</EmptyTitle>
              <EmptyDescription>
                {t('videoStudio.tasksFailedDescription')}
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button
                variant='outline'
                onClick={() => void generationsQuery.refresh()}
              >
                <RotateCw aria-hidden='true' />
                {t('videoStudio.retry')}
              </Button>
            </EmptyContent>
          </Empty>
        )}

        {!generationsQuery.isLoading &&
          !generationsQuery.isError &&
          generations.length === 0 && (
            <Empty className='min-h-72 border'>
              <EmptyHeader>
                <EmptyMedia variant='icon'>
                  <Film aria-hidden='true' />
                </EmptyMedia>
                <EmptyTitle>{t('videoStudio.tasksEmpty')}</EmptyTitle>
                <EmptyDescription>
                  {t('videoStudio.tasksEmptyDescription')}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}

        {generationsQuery.isError && generations.length > 0 && (
          <div
            className='border-destructive/30 bg-destructive/5 text-destructive mb-3 flex items-center justify-between gap-3 rounded-lg border px-3 py-2 text-xs'
            role='alert'
          >
            <span>{t('videoStudio.refreshFailed')}</span>
            <Button
              size='sm'
              variant='outline'
              onClick={() => void generationsQuery.refresh()}
            >
              {t('videoStudio.retry')}
            </Button>
          </div>
        )}

        {generations.length > 0 && (
          <div className='divide-border overflow-hidden rounded-lg border'>
            {generations.map((generation) => {
              const status = generation.status
              const progress = getVideoProgress(generation)
              const failureMessageKey =
                getVideoGenerationFailureMessageKey(generation)
              return (
                <article
                  key={generation.id}
                  className='bg-background flex min-w-0 items-center gap-3 border-b p-3 last:border-b-0 sm:gap-4'
                >
                  <div className='bg-muted flex aspect-video w-24 shrink-0 items-center justify-center overflow-hidden rounded-md sm:w-32'>
                    {generation.poster_url ? (
                      <img
                        src={generation.poster_url}
                        alt={generation.prompt}
                        className='size-full object-cover'
                      />
                    ) : (
                      <Film
                        className='text-muted-foreground size-5'
                        aria-hidden='true'
                      />
                    )}
                  </div>

                  <div className='min-w-0 flex-1 space-y-1.5'>
                    <div className='flex min-w-0 items-center gap-2'>
                      <p className='min-w-0 flex-1 truncate text-sm font-medium'>
                        {generation.prompt}
                      </p>
                      <StatusBadge
                        label={t(STATUS_LABELS[status])}
                        variant={STATUS_VARIANTS[status]}
                        copyable={false}
                        pulse={
                          status === 'processing' || status === 'archiving'
                        }
                        className='shrink-0 text-xs motion-reduce:animate-none'
                      />
                    </div>
                    <div className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
                      <span>{generation.model}</span>
                      <code className='max-w-48 truncate'>
                        {generation.task_id}
                      </code>
                      <span>
                        {formatTimestampToDate(generation.created_at)}
                      </span>
                    </div>
                    {(status === 'queued' || status === 'processing') && (
                      <div className='flex items-center gap-2'>
                        <Progress
                          value={progress}
                          className='max-w-56 flex-1'
                        />
                        <span className='text-muted-foreground text-[11px] tabular-nums'>
                          {progress}%
                        </span>
                      </div>
                    )}
                    {failureMessageKey && (
                      <p className='text-destructive line-clamp-1 text-xs'>
                        {t(failureMessageKey)}
                      </p>
                    )}
                  </div>

                  {status === 'ready' && generation.video_url && (
                    <Button
                      size='sm'
                      variant='outline'
                      className='hidden shrink-0 sm:inline-flex'
                      render={
                        <a href={generation.video_url}>
                          {t('videoStudio.play')}
                        </a>
                      }
                    />
                  )}
                </article>
              )
            })}
          </div>
        )}

        {generationsQuery.hasNextPage && (
          <div className='flex justify-center pt-5'>
            <Button
              variant='outline'
              onClick={() => generationsQuery.fetchNextPage()}
              disabled={generationsQuery.isFetchingNextPage}
            >
              {generationsQuery.isFetchingNextPage && (
                <LoaderCircle
                  className='animate-spin motion-reduce:animate-none'
                  aria-hidden='true'
                />
              )}
              {t('videoStudio.loadMore')}
            </Button>
          </div>
        )}
      </div>
    </main>
  )
}

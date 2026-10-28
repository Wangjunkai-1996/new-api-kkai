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
import { Link } from '@tanstack/react-router'
import { Copy, Film, LoaderCircle, RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import emptyArtwork from '@/assets/video-works/empty.webp'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { formatTimestampToDate } from '@/lib/format'
import { signedStudioMediaUrl, useStudioMediaUrls } from '@/lib/studio-media'
import { useAuthStore } from '@/stores/auth-store'

import { VideoGenerationStatus } from './components/video-generation-status'
import { VideoStudioNav } from './components/video-studio-nav'
import { useVideoGenerations } from './queries'
import { getVideoGenerationFailureMessageKey } from './video-domain'

export function VideoTasksPage() {
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const sessionId = useAuthStore((state) => state.auth.session?.sid ?? '')
  return <VideoTasksContent key={`${userId}:${sessionId}`} />
}

function VideoTasksContent() {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  const query = useVideoGenerations({}, true)
  const mediaQuery = useStudioMediaUrls(
    query.items.map((generation) => generation.poster_url)
  )

  return (
    <main id='content' className='video-studio-v2 video-studio-page'>
      <VideoStudioNav />
      <div className='video-studio-page-content'>
        <header className='video-studio-page-toolbar'>
          <div>
            <h2>{t('videoStudio.tasks')}</h2>
            <p>{t('Follow each video from submission to completion.')}</p>
          </div>
          <Button variant='outline' onClick={() => void query.refresh()}>
            <RotateCw aria-hidden='true' />
            {t('videoStudio.refresh')}
          </Button>
        </header>
        {query.isLoading && query.items.length === 0 && (
          <div className='space-y-3'>
            {Array.from({ length: 5 }, (_, index) => (
              <Skeleton key={index} className='h-24 rounded-xl' />
            ))}
          </div>
        )}
        {query.isError && (
          <div className='video-studio-notice' role='alert'>
            <span>
              {query.items.length
                ? t('videoStudio.refreshFailed')
                : t('videoStudio.tasksFailedDescription')}
            </span>
            <Button variant='outline' onClick={() => void query.refresh()}>
              {t('Retry')}
            </Button>
          </div>
        )}
        {query.isSuccess && query.items.length === 0 && (
          <div className='video-studio-empty'>
            <img src={emptyArtwork} alt='' />
            <h3>{t('videoStudio.tasksEmpty')}</h3>
            <p>{t('videoStudio.tasksEmptyDescription')}</p>
            <Button render={<Link to='/video-studio/create' />}>
              {t('videoStudio.create')}
            </Button>
          </div>
        )}
        {query.items.length > 0 && (
          <div className='video-studio-tasks-list'>
            <div
              className='video-studio-task-row video-studio-task-row-head'
              aria-hidden='true'
            >
              <span>{t('Video task')}</span>
              <span>{t('Status')}</span>
              <span>{t('Actions')}</span>
            </div>
            {query.items.map((generation) => {
              const posterUrl = mediaQuery.identityDenied
                ? undefined
                : signedStudioMediaUrl(
                    generation.poster_url,
                    mediaQuery.data?.urls,
                    mediaQuery.data?.deniedPaths,
                    mediaQuery.identityDenied
                  )
              const failureMessageKey =
                getVideoGenerationFailureMessageKey(generation)
              return (
                <article key={generation.id} className='video-studio-task-row'>
                  <div className='video-studio-task-icon'>
                    {posterUrl ? (
                      <img
                        src={posterUrl}
                        alt=''
                        loading='lazy'
                        decoding='async'
                      />
                    ) : (
                      <Film aria-hidden='true' />
                    )}
                  </div>
                  <div className='video-studio-task-info'>
                    <h3>{generation.prompt}</h3>
                    <p>{generation.model}</p>
                    <button
                      type='button'
                      className='video-studio-task-copy'
                      onClick={() => {
                        void copyToClipboard(generation.task_id)
                      }}
                      aria-label={t('Copy task ID: {{id}}', {
                        id: generation.task_id,
                      })}
                    >
                      <code>{generation.task_id}</code>
                      <Copy aria-hidden='true' />
                    </button>
                    {failureMessageKey && (
                      <p className='video-studio-failure'>
                        {t(failureMessageKey)}
                      </p>
                    )}
                  </div>
                  <div className='video-studio-task-status'>
                    <VideoGenerationStatus generation={generation} compact />
                    <time
                      dateTime={new Date(
                        generation.created_at * 1000
                      ).toISOString()}
                    >
                      {formatTimestampToDate(generation.created_at)}
                    </time>
                  </div>
                  <div className='video-studio-task-action'>
                    <Button
                      variant='outline'
                      render={
                        <Link
                          to={
                            generation.status === 'ready'
                              ? '/video-studio/library'
                              : '/video-studio/create'
                          }
                          search={{
                            task: generation.task_id,
                            generation: generation.id,
                          }}
                        />
                      }
                    >
                      {generation.status === 'ready'
                        ? t('View video')
                        : t('View task')}
                    </Button>
                  </div>
                </article>
              )
            })}
          </div>
        )}
        {query.hasNextPage && (
          <div className='video-studio-load-more'>
            <Button
              variant='outline'
              disabled={query.isFetchingNextPage}
              onClick={() => void query.fetchNextPage()}
            >
              {query.isFetchingNextPage && (
                <LoaderCircle
                  className='animate-spin motion-reduce:animate-none'
                  aria-hidden='true'
                />
              )}
              {query.isFetchNextPageError
                ? t('Retry loading more videos')
                : t('videoStudio.loadMore')}
            </Button>
          </div>
        )}
      </div>
    </main>
  )
}

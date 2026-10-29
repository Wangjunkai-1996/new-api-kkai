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
import {
  ArrowDown,
  ArrowRight,
  Film,
  LoaderCircle,
  RotateCw,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import emptyArtwork from '@/assets/video-works/empty.webp'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { toIntlLocale } from '@/i18n/languages'
import { signedStudioMediaUrl, useStudioMediaUrls } from '@/lib/studio-media'
import { useAuthStore } from '@/stores/auth-store'

import { VideoGenerationStatus } from './components/video-generation-status'
import { VideoStudioNav } from './components/video-studio-nav'
import { useVideoGenerations } from './queries'
import type { VideoGeneration } from './types'
import { getVideoGenerationFailureMessageKey } from './video-domain'
import { VideoTaskDetail } from './video-task-detail'

import './video-tasks.css'

export function VideoTasksPage() {
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const sessionId = useAuthStore((state) => state.auth.session?.sid ?? '')
  return <VideoTasksContent key={`${userId}:${sessionId}`} />
}

function VideoTasksContent() {
  const { t, i18n } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  const query = useVideoGenerations({}, true)
  const mediaQuery = useStudioMediaUrls(
    query.items.map((generation) => generation.poster_url)
  )
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [sheetOpen, setSheetOpen] = useState(false)
  const [previewOpen, setPreviewOpen] = useState(false)
  const [compact, setCompact] = useState(false)
  const pageRef = useRef<HTMLElement>(null)
  const selected =
    query.items.find((generation) => generation.id === selectedId) ??
    query.items[0]
  const posterUrl =
    selected && !mediaQuery.identityDenied
      ? signedStudioMediaUrl(
          selected.poster_url,
          mediaQuery.data?.urls,
          mediaQuery.data?.deniedPaths,
          mediaQuery.identityDenied
        )
      : undefined
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const modes: Record<VideoGeneration['mode'], string> = {
    text_to_video: t('Text to video'),
    image_to_video: t('Image to video'),
    first_last_frame: t('First and last frames'),
  }

  useEffect(() => {
    const page = pageRef.current
    if (!page) return
    const observer = new ResizeObserver(([entry]) => {
      const nextCompact = entry.contentRect.width <= 1100
      setCompact(nextCompact)
      if (!nextCompact) setSheetOpen(false)
    })
    observer.observe(page)
    return () => observer.disconnect()
  }, [])

  return (
    <main
      ref={pageRef}
      id='content'
      className='video-studio-v2 video-studio-page video-tasks-page'
    >
      <VideoStudioNav />
      <div className='video-studio-page-content'>
        <header className='video-task-heading'>
          <div>
            <span className='video-task-kicker'>{t('Task queue')}</span>
            <h2>{t('videoStudio.tasks')}</h2>
          </div>
          <Button
            variant='outline'
            className='video-task-refresh'
            aria-label={t('videoStudio.refresh')}
            onClick={() => void query.refresh()}
          >
            <RotateCw aria-hidden='true' />
            <span>{t('videoStudio.refresh')}</span>
          </Button>
        </header>

        {query.isLoading && query.items.length === 0 && (
          <div className='video-task-loading' aria-label={t('Loading')}>
            <div className='video-task-loading-queue'>
              {Array.from({ length: 3 }, (_, index) => (
                <Skeleton key={index} className='video-task-loading-row' />
              ))}
            </div>
            <Skeleton className='video-task-loading-detail' />
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

        {selected && (
          <div className='video-task-workspace'>
            <section
              className='video-task-queue'
              aria-label={t('Recent tasks')}
            >
              <div className='video-task-queue-heading'>
                <h3>{t('Recent tasks')}</h3>
                <span>
                  {t('Loaded: {{count}}', { count: query.items.length })}
                </span>
              </div>
              <div className='video-task-list'>
                {query.items.map((generation) => {
                  const taskPosterUrl = mediaQuery.identityDenied
                    ? undefined
                    : signedStudioMediaUrl(
                        generation.poster_url,
                        mediaQuery.data?.urls,
                        mediaQuery.data?.deniedPaths,
                        mediaQuery.identityDenied
                      )
                  const failureKey =
                    getVideoGenerationFailureMessageKey(generation)
                  const time = new Date(generation.created_at * 1000)
                  return (
                    <button
                      key={generation.id}
                      type='button'
                      className='video-task-row'
                      aria-pressed={selected.id === generation.id}
                      onClick={() => {
                        setSelectedId(generation.id)
                        if (compact) setSheetOpen(true)
                      }}
                    >
                      <TaskCover key={taskPosterUrl} url={taskPosterUrl} />
                      <span className='video-task-copy'>
                        <span className='video-task-topline'>
                          <VideoGenerationStatus
                            generation={generation}
                            compact
                            icon
                          />
                          <time dateTime={time.toISOString()}>
                            {time.toLocaleString(locale, {
                              month: 'short',
                              day: 'numeric',
                              hour: '2-digit',
                              minute: '2-digit',
                            })}
                          </time>
                        </span>
                        <span className='video-task-summary'>
                          {generation.prompt}
                        </span>
                        <span className='video-task-meta'>
                          <span>{generation.model}</span>
                          <span aria-hidden='true'>·</span>
                          <span>{modes[generation.mode]}</span>
                        </span>
                        <span className='video-task-bottom'>
                          {failureKey ? t(failureKey) : t('Task Details')}
                          <ArrowRight aria-hidden='true' />
                        </span>
                      </span>
                    </button>
                  )
                })}
              </div>
              {query.hasNextPage && (
                <div className='video-task-load-more'>
                  <Button
                    variant='ghost'
                    disabled={query.isFetchingNextPage}
                    onClick={() => void query.fetchNextPage()}
                  >
                    {query.isFetchNextPageError
                      ? t('Retry loading more videos')
                      : t('videoStudio.loadMore')}
                    {query.isFetchingNextPage ? (
                      <LoaderCircle
                        className='animate-spin motion-reduce:animate-none'
                        aria-hidden='true'
                      />
                    ) : (
                      <ArrowDown aria-hidden='true' />
                    )}
                  </Button>
                </div>
              )}
            </section>
            <section
              className='video-task-detail'
              aria-label={t('Task Details')}
            >
              <VideoTaskDetail
                generation={selected}
                posterUrl={posterUrl}
                locale={locale}
                onCopyTask={() => void copyToClipboard(selected.task_id)}
                onPreview={() => setPreviewOpen(true)}
              />
            </section>
          </div>
        )}
      </div>

      <Sheet open={sheetOpen && compact} onOpenChange={setSheetOpen}>
        <SheetContent
          side='bottom'
          showCloseButton={false}
          className='video-studio-v2 video-task-sheet'
        >
          <SheetTitle className='sr-only'>{t('Task Details')}</SheetTitle>
          {selected && (
            <VideoTaskDetail
              generation={selected}
              posterUrl={posterUrl}
              locale={locale}
              mobile
              onClose={() => setSheetOpen(false)}
              onCopyTask={() => void copyToClipboard(selected.task_id)}
              onPreview={() => setPreviewOpen(true)}
            />
          )}
        </SheetContent>
      </Sheet>

      <Dialog
        open={previewOpen && Boolean(posterUrl)}
        onOpenChange={setPreviewOpen}
        title={t('Cover preview')}
        contentClassName='video-studio-v2 video-task-preview-dialog sm:max-w-[1100px]'
      >
        {posterUrl && <img src={posterUrl} alt={t('Video cover')} />}
      </Dialog>
    </main>
  )
}

function TaskCover(props: { url?: string }) {
  const [failed, setFailed] = useState(false)
  if (!props.url || failed) {
    return (
      <span className='video-task-placeholder'>
        <Film aria-hidden='true' />
      </span>
    )
  }
  return (
    <img
      className='video-task-cover'
      src={props.url}
      alt=''
      loading='lazy'
      decoding='async'
      onError={() => setFailed(true)}
    />
  )
}

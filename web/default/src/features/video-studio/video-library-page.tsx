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
import { CircleAlert, LoaderCircle, RotateCw, Trash2, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import emptyArtwork from '@/assets/video-works/empty.webp'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { toIntlLocale } from '@/i18n/languages'
import { signedStudioMediaUrl, useStudioMediaUrls } from '@/lib/studio-media'
import { useAuthStore } from '@/stores/auth-store'

import { VideoGenerationCard } from './components/video-generation-card'
import { VideoGenerationDetails } from './components/video-generation-details'
import { VideoStudioNav } from './components/video-studio-nav'
import { useVideoGenerationTarget } from './hooks/use-video-generation-target'
import { useDeleteVideoGeneration, useVideoGenerations } from './queries'
import type { VideoGeneration } from './types'
import {
  acceptVideoLibraryOrder,
  type LibraryOrder,
} from './video-library-order'

type VideoLibraryPageProps = {
  targetTaskId?: string
  targetGenerationId?: number
  onTargetChange?: (target: { task?: string; generation?: number }) => void
}

export function VideoLibraryPage(props: VideoLibraryPageProps) {
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const sessionId = useAuthStore((state) => state.auth.session?.sid ?? '')
  return (
    <VideoLibraryContent
      key={`${userId}:${sessionId}`}
      {...props}
      userId={userId}
    />
  )
}

function VideoLibraryContent(
  props: VideoLibraryPageProps & { userId: number }
) {
  const { t, i18n } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  const [status, setStatus] = useState<'' | 'ready'>('')
  const query = useVideoGenerations(
    { status: status || undefined },
    true,
    props.targetTaskId,
    props.targetGenerationId
  )
  const target = useVideoGenerationTarget({
    generationsQuery: query,
    targetTaskId: props.targetTaskId,
    targetGenerationId: props.targetGenerationId,
  })
  const [order, setOrder] = useState<LibraryOrder>({
    status,
    ids: [],
    pageCount: 0,
    initialized: false,
  })
  const nextOrder = acceptVideoLibraryOrder(
    order,
    query.items,
    query.data?.pages.slice(order.pageCount).flatMap((page) => page.items) ??
      [],
    query.data?.pages.length ?? 0,
    status,
    query.isSuccess
  )
  if (nextOrder !== order) setOrder(nextOrder)
  const [detailsOpen, setDetailsOpen] = useState(
    Boolean(props.targetTaskId || props.targetGenerationId)
  )
  const [observedTarget, setObservedTarget] = useState(
    `${props.targetTaskId ?? ''}:${props.targetGenerationId ?? ''}`
  )
  const incomingTarget = `${props.targetTaskId ?? ''}:${props.targetGenerationId ?? ''}`
  if (observedTarget !== incomingTarget) {
    setObservedTarget(incomingTarget)
    setDetailsOpen(Boolean(props.targetTaskId || props.targetGenerationId))
  }
  const [deleteTarget, setDeleteTarget] = useState<VideoGeneration | null>(null)
  const deleteMutation = useDeleteVideoGeneration()
  const rootRef = useRef<HTMLDivElement>(null)
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const itemsById = new Map(query.items.map((item) => [item.id, item]))
  const generations = nextOrder.ids.flatMap((id) => {
    const generation = itemsById.get(id)
    return generation ? [generation] : []
  })
  const acceptedIds = new Set(nextOrder.ids)
  const newCount = query.items.filter(
    (item) => !acceptedIds.has(item.id)
  ).length
  const rawSelected = detailsOpen ? target.generation : null
  const mediaQuery = useStudioMediaUrls([
    ...generations.map((item) => item.poster_url),
    rawSelected?.poster_url,
    rawSelected?.video_url,
    rawSelected?.download_url,
  ])
  const selected = rawSelected
    ? {
        ...rawSelected,
        video_url: signedStudioMediaUrl(
          rawSelected.video_url,
          mediaQuery.data?.urls,
          mediaQuery.data?.deniedPaths,
          mediaQuery.identityDenied
        ),
        poster_url: signedStudioMediaUrl(
          rawSelected.poster_url,
          mediaQuery.data?.urls,
          mediaQuery.data?.deniedPaths,
          mediaQuery.identityDenied
        ),
        download_url: signedStudioMediaUrl(
          rawSelected.download_url,
          mediaQuery.data?.urls,
          mediaQuery.data?.deniedPaths,
          mediaQuery.identityDenied
        ),
      }
    : null
  const denied =
    mediaQuery.identityDenied ||
    Boolean(
      rawSelected?.video_url &&
      mediaQuery.data?.deniedPaths[rawSelected.video_url]
    )
  const posterDenied =
    mediaQuery.identityDenied ||
    Boolean(
      rawSelected?.poster_url &&
      mediaQuery.data?.deniedPaths[rawSelected.poster_url]
    )
  const groups = useMemo(() => {
    const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
    const dateFormatter = new Intl.DateTimeFormat(locale, {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
    })
    const grouped = new Map<string, VideoGeneration[]>()
    for (const generation of generations) {
      const date = dateFormatter.format(new Date(generation.created_at * 1000))
      grouped.set(date, [...(grouped.get(date) ?? []), generation])
    }
    return [...grouped]
  }, [generations, i18n.language, i18n.resolvedLanguage])

  const openDetails = (generation: VideoGeneration) => {
    target.selectGeneration(generation.id)
    props.onTargetChange?.({
      task: generation.task_id,
      generation: generation.id,
    })
    setDetailsOpen(true)
  }
  const closeDetails = () => {
    setDetailsOpen(false)
    props.onTargetChange?.({})
  }
  const refresh = async () => {
    await query.refresh()
    if (!alive.current) return
    // The next successful render uses the current query objects in their newest order.
    setOrder((current) => ({ ...current, initialized: false }))
  }
  const download = async () => {
    if (!rawSelected) return
    const result = await mediaQuery.refetch()
    if (!alive.current) return
    const url = signedStudioMediaUrl(
      rawSelected.download_url,
      result.data?.urls,
      result.data?.deniedPaths
    )
    if (
      !url ||
      result.isError ||
      result.data?.deniedPaths[rawSelected.download_url ?? '']
    ) {
      toast.error(t('Video download is temporarily unavailable.'))
      return
    }
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = ''
    anchor.rel = 'noopener noreferrer'
    anchor.click()
  }
  const confirmDelete = async () => {
    if (!deleteTarget || deleteMutation.isPending) return
    const deleted =
      query.targetGeneration?.id === deleteTarget.id
        ? query.targetGeneration
        : query.items.find((item) => item.id === deleteTarget.id)
    if (
      !deleted ||
      (deleted.status !== 'ready' && deleted.status !== 'failed')
    ) {
      setDeleteTarget(null)
      void query.refresh()
      toast.error(
        t(
          'This video can no longer be deleted. Refresh its status and try again.'
        )
      )
      return
    }
    const position = nextOrder.ids.indexOf(deleted.id)
    const adjacentId =
      nextOrder.ids[position + 1] ?? nextOrder.ids[position - 1]
    try {
      await deleteMutation.mutateAsync(deleted.id)
      if (!alive.current) return
      await query.forgetObservedGeneration(deleted.id)
      if (!alive.current) return
      setDeleteTarget(null)
      setOrder((current) => ({
        ...current,
        ids: current.ids.filter((id) => id !== deleted.id),
      }))
      if (target.selectedId === deleted.id) closeDetails()
      toast.success(t('videoStudio.deleted'))
      window.requestAnimationFrame(() => {
        if (!alive.current) return
        const adjacent = rootRef.current?.querySelector<HTMLButtonElement>(
          `[data-video-generation-id="${adjacentId}"]`
        )
        ;(adjacent ?? rootRef.current)?.focus()
      })
    } catch {
      if (alive.current) toast.error(t('videoStudio.deleteFailed'))
    }
  }
  const targetProblem = target.explicitTarget && target.status !== 'ready'

  return (
    <main id='content' className='video-studio-v2 video-studio-page'>
      <VideoStudioNav />
      <div className='video-studio-page-content' ref={rootRef} tabIndex={-1}>
        <header className='video-studio-page-toolbar'>
          <div>
            <h2>{t('videoStudio.library')}</h2>
            <p>{t('Your videos, organized by creation date.')}</p>
          </div>
          <div className='video-studio-toolbar-actions'>
            <div
              className='video-studio-segmented'
              role='group'
              aria-label={t('Filter videos')}
            >
              <button
                type='button'
                aria-pressed={status === ''}
                onClick={() => setStatus('')}
              >
                {t('All')}
              </button>
              <button
                type='button'
                aria-pressed={status === 'ready'}
                onClick={() => setStatus('ready')}
              >
                {t('Playable')}
              </button>
            </div>
            <Button
              variant='ghost'
              size='icon'
              onClick={() => void refresh()}
              aria-label={t('videoStudio.refresh')}
            >
              <RotateCw aria-hidden='true' />
            </Button>
          </div>
        </header>
        {newCount > 0 && (
          <div className='video-studio-notice' role='status'>
            <span>
              {t('{{count}} new videos are available.', { count: newCount })}
            </span>
            <Button variant='ghost' onClick={() => void refresh()}>
              {t('Show new videos')}
            </Button>
          </div>
        )}
        {targetProblem && (
          <div className='video-studio-notice' role='status'>
            {target.status === 'loading' || target.status === 'locating' ? (
              <LoaderCircle
                className='animate-spin motion-reduce:animate-none'
                aria-hidden='true'
              />
            ) : (
              <CircleAlert aria-hidden='true' />
            )}
            <div>
              <p>
                {target.status === 'loading' || target.status === 'locating'
                  ? t('videoStudio.generationLocating')
                  : t('Video result is not available yet.')}
              </p>
              {target.status === 'missing' && (
                <p>{t('videoStudio.generationNotVisibleDescription')}</p>
              )}
              {target.status === 'unavailable' && (
                <p>
                  {t('This video was deleted or you no longer have access.')}
                </p>
              )}
              <code>{props.targetTaskId ?? props.targetGenerationId}</code>
            </div>
            <Button variant='ghost' onClick={target.retryTargetDiscovery}>
              {t('videoStudio.refresh')}
            </Button>
            <Button
              variant='ghost'
              size='icon'
              onClick={closeDetails}
              aria-label={t('Close')}
            >
              <X aria-hidden='true' />
            </Button>
          </div>
        )}
        {query.isError && (
          <div className='video-studio-notice' role='alert'>
            <span>
              {generations.length
                ? t('videoStudio.refreshFailed')
                : t('videoStudio.libraryFailedDescription')}
            </span>
            <Button variant='outline' onClick={() => void query.refresh()}>
              {t('Retry')}
            </Button>
          </div>
        )}
        {query.isLoading && generations.length === 0 && (
          <div className='video-studio-library-grid'>
            {Array.from({ length: 6 }, (_, index) => (
              <Skeleton key={index} className='aspect-[4/3] rounded-xl' />
            ))}
          </div>
        )}
        {query.isSuccess && generations.length === 0 && newCount === 0 && (
          <div className='video-studio-empty'>
            <img src={emptyArtwork} alt='' />
            <h3>
              {status === 'ready'
                ? t('No playable videos yet.')
                : t('videoStudio.libraryEmpty')}
            </h3>
            <p>
              {status === 'ready'
                ? t(
                    'Completed videos will appear here when their files are ready.'
                  )
                : t('videoStudio.libraryEmptyDescription')}
            </p>
            {status === 'ready' ? (
              <Button variant='outline' onClick={() => setStatus('')}>
                {t('Show all videos')}
              </Button>
            ) : (
              <Button render={<Link to='/video-studio/create' />}>
                {t('videoStudio.create')}
              </Button>
            )}
          </div>
        )}
        {groups.map(([date, items]) => (
          <section key={date} aria-label={date}>
            <h3 className='video-studio-library-date'>{date}</h3>
            <div className='video-studio-library-grid'>
              {items.map((generation) => (
                <VideoGenerationCard
                  key={generation.id}
                  generation={{
                    ...generation,
                    poster_url: mediaQuery.identityDenied
                      ? undefined
                      : signedStudioMediaUrl(
                          generation.poster_url,
                          mediaQuery.data?.urls,
                          mediaQuery.data?.deniedPaths,
                          mediaQuery.identityDenied
                        ),
                  }}
                  highlighted={
                    detailsOpen && target.selectedId === generation.id
                  }
                  onOpen={openDetails}
                  onDelete={setDeleteTarget}
                />
              ))}
            </div>
          </section>
        ))}
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
      <VideoGenerationDetails
        generation={selected}
        open={detailsOpen && Boolean(selected)}
        onOpenChange={(open) => {
          if (!open) closeDetails()
        }}
        active={detailsOpen && !deleteTarget}
        mediaIdentity={`${props.userId}:${rawSelected?.id ?? ''}`}
        denied={denied}
        posterDenied={posterDenied}
        onRefresh={async () => {
          const result = await mediaQuery.refetch()
          if (result.error) return undefined
          return signedStudioMediaUrl(
            rawSelected?.video_url,
            result.data?.urls,
            result.data?.deniedPaths
          )
        }}
        onCopyTask={(generation) => {
          void copyToClipboard(generation.task_id)
        }}
        onDownload={() => {
          void download()
        }}
        onDelete={setDeleteTarget}
      />
      <AlertDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => {
          if (!open && !deleteMutation.isPending) setDeleteTarget(null)
        }}
      >
        <AlertDialogContent className='video-studio-dialog'>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('videoStudio.deleteTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('videoStudio.deleteDescription')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {deleteTarget && (
            <p className='text-sm break-words'>{deleteTarget.prompt}</p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t('videoStudio.cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending}
              onClick={() => void confirmDelete()}
            >
              {deleteMutation.isPending ? (
                <LoaderCircle
                  className='animate-spin motion-reduce:animate-none'
                  aria-hidden='true'
                />
              ) : (
                <Trash2 aria-hidden='true' />
              )}
              {t('videoStudio.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </main>
  )
}

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
import {
  CircleAlert,
  FolderOpen,
  LoaderCircle,
  RotateCw,
  Trash2,
  X,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'

import { VideoGenerationCard } from './components/video-generation-card'
import { VideoStudioNav } from './components/video-studio-nav'
import { useDeleteVideoGeneration, useVideoGenerations } from './queries'
import type { VideoGeneration } from './types'

type VideoLibraryPageProps = {
  targetTaskId?: string
  onClearTarget?: () => void
}

const TARGET_DISCOVERY_TIMEOUT_MS = 30_000

export function VideoLibraryPage(props: VideoLibraryPageProps) {
  const { t } = useTranslation()
  const [targetWaitExpired, setTargetWaitExpired] = useState(false)
  const [targetWaitAttempt, setTargetWaitAttempt] = useState(0)
  const generationsQuery = useVideoGenerations(
    {},
    true,
    targetWaitExpired ? undefined : props.targetTaskId
  )
  const deleteMutation = useDeleteVideoGeneration()
  const [deleteTarget, setDeleteTarget] = useState<VideoGeneration | null>(null)
  const [activePlayerId, setActivePlayerId] = useState<number | null>(null)
  const targetCardRef = useRef<HTMLDivElement>(null)
  const scrolledTaskRef = useRef<string | null>(null)
  const generations = useMemo(() => {
    const seenTaskIds = new Set<string>()
    return (
      generationsQuery.data?.pages
        .flatMap((page) => page.items)
        .filter((generation) => {
          if (seenTaskIds.has(generation.task_id)) return false
          seenTaskIds.add(generation.task_id)
          return true
        }) ?? []
    )
  }, [generationsQuery.data])
  const targetGeneration = props.targetTaskId
    ? generations.find(
        (generation) => generation.task_id === props.targetTaskId
      )
    : undefined
  const targetFound = Boolean(targetGeneration)
  const targetPending = Boolean(
    props.targetTaskId && !targetFound && !targetWaitExpired
  )
  const targetMissing = Boolean(
    props.targetTaskId && !targetFound && targetWaitExpired
  )

  useEffect(() => {
    setTargetWaitExpired(false)
    if (!props.targetTaskId || targetFound) return

    const timeout = window.setTimeout(
      () => setTargetWaitExpired(true),
      TARGET_DISCOVERY_TIMEOUT_MS
    )
    return () => window.clearTimeout(timeout)
  }, [props.targetTaskId, targetFound, targetWaitAttempt])

  useEffect(() => {
    if (
      !props.targetTaskId ||
      !targetGeneration ||
      scrolledTaskRef.current === props.targetTaskId
    ) {
      return
    }

    const card = targetCardRef.current
    if (!card) return
    scrolledTaskRef.current = props.targetTaskId
    const frame = window.requestAnimationFrame(() => {
      const reduceMotion = window.matchMedia(
        '(prefers-reduced-motion: reduce)'
      ).matches
      card.scrollIntoView({
        behavior: reduceMotion ? 'auto' : 'smooth',
        block: 'nearest',
      })
    })
    return () => window.cancelAnimationFrame(frame)
  }, [props.targetTaskId, targetGeneration])

  useEffect(() => {
    if (activePlayerId === null) return
    const activeGeneration = generations.find(
      (generation) => generation.id === activePlayerId
    )
    if (
      !activeGeneration ||
      activeGeneration.status !== 'ready' ||
      !activeGeneration.video_url
    ) {
      setActivePlayerId(null)
    }
  }, [activePlayerId, generations])

  const confirmDelete = async () => {
    if (!deleteTarget) return
    try {
      if (activePlayerId === deleteTarget.id) setActivePlayerId(null)
      await deleteMutation.mutateAsync(deleteTarget.id)
      if (deleteTarget.task_id === props.targetTaskId) props.onClearTarget?.()
      toast.success(t('videoStudio.deleted'))
      setDeleteTarget(null)
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('videoStudio.deleteFailed')
      )
    }
  }

  const retryTargetDiscovery = () => {
    setTargetWaitAttempt((attempt) => attempt + 1)
    void generationsQuery.refetch()
  }

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
            onClick={() => generationsQuery.refetch()}
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
        {targetPending && !generationsQuery.isError && (
          <div
            className='border-primary/30 bg-primary/5 mb-3 flex min-h-20 items-center gap-3 rounded-lg border px-4 py-3'
            role='status'
            aria-live='polite'
          >
            <LoaderCircle
              className='text-primary size-5 shrink-0 animate-spin motion-reduce:animate-none'
              aria-hidden='true'
            />
            <div className='min-w-0 flex-1'>
              <p className='text-sm font-medium'>
                {t('videoStudio.generationLocating')}
              </p>
              <code className='text-muted-foreground block truncate text-xs'>
                {props.targetTaskId}
              </code>
            </div>
            <Button
              size='icon-sm'
              variant='ghost'
              onClick={() => generationsQuery.refetch()}
              aria-label={t('videoStudio.refresh')}
            >
              <RotateCw aria-hidden='true' />
            </Button>
          </div>
        )}

        {targetMissing && !generationsQuery.isError && (
          <div
            className='border-warning/40 bg-warning/5 mb-3 flex min-h-20 items-center gap-3 rounded-lg border px-4 py-3'
            role='alert'
          >
            <CircleAlert
              className='text-warning size-5 shrink-0'
              aria-hidden='true'
            />
            <div className='min-w-0 flex-1'>
              <p className='text-sm font-medium'>
                {t('videoStudio.generationNotVisible')}
              </p>
              <p className='text-muted-foreground text-xs'>
                {t('videoStudio.generationNotVisibleDescription')}
              </p>
              <code className='text-muted-foreground mt-1 block truncate text-xs'>
                {props.targetTaskId}
              </code>
            </div>
            <div className='flex shrink-0 items-center gap-1'>
              <Button
                size='icon-sm'
                variant='ghost'
                onClick={retryTargetDiscovery}
                aria-label={t('videoStudio.retry')}
              >
                <RotateCw aria-hidden='true' />
              </Button>
              {props.onClearTarget && (
                <Button
                  size='icon-sm'
                  variant='ghost'
                  onClick={props.onClearTarget}
                  aria-label={t('Close')}
                >
                  <X aria-hidden='true' />
                </Button>
              )}
            </div>
          </div>
        )}

        {generationsQuery.isLoading &&
          generations.length === 0 &&
          !props.targetTaskId && (
            <div className='grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4'>
              {Array.from({ length: 8 }, (_, index) => (
                <Skeleton
                  key={`library-skeleton-${index}`}
                  className='aspect-[4/5] w-full rounded-lg'
                />
              ))}
            </div>
          )}

        {generationsQuery.isError && generations.length === 0 && (
          <Empty className='min-h-72 border'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <FolderOpen aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>{t('videoStudio.libraryFailed')}</EmptyTitle>
              <EmptyDescription>
                {t('videoStudio.libraryFailedDescription')}
                {props.targetTaskId && (
                  <code className='mt-2 block'>{props.targetTaskId}</code>
                )}
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button
                variant='outline'
                onClick={() => generationsQuery.refetch()}
              >
                <RotateCw aria-hidden='true' />
                {t('videoStudio.retry')}
              </Button>
            </EmptyContent>
          </Empty>
        )}

        {!generationsQuery.isLoading &&
          !generationsQuery.isError &&
          generations.length === 0 &&
          !props.targetTaskId && (
            <Empty className='min-h-72 border'>
              <EmptyHeader>
                <EmptyMedia variant='icon'>
                  <FolderOpen aria-hidden='true' />
                </EmptyMedia>
                <EmptyTitle>{t('videoStudio.libraryEmpty')}</EmptyTitle>
                <EmptyDescription>
                  {t('videoStudio.libraryEmptyDescription')}
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button render={<a href='/video-studio/create' />}>
                  {t('videoStudio.create')}
                </Button>
              </EmptyContent>
            </Empty>
          )}

        {generationsQuery.isError && generations.length > 0 && (
          <div
            className='border-destructive/30 bg-destructive/5 text-destructive mb-3 flex items-center justify-between gap-3 rounded-lg border px-3 py-2 text-xs'
            role='alert'
          >
            <span className='min-w-0'>
              {t('videoStudio.refreshFailed')}
              {targetPending && (
                <code className='ml-2'>{props.targetTaskId}</code>
              )}
            </span>
            <Button
              size='sm'
              variant='outline'
              onClick={() => generationsQuery.refetch()}
            >
              {t('videoStudio.retry')}
            </Button>
          </div>
        )}

        {generations.length > 0 && (
          <div className='grid grid-cols-1 items-start gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4'>
            {generations.map((generation) => {
              const highlighted = generation.task_id === props.targetTaskId
              return (
                <div
                  key={generation.task_id}
                  ref={highlighted ? targetCardRef : undefined}
                >
                  <VideoGenerationCard
                    generation={generation}
                    highlighted={highlighted}
                    playing={activePlayerId === generation.id}
                    onPlay={(target) => setActivePlayerId(target.id)}
                    onClose={() => setActivePlayerId(null)}
                    onDelete={setDeleteTarget}
                  />
                </div>
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

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <Trash2 aria-hidden='true' />
            </AlertDialogMedia>
            <AlertDialogTitle>{t('videoStudio.deleteTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('videoStudio.deleteDescription')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('videoStudio.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending}
              onClick={() => void confirmDelete()}
            >
              {deleteMutation.isPending && (
                <LoaderCircle
                  className='animate-spin motion-reduce:animate-none'
                  aria-hidden='true'
                />
              )}
              {t('videoStudio.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </main>
  )
}

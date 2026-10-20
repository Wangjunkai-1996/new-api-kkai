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
  ArrowUp,
  FolderOpen,
  LoaderCircle,
  RotateCw,
  Sparkles,
} from 'lucide-react'
import { useReducedMotion } from 'motion/react'
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
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { signedStudioMediaUrl, useStudioMediaUrls } from '@/lib/studio-media'
import { useAuthStore } from '@/stores/auth-store'

import { ImageGenerationCard } from './components/image-generation-card'
import { ImageGenerationDetails } from './components/image-generation-details'
import { ImageMasonry } from './components/image-masonry'
import { ImageRegenerationSheet } from './components/image-regeneration-sheet'
import { ImageStudioNav } from './components/image-studio-nav'
import {
  useDeleteImageGeneration,
  useImageGeneration,
  useImageGenerations,
} from './queries'
import type { ImageGeneration, ImageGenerationStatus } from './types'

import './image-library.css'

export function ImageLibraryPage(props: {
  targetGenerationId?: number
  onClearTarget?: () => void
}) {
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  return <ImageLibraryContent key={userId} {...props} />
}

function ImageLibraryContent(props: {
  targetGenerationId?: number
  onClearTarget?: () => void
}) {
  const { t } = useTranslation()
  const reduceMotion = useReducedMotion()
  const [status, setStatus] = useState<ImageGenerationStatus | undefined>()
  const query = useImageGenerations(status ? { status } : {})
  const deleteMutation = useDeleteImageGeneration()
  const [deleteTarget, setDeleteTarget] = useState<ImageGeneration | null>(null)
  const [detailsTarget, setDetailsTarget] = useState<ImageGeneration | null>(
    null
  )
  const detailsQuery = useImageGeneration(detailsTarget?.id)
  const [regenerationTarget, setRegenerationTarget] =
    useState<ImageGeneration | null>(null)
  const [submittedGeneration, setSubmittedGeneration] =
    useState<ImageGeneration | null>(null)
  const [submittedTargetId, setSubmittedTargetId] = useState<number>()
  const [nearTop, setNearTop] = useState(true)
  const scrollRef = useRef<HTMLDivElement>(null)
  const galleryRef = useRef<HTMLDivElement>(null)
  const scrolledRef = useRef<number | undefined>(undefined)
  const targetId = submittedTargetId ?? props.targetGenerationId
  const generations = useMemo(() => {
    const byId = new Map<number, ImageGeneration>()
    if (
      submittedGeneration &&
      (!status || submittedGeneration.status === status)
    ) {
      byId.set(submittedGeneration.id, submittedGeneration)
    }
    for (const page of query.data?.pages ?? []) {
      for (const generation of page.items) byId.set(generation.id, generation)
    }
    return [...byId.values()].sort(
      (left, right) => right.created_at - left.created_at || right.id - left.id
    )
  }, [query.data, status, submittedGeneration])
  const details = detailsTarget
    ? (detailsQuery.data ??
      generations.find((generation) => generation.id === detailsTarget.id) ??
      detailsTarget)
    : null
  const mediaPaths = useMemo(
    () =>
      [...generations, ...(details ? [details] : [])].flatMap((generation) =>
        generation.assets.flatMap((asset) => [
          asset.content_url,
          asset.thumbnail_url,
          asset.download_url,
        ])
      ),
    [details, generations]
  )
  const mediaQuery = useStudioMediaUrls(mediaPaths)
  const [mediaSnapshot, setMediaSnapshot] = useState({
    source: mediaQuery.data?.urls,
    urls: mediaQuery.data?.urls ?? {},
  })
  if (mediaSnapshot.source !== mediaQuery.data?.urls) {
    setMediaSnapshot({
      source: mediaQuery.data?.urls,
      urls: { ...mediaSnapshot.urls, ...mediaQuery.data?.urls },
    })
  }
  const mediaUrls = mediaSnapshot.urls
  const signedGenerations = useMemo(
    () =>
      generations.map((generation) => ({
        ...generation,
        assets: generation.assets.map((asset) => ({
          ...asset,
          content_url: signedStudioMediaUrl(asset.content_url, mediaUrls),
          thumbnail_url: signedStudioMediaUrl(asset.thumbnail_url, mediaUrls),
          download_url: signedStudioMediaUrl(asset.download_url, mediaUrls),
        })),
      })),
    [generations, mediaUrls]
  )
  const detailsGeneration = useMemo(
    () =>
      details && {
        ...details,
        assets: details.assets.map((asset) => ({
          ...asset,
          content_url: signedStudioMediaUrl(asset.content_url, mediaUrls),
          thumbnail_url: signedStudioMediaUrl(asset.thumbnail_url, mediaUrls),
          download_url: signedStudioMediaUrl(asset.download_url, mediaUrls),
        })),
      },
    [details, mediaUrls]
  )
  const [wallSnapshot, setWallSnapshot] = useState({
    source: generations,
    filter: status,
    target: targetId,
    ids: generations.map((generation) => generation.id),
  })
  if (
    wallSnapshot.source !== generations ||
    wallSnapshot.filter !== status ||
    wallSnapshot.target !== targetId
  ) {
    const ids = generations.map((generation) => generation.id)
    const mayInsert =
      wallSnapshot.filter !== status ||
      nearTop ||
      wallSnapshot.target !== targetId
    const previousSet = new Set(wallSnapshot.ids)
    const firstVisibleIndex = ids.findIndex((id) => previousSet.has(id))
    const next =
      mayInsert || wallSnapshot.ids.length === 0
        ? ids
        : ids.filter(
            (id, index) =>
              previousSet.has(id) ||
              (firstVisibleIndex >= 0 && index > firstVisibleIndex)
          )
    setWallSnapshot({
      source: generations,
      filter: status,
      target: targetId,
      ids: next,
    })
  }
  const visibleIds = wallSnapshot.ids
  const visibleSet = useMemo(() => new Set(visibleIds), [visibleIds])
  const visibleGenerations = signedGenerations.filter((generation) =>
    visibleSet.has(generation.id)
  )
  const deferredCount = signedGenerations.length - visibleGenerations.length
  useEffect(() => {
    if (
      !targetId ||
      scrolledRef.current === targetId ||
      !visibleSet.has(targetId)
    ) {
      return
    }
    const card = galleryRef.current?.querySelector<HTMLElement>(
      `[data-generation-id="${String(targetId)}"]`
    )
    if (!card) return
    const frame = window.requestAnimationFrame(() => {
      card.scrollIntoView({
        behavior: reduceMotion ? 'auto' : 'smooth',
        block: 'nearest',
      })
      scrolledRef.current = targetId
    })
    return () => window.cancelAnimationFrame(frame)
  }, [targetId, visibleSet, reduceMotion])

  const confirmDelete = async (): Promise<void> => {
    if (!deleteTarget) return
    try {
      await deleteMutation.mutateAsync(deleteTarget.id)
      if (deleteTarget.id === props.targetGenerationId) props.onClearTarget?.()
      if (deleteTarget.id === submittedGeneration?.id) {
        setSubmittedGeneration(null)
      }
      if (deleteTarget.id === submittedTargetId) setSubmittedTargetId(undefined)
      if (deleteTarget.id === detailsTarget?.id) setDetailsTarget(null)
      toast.success(t('imageStudio.deleted'))
      setDeleteTarget(null)
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('imageStudio.deleteFailed')
      )
    }
  }

  const regenerate = (generation: ImageGeneration): void => {
    setDetailsTarget(null)
    setRegenerationTarget(generation)
  }

  const submitted = (generation: ImageGeneration): void => {
    setRegenerationTarget(null)
    setStatus(undefined)
    setSubmittedGeneration(generation)
    setSubmittedTargetId(generation.id)
  }

  return (
    <main
      id='content'
      className='flex size-full min-h-0 flex-col overflow-hidden'
    >
      <ImageStudioNav
        action={
          <Button
            size='icon-sm'
            variant='ghost'
            onClick={() => void query.refetch()}
            aria-label={t('Refresh')}
            className='min-h-11 min-w-11'
          >
            <RotateCw
              className={
                query.isFetching
                  ? 'animate-spin motion-reduce:animate-none'
                  : undefined
              }
              aria-hidden='true'
            />
          </Button>
        }
      />
      <div
        ref={scrollRef}
        onScroll={(event) => setNearTop(event.currentTarget.scrollTop < 160)}
        className='min-h-0 flex-1 overflow-y-auto overscroll-contain px-3 pt-5 pb-6 sm:px-5 sm:pt-6'
      >
        <div className='mb-5 flex flex-wrap items-end justify-between gap-4'>
          <div className='space-y-1.5'>
            <div className='flex items-center gap-2'>
              <Sparkles className='text-primary size-4' aria-hidden='true' />
              <h2 className='text-lg font-semibold tracking-tight'>
                {t('Your creations')}
              </h2>
              <span className='text-muted-foreground rounded-full border px-2 py-0.5 text-[10px]'>
                {generations.length}
              </span>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t('Explore your images. Keep creating.')}
            </p>
          </div>
          <NativeSelect
            className='w-40'
            value={status ?? ''}
            aria-label={t('imageStudio.filterStatus')}
            onChange={(event) =>
              setStatus(
                (event.target.value as ImageGenerationStatus) || undefined
              )
            }
          >
            <NativeSelectOption value=''>
              {t('imageStudio.allStatuses')}
            </NativeSelectOption>
            {[
              'submitting',
              'succeeded',
              'partial',
              'failed',
              'archive_failed',
              'unknown',
            ].map((value) => (
              <NativeSelectOption key={value} value={value}>
                {t(`imageStudio.status.${value}`)}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        {deferredCount > 0 && visibleGenerations.length > 0 && (
          <div
            className='sticky top-2 z-10 mb-4 flex justify-center'
            aria-live='polite'
          >
            <Button
              variant='secondary'
              className='min-h-11 rounded-full border shadow-lg'
              onClick={() => {
                setWallSnapshot((previous) => ({
                  ...previous,
                  ids: generations.map((generation) => generation.id),
                }))
                scrollRef.current?.scrollTo({
                  top: 0,
                  behavior: reduceMotion ? 'auto' : 'smooth',
                })
              }}
            >
              <ArrowUp aria-hidden='true' />
              {t('Show {{count}} new batches', { count: deferredCount })}
            </Button>
          </div>
        )}
        {query.isLoading && generations.length === 0 && (
          <div className='grid grid-cols-2 items-start gap-3 md:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5'>
            {Array.from({ length: 10 }, (_, index) => (
              <Skeleton
                key={index}
                className={
                  index % 3 === 0
                    ? 'aspect-[3/4] rounded-2xl motion-reduce:animate-none'
                    : 'aspect-[4/3] rounded-2xl motion-reduce:animate-none'
                }
              />
            ))}
          </div>
        )}
        {query.isError && (
          <Empty
            className={
              generations.length > 0 ? 'mb-4 min-h-0 border py-4' : 'min-h-64'
            }
          >
            <EmptyHeader>
              {generations.length === 0 && (
                <EmptyMedia variant='icon'>
                  <FolderOpen aria-hidden='true' />
                </EmptyMedia>
              )}
              <EmptyTitle>{t('imageStudio.libraryFailed')}</EmptyTitle>
              <EmptyDescription>
                {t('imageStudio.libraryFailedDescription')}
              </EmptyDescription>
            </EmptyHeader>
            <Button variant='outline' onClick={() => void query.refetch()}>
              {t('Retry')}
            </Button>
          </Empty>
        )}
        {!query.isLoading && !query.isError && generations.length === 0 && (
          <Empty className='min-h-64'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <FolderOpen aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>{t('imageStudio.libraryEmpty')}</EmptyTitle>
              <EmptyDescription>
                {t('imageStudio.libraryEmptyDescription')}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        <div ref={galleryRef}>
          {visibleGenerations.length > 0 && (
            <ImageMasonry
              items={visibleGenerations}
              renderItem={(generation) => (
                <ImageGenerationCard
                  generation={generation}
                  highlighted={generation.id === targetId}
                  onDetails={setDetailsTarget}
                  onRegenerate={regenerate}
                />
              )}
            />
          )}
        </div>
        {query.hasNextPage && (
          <div className='flex justify-center py-6'>
            <Button
              variant='outline'
              className='min-h-11 rounded-full px-5'
              disabled={query.isFetchingNextPage}
              onClick={() => void query.fetchNextPage()}
            >
              {query.isFetchingNextPage && (
                <LoaderCircle
                  className='animate-spin motion-reduce:animate-none'
                  aria-hidden='true'
                />
              )}
              {t('imageStudio.loadMore')}
            </Button>
          </div>
        )}
      </div>
      <ImageGenerationDetails
        generation={detailsGeneration}
        refreshing={detailsQuery.isFetching}
        onClose={() => setDetailsTarget(null)}
        onRegenerate={regenerate}
        onDelete={(generation) => {
          setDetailsTarget(null)
          setDeleteTarget(generation)
        }}
        onRefresh={() => void detailsQuery.refetch()}
      />
      <ImageRegenerationSheet
        generation={regenerationTarget}
        onClose={() => setRegenerationTarget(null)}
        onSubmitted={submitted}
      />
      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <FolderOpen aria-hidden='true' />
            </AlertDialogMedia>
            <AlertDialogTitle>{t('Delete this batch')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This deletes every image in this batch. This cannot be undone.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending}
              onClick={(event) => {
                event.preventDefault()
                void confirmDelete()
              }}
            >
              {deleteMutation.isPending && (
                <LoaderCircle
                  className='animate-spin motion-reduce:animate-none'
                  aria-hidden='true'
                />
              )}
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </main>
  )
}

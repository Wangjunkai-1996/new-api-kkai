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
  Search,
  X,
  RotateCw,
  Sparkles,
} from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
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
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
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

type ImageGenerationGroup = {
  key: string
  label: string
  items: ImageGeneration[]
}

const calendarKey = (timestamp: number): string => {
  const date = new Date(timestamp * 1000)
  return [date.getFullYear(), date.getMonth() + 1, date.getDate()]
    .map((part, index) =>
      index === 0 ? String(part) : String(part).padStart(2, '0')
    )
    .join('-')
}

const isToday = (timestamp: number): boolean => {
  const now = new Date()
  return (
    calendarKey(timestamp) === calendarKey(Math.floor(now.getTime() / 1000))
  )
}

const matchesSearch = (generation: ImageGeneration, query: string): boolean => {
  if (!query) return true
  const normalized = query.toLocaleLowerCase()
  return [generation.prompt, generation.model, generation.request_id].some(
    (value) => value.toLocaleLowerCase().includes(normalized)
  )
}

const groupGenerationsByDate = (
  generations: ImageGeneration[],
  locale: string
): ImageGenerationGroup[] => {
  const groups = new Map<string, ImageGeneration[]>()
  for (const generation of generations) {
    const key = calendarKey(generation.created_at)
    const items = groups.get(key)
    if (items) items.push(generation)
    else groups.set(key, [generation])
  }
  return [...groups.entries()].map(([key, items]) => ({
    key,
    label: isToday(items[0]?.created_at ?? 0)
      ? 'Today'
      : new Intl.DateTimeFormat(toIntlLocale(locale), {
          day: 'numeric',
          month: 'long',
          year: 'numeric',
        }).format(new Date((items[0]?.created_at ?? 0) * 1000)),
    items,
  }))
}

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
  const { t, i18n } = useTranslation()
  const reduceMotion = useReducedMotion()
  const [status, setStatus] = useState<ImageGenerationStatus | undefined>()
  const [searchDraft, setSearchDraft] = useState('')
  const search = useDeferredValue(searchDraft.trim())
  const query = useImageGenerations({
    q: search || undefined,
    status,
  })
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
      (!status || submittedGeneration.status === status) &&
      matchesSearch(submittedGeneration, search)
    ) {
      byId.set(submittedGeneration.id, submittedGeneration)
    }
    for (const page of query.data?.pages ?? []) {
      for (const generation of page.items) byId.set(generation.id, generation)
    }
    return [...byId.values()].sort(
      (left, right) => right.created_at - left.created_at || right.id - left.id
    )
  }, [query.data, search, status, submittedGeneration])
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
    identityDenied: mediaQuery.identityDenied,
    deniedPaths: mediaQuery.data?.deniedPaths,
  })
  if (
    mediaSnapshot.source !== mediaQuery.data?.urls ||
    mediaSnapshot.identityDenied !== mediaQuery.identityDenied ||
    mediaSnapshot.deniedPaths !== mediaQuery.data?.deniedPaths
  ) {
    const urls = mediaQuery.identityDenied
      ? {}
      : { ...mediaSnapshot.urls, ...mediaQuery.data?.urls }
    for (const path of Object.keys(mediaQuery.data?.deniedPaths ?? {})) {
      delete urls[path]
    }
    setMediaSnapshot({
      source: mediaQuery.data?.urls,
      urls,
      identityDenied: mediaQuery.identityDenied,
      deniedPaths: mediaQuery.data?.deniedPaths,
    })
  }
  const mediaUrls = mediaQuery.identityDenied ? undefined : mediaSnapshot.urls
  const deniedPaths = mediaQuery.data?.deniedPaths
  const signedGenerations = useMemo(
    () =>
      generations.map((generation) => ({
        ...generation,
        assets: generation.assets.map((asset) => ({
          ...asset,
          content_url: signedStudioMediaUrl(
            asset.content_url,
            mediaUrls,
            deniedPaths,
            mediaQuery.identityDenied
          ),
          thumbnail_url: signedStudioMediaUrl(
            asset.thumbnail_url,
            mediaUrls,
            deniedPaths,
            mediaQuery.identityDenied
          ),
          download_url: signedStudioMediaUrl(
            asset.download_url,
            mediaUrls,
            deniedPaths,
            mediaQuery.identityDenied
          ),
        })),
      })),
    [generations, mediaUrls, deniedPaths, mediaQuery.identityDenied]
  )
  const detailsGeneration = useMemo(
    () =>
      details && {
        ...details,
        assets: details.assets.map((asset) => ({
          ...asset,
          content_url: signedStudioMediaUrl(
            asset.content_url,
            mediaUrls,
            deniedPaths,
            mediaQuery.identityDenied
          ),
          thumbnail_url: signedStudioMediaUrl(
            asset.thumbnail_url,
            mediaUrls,
            deniedPaths,
            mediaQuery.identityDenied
          ),
          download_url: signedStudioMediaUrl(
            asset.download_url,
            mediaUrls,
            deniedPaths,
            mediaQuery.identityDenied
          ),
        })),
      },
    [details, mediaUrls, deniedPaths, mediaQuery.identityDenied]
  )
  const [wallSnapshot, setWallSnapshot] = useState({
    source: generations,
    filter: status,
    search,
    target: targetId,
    ids: generations.map((generation) => generation.id),
  })
  if (
    wallSnapshot.source !== generations ||
    wallSnapshot.filter !== status ||
    wallSnapshot.search !== search ||
    wallSnapshot.target !== targetId
  ) {
    const ids = generations.map((generation) => generation.id)
    const mayInsert =
      wallSnapshot.filter !== status ||
      wallSnapshot.search !== search ||
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
      search,
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
  const dateGroups = useMemo(
    () => groupGenerationsByDate(visibleGenerations, i18n.language),
    [i18n.language, visibleGenerations]
  )
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
        <motion.div
          className='image-library-hero border-primary/10 from-primary/[0.08] via-card to-card mb-6 flex flex-col gap-5 rounded-3xl border bg-gradient-to-br p-4 shadow-sm sm:p-5'
          initial={reduceMotion ? false : { opacity: 0, y: -12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{
            duration: reduceMotion ? 0 : 0.45,
            ease: [0.22, 1, 0.36, 1],
          }}
        >
          <div className='flex flex-wrap items-end justify-between gap-4'>
            <div className='space-y-1.5'>
              <div className='flex items-center gap-2'>
                <Sparkles className='text-primary size-4' aria-hidden='true' />
                <h2 className='text-lg font-semibold tracking-tight'>
                  {t('Your creations')}
                </h2>
                <span className='text-muted-foreground bg-background/70 rounded-full border px-2 py-0.5 text-[10px]'>
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
          <div className='relative max-w-2xl'>
            <Search
              className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2'
              aria-hidden='true'
            />
            <Input
              value={searchDraft}
              onChange={(event) => setSearchDraft(event.target.value)}
              placeholder={t('Search...')}
              aria-label={t('Search')}
              className='bg-background/80 h-10 rounded-xl pr-10 pl-9 shadow-sm'
            />
            {searchDraft && (
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                className='absolute top-1/2 right-1.5 -translate-y-1/2'
                aria-label={t('Clear search')}
                onClick={() => setSearchDraft('')}
              >
                <X aria-hidden='true' />
              </Button>
            )}
          </div>
        </motion.div>
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
              <EmptyTitle>
                {search
                  ? t(
                      'No results for "{{query}}". Try adjusting your search or filters.',
                      {
                        query: search,
                      }
                    )
                  : t('imageStudio.libraryEmpty')}
              </EmptyTitle>
              <EmptyDescription>
                {search
                  ? t('No results found')
                  : t('imageStudio.libraryEmptyDescription')}
              </EmptyDescription>
            </EmptyHeader>
            {search && (
              <Button variant='outline' onClick={() => setSearchDraft('')}>
                {t('Clear search')}
              </Button>
            )}
          </Empty>
        )}
        <div ref={galleryRef} className='image-library-timeline space-y-10'>
          {dateGroups.map((group, index) => (
            <motion.section
              key={group.key}
              aria-labelledby={`image-group-${group.key}`}
              className='image-library-date-group'
              initial={reduceMotion ? false : { opacity: 0, y: 22 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{
                duration: reduceMotion ? 0 : 0.42,
                delay: reduceMotion ? 0 : Math.min(index, 8) * 0.08,
                ease: [0.22, 1, 0.36, 1],
              }}
            >
              <div className='image-library-date-heading mb-3 flex items-center gap-3'>
                <span className='image-library-date-dot' aria-hidden='true' />
                <h3
                  id={`image-group-${group.key}`}
                  className='text-sm font-semibold tracking-tight'
                >
                  {group.label === 'Today' ? t('Today') : group.label}
                </h3>
                <span className='text-muted-foreground bg-background/70 rounded-full border px-2 py-0.5 text-[10px]'>
                  {group.items.length}
                </span>
              </div>
              <ImageMasonry
                items={group.items}
                renderItem={(generation) => (
                  <ImageGenerationCard
                    generation={generation}
                    highlighted={generation.id === targetId}
                    onDetails={setDetailsTarget}
                    onRegenerate={regenerate}
                  />
                )}
              />
            </motion.section>
          ))}
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

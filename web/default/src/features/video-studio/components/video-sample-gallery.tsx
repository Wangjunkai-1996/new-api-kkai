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
import { useVirtualizer } from '@tanstack/react-virtual'
import { AxiosError } from 'axios'
import { Film, LoaderCircle, RotateCw } from 'lucide-react'
import {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'

import { useVideoSamples } from '../queries'
import type {
  VideoModelProfile,
  VideoSample,
  VideoStudioApiError,
} from '../types'
import { shouldAutoLoadNextVideoSamplePage } from '../video-domain'
import {
  VIDEO_SAMPLE_CATEGORIES,
  VIDEO_SAMPLE_CATEGORIES_ENABLED,
  VIDEO_SAMPLE_CATEGORY_LABEL_KEYS,
  type VideoSampleCategory,
} from '../video-sample-categories'
import {
  getVideoTokenErrorKind,
  type VideoTokenErrorKind,
} from '../video-token-access'
import { VideoSampleCard } from './video-sample-card'

type VideoSampleGalleryProps = {
  models: VideoModelProfile[]
  tokenId?: number | null
  selectedSampleId?: number
  onTokenError: (errorKind: VideoTokenErrorKind) => boolean
  onTrySample: (sample: VideoSample) => void
}

type VideoSamplePreviewRegistry = {
  activeId: number | null
  warmedIds: readonly number[]
}

type VideoSamplePreviewAction =
  | { type: 'warm'; id: number }
  | { type: 'start'; id: number }
  | { type: 'stop'; id: number }
  | { type: 'reset' }

const MAX_WARMED_PREVIEWS = 2

const VIDEO_SAMPLE_PREVIEW_INITIAL_STATE: VideoSamplePreviewRegistry = {
  activeId: null,
  warmedIds: [],
}

// oxlint-disable-next-line react/only-export-components -- Exported for deterministic preview ownership regression tests.
export const reduceVideoSamplePreviewRegistry = (
  state: VideoSamplePreviewRegistry,
  action: VideoSamplePreviewAction
): VideoSamplePreviewRegistry => {
  if (action.type === 'reset') return VIDEO_SAMPLE_PREVIEW_INITIAL_STATE

  if (action.type === 'stop') {
    if (state.activeId !== action.id) return state
    return { ...state, activeId: null }
  }

  const activeIds =
    state.activeId !== null && state.activeId !== action.id
      ? [state.activeId]
      : []
  const warmedIds = [
    action.id,
    ...activeIds,
    ...state.warmedIds.filter(
      (id) => id !== action.id && id !== state.activeId
    ),
  ].slice(0, MAX_WARMED_PREVIEWS)

  if (action.type === 'warm') return { ...state, warmedIds }
  return { activeId: action.id, warmedIds }
}

const getLaneCount = (width: number): number => {
  if (width < 480) return 1
  if (width < 760) return 2
  if (width < 1_080) return 3
  return 4
}

export function VideoSampleGallery(props: VideoSampleGalleryProps) {
  const { t } = useTranslation()
  const onTokenError = props.onTokenError
  const scrollRef = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [previewRegistry, dispatchPreview] = useReducer(
    reduceVideoSamplePreviewRegistry,
    VIDEO_SAMPLE_PREVIEW_INITIAL_STATE
  )
  const [modelFilter, setModelFilter] = useState('')
  const [categoryFilter, setCategoryFilter] = useState<
    VideoSampleCategory | ''
  >('')
  const samplesQuery = useVideoSamples(props.tokenId, {
    model: modelFilter || undefined,
    category: categoryFilter || undefined,
  })
  const {
    fetchNextPage,
    hasNextPage,
    isFetchNextPageError,
    isFetchingNextPage,
  } = samplesQuery
  const samples = useMemo(
    () => samplesQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [samplesQuery.data]
  )
  const lanes = getLaneCount(width)
  const gap = 12
  const columnWidth = Math.max(0, (width - gap * (lanes - 1)) / lanes)

  const warmPreview = useCallback((id: number) => {
    dispatchPreview({ type: 'warm', id })
  }, [])

  const startPreview = useCallback((id: number) => {
    dispatchPreview({ type: 'start', id })
  }, [])

  const stopPreview = useCallback((id: number) => {
    dispatchPreview({ type: 'stop', id })
  }, [])

  useEffect(() => {
    if (!samplesQuery.isError && !samplesQuery.isFetchNextPageError) return
    const responseError =
      samplesQuery.error instanceof AxiosError
        ? (samplesQuery.error.response?.data as VideoStudioApiError | undefined)
        : undefined
    onTokenError(getVideoTokenErrorKind(responseError?.code))
  }, [
    onTokenError,
    samplesQuery.error,
    samplesQuery.isError,
    samplesQuery.isFetchNextPageError,
  ])

  useEffect(() => {
    const element = scrollRef.current
    if (!element) return
    const observer = new ResizeObserver((entries) => {
      setWidth(entries[0]?.contentRect.width ?? 0)
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  // oxlint-disable-next-line react/incompatible-library -- TanStack Virtual exposes non-memoizable callbacks by design.
  const virtualizer = useVirtualizer({
    count: samples.length,
    getScrollElement: () => scrollRef.current,
    getItemKey: (index) => samples[index]?.id ?? index,
    estimateSize: (index) => {
      const ratio = Math.min(
        1.8,
        Math.max(0.55, samples[index]?.aspect_ratio || 1)
      )
      return columnWidth / ratio + 108
    },
    lanes,
    gap,
    overscan: 5,
  })
  const virtualItems = virtualizer.getVirtualItems()

  useEffect(() => {
    virtualizer.measure()
  }, [
    categoryFilter,
    columnWidth,
    lanes,
    modelFilter,
    samples.length,
    virtualizer,
  ])

  useEffect(() => {
    const lastItem = virtualItems.at(-1)
    if (
      !shouldAutoLoadNextVideoSamplePage({
        hasNextPage,
        isFetchNextPageError,
        isFetchingNextPage,
        lanes,
        lastVisibleIndex: lastItem?.index,
        sampleCount: samples.length,
      })
    ) {
      return
    }
    void fetchNextPage()
  }, [
    fetchNextPage,
    hasNextPage,
    isFetchNextPageError,
    isFetchingNextPage,
    lanes,
    samples.length,
    virtualItems,
  ])

  const initialLoading = samplesQuery.isLoading && samples.length === 0
  const initialError = samplesQuery.isError && samples.length === 0

  return (
    <section
      className='flex min-h-0 flex-1 flex-col'
      aria-labelledby='video-samples-heading'
    >
      <div className='bg-background/95 flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2.5 backdrop-blur sm:px-4'>
        <h2
          id='video-samples-heading'
          className='me-auto text-sm font-semibold'
        >
          {t('videoStudio.samples')}
        </h2>
        <NativeSelect
          size='sm'
          value={modelFilter}
          onChange={(event) => {
            dispatchPreview({ type: 'reset' })
            scrollRef.current?.scrollTo({ top: 0 })
            setModelFilter(event.target.value)
          }}
          aria-label={t('videoStudio.filterModel')}
        >
          <NativeSelectOption value=''>
            {t('videoStudio.allModels')}
          </NativeSelectOption>
          {props.models.map((profile) => (
            <NativeSelectOption key={profile.id} value={profile.model}>
              {profile.display_name}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        {VIDEO_SAMPLE_CATEGORIES_ENABLED && (
          <NativeSelect
            size='sm'
            value={categoryFilter}
            onChange={(event) => {
              dispatchPreview({ type: 'reset' })
              scrollRef.current?.scrollTo({ top: 0 })
              setCategoryFilter(event.target.value as VideoSampleCategory | '')
            }}
            aria-label={t('videoStudio.filterCategory')}
          >
            <NativeSelectOption value=''>
              {t('videoStudio.allCategories')}
            </NativeSelectOption>
            {VIDEO_SAMPLE_CATEGORIES.map((category) => (
              <NativeSelectOption key={category} value={category}>
                {t(VIDEO_SAMPLE_CATEGORY_LABEL_KEYS[category])}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        )}
      </div>

      <div
        ref={scrollRef}
        className='min-h-0 flex-1 overflow-y-auto p-3 sm:p-4'
      >
        {initialLoading && (
          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3'>
            {Array.from({ length: 9 }, (_, index) => (
              <div key={`sample-skeleton-${index}`} className='space-y-2'>
                <Skeleton className='aspect-[4/5] w-full rounded-lg' />
                <Skeleton className='h-4 w-4/5' />
                <Skeleton className='h-3 w-2/5' />
              </div>
            ))}
          </div>
        )}

        {initialError && (
          <Empty className='min-h-72 border'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <Film aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>{t('videoStudio.samplesFailed')}</EmptyTitle>
              <EmptyDescription>
                {t('videoStudio.samplesFailedDescription')}
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button
                variant='outline'
                size='sm'
                onClick={() => samplesQuery.refetch()}
              >
                <RotateCw aria-hidden='true' />
                {t('videoStudio.retry')}
              </Button>
            </EmptyContent>
          </Empty>
        )}

        {!initialLoading && !initialError && samples.length === 0 && (
          <Empty className='min-h-72 border'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <Film aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>{t('videoStudio.noSamples')}</EmptyTitle>
              <EmptyDescription>
                {t('videoStudio.noSamplesDescription')}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}

        {samples.length > 0 && width > 0 && (
          <div
            className='relative w-full'
            style={{ height: virtualizer.getTotalSize() }}
          >
            {virtualItems.map((virtualItem) => {
              const sample = samples[virtualItem.index]
              if (!sample) return null
              const x = virtualItem.lane * (columnWidth + gap)
              return (
                <div
                  key={virtualItem.key}
                  ref={virtualizer.measureElement}
                  data-index={virtualItem.index}
                  className='absolute top-0 left-0'
                  style={{
                    width: columnWidth,
                    transform: `translate3d(${x}px, ${virtualItem.start}px, 0)`,
                  }}
                >
                  <VideoSampleCard
                    sample={sample}
                    active={previewRegistry.activeId === sample.id}
                    warmed={previewRegistry.warmedIds.includes(sample.id)}
                    selected={props.selectedSampleId === sample.id}
                    onPreviewWarm={warmPreview}
                    onPreviewStart={startPreview}
                    onPreviewEnd={stopPreview}
                    onTry={props.onTrySample}
                  />
                </div>
              )
            })}
          </div>
        )}

        {samplesQuery.isFetchingNextPage && (
          <div
            className='text-muted-foreground flex items-center justify-center gap-2 py-4 text-xs'
            role='status'
          >
            <LoaderCircle
              className='size-4 animate-spin motion-reduce:animate-none'
              aria-hidden='true'
            />
            {t('videoStudio.loadingMore')}
          </div>
        )}

        {samplesQuery.isFetchNextPageError &&
          !samplesQuery.isFetchingNextPage &&
          samples.length > 0 && (
            <div
              className='text-destructive flex items-center justify-center gap-3 py-4 text-xs'
              role='alert'
            >
              <span>{t('videoStudio.refreshFailed')}</span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={samplesQuery.isFetchingNextPage}
                onClick={() => void samplesQuery.fetchNextPage()}
              >
                <RotateCw aria-hidden='true' />
                {t('videoStudio.retry')}
              </Button>
            </div>
          )}
      </div>
    </section>
  )
}

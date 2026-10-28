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
import { AxiosError } from 'axios'
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  ChevronDown,
  CircleAlert,
  Copy,
  Download,
  Film,
  LoaderCircle,
  RotateCw,
  Sparkles,
} from 'lucide-react'
import { type ReactNode, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import cinemaArt from '@/assets/video-studio/cinema-tools-still-life.webp'
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
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { toIntlLocale } from '@/i18n/languages'
import { signedStudioMediaUrl, useStudioMediaUrls } from '@/lib/studio-media'
import { useAuthStore } from '@/stores/auth-store'

import { VideoComposer } from './components/video-composer'
import { VideoGenerationDetails } from './components/video-generation-details'
import { VideoGenerationPlayer } from './components/video-generation-player'
import { VideoGenerationStatus } from './components/video-generation-status'
import { VideoSampleGallery } from './components/video-sample-gallery'
import { VideoStudioEntryGate } from './components/video-studio-entry-gate'
import { VideoStudioNav } from './components/video-studio-nav'
import { VideoSubmissionNotice } from './components/video-submission-notice'
import { useVideoGenerationTarget } from './hooks/use-video-generation-target'
import { useVideoTokenGate } from './hooks/use-video-token-gate'
import { useVideoGenerations, useVideoModels, useVideoSample } from './queries'
import type {
  VideoGeneration,
  VideoSample,
  VideoStudioApiError,
  VideoSubmissionReceipt,
} from './types'
import { getVideoTokenErrorKind } from './video-token-access'

type VideoCreatePageProps = {
  initialSampleId?: number
  targetTaskId?: string
  targetGenerationId?: number
  onTargetChange?: (target: { task?: string; generation?: number }) => void
}

export function VideoCreatePage(props: VideoCreatePageProps) {
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const sessionId = useAuthStore((state) => state.auth.session?.sid ?? '')
  return <VideoCreateWorkspace key={`${userId}:${sessionId}`} {...props} />
}

function VideoCreateWorkspace(props: VideoCreatePageProps) {
  const { t, i18n } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const copy = useCopyToClipboard()
  const videoTokenGate = useVideoTokenGate()
  const modelsQuery = useVideoModels(videoTokenGate.tokenId)
  const initialSampleQuery = useVideoSample(
    props.initialSampleId,
    videoTokenGate.tokenId
  )
  const [selectedSampleOverride, setSelectedSample] = useState<VideoSample>()
  const [sampleSelectionId, setSampleSelectionId] = useState(0)
  const selectedSample = selectedSampleOverride ?? initialSampleQuery.data
  const [localTarget, setLocalTarget] = useState<{
    task?: string
    generation?: number
  }>({})
  const targetTaskId = props.onTargetChange
    ? props.targetTaskId
    : (localTarget.task ?? props.targetTaskId)
  const targetGenerationId = props.onTargetChange
    ? props.targetGenerationId
    : (localTarget.generation ?? props.targetGenerationId)
  const generationsQuery = useVideoGenerations(
    {},
    true,
    targetTaskId,
    targetGenerationId
  )
  const target = useVideoGenerationTarget({
    generationsQuery,
    targetTaskId,
    targetGenerationId,
  })
  const generation = target.generation
  const [activeMedia, setActiveMedia] = useState<
    'generation' | 'sample' | 'details' | null
  >(null)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const [inspirationOpen, setInspirationOpen] = useState(true)
  const inspirationChosen = useRef(false)
  const activeScope = useRef(true)
  useEffect(() => {
    activeScope.current = true
    return () => {
      activeScope.current = false
    }
  }, [])
  const resultRef = useRef<HTMLElement>(null)
  const editorRef = useRef<HTMLElement>(null)
  const blockAndRecheckVideoToken = videoTokenGate.blockAndRecheck
  const markVideoTokenHealthy = videoTokenGate.markTokenHealthy
  const recent = generationsQuery.items.slice(0, 3)
  const paths = [
    generation?.video_url,
    generation?.poster_url,
    generation?.download_url,
    ...recent.map((item) => item.poster_url),
  ]
  const mediaQuery = useStudioMediaUrls(paths)
  const signedGeneration = generation
    ? {
        ...generation,
        video_url: signedStudioMediaUrl(
          generation.video_url,
          mediaQuery.data?.urls,
          mediaQuery.data?.deniedPaths,
          mediaQuery.identityDenied
        ),
        poster_url: signedStudioMediaUrl(
          generation.poster_url,
          mediaQuery.data?.urls,
          mediaQuery.data?.deniedPaths,
          mediaQuery.identityDenied
        ),
        download_url: signedStudioMediaUrl(
          generation.download_url,
          mediaQuery.data?.urls,
          mediaQuery.data?.deniedPaths,
          mediaQuery.identityDenied
        ),
      }
    : null
  const denied =
    mediaQuery.identityDenied ||
    Boolean(
      generation?.video_url &&
      mediaQuery.data?.deniedPaths?.[generation.video_url]
    )
  const posterDenied =
    mediaQuery.identityDenied ||
    Boolean(
      generation?.poster_url &&
      mediaQuery.data?.deniedPaths?.[generation.poster_url]
    )

  useEffect(() => {
    if (!modelsQuery.isError) return
    const responseError =
      modelsQuery.error instanceof AxiosError
        ? (modelsQuery.error.response?.data as VideoStudioApiError | undefined)
        : undefined
    blockAndRecheckVideoToken(getVideoTokenErrorKind(responseError?.code))
  }, [blockAndRecheckVideoToken, modelsQuery.error, modelsQuery.isError])

  useEffect(() => {
    if (!modelsQuery.isSuccess || !videoTokenGate.tokenId) return
    markVideoTokenHealthy(videoTokenGate.tokenId)
  }, [markVideoTokenHealthy, modelsQuery.isSuccess, videoTokenGate.tokenId])

  useEffect(() => {
    if (!initialSampleQuery.isError) return
    const responseError =
      initialSampleQuery.error instanceof AxiosError
        ? (initialSampleQuery.error.response?.data as
            | VideoStudioApiError
            | undefined)
        : undefined
    blockAndRecheckVideoToken(getVideoTokenErrorKind(responseError?.code))
  }, [
    blockAndRecheckVideoToken,
    initialSampleQuery.error,
    initialSampleQuery.isError,
  ])

  const changeTarget = (next: { task?: string; generation?: number }) => {
    setActiveMedia(null)
    setDetailsOpen(false)
    setLocalTarget(next)
    props.onTargetChange?.(next)
  }
  const scrollTo = (element: HTMLElement | null) => {
    element?.scrollIntoView({
      behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches
        ? 'auto'
        : 'smooth',
      block: 'start',
    })
  }
  const handleSubmitted = (receipt: VideoSubmissionReceipt) => {
    changeTarget({ task: receipt.task_id || receipt.id })
    if (!inspirationChosen.current) setInspirationOpen(false)
    scrollTo(resultRef.current)
  }
  const inspectTask = (task: string) => {
    changeTarget({ task })
    scrollTo(resultRef.current)
  }
  const trySample = (sample: VideoSample) => {
    setSelectedSample(sample)
    setSampleSelectionId((value) => value + 1)
    scrollTo(editorRef.current)
  }
  const refreshMedia = async () => {
    const result = await mediaQuery.refetch()
    if (
      result.error ||
      !generation?.video_url ||
      result.data?.deniedPaths?.[generation.video_url]
    ) {
      return undefined
    }
    return signedStudioMediaUrl(generation.video_url, result.data?.urls)
  }
  const download = async () => {
    if (!generation || generation.status !== 'ready') return
    const result = await mediaQuery.refetch()
    if (
      !activeScope.current ||
      useAuthStore.getState().auth.user?.id !== userId
    ) {
      return
    }
    const url =
      result.error || result.data?.deniedPaths?.[generation.download_url ?? '']
        ? undefined
        : signedStudioMediaUrl(generation.download_url, result.data?.urls)
    if (!url) {
      toast.error(t('Could not download this video. Please try again.'))
      return
    }
    const link = document.createElement('a')
    link.href = url
    link.download = generation.task_id
    link.rel = 'noopener noreferrer'
    link.click()
  }
  const models = modelsQuery.data ?? []
  let entryState: ReactNode = null
  if (!videoTokenGate.tokenId) {
    entryState = <VideoStudioEntryGate gate={videoTokenGate} />
  } else if (modelsQuery.isError && !modelsQuery.data) {
    entryState = (
      <Empty className='min-h-80 rounded-none' role='alert'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <CircleAlert className='text-destructive' aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>{t('videoStudio.workspace.prepareFailed')}</EmptyTitle>
          <EmptyDescription>
            {t('videoStudio.workspace.prepareFailedDescription')}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button
            type='button'
            size='sm'
            disabled={modelsQuery.isFetching}
            onClick={() => void modelsQuery.refetch()}
          >
            <RotateCw data-icon='inline-start' aria-hidden='true' />
            {t('videoStudio.retry')}
          </Button>
        </EmptyContent>
      </Empty>
    )
  } else if (
    !modelsQuery.data ||
    (models.length === 0 && modelsQuery.isFetching)
  ) {
    entryState = (
      <Empty className='min-h-80 rounded-none' role='status'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <LoaderCircle
              className='animate-spin motion-reduce:animate-none'
              aria-hidden='true'
            />
          </EmptyMedia>
          <EmptyTitle>{t('videoStudio.workspace.preparing')}</EmptyTitle>
          <EmptyDescription>
            {t('videoStudio.workspace.preparingDescription')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else if (models.length === 0) {
    entryState = (
      <Empty className='min-h-80 rounded-none' role='alert'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <CircleAlert className='text-destructive' aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>{t('videoStudio.noModels')}</EmptyTitle>
        </EmptyHeader>
        <EmptyContent>
          <Button
            type='button'
            size='sm'
            disabled={modelsQuery.isFetching}
            onClick={() => void modelsQuery.refetch()}
          >
            <RotateCw data-icon='inline-start' aria-hidden='true' />
            {t('videoStudio.retry')}
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  let resultContent: ReactNode
  if (signedGeneration && target.status === 'ready') {
    resultContent = (
      <>
        <VideoGenerationPlayer
          generation={signedGeneration}
          active={activeMedia === 'generation'}
          mediaIdentity={`${userId}:${generation?.id}:${generation?.output_asset_id ?? ''}`}
          loading={Boolean(generation?.video_url) && mediaQuery.isPending}
          denied={denied}
          posterDenied={posterDenied}
          onPlay={() => setActiveMedia('generation')}
          onRefresh={refreshMedia}
        />
        <div className='video-studio-result-meta'>
          <div>
            <h3>{generation?.model}</h3>
            <p className='video-studio-meta'>
              {Object.values(generation?.parameters ?? {})
                .filter((value) => typeof value !== 'boolean')
                .join(' · ')}
              {generation && (
                <time>
                  {new Date(generation.created_at * 1000).toLocaleString(
                    toIntlLocale(i18n.language),
                    {
                      month: 'short',
                      day: 'numeric',
                      hour: '2-digit',
                      minute: '2-digit',
                    }
                  )}
                </time>
              )}
            </p>
          </div>
          <div className='video-studio-result-actions'>
            <Button
              variant='ghost'
              size='icon'
              aria-label={t('Copy task ID')}
              onClick={() =>
                void copy.copyToClipboard(signedGeneration.task_id)
              }
            >
              <Copy aria-hidden='true' />
            </Button>
            <Button
              variant='outline'
              onClick={() => {
                setActiveMedia('details')
                setDetailsOpen(true)
              }}
            >
              {t('Details')}
            </Button>
            {generation?.status === 'ready' && (
              <Button
                variant='outline'
                disabled={denied}
                onClick={() => void download()}
              >
                <Download aria-hidden='true' />
                {t('Download')}
              </Button>
            )}
          </div>
        </div>
      </>
    )
  } else if (target.status === 'loading' || target.status === 'locating') {
    resultContent = (
      <div className='video-studio-target-state' role='status'>
        <Skeleton className='video-studio-result-skeleton' />
        <p>
          {t(
            target.status === 'locating'
              ? 'videoStudio.generationLocating'
              : 'Loading...'
          )}
        </p>
        {targetTaskId && <code>{targetTaskId}</code>}
      </div>
    )
  } else if (['missing', 'error', 'unavailable'].includes(target.status)) {
    let message = t('Could not load your videos.')
    if (target.status === 'missing') {
      message = t('videoStudio.generationNotVisible')
    }
    if (target.status === 'unavailable') {
      message = t('This result is unavailable or has been removed.')
    }
    resultContent = (
      <div className='video-studio-target-state' role='alert'>
        <CircleAlert aria-hidden='true' />
        <h3>{message}</h3>
        {targetTaskId && <code>{targetTaskId}</code>}
        <div className='video-studio-result-actions'>
          <Button
            variant='outline'
            onClick={() => void target.retryTargetDiscovery()}
          >
            <RotateCw aria-hidden='true' />
            {t('Retry')}
          </Button>
          <Button
            variant='ghost'
            nativeButton={false}
            render={<Link to='/video-studio/tasks' />}
          >
            {t('videoStudio.tasks')}
          </Button>
        </div>
      </div>
    )
  } else {
    resultContent = (
      <div className='video-studio-first-state'>
        <img src={cinemaArt} alt='' width={240} height={240} />
        <div>
          <h3>{t('Your next scene starts here')}</h3>
          <p>
            {t(
              'Describe a scene, choose your settings, and turn your idea into a video.'
            )}
          </p>
          <p className='video-studio-first-steps'>
            {t('Describe · Create · Preview')}
          </p>
        </div>
      </div>
    )
  }

  return (
    <main id='content' className='video-studio video-studio-page'>
      <div className='video-studio-container'>
        <VideoStudioNav />
        <VideoSubmissionNotice
          generations={generationsQuery.items}
          onInspectTask={inspectTask}
        />
        <div className='video-studio-mobile-jump'>
          <Button variant='ghost' onClick={() => scrollTo(resultRef.current)}>
            <ArrowDown aria-hidden='true' />
            {t('View result')}
          </Button>
          <Button variant='ghost' onClick={() => scrollTo(editorRef.current)}>
            <ArrowUp aria-hidden='true' />
            {t('Back to editor')}
          </Button>
        </div>
        <div className='video-studio-workspace'>
          <aside
            ref={editorRef}
            className='video-studio-editor video-studio-panel'
            aria-label={t('videoStudio.composer')}
          >
            <div className='video-studio-panel-heading'>
              <h2>
                <Sparkles aria-hidden='true' />
                {t('videoStudio.composer')}
              </h2>
              {!entryState && <span>{t('Local draft')}</span>}
            </div>
            {entryState || (
              <VideoComposer
                models={models}
                sample={selectedSample}
                sampleSelectionId={sampleSelectionId}
                videoTokenGate={videoTokenGate}
                onSubmitted={handleSubmitted}
                onSubmissionUnknown={inspectTask}
              />
            )}
          </aside>
          <div className='video-studio-results-column'>
            <section
              ref={resultRef}
              className='video-studio-result video-studio-panel'
              aria-label={t('Current result')}
            >
              <div className='video-studio-panel-heading'>
                <h2>{t('Current result')}</h2>
                {generation && (
                  <VideoGenerationStatus generation={generation} compact />
                )}
              </div>
              {target.isStale && (
                <p className='video-studio-notice' role='status'>
                  {t(
                    'Status could not be updated. Showing the last available result.'
                  )}
                </p>
              )}
              {resultContent}
            </section>
            {recent.length > 0 && (
              <section aria-label={t('Recent creations')}>
                <div className='video-studio-section-heading'>
                  <h2>{t('Recent creations')}</h2>
                  <Link to='/video-studio/library'>
                    {t('View all')}
                    <ArrowLeft className='rotate-180' aria-hidden='true' />
                  </Link>
                </div>
                <div className='video-studio-recent-grid'>
                  {recent.map((item) => (
                    <button
                      key={item.id}
                      type='button'
                      className='video-studio-recent-card'
                      aria-pressed={generation?.id === item.id}
                      onClick={() => {
                        target.selectGeneration(item.id)
                        changeTarget({
                          task: item.task_id,
                          generation: item.id,
                        })
                      }}
                    >
                      <div className='video-studio-recent-image'>
                        {signedStudioMediaUrl(
                          item.poster_url,
                          mediaQuery.data?.urls,
                          mediaQuery.data?.deniedPaths,
                          mediaQuery.identityDenied
                        ) ? (
                          <img
                            src={signedStudioMediaUrl(
                              item.poster_url,
                              mediaQuery.data?.urls,
                              mediaQuery.data?.deniedPaths,
                              mediaQuery.identityDenied
                            )}
                            alt=''
                            loading='lazy'
                            decoding='async'
                          />
                        ) : (
                          <Film aria-hidden='true' />
                        )}
                        <VideoGenerationStatus generation={item} compact />
                      </div>
                      <div className='video-studio-recent-info'>
                        <strong>{item.prompt || item.model}</strong>
                        <span>{item.model}</span>
                      </div>
                    </button>
                  ))}
                </div>
              </section>
            )}
            {!entryState && (
              <section className='video-studio-inspiration'>
                <button
                  type='button'
                  className='video-studio-inspiration-toggle'
                  aria-expanded={inspirationOpen}
                  aria-controls='video-studio-inspiration-content'
                  onClick={() => {
                    inspirationChosen.current = true
                    setInspirationOpen(!inspirationOpen)
                    setActiveMedia(null)
                  }}
                >
                  <img src={cinemaArt} alt='' width={72} height={56} />
                  <span>
                    <strong>
                      <Sparkles aria-hidden='true' />
                      {t('videoStudio.samples')}
                    </strong>
                    <small>
                      {t('Find a starting point for your next video.')}
                    </small>
                  </span>
                  <ChevronDown aria-hidden='true' />
                </button>
                <div
                  id='video-studio-inspiration-content'
                  hidden={!inspirationOpen}
                  className='video-studio-inspiration-content'
                >
                  <VideoSampleGallery
                    models={models}
                    tokenId={videoTokenGate.tokenId}
                    selectedSampleId={selectedSample?.id}
                    onTokenError={blockAndRecheckVideoToken}
                    onTrySample={trySample}
                    visible={inspirationOpen}
                    previewActive={inspirationOpen && activeMedia === 'sample'}
                    onPreviewStart={() => setActiveMedia('sample')}
                  />
                </div>
              </section>
            )}
          </div>
        </div>
      </div>
      <VideoGenerationDetails
        generation={signedGeneration}
        open={detailsOpen && Boolean(generation)}
        onOpenChange={(open) => {
          setDetailsOpen(open)
          setActiveMedia(open ? 'details' : null)
        }}
        active={activeMedia === 'details'}
        mediaIdentity={`${userId}:${generation?.id}:${generation?.output_asset_id ?? ''}`}
        loading={Boolean(generation?.video_url) && mediaQuery.isPending}
        denied={denied}
        posterDenied={posterDenied}
        onPlay={() => setActiveMedia('details')}
        onRefresh={refreshMedia}
        onCopyTask={(item: VideoGeneration) =>
          void copy.copyToClipboard(item.task_id)
        }
        onDownload={() => void download()}
      />
    </main>
  )
}

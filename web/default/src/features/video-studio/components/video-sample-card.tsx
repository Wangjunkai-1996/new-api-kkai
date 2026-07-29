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
import { Film, LoaderCircle, VideoOff, WandSparkles } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { useCallback, useEffect, useReducer, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { MOTION_TRANSITION } from '@/lib/motion'
import { cn } from '@/lib/utils'

import type { VideoSample } from '../types'
import { VIDEO_MODE_LABEL_KEYS } from '../video-domain'

type VideoSampleCardProps = {
  sample: VideoSample
  active: boolean
  warmed: boolean
  selected: boolean
  onPreviewWarm: (id: number) => void
  onPreviewStart: (id: number) => void
  onPreviewEnd: (id: number) => void
  onTry: (sample: VideoSample) => void
}

type VideoSamplePlaybackState = {
  hasPlayingFrame: boolean
  loading: boolean
  error: boolean
}

type VideoSamplePlaybackAction =
  | { type: 'activate' }
  | { type: 'playing' }
  | { type: 'waiting' }
  | { type: 'error' }
  | { type: 'deactivate' }

const PREVIEW_HOVER_INTENT_MS = 80
const PREVIEW_LOADING_INDICATOR_DELAY_MS = 180

const VIDEO_SAMPLE_PLAYBACK_INITIAL_STATE: VideoSamplePlaybackState = {
  hasPlayingFrame: false,
  loading: false,
  error: false,
}

type ReleasableVideoElement = Pick<
  HTMLVideoElement,
  'load' | 'pause' | 'removeAttribute'
>

// oxlint-disable-next-line react/only-export-components -- Exported for media resource lifecycle regression tests.
export const releaseVideoSamplePreview = (
  video: ReleasableVideoElement | null
) => {
  if (!video) return
  video.pause()
  video.removeAttribute('src')
  video.load()
}

// oxlint-disable-next-line react/only-export-components -- Exported for deterministic media lifecycle regression tests.
export const reduceVideoSamplePlayback = (
  state: VideoSamplePlaybackState,
  action: VideoSamplePlaybackAction
): VideoSamplePlaybackState => {
  switch (action.type) {
    case 'activate':
      return { hasPlayingFrame: false, loading: true, error: false }
    case 'playing':
      return { hasPlayingFrame: true, loading: false, error: false }
    case 'waiting':
      return { ...state, loading: true, error: false }
    case 'error':
      return { hasPlayingFrame: false, loading: false, error: true }
    case 'deactivate':
      return VIDEO_SAMPLE_PLAYBACK_INITIAL_STATE
  }
}

export function VideoSampleCard(props: VideoSampleCardProps) {
  const { t } = useTranslation()
  const reducedMotion = useReducedMotion()
  const videoRef = useRef<HTMLVideoElement>(null)
  const hoverTimerRef = useRef<number | null>(null)
  const loadingTimerRef = useRef<number | null>(null)
  const playbackAttemptRef = useRef(0)
  const pointerInsideRef = useRef(false)
  const keyboardFocusInsideRef = useRef(false)
  const activeRef = useRef(props.active)
  activeRef.current = props.active
  const [playback, dispatchPlayback] = useReducer(
    reduceVideoSamplePlayback,
    VIDEO_SAMPLE_PLAYBACK_INITIAL_STATE
  )
  const [loadingIndicatorVisible, setLoadingIndicatorVisible] = useState(false)
  const onPreviewEnd = props.onPreviewEnd
  const sampleId = props.sample.id
  const descriptionId = `video-sample-${props.sample.id}-prompt`

  const posterUrl = props.sample.poster_url || props.sample.video_url
  const previewUrl = props.sample.preview_url
  const previewVisible =
    props.active && playback.hasPlayingFrame && !playback.error

  const clearLoadingIndicator = useCallback(() => {
    if (loadingTimerRef.current !== null) {
      window.clearTimeout(loadingTimerRef.current)
      loadingTimerRef.current = null
    }
    setLoadingIndicatorVisible(false)
  }, [])

  const queueLoadingIndicator = useCallback(() => {
    clearLoadingIndicator()
    loadingTimerRef.current = window.setTimeout(() => {
      if (activeRef.current) setLoadingIndicatorVisible(true)
      loadingTimerRef.current = null
    }, PREVIEW_LOADING_INDICATOR_DELAY_MS)
  }, [clearLoadingIndicator])

  const beginPreview = () => {
    if (!previewUrl || reducedMotion) return
    props.onPreviewWarm(props.sample.id)
    if (props.active || hoverTimerRef.current !== null) return
    hoverTimerRef.current = window.setTimeout(() => {
      hoverTimerRef.current = null
      activeRef.current = true
      props.onPreviewStart(props.sample.id)
    }, PREVIEW_HOVER_INTENT_MS)
  }

  const endPreviewIfDisengaged = () => {
    if (pointerInsideRef.current || keyboardFocusInsideRef.current) return
    if (hoverTimerRef.current !== null) {
      window.clearTimeout(hoverTimerRef.current)
      hoverTimerRef.current = null
    }
    activeRef.current = false
    videoRef.current?.pause()
    clearLoadingIndicator()
    dispatchPlayback({ type: 'deactivate' })
    props.onPreviewEnd(props.sample.id)
  }

  useEffect(() => {
    const video = videoRef.current
    if (
      reducedMotion ||
      !props.active ||
      !props.warmed ||
      !previewUrl ||
      !video
    ) {
      if (video && !props.active) {
        video.pause()
        if (video.readyState >= 1) video.currentTime = 0
      }
      clearLoadingIndicator()
      dispatchPlayback({ type: 'deactivate' })
      return
    }

    dispatchPlayback({ type: 'activate' })
    queueLoadingIndicator()
    const playbackAttempt = ++playbackAttemptRef.current
    const isCurrentPlayback = () =>
      activeRef.current && playbackAttemptRef.current === playbackAttempt
    const handlePlaying = () => {
      if (!isCurrentPlayback()) return
      clearLoadingIndicator()
      dispatchPlayback({ type: 'playing' })
    }
    const handleWaiting = () => {
      if (!isCurrentPlayback()) return
      dispatchPlayback({ type: 'waiting' })
      queueLoadingIndicator()
    }
    const handleError = () => {
      if (!isCurrentPlayback()) return
      clearLoadingIndicator()
      dispatchPlayback({ type: 'error' })
    }
    video.addEventListener('playing', handlePlaying)
    video.addEventListener('waiting', handleWaiting)
    video.addEventListener('stalled', handleWaiting)
    video.addEventListener('error', handleError)
    if (video.error) video.load()
    const playResult = video.play()
    if (playResult) {
      void playResult.catch((error: unknown) => {
        const interrupted =
          error instanceof DOMException && error.name === 'AbortError'
        if (!isCurrentPlayback() || interrupted) return
        clearLoadingIndicator()
        dispatchPlayback({ type: 'error' })
      })
    }
    return () => {
      if (playbackAttemptRef.current === playbackAttempt) {
        playbackAttemptRef.current += 1
      }
      video.removeEventListener('playing', handlePlaying)
      video.removeEventListener('waiting', handleWaiting)
      video.removeEventListener('stalled', handleWaiting)
      video.removeEventListener('error', handleError)
      video.pause()
    }
  }, [
    clearLoadingIndicator,
    previewUrl,
    props.active,
    props.warmed,
    queueLoadingIndicator,
    reducedMotion,
  ])

  useEffect(() => {
    if (reducedMotion || !props.warmed || !previewUrl) return
    const video = videoRef.current
    if (!video) return
    return () => releaseVideoSamplePreview(video)
  }, [previewUrl, props.warmed, reducedMotion])

  useEffect(
    () => () => {
      if (hoverTimerRef.current !== null) {
        window.clearTimeout(hoverTimerRef.current)
      }
      if (loadingTimerRef.current !== null) {
        window.clearTimeout(loadingTimerRef.current)
      }
      if (activeRef.current) onPreviewEnd(sampleId)
    },
    [onPreviewEnd, sampleId]
  )

  return (
    <motion.article
      className={cn(
        'bg-card ring-border/70 relative z-0 overflow-hidden rounded-lg ring-1 transition-[box-shadow,ring-color] hover:z-10 hover:shadow-xl hover:shadow-black/10',
        props.selected && 'ring-primary ring-2'
      )}
      animate={{ scale: props.active && !reducedMotion ? 1.025 : 1 }}
      transition={MOTION_TRANSITION.spring}
      onPointerEnter={(event) => {
        if (event.pointerType === 'touch') return
        pointerInsideRef.current = true
        beginPreview()
      }}
      onPointerLeave={() => {
        pointerInsideRef.current = false
        endPreviewIfDisengaged()
      }}
      onPointerCancel={() => {
        pointerInsideRef.current = false
        endPreviewIfDisengaged()
      }}
      onFocusCapture={(event) => {
        if (
          !(event.target instanceof HTMLElement) ||
          !event.target.matches(':focus-visible')
        ) {
          return
        }
        keyboardFocusInsideRef.current = true
        beginPreview()
      }}
      onBlurCapture={(event) => {
        if (event.currentTarget.contains(event.relatedTarget)) return
        keyboardFocusInsideRef.current = false
        endPreviewIfDisengaged()
      }}
    >
      <button
        type='button'
        className='focus-visible:ring-ring/60 block w-full text-left outline-none focus-visible:ring-3 focus-visible:ring-inset'
        onClick={() => props.onTry(props.sample)}
        aria-describedby={descriptionId}
        aria-pressed={props.selected}
      >
        <div
          className='bg-muted relative overflow-hidden'
          style={{ aspectRatio: props.sample.aspect_ratio || 9 / 16 }}
        >
          {posterUrl && (
            <img
              src={posterUrl}
              alt={props.sample.title || props.sample.prompt}
              loading='lazy'
              className={cn(
                'relative z-10 size-full object-cover transition-opacity duration-150 motion-reduce:transition-none',
                previewVisible && 'opacity-0'
              )}
            />
          )}
          {props.warmed && previewUrl && !reducedMotion && (
            <video
              ref={videoRef}
              className={cn(
                'pointer-events-none absolute inset-0 z-0 size-full object-cover opacity-0 transition-opacity duration-150 motion-reduce:transition-none',
                previewVisible && 'opacity-100'
              )}
              src={previewUrl}
              muted
              loop={!reducedMotion}
              playsInline
              preload='auto'
              aria-hidden='true'
            />
          )}
          {!posterUrl && (
            <Film
              className={cn(
                'text-muted-foreground absolute top-1/2 left-1/2 z-10 size-8 -translate-x-1/2 -translate-y-1/2 transition-opacity duration-150 motion-reduce:transition-none',
                previewVisible && 'opacity-0'
              )}
              aria-hidden='true'
            />
          )}
          {props.active && playback.loading && loadingIndicatorVisible && (
            <span
              className='absolute top-1/2 left-1/2 z-30 grid size-8 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full bg-black/55 text-white shadow-sm'
              aria-hidden='true'
            >
              <LoaderCircle className='size-4 animate-spin motion-reduce:animate-none' />
            </span>
          )}
          {props.active && playback.error && (
            <span
              className='absolute top-2 right-2 z-30 grid size-7 place-items-center rounded-full bg-black/55 text-white shadow-sm'
              aria-hidden='true'
            >
              <VideoOff className='size-3.5' />
            </span>
          )}
          <div className='absolute inset-x-0 bottom-0 z-20 flex items-end justify-between gap-2 bg-linear-to-t from-black/70 to-transparent p-2 pt-8 text-white'>
            <span className='truncate text-xs font-semibold'>
              {props.sample.model_display_name || t('videoStudio.sample')}
            </span>
          </div>
        </div>

        <div className='space-y-2 p-3'>
          <div className='flex items-start justify-between gap-2'>
            <p
              id={descriptionId}
              className='text-foreground line-clamp-3 min-w-0 text-sm leading-5'
            >
              {props.sample.prompt}
            </p>
            <WandSparkles
              className='text-primary mt-0.5 size-4 shrink-0'
              aria-hidden='true'
            />
          </div>
          <div className='flex flex-wrap items-center gap-1.5'>
            <StatusBadge
              label={t(VIDEO_MODE_LABEL_KEYS[props.sample.mode])}
              variant='neutral'
              copyable={false}
              className='text-[11px]'
            />
            <span className='text-muted-foreground ms-auto text-xs font-medium'>
              {t('videoStudio.try')}
            </span>
          </div>
        </div>
      </button>
    </motion.article>
  )
}

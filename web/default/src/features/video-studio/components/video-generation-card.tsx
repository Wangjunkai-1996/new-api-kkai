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
  Download,
  Film,
  LoaderCircle,
  Play,
  RotateCw,
  Trash2,
  X,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { toIntlLocale } from '@/i18n/languages'
import { formatTimestampRelative } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { VideoGeneration, VideoGenerationStatus } from '../types'
import {
  getVideoGenerationFailureMessageKey,
  getVideoProgress,
} from '../video-domain'

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

type VideoGenerationCardProps = {
  generation: VideoGeneration
  highlighted?: boolean
  playing: boolean
  onPlay: (generation: VideoGeneration) => void
  onClose: () => void
  onDelete: (generation: VideoGeneration) => void
}

type PlayerState = 'idle' | 'loading' | 'ready' | 'error'

const releaseVideoElement = (video: HTMLVideoElement | null) => {
  if (!video) return
  video.pause()
  video.removeAttribute('src')
  video.load()
}

export function VideoGenerationCard(props: VideoGenerationCardProps) {
  const { t, i18n } = useTranslation()
  const playerRef = useRef<HTMLVideoElement>(null)
  const [playerState, setPlayerState] = useState<PlayerState>('idle')
  const [playerAttempt, setPlayerAttempt] = useState(0)
  const [posterFailed, setPosterFailed] = useState(false)
  const status = props.generation.status
  const progress = getVideoProgress(props.generation)
  const ready = status === 'ready' && props.generation.video_url
  const deletable = status === 'ready' || status === 'failed'
  const showPoster = Boolean(props.generation.poster_url) && !posterFailed
  const failureMessageKey = getVideoGenerationFailureMessageKey(
    props.generation
  )

  useEffect(() => {
    setPosterFailed(false)
  }, [props.generation.poster_url])

  useEffect(() => {
    if (!props.playing) {
      setPlayerState('idle')
      setPlayerAttempt(0)
      return
    }

    setPlayerState('loading')
    const video = playerRef.current
    return () => releaseVideoElement(video)
  }, [playerAttempt, props.generation.video_url, props.playing])

  const closePlayer = () => {
    releaseVideoElement(playerRef.current)
    setPlayerState('idle')
    props.onClose()
  }

  const retryPlayer = () => {
    setPlayerState('loading')
    setPlayerAttempt((attempt) => attempt + 1)
  }

  return (
    <article
      className={cn(
        'bg-card ring-border/70 overflow-hidden rounded-lg ring-1 transition-shadow',
        props.highlighted && 'ring-primary ring-2 shadow-sm'
      )}
      aria-current={props.highlighted ? 'true' : undefined}
    >
      <div className='bg-muted relative aspect-video overflow-hidden'>
        {ready && showPoster && (
          <img
            src={props.generation.poster_url}
            alt=''
            loading='lazy'
            decoding='async'
            className={cn(
              'absolute inset-0 size-full object-cover transition-opacity',
              props.playing && playerState === 'ready' && 'opacity-0'
            )}
            onError={() => setPosterFailed(true)}
          />
        )}

        {ready && !showPoster && (
          <div className='absolute inset-0 flex items-center justify-center'>
            <Film className='text-muted-foreground size-7' aria-hidden='true' />
          </div>
        )}

        {ready && !props.playing && (
          <Button
            type='button'
            size='icon-lg'
            className='absolute top-1/2 left-1/2 size-12 -translate-x-1/2 -translate-y-1/2 rounded-full shadow-lg'
            onClick={() => props.onPlay(props.generation)}
            aria-label={t('videoStudio.play')}
          >
            <Play className='ml-0.5 size-5 fill-current' aria-hidden='true' />
          </Button>
        )}

        {ready && props.playing && playerState !== 'error' && (
          <video
            key={playerAttempt}
            ref={playerRef}
            className={cn(
              'absolute inset-0 size-full object-cover transition-opacity',
              playerState === 'ready' ? 'opacity-100' : 'opacity-0'
            )}
            src={props.generation.video_url}
            poster={props.generation.poster_url}
            controls
            autoPlay
            playsInline
            preload='metadata'
            aria-label={props.generation.prompt}
            onCanPlay={() => setPlayerState('ready')}
            onError={(event) => {
              releaseVideoElement(event.currentTarget)
              setPlayerState('error')
            }}
          />
        )}

        {ready && props.playing && playerState === 'loading' && (
          <div
            className='absolute inset-0 flex items-center justify-center bg-black/20'
            role='status'
          >
            <LoaderCircle
              className='size-7 animate-spin text-white motion-reduce:animate-none'
              aria-hidden='true'
            />
            <span className='sr-only'>{t('Loading...')}</span>
          </div>
        )}

        {ready && props.playing && playerState === 'error' && (
          <div
            className='absolute inset-0 flex flex-col items-center justify-center gap-2 bg-black/55 p-4 text-white'
            role='alert'
          >
            <div className='flex items-center gap-2'>
              <Button type='button' size='sm' onClick={retryPlayer}>
                <RotateCw aria-hidden='true' />
                {t('Retry')}
              </Button>
              <Button
                type='button'
                size='sm'
                variant='secondary'
                onClick={closePlayer}
              >
                <X aria-hidden='true' />
                {t('Close')}
              </Button>
            </div>
          </div>
        )}

        {ready && props.playing && playerState !== 'error' && (
          <Button
            type='button'
            size='icon-sm'
            variant='secondary'
            className='absolute top-2 right-2 z-10 rounded-full shadow'
            onClick={closePlayer}
            aria-label={t('Close')}
          >
            <X aria-hidden='true' />
          </Button>
        )}

        {!ready && (
          <div className='flex size-full min-h-40 flex-col items-center justify-center gap-3 p-5'>
            {status === 'processing' || status === 'archiving' ? (
              <LoaderCircle
                className='text-muted-foreground size-7 animate-spin motion-reduce:animate-none'
                aria-hidden='true'
              />
            ) : (
              <Film
                className='text-muted-foreground size-7'
                aria-hidden='true'
              />
            )}
            <StatusBadge
              label={t(STATUS_LABELS[status])}
              variant={STATUS_VARIANTS[status]}
              copyable={false}
              pulse={status === 'processing' || status === 'archiving'}
              className='motion-reduce:animate-none'
            />
            {(status === 'processing' || status === 'queued') && (
              <Progress value={progress} className='max-w-40' />
            )}
          </div>
        )}
      </div>

      <div className='space-y-3 p-3'>
        <div className='flex items-start gap-2'>
          <div className='min-w-0 flex-1'>
            <p className='line-clamp-2 text-sm leading-5'>
              {props.generation.prompt}
            </p>
            <p className='text-muted-foreground mt-1 truncate text-xs'>
              {props.generation.model} ·{' '}
              {formatTimestampRelative(
                props.generation.created_at,
                'seconds',
                toIntlLocale(i18n.resolvedLanguage || i18n.language)
              )}
            </p>
          </div>
          <StatusBadge
            label={t(STATUS_LABELS[status])}
            variant={STATUS_VARIANTS[status]}
            copyable={false}
            className='shrink-0 text-xs'
          />
        </div>

        {failureMessageKey && (
          <p className='text-destructive line-clamp-2 text-xs' role='alert'>
            {t(failureMessageKey)}
          </p>
        )}

        <div className='flex items-center justify-between gap-2 border-t pt-2'>
          <code className='text-muted-foreground min-w-0 truncate text-[11px]'>
            {props.generation.task_id}
          </code>
          <div className='flex shrink-0 items-center gap-1'>
            {ready && props.generation.download_url && (
              <Button
                size='icon-sm'
                variant='ghost'
                render={
                  <a
                    href={props.generation.download_url}
                    aria-label={t('videoStudio.download')}
                  />
                }
              >
                <Download aria-hidden='true' />
              </Button>
            )}
            {deletable && (
              <Button
                size='icon-sm'
                variant='ghost'
                onClick={() => props.onDelete(props.generation)}
                aria-label={t('videoStudio.delete')}
              >
                <Trash2 aria-hidden='true' />
              </Button>
            )}
          </div>
        </div>
      </div>
    </article>
  )
}

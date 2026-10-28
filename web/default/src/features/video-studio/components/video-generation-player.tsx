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
import { Play, RotateCw, Video } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import type { VideoGeneration } from '../types'
import { VideoGenerationStatus } from './video-generation-status'

export type VideoGenerationPlayerProps = {
  generation: VideoGeneration
  active: boolean
  mediaIdentity?: string
  denied?: boolean
  posterDenied?: boolean
  onPlay?: () => void
  onRefresh?: () => Promise<string | undefined>
  className?: string
}

export function VideoGenerationPlayer(props: VideoGenerationPlayerProps) {
  return (
    <VideoGenerationPlayerSession
      key={`${props.mediaIdentity ?? ''}:${props.generation.id}:${props.generation.output_asset_id ?? ''}`}
      {...props}
    />
  )
}

function VideoGenerationPlayerSession(props: VideoGenerationPlayerProps) {
  const { t } = useTranslation()
  const videoRef = useRef<HTMLVideoElement>(null)
  const mounted = useRef(true)
  const allowed = useRef(false)
  const automaticRenewalUsed = useRef(false)
  const renewalInFlight = useRef(false)
  const resume = useRef<
    | { time: number; muted: boolean; volume: number; playing: boolean }
    | undefined
  >(undefined)
  const [source, setSource] = useState(props.generation.video_url)
  const [revision, setRevision] = useState(0)
  const [failed, setFailed] = useState(false)
  const [renewing, setRenewing] = useState(false)
  const [failedPoster, setFailedPoster] = useState<string>()
  const [startRequested, setStartRequested] = useState(false)
  const ready = props.generation.status === 'ready'
  useEffect(() => {
    allowed.current = props.active && ready && !props.denied
    return () => {
      allowed.current = false
    }
  }, [props.active, ready, props.denied])
  if (source && (props.denied || !ready)) setSource(undefined)
  if (
    ready &&
    !source &&
    props.generation.video_url &&
    !failed &&
    !props.denied
  ) {
    setSource(props.generation.video_url)
  }

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  useEffect(() => {
    const video = videoRef.current
    return () => {
      if (!video) return
      video.pause()
      video.removeAttribute('src')
      video.load()
    }
  }, [props.active, props.denied, ready, revision, failed])

  const refresh = async () => {
    if (renewalInFlight.current || !allowed.current || !props.onRefresh) return
    renewalInFlight.current = true
    const video = videoRef.current
    if (video) {
      resume.current = {
        time: video.currentTime,
        muted: video.muted,
        volume: video.volume,
        playing: !video.paused,
      }
      video.pause()
    }
    setRenewing(true)
    try {
      const next = await props.onRefresh()
      if (!mounted.current || !allowed.current) return
      if (!next) {
        setFailed(true)
        return
      }
      setSource(next)
      setFailed(false)
      setRevision((value) => value + 1)
    } catch {
      if (mounted.current && allowed.current) setFailed(true)
    } finally {
      renewalInFlight.current = false
      if (mounted.current) setRenewing(false)
    }
  }

  const onError = () => {
    if (renewalInFlight.current) return
    if (!automaticRenewalUsed.current && props.onRefresh) {
      automaticRenewalUsed.current = true
      void refresh()
    } else {
      setFailed(true)
    }
  }

  const poster =
    props.posterDenied ||
    props.denied ||
    failedPoster === props.generation.poster_url
      ? undefined
      : props.generation.poster_url
  const unavailable = props.denied || failed || (ready && !source)

  return (
    <div
      className={cn('video-studio-player', props.className)}
      data-status={props.generation.status}
    >
      {!ready && <VideoGenerationStatus generation={props.generation} />}
      {ready && poster && !props.active && (
        <img
          className='video-studio-player-poster'
          src={poster}
          alt=''
          onError={() => setFailedPoster(props.generation.poster_url)}
        />
      )}
      {ready && !props.active && !props.denied && (
        <Button
          type='button'
          className='video-studio-player-play'
          size='icon-lg'
          onClick={() => {
            setStartRequested(true)
            props.onPlay?.()
          }}
          aria-label={t('Play video')}
        >
          <Play aria-hidden='true' />
        </Button>
      )}
      {ready && props.active && source && !props.denied && !failed && (
        <video
          key={revision}
          ref={videoRef}
          className='video-studio-player-video'
          src={source}
          poster={poster}
          controls
          autoPlay={startRequested}
          playsInline
          preload='metadata'
          aria-label={t('Generated video')}
          onPlay={props.onPlay}
          onError={onError}
          onLoadedMetadata={(event) => {
            const saved = resume.current
            if (!saved) return
            const video = event.currentTarget
            video.muted = saved.muted
            video.volume = saved.volume
            if (Number.isFinite(video.duration) && video.duration > 0) {
              video.currentTime = Math.min(saved.time, video.duration)
            }
            resume.current = undefined
            if (saved.playing) void video.play().catch(() => {})
          }}
        />
      )}
      {ready && (unavailable || renewing) && (
        <div
          className='video-studio-player-state'
          role={unavailable ? 'alert' : 'status'}
        >
          <Video aria-hidden='true' />
          <p>
            {renewing
              ? t('Refreshing video access')
              : t('Video preview is unavailable')}
          </p>
          <span>{t('Your task information is still available.')}</span>
          {!props.denied && props.onRefresh && props.active && (
            <Button
              type='button'
              variant='outline'
              disabled={renewing}
              onClick={() => void refresh()}
            >
              <RotateCw aria-hidden='true' />
              {t('Retry preview')}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}

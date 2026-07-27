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
import { Film, WandSparkles } from 'lucide-react'
import { motion } from 'motion/react'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { MOTION_TRANSITION } from '@/lib/motion'
import { cn } from '@/lib/utils'

import { usePreviewPolicy } from '../hooks/use-preview-policy'
import type { VideoSample } from '../types'
import { VIDEO_MODE_LABEL_KEYS } from '../video-domain'

type VideoSampleCardProps = {
  sample: VideoSample
  active: boolean
  selected: boolean
  onPreviewChange: (id: number | null) => void
  onTry: (sample: VideoSample) => void
}

export function VideoSampleCard(props: VideoSampleCardProps) {
  const { t } = useTranslation()
  const previewPolicy = usePreviewPolicy()
  const hoverTimerRef = useRef<number | null>(null)
  const activeRef = useRef(props.active)
  activeRef.current = props.active
  const onPreviewChange = props.onPreviewChange
  const descriptionId = `video-sample-${props.sample.id}-prompt`

  const posterUrl = props.sample.poster_url || props.sample.video_url
  const previewUrl = props.sample.preview_url

  const beginPreview = () => {
    if (!previewPolicy.autoplay || !previewUrl) return
    if (hoverTimerRef.current !== null) {
      window.clearTimeout(hoverTimerRef.current)
    }
    hoverTimerRef.current = window.setTimeout(() => {
      props.onPreviewChange(props.sample.id)
    }, 150)
  }

  const endPreview = () => {
    if (hoverTimerRef.current !== null) {
      window.clearTimeout(hoverTimerRef.current)
      hoverTimerRef.current = null
    }
    if (props.active) props.onPreviewChange(null)
  }

  useEffect(
    () => () => {
      if (hoverTimerRef.current !== null) {
        window.clearTimeout(hoverTimerRef.current)
      }
      if (activeRef.current) onPreviewChange(null)
    },
    [onPreviewChange]
  )

  return (
    <motion.article
      className={cn(
        'bg-card ring-border/70 relative z-0 overflow-hidden rounded-lg ring-1 transition-[box-shadow,ring-color] hover:z-10 hover:shadow-xl hover:shadow-black/10',
        props.selected && 'ring-primary ring-2'
      )}
      animate={{ scale: props.active && previewPolicy.motion ? 1.025 : 1 }}
      transition={MOTION_TRANSITION.spring}
      onPointerEnter={beginPreview}
      onPointerLeave={endPreview}
      onFocusCapture={beginPreview}
      onBlurCapture={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) endPreview()
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
                'size-full object-cover transition-opacity duration-150',
                props.active && previewUrl && 'opacity-0'
              )}
            />
          )}
          {props.active && previewUrl && (
            <video
              className='absolute inset-0 size-full object-cover'
              src={previewUrl}
              autoPlay
              muted
              loop
              playsInline
              preload='metadata'
              aria-label={props.sample.title || props.sample.prompt}
            />
          )}
          {!posterUrl && (
            <Film
              className='text-muted-foreground absolute top-1/2 left-1/2 size-8 -translate-x-1/2 -translate-y-1/2'
              aria-hidden='true'
            />
          )}
          <div className='absolute inset-x-0 bottom-0 flex items-end justify-between gap-2 bg-linear-to-t from-black/70 to-transparent p-2 pt-8 text-white'>
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

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
import { Download, Film, LoaderCircle, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { toIntlLocale } from '@/i18n/languages'
import { formatTimestampRelative } from '@/lib/format'

import type { VideoGeneration, VideoGenerationStatus } from '../types'
import { getVideoProgress } from '../video-domain'

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
  onDelete: (generation: VideoGeneration) => void
}

export function VideoGenerationCard(props: VideoGenerationCardProps) {
  const { t, i18n } = useTranslation()
  const status = props.generation.status
  const progress = getVideoProgress(props.generation)
  const ready = status === 'ready' && props.generation.video_url

  return (
    <article className='bg-card ring-border/70 overflow-hidden rounded-lg ring-1'>
      <div className='bg-muted relative aspect-video overflow-hidden'>
        {ready ? (
          <video
            className='size-full object-cover'
            src={props.generation.video_url}
            poster={props.generation.poster_url}
            controls
            playsInline
            preload='none'
            aria-label={props.generation.prompt}
          />
        ) : (
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

        {props.generation.failure_reason && (
          <p className='text-destructive line-clamp-2 text-xs' role='alert'>
            {props.generation.failure_reason}
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
            <Button
              size='icon-sm'
              variant='ghost'
              onClick={() => props.onDelete(props.generation)}
              aria-label={t('videoStudio.delete')}
            >
              <Trash2 aria-hidden='true' />
            </Button>
          </div>
        </div>
      </div>
    </article>
  )
}

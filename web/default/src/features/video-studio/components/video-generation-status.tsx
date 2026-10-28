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
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import type { VideoGeneration } from '../types'
import { getVideoGenerationFailureMessageKey } from '../video-domain'

type VideoGenerationStatusProps = {
  generation: VideoGeneration
  compact?: boolean
  className?: string
}

export function VideoGenerationStatus(props: VideoGenerationStatusProps) {
  const { t } = useTranslation()
  const status = props.generation.status
  const failureKey = getVideoGenerationFailureMessageKey(props.generation)
  const labels = {
    queued: t('Queued'),
    processing: t('Generating video'),
    archiving: t('Preparing your video'),
    ready: t('Ready to play'),
    failed: t('Video unavailable'),
  }
  const rawProgress = props.generation.progress.trim()
  const numericProgress = /^\d+(?:\.\d+)?%?$/.test(rawProgress)
    ? Number(rawProgress.replace('%', ''))
    : Number.NaN
  const progress =
    status === 'processing' && numericProgress > 0 && numericProgress <= 100
      ? numericProgress
      : undefined

  return (
    <div
      className={cn(
        'video-studio-generation-status',
        props.compact && 'is-compact',
        props.className
      )}
      data-status={status}
    >
      <span
        className='video-studio-status-label'
        role='status'
        aria-live={props.compact ? 'off' : 'polite'}
        aria-atomic='true'
      >
        <span className='video-studio-status-dot' aria-hidden='true' />
        {labels[status]}
      </span>
      {progress !== undefined && (
        <span className='video-studio-status-percent'>{progress}%</span>
      )}
      {!props.compact && status === 'queued' && (
        <p>
          {t(
            'Your request is in the queue. You can keep editing your next idea.'
          )}
        </p>
      )}
      {!props.compact && status === 'processing' && (
        <p>
          {t(
            'Your video is taking shape. The result will appear here when it is ready.'
          )}
        </p>
      )}
      {!props.compact && status === 'archiving' && (
        <p>
          {t(
            'Generation is complete. The video is being prepared for playback.'
          )}
        </p>
      )}
      {!props.compact && status === 'failed' && (
        <p>
          {failureKey
            ? t(failureKey)
            : t(
                'The video is currently unavailable. Check the task for more information.'
              )}
        </p>
      )}
      {!props.compact && progress !== undefined && (
        <progress
          className='video-studio-generation-progress'
          max={100}
          value={progress}
          aria-label={t('Generation progress')}
        />
      )}
    </div>
  )
}

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
import { Film, Play, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import type { VideoGeneration } from '../types'
import { getVideoGenerationFailureMessageKey } from '../video-domain'
import { VideoGenerationStatus } from './video-generation-status'

type VideoGenerationCardProps = {
  generation: VideoGeneration
  highlighted?: boolean
  onOpen: (generation: VideoGeneration) => void
  onDelete: (generation: VideoGeneration) => void
}

export function VideoGenerationCard(props: VideoGenerationCardProps) {
  const { t } = useTranslation()
  const [failedPoster, setFailedPoster] = useState<string | undefined>()
  const generation = props.generation
  const ready = generation.status === 'ready'
  const deletable = ready || generation.status === 'failed'
  const failureMessageKey = getVideoGenerationFailureMessageKey(generation)
  const poster =
    generation.poster_url && failedPoster !== generation.poster_url
      ? generation.poster_url
      : undefined

  return (
    <article
      className={cn(
        'video-studio-library-card',
        props.highlighted && 'is-selected'
      )}
      aria-current={props.highlighted ? 'true' : undefined}
    >
      <button
        type='button'
        className='video-studio-library-preview'
        data-video-generation-id={generation.id}
        onClick={() => props.onOpen(generation)}
        aria-label={t('View video details: {{prompt}}', {
          prompt: generation.prompt,
        })}
      >
        {poster ? (
          <img
            src={poster}
            alt=''
            loading='lazy'
            decoding='async'
            onError={() => setFailedPoster(poster)}
          />
        ) : (
          <span className='video-studio-library-placeholder'>
            <Film aria-hidden='true' />
          </span>
        )}
        <VideoGenerationStatus generation={generation} compact />
        {ready && (
          <span className='video-studio-library-play'>
            <Play aria-hidden='true' />
          </span>
        )}
      </button>
      <div className='video-studio-library-body'>
        <h3>{generation.prompt}</h3>
        <p className='video-studio-meta'>{generation.model}</p>
        {failureMessageKey && (
          <p className='video-studio-failure'>{t(failureMessageKey)}</p>
        )}
        <div className='video-studio-library-tools'>
          <code title={generation.task_id}>{generation.task_id}</code>
          {deletable && (
            <Button
              variant='ghost'
              size='icon'
              onClick={() => props.onDelete(generation)}
              aria-label={t('videoStudio.delete')}
            >
              <Trash2 aria-hidden='true' />
            </Button>
          )}
        </div>
      </div>
    </article>
  )
}

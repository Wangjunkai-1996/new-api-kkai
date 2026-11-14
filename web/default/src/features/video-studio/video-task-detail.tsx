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
import { AlertTriangle, ArrowRight, Copy, Expand, Film, X } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { VideoGenerationStatus } from './components/video-generation-status'
import { VIDEO_PARAMETER_LABEL_KEYS } from './schemas'
import type { VideoGeneration } from './types'
import { getVideoGenerationFailureMessageKey } from './video-domain'

type VideoTaskDetailProps = {
  generation: VideoGeneration
  posterUrl?: string
  locale?: string
  mobile?: boolean
  onClose?: () => void
  onCopyTask: () => void
  onPreview: () => void
}

export function VideoTaskDetail(props: VideoTaskDetailProps) {
  const { t } = useTranslation()
  const generation = props.generation
  const failed = generation.status === 'failed'
  const ready = generation.status === 'ready'
  const failureKey = getVideoGenerationFailureMessageKey(generation)
  const modes: Record<VideoGeneration['mode'], string> = {
    text_to_video: t('Text to video'),
    image_to_video: t('Image to video'),
    reference_to_video: t('videoStudio.mode.reference'),
    first_frame_to_video: t('videoStudio.mode.firstFrame'),
    first_last_frame: t('First and last frames'),
  }
  const parameters = Object.entries(generation.parameters)
  const ratio =
    generation.parameters.ratio ?? generation.parameters.aspect_ratio
  const duration = generation.parameters.duration
  const [failedPosterUrl, setFailedPosterUrl] = useState<string | null>(null)
  const usablePosterUrl = props.posterUrl && props.posterUrl !== failedPosterUrl
  let stageMessage = t(
    'Your video is taking shape. The result will appear here when it is ready.'
  )
  if (generation.status === 'queued') {
    stageMessage = t(
      'Your request is in the queue. You can keep editing your next idea.'
    )
  }
  if (generation.status === 'archiving') {
    stageMessage = t(
      'Generation is complete. The video is being prepared for playback.'
    )
  }
  let footerMessage = t('Refresh to check for status updates')
  if (ready) footerMessage = t('View video in the library')
  if (failed) footerMessage = t('Your task information is still available.')

  return (
    <div className='video-task-detail-inner'>
      <header className='video-task-detail-header'>
        <div>
          <p className='video-task-detail-label'>
            {ready ? t('Current result') : t('Task Details')}
          </p>
          <h3>
            {failed
              ? t('This video could not be generated')
              : `${t('Video task')} #${generation.id}`}
          </h3>
        </div>
        <div className='video-task-detail-actions'>
          <VideoGenerationStatus generation={generation} compact icon />
        </div>
        {props.mobile && (
          <Button
            type='button'
            variant='ghost'
            size='icon'
            className='video-task-close'
            aria-label={t('Close')}
            onClick={props.onClose}
          >
            <X aria-hidden='true' />
          </Button>
        )}
      </header>

      {failed ? (
        <div className='video-task-failure-body'>
          <div className='video-task-failure-message'>
            <AlertTriangle aria-hidden='true' />
            <div>
              <strong>{t('Video unavailable')}</strong>
              <p>
                {failureKey
                  ? t(failureKey)
                  : t(
                      'The video is currently unavailable. Check the task for more information.'
                    )}
              </p>
            </div>
          </div>
          <div className='video-task-failure-grid'>
            <section className='video-task-info-section'>
              <h4>{t('Full prompt')}</h4>
              <p className='video-task-full-prompt'>{generation.prompt}</p>
            </section>
            <div>
              <section className='video-task-info-section'>
                <h4>{t('Creation details')}</h4>
                <TaskMetadata
                  generation={generation}
                  locale={props.locale}
                  modes={modes}
                  parameters={parameters}
                />
              </section>
              <TaskId generation={generation} onCopy={props.onCopyTask} />
            </div>
          </div>
        </div>
      ) : (
        <div className='video-task-detail-body'>
          <div className='video-task-detail-media'>
            {ready && usablePosterUrl ? (
              <div className='video-task-media-frame'>
                <img
                  src={props.posterUrl}
                  alt={t('Video cover')}
                  onError={() => setFailedPosterUrl(props.posterUrl ?? null)}
                />
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  aria-label={t('Enlarge video cover')}
                  onClick={props.onPreview}
                >
                  <Expand aria-hidden='true' />
                </Button>
              </div>
            ) : (
              <div className='video-task-stage-frame'>
                <Film aria-hidden='true' />
                {ready ? (
                  <p>{t('Cover preview')}</p>
                ) : (
                  <>
                    <VideoGenerationStatus generation={generation} compact />
                    <p>{stageMessage}</p>
                  </>
                )}
              </div>
            )}
            {ready && (
              <div className='video-task-media-caption'>
                <strong>{t('Video cover')}</strong>
                <span>
                  {[
                    ratio,
                    duration !== undefined
                      ? `${duration} ${t('seconds')}`
                      : null,
                  ]
                    .filter((value) => value !== null && value !== undefined)
                    .join(' · ')}
                </span>
              </div>
            )}
          </div>
          <div className='video-task-detail-info'>
            <section className='video-task-info-section'>
              <h4>{t('Full prompt')}</h4>
              <p className='video-task-full-prompt'>{generation.prompt}</p>
            </section>
            <section className='video-task-info-section'>
              <h4>{t('Creation details')}</h4>
              <TaskMetadata
                generation={generation}
                locale={props.locale}
                modes={modes}
                parameters={parameters}
              />
            </section>
            <TaskId generation={generation} onCopy={props.onCopyTask} />
          </div>
        </div>
      )}

      <footer className='video-task-detail-footer'>
        <p>{footerMessage}</p>
        <Button
          variant={ready ? 'default' : 'outline'}
          className={
            ready ? 'video-task-primary-action' : 'video-task-quiet-action'
          }
          render={
            <Link
              to={ready ? '/video-studio/library' : '/video-studio/create'}
              search={{ task: generation.task_id, generation: generation.id }}
            />
          }
        >
          {ready ? t('View result') : t('View task')}
          <ArrowRight aria-hidden='true' />
        </Button>
      </footer>
    </div>
  )
}

function TaskMetadata(props: {
  generation: VideoGeneration
  locale?: string
  modes: Record<VideoGeneration['mode'], string>
  parameters: [string, string | number | boolean][]
}) {
  const { t } = useTranslation()
  return (
    <dl className='video-task-metadata'>
      <div>
        <dt>{t('Model')}</dt>
        <dd>{props.generation.model}</dd>
      </div>
      <div>
        <dt>{t('Generation mode')}</dt>
        <dd>{props.modes[props.generation.mode]}</dd>
      </div>
      {props.parameters.map(([key, value]) => (
        <div key={key}>
          <dt>
            {t(
              VIDEO_PARAMETER_LABEL_KEYS[
                key === 'aspect_ratio' ? 'ratio' : key
              ] ?? key
            )}
          </dt>
          <dd>
            {typeof value === 'boolean'
              ? t(value ? 'Enabled' : 'Disabled')
              : String(value)}
          </dd>
        </div>
      ))}
      <div>
        <dt>{t('Created at')}</dt>
        <dd>
          {new Date(props.generation.created_at * 1000).toLocaleString(
            props.locale
          )}
        </dd>
      </div>
    </dl>
  )
}

function TaskId(props: { generation: VideoGeneration; onCopy: () => void }) {
  const { t } = useTranslation()
  return (
    <div className='video-task-id'>
      <code>{props.generation.task_id}</code>
      <Button
        type='button'
        variant='ghost'
        size='icon'
        aria-label={t('Copy task ID')}
        title={t('Copy task ID')}
        onClick={props.onCopy}
      >
        <Copy aria-hidden='true' />
      </Button>
    </div>
  )
}

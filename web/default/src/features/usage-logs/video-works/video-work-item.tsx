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
  Download01Icon,
  InformationCircleIcon,
  PlayIcon,
  Video01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import failedArt from '@/assets/video-works/failed.webp'
import processingArt from '@/assets/video-works/processing.webp'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import dayjs from '@/lib/dayjs'

import { taskActionMapper } from '../lib/mappers'
import type { TaskLog } from '../types'
import {
  isVideoWorkActive,
  videoWorkElapsed,
  videoWorkModel,
} from './video-work-state'
import {
  VideoWorkCopy,
  VideoWorkCost,
  VideoWorkStatus,
} from './video-work-summary'

interface VideoWorkItemProps {
  log: TaskLog
  isAdmin: boolean
  onPreview: () => void
  onBilling: () => void
  onDownload: () => void
  downloading?: boolean
  renewPoster?: () => Promise<boolean>
}

export function VideoWorkItem(props: VideoWorkItemProps) {
  const { t } = useTranslation()
  const [failedPoster, setFailedPoster] = useState<string>()
  const [posterRevision, setPosterRevision] = useState(0)
  const log = props.log
  const active = isVideoWorkActive(log) || log.media?.state === 'processing'
  const model = videoWorkModel(log) || t('Unknown model')
  const poster = log.media?.state === 'ready' ? log.media.poster_url : undefined
  const showPoster = poster && poster !== failedPoster
  const completed = log.status === 'SUCCESS'
  const unavailable =
    log.result_discarded ||
    log.media?.state === 'unavailable' ||
    log.media?.state === 'restricted'
  const canPreview = completed && !active && !unavailable
  let action = t('View details')
  if (canPreview) action = t('View creations')
  else if (log.status === 'FAILURE') action = t('View failure reason')
  else if (active) action = t('View status')
  let stateArt: string | undefined
  if (log.status === 'FAILURE') stateArt = failedArt
  else if (active) stateArt = processingArt
  const media = log.media
  const duration = media?.duration_seconds
  const elapsed = videoWorkElapsed(log)
  const metadata = [t(taskActionMapper.getLabel(log.action, log.action))]
  if (media?.width && media.height) {
    metadata.push(`${media.width} × ${media.height}`)
  }
  if (duration) metadata.push(t('{{count}} seconds', { count: duration }))
  let mediaContent = (
    <HugeiconsIcon
      icon={Video01Icon}
      className='video-work-placeholder-icon'
      aria-hidden='true'
    />
  )
  if (showPoster) {
    mediaContent = (
      <img
        key={`${poster}:${posterRevision}`}
        src={poster}
        alt=''
        loading='lazy'
        onError={() => {
          if (!props.renewPoster) {
            setFailedPoster(poster)
            return
          }
          void props.renewPoster().then((renewed) => {
            if (renewed) setPosterRevision((value) => value + 1)
            else setFailedPoster(poster)
          })
        }}
      />
    )
  } else if (stateArt) {
    mediaContent = (
      <img className='video-work-state-art' src={stateArt} alt='' />
    )
  }

  return (
    <article
      className='video-work-item'
      aria-label={`${model} · ${log.task_id}`}
      data-state={stateArt || unavailable ? 'state' : 'video'}
    >
      <button
        type='button'
        className='video-work-media'
        onClick={props.onPreview}
        aria-label={`${action}: ${model}`}
      >
        {mediaContent}
        {canPreview && (
          <span className='video-work-play'>
            <HugeiconsIcon icon={PlayIcon} aria-hidden='true' />
          </span>
        )}
        {canPreview && duration && (
          <span className='video-work-duration'>
            {t('{{count}} seconds', { count: duration })}
          </span>
        )}
        {unavailable && completed && (
          <span className='video-work-media-note'>
            {log.result_discarded
              ? t('Result not retained')
              : t('Preview unavailable')}
          </span>
        )}
      </button>
      <div className='video-work-model-info'>
        <h2 className='video-work-model'>{model}</h2>
        <p className='video-work-metadata'>{metadata.join(' · ')}</p>
        <div className='video-work-task-id'>
          <code title={log.task_id}>{log.task_id}</code>
          <VideoWorkCopy taskId={log.task_id} />
        </div>
        {props.isAdmin && (
          <p className='video-work-owner'>
            {log.username || log.user_id} · {t('Channel')} #{log.channel_id}
          </p>
        )}
      </div>
      <div className='video-work-status-cell'>
        <VideoWorkStatus log={log} />
        {log.media?.state === 'processing' && completed && (
          <p>{t('Processing video')}</p>
        )}
        {elapsed !== undefined && (
          <p>{t('{{count}} seconds', { count: elapsed })}</p>
        )}
      </div>
      <time
        className='video-work-time'
        dateTime={
          log.submit_time > 0
            ? new Date(log.submit_time * 1000).toISOString()
            : undefined
        }
      >
        <span>
          {log.submit_time > 0
            ? dayjs.unix(log.submit_time).format('HH:mm:ss')
            : '—'}
        </span>
        <span>
          {log.submit_time > 0
            ? dayjs.unix(log.submit_time).format('YYYY-MM-DD')
            : ''}
        </span>
      </time>
      <VideoWorkCost log={log} onDetails={props.onBilling} />
      <div className='video-work-actions'>
        <Button size='sm' className='video-work-view' onClick={props.onPreview}>
          <HugeiconsIcon
            icon={canPreview ? PlayIcon : InformationCircleIcon}
            data-icon='inline-start'
            aria-hidden='true'
          />
          {action}
        </Button>
        <div className='video-work-tools'>
          {canPreview && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant='outline'
                    size='icon-sm'
                    aria-label={t('Download video')}
                    disabled={props.downloading}
                    onClick={props.onDownload}
                  />
                }
              >
                <HugeiconsIcon icon={Download01Icon} aria-hidden='true' />
                <span className='video-work-tool-label'>{t('Download')}</span>
              </TooltipTrigger>
              <TooltipContent>{t('Download video')}</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='outline'
                  size='icon-sm'
                  aria-label={t('Task Details')}
                  onClick={props.onPreview}
                />
              }
            >
              <HugeiconsIcon icon={InformationCircleIcon} aria-hidden='true' />
              <span className='video-work-tool-label'>{t('Details')}</span>
            </TooltipTrigger>
            <TooltipContent>{t('Task Details')}</TooltipContent>
          </Tooltip>
        </div>
      </div>
    </article>
  )
}

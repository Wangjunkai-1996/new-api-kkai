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
import { RefreshIcon, Video01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import failedArt from '@/assets/video-works/failed.webp'
import processingArt from '@/assets/video-works/processing.webp'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

import { getTaskArtifactDownloadUrl } from '../lib/task-artifacts'
import type { TaskLog } from '../types'
import { useVideoWorkArtifacts } from './use-video-work-artifacts'
import { VideoWorkDetails } from './video-work-details'
import { isVideoWorkActive, videoWorkModel } from './video-work-state'
import { VideoWorkCopy } from './video-work-summary'

interface VideoWorkPreviewProps {
  log: TaskLog
  isAdmin: boolean
  isRoot: boolean
  onClose: () => void
  onBilling: () => void
}

export function VideoWorkPreview(props: VideoWorkPreviewProps) {
  const { t } = useTranslation()
  const [failed, setFailed] = useState(false)
  const [revision, setRevision] = useState(0)
  const [selectedKey, setSelectedKey] = useState<string>()
  const active =
    isVideoWorkActive(props.log) || props.log.media?.state === 'processing'
  const query = useVideoWorkArtifacts(
    props.log,
    props.log.status === 'SUCCESS' &&
      !props.log.result_discarded &&
      props.log.media?.state !== 'restricted'
  )
  const hosted = query.data?.hostedVideo
  const artifacts = query.data?.artifacts ?? []
  const videos = artifacts.filter((artifact) => artifact.type === 'video')
  const selected =
    videos.find((artifact) => artifact.key === selectedKey) || videos[0]
  let source = selected?.content_url || query.data?.legacyContentUrl
  let download = source ? getTaskArtifactDownloadUrl(source) : undefined
  if (hosted) {
    source = hosted.state === 'ready' ? hosted.content_url : undefined
    download = hosted.state === 'ready' ? hosted.download_url : undefined
  }
  const model = videoWorkModel(props.log) || t('Unknown model')
  let title = t('Preview unavailable')
  let description = t(
    'This video is temporarily unavailable. You can try again.'
  )
  let art: string | undefined
  if (active || hosted?.state === 'processing') {
    title = t('Your video is being prepared')
    description = t(
      'You can preview and download it when processing is complete.'
    )
    art = processingArt
  } else if (props.log.status === 'FAILURE') {
    title = t('This video could not be generated')
    description = t('Review the failure reason and billing details below.')
    art = failedArt
  } else if (props.log.result_discarded) {
    title = t('Result not retained')
    description = t(
      'The result was returned in the API response and was not saved as an artifact'
    )
  } else if (
    hosted?.state === 'restricted' ||
    props.log.media?.state === 'restricted'
  ) {
    title = t('Media access unavailable')
    description = t('You can still view this task and its billing information.')
  }
  const retry = async () => {
    const result = await query.refetch()
    if (!result.error) {
      setFailed(false)
      setRevision((value) => value + 1)
    }
  }
  let playerContent = (
    <div
      className='video-work-player-state'
      role={query.error || failed ? 'alert' : 'status'}
    >
      {art ? (
        <img src={art} alt='' />
      ) : (
        <HugeiconsIcon icon={Video01Icon} aria-hidden='true' />
      )}
      <h3>{title}</h3>
      <p>{description}</p>
      {!active &&
        props.log.status === 'SUCCESS' &&
        !props.log.result_discarded &&
        hosted?.state !== 'restricted' &&
        props.log.media?.state !== 'restricted' && (
          <Button
            variant='outline'
            disabled={query.isFetching}
            onClick={() => void retry()}
          >
            <HugeiconsIcon
              icon={RefreshIcon}
              data-icon='inline-start'
              aria-hidden='true'
            />
            {t('Retry')}
          </Button>
        )}
    </div>
  )
  if (query.isLoading) {
    playerContent = (
      <Skeleton
        className='aspect-video w-full rounded-lg'
        aria-label={t('Loading...')}
      />
    )
  } else if (source && !failed && !query.error) {
    playerContent = (
      <video
        key={`${source}:${revision}:${selected?.key || ''}`}
        className='video-work-player'
        src={source}
        controls
        playsInline
        preload='metadata'
        poster={props.log.media?.poster_url}
        onError={() => {
          void query.renewMedia().then((renewed) => {
            if (renewed) setRevision((value) => value + 1)
            else setFailed(true)
          })
        }}
      />
    )
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('View creations')}
      description={model}
      contentClassName='video-work-preview-dialog sm:max-w-[900px]'
      bodyClassName='video-work-preview-body'
    >
      <div className='video-work-preview-layout'>
        <div className='video-work-player-panel'>
          {playerContent}
          {videos.length > 1 && (
            <div
              className='video-work-artifact-picker'
              aria-label={t('Generated works')}
            >
              {videos.map((video, index) => (
                <Button
                  key={video.key}
                  size='sm'
                  variant={selected?.key === video.key ? 'secondary' : 'ghost'}
                  aria-pressed={selected?.key === video.key}
                  onClick={() => {
                    setSelectedKey(video.key)
                    setFailed(false)
                  }}
                >
                  {t('Video')} {index + 1}
                </Button>
              ))}
            </div>
          )}
          <div className='video-work-preview-id'>
            <code>{props.log.task_id}</code>
            <VideoWorkCopy taskId={props.log.task_id} />
          </div>
          {props.log.fail_reason && (
            <div className='video-work-failure'>
              <strong>{t('Fail Reason')}</strong>
              <p>{props.log.fail_reason}</p>
            </div>
          )}
          {artifacts.some((artifact) => artifact.type !== 'video') && (
            <div className='video-work-attachments'>
              {artifacts
                .filter((artifact) => artifact.type !== 'video')
                .map((artifact) => (
                  <a
                    key={artifact.key}
                    href={getTaskArtifactDownloadUrl(artifact.content_url)}
                    target='_blank'
                    rel='noopener noreferrer'
                  >
                    {artifact.key}
                  </a>
                ))}
            </div>
          )}
        </div>
        <VideoWorkDetails
          log={props.log}
          isAdmin={props.isAdmin}
          isRoot={props.isRoot}
          download={download}
          onBilling={props.onBilling}
        />
      </div>
    </Dialog>
  )
}

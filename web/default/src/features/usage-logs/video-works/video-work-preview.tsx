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
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import processingArt from '@/assets/video-works/processing.webp'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

import { prepareVideoPosters } from '../api'
import { getTaskArtifactDownloadUrl } from '../lib/task-artifacts'
import type { TaskLog } from '../types'
import { useVideoWorkArtifacts } from './use-video-work-artifacts'
import { useVideoWorksIdentity } from './use-video-works-identity'
import { VideoWorkDetails } from './video-work-details'
import { VideoWorkFailure } from './video-work-failure'
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
  const preparedArtifacts = useRef(new Set<string>())
  const identity = useVideoWorksIdentity()
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
  const artifactKey = selected?.key
  const artifactHasPoster = Boolean(selected?.poster)
  const refetchArtifacts = query.refetch
  useEffect(() => {
    if (
      !identity.ready ||
      !identity.active ||
      props.log.status !== 'SUCCESS' ||
      !artifactKey ||
      artifactHasPoster ||
      props.log.poster?.artifact_key === artifactKey ||
      preparedArtifacts.current.has(artifactKey)
    ) {
      return
    }
    preparedArtifacts.current.add(artifactKey)
    const controller = new AbortController()
    void prepareVideoPosters(
      [props.log.id],
      props.isAdmin,
      controller.signal,
      artifactKey
    )
      .then(() => {
        if (!controller.signal.aborted) void refetchArtifacts()
      })
      .catch(() => {})
    return () => controller.abort()
  }, [
    identity.ready,
    identity.active,
    props.log.id,
    props.log.status,
    props.log.poster?.artifact_key,
    props.isAdmin,
    artifactKey,
    artifactHasPoster,
    refetchArtifacts,
  ])
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
        poster={
          hosted
            ? props.log.media?.poster_url
            : (selected?.poster?.url ??
              (props.log.poster?.artifact_key === selected?.key
                ? props.log.poster?.url
                : undefined))
        }
        onError={() => {
          void query.renewMedia().then((renewed) => {
            if (renewed) setRevision((value) => value + 1)
            else setFailed(true)
          })
        }}
      />
    )
  }
  if (props.log.status === 'FAILURE') {
    return (
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) props.onClose()
        }}
        title={t('Generation failed')}
        contentClassName='video-work-preview-dialog video-work-failure-dialog sm:max-w-[680px]'
        bodyClassName='video-work-failure-content'
      >
        <VideoWorkFailure log={props.log} />
        <VideoWorkDetails
          log={props.log}
          isAdmin={props.isAdmin}
          isRoot={props.isRoot}
          onBilling={props.onBilling}
        />
        <div className='video-work-preview-id'>
          <code>{props.log.task_id}</code>
          <VideoWorkCopy taskId={props.log.task_id} />
        </div>
      </Dialog>
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

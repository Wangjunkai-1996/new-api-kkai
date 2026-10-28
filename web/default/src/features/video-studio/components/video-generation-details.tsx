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
import { Copy, Download, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'

import { VIDEO_PARAMETER_LABEL_KEYS } from '../schemas'
import type { VideoGeneration } from '../types'
import { VideoGenerationPlayer } from './video-generation-player'
import { VideoGenerationStatus } from './video-generation-status'

export type VideoGenerationDetailsProps = {
  generation: VideoGeneration | null
  open: boolean
  onOpenChange: (open: boolean) => void
  active?: boolean
  mediaIdentity?: string
  loading?: boolean
  denied?: boolean
  posterDenied?: boolean
  onPlay?: () => void
  onRefresh?: () => Promise<string | undefined>
  onCopyTask?: (generation: VideoGeneration) => void
  onDownload?: (generation: VideoGeneration) => void
  onDelete?: (generation: VideoGeneration) => void
  deletePending?: boolean
}

export function VideoGenerationDetails(props: VideoGenerationDetailsProps) {
  const { t, i18n } = useTranslation()
  const generation = props.generation
  if (!generation) return null
  const parameters = Object.entries(generation.parameters)
  const deletable =
    generation.status === 'ready' || generation.status === 'failed'
  const modes = {
    text_to_video: t('Text to video'),
    image_to_video: t('Image to video'),
    first_last_frame: t('First and last frames'),
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Video details')}
      description={generation.model}
      contentClassName='video-studio-v2 video-studio-details-dialog sm:max-w-[960px]'
      bodyClassName='video-studio-details-body'
    >
      <div className='video-studio-details-layout'>
        <VideoGenerationPlayer
          generation={generation}
          active={props.open && (props.active ?? true)}
          mediaIdentity={props.mediaIdentity}
          loading={props.loading}
          denied={props.denied}
          posterDenied={props.posterDenied}
          onPlay={props.onPlay}
          onRefresh={props.onRefresh}
        />
        <section
          className='video-studio-details-info'
          aria-label={t('Video information')}
        >
          <VideoGenerationStatus generation={generation} compact />
          <p className='video-studio-details-prompt'>{generation.prompt}</p>
          <dl className='video-studio-details-metadata'>
            <div>
              <dt>{t('Model')}</dt>
              <dd>{generation.model}</dd>
            </div>
            <div>
              <dt>{t('Generation mode')}</dt>
              <dd>{modes[generation.mode]}</dd>
            </div>
            <div>
              <dt>{t('Created at')}</dt>
              <dd>
                {new Date(generation.created_at * 1000).toLocaleString(
                  toIntlLocale(i18n.resolvedLanguage || i18n.language)
                )}
              </dd>
            </div>
            {parameters.map(([key, value]) => (
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
          </dl>
          <div className='video-studio-details-task'>
            <span>{t('Task ID')}</span>
            <code>{generation.task_id}</code>
            {props.onCopyTask && (
              <Button
                variant='ghost'
                size='icon-lg'
                aria-label={t('Copy task ID')}
                onClick={() => props.onCopyTask?.(generation)}
              >
                <Copy aria-hidden='true' />
              </Button>
            )}
          </div>
          <div className='video-studio-details-actions'>
            {generation.status === 'ready' &&
              generation.download_url &&
              !props.denied &&
              (props.onDownload ? (
                <Button onClick={() => props.onDownload?.(generation)}>
                  <Download aria-hidden='true' />
                  {t('Download video')}
                </Button>
              ) : (
                <Button
                  render={<a href={generation.download_url} />}
                  nativeButton={false}
                >
                  <Download aria-hidden='true' />
                  {t('Download video')}
                </Button>
              ))}
            {deletable && props.onDelete && (
              <Button
                variant='ghost'
                disabled={props.deletePending}
                onClick={() => props.onDelete?.(generation)}
              >
                <Trash2 aria-hidden='true' />
                {t('Delete video')}
              </Button>
            )}
          </div>
        </section>
      </div>
    </Dialog>
  )
}

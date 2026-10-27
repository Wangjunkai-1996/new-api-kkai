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
import { Download01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import dayjs from '@/lib/dayjs'

import { taskActionMapper } from '../lib/mappers'
import { resolveTaskDetailAccess } from '../lib/task-details'
import type { TaskLog } from '../types'
import { videoWorkElapsed, videoWorkModel } from './video-work-state'
import { VideoWorkCost, VideoWorkStatus } from './video-work-summary'

export function VideoWorkDetails(props: {
  log: TaskLog
  isAdmin: boolean
  isRoot: boolean
  download?: string
  onBilling: () => void
}) {
  const { t } = useTranslation()
  const access = resolveTaskDetailAccess(props.log, props.isAdmin, props.isRoot)
  const model = videoWorkModel(props.log) || t('Unknown model')
  const download = props.download
  const elapsed = videoWorkElapsed(props.log)
  return (
    <aside className='video-work-detail-panel'>
      <VideoWorkStatus log={props.log} />
      <h2>{model}</h2>
      <dl>
        <div>
          <dt>{t('Type')}</dt>
          <dd>
            {t(taskActionMapper.getLabel(props.log.action, props.log.action))}
          </dd>
        </div>
        {props.log.media?.duration_seconds && (
          <div>
            <dt>{t('Video duration')}</dt>
            <dd>
              {t('{{count}} seconds', {
                count: props.log.media.duration_seconds,
              })}
            </dd>
          </div>
        )}
        {props.log.media?.width && props.log.media.height && (
          <div>
            <dt>{t('Resolution')}</dt>
            <dd>
              {props.log.media.width} × {props.log.media.height}
            </dd>
          </div>
        )}
        {elapsed !== undefined && (
          <div>
            <dt>{t('Generation time')}</dt>
            <dd>{t('{{count}} seconds', { count: elapsed })}</dd>
          </div>
        )}
        <div>
          <dt>{t('Submit Time')}</dt>
          <dd>
            {props.log.submit_time > 0
              ? dayjs.unix(props.log.submit_time).format('YYYY-MM-DD HH:mm:ss')
              : '—'}
          </dd>
        </div>
        {props.isAdmin && (
          <>
            <div>
              <dt>{t('User')}</dt>
              <dd>{props.log.username || props.log.user_id}</dd>
            </div>
            <div>
              <dt>{t('Channel')}</dt>
              <dd>#{props.log.channel_id}</dd>
            </div>
          </>
        )}
      </dl>
      <div className='video-work-detail-cost'>
        <span>
          {props.log.billing?.state === 'reserved'
            ? t('Current pre-charge')
            : t('Current charge')}
        </span>
        <VideoWorkCost log={props.log} onDetails={props.onBilling} />
      </div>
      {download && (
        <Button
          className='video-work-download w-full'
          nativeButton={false}
          render={
            <a href={download} target='_blank' rel='noopener noreferrer' />
          }
        >
          <HugeiconsIcon
            icon={Download01Icon}
            data-icon='inline-start'
            aria-hidden='true'
          />
          {t('Download video')}
        </Button>
      )}
      {(access.plugin ||
        access.runtime ||
        access.upstreamTaskId ||
        access.nodeName) && (
        <details className='video-work-technical'>
          <summary>{t('Technical details')}</summary>
          <dl>
            {access.plugin && (
              <div>
                <dt>{t('Task Plugin')}</dt>
                <dd>{access.plugin.name || access.plugin.key}</dd>
              </div>
            )}
            {access.upstreamTaskId && (
              <div>
                <dt>{t('Upstream Task ID')}</dt>
                <dd>{access.upstreamTaskId}</dd>
              </div>
            )}
            {access.nodeName && (
              <div>
                <dt>{t('Node Name')}</dt>
                <dd>{access.nodeName}</dd>
              </div>
            )}
            {access.runtime && (
              <div>
                <dt>{t('API Version')}</dt>
                <dd>{access.runtime.api_version}</dd>
              </div>
            )}
          </dl>
        </details>
      )}
    </aside>
  )
}

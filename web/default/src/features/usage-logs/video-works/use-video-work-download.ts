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
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { getTaskArtifacts } from '../api'
import { getTaskArtifactDownloadUrl } from '../lib/task-artifacts'
import type { TaskLog } from '../types'

export function useVideoWorkDownload(onMultipleVideos: (log: TaskLog) => void) {
  const { t } = useTranslation()
  const [downloadingId, setDownloadingId] = useState<number>()
  const downloadRequest = useRef<AbortController | null>(null)
  useEffect(() => () => downloadRequest.current?.abort(), [])
  const download = async (log: TaskLog) => {
    downloadRequest.current?.abort()
    const request = new AbortController()
    downloadRequest.current = request
    setDownloadingId(log.id)
    try {
      const projection = await getTaskArtifacts(log.task_id, {
        recordId: log.id,
        requireHosted: log.media?.source === 'studio',
        signal: request.signal,
      })
      if (request.signal.aborted) return
      const hosted = projection.hostedVideo
      const videos = projection.artifacts.filter(
        (artifact) => artifact.type === 'video'
      )
      if (!hosted && videos.length > 1) {
        onMultipleVideos(log)
        return
      }
      const content = videos[0]?.content_url || projection.legacyContentUrl
      let href: string | undefined
      if (hosted?.state === 'ready') href = hosted.download_url
      else if (!hosted && content) href = getTaskArtifactDownloadUrl(content)
      if (!href) {
        toast.error(
          t('This video is temporarily unavailable. You can try again.')
        )
        return
      }
      const link = document.createElement('a')
      link.href = href
      link.download = log.task_id
      link.rel = 'noopener noreferrer'
      document.body.append(link)
      link.click()
      link.remove()
    } catch {
      if (!request.signal.aborted) {
        toast.error(t('Could not download this video. Please try again.'))
      }
    } finally {
      if (!request.signal.aborted) setDownloadingId(undefined)
    }
  }
  return { download, downloadingId }
}

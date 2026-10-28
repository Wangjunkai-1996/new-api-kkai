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
import { AlertCircle, ArrowUpRight } from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth-store'
import { useVideoStudioSubmissionStore } from '@/stores/video-studio-submission-store'

import type { VideoGeneration } from '../types'

type VideoSubmissionNoticeProps = {
  generations?: VideoGeneration[]
  onInspectTask: (taskId: string) => void
}

export function VideoSubmissionNotice(props: VideoSubmissionNoticeProps) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const ownerId = useVideoStudioSubmissionStore((state) => state.ownerId)
  const records = useVideoStudioSubmissionStore((state) => state.records)
  const storageError = useVideoStudioSubmissionStore(
    (state) => state.storageError
  )
  const hydrate = useVideoStudioSubmissionStore((state) => state.hydrate)
  const confirmGenerations = useVideoStudioSubmissionStore(
    (state) => state.confirmGenerations
  )

  useEffect(() => {
    hydrate(userId)
  }, [hydrate, userId])
  useEffect(() => {
    if (!userId || !props.generations) return
    try {
      confirmGenerations(userId, props.generations)
    } catch {
      // The store retains the pending guard and exposes the persistence error.
    }
  }, [confirmGenerations, props.generations, userId, records])

  if (!userId || ownerId !== userId) return null
  const pending = records.filter((record) => record.state !== 'accepted')
  if (!storageError && pending.length === 0) return null

  return (
    <section
      className='video-studio-submission-notice'
      aria-label={t('Submission recovery')}
    >
      {storageError && (
        <div className='video-studio-notice-row' role='alert'>
          <AlertCircle aria-hidden='true' />
          <p>
            {t(
              'Recovery storage is unavailable. Generation is paused to prevent duplicate charges.'
            )}
          </p>
          <Button
            type='button'
            variant='outline'
            onClick={() => hydrate(userId)}
          >
            {t('Check storage again')}
          </Button>
        </div>
      )}
      {pending.map((record) => (
        <div className='video-studio-notice-row' key={record.operationId}>
          <AlertCircle aria-hidden='true' />
          <div className='min-w-0 flex-1'>
            <p className='font-medium'>{t('Submission needs verification')}</p>
            <p>
              {t(
                'This request may have been accepted. Check its status before submitting the same content again.'
              )}
            </p>
            {record.taskId && (
              <span className='flex items-center gap-1'>
                <code className='break-all'>{record.taskId}</code>
                <CopyButton
                  value={record.taskId}
                  aria-label={t('Copy task ID')}
                />
              </span>
            )}
            {!record.taskId && (
              <p>
                {t(
                  'No task ID was returned. Keep this notice and check your task history.'
                )}
              </p>
            )}
          </div>
          {record.taskId && (
            <Button
              type='button'
              variant='outline'
              onClick={() => {
                if (record.taskId) props.onInspectTask(record.taskId)
              }}
            >
              {t('Check task status')}
              <ArrowUpRight aria-hidden='true' />
            </Button>
          )}
        </div>
      ))}
    </section>
  )
}

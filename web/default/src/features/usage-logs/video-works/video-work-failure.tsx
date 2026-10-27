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
import { Copy01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import failedArt from '@/assets/video-works/failed.webp'
import { Button } from '@/components/ui/button'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'

import type { TaskLog } from '../types'
import { videoWorkFailure } from './video-work-failure-copy'

export function VideoWorkFailure(props: { log: TaskLog }) {
  const { t } = useTranslation()
  const copy = useCopyToClipboard()
  const failure = videoWorkFailure(props.log, t)
  const message = props.log.failure?.message?.trim()
  return (
    <>
      <div className='video-work-failure-heading'>
        <img src={failedArt} alt='' />
        <div>
          <h2>{failure.title}</h2>
          {failure.explanation && <p>{failure.explanation}</p>}
        </div>
      </div>
      {message && (
        <section
          className='video-work-failure-original'
          aria-label={t('Provider error details')}
        >
          <header>
            <h3>{t('Provider error details')}</h3>
            <Button
              variant='ghost'
              size='sm'
              onClick={() => void copy.copyToClipboard(message)}
            >
              <HugeiconsIcon
                icon={Copy01Icon}
                data-icon='inline-start'
                aria-hidden='true'
              />
              {copy.copiedText === message
                ? t('Copied')
                : t('Copy error details')}
            </Button>
          </header>
          <p>{message}</p>
        </section>
      )}
      <section
        className='video-work-failure-advice'
        aria-label={t('Suggested next steps')}
      >
        <h3>{t('Suggested next steps')}</h3>
        <p>{failure.guidance}</p>
      </section>
    </>
  )
}

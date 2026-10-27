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
  Alert02Icon,
  CheckmarkCircle02Icon,
  Clock01Icon,
  Copy01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { formatLogQuota } from '@/lib/format'

import { getTaskBillingStateLabel } from '../lib/task-billing'
import type { TaskLog } from '../types'
import { isVideoWorkActive } from './video-work-state'

export function VideoWorkStatus(props: { log: TaskLog }) {
  const { t } = useTranslation()
  let label = t('Status unknown')
  let tone = 'neutral'
  let icon = Clock01Icon
  if (props.log.status === 'SUCCESS') {
    label = t('Completed')
    tone = 'success'
    icon = CheckmarkCircle02Icon
  } else if (props.log.status === 'FAILURE') {
    label = t('Generation failed')
    tone = 'failed'
    icon = Alert02Icon
  } else if (props.log.status === 'IN_PROGRESS') {
    label = t('Generating')
    tone = 'processing'
  } else if (isVideoWorkActive(props.log)) {
    label = t('Queued')
    tone = 'processing'
  }
  return (
    <span className='video-work-status' data-tone={tone}>
      <HugeiconsIcon icon={icon} aria-hidden='true' />
      {label}
    </span>
  )
}

export function VideoWorkCost(props: { log: TaskLog; onDetails?: () => void }) {
  const { t } = useTranslation()
  return (
    <div className='video-work-cost'>
      <span className='video-work-amount'>
        {props.log.billing?.amount_available === true
          ? formatLogQuota(props.log.quota)
          : '—'}
      </span>
      <span className='video-work-billing-state'>
        {getTaskBillingStateLabel(props.log.billing?.state, t)}
      </span>
      {props.onDetails && (
        <Button
          variant='link'
          className='video-work-billing-link'
          onClick={props.onDetails}
        >
          {t('Billing Details')}
        </Button>
      )}
    </div>
  )
}

export function VideoWorkCopy(props: { taskId: string }) {
  const { t } = useTranslation()
  const copy = useCopyToClipboard()
  const label =
    copy.copiedText === props.taskId ? t('Copied') : t('Copy task ID')
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant='ghost'
            size='icon-xs'
            className='video-work-copy'
            aria-label={label}
            onClick={() => void copy.copyToClipboard(props.taskId)}
          />
        }
      >
        <HugeiconsIcon icon={Copy01Icon} aria-hidden='true' />
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

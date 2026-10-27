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
import { Cancel01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import { TaskBillingDetails } from '../components/task-billing'
import type { TaskLog } from '../types'
import { videoWorkModel } from './video-work-state'
import { VideoWorkCopy } from './video-work-summary'

export function VideoWorkBillingSheet(props: {
  log: TaskLog
  onClose: () => void
}) {
  const { t } = useTranslation()
  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <SheetContent
        className='video-work-billing-sheet w-full sm:max-w-[380px]'
        showCloseButton={false}
      >
        <SheetHeader className='border-b p-6 pr-14'>
          <SheetTitle>{t('Billing Details')}</SheetTitle>
          <SheetDescription>
            {videoWorkModel(props.log) || t('Unknown model')}
          </SheetDescription>
        </SheetHeader>
        <SheetClose
          render={
            <Button
              variant='ghost'
              size='icon'
              className='absolute top-4 right-4'
              aria-label={t('Close')}
            />
          }
        >
          <HugeiconsIcon icon={Cancel01Icon} aria-hidden='true' />
        </SheetClose>
        <div className='video-work-billing-body min-h-0 flex-1 overflow-y-auto px-6'>
          <TaskBillingDetails log={props.log} presentation='video' />
          <div className='video-work-preview-id'>
            <code>{props.log.task_id}</code>
            <VideoWorkCopy taskId={props.log.task_id} />
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}

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
import { Link } from '@tanstack/react-router'
import { CircleAlert, LoaderCircle, RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'

import type { VideoTokenGateState } from '../hooks/use-video-token-gate'

type VideoStudioEntryGateProps = {
  gate: VideoTokenGateState
}

export function VideoStudioEntryGate(props: VideoStudioEntryGateProps) {
  const { t } = useTranslation()
  const gate = props.gate

  if (gate.preparing) {
    return (
      <Empty className='min-h-80 rounded-none' role='status'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <LoaderCircle
              className='animate-spin motion-reduce:animate-none'
              aria-hidden='true'
            />
          </EmptyMedia>
          <EmptyTitle>{t('videoStudio.workspace.preparing')}</EmptyTitle>
          <EmptyDescription>
            {t('videoStudio.workspace.preparingDescription')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  let description =
    gate.createError || t('videoStudio.workspace.prepareFailedDescription')
  if (gate.access?.kind === 'group-unavailable') {
    description = t('videoStudio.videoKey.groupUnavailable', {
      group: gate.requiredGroup,
    })
  }
  if (gate.access?.kind === 'limit-reached') {
    description = t('videoStudio.videoKey.limitReached')
  }
  if (gate.access?.kind === 'models-unavailable') {
    description = t('videoStudio.videoKey.modelsUnavailable')
  }

  return (
    <Empty className='min-h-80 rounded-none' role='alert'>
      <EmptyHeader>
        <EmptyMedia variant='icon'>
          <CircleAlert className='text-destructive' aria-hidden='true' />
        </EmptyMedia>
        <EmptyTitle>{t('videoStudio.workspace.prepareFailed')}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent className='flex-row flex-wrap justify-center'>
        {gate.access?.kind === 'limit-reached' && (
          <Button
            size='sm'
            variant='outline'
            nativeButton={false}
            render={<Link to='/keys' />}
          >
            {t('videoStudio.videoKey.manageKeys')}
          </Button>
        )}
        {gate.actionAvailable && (
          <Button type='button' size='sm' onClick={gate.retry}>
            <RotateCw data-icon='inline-start' aria-hidden='true' />
            {t('videoStudio.retry')}
          </Button>
        )}
      </EmptyContent>
    </Empty>
  )
}

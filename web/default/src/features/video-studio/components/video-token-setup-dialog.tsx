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
import { KeyRound, LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

type VideoTokenSetupDialogProps = {
  open: boolean
  requiredGroup: string
  creating: boolean
  errorMessage?: string | null
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}

export function VideoTokenSetupDialog(props: VideoTokenSetupDialogProps) {
  const { t } = useTranslation()

  const handleOpenChange = (open: boolean) => {
    if (props.creating) return
    props.onOpenChange(open)
  }

  return (
    <AlertDialog open={props.open} onOpenChange={handleOpenChange}>
      <AlertDialogContent size='default'>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <KeyRound aria-hidden='true' />
          </AlertDialogMedia>
          <AlertDialogTitle>
            {t('videoStudio.videoKey.createTitle')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t('videoStudio.videoKey.createDescription', {
              group: props.requiredGroup,
            })}
          </AlertDialogDescription>
        </AlertDialogHeader>

        {props.errorMessage && (
          <p
            className='bg-destructive/10 text-destructive rounded-md px-3 py-2 text-sm'
            role='alert'
          >
            {props.errorMessage}
          </p>
        )}

        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.creating}>
            {t('videoStudio.cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            onClick={props.onConfirm}
            disabled={props.creating}
          >
            {props.creating && (
              <LoaderCircle
                className='animate-spin motion-reduce:animate-none'
                aria-hidden='true'
              />
            )}
            {props.creating
              ? t('videoStudio.videoKey.creating')
              : t('videoStudio.videoKey.createAndContinue')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

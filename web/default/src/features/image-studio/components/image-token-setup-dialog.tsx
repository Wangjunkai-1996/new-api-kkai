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

import type { ImageTokenGateState } from '../hooks/use-image-token-gate'

export function ImageTokenSetupDialog(props: { gate: ImageTokenGateState }) {
  const { t } = useTranslation()
  const group =
    props.gate.capability?.required_group || t('imageStudio.token.imageGroup')
  return (
    <AlertDialog
      open={props.gate.dialogOpen}
      onOpenChange={props.gate.setDialogOpen}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <KeyRound aria-hidden='true' />
          </AlertDialogMedia>
          <AlertDialogTitle>
            {t('imageStudio.token.createTitle')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t('imageStudio.token.createDescription', { group })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {props.gate.createError && (
          <p className='text-destructive text-sm' role='alert'>
            {t('imageStudio.token.createFailed')}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.gate.creating}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={props.gate.creating}
            onClick={(event) => {
              event.preventDefault()
              void props.gate.createAndContinue()
            }}
          >
            {props.gate.creating && (
              <LoaderCircle
                className='animate-spin motion-reduce:animate-none'
                aria-hidden='true'
              />
            )}
            {t('imageStudio.token.createAndContinue')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

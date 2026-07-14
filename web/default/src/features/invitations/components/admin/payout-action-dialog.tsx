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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

import type { RebatePayoutCommand, RebateRecord } from '../../types'

export type PayoutActionInput =
  | {
      kind: 'payout'
      record: RebateRecord
      command: RebatePayoutCommand
    }
  | { kind: 'revoke-signup'; record: RebateRecord }

export const PayoutActionDialog = (props: {
  input: PayoutActionInput | null
  pending: boolean
  onClose: () => void
  onConfirm: () => void
}) => {
  const { t } = useTranslation()
  const title =
    props.input?.kind === 'revoke-signup'
      ? t('Revoke signup reward?')
      : t('Confirm payout {{action}}?', {
          action:
            props.input?.kind === 'payout' ? props.input.command.action : '',
        })

  return (
    <Dialog
      open={Boolean(props.input)}
      onOpenChange={(open) => !open && !props.pending && props.onClose()}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            disabled={props.pending}
            onClick={props.onClose}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={props.pending}
            onClick={props.onConfirm}
          >
            {props.pending ? t('Processing...') : t('Confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

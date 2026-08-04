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
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import type { ApproveAndPayCommand } from '../../types'

export type RebateRequestAction =
  | { kind: 'approve'; requestId: number }
  | {
      kind: 'approve-pay'
      requestId: number
      command: ApproveAndPayCommand
    }
  | {
      kind: 'batch-approve-pay'
      requestIds: number[]
      command: ApproveAndPayCommand
    }
  | { kind: 'reject'; requestId: number; reason: string }
  | { kind: 'reset'; requestId: number }
  | { kind: 'complete'; requestId: number }
  | { kind: 'undo-complete'; requestId: number }

const actionTitle = (action: RebateRequestAction): string => {
  if (action.kind === 'approve') return 'Approve rebate request?'
  if (action.kind === 'approve-pay') return 'Approve and pay rebate request?'
  if (action.kind === 'batch-approve-pay') {
    return 'Approve and pay selected requests?'
  }
  if (action.kind === 'reject') return 'Reject rebate request?'
  if (action.kind === 'reset') return 'Reset rebate review?'
  if (action.kind === 'complete') return 'Mark rebate request completed?'
  return 'Undo completed rebate request?'
}

export const RequestActionDialog = (props: {
  action: RebateRequestAction | null
  pending: boolean
  onActionChange: (action: RebateRequestAction | null) => void
  onConfirm: () => void
}) => {
  const { t } = useTranslation()
  const rejectAction = props.action?.kind === 'reject' ? props.action : null
  const rejectReason = rejectAction?.reason ?? ''
  const canConfirm =
    props.action?.kind !== 'reject' || rejectReason.trim().length > 0

  return (
    <Dialog
      open={Boolean(props.action)}
      onOpenChange={(open) => {
        if (!open && !props.pending) props.onActionChange(null)
      }}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {props.action ? t(actionTitle(props.action)) : ''}
          </DialogTitle>
        </DialogHeader>
        {rejectAction && (
          <div className='space-y-2'>
            <Label htmlFor='rebate-rejection-reason'>{t('Reason')}</Label>
            <Textarea
              id='rebate-rejection-reason'
              value={rejectAction.reason}
              onChange={(event) =>
                props.onActionChange({
                  ...rejectAction,
                  reason: event.target.value,
                })
              }
            />
          </div>
        )}
        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            disabled={props.pending}
            onClick={() => props.onActionChange(null)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={!canConfirm || props.pending}
            onClick={props.onConfirm}
          >
            {props.pending ? t('Processing...') : t('Confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

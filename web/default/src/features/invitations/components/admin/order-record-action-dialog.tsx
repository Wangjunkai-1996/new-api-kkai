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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import type { AdminRebateOrderRecord } from '../../types'

export type OrderRecordAction =
  | 'modify'
  | 'close'
  | 'reopen'
  | 'end-initialization'
  | 'extend-initialization'

export type OrderRecordActionInput = {
  record: AdminRebateOrderRecord
  action: OrderRecordAction
  rebateAmount?: number
  rebateRatio?: number
  initializationEndsAt?: number
}

export const OrderRecordActionDialog = (props: {
  input: OrderRecordActionInput | null
  pending: boolean
  onClose: () => void
  onConfirm: (input: OrderRecordActionInput) => void
}) => {
  const { t } = useTranslation()
  const [rebateAmount, setRebateAmount] = useState('')
  const [rebateRatio, setRebateRatio] = useState('')
  const [endsAt, setEndsAt] = useState('')

  const input = props.input
  if (!input) return null
  const action = input.action
  const canConfirm =
    (action !== 'modify' || rebateAmount !== '' || rebateRatio !== '') &&
    (action !== 'extend-initialization' || endsAt !== '')

  const confirm = () => {
    const next: OrderRecordActionInput = { ...input }
    if (rebateAmount !== '') next.rebateAmount = Number(rebateAmount)
    if (rebateRatio !== '') next.rebateRatio = Number(rebateRatio) / 100
    if (endsAt !== '') next.initializationEndsAt = new Date(endsAt).getTime()
    props.onConfirm(next)
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => !open && !props.pending && props.onClose()}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Confirm {{action}}', { action })}</DialogTitle>
        </DialogHeader>
        {action === 'modify' && (
          <div className='grid gap-4 sm:grid-cols-2'>
            <div className='space-y-2'>
              <Label htmlFor='order-rebate-amount'>
                {t('Rebate Amount (cents)')}
              </Label>
              <Input
                id='order-rebate-amount'
                type='number'
                min='0'
                value={rebateAmount}
                onChange={(event) => setRebateAmount(event.target.value)}
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='order-rebate-ratio'>{t('Rebate Rate')} (%)</Label>
              <Input
                id='order-rebate-ratio'
                type='number'
                min='0'
                max='100'
                step='0.01'
                value={rebateRatio}
                onChange={(event) => setRebateRatio(event.target.value)}
              />
            </div>
          </div>
        )}
        {action === 'extend-initialization' && (
          <div className='space-y-2'>
            <Label htmlFor='order-initialization-ends-at'>
              {t('Initialization Ends At')}
            </Label>
            <Input
              id='order-initialization-ends-at'
              type='datetime-local'
              value={endsAt}
              onChange={(event) => setEndsAt(event.target.value)}
            />
          </div>
        )}
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
            disabled={!canConfirm || props.pending}
            onClick={confirm}
          >
            {props.pending ? t('Processing...') : t('Confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

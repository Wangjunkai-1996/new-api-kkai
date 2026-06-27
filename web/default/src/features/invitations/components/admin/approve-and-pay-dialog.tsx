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
import { WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

import { formatRebateAmount } from '../../lib/format'
import type { RebateRequestAdmin } from '../../types'

interface ApproveAndPayDialogProps {
  open: boolean
  requests: RebateRequestAdmin[]
  loading: boolean
  onClose: () => void
  onConfirm: () => void
}

export function ApproveAndPayDialog({
  open,
  requests,
  loading,
  onClose,
  onConfirm,
}: ApproveAndPayDialogProps) {
  const { t } = useTranslation()
  const totalAmount = requests.reduce((sum, request) => sum + request.amount, 0)
  const isBatch = requests.length > 1

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()}>
      <DialogContent className='sm:max-w-[520px]'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <WalletCards className='size-5' />
            {isBatch
              ? t('Approve and Pay Selected Rebates')
              : t('Approve and Pay Rebate')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Approved rebate requests will be paid directly to the inviter balance.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-3 text-sm'>
          <div className='grid grid-cols-2 gap-3'>
            <div className='rounded-md border p-3'>
              <div className='text-muted-foreground text-xs'>
                {t('Rebate Request Count')}
              </div>
              <div className='mt-1 text-lg font-semibold'>
                {requests.length}
              </div>
            </div>
            <div className='rounded-md border p-3'>
              <div className='text-muted-foreground text-xs'>
                {t('Rebate Amount')}
              </div>
              <div className='mt-1 text-lg font-semibold'>
                {formatRebateAmount(totalAmount)}
              </div>
            </div>
          </div>

          {!isBatch && requests[0] && (
            <div className='flex justify-between gap-4 rounded-md border p-3'>
              <span className='text-muted-foreground'>{t('Rebate User')}</span>
              <span className='font-mono'>
                {requests[0].userName
                  ? `${requests[0].userName} (#${requests[0].userId})`
                  : `#${requests[0].userId}`}
              </span>
            </div>
          )}

          <div className='bg-muted rounded-md p-3 leading-6'>
            {t(
              'This action approves eligible requests, creates balance ledger entries through the payout service, and skips records that were already paid.'
            )}
          </div>
        </div>

        <DialogFooter>
          <Button type='button' variant='outline' onClick={onClose}>
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={loading || requests.length === 0}
            onClick={onConfirm}
          >
            {loading ? t('Processing...') : t('Approve and Pay')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

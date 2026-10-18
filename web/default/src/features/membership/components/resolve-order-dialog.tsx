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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import { resolveMembershipOrder } from '../api'
import { formatMembershipPrice } from '../lib'
import type { MembershipOrder } from '../types'

export function ResolveMembershipOrderDialog(props: {
  order: MembershipOrder
  onClose: () => void
}) {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const [action, setAction] = useState<'grant' | 'dismiss'>(
    props.order.status === 'paid_exception' ? 'grant' : 'dismiss'
  )
  const [reason, setReason] = useState('')
  const [operationId, setOperationId] = useState(() => crypto.randomUUID())
  const resolve = useMutation({
    mutationFn: resolveMembershipOrder,
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ['membership'] })
      props.onClose()
    },
    onError: (error) =>
      handleServerError(error, t('Order could not be resolved.')),
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !resolve.isPending) props.onClose()
      }}
      title={t('Review exceptional payment')}
      description={props.order.trade_no}
      footer={
        <Button
          disabled={!reason.trim() || resolve.isPending}
          onClick={() =>
            resolve.mutate({
              trade_no: props.order.trade_no,
              action,
              reason: reason.trim(),
              operation_id: operationId,
            })
          }
        >
          {resolve.isPending ? t('Saving...') : t('Confirm handling')}
        </Button>
      }
    >
      <div className='space-y-4'>
        {props.order.payments
          ?.filter((payment) => payment.status === 'paid_exception')
          .map((payment) => (
            <div
              key={payment.receipt_id}
              className='rounded-lg border p-3 text-sm'
            >
              <p className='break-all'>{payment.receipt_id}</p>
              <p>
                {formatMembershipPrice(
                  payment.amount_minor,
                  payment.currency,
                  i18n.language
                )}
              </p>
              <p className='text-muted-foreground'>{payment.reason}</p>
            </div>
          ))}
        <div className='space-y-2'>
          <Label htmlFor='membership-order-resolution'>{t('Action')}</Label>
          <select
            id='membership-order-resolution'
            className='bg-background h-10 w-full rounded-md border px-3 text-sm'
            value={action}
            onChange={(event) => {
              setAction(event.target.value as 'grant' | 'dismiss')
              setOperationId(crypto.randomUUID())
            }}
            disabled={resolve.isPending}
          >
            {props.order.status === 'paid_exception' && (
              <option value='grant'>
                {t('Retry activation from original order')}
              </option>
            )}
            <option value='dismiss'>{t('Record as handled manually')}</option>
          </select>
        </div>
        <p className='text-muted-foreground text-sm'>
          {action === 'grant'
            ? t(
                'Activation uses the original paid terms and checks current eligibility again. It does not bypass membership rules.'
              )
            : t(
                'This records manual handling only. It does not issue a refund or grant membership benefits.'
              )}
        </p>
        <div className='space-y-2'>
          <Label htmlFor='membership-order-reason'>
            {t('Handling reason')}
          </Label>
          <Textarea
            id='membership-order-reason'
            value={reason}
            onChange={(event) => {
              setReason(event.target.value)
              setOperationId(crypto.randomUUID())
            }}
            maxLength={1000}
            disabled={resolve.isPending}
          />
        </div>
        {resolve.isError && (
          <Alert variant='destructive'>
            <AlertDescription>
              {t('Order could not be resolved.')}
            </AlertDescription>
          </Alert>
        )}
      </div>
    </Dialog>
  )
}

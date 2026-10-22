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
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { formatQuota } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { createMembershipOrder } from '../api'
import {
  canPurchaseMembership,
  formatMembershipPrice,
  openMembershipCheckout,
} from '../lib'
import type {
  MembershipCheckout,
  MembershipSelf,
  MembershipTier,
} from '../types'

export function MembershipPurchaseDialog(props: {
  tier: MembershipTier
  membership: MembershipSelf
  salesEnabled: boolean
  releaseId: number
  onClose: () => void
  onRefresh?: () => void
  onOrderCreated: (order: MembershipCheckout) => void
}) {
  const { t, i18n } = useTranslation()
  const [methodKey, setMethodKey] = useState('')
  const methods = props.tier.payment_methods ?? []
  const selectedMethod =
    methods.find(
      (method) => `${method.provider}:${method.method}` === methodKey
    ) ?? methods[0]
  const upgrade = props.tier.eligibility?.action === 'upgrade'
  const account = props.membership.account
  const carry =
    upgrade && account
      ? Math.max(0, account.amount_total - account.amount_used)
      : 0
  const purchase = useMutation({
    mutationFn: createMembershipOrder,
    onSuccess: (order) => {
      props.onOrderCreated(order)
      if (openMembershipCheckout(order)) props.onClose()
    },
    onError: (error) => handleServerError(error, t('Payment request failed')),
  })

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !purchase.isPending) props.onClose()
      }}
      title={
        upgrade ? t('Upgrade membership') : t('Confirm membership purchase')
      }
      description={t('Benefits activate only after payment is confirmed.')}
      contentClassName='sm:max-w-lg'
    >
      <div className='space-y-5'>
        <div className='rounded-xl border p-4'>
          <p className='font-semibold'>{props.tier.name}</p>
          <p className='mt-2 text-2xl font-semibold'>
            {formatMembershipPrice(
              props.tier.price_minor,
              props.tier.currency,
              i18n.language
            )}
          </p>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('One-time payment')} · {t('30 days')}
          </p>
        </div>
        <dl className='space-y-3 text-sm'>
          <div className='flex justify-between gap-3'>
            <dt>{t('Included quota')}</dt>
            <dd className='font-medium'>{formatQuota(props.tier.quota)}</dd>
          </div>
          {upgrade && (
            <div className='flex justify-between gap-3'>
              <dt>{t('Estimated carried quota')}</dt>
              <dd className='font-medium'>{formatQuota(carry)}</dd>
            </div>
          )}
        </dl>
        {upgrade && (
          <p className='text-muted-foreground text-sm leading-relaxed'>
            {t(
              'Upgrade pays the full new price. Remaining quota carries over without conversion; reserved requests keep their original pricing. A new 30-day period starts. Future requests use the latest published tier discounts. Final carried quota is calculated when payment is confirmed.'
            )}
          </p>
        )}
        <p className='text-muted-foreground text-sm leading-relaxed'>
          {t(
            'Quota is not reset. Membership ends on expiry or settled exhaustion. Requests then continue using your wallet without membership discounts.'
          )}
        </p>
        <div className='space-y-2'>
          <Label htmlFor='membership-payment-method'>
            {t('Payment method')}
          </Label>
          <select
            id='membership-payment-method'
            value={
              selectedMethod
                ? `${selectedMethod.provider}:${selectedMethod.method}`
                : ''
            }
            onChange={(event) => setMethodKey(event.target.value)}
            className='border-input bg-background h-10 w-full rounded-md border px-3 text-sm'
            disabled={purchase.isPending}
          >
            {methods.map((method) => (
              <option
                key={`${method.provider}:${method.method}`}
                value={`${method.provider}:${method.method}`}
              >
                {method.name}
              </option>
            ))}
          </select>
        </div>
        {purchase.isError && (
          <Alert variant='destructive'>
            <AlertDescription>
              {t(
                'Payment could not be started. Refresh your membership before trying again.'
              )}
              <Button
                variant='outline'
                size='sm'
                onClick={() => {
                  props.onRefresh?.()
                  props.onClose()
                }}
              >
                {t('Refresh')}
              </Button>
            </AlertDescription>
          </Alert>
        )}
        {purchase.isSuccess &&
          !purchase.data.payment_url &&
          !purchase.data.submit_url && (
            <Alert>
              <AlertDescription>
                {t(
                  'Order created. Check its payment status below before placing another order.'
                )}
              </AlertDescription>
            </Alert>
          )}
        <Button
          className='w-full'
          disabled={
            purchase.isPending ||
            purchase.isSuccess ||
            !selectedMethod ||
            !canPurchaseMembership(props.tier, props.salesEnabled)
          }
          onClick={() => {
            if (selectedMethod) {
              purchase.mutate({
                tier_id: props.tier.id,
                payment_provider: selectedMethod.provider,
                payment_method: selectedMethod.method,
                release_id: props.releaseId,
                expected_account_id: account?.id ?? 0,
                expected_revision: account?.revision ?? 0,
              })
            }
          }}
        >
          {purchase.isPending
            ? t('Creating order...')
            : t('Continue to payment')}
        </Button>
      </div>
    </Dialog>
  )
}

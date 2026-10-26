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
import { ArrowRight, Clock3 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { formatQuota } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { createMembershipOrder } from '../api'
import {
  canPurchaseMembership,
  formatMembershipPrice,
  membershipEmblems,
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
  const [accepted, setAccepted] = useState(false)
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
        <span className='flex items-center gap-3 pr-5'>
          <img
            src={membershipEmblems[props.tier.style]}
            width={64}
            height={64}
            alt=''
            aria-hidden='true'
            className='size-16 shrink-0 object-contain'
          />
          <span className='min-w-0 leading-snug'>
            {upgrade
              ? t('Upgrade membership')
              : t('Confirm membership purchase')}
            <span className='mt-1 block text-base font-medium'>
              {props.tier.name}
            </span>
          </span>
        </span>
      }
      description={t('Benefits activate only after payment is confirmed.')}
      contentClassName='rounded-2xl sm:max-w-[28rem] sm:p-7'
    >
      <div className='space-y-4'>
        <dl className='bg-muted/40 space-y-3 rounded-lg border p-4 text-sm'>
          <div className='flex items-baseline justify-between gap-3'>
            <dt className='text-muted-foreground'>{t('One-time payment')}</dt>
            <dd className='font-semibold tabular-nums'>
              {formatMembershipPrice(
                props.tier.price_minor,
                props.tier.currency,
                i18n.language
              )}
            </dd>
          </div>
          <div className='flex items-baseline justify-between gap-3'>
            <dt className='text-muted-foreground'>{t('Included quota')}</dt>
            <dd className='font-medium tabular-nums'>
              {formatQuota(props.tier.quota)}
            </dd>
          </div>
          {upgrade && (
            <div className='flex items-baseline justify-between gap-3'>
              <dt className='text-muted-foreground'>
                {t('Estimated carried quota')}
              </dt>
              <dd className='font-medium tabular-nums'>
                + {formatQuota(carry)}
              </dd>
            </div>
          )}
          <div className='flex items-baseline justify-between gap-3 border-t pt-3'>
            <dt className='font-medium'>
              {t('Estimated quota after activation')}
            </dt>
            <dd className='font-semibold tabular-nums'>
              {formatQuota(props.tier.quota + carry)}
            </dd>
          </div>
          {upgrade && account && account.amount_reserved > 0 && (
            <div className='flex items-baseline justify-between gap-3 text-xs'>
              <dt className='text-muted-foreground'>
                {t('Reserved by requests')}
              </dt>
              <dd className='text-muted-foreground tabular-nums'>
                {formatQuota(account.amount_reserved)}
              </dd>
            </div>
          )}
        </dl>
        <div className='bg-muted/50 text-muted-foreground flex gap-2 rounded-lg border p-3 text-xs leading-relaxed'>
          <Clock3 className='mt-0.5 size-4 shrink-0' aria-hidden='true' />
          <div>
            <p>{t('A new 30-day period starts when payment is confirmed.')}</p>
            {upgrade && (
              <p className='mt-1'>
                {t(
                  'Upgrade pays the full new price. Remaining quota carries over without conversion; reserved requests keep their original pricing. A new 30-day period starts. Future requests use the latest published tier discounts. Final carried quota is calculated when payment is confirmed.'
                )}
              </p>
            )}
          </div>
        </div>
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
        <div className='flex items-start gap-2.5'>
          <Checkbox
            id='membership-purchase-confirm'
            checked={accepted}
            onCheckedChange={(checked) => setAccepted(checked === true)}
            disabled={purchase.isPending || purchase.isSuccess}
            className='mt-0.5'
          />
          <Label
            htmlFor='membership-purchase-confirm'
            className='text-muted-foreground cursor-pointer text-xs leading-relaxed font-normal'
          >
            {t(
              'I understand that quota expires, does not transfer to my wallet, and does not renew automatically.'
            )}
          </Label>
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
                  'Order created. Check its status in Membership orders before placing another order.'
                )}
              </AlertDescription>
            </Alert>
          )}
        <div className='grid grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)] gap-2.5 pt-1'>
          <Button
            variant='outline'
            size='lg'
            disabled={purchase.isPending}
            onClick={props.onClose}
          >
            {t('Cancel')}
          </Button>
          <Button
            size='lg'
            className='h-auto min-h-9 min-w-0 py-2'
            disabled={
              purchase.isPending ||
              purchase.isSuccess ||
              !accepted ||
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
            <span className='min-w-0 text-center leading-snug whitespace-normal'>
              {purchase.isPending
                ? t('Creating order...')
                : t('Continue to payment')}
            </span>
            {!purchase.isPending && (
              <ArrowRight className='size-4' aria-hidden='true' />
            )}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}

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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { getCurrencyLabel } from '@/lib/currency'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  getMembershipBillingReviews,
  resolveMembershipBillingReview,
} from '../api'
import type { MembershipBillingReview } from '../types'

export function MembershipBillingReviews() {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<MembershipBillingReview | null>(null)
  const query = useQuery({
    queryKey: ['membership-admin', 'billing-reviews'],
    queryFn: getMembershipBillingReviews,
  })
  return (
    <section className='space-y-4'>
      <div className='flex items-center justify-between gap-3'>
        <h2 className='text-lg font-semibold'>
          {t('Calls awaiting billing review')}
        </h2>
        <Button
          size='sm'
          variant='outline'
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
        >
          {t('Refresh')}
        </Button>
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Unknown upstream outcomes keep their original reservation until verified. No automatic refund is issued.'
        )}
      </p>
      {query.isPending && <p>{t('Loading...')}</p>}
      {query.isError && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Could not load billing reviews.')}
          </AlertDescription>
        </Alert>
      )}
      {query.data?.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t('No calls require billing review.')}
        </p>
      )}
      {query.data?.map((row) => (
        <article
          key={row.request_id}
          className='space-y-2 rounded-xl border p-4'
        >
          <p className='font-medium break-all'>{row.request_id}</p>
          <p className='text-muted-foreground text-sm'>
            {t('User ID')}: {row.user_id} · {row.group_key} ·{' '}
            {row.source === 'membership' ? t('Membership') : t('Wallet')}
          </p>
          <p className='text-sm'>
            {t('Reserved quota')}: {formatQuota(row.reserved_quota)}
          </p>
          <p className='text-muted-foreground text-xs break-words'>
            {row.review_reason}
          </p>
          <Button size='sm' variant='outline' onClick={() => setSelected(row)}>
            {t('Review billing')}
          </Button>
        </article>
      ))}
      {selected && (
        <BillingReviewDialog
          key={selected.request_id}
          record={selected}
          onClose={() => setSelected(null)}
        />
      )}
    </section>
  )
}

export function BillingReviewDialog(props: {
  record: MembershipBillingReview
  onClose: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [action, setAction] = useState<'settle' | 'release'>('settle')
  const [amount, setAmount] = useState('')
  const [reason, setReason] = useState('')
  const [verified, setVerified] = useState(false)
  const [operationId, setOperationId] = useState(() => crypto.randomUUID())
  const quota = action === 'release' ? 0 : parseQuotaFromDollars(Number(amount))
  const valid =
    reason.trim().length > 0 &&
    verified &&
    (action === 'release' ||
      (amount.trim() !== '' &&
        Number.isFinite(Number(amount)) &&
        Number(amount) >= 0 &&
        Number.isSafeInteger(quota) &&
        quota <= 2147483647))
  const resolve = useMutation({
    mutationFn: resolveMembershipBillingReview,
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({
          queryKey: ['membership-admin', 'billing-reviews'],
        }),
        client.invalidateQueries({ queryKey: ['membership'] }),
      ])
      props.onClose()
    },
    onError: (error) =>
      handleServerError(error, t('Billing review could not be saved.')),
  })
  return (
    <Dialog
      open
      title={t('Review billing')}
      description={props.record.request_id}
      onOpenChange={(open) => {
        if (!open && !resolve.isPending) props.onClose()
      }}
      footer={
        <Button
          disabled={!valid || resolve.isPending}
          onClick={() =>
            resolve.mutate({
              request_id: props.record.request_id,
              operation_id: operationId,
              action,
              actual_quota: quota,
              reason: reason.trim(),
            })
          }
        >
          {resolve.isPending ? t('Saving...') : t('Confirm handling')}
        </Button>
      }
    >
      <div className='space-y-4'>
        {props.record.snapshot && (
          <details className='rounded-lg border p-3'>
            <summary className='cursor-pointer text-sm'>
              {t('Frozen billing terms')}
            </summary>
            <pre className='mt-3 max-h-56 overflow-auto text-xs break-all whitespace-pre-wrap'>
              {props.record.snapshot}
            </pre>
          </details>
        )}
        <div className='space-y-2'>
          <Label htmlFor='billing-review-action'>{t('Action')}</Label>
          <select
            id='billing-review-action'
            className='bg-background h-10 w-full rounded-md border px-3 text-sm'
            value={action}
            disabled={resolve.isPending}
            onChange={(event) => {
              setAction(event.target.value as 'settle' | 'release')
              setVerified(false)
              setOperationId(crypto.randomUUID())
            }}
          >
            <option value='settle'>{t('Settle verified usage')}</option>
            <option value='release'>
              {t('Release a confirmed uncharged request')}
            </option>
          </select>
        </div>
        {action === 'settle' && (
          <div className='space-y-2'>
            <Label htmlFor='billing-review-amount'>
              {t('Verified charge')} ({getCurrencyLabel()})
            </Label>
            <Input
              id='billing-review-amount'
              type='number'
              min={0}
              step='any'
              value={amount}
              onChange={(event) => {
                setAmount(event.target.value)
                setOperationId(crypto.randomUUID())
              }}
              disabled={resolve.isPending}
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'The user charge is capped at the original reservation. Any verified excess is recorded separately.'
              )}
            </p>
          </div>
        )}
        <div className='space-y-2'>
          <Label htmlFor='billing-review-reason'>{t('Handling reason')}</Label>
          <Textarea
            id='billing-review-reason'
            value={reason}
            onChange={(event) => {
              setReason(event.target.value)
              setOperationId(crypto.randomUUID())
            }}
            maxLength={1000}
            disabled={resolve.isPending}
          />
        </div>
        <label className='flex items-start gap-2 text-sm'>
          <Checkbox
            checked={verified}
            onCheckedChange={(value) => setVerified(value === true)}
            disabled={resolve.isPending}
          />
          {action === 'release'
            ? t('I verified that the upstream request was not charged.')
            : t(
                'I verified the upstream usage and the original pricing terms.'
              )}
        </label>
        {resolve.isError && (
          <Alert variant='destructive'>
            <AlertDescription>
              {t('Billing review could not be saved.')}
            </AlertDescription>
          </Alert>
        )}
      </div>
    </Dialog>
  )
}

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

import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuota } from '@/lib/format'

import type { MembershipSelf } from '../types'

export function CurrentMembership(props: {
  membership: MembershipSelf
  now: number
}) {
  const { t, i18n } = useTranslation()
  const account = props.membership.account
  if (!account) {
    return (
      <section className='bg-card rounded-2xl border p-6'>
        <h2 className='font-semibold'>{t('My membership')}</h2>
        <p className='text-muted-foreground mt-2 text-sm'>
          {t(
            'Choose a membership to prepay quota and receive group discounts.'
          )}
        </p>
      </section>
    )
  }
  const active = account.status === 'active' && account.expires_at > props.now
  const remaining = Math.max(0, account.amount_total - account.amount_used)
  const available = Math.max(0, remaining - account.amount_reserved)
  let label = t('Expired')
  if (active) label = t('Active')
  else if (account.status === 'exhausted') label = t('Quota exhausted')
  else if (account.status === 'cancelled') label = t('Cancelled')

  return (
    <section className='bg-card rounded-2xl border p-5 sm:p-6'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div>
          <p className='text-muted-foreground text-xs'>{t('My membership')}</p>
          <h2 className='mt-1 text-lg font-semibold'>
            {props.membership.tier?.name ?? account.tier_id}
          </h2>
        </div>
        <Badge variant={active ? 'secondary' : 'outline'}>{label}</Badge>
      </div>
      <dl className='mt-5 grid grid-cols-2 gap-5 lg:grid-cols-4'>
        <div>
          <dt className='text-muted-foreground text-xs'>
            {t('Remaining quota')}
          </dt>
          <dd className='mt-1 text-xl font-semibold'>
            {formatQuota(remaining)}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>
            {t('Available now')}
          </dt>
          <dd className='mt-1 text-xl font-semibold'>
            {formatQuota(active ? available : 0)}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>
            {t('Reserved by requests')}
          </dt>
          <dd className='mt-1 text-xl font-semibold'>
            {formatQuota(account.amount_reserved)}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>{t('Expires at')}</dt>
          <dd className='mt-1 text-sm font-medium'>
            {new Date(account.expires_at * 1000).toLocaleString(
              toIntlLocale(i18n.language)
            )}
          </dd>
        </div>
      </dl>
      {active && (
        <Progress
          value={
            account.amount_total > 0
              ? Math.min(
                  100,
                  (account.amount_used / account.amount_total) * 100
                )
              : 0
          }
          className='mt-5 h-1.5'
        />
      )}
      <p className='text-muted-foreground mt-3 text-xs leading-relaxed'>
        {active
          ? t(
              'Reserved quota is temporarily unavailable. It does not end your membership.'
            )
          : t(
              'Membership discounts have ended. Requests continue using your wallet at your existing group price.'
            )}
      </p>
    </section>
  )
}

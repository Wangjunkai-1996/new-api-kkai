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

import { toIntlLocale } from '@/i18n/languages'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import { membershipEmblems } from '../lib'
import type { MembershipSelf } from '../types'

const appearances = {
  gold: {
    surface:
      'border-amber-300/50 bg-gradient-to-br from-amber-50 via-card to-card dark:border-amber-200/25 dark:from-[#2f2b22] dark:via-[#242528] dark:to-card',
    accent: 'text-amber-800 dark:text-amber-200',
    image: membershipEmblems.gold,
  },
  diamond: {
    surface:
      'border-sky-300/50 bg-gradient-to-br from-sky-50 via-card to-card dark:border-sky-200/25 dark:from-[#223441] dark:via-[#242a30] dark:to-card',
    accent: 'text-sky-800 dark:text-sky-200',
    image: membershipEmblems.diamond,
  },
  king: {
    surface:
      'border-violet-300/50 bg-gradient-to-br from-violet-50 via-card to-card dark:border-violet-200/25 dark:from-[#30273a] dark:via-[#27252d] dark:to-card',
    accent: 'text-violet-800 dark:text-violet-200',
    image: membershipEmblems.king,
  },
}

export function CurrentMembership(props: {
  membership: MembershipSelf
  now: number
}) {
  const { t, i18n } = useTranslation()
  const account = props.membership.account
  const appearance =
    appearances[props.membership.tier?.style ?? 'gold'] ?? appearances.gold
  const active = account?.status === 'active' && account.expires_at > props.now
  const available = account
    ? Math.max(
        0,
        account.amount_total - account.amount_used - account.amount_reserved
      )
    : 0
  const daysRemaining = account
    ? Math.ceil((account.expires_at - props.now) / 86_400)
    : 0
  let label = t('Expired')
  if (active) label = t('Active')
  else if (account?.status === 'exhausted') label = t('Quota exhausted')
  else if (account?.status === 'cancelled') label = t('Cancelled')

  return (
    <section
      aria-label={t('My membership')}
      className={cn(
        'relative overflow-hidden rounded-[16px] border p-5 sm:p-7',
        appearance.surface
      )}
    >
      <div className='grid grid-cols-[minmax(0,1fr)_6.5rem] gap-2 sm:grid-cols-[minmax(0,1fr)_11rem] sm:gap-6'>
        <div className='min-w-0'>
          <p
            className={cn('text-xs font-semibold uppercase', appearance.accent)}
          >
            {t('My membership')}
          </p>
          <div className='mt-2 flex flex-wrap items-center gap-2'>
            <h2 className='min-w-0 text-xl font-semibold sm:text-2xl'>
              {account
                ? (props.membership.tier?.name ?? account.tier_id)
                : t('Membership center')}
            </h2>
            {account && (
              <span
                className={cn(
                  'rounded px-2 py-0.5 text-xs font-medium',
                  active
                    ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/50 dark:text-emerald-200'
                    : 'bg-muted text-muted-foreground'
                )}
              >
                {label}
              </span>
            )}
          </div>
          {active ? (
            <>
              <p className='text-muted-foreground mt-6 text-xs'>
                {t('Available now')} · {t('Membership quota')}
              </p>
              <p className='mt-1 text-4xl leading-tight font-semibold break-all tabular-nums sm:text-5xl'>
                {formatQuota(available)}
              </p>
              <p className='text-muted-foreground mt-2 text-xs'>
                {t('{{count}} days remaining', {
                  count: Math.max(0, daysRemaining),
                })}
              </p>
            </>
          ) : (
            <p className='text-muted-foreground mt-5 max-w-sm text-sm leading-relaxed'>
              {account
                ? t(
                    'Membership discounts have ended. Requests continue using your wallet at your existing group price.'
                  )
                : t(
                    'Choose a membership to prepay quota and receive group discounts.'
                  )}
            </p>
          )}
        </div>
        <div className='flex min-w-0 flex-col items-center justify-center'>
          {(!account || props.membership.tier) && (
            <img
              src={appearance.image}
              alt=''
              aria-hidden='true'
              className='aspect-square w-24 object-contain drop-shadow-lg sm:w-40'
            />
          )}
          {account && props.membership.tier && (
            <p className={cn('hidden text-xs sm:block', appearance.accent)}>
              KKAI · {props.membership.tier.name}
            </p>
          )}
        </div>
      </div>
      {active && account && (
        <dl className='mt-6 grid grid-cols-2 gap-y-4 border-t border-current/10 pt-4 text-sm sm:grid-cols-3'>
          <div>
            <dt className='text-muted-foreground text-xs'>{t('Used')}</dt>
            <dd className='mt-1 font-medium tabular-nums'>
              {formatQuota(account.amount_used)}
            </dd>
          </div>
          <div className='border-l border-current/10 pl-4'>
            <dt className='text-muted-foreground text-xs'>
              {t('Reserved by requests')}
            </dt>
            <dd className='mt-1 font-medium tabular-nums'>
              {formatQuota(account.amount_reserved)}
            </dd>
          </div>
          <div className='col-span-2 flex flex-wrap items-baseline gap-x-2 sm:col-span-1 sm:block sm:border-l sm:border-current/10 sm:pl-4'>
            <dt className='text-muted-foreground text-xs'>{t('Expires at')}</dt>
            <dd className='mt-1 font-medium tabular-nums'>
              {new Date(account.expires_at * 1000).toLocaleString(
                toIntlLocale(i18n.language)
              )}
            </dd>
          </div>
        </dl>
      )}
    </section>
  )
}

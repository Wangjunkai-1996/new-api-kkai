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
import { toIntlLocale } from '@/i18n/languages'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  canPurchaseMembership,
  formatMembershipPrice,
  membershipEmblems,
} from '../lib'
import type { MembershipGroup, MembershipTier } from '../types'

const appearances = {
  gold: {
    accent: 'text-amber-700 dark:text-[#e2c17d]',
    border: 'border-amber-300/60 dark:border-[#4a4236]',
    current: 'bg-amber-50/50 dark:bg-[#29261f]',
    action:
      'border-amber-400/40 bg-amber-50 text-amber-800 hover:bg-amber-100 dark:border-amber-600/35 dark:bg-amber-900/15 dark:text-amber-200 dark:hover:bg-amber-900/25',
  },
  diamond: {
    accent: 'text-sky-700 dark:text-[#a2d5f5]',
    border: 'border-sky-300/60 dark:border-[#465e70]',
    current: 'bg-sky-50/60 dark:bg-[#202d37]',
    action:
      'border-sky-400/40 bg-sky-50 text-sky-800 hover:bg-sky-100 dark:border-sky-500/35 dark:bg-sky-900/20 dark:text-sky-200 dark:hover:bg-sky-900/30',
  },
  king: {
    accent: 'text-violet-700 dark:text-[#d6b9f5]',
    border: 'border-violet-300/60 dark:border-[#58476a]',
    current: 'bg-violet-50/60 dark:bg-[#2b2634]',
    action:
      'border-violet-400/40 bg-violet-50 text-violet-800 hover:bg-violet-100 dark:border-violet-500/35 dark:bg-violet-900/20 dark:text-violet-200 dark:hover:bg-violet-900/30',
  },
}

export function MembershipTierCard(props: {
  tier: MembershipTier
  sampleGroup?: MembershipGroup
  salesEnabled: boolean
  onPurchase: (tier: MembershipTier) => void
}) {
  const { t, i18n } = useTranslation()
  const style = appearances[props.tier.style] ?? appearances.gold
  const current = props.tier.eligibility?.action === 'current'
  let actionLabel = t('Choose membership')
  if (current) actionLabel = t('Current membership')
  else if (props.tier.eligibility?.action === 'upgrade') {
    actionLabel = t('Upgrade membership')
  } else if (!canPurchaseMembership(props.tier, props.salesEnabled)) {
    actionLabel = t('Currently unavailable')
  }

  const reasons: Record<string, string> = {
    downgrade_not_available: t(
      'Downgrades are not available during an active membership.'
    ),
    sales_closed: t('Membership sales are currently closed.'),
    payment_unavailable: t('No payment method is available for this tier.'),
  }
  const reason = reasons[props.tier.eligibility?.reason ?? '']
  const sampleDiscount = props.sampleGroup
    ? props.tier.discounts[props.sampleGroup.key]
    : undefined
  const samplePercent =
    sampleDiscount === undefined
      ? null
      : new Intl.NumberFormat(toIntlLocale(i18n.language), {
          style: 'percent',
          maximumFractionDigits: 2,
        }).format(sampleDiscount)

  return (
    <article
      className={cn(
        'bg-card flex h-full min-w-0 flex-col rounded-lg border p-5 transition-colors',
        style.border,
        current && style.current
      )}
    >
      <div className='grid grid-cols-[2.75rem_minmax(0,1fr)_auto] items-center gap-x-2 sm:block'>
        <div className='contents sm:flex sm:min-h-15 sm:items-start sm:justify-between sm:gap-3'>
          <div className='order-2 min-w-0 sm:order-1 sm:pt-1'>
            <h2 className='text-sm font-semibold sm:text-base'>
              {props.tier.name}
            </h2>
            <p className='text-muted-foreground mt-0.5 text-xs'>
              {current
                ? t('Current membership')
                : t('Prepaid calls, member pricing')}
            </p>
          </div>
          <img
            src={membershipEmblems[props.tier.style]}
            width={60}
            height={60}
            alt=''
            aria-hidden='true'
            className='order-1 size-11 shrink-0 object-contain sm:order-2 sm:size-15'
          />
        </div>
        <div className='order-3 min-w-0 text-right sm:mt-4 sm:text-left'>
          <span className='text-[22px] leading-tight font-semibold break-all tabular-nums sm:text-[28px]'>
            {formatMembershipPrice(
              props.tier.price_minor,
              props.tier.currency,
              i18n.language
            )}
          </span>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('One-time payment')} · {t('30 days')}
          </p>
        </div>
      </div>
      <dl className='mt-4 border-t pt-3 text-xs sm:pt-4'>
        <div className='flex items-start justify-between gap-3'>
          <dt>{t('Included quota')}</dt>
          <dd className='font-medium tabular-nums'>
            {formatQuota(props.tier.quota)}
          </dd>
        </div>
      </dl>
      <div className='mt-auto grid grid-cols-[minmax(0,1fr)_auto] items-end gap-2 pt-2.5 sm:block'>
        <div className='min-w-0 text-xs'>
          <dl>
            <div className='flex items-start justify-between gap-2'>
              <dt className='text-muted-foreground min-w-0 truncate'>
                {props.sampleGroup?.display_name || t('Group discounts')}
              </dt>
              <dd
                className={cn(
                  'shrink-0 font-medium tabular-nums',
                  style.accent
                )}
              >
                {samplePercent ?? t('See comparison below')}
              </dd>
            </div>
          </dl>
          <p className='text-muted-foreground mt-1.5 sm:mt-2.5'>
            {t('Existing lower price retained')}
          </p>
        </div>
        <div className='sm:pt-5'>
          <Button
            className={cn(
              'h-auto min-h-9 max-w-36 min-w-0 px-3 py-2 text-xs whitespace-normal sm:w-full sm:max-w-none sm:text-sm',
              current ? '' : style.action
            )}
            variant={current ? 'secondary' : 'outline'}
            disabled={!canPurchaseMembership(props.tier, props.salesEnabled)}
            onClick={() => props.onPurchase(props.tier)}
          >
            {actionLabel}
          </Button>
        </div>
      </div>
      {reason && <p className='text-muted-foreground mt-2 text-xs'>{reason}</p>}
    </article>
  )
}

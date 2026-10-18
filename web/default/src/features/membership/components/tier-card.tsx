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
import {
  CrownIcon,
  Diamond01Icon,
  Medal01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import { canPurchaseMembership, formatMembershipPrice } from '../lib'
import type { MembershipTier } from '../types'

const appearances = {
  gold: {
    icon: Medal01Icon,
    accent: 'text-amber-800 dark:text-amber-200',
    background: 'from-amber-100/80 dark:from-amber-950/40',
    border: 'border-amber-300/60 dark:border-amber-800/60',
  },
  diamond: {
    icon: Diamond01Icon,
    accent: 'text-sky-800 dark:text-sky-200',
    background: 'from-sky-100/80 dark:from-sky-950/40',
    border: 'border-sky-300/60 dark:border-sky-800/60',
  },
  king: {
    icon: CrownIcon,
    accent: 'text-violet-800 dark:text-violet-200',
    background: 'from-violet-100/80 dark:from-violet-950/40',
    border: 'border-violet-300/60 dark:border-violet-800/60',
  },
}

export function MembershipTierCard(props: {
  tier: MembershipTier
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

  return (
    <article
      className={cn(
        'bg-card flex h-full min-w-0 flex-col overflow-hidden rounded-2xl border shadow-sm',
        style.border
      )}
    >
      <div
        className={cn(
          'flex items-center justify-between gap-3 bg-gradient-to-br to-transparent px-6 pt-6 pb-5',
          style.background
        )}
      >
        <div
          className={cn(
            'flex size-12 items-center justify-center rounded-2xl border border-current/10 bg-background/60',
            style.accent
          )}
        >
          <HugeiconsIcon
            icon={style.icon}
            size={26}
            strokeWidth={1.5}
            aria-hidden='true'
          />
        </div>
        {current && (
          <Badge variant='secondary'>{t('Current membership')}</Badge>
        )}
      </div>
      <div className='flex flex-1 flex-col gap-5 px-6 pb-6'>
        <div>
          <h2 className='text-xl font-semibold tracking-tight'>
            {props.tier.name}
          </h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Prepaid calls, member pricing')}
          </p>
        </div>
        <div>
          <span className='text-3xl font-semibold tracking-tight'>
            {formatMembershipPrice(
              props.tier.price_minor,
              props.tier.currency,
              i18n.language
            )}
          </span>
          <span className='text-muted-foreground ml-2 text-sm'>
            {t('One-time payment')}
          </span>
        </div>
        <dl className='space-y-3 border-y py-4 text-sm'>
          <div className='flex items-center justify-between gap-3'>
            <dt className='text-muted-foreground'>{t('Included quota')}</dt>
            <dd className='font-medium'>{formatQuota(props.tier.quota)}</dd>
          </div>
          <div className='flex items-center justify-between gap-3'>
            <dt className='text-muted-foreground'>{t('Validity Period')}</dt>
            <dd className='font-medium'>{t('30 days')}</dd>
          </div>
          <div className='flex items-center justify-between gap-3'>
            <dt className='text-muted-foreground'>{t('Group discounts')}</dt>
            <dd className='font-medium'>{t('See comparison below')}</dd>
          </div>
        </dl>
        <p className='text-muted-foreground text-xs leading-relaxed'>
          {t(
            'Membership quota is shared across your accessible groups. No automatic renewal.'
          )}
        </p>
        <Button
          className='mt-auto w-full'
          variant={current ? 'secondary' : 'default'}
          disabled={!canPurchaseMembership(props.tier, props.salesEnabled)}
          onClick={() => props.onPurchase(props.tier)}
        >
          {actionLabel}
        </Button>
        {reason && <p className='text-muted-foreground text-xs'>{reason}</p>}
      </div>
    </article>
  )
}

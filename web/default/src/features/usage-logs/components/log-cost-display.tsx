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
  Wallet01Icon,
  Wrench01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { toIntlLocale } from '@/i18n/languages'
import { formatLogQuota } from '@/lib/format'

import type { UsageLog } from '../data/schema'
import { hasToolSurcharge } from '../lib/format'
import type { LogOtherData } from '../types'

interface LogCostDisplayProps {
  quota: number
  other: LogOtherData | null
  logType?: number
  showBillingSource?: boolean
  prominent?: boolean
  settlement?: UsageLog['tenant_settlement']
}

export function TenantSettlementBadge(props: {
  settlement: NonNullable<UsageLog['tenant_settlement']>
}) {
  const { t } = useTranslation()
  const states = {
    reserved: { label: t('Reservation active'), variant: 'neutral' },
    settlement_pending: { label: t('Awaiting settlement'), variant: 'warning' },
    settled: { label: t('Settled'), variant: 'success' },
    refund_pending: { label: t('Refund pending'), variant: 'warning' },
    refunded: { label: t('Refunded'), variant: 'neutral' },
  } as const
  const state = states[props.settlement.status]
  return (
    <StatusBadge
      label={state.label}
      variant={state.variant}
      size='sm'
      copyable={false}
    />
  )
}

function ToolSurchargeMarker() {
  const { t } = useTranslation()
  const label = t('Includes tool-call surcharge')

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Badge
            variant='outline'
            className='h-5 min-w-5 cursor-help gap-0 rounded-full px-1'
            role='img'
            aria-label={label}
            tabIndex={0}
            data-tool-surcharge-indicator='true'
          >
            <HugeiconsIcon
              icon={Wrench01Icon}
              strokeWidth={2}
              aria-hidden='true'
            />
            <span
              className='text-[9px] leading-none font-bold'
              aria-hidden='true'
            >
              +
            </span>
          </Badge>
        }
      />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

export function LogCostDisplay(props: LogCostDisplayProps) {
  const { t, i18n } = useTranslation()
  const isSubscription = props.other?.billing_source === 'subscription'
  const isMembership =
    props.logType === 2 && props.other?.billing_source === 'membership'
  const isMembershipWallet =
    props.logType === 2 &&
    props.other?.billing_source === 'wallet' &&
    (props.other.membership_wallet_fallback === true ||
      (props.other.membership_account_id ?? 0) > 0)
  const showToolSurcharge = hasToolSurcharge(props.other)
  const quota = isSubscription
    ? (props.other?.subscription_consumed ?? props.quota)
    : props.quota
  let source: string | undefined

  // A log billed to a subscription always names its funding source: that
  // subscription may have expired since, and the viewer may hold no active
  // plan today. Only the wallet marker is contextual and follows
  // showBillingSource.
  if (isSubscription) {
    source = t('Subscription')
  } else if (isMembershipWallet) {
    source = t('Wallet')
  } else if (
    props.showBillingSource &&
    props.other?.billing_source === 'wallet'
  ) {
    source = t('Wallet')
  }

  let memberTone = 'bg-muted text-foreground'
  let memberIcon = CrownIcon
  if (props.other?.membership_tier_style === 'gold') {
    memberTone =
      'bg-amber-50 text-amber-800 dark:bg-amber-950/40 dark:text-amber-200'
    memberIcon = Medal01Icon
  } else if (props.other?.membership_tier_style === 'diamond') {
    memberTone =
      'bg-cyan-50 text-cyan-800 dark:bg-cyan-950/40 dark:text-cyan-200'
    memberIcon = Diamond01Icon
  } else if (props.other?.membership_tier_style === 'king') {
    memberTone =
      'bg-fuchsia-50 text-fuchsia-800 dark:bg-fuchsia-950/40 dark:text-fuchsia-200'
  }

  const savedQuota = props.other?.membership_estimated_saved_quota
  const savings = props.other?.membership_savings
  let benefit: string | undefined
  if (
    isMembership &&
    savedQuota != null &&
    Number.isFinite(savedQuota) &&
    savedQuota > 0
  ) {
    benefit = t('About {{amount}} saved with membership', {
      amount: formatLogQuota(savedQuota),
    })
  } else if (
    isMembership &&
    savings != null &&
    Number.isFinite(savings) &&
    savings > 0
  ) {
    benefit = t('Membership ratio {{percent}} below wallet', {
      percent: new Intl.NumberFormat(toIntlLocale(i18n.language), {
        style: 'percent',
        maximumFractionDigits: 2,
      }).format(savings),
    })
  } else if (isMembership && props.other?.membership_discount != null) {
    benefit =
      props.other.membership_discount < 1
        ? t('Existing lower price retained')
        : t('No extra discount for this group')
  } else if (isMembership) {
    benefit = t('Discount details unavailable for this historical log')
  } else if (isMembershipWallet) {
    benefit = t('Membership discount not used for this request')
  }

  const tierName = props.other?.membership_tier_name?.trim()
  const hasSavings =
    isMembership && ((savedQuota ?? 0) > 0 || (savings ?? 0) > 0)

  return (
    <TooltipProvider>
      <div className='inline-flex w-fit flex-wrap items-center gap-1.5'>
        <div className='flex flex-col items-start gap-1'>
          <StatusBadge
            type='badge'
            variant='neutral'
            size='lg'
            copyable={false}
            className={
              props.prominent
                ? 'text-foreground h-auto border-0 bg-transparent p-0 text-[38px] leading-tight font-semibold tracking-tight tabular-nums'
                : 'border-border/80 bg-muted/60 text-foreground rounded-md border font-semibold tabular-nums'
            }
          >
            {source ? (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <span
                      className='inline-flex shrink-0 cursor-help'
                      role='img'
                      aria-label={source}
                      tabIndex={0}
                    >
                      <HugeiconsIcon
                        icon={isSubscription ? CrownIcon : Wallet01Icon}
                        className='size-3.5'
                        strokeWidth={2}
                        aria-hidden='true'
                      />
                    </span>
                  }
                />
                <TooltipContent>{source}</TooltipContent>
              </Tooltip>
            ) : null}
            <span className='whitespace-nowrap'>{formatLogQuota(quota)}</span>
          </StatusBadge>
          {isMembership ? (
            <span
              className={`inline-flex max-w-full items-center gap-1 rounded px-1.5 py-0.5 text-[11px] leading-tight ${memberTone}`}
            >
              <HugeiconsIcon
                icon={memberIcon}
                className='size-3 shrink-0'
                strokeWidth={2}
                aria-hidden='true'
              />
              <span className='break-words'>
                {tierName ? `${tierName} · ` : ''}
                {t('Membership quota')}
              </span>
            </span>
          ) : null}
          {isMembershipWallet ? (
            <span className='bg-muted text-muted-foreground inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] leading-tight'>
              <HugeiconsIcon
                icon={Wallet01Icon}
                className='size-3'
                strokeWidth={2}
                aria-hidden='true'
              />
              {t('Wallet payment')}
            </span>
          ) : null}
          {benefit ? (
            <span
              className={
                hasSavings
                  ? 'text-xs text-emerald-700 dark:text-emerald-300'
                  : 'text-muted-foreground text-xs'
              }
            >
              {benefit}
            </span>
          ) : null}
        </div>
        {showToolSurcharge ? <ToolSurchargeMarker /> : null}
        {props.settlement ? (
          <TenantSettlementBadge settlement={props.settlement} />
        ) : null}
      </div>
    </TooltipProvider>
  )
}

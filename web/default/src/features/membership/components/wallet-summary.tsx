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
import { Link } from '@tanstack/react-router'
import { ArrowUpRight, Crown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { formatQuota } from '@/lib/format'

import { useMembership } from '../hooks'

export function MembershipWalletSummary() {
  const { t } = useTranslation()
  const { self, catalog, now } = useMembership()
  if (
    self.isPending ||
    self.isError ||
    catalog.isError ||
    self.data?.runtime_enabled === false
  ) {
    return null
  }
  const account = self.data?.account
  const active = account?.status === 'active' && account.expires_at > now
  return (
    <section className='wallet-membership wallet-enter bg-card flex items-center gap-3 rounded-[12px] border px-4 py-3'>
      <span className='bg-muted flex size-9 shrink-0 items-center justify-center rounded-lg text-[var(--wallet-gold)]'>
        <Crown className='size-5' aria-hidden='true' />
      </span>
      <div className='wallet-membership-copy flex min-w-0 flex-1 flex-wrap items-center gap-x-4 gap-y-1'>
        <h2 className='text-sm font-semibold [overflow-wrap:anywhere]'>
          {active && account
            ? (self.data?.tier?.name ?? account.tier_id)
            : t('Membership center')}
        </h2>
        <p className='text-muted-foreground text-xs [overflow-wrap:anywhere]'>
          {active && account
            ? `${t('Available now')} ${formatQuota(Math.max(0, account.amount_total - account.amount_used - account.amount_reserved))}`
            : t('View membership plans and your current benefits.')}
        </p>
      </div>
      <Button
        variant='outline'
        className='h-10 shrink-0 gap-1.5 rounded-[9px] px-2.5 text-xs'
        render={<Link to='/membership' />}
      >
        {t('View membership')}
        <ArrowUpRight className='size-3.5' aria-hidden='true' />
      </Button>
    </section>
  )
}

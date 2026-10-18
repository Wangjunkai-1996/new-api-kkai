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
    <section className='bg-card flex flex-col justify-between gap-4 rounded-2xl border p-5 sm:flex-row sm:items-center'>
      <div>
        <h2 className='font-semibold'>{t('Membership center')}</h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          {active && account
            ? `${self.data?.tier?.name ?? account.tier_id} · ${t('Available now')} ${formatQuota(Math.max(0, account.amount_total - account.amount_used - account.amount_reserved))}`
            : t('View membership plans and your current benefits.')}
        </p>
      </div>
      <Button variant='outline' render={<Link to='/membership' />}>
        {t('View membership')}
      </Button>
    </section>
  )
}

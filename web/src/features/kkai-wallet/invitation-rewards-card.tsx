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
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, Gift } from 'lucide-react'
import type { ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Input } from '@/components/ui/input'
import { getMyInvitationCode } from '@/features/invitations/api'
import { formatRebateAmount } from '@/features/invitations/format'
import { useInvitationFeatureStatus } from '@/features/invitations/hooks/use-invitation-feature-status'
import { AffiliateRewardsCard } from '@/features/wallet/components/affiliate-rewards-card'
import { generateAffiliateLink } from '@/features/wallet/lib'

import { isInvitationWalletLoading } from './loading-state'

type WalletRewardsProps = ComponentProps<typeof AffiliateRewardsCard>

export const KkaiWalletRewardsCard = (props: WalletRewardsProps) => {
  const { t } = useTranslation()
  const feature = useInvitationFeatureStatus()
  const statsQuery = useQuery({
    queryKey: ['kkai', 'invitations', 'my-code'],
    queryFn: async () => {
      const response = await getMyInvitationCode({
        skipBusinessError: true,
        skipErrorHandler: true,
      })
      return response.success ? response.data : null
    },
    enabled: feature.userVisible,
    retry: false,
    staleTime: 60_000,
  })
  const stats = feature.userVisible ? statsQuery.data : null

  if (
    isInvitationWalletLoading(
      feature.query.isPending,
      feature.userVisible,
      statsQuery.isPending
    )
  ) {
    return <AffiliateRewardsCard {...props} loading />
  }
  if (!stats?.invitationCode) return <AffiliateRewardsCard {...props} />

  const invitationLink = generateAffiliateLink(stats.invitationCode)
  const statItems = [
    [t('Pending Rebate'), formatRebateAmount(stats.pendingRebate)],
    [t('Total Rebate'), formatRebateAmount(stats.totalRebate)],
    [t('Invites'), String(stats.invitedCount)],
  ]

  return (
    <Card data-card-hover='false' className='bg-muted/20 py-0'>
      <CardContent className='grid gap-3 p-3 sm:p-4 lg:grid-cols-[minmax(200px,1fr)_minmax(180px,0.65fr)_minmax(360px,1.2fr)] lg:items-center'>
        <div className='flex min-w-0 items-center gap-2.5'>
          <IconBadge tone='chart-3'>
            <Gift aria-hidden='true' />
          </IconBadge>
          <div className='min-w-0'>
            <h3 className='truncate text-sm font-semibold'>
              {t('Invitation Rebate')}
            </h3>
            <p className='text-muted-foreground line-clamp-1 text-xs'>
              {t('Share your invitation code to earn rebates')}
            </p>
          </div>
        </div>

        <div className='grid grid-cols-3 gap-1.5 text-center'>
          {statItems.map(([label, value]) => (
            <div key={label}>
              <div className='text-muted-foreground truncate text-[10px] font-medium tracking-wider uppercase'>
                {label}
              </div>
              <div className='mt-0.5 truncate text-sm font-semibold tabular-nums'>
                {value}
              </div>
            </div>
          ))}
        </div>

        <div className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] lg:flex lg:items-center'>
          <div className='flex min-w-0 items-center gap-2'>
            <Input
              value={invitationLink}
              readOnly
              className='border-muted bg-background/70 h-9 min-w-0 flex-1 font-mono text-xs'
            />
            <CopyButton
              value={invitationLink}
              variant='outline'
              className='bg-background size-9 shrink-0'
              iconClassName='size-4'
              tooltip={t('Copy referral link')}
              aria-label={t('Copy referral link')}
            />
          </div>
          <Button
            render={<Link to='/invitations' />}
            variant='outline'
            className='h-9 w-full gap-2 sm:w-auto'
          >
            {t('Rebate Center')}
            <ArrowRight aria-hidden='true' />
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

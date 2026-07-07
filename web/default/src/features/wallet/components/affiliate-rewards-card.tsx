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
import { ArrowRight, Gift, Share2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  getInvitationFeatureStatus,
  getMyCode,
} from '@/features/invitations/api'
import { formatRebateAmount } from '@/features/invitations/lib/format'
import { formatQuota } from '@/lib/format'

import { generateAffiliateLink } from '../lib'
import type { UserWalletData } from '../types'

interface AffiliateRewardsCardProps {
  user: UserWalletData | null
  affiliateLink: string
  onTransfer: () => void
  onInvitationRebateTransfer?: () => void
  complianceConfirmed?: boolean
  loading?: boolean
}

export function AffiliateRewardsCard({
  user,
  affiliateLink,
  onTransfer,
  onInvitationRebateTransfer,
  complianceConfirmed = true,
  loading,
}: AffiliateRewardsCardProps) {
  const { t } = useTranslation()

  const featureQuery = useQuery({
    queryKey: ['walletInvitationFeatureStatus'],
    queryFn: async () => {
      const response = await getInvitationFeatureStatus()
      return response.success ? response.data : null
    },
    retry: false,
    staleTime: 60_000,
  })

  const invitationFeature = featureQuery.data
  const canUseInvitationBackend = Boolean(
    invitationFeature?.available &&
    invitationFeature.userInvitationRebateEnabled &&
    (invitationFeature.orderRebateEnabled ||
      invitationFeature.invitationSignupRewardEnabled)
  )

  const invitationStatsQuery = useQuery({
    queryKey: ['walletInvitationStats'],
    queryFn: async () => {
      const response = await getMyCode({
        skipBusinessError: true,
        skipErrorHandler: true,
      })
      return response.success ? response.data : null
    },
    enabled: canUseInvitationBackend,
    retry: false,
    staleTime: 60_000,
  })

  const invitationStats = invitationStatsQuery.data
  const usingInvitationBackend = Boolean(
    canUseInvitationBackend && invitationStats
  )
  const statsLoading =
    featureQuery.isLoading ||
    (canUseInvitationBackend && invitationStatsQuery.isLoading)

  if (loading || statsLoading) {
    return (
      <Card data-card-hover='false' className='bg-muted/20 py-0'>
        <CardContent className='grid gap-4 p-3 sm:p-4 lg:grid-cols-[minmax(220px,1fr)_minmax(220px,0.72fr)_minmax(320px,1.15fr)] lg:items-center'>
          <div>
            <Skeleton className='h-5 w-32' />
            <Skeleton className='mt-2 h-4 w-48' />
          </div>
          <Skeleton className='h-14 rounded-lg' />
          <Skeleton className='h-10 rounded-lg' />
        </CardContent>
      </Card>
    )
  }

  const pendingAmount = usingInvitationBackend
    ? (invitationStats?.pendingRebate ?? 0)
    : (user?.aff_quota ?? 0)
  const totalAmount = usingInvitationBackend
    ? (invitationStats?.totalRebate ?? 0)
    : (user?.aff_history_quota ?? 0)
  const inviteCount = usingInvitationBackend
    ? (invitationStats?.invitedCount ?? 0)
    : (user?.aff_count ?? 0)
  const displayAffiliateLink =
    usingInvitationBackend && invitationStats?.invitationCode
      ? generateAffiliateLink(invitationStats.invitationCode)
      : affiliateLink
  const pendingDisplay = usingInvitationBackend
    ? formatRebateAmount(pendingAmount)
    : formatQuota(pendingAmount)
  const totalDisplay = usingInvitationBackend
    ? formatRebateAmount(totalAmount)
    : formatQuota(totalAmount)
  const hasRewards = pendingAmount > 0
  const canTransferInvitationRebate =
    usingInvitationBackend && invitationFeature?.rebateToBalanceEnabled === true
  const handleAction = usingInvitationBackend
    ? (onInvitationRebateTransfer ?? onTransfer)
    : onTransfer
  const showTransferButton =
    hasRewards && (!usingInvitationBackend || canTransferInvitationRebate)
  const showActionButton = usingInvitationBackend || showTransferButton
  const actionButtonLabel =
    usingInvitationBackend && (!hasRewards || !canTransferInvitationRebate)
      ? t('Rebate Center')
      : usingInvitationBackend
        ? t('Apply Rebate to Balance')
        : t('Transfer to Balance')
  const actionButtonVariant =
    usingInvitationBackend && (!hasRewards || !canTransferInvitationRebate)
      ? 'outline'
      : 'default'
  const actionButtonDisabled =
    !complianceConfirmed &&
    hasRewards &&
    (!usingInvitationBackend || canTransferInvitationRebate)
  const statItems = usingInvitationBackend
    ? [
        [t('Pending Rebate'), pendingDisplay],
        [t('Total Rebate'), totalDisplay],
        [t('Invites'), String(inviteCount)],
      ]
    : [
        [t('Pending'), pendingDisplay],
        [t('Total Earned'), totalDisplay],
        [t('Invites'), String(inviteCount)],
      ]

  return (
    <Card data-card-hover='false' className='bg-muted/20 py-0'>
      <CardContent className='grid gap-3 p-3 sm:gap-4 sm:p-4 lg:grid-cols-[minmax(200px,1fr)_minmax(180px,0.65fr)_minmax(360px,1.2fr)] lg:items-center'>
        <div className='flex min-w-0 items-center gap-2.5'>
          <div className='bg-background flex size-8 shrink-0 items-center justify-center rounded-lg border'>
            {usingInvitationBackend ? (
              <Gift className='text-muted-foreground size-4' />
            ) : (
              <Share2 className='text-muted-foreground size-4' />
            )}
          </div>
          <div className='min-w-0'>
            <h3 className='truncate text-sm font-semibold'>
              {usingInvitationBackend
                ? t('Invitation Rebate')
                : t('Referral Program')}
            </h3>
            <p className='text-muted-foreground line-clamp-1 text-xs'>
              {usingInvitationBackend
                ? t('Share your invitation code to earn rebates')
                : t(
                    'Earn rewards when your referrals add funds. Transfer accumulated rewards to your balance anytime.'
                  )}
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
              value={displayAffiliateLink}
              readOnly
              className='border-muted bg-background/70 h-9 min-w-0 flex-1 font-mono text-xs'
            />
            <CopyButton
              value={displayAffiliateLink}
              variant='outline'
              className='bg-background size-9 shrink-0'
              iconClassName='size-4'
              tooltip={t('Copy referral link')}
              aria-label={t('Copy referral link')}
            />
          </div>
          {showActionButton && (
            <Button
              onClick={handleAction}
              disabled={actionButtonDisabled}
              variant={actionButtonVariant}
              className='h-9 w-full shrink-0 gap-2 px-3 sm:w-auto'
              size='sm'
            >
              {actionButtonLabel}
              {usingInvitationBackend && <ArrowRight className='size-3.5' />}
            </Button>
          )}
        </div>
        {!complianceConfirmed ? (
          <p className='text-muted-foreground text-xs lg:col-span-3'>
            {t(
              'Referral reward transfer is disabled until the administrator confirms compliance terms.'
            )}
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}

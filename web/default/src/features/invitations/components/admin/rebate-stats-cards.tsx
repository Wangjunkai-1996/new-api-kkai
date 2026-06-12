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
  DollarSign,
  CheckCircle,
  Clock,
  FileCheck,
  ThumbsUp,
  Users,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatRebateAmount } from '../../lib/format'
import type { RebateStats } from '../../types'

interface RebateStatsCardsProps {
  stats: RebateStats | null
  loading: boolean
}

export function RebateStatsCards({ stats, loading }: RebateStatsCardsProps) {
  const { t } = useTranslation()

  const statsCards = [
    {
      title: t('Total Rebate'),
      value: stats ? formatRebateAmount(stats.total_rebate) : '-',
      icon: DollarSign,
      color: 'text-blue-600',
    },
    {
      title: t('Completed Rebate'),
      value: stats ? formatRebateAmount(stats.completed_rebate) : '-',
      icon: CheckCircle,
      color: 'text-green-600',
    },
    {
      title: t('Pending Rebate'),
      value: stats ? formatRebateAmount(stats.pending_rebate) : '-',
      icon: Clock,
      color: 'text-yellow-600',
    },
    {
      title: t('Requested Rebate'),
      value: stats ? formatRebateAmount(stats.requested_rebate) : '-',
      icon: FileCheck,
      color: 'text-orange-600',
    },
    {
      title: t('Approved Rebate'),
      value: stats ? formatRebateAmount(stats.approved_rebate) : '-',
      icon: ThumbsUp,
      color: 'text-purple-600',
    },
    {
      title: t('Total Invitations'),
      value: stats ? stats.total_invitations.toString() : '-',
      icon: Users,
      color: 'text-indigo-600',
    },
  ]

  if (loading) {
    return (
      <div className='grid gap-4 md:grid-cols-2 lg:grid-cols-3'>
        {Array.from({ length: 6 }).map((_, i) => (
          <Card key={i}>
            <CardHeader className='flex flex-row items-center justify-between space-y-0 pb-2'>
              <CardTitle className='text-sm font-medium'>
                <div className='bg-muted h-4 w-24 animate-pulse rounded' />
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className='bg-muted h-8 w-32 animate-pulse rounded' />
            </CardContent>
          </Card>
        ))}
      </div>
    )
  }

  return (
    <div className='grid gap-4 md:grid-cols-2 lg:grid-cols-3'>
      {statsCards.map((card, index) => {
        const Icon = card.icon
        return (
          <Card key={index}>
            <CardHeader className='flex flex-row items-center justify-between space-y-0 pb-2'>
              <CardTitle className='text-sm font-medium'>
                {card.title}
              </CardTitle>
              <Icon className={`size-4 ${card.color}`} />
            </CardHeader>
            <CardContent>
              <div className='text-2xl font-bold'>{card.value}</div>
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}

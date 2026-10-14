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

import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota } from '@/lib/format'

import type { UserWalletData } from '../types'

interface WalletStatsCardProps {
  user: UserWalletData | null
  loading?: boolean
}

export function WalletStatsCard(props: WalletStatsCardProps) {
  const { t } = useTranslation()

  if (props.loading) {
    return (
      <div className='grid grid-cols-2 gap-x-4 gap-y-4 border-y py-5 sm:gap-x-5 sm:py-6 lg:grid-cols-[minmax(0,1.7fr)_minmax(120px,0.63fr)_minmax(115px,0.55fr)] lg:items-center'>
        <div className='col-span-2 min-w-0 lg:col-span-1'>
          <Skeleton className='h-4 w-24' />
          <Skeleton className='mt-2 h-9 w-64 max-w-full' />
        </div>
        {['usage', 'requests'].map((key) => (
          <div key={key} className='min-w-0 lg:border-l lg:pl-5'>
            <Skeleton className='h-3 w-20 max-w-full' />
            <Skeleton className='mt-2 h-5 w-28 max-w-full' />
          </div>
        ))}
      </div>
    )
  }

  return (
    <div className='grid grid-cols-2 gap-x-4 gap-y-4 border-y py-5 sm:gap-x-5 sm:py-6 lg:grid-cols-[minmax(0,1.7fr)_minmax(120px,0.63fr)_minmax(115px,0.55fr)] lg:items-center'>
      <div className='col-span-2 min-w-0 lg:col-span-1'>
        <div className='text-muted-foreground flex items-center gap-2 text-xs'>
          <span
            className='bg-success size-1.5 shrink-0 rounded-full'
            aria-hidden='true'
          />
          {t('Current Balance')}
        </div>
        <div className='mt-2 text-3xl leading-tight font-semibold break-all tabular-nums sm:text-[34px]'>
          {formatQuota(props.user?.quota ?? 0)}
        </div>
      </div>
      <div className='flex min-w-0 flex-col justify-center gap-1.5 lg:min-h-12 lg:border-l lg:pl-5'>
        <span className='text-muted-foreground text-xs'>
          {t('Total Usage')}
        </span>
        <strong className='text-sm font-medium break-all tabular-nums'>
          {formatQuota(props.user?.used_quota ?? 0)}
        </strong>
      </div>
      <div className='flex min-w-0 flex-col justify-center gap-1.5 lg:min-h-12 lg:border-l lg:pl-5'>
        <span className='text-muted-foreground text-xs'>
          {t('API Requests')}
        </span>
        <strong className='text-sm font-medium break-all tabular-nums'>
          {(props.user?.request_count ?? 0).toLocaleString()}
        </strong>
      </div>
    </div>
  )
}

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
import { StatusBadge } from '@/components/status-badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatNumber, formatPercent } from '@/lib/format'
import { cn } from '@/lib/utils'
import {
  getConfidenceStatus,
  getExperienceLabel,
  getMessageKey,
  getRecommendationLevel,
  shouldShowExperience,
} from './status-display'
import {
  CONFIDENCE_META,
  EXPERIENCE_META,
  RECOMMENDATION_META,
} from './status-meta'
import type { GroupStatusEntry } from './types'

export function GroupStatusCards({ groups }: { groups: GroupStatusEntry[] }) {
  return (
    <div className='space-y-2 md:hidden'>
      {groups.map((group) => (
        <GroupStatusCard key={group.group} group={group} />
      ))}
    </div>
  )
}

function GroupStatusCard({ group }: { group: GroupStatusEntry }) {
  const { t } = useTranslation()
  const confidenceMeta = CONFIDENCE_META[getConfidenceStatus(group)]
  const recommendationMeta = RECOMMENDATION_META[getRecommendationLevel(group)]
  const experienceMeta = EXPERIENCE_META[getExperienceLabel(group)]
  const ConfidenceIcon = confidenceMeta.icon
  const ExperienceIcon = experienceMeta.icon

  return (
    <Card className='relative overflow-hidden rounded-lg'>
      <span
        className={cn('absolute inset-y-0 left-0 w-1', confidenceMeta.barClass)}
        aria-hidden='true'
      />
      <CardHeader className='pb-3'>
        <div className='flex min-w-0 items-start justify-between gap-3 pl-1'>
          <div className='min-w-0'>
            <CardTitle className='truncate text-base'>{group.group}</CardTitle>
            <div className='text-muted-foreground truncate text-xs'>
              {group.desc || '-'}
            </div>
          </div>
          <StatusBadge
            copyable={false}
            label={t(recommendationMeta.labelKey)}
            variant={recommendationMeta.variant}
          />
        </div>
      </CardHeader>
      <CardContent className='space-y-3 pl-5'>
        <div className='flex items-end justify-between gap-3'>
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Success Rate')}
            </div>
            <div className={cn('text-3xl font-semibold', confidenceMeta.toneClass)}>
              {formatSuccessRate(group)}
            </div>
          </div>
          <StatusBadge
            copyable={false}
            icon={ConfidenceIcon}
            label={t(confidenceMeta.labelKey)}
            variant={confidenceMeta.badgeVariant}
            size='lg'
          />
        </div>
        <div className='grid grid-cols-2 gap-x-4 gap-y-2 text-sm'>
          <MobileMetric label={t('Samples')} value={formatNumber(group.request_count)} />
          <MobileMetric
            label={t('Routable Models')}
            value={formatNumber(group.available_model_count)}
          />
        </div>
        {shouldShowExperience(group) && (
          <StatusBadge
            copyable={false}
            icon={ExperienceIcon}
            label={t(experienceMeta.labelKey)}
            variant={experienceMeta.variant}
          />
        )}
        <p className='text-muted-foreground text-sm'>{t(getMessageKey(group))}</p>
      </CardContent>
    </Card>
  )
}

function MobileMetric(props: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='truncate font-medium tabular-nums'>{props.value}</div>
    </div>
  )
}

function formatSuccessRate(group: GroupStatusEntry): string {
  if (group.request_count <= 0) return '-'
  return formatPercent(group.success_rate)
}

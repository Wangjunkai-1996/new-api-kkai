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
import { useTranslation } from 'react-i18next'

import { getPerfMetrics } from '@/features/performance-metrics/api'
import type { PricingModel } from '@/features/pricing/types'

function linePoints(values: number[]) {
  if (values.length === 0) return ''
  const width = 1100
  const height = 280
  return values
    .map((value, index) => {
      const x = (index / Math.max(1, values.length - 1)) * width
      const y = height - (Math.max(0, Math.min(100, value)) / 100) * height
      return `${x},${y}`
    })
    .join(' ')
}

export function ModelAvailabilityChart(props: { model: PricingModel }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['linkai-model-availability', props.model.model_name],
    queryFn: () => getPerfMetrics(props.model.model_name, 24),
    staleTime: 60_000,
    retry: false,
  })
  const groups = query.data?.data.groups || []

  return (
    <section className='overflow-hidden rounded-2xl border border-white/10 bg-[#0c0c0c]'>
      <header className='border-b border-white/10 px-7 py-6'>
        <h2 className='text-lg text-[#eeeeee]'>{t('Availability Monitor')}</h2>
        <p className='mt-1 text-sm text-white/55'>
          {t('Availability trend for each group over the last 24 hours')}
        </p>
      </header>
      <div className='px-7 py-7'>
        {groups.length > 0 ? (
          <>
            <div className='mb-6 flex flex-wrap gap-5 text-sm'>
              {groups.map((group, index) => (
                <span key={group.group} className='flex items-center gap-2'>
                  <span
                    className='size-3 rounded-full'
                    style={{
                      background: index % 2 === 0 ? '#3b82f6' : '#10b981',
                    }}
                  />
                  <span className='text-white/60'>{group.group}</span>
                  <strong className='text-emerald-400'>
                    {group.success_rate.toFixed(1)}%
                  </strong>
                </span>
              ))}
            </div>
            <div className='relative min-h-[330px] overflow-hidden rounded-xl border border-white/[0.04] bg-[#0e0e0e] p-5'>
              <div className='absolute inset-5 grid grid-rows-4'>
                {[100, 75, 50, 25].map((value) => (
                  <div
                    key={value}
                    className='relative border-t border-dashed border-white/10'
                  >
                    <span className='absolute -top-3 left-0 bg-[#0e0e0e] pr-2 text-xs text-white/45'>
                      {value}%
                    </span>
                  </div>
                ))}
              </div>
              <svg
                viewBox='0 0 1100 280'
                className='relative mt-4 h-[280px] w-full overflow-visible'
                role='img'
                aria-label={t('Availability chart')}
              >
                {groups.map((group, index) => (
                  <polyline
                    key={group.group}
                    points={linePoints(
                      group.series.map((point) => point.success_rate)
                    )}
                    fill='none'
                    stroke={index % 2 === 0 ? '#3b82f6' : '#10b981'}
                    strokeWidth='2.5'
                    vectorEffect='non-scaling-stroke'
                  />
                ))}
              </svg>
              <div className='relative flex justify-between text-xs text-white/45'>
                <span>{t('24h ago')}</span>
                <span>{t('16h ago')}</span>
                <span>{t('8h ago')}</span>
                <span>{t('Now')}</span>
              </div>
            </div>
          </>
        ) : (
          <div className='flex min-h-[300px] items-center justify-center text-center text-sm text-white/45'>
            {query.isLoading
              ? t('Loading availability data...')
              : t('Performance data is not yet available for this model.')}
          </div>
        )}
      </div>
    </section>
  )
}

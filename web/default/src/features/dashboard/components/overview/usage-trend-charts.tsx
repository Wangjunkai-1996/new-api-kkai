import { VChart } from '@visactor/react-vchart'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { useTheme } from '@/context/theme-provider'
import { processChartData } from '@/features/dashboard/lib/charts'
import type { QuotaDataItem } from '@/features/dashboard/types'
import { toIntlLocale } from '@/i18n/languages'
import type { TimeGranularity } from '@/lib/time'
import { VCHART_OPTION } from '@/lib/vchart'

type TrendRow = {
  TimeKey: number
  Time: string
  Model: string
  Count: number
}
type DistributionRow = { type: string; value: number }

export default function UsageTrendCharts(props: {
  rows: QuotaDataItem[]
  granularity: TimeGranularity
  range: { start_timestamp: number; end_timestamp: number }
}) {
  const { t, i18n } = useTranslation()
  const { resolvedTheme } = useTheme()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const formatCount = useMemo(
    () =>
      new Intl.NumberFormat(locale, {
        maximumFractionDigits: 0,
      }).format,
    [locale]
  )
  const charts = useMemo(() => {
    const data = processChartData(
      props.rows,
      props.granularity,
      t,
      undefined,
      props.range,
      locale,
      false
    )
    const decorate = (
      spec: typeof data.spec_model_line
    ): typeof data.spec_model_line => ({
      ...spec,
      theme: resolvedTheme,
      background: 'transparent',
      title: { visible: false },
      tooltip: { ...spec.tooltip, renderMode: 'canvas' },
    })
    return {
      trend: decorate(data.spec_model_line),
      distribution: decorate(data.spec_pie),
    }
  }, [props.rows, props.granularity, props.range, t, locale, resolvedTheme])
  const trendRows = (charts.trend.data[0]?.values ?? []) as TrendRow[]
  const distributionRows = (charts.distribution.data[0]?.values ??
    []) as DistributionRow[]

  return (
    <div className='grid min-w-0 gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]'>
      <figure className='min-w-0'>
        <figcaption className='mb-2 text-sm font-semibold'>
          {t('Call Trend')}
        </figcaption>
        <div
          role='img'
          aria-label={t('Call Trend')}
          className='h-72 min-w-0 sm:h-80'
        >
          <VChart spec={charts.trend} option={VCHART_OPTION} />
        </div>
        <details className='mt-2 rounded-md border text-sm'>
          <summary className='focus-visible:ring-ring cursor-pointer px-3 py-2 font-medium focus-visible:ring-2 focus-visible:outline-none'>
            {t('Table view')}
          </summary>
          <div className='max-h-72 overflow-auto' tabIndex={0}>
            <table className='w-full min-w-[24rem] text-left'>
              <caption className='sr-only'>{t('Call Trend')}</caption>
              <thead>
                <tr>
                  <th scope='col' className='px-3 py-2 font-medium'>
                    {t('Time')}
                  </th>
                  <th scope='col' className='px-3 py-2 font-medium'>
                    {t('Model')}
                  </th>
                  <th scope='col' className='px-3 py-2 text-right font-medium'>
                    {t('Count')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {trendRows.map((row) => (
                  <tr key={`${row.TimeKey}-${row.Model}`}>
                    <td className='px-3 py-1.5'>{row.Time}</td>
                    <td className='px-3 py-1.5'>{row.Model}</td>
                    <td className='px-3 py-1.5 text-right tabular-nums'>
                      {formatCount(row.Count)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      </figure>
      <figure className='min-w-0'>
        <figcaption className='mb-2 text-sm font-semibold'>
          {t('Call Count Distribution')}
        </figcaption>
        <div
          role='img'
          aria-label={t('Call Count Distribution')}
          className='h-72 min-w-0 sm:h-80'
        >
          <VChart spec={charts.distribution} option={VCHART_OPTION} />
        </div>
        <details className='mt-2 rounded-md border text-sm'>
          <summary className='focus-visible:ring-ring cursor-pointer px-3 py-2 font-medium focus-visible:ring-2 focus-visible:outline-none'>
            {t('Table view')}
          </summary>
          <div className='max-h-72 overflow-auto' tabIndex={0}>
            <table className='w-full min-w-[16rem] text-left'>
              <caption className='sr-only'>
                {t('Call Count Distribution')}
              </caption>
              <thead>
                <tr>
                  <th scope='col' className='px-3 py-2 font-medium'>
                    {t('Model')}
                  </th>
                  <th scope='col' className='px-3 py-2 text-right font-medium'>
                    {t('Count')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {distributionRows.map((row) => (
                  <tr key={row.type}>
                    <td className='px-3 py-1.5'>{row.type}</td>
                    <td className='px-3 py-1.5 text-right tabular-nums'>
                      {formatCount(row.value)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      </figure>
    </div>
  )
}

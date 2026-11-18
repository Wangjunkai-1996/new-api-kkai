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
import { dataScheme as vchartDefaultDataScheme } from '@visactor/vchart/esm/theme/color-scheme/builtin/default'

import {
  MAX_CHART_TREND_POINTS,
  MAX_DENSE_CHART_BUCKETS,
} from '@/features/dashboard/constants'
import type {
  ProcessedChartData,
  ProcessedUserChartData,
  QuotaDataItem,
} from '@/features/dashboard/types'
import { getCurrencyDisplay } from '@/lib/currency'
import {
  formatChartTime,
  getChartBucketStart,
  shiftChartBucket,
  type TimeGranularity,
} from '@/lib/time'

type TFunction = (key: string) => string
type ChartRange = { start_timestamp: number; end_timestamp: number }
type Usage = { count: number; quota: number }
type ChartSummary = {
  totalCount: number
  totalQuota: number
  totalTokens: number
}
type TimeBucket = { TimeKey: number; Time: string }
type TooltipLine = {
  key: string
  value: string | number
  datum?: { TimeSum: number }
}

const MAX_QUOTA_MODELS = 15
const MAX_CALL_MODELS = 20
const MAX_PIE_MODELS = 10

function normalizeUsageValue(value: unknown): number {
  const numeric = Number(value)
  if (!Number.isSafeInteger(numeric) || numeric < 0) return 0
  return numeric
}

function addUsageValue(left: number, right: number): number {
  const total = left + right
  return Number.isSafeInteger(total) ? total : Number.MAX_SAFE_INTEGER
}

export function getDashboardChartColors(domainLength: number): string[] {
  const scheme =
    vchartDefaultDataScheme.find(
      (item) => !item.maxDomainLength || domainLength <= item.maxDomainLength
    ) ?? vchartDefaultDataScheme.at(-1)

  return (scheme?.scheme ?? []).filter(
    (color): color is string => typeof color === 'string'
  )
}

function renderQuotaCompat(
  rawQuota: number,
  digits = 4,
  locale?: Intl.LocalesArgument
): string {
  const { config, meta } = getCurrencyDisplay()
  if (meta.kind === 'tokens') return rawQuota.toLocaleString(locale)
  const usd = rawQuota / config.quotaPerUnit
  const rate = 'exchangeRate' in meta ? meta.exchangeRate : 1
  const symbol = 'symbol' in meta ? meta.symbol : '$'
  const value = usd * rate
  const fixed = value.toFixed(digits)
  const formatted = value.toLocaleString(locale, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  })
  if (Number.parseFloat(fixed) === 0 && rawQuota > 0 && value > 0) {
    return (
      symbol +
      Math.pow(10, -digits).toLocaleString(locale, {
        minimumFractionDigits: digits,
        maximumFractionDigits: digits,
      })
    )
  }
  return symbol + formatted
}

function aggregateChartUsage(
  rows: QuotaDataItem[],
  group: 'model_name' | 'username',
  granularity: TimeGranularity,
  range?: ChartRange
) {
  const totals = new Map<string, Usage>()
  const buckets = new Map<number, Map<string, Usage>>()
  const summary: ChartSummary = { totalCount: 0, totalQuota: 0, totalTokens: 0 }
  const times: TimeBucket[] = []
  if (
    range &&
    (!Number.isSafeInteger(range.start_timestamp) ||
      !Number.isSafeInteger(range.end_timestamp) ||
      range.start_timestamp <= 0 ||
      range.end_timestamp <= 0 ||
      range.start_timestamp > range.end_timestamp)
  ) {
    return { totals, buckets, summary, times }
  }
  for (const row of rows ?? []) {
    const timestamp = row.created_at
    if (!Number.isSafeInteger(timestamp) || timestamp <= 0) continue
    if (
      range &&
      (timestamp < range.start_timestamp || timestamp > range.end_timestamp)
    ) {
      continue
    }
    const start = getChartBucketStart(timestamp, granularity)
    if (!Number.isFinite(start)) continue
    const name = row[group] || (group === 'username' ? 'unknown' : 'Unknown')
    const count = normalizeUsageValue(row.count)
    const quota = normalizeUsageValue(row.quota)
    const tokenUsed = normalizeUsageValue(row.token_used)
    let bucket = buckets.get(start)
    if (!bucket) {
      bucket = new Map()
      buckets.set(start, bucket)
    }
    for (const groups of [totals, bucket]) {
      const usage = groups.get(name) ?? { count: 0, quota: 0 }
      usage.count = addUsageValue(usage.count, count)
      usage.quota = addUsageValue(usage.quota, quota)
      groups.set(name, usage)
    }
    summary.totalCount = addUsageValue(summary.totalCount, count)
    summary.totalQuota = addUsageValue(summary.totalQuota, quota)
    summary.totalTokens = addUsageValue(summary.totalTokens, tokenUsed)
  }
  if (!buckets.size) return { totals, buckets, summary, times }

  const starts = new Set(buckets.keys())
  let firstObserved = Infinity
  let lastObserved = -Infinity
  for (const start of starts) {
    firstObserved = Math.min(firstObserved, start)
    lastObserved = Math.max(lastObserved, start)
  }
  const first = range
    ? getChartBucketStart(range.start_timestamp, granularity)
    : Math.min(
        firstObserved,
        shiftChartBucket(lastObserved, granularity, 1 - MAX_CHART_TREND_POINTS)
      )
  const last = range
    ? getChartBucketStart(range.end_timestamp, granularity)
    : lastObserved
  if (Number.isFinite(first)) starts.add(first)
  if (Number.isFinite(last)) starts.add(last)
  const denseEnd = shiftChartBucket(
    first,
    granularity,
    MAX_DENSE_CHART_BUCKETS - 1
  )
  if (last <= denseEnd || !Number.isFinite(denseEnd)) {
    let start = first
    for (
      let index = 1;
      index < MAX_DENSE_CHART_BUCKETS && start < last;
      index++
    ) {
      const next = shiftChartBucket(start, granularity, 1)
      if (!(next > start && next <= last)) break
      starts.add(next)
      start = next
    }
  }
  for (const TimeKey of [...starts].sort((left, right) => left - right)) {
    times.push({ TimeKey, Time: formatChartTime(TimeKey, granularity) })
  }
  return { totals, buckets, summary, times }
}

function overflowLabel(
  totals: Map<string, Usage>,
  label: string,
  fallback: string
) {
  if (!totals.has(label)) return label
  let name = fallback
  for (let suffix = 2; totals.has(name); suffix++) {
    name = `${fallback} (${suffix})`
  }
  return name
}

function rankedNames(totals: Map<string, Usage>, metric: keyof Usage) {
  return [...totals.entries()]
    .sort(([left, first], [right, second]) => {
      const difference = second[metric] - first[metric]
      if (difference !== 0) return difference
      if (left < right) return -1
      if (left > right) return 1
      return 0
    })
    .map(([name]) => name)
}

function topSeries(
  names: string[],
  limit: number,
  groups: Map<string, Usage> | undefined,
  metric: keyof Usage,
  total: number,
  other: string
) {
  const values = names
    .slice(0, limit)
    .map((name) => ({ name, value: groups?.get(name)?.[metric] ?? 0 }))
  if (names.length > limit) {
    values.push({
      name: other,
      value: Math.max(
        0,
        total - values.reduce((sum, row) => sum + row.value, 0)
      ),
    })
  }
  return values
}

function timeAxis(granularity: TimeGranularity) {
  return {
    orient: 'bottom',
    type: 'band',
    label: {
      formatMethod: (value: string | number) =>
        formatChartTime(Number(value), granularity),
    },
  }
}

function trendTooltip(
  name: 'Model' | 'User',
  metric: 'Count' | 'rawQuota',
  format: (value: number) => string,
  totalLabel: string,
  other: string,
  collapse = false
) {
  const title = { value: { field: 'Time' } }
  return {
    renderMode: 'canvas',
    mark: {
      title,
      content: [
        {
          key: { field: name },
          value: (datum: Record<string, number>) => format(datum[metric]),
        },
        {
          key: totalLabel,
          value: (datum: Record<string, number>) => format(datum.TimeSum),
        },
      ],
    },
    dimension: {
      title,
      content: [{ key: { field: name }, value: { field: metric } }],
      updateContent: (items: TooltipLine[] = []) => {
        const sorted = [...items].sort(
          (first, second) =>
            Number(first.key === other) - Number(second.key === other) ||
            Number(second.value) - Number(first.value)
        )
        const total =
          items[0]?.datum?.TimeSum ??
          items.reduce((sum, item) => sum + Number(item.value), 0)
        if (collapse && sorted.length > MAX_QUOTA_MODELS) {
          const tail = sorted.splice(MAX_QUOTA_MODELS)
          sorted.push({
            key: other,
            value: tail.reduce((sum, item) => sum + Number(item.value), 0),
          })
        }
        return [
          { key: totalLabel, value: format(total) },
          ...sorted.map((item) => ({
            ...item,
            value: format(Number(item.value)),
          })),
        ]
      },
    },
  }
}

export function processChartData(
  data: QuotaDataItem[],
  timeGranularity: TimeGranularity = 'day',
  t: TFunction = (key) => key,
  chartCornerRadius?: number,
  range?: ChartRange,
  locale?: Intl.LocalesArgument,
  includeQuotaCharts = true
): ProcessedChartData & ChartSummary {
  const { buckets, totals, summary, times } = aggregateChartUsage(
    data,
    'model_name',
    timeGranularity,
    range
  )
  const other = overflowLabel(totals, t('Other'), t('Other models'))
  const totalLabel = t('Total:')
  const byCount = rankedNames(totals, 'count')
  const byQuota = includeQuotaCharts ? rankedNames(totals, 'quota') : []
  const domain = [...totals.keys()].sort()
  if (domain.length > MAX_PIE_MODELS) domain.push(other)
  const color = {
    type: 'ordinal',
    domain,
    range: getDashboardChartColors(domain.length),
  }
  const quotaPerUnit = getCurrencyDisplay().config.quotaPerUnit
  const formatCount = new Intl.NumberFormat(locale, {
    maximumFractionDigits: 0,
  }).format
  type QuotaPoint = TimeBucket & {
    Model: string
    rawQuota: number
    Usage: number
    TimeSum: number
  }
  const bars: QuotaPoint[] = []
  const areas: QuotaPoint[] = []
  const calls: (TimeBucket & {
    Model: string
    Count: number
    TimeSum: number
  })[] = []
  for (const time of times) {
    const groups = buckets.get(time.TimeKey)
    let count = 0
    for (const usage of groups?.values() ?? []) {
      count += usage.count
    }
    if (includeQuotaCharts) {
      let quota = 0
      for (const usage of groups?.values() ?? []) quota += usage.quota
      const quotaPoint = (Model: string, rawQuota: number) => ({
        ...time,
        Model,
        rawQuota,
        Usage: rawQuota / quotaPerUnit,
        TimeSum: quota,
      })
      const area = topSeries(
        byQuota,
        MAX_QUOTA_MODELS,
        groups,
        'quota',
        quota,
        other
      ).map(({ name, value }) => quotaPoint(name, value))
      areas.push(...area)
      bars.push(
        ...[...area].sort((first, second) => second.rawQuota - first.rawQuota)
      )
    }
    for (const { name, value } of topSeries(
      byCount,
      MAX_CALL_MODELS,
      groups,
      'count',
      count,
      other
    )) {
      calls.push({ ...time, Model: name, Count: value, TimeSum: count })
    }
  }
  const pie = topSeries(
    byCount,
    MAX_PIE_MODELS,
    totals,
    'count',
    summary.totalCount,
    other
  ).map(({ name, value }) => ({ type: name, value }))
  const ranking = includeQuotaCharts
    ? topSeries(
        byCount,
        MAX_CALL_MODELS,
        totals,
        'count',
        summary.totalCount,
        other
      ).map(({ name, value }) => ({ Model: name, Count: value }))
    : []
  const shared = {
    color,
    legends: { visible: totals.size > 0, selectMode: 'single' },
    background: { fill: 'transparent' },
    animation: true,
  }
  const trend = {
    ...shared,
    xField: 'TimeKey',
    seriesField: 'Model',
    axes: [timeAxis(timeGranularity)],
  }
  const areaStyle = {
    stack: false,
    area: { style: { fillOpacity: 0.08, curveType: 'monotone' } },
    line: { style: { lineWidth: 2, curveType: 'monotone' } },
    point: { visible: times.length === 1 },
  }
  const bar = { state: { hover: { stroke: '#000', lineWidth: 1 } } }
  const totalLine = { key: totalLabel, value: formatCount(summary.totalCount) }
  return {
    ...summary,
    totalQuotaDisplay: renderQuotaCompat(summary.totalQuota, 2, locale),
    totalCountDisplay: formatCount(summary.totalCount),
    spec_pie: {
      ...shared,
      type: 'pie',
      data: [{ id: 'id0', values: pie }],
      outerRadius: 0.8,
      innerRadius: 0.5,
      padAngle: 0.6,
      valueField: 'value',
      categoryField: 'type',
      pie: {
        style:
          chartCornerRadius == null ? {} : { cornerRadius: chartCornerRadius },
        state: {
          hover: { outerRadius: 0.85, stroke: '#000', lineWidth: 1 },
          selected: { outerRadius: 0.85, stroke: '#000', lineWidth: 1 },
        },
      },
      title: {
        visible: true,
        text: t('Call Count Distribution'),
        subtext: totals.size ? undefined : t('No data available'),
      },
      legends: { visible: totals.size > 0, orient: 'left' },
      label: { visible: totals.size > 0 },
      tooltip: {
        renderMode: 'canvas',
        activeType: 'mark',
        mark: {
          content: [
            {
              key: { field: 'type' },
              value: (datum: { value: number }) => formatCount(datum.value),
            },
            totalLine,
          ],
        },
      },
    },
    spec_line: {
      ...trend,
      type: 'bar',
      data: [{ id: 'barData', values: bars }],
      yField: 'Usage',
      stack: true,
      bar,
      tooltip: trendTooltip(
        'Model',
        'rawQuota',
        (value) => renderQuotaCompat(value, 4, locale),
        totalLabel,
        other,
        true
      ),
    },
    spec_area: {
      ...trend,
      ...areaStyle,
      type: 'area',
      data: [{ id: 'areaData', values: areas }],
      yField: 'Usage',
      tooltip: trendTooltip(
        'Model',
        'rawQuota',
        (value) => renderQuotaCompat(value, 4, locale),
        totalLabel,
        other
      ),
    },
    spec_model_line: {
      ...trend,
      ...areaStyle,
      type: 'area',
      data: [{ id: 'lineData', values: calls }],
      yField: 'Count',
      title: { visible: true, text: t('Call Trend') },
      tooltip: trendTooltip('Model', 'Count', formatCount, totalLabel, other),
    },
    spec_rank_bar: {
      ...shared,
      type: 'bar',
      data: [{ id: 'rankData', values: ranking }],
      xField: 'Model',
      yField: 'Count',
      seriesField: 'Model',
      bar,
      title: { visible: true, text: t('Call Count Ranking') },
      tooltip: {
        renderMode: 'canvas',
        activeType: 'mark',
        mark: {
          content: [
            {
              key: { field: 'Model' },
              value: (datum: { Count: number }) => formatCount(datum.Count),
            },
            totalLine,
          ],
        },
      },
    },
  }
}

const USER_COLORS = [
  '#5B8FF9',
  '#5AD8A6',
  '#F6BD16',
  '#E8684A',
  '#6DC8EC',
  '#9270CA',
  '#FF9D4D',
  '#269A99',
  '#FF99C3',
  '#5D7092',
]

export function processUserChartData(
  data: QuotaDataItem[],
  timeGranularity: TimeGranularity = 'day',
  t: TFunction = (key) => key,
  limit = 10,
  range?: ChartRange
): ProcessedUserChartData {
  const { totals, buckets, summary, times } = aggregateChartUsage(
    data,
    'username',
    timeGranularity,
    range
  )
  const other = overflowLabel(totals, t('Other'), t('Other users'))
  const names = rankedNames(totals, 'quota')
  const top = Number.isFinite(limit) ? Math.max(0, Math.floor(limit)) : 10
  const quotaPerUnit = getCurrencyDisplay().config.quotaPerUnit
  const quotaPoint = ({ name, value }: { name: string; value: number }) => ({
    User: name,
    rawQuota: value,
    Usage: value / quotaPerUnit,
  })
  const ranking = topSeries(
    names,
    top,
    totals,
    'quota',
    summary.totalQuota,
    other
  ).map(quotaPoint)
  const values = times.flatMap((time) => {
    const groups = buckets.get(time.TimeKey)
    let TimeSum = 0
    for (const usage of groups?.values() ?? []) TimeSum += usage.quota
    return topSeries(names, top, groups, 'quota', TimeSum, other).map(
      (user) => ({ ...time, ...quotaPoint(user), TimeSum })
    )
  })
  const formatValue = (raw: number) => renderQuotaCompat(raw, 2)
  const totalLabel = t('Total:')
  const subtext = totals.size
    ? `${totalLabel} ${formatValue(summary.totalQuota)}`
    : t('No data available')
  const shared = {
    seriesField: 'User',
    color: {
      type: 'ordinal',
      domain: ranking.map((row) => row.User),
      range: USER_COLORS,
    },
    background: { fill: 'transparent' },
    animation: true,
  }
  return {
    spec_user_rank: {
      ...shared,
      type: 'bar',
      data: [{ id: 'userRankData', values: ranking }],
      xField: 'rawQuota',
      yField: 'User',
      direction: 'horizontal',
      title: { visible: true, text: t('User Consumption Ranking'), subtext },
      legends: { visible: false },
      bar: { state: { hover: { stroke: '#000', lineWidth: 1 } } },
      label: {
        visible: true,
        position: 'outside',
        formatMethod: formatValue,
        style: { fontSize: 11 },
      },
      axes: [
        { orient: 'left', type: 'band' },
        { orient: 'bottom', type: 'linear', visible: false },
      ],
      tooltip: {
        renderMode: 'canvas',
        activeType: 'mark',
        mark: {
          content: [
            {
              key: { field: 'User' },
              value: (datum: { rawQuota: number }) =>
                formatValue(datum.rawQuota),
            },
          ],
        },
      },
    },
    spec_user_trend: {
      ...shared,
      type: 'area',
      data: [{ id: 'userTrendData', values }],
      xField: 'TimeKey',
      yField: 'rawQuota',
      stack: false,
      title: { visible: true, text: t('User Consumption Trend'), subtext },
      legends: { visible: totals.size > 0, selectMode: 'single' },
      axes: [
        timeAxis(timeGranularity),
        {
          orient: 'left',
          type: 'linear',
          label: { formatMethod: formatValue },
        },
      ],
      tooltip: trendTooltip('User', 'rawQuota', formatValue, totalLabel, other),
      area: { style: { fillOpacity: 0.15, curveType: 'monotone' } },
      line: { style: { lineWidth: 2, curveType: 'monotone' } },
      point: { visible: times.length === 1 },
    },
  }
}

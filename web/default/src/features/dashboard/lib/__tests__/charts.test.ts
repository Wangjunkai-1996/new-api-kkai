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
import { describe, expect, test } from 'vitest'

import type { QuotaDataItem } from '@/features/dashboard/types'
import type { TimeGranularity } from '@/lib/time'

import { processChartData, processUserChartData } from '../charts'

const cases: Array<{
  granularity: TimeGranularity
  start: string
  interval: number
  labels: string[]
}> = [
  {
    granularity: 'hour',
    start: '2025-12-31T20:00:00',
    interval: 3600,
    labels: [
      '12-31 20:00',
      '12-31 21:00',
      '12-31 22:00',
      '12-31 23:00',
      '01-01 00:00',
      '01-01 01:00',
      '01-01 02:00',
      '01-01 03:00',
    ],
  },
  {
    granularity: 'day',
    start: '2025-12-27T12:00:00',
    interval: 86400,
    labels: [
      '12-27',
      '12-28',
      '12-29',
      '12-30',
      '12-31',
      '01-01',
      '01-02',
      '01-03',
    ],
  },
  {
    granularity: 'week',
    start: '2025-11-29T12:00:00',
    interval: 604800,
    labels: [
      '11-24 - 11-30',
      '12-01 - 12-07',
      '12-08 - 12-14',
      '12-15 - 12-21',
      '12-22 - 12-28',
      '12-29 - 01-04',
      '01-05 - 01-11',
      '01-12 - 01-18',
    ],
  },
]

function unorderedUsage(start: string, interval: number): QuotaDataItem[] {
  const first = Date.parse(`${start}+08:00`) / 1000
  const rows = Array.from({ length: 8 }, (_, i) => ({
    created_at: first + i * interval,
    model_name: 'model-a',
    username: 'alice',
    quota: (i + 1) * 500000,
    count: i + 1,
  }))
  // Same hour, or a second hourly record within the same day/week label.
  const duplicate = {
    ...rows[0],
    created_at: first + (interval === 3600 ? 0 : 3600),
    quota: 5000000,
    count: 10,
  }
  return [
    rows[7],
    rows[2],
    duplicate,
    rows[5],
    rows[0],
    rows[6],
    rows[1],
    rows[4],
    rows[3],
  ]
}

describe.each(cases)('dashboard $granularity chart chronology', (scenario) => {
  test('orders model buckets across New Year while aggregating repeated labels', () => {
    const data = unorderedUsage(scenario.start, scenario.interval)
    const original = structuredClone(data)
    const result = processChartData(data, scenario.granularity)

    for (const key of ['spec_line', 'spec_area', 'spec_model_line'] as const) {
      const values: Array<{ Time: string; rawQuota: number; Count: number }> =
        result[key].data[0].values
      expect(values.map((row) => row.Time)).toEqual(scenario.labels)
      expect(
        values.map((row) =>
          key === 'spec_model_line' ? row.Count : row.rawQuota / 500000
        )
      ).toEqual([11, 2, 3, 4, 5, 6, 7, 8])
    }
    expect(data).toEqual(original)
  })

  test('orders user buckets across New Year while aggregating repeated labels', () => {
    const data = unorderedUsage(scenario.start, scenario.interval)
    const original = structuredClone(data)
    const result = processUserChartData(data, scenario.granularity)
    const values: Array<{ Time: string; rawQuota: number }> =
      result.spec_user_trend.data[0].values

    expect(values.map((row) => row.Time)).toEqual(scenario.labels)
    expect(values.map((row) => row.rawQuota / 500000)).toEqual([
      11, 2, 3, 4, 5, 6, 7, 8,
    ])
    expect(data).toEqual(original)
  })

  test('keeps the existing seven padded model buckets in chronological order', () => {
    const data = [unorderedUsage(scenario.start, scenario.interval)[0]]
    const result = processChartData(data, scenario.granularity)

    for (const key of ['spec_line', 'spec_area', 'spec_model_line'] as const) {
      const values: Array<{ Time: string; rawQuota: number; Count: number }> =
        result[key].data[0].values
      expect(values.map((row) => row.Time)).toEqual(scenario.labels.slice(1))
      expect(
        values.map((row) =>
          key === 'spec_model_line' ? row.Count : row.rawQuota / 500000
        )
      ).toEqual([0, 0, 0, 0, 0, 0, 8])
    }
  })
})

const unix = (date: string) => Date.parse(`${date}+08:00`) / 1000

test('clips hourly rows inclusively and fills natural months in all four time charts', () => {
  const start = unix('2023-12-15T12:00:00')
  const end = unix('2024-04-15T12:00:00')
  const middle = unix('2024-02-29T23:00:00')
  const range = { start_timestamp: start, end_timestamp: end }
  const row = (created_at: number, count: number) => ({
    created_at,
    count,
    token_used: count * 7,
    quota: count * 500000,
  })
  const data = [
    row(end + 1, 99),
    row(middle, 2),
    row(start - 1, 99),
    row(middle - 3600, 3),
  ]
  const models = processChartData(data, 'month', undefined, undefined, range)
  const users = processUserChartData(data, 'month', undefined, undefined, range)
  for (const spec of [
    models.spec_model_line,
    models.spec_line,
    models.spec_area,
    users.spec_user_trend,
  ]) {
    const rows: Array<{
      Time: string
      TimeKey: number
      Count?: number
      rawQuota: number
    }> = spec.data[0].values
    expect(rows.map((item) => item.Time)).toEqual([
      '2023-12',
      '2024-01',
      '2024-02',
      '2024-03',
      '2024-04',
    ])
    expect(rows.map((item) => item.Count ?? item.rawQuota / 500000)).toEqual([
      0, 0, 5, 0, 0,
    ])
  }
  const endpoints = processChartData(
    [...data, row(start, 1), row(end, 2)],
    'month',
    undefined,
    undefined,
    range
  )
  expect(endpoints).toMatchObject({
    totalCount: 8,
    totalTokens: 56,
    totalQuota: 4000000,
    totalCountDisplay: '8',
  })
  expect(endpoints.spec_pie.data[0].values).toEqual([
    { type: 'Unknown', value: 8 },
  ])
  expect(endpoints.spec_rank_bar.data[0].values).toEqual([
    { Model: 'Unknown', Count: 8 },
  ])
})

test('keeps identical display labels in separate numeric band categories across years', () => {
  const first = unix('2025-01-01T00:00:00')
  const last = unix('2026-01-01T00:00:00')
  const data = [
    { created_at: last, count: 5, quota: 5 },
    { created_at: first, count: 3, quota: 3 },
  ]
  const model = processChartData(data, 'day')
  for (const spec of [
    model.spec_model_line,
    model.spec_line,
    model.spec_area,
    processUserChartData(data, 'day').spec_user_trend,
  ]) {
    const rows: Array<Record<string, string | number>> = spec.data[0].values
    const newYears = rows.filter((row) => row.Time === '01-01')
    expect(newYears.map((row) => row[spec.xField])).toEqual([first, last])
    expect(newYears.map((row) => row.Count ?? row.rawQuota)).toEqual([3, 5])
    expect(spec.axes[0].label.formatMethod(first)).toBe('01-01')
    expect(rows).toHaveLength(366)
  }
})

test('retains observations at both ends and in the middle of a huge sparse range', () => {
  const times = ['1980-01-01', '2025-01-01', '2300-01-01'].map((date) =>
    unix(`${date}T00:00:00`)
  )
  const range = {
    start_timestamp: times[0] - 3600,
    end_timestamp: times[2] + 3600,
  }
  const rows = times.map((created_at, index) => ({
    created_at,
    count: index + 1,
    quota: index + 1,
  }))
  const model = processChartData(rows, 'hour', undefined, undefined, range)
  const user = processUserChartData(rows, 'hour', undefined, undefined, range)
  for (const spec of [
    model.spec_model_line,
    model.spec_line,
    model.spec_area,
    user.spec_user_trend,
  ]) {
    expect(
      spec.data[0].values.map((row: { TimeKey: number }) => row.TimeKey)
    ).toEqual([range.start_timestamp, ...times, range.end_timestamp])
    expect(
      spec.data[0].values.map(
        (row: { Count?: number; rawQuota: number }) => row.Count ?? row.rawQuota
      )
    ).toEqual([0, 1, 2, 3, 0])
  }
  expect(model.totalCount).toBe(6)
})

test('returns empty specs when the range contains no valid timestamps', () => {
  const time = unix('2025-01-01T00:00:00')
  const data = [
    Number.NaN,
    Infinity,
    Number.MAX_SAFE_INTEGER,
    0,
    -1,
    time + 0.5,
  ].map((created_at) => ({ created_at, count: 10, quota: 100 }))
  for (const range of [
    undefined,
    { start_timestamp: time, end_timestamp: time },
    { start_timestamp: time, end_timestamp: time - 1 },
  ]) {
    const model = processChartData(data, 'day', undefined, undefined, range)
    const user = processUserChartData(data, 'day', undefined, undefined, range)
    expect(model).toMatchObject({
      totalCount: 0,
      totalQuota: 0,
      totalTokens: 0,
    })
    for (const spec of [
      model.spec_model_line,
      model.spec_area,
      model.spec_line,
      model.spec_rank_bar,
      model.spec_pie,
      user.spec_user_rank,
      user.spec_user_trend,
    ]) {
      expect(spec.data[0].values).toEqual([])
    }
  }
})

test('keeps real Other names separate from overflow and shows unfiltered totals in canvas tooltips', () => {
  const timestamp = unix('2026-01-01T00:00:00')
  const range = { start_timestamp: timestamp, end_timestamp: timestamp }
  const names = [
    '其他',
    '其他模型',
    '<plain text>',
    ...Array.from({ length: 20 }, (_, index) => `model-${index}`),
  ]
  const data = names.map((name, index) => ({
    created_at: timestamp,
    model_name: name,
    username: name,
    count: 23 - index,
    quota: 23 - index,
  }))
  const translate = (key: string) =>
    ({
      Other: '其他',
      'Other models': '其他模型',
      'Other users': '其他用户',
      'Total:': '总计',
    })[key] ?? key
  const model = processChartData(data, 'day', translate, undefined, range)
  const user = processUserChartData(data, 'day', translate, 3, range)
  for (const [spec, dimension, metric, length] of [
    [model.spec_pie, 'type', 'value', 11],
    [model.spec_model_line, 'Model', 'Count', 21],
    [model.spec_rank_bar, 'Model', 'Count', 21],
    [model.spec_area, 'Model', 'rawQuota', 16],
    [model.spec_line, 'Model', 'rawQuota', 16],
    [user.spec_user_rank, 'User', 'rawQuota', 4],
    [user.spec_user_trend, 'User', 'rawQuota', 4],
  ] as const) {
    const rows: Array<Record<string, string | number>> = spec.data[0].values
    expect(new Set(rows.map((row) => row[dimension])).size).toBe(length)
    expect(rows.find((row) => row[dimension] === '其他')?.[metric]).toBe(23)
    expect(
      rows.find((row) => row[dimension] === '<plain text>')?.[metric]
    ).toBe(21)
    expect(rows.reduce((sum, row) => sum + Number(row[metric]), 0)).toBe(276)
    expect(spec.tooltip.renderMode).toBe('canvas')
  }
  const trend = model.spec_model_line
  const datum = trend.data[0].values[0]
  expect(trend.tooltip.mark.title.value.field).toBe('Time')
  expect(trend.tooltip.mark.content[1].value(datum)).toBe('276')
  expect(
    trend.tooltip.dimension.updateContent([{ key: '其他', value: 23, datum }])
  ).toEqual([
    { key: '总计', value: '276' },
    { key: '其他', value: '23', datum },
  ])
})

test('keeps the same limited quota series in empty buckets', () => {
  const first = unix('2026-01-01T00:00:00')
  const last = unix('2026-01-01T02:00:00')
  const rows = Array.from({ length: 30 }, (_, index) => ({
    created_at: index === 0 ? first : last,
    model_name: `model-${index}`,
    count: 1,
    quota: index + 1,
  }))
  const result = processChartData(rows, 'hour', undefined, undefined, {
    start_timestamp: first,
    end_timestamp: last,
  })

  const values = result.spec_line.data[0].values as Array<{
    TimeKey: number
    Model: string
  }>
  const modelsByTime = new Map<number, string[]>()
  for (const row of values) {
    const models = modelsByTime.get(row.TimeKey) ?? []
    models.push(row.Model)
    modelsByTime.set(row.TimeKey, models)
  }
  expect([...new Set(modelsByTime.get(first))].sort()).toEqual(
    [...new Set(modelsByTime.get(first + 3600))].sort()
  )
  expect([...new Set(modelsByTime.get(first + 3600))].sort()).toEqual(
    [...new Set(modelsByTime.get(last))].sort()
  )
  expect(modelsByTime.get(first)).toHaveLength(16)
  expect(modelsByTime.get(first)).toContain('Other')
})

test('ignores non-finite and negative usage values', () => {
  const timestamp = unix('2026-01-01T00:00:00')
  const result = processChartData(
    [
      {
        created_at: timestamp,
        model_name: 'invalid',
        count: Infinity,
        quota: -1,
        token_used: Number.NaN,
      },
      {
        created_at: timestamp,
        model_name: 'valid',
        count: 2,
        quota: 3,
        token_used: 4,
      },
    ],
    'day'
  )
  expect(result.totalCount).toBe(2)
  expect(result.totalQuota).toBe(3)
  expect(result.totalTokens).toBe(4)
})

test('shows area points for one bucket and hides them for multiple buckets', () => {
  const first = unix('2026-01-01T00:00:00')
  const row = { created_at: first, count: 1, quota: 1 }
  const singleBucket = processChartData([row], 'day', undefined, undefined, {
    start_timestamp: first,
    end_timestamp: first,
  })
  const singleUserBucket = processUserChartData(
    [row],
    'day',
    undefined,
    undefined,
    { start_timestamp: first, end_timestamp: first }
  )
  const multipleBuckets = processChartData([row], 'day', undefined, undefined, {
    start_timestamp: first,
    end_timestamp: first + 86400,
  })

  expect(singleBucket.spec_area.point.visible).toBe(true)
  expect(singleBucket.spec_model_line.point.visible).toBe(true)
  expect(singleUserBucket.spec_user_trend.point.visible).toBe(true)
  expect(multipleBuckets.spec_area.point.visible).toBe(false)
  expect(multipleBuckets.spec_model_line.point.visible).toBe(false)
})

test('formats chart counts with the requested locale', () => {
  const timestamp = unix('2026-01-01T00:00:00')
  const result = processChartData(
    [{ created_at: timestamp, count: 1234, quota: 1 }],
    'day',
    undefined,
    undefined,
    { start_timestamp: timestamp, end_timestamp: timestamp },
    'de-DE'
  )
  const expected = new Intl.NumberFormat('de-DE', {
    maximumFractionDigits: 0,
  }).format(1234)
  const datum = result.spec_model_line.data[0].values[0]

  expect(result.totalCountDisplay).toBe(expected)
  expect(result.spec_model_line.tooltip.mark.content[0].value(datum)).toBe(
    expected
  )
  expect(result.spec_pie.tooltip.mark.content[0].value({ value: 1234 })).toBe(
    expected
  )
})

test('skips unused quota series when requested while preserving call series', () => {
  const timestamp = unix('2026-01-01T00:00:00')
  const rows = Array.from({ length: 30 }, (_, index) => ({
    created_at: timestamp,
    model_name: `model-${index}`,
    count: index + 1,
    quota: index + 1,
  }))
  const result = processChartData(
    rows,
    'day',
    undefined,
    undefined,
    { start_timestamp: timestamp, end_timestamp: timestamp },
    undefined,
    false
  )

  expect(result.spec_line.data[0].values).toEqual([])
  expect(result.spec_area.data[0].values).toEqual([])
  expect(result.spec_model_line.data[0].values).toHaveLength(21)
  expect(result.spec_pie.data[0].values).toHaveLength(11)

  const defaultResult = processChartData(rows, 'day', undefined, undefined, {
    start_timestamp: timestamp,
    end_timestamp: timestamp,
  })
  expect(defaultResult.spec_line.data[0].values).toHaveLength(16)
  expect(defaultResult.spec_line.data[0].values).toContainEqual(
    expect.objectContaining({ Model: 'Other' })
  )
})

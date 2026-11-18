import { describe, expect, it } from 'vitest'

import {
  formatChartTime,
  getChartBucketStart,
  normalizeDashboardHourRange,
  shiftChartBucket,
} from '../time'

const seconds = (iso: string): number => Date.parse(iso) / 1000

describe('UTC+8 chart buckets', () => {
  it.each([
    [
      'hour',
      '2024-12-31T16:59:59Z',
      '2025-01-01T00:00:00+08:00',
      '01-01 00:00',
    ],
    ['day', '2024-12-31T15:59:59Z', '2024-12-31T00:00:00+08:00', '12-31'],
    ['day', '2024-12-31T16:00:00Z', '2025-01-01T00:00:00+08:00', '01-01'],
    [
      'week',
      '2025-01-05T15:59:59Z',
      '2024-12-30T00:00:00+08:00',
      '12-30 - 01-05',
    ],
    [
      'week',
      '2025-01-05T16:00:00Z',
      '2025-01-06T00:00:00+08:00',
      '01-06 - 01-12',
    ],
    ['month', '2024-02-29T15:59:59Z', '2024-02-01T00:00:00+08:00', '2024-02'],
    ['month', '2024-02-29T16:00:00Z', '2024-03-01T00:00:00+08:00', '2024-03'],
  ] as const)('%s bucket containing %s', (granularity, input, start, label) => {
    const bucket = getChartBucketStart(seconds(input), granularity)
    expect(bucket).toBe(seconds(start))
    expect(formatChartTime(bucket, granularity)).toBe(label)
  })

  it('steps over a leap February and across New Year by calendar month', () => {
    const march = seconds('2024-03-01T00:00:00+08:00')
    expect(shiftChartBucket(march, 'month', -1)).toBe(
      seconds('2024-02-01T00:00:00+08:00')
    )
    expect(shiftChartBucket(march, 'month', 10)).toBe(
      seconds('2025-01-01T00:00:00+08:00')
    )
  })

  it('keeps cross-year keys distinct even when the day labels match', () => {
    const first = seconds('2024-01-01T00:00:00+08:00')
    const next = seconds('2025-01-01T00:00:00+08:00')
    expect(getChartBucketStart(first, 'day')).toBeLessThan(
      getChartBucketStart(next, 'day')
    )
    expect(formatChartTime(first, 'day')).toBe(formatChartTime(next, 'day'))
  })

  it.each([Number.NaN, Infinity, Number.MAX_VALUE])('rejects %s', (input) => {
    expect(getChartBucketStart(input, 'day')).toBeNaN()
    expect(shiftChartBucket(input, 'month', 1)).toBeNaN()
    expect(formatChartTime(input, 'week')).toBe('')
  })

  it('aligns dashboard ranges to the stored hourly buckets', () => {
    expect(
      normalizeDashboardHourRange({
        start_timestamp: seconds('2025-01-01T00:02:03+08:00'),
        end_timestamp: seconds('2025-01-01T01:59:59+08:00'),
      })
    ).toEqual({
      start_timestamp: seconds('2025-01-01T00:00:00+08:00'),
      end_timestamp: seconds('2025-01-01T01:00:00+08:00'),
    })
  })

  it.each([
    { start_timestamp: Number.NaN, end_timestamp: 3600 },
    { start_timestamp: 3600, end_timestamp: Infinity },
    { start_timestamp: Number.MAX_VALUE, end_timestamp: Number.MAX_VALUE },
    { start_timestamp: 7200, end_timestamp: 3600 },
  ])('rejects invalid dashboard range %#', (range) => {
    expect(normalizeDashboardHourRange(range)).toEqual({
      start_timestamp: Number.NaN,
      end_timestamp: Number.NaN,
    })
  })
})

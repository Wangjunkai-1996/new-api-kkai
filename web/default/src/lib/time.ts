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
/**
 * Time utility functions for consistent time handling across the application
 */
import utc from 'dayjs/plugin/utc'

import dayjs from '@/lib/dayjs'

dayjs.extend(utc)

/**
 * Time granularity type
 */
export type TimeGranularity = 'hour' | 'day' | 'week' | 'month'

const CHART_TIMEZONE_OFFSET_SECONDS = 8 * 60 * 60

export function formatDashboardDateTimeInput(date?: Date): string {
  if (!date || !Number.isFinite(date.getTime())) return ''
  return dayjs
    .utc(date)
    .add(CHART_TIMEZONE_OFFSET_SECONDS, 'second')
    .format('YYYY-MM-DDTHH:mm:ss')
}

export function parseDashboardDateTimeInput(value: string): Date | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(value)) return undefined
  const date = new Date(
    Date.parse(`${value}Z`) - CHART_TIMEZONE_OFFSET_SECONDS * 1000
  )
  const normalized = value.length === 16 ? `${value}:00` : value
  return formatDashboardDateTimeInput(date) === normalized ? date : undefined
}

function chartBucketDate(
  timestamp: number,
  granularity: TimeGranularity
): dayjs.Dayjs {
  if (!Number.isFinite(timestamp)) return dayjs.utc(Number.NaN)
  const date = dayjs.utc((timestamp + CHART_TIMEZONE_OFFSET_SECONDS) * 1000)
  switch (granularity) {
    case 'week':
      return date.startOf('day').subtract((date.day() + 6) % 7, 'day')
    case 'day':
    case 'hour':
    case 'month':
      return date.startOf(granularity)
    default:
      return dayjs.utc(Number.NaN)
  }
}

export function getChartBucketStart(
  timestamp: number,
  granularity: TimeGranularity
): number {
  return (
    chartBucketDate(timestamp, granularity).unix() -
    CHART_TIMEZONE_OFFSET_SECONDS
  )
}

export function shiftChartBucket(
  start: number,
  granularity: TimeGranularity,
  amount: number
): number {
  if (!Number.isSafeInteger(amount)) return Number.NaN
  const shifted = chartBucketDate(start, granularity).add(amount, granularity)
  return shifted.unix() - CHART_TIMEZONE_OFFSET_SECONDS
}

export function getChartBucketCount(
  startTimestamp: number,
  endTimestamp: number,
  granularity: TimeGranularity
): number {
  if (
    !Number.isFinite(startTimestamp) ||
    !Number.isFinite(endTimestamp) ||
    endTimestamp < startTimestamp
  ) {
    return 0
  }
  const first = chartBucketDate(startTimestamp, granularity)
  const last = chartBucketDate(endTimestamp, granularity)
  if (!first.isValid() || !last.isValid()) return 0
  const difference =
    granularity === 'week'
      ? last.diff(first, 'day') / 7
      : last.diff(first, granularity)
  return Number.isSafeInteger(difference) ? difference + 1 : 0
}

/**
 * Convert Date object to Unix timestamp (seconds)
 */
export function dateToUnixTimestamp(date: Date): number {
  return Math.floor(date.getTime() / 1000)
}

export function normalizeDashboardHourRange(range: {
  start_timestamp: number
  end_timestamp: number
}): { start_timestamp: number; end_timestamp: number } {
  const start_timestamp = getChartBucketStart(range.start_timestamp, 'hour')
  const end_timestamp = getChartBucketStart(range.end_timestamp, 'hour')
  if (
    !Number.isSafeInteger(start_timestamp) ||
    !Number.isSafeInteger(end_timestamp) ||
    start_timestamp <= 0 ||
    range.end_timestamp < range.start_timestamp
  ) {
    return { start_timestamp: Number.NaN, end_timestamp: Number.NaN }
  }
  return { start_timestamp, end_timestamp }
}

/**
 * Get start of day for a Unix timestamp (seconds)
 * Sets time to 00:00:00
 */
export function toStartOfDay(tsSec: number): number {
  const d = new Date(tsSec * 1000)
  d.setHours(0, 0, 0, 0)
  return Math.floor(d.getTime() / 1000)
}

/**
 * Get start of day for a Date object
 * Returns new Date with time set to 00:00:00
 */
export function getStartOfDay(date: Date = new Date()): Date {
  const d = new Date(date)
  d.setHours(0, 0, 0, 0)
  return d
}

/**
 * Get end of day for a Date object
 * Returns new Date with time set to 23:59:59.999
 */
export function getEndOfDay(date: Date = new Date()): Date {
  const d = new Date(date)
  d.setHours(23, 59, 59, 999)
  return d
}

/**
 * Calculate date range with start and end of day normalization
 * @param days Number of days to go back
 * @param fromDate Starting point (defaults to now)
 * @returns Object with normalized start (00:00:00) and end (23:59:59) dates
 */
export function getNormalizedDateRange(
  days: number,
  fromDate: Date = new Date()
): { start: Date; end: Date } {
  const end = new Date(fromDate)
  const start = new Date(fromDate)
  start.setDate(end.getDate() - days)

  return {
    start: getStartOfDay(start),
    end: getEndOfDay(end),
  }
}

/**
 * Calculate a rolling date range ending at the current moment.
 * Example: 1 day means the last 24 hours, not yesterday 00:00 to today 23:59.
 */
export function getRollingDateRange(
  days: number,
  fromDate: Date = new Date()
): { start: Date; end: Date } {
  const end = new Date(fromDate)
  const start = new Date(end.getTime() - days * 24 * 60 * 60 * 1000)

  return { start, end }
}

/**
 * Compute time range as Unix timestamps (seconds)
 * @param days Default number of days if no dates provided
 * @param startDate Optional start date
 * @param endDate Optional end date
 * @param useStartOfDay Whether to normalize to start/end of day
 * @returns Object with start_timestamp and end_timestamp in seconds
 */
export function computeTimeRange(
  days: number,
  startDate?: Date,
  endDate?: Date,
  useStartOfDay = false
): { start_timestamp: number; end_timestamp: number } {
  const now = Math.floor(Date.now() / 1000)

  if (useStartOfDay) {
    const defaultEnd = toStartOfDay(now)
    const end = endDate
      ? toStartOfDay(dateToUnixTimestamp(endDate))
      : defaultEnd
    const start = startDate
      ? toStartOfDay(dateToUnixTimestamp(startDate))
      : end - days * 24 * 3600

    return {
      start_timestamp: start,
      end_timestamp: end + 24 * 3600 - 1, // End of day
    }
  }

  // Normal mode without day normalization
  // Add 1 hour buffer to end time (matches legacy frontend behavior)
  // This ensures the current hour's data is fully included
  const end = endDate ? dateToUnixTimestamp(endDate) : now + 3600
  const start = startDate
    ? dateToUnixTimestamp(startDate)
    : end - days * 24 * 3600

  return { start_timestamp: start, end_timestamp: end }
}

/**
 * Format Unix timestamp (seconds) to YYYY-MM-DD
 */
export function formatDate(tsSec: number): string {
  return dayjs(tsSec * 1000).format('YYYY-MM-DD')
}

/**
 * Format Date object to YYYY-MM-DD HH:mm:ss
 */
export function formatDateTimeObject(date: Date): string {
  return dayjs(date).format('YYYY-MM-DD HH:mm:ss')
}

/**
 * Format timestamp for chart display based on time granularity
 * @param timestamp Unix timestamp in seconds
 * @param granularity Hour, natural day, Monday–Sunday week, or calendar month
 * @returns Formatted string suitable for chart axis
 */
export function formatChartTime(
  timestamp: number,
  granularity: TimeGranularity = 'day'
): string {
  const date = chartBucketDate(timestamp, granularity)
  if (!date.isValid()) return ''
  if (granularity === 'month') return date.format('YYYY-MM')
  if (granularity === 'hour') return date.format('MM-DD HH:00')
  if (granularity === 'week') {
    return `${date.format('MM-DD')} - ${date.add(6, 'day').format('MM-DD')}`
  }
  return date.format('MM-DD')
}

/**
 * Add time duration to a date
 * @param months Number of months to add
 * @param days Number of days to add
 * @param hours Number of hours to add
 * @param baseDate Base date to add time to (defaults to now)
 * @returns New date with added time, or undefined if all parameters are 0
 */
export function addTimeToDate(
  months: number,
  days: number,
  hours: number,
  baseDate: Date = new Date()
): Date | undefined {
  if (months === 0 && days === 0 && hours === 0) {
    return undefined
  }

  const result = new Date(baseDate)
  result.setMonth(result.getMonth() + months)
  result.setDate(result.getDate() + days)
  result.setHours(result.getHours() + hours)

  return result
}

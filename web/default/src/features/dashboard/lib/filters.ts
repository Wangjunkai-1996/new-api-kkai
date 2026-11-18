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
  DASHBOARD_CHART_PREFERENCES_STORAGE_KEY,
  DEFAULT_DASHBOARD_CHART_PREFERENCES,
  EMPTY_DASHBOARD_FILTERS,
  TIME_GRANULARITY_OPTIONS,
  TIME_GRANULARITY_STORAGE_KEY,
  TIME_RANGE_30_DAYS,
  TIME_RANGE_BY_GRANULARITY,
  TIME_RANGE_PRESETS,
  USER_TIME_GRANULARITY_STORAGE_KEY,
} from '@/features/dashboard/constants'
import type {
  ConsumptionDistributionChartType,
  DashboardChartPreferences,
  DashboardFilters,
  ModelAnalyticsChartTab,
} from '@/features/dashboard/types'
import {
  dateToUnixTimestamp,
  getRollingDateRange,
  normalizeDashboardHourRange,
  type TimeGranularity,
} from '@/lib/time'

function isTimeGranularity(value: unknown): value is TimeGranularity {
  return TIME_GRANULARITY_OPTIONS.some((option) => option.value === value)
}

function readPreference(key: string): string | null {
  try {
    return typeof window === 'undefined' ? null : localStorage.getItem(key)
  } catch {
    return null
  }
}

function writePreference(key: string, value: string): void {
  try {
    if (typeof window !== 'undefined') localStorage.setItem(key, value)
  } catch {
    return
  }
}

function getStoredGranularity(key: string): TimeGranularity | undefined {
  const saved = readPreference(key)
  return isTimeGranularity(saved) ? saved : undefined
}

function isConsumptionDistributionChartType(
  value: unknown
): value is ConsumptionDistributionChartType {
  return value === 'bar' || value === 'area'
}

function isModelAnalyticsChartTab(
  value: unknown
): value is ModelAnalyticsChartTab {
  return value === 'trend' || value === 'proportion' || value === 'top'
}

function normalizeSavedTimeRangeDays(value: unknown): number | undefined {
  if (value === 29) return TIME_RANGE_30_DAYS
  return TIME_RANGE_PRESETS.find((preset) => preset.days === value)?.days
}

export function cleanFilters<T extends Record<string, unknown>>(
  filters: T
): Partial<T> {
  const cleaned: Partial<T> = {}
  for (const [key, value] of Object.entries(filters)) {
    if (value === undefined || value === null) continue
    if (typeof value === 'string') {
      const trimmed = value.trim()
      if (trimmed) cleaned[key as keyof T] = trimmed as T[keyof T]
      continue
    }
    cleaned[key as keyof T] = value as T[keyof T]
  }
  return cleaned
}

export function getSavedGranularity(
  override?: TimeGranularity
): TimeGranularity {
  if (override) return override
  return getSavedChartPreferences().defaultTimeGranularity
}

export function getSavedUserGranularity(): TimeGranularity {
  return (
    getStoredGranularity(USER_TIME_GRANULARITY_STORAGE_KEY) ??
    getSavedGranularity()
  )
}

export function saveUserGranularity(granularity: TimeGranularity): void {
  writePreference(USER_TIME_GRANULARITY_STORAGE_KEY, granularity)
}

export function getSavedChartPreferences(): DashboardChartPreferences {
  if (typeof window === 'undefined') return DEFAULT_DASHBOARD_CHART_PREFERENCES

  const fallbackPreferences = {
    ...DEFAULT_DASHBOARD_CHART_PREFERENCES,
    defaultTimeGranularity:
      getStoredGranularity(TIME_GRANULARITY_STORAGE_KEY) ??
      DEFAULT_DASHBOARD_CHART_PREFERENCES.defaultTimeGranularity,
  }

  try {
    const raw = readPreference(DASHBOARD_CHART_PREFERENCES_STORAGE_KEY)
    if (!raw) return fallbackPreferences

    const parsed = JSON.parse(raw) as Partial<DashboardChartPreferences>
    return {
      consumptionDistributionChart: isConsumptionDistributionChartType(
        parsed.consumptionDistributionChart
      )
        ? parsed.consumptionDistributionChart
        : fallbackPreferences.consumptionDistributionChart,
      modelAnalyticsChart: isModelAnalyticsChartTab(parsed.modelAnalyticsChart)
        ? parsed.modelAnalyticsChart
        : fallbackPreferences.modelAnalyticsChart,
      defaultTimeRangeDays:
        normalizeSavedTimeRangeDays(parsed.defaultTimeRangeDays) ??
        fallbackPreferences.defaultTimeRangeDays,
      defaultTimeGranularity: isTimeGranularity(parsed.defaultTimeGranularity)
        ? parsed.defaultTimeGranularity
        : fallbackPreferences.defaultTimeGranularity,
    }
  } catch {
    return fallbackPreferences
  }
}

export function saveChartPreferences(
  preferences: DashboardChartPreferences
): void {
  writePreference(
    DASHBOARD_CHART_PREFERENCES_STORAGE_KEY,
    JSON.stringify(preferences)
  )
}

export function getDefaultDays(granularity?: TimeGranularity): number {
  if (!granularity) return getSavedChartPreferences().defaultTimeRangeDays
  return TIME_RANGE_BY_GRANULARITY[getSavedGranularity(granularity)]
}

export function resolveDashboardTimeRange(filters?: DashboardFilters): {
  start_timestamp: number
  end_timestamp: number
} {
  const presetDays = filters?.presetDays
  const { start, end } = getRollingDateRange(
    presetDays ?? getDefaultDays(filters?.time_granularity)
  )
  return normalizeDashboardHourRange({
    start_timestamp: dateToUnixTimestamp(
      presetDays === undefined ? (filters?.start_timestamp ?? start) : start
    ),
    end_timestamp: dateToUnixTimestamp(
      presetDays === undefined ? (filters?.end_timestamp ?? end) : end
    ),
  })
}

export function buildDefaultDashboardFilters(
  preferences: DashboardChartPreferences = getSavedChartPreferences()
): DashboardFilters {
  const { start, end } = getRollingDateRange(preferences.defaultTimeRangeDays)
  return {
    ...EMPTY_DASHBOARD_FILTERS,
    presetDays: preferences.defaultTimeRangeDays,
    start_timestamp: start,
    end_timestamp: end,
    time_granularity: preferences.defaultTimeGranularity,
  }
}

export function buildQueryParams(
  timeRange: { start_timestamp: number; end_timestamp: number },
  filters?: { time_granularity?: TimeGranularity; username?: string }
): {
  start_timestamp: number
  end_timestamp: number
  default_time: string
  username?: string
} {
  return {
    ...timeRange,
    default_time: 'hour',
    ...(filters?.username && { username: filters.username }),
  }
}

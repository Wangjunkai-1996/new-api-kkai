import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  DASHBOARD_CHART_PREFERENCES_STORAGE_KEY,
  DEFAULT_DASHBOARD_CHART_PREFERENCES,
  OVERVIEW_CHART_PREFERENCES,
  TIME_GRANULARITY_STORAGE_KEY,
  USER_TIME_GRANULARITY_STORAGE_KEY,
} from '../../constants'
import {
  buildDefaultDashboardFilters,
  getSavedChartPreferences,
  getSavedUserGranularity,
  resolveDashboardTimeRange,
  saveChartPreferences,
  saveUserGranularity,
} from '../filters'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  localStorage.clear()
})

describe('dashboard time preferences', () => {
  it('builds independent rolling ranges for overview and models', () => {
    vi.useFakeTimers()
    const end = new Date('2025-01-04T17:12:34Z')
    vi.setSystemTime(end)
    expect(buildDefaultDashboardFilters()).toMatchObject({
      start_timestamp: new Date('2025-01-03T17:12:34Z'),
      end_timestamp: end,
      time_granularity: 'hour',
    })
    saveChartPreferences({
      ...DEFAULT_DASHBOARD_CHART_PREFERENCES,
      defaultTimeRangeDays: 30,
      defaultTimeGranularity: 'month',
    })
    expect(
      buildDefaultDashboardFilters(OVERVIEW_CHART_PREFERENCES)
    ).toMatchObject({
      start_timestamp: new Date('2024-12-28T17:12:34Z'),
      end_timestamp: end,
      time_granularity: 'day',
    })
    expect(buildDefaultDashboardFilters()).toMatchObject({
      start_timestamp: new Date('2024-12-05T17:12:34Z'),
      end_timestamp: end,
      time_granularity: 'month',
    })
  })

  it('reads the former 29-day range as 30 days and preserves saved choices', () => {
    const raw = JSON.stringify({
      ...DEFAULT_DASHBOARD_CHART_PREFERENCES,
      defaultTimeRangeDays: 29,
      defaultTimeGranularity: 'week',
      modelAnalyticsChart: 'top',
    })
    localStorage.setItem(DASHBOARD_CHART_PREFERENCES_STORAGE_KEY, raw)
    localStorage.setItem(TIME_GRANULARITY_STORAGE_KEY, 'day')
    expect(getSavedChartPreferences()).toMatchObject({
      defaultTimeRangeDays: 30,
      defaultTimeGranularity: 'week',
      modelAnalyticsChart: 'top',
    })
    expect(localStorage.getItem(DASHBOARD_CHART_PREFERENCES_STORAGE_KEY)).toBe(
      raw
    )
  })

  it('falls back to the legacy granularity and saves user analytics independently', () => {
    localStorage.setItem(TIME_GRANULARITY_STORAGE_KEY, 'week')
    localStorage.setItem(DASHBOARD_CHART_PREFERENCES_STORAGE_KEY, '{')
    localStorage.setItem(USER_TIME_GRANULARITY_STORAGE_KEY, 'unknown')
    expect(getSavedUserGranularity()).toBe('week')
    saveUserGranularity('month')
    expect(getSavedUserGranularity()).toBe('month')
    expect(getSavedChartPreferences().defaultTimeGranularity).toBe('week')
    expect(localStorage.getItem(DASHBOARD_CHART_PREFERENCES_STORAGE_KEY)).toBe(
      '{'
    )
    expect(localStorage.getItem(TIME_GRANULARITY_STORAGE_KEY)).toBe('week')
  })

  it('uses defaults and leaves the page usable when storage throws', () => {
    const denied = () => {
      throw new Error('Storage access denied')
    }
    vi.stubGlobal('localStorage', { getItem: denied, setItem: denied })
    expect(getSavedUserGranularity()).toBe('hour')
    expect(getSavedChartPreferences()).toEqual(
      DEFAULT_DASHBOARD_CHART_PREFERENCES
    )
    expect(() => saveUserGranularity('week')).not.toThrow()
    expect(() => saveChartPreferences(OVERVIEW_CHART_PREFERENCES)).not.toThrow()
  })

  it('recomputes rolling presets and aligns custom ranges to hour buckets', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-18T10:01:02+08:00'))
    expect(
      resolveDashboardTimeRange({ presetDays: 7, time_granularity: 'day' })
    ).toEqual({
      start_timestamp: Date.parse('2026-11-11T10:00:00+08:00') / 1000,
      end_timestamp: Date.parse('2026-11-18T10:00:00+08:00') / 1000,
    })
    expect(
      resolveDashboardTimeRange({
        start_timestamp: new Date('2026-11-18T00:02:03+08:00'),
        end_timestamp: new Date('2026-11-18T01:59:59+08:00'),
        time_granularity: 'hour',
      })
    ).toEqual({
      start_timestamp: Date.parse('2026-11-18T00:00:00+08:00') / 1000,
      end_timestamp: Date.parse('2026-11-18T01:00:00+08:00') / 1000,
    })
  })
})

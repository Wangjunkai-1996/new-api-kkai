import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import {
  DASHBOARD_CHART_PREFERENCES_STORAGE_KEY,
  OVERVIEW_CHART_PREFERENCES,
} from '../../constants'
import {
  buildDefaultDashboardFilters,
  getSavedChartPreferences,
  saveChartPreferences,
} from '../../lib/filters'
import type { DashboardFilters } from '../../types'
import { ModelsChartPreferences } from './models-chart-preferences'
import { ModelsFilter } from './models-filter-dialog'

afterEach(() => localStorage.clear())

const preferences = {
  ...OVERVIEW_CHART_PREFERENCES,
  defaultTimeRangeDays: 14,
  defaultTimeGranularity: 'week',
} as const

function FilterFixture(props: {
  onApply: (filters: DashboardFilters) => void
  allowUsernameFilter?: boolean
}) {
  const [filters, setFilters] = useState(() =>
    buildDefaultDashboardFilters(preferences)
  )
  return (
    <ModelsFilter
      currentFilters={filters}
      preferences={preferences}
      allowUsernameFilter={props.allowUsernameFilter ?? false}
      onFilterChange={(next) => {
        props.onApply(next)
        setFilters(next)
      }}
      onReset={() => setFilters(buildDefaultDashboardFilters(preferences))}
    />
  )
}

it('restores the applied preset, manual granularity and username, and resets to preferences', async () => {
  const user = userEvent.setup()
  const onApply = vi.fn()
  render(<FilterFixture allowUsernameFilter onApply={onApply} />)
  await user.click(screen.getByRole('button', { name: 'Filter' }))
  expect(
    screen.getByRole('button', { name: 'Last 14 days', pressed: true })
  ).toBeVisible()
  const granularity = screen.getByRole('combobox', { name: 'Time Granularity' })
  expect(granularity).toHaveTextContent('Weekly')
  expect(screen.queryByLabelText('Start Time')).not.toBeInTheDocument()
  expect(screen.queryByText(/UTC\+8/)).not.toBeInTheDocument()
  for (const [preset, suggested] of [
    ['Last 24 hours', 'Hourly'],
    ['Last 7 days', 'Daily'],
    ['Last 14 days', 'Daily'],
    ['Last 30 days', 'Daily'],
  ]) {
    await user.click(screen.getByRole('button', { name: preset }))
    expect(granularity).toHaveTextContent(suggested)
  }
  await user.click(granularity)
  await user.click(await screen.findByRole('option', { name: 'Monthly' }))
  await user.type(screen.getByLabelText('Username'), '  alice  ')
  await user.click(screen.getByRole('button', { name: 'Apply Filters' }))
  expect(onApply).toHaveBeenCalledExactlyOnceWith(
    expect.objectContaining({
      presetDays: 30,
      time_granularity: 'month',
      username: 'alice',
    })
  )
  await user.click(await screen.findByRole('button', { name: 'Filter' }))
  expect(
    screen.getByRole('button', { name: 'Last 30 days', pressed: true })
  ).toBeVisible()
  expect(
    screen.getByRole('combobox', { name: 'Time Granularity' })
  ).toHaveTextContent('Monthly')
  expect(screen.getByLabelText('Username')).toHaveValue('alice')
  await user.click(screen.getByRole('button', { name: 'Reset' }))
  await user.click(await screen.findByRole('button', { name: 'Filter' }))
  expect(
    screen.getByRole('button', { name: 'Last 14 days', pressed: true })
  ).toBeVisible()
  expect(
    screen.getByRole('combobox', { name: 'Time Granularity' })
  ).toHaveTextContent('Weekly')
  expect(screen.getByLabelText('Username')).toHaveValue('')
})

it('validates both UTC+8 endpoints and preserves an exactly seven-day custom range', async () => {
  const user = userEvent.setup()
  const onApply = vi.fn()
  render(<FilterFixture onApply={onApply} />)
  await user.click(screen.getByRole('button', { name: 'Filter' }))
  await user.click(screen.getByRole('button', { name: 'Last 30 days' }))
  await user.tab()
  expect(screen.getByRole('button', { name: 'Custom' })).toHaveFocus()
  await user.keyboard('{Enter}')
  const start = screen.getByLabelText('Start Time')
  const end = screen.getByLabelText('End Time')
  const apply = screen.getByRole('button', { name: 'Apply Filters' })
  await user.tab()
  expect(start).toHaveFocus()
  expect(start).toHaveAccessibleDescription(/UTC\+8/)
  fireEvent.input(start, { target: { value: '' } })
  expect(screen.getByRole('alert')).toHaveTextContent('Select start time')
  expect(apply).toBeDisabled()
  fireEvent.input(start, { target: { value: '2026-02-01T08:02:03' } })
  fireEvent.input(end, { target: { value: '' } })
  expect(screen.getByRole('alert')).toHaveTextContent('Select end time')
  expect(apply).toBeDisabled()
  fireEvent.input(end, { target: { value: '2026-02-01T08:02:02' } })
  expect(end).toHaveAttribute('aria-invalid', 'true')
  expect(screen.getByRole('alert')).toHaveTextContent(
    'End time must be after start time'
  )
  await user.click(apply)
  expect(onApply).not.toHaveBeenCalled()
  fireEvent.input(end, { target: { value: '2026-02-01T08:02:03' } })
  expect(apply).toBeEnabled()
  fireEvent.input(end, { target: { value: '2026-02-08T08:02:03' } })
  await user.click(apply)
  expect(onApply).toHaveBeenCalledExactlyOnceWith({
    start_timestamp: new Date('2026-02-01T00:02:03Z'),
    end_timestamp: new Date('2026-02-08T00:02:03Z'),
    time_granularity: 'day',
  })
  await user.click(await screen.findByRole('button', { name: 'Filter' }))
  expect(
    screen.getByRole('button', { name: 'Custom', pressed: true })
  ).toBeVisible()
  expect(screen.getByLabelText('Start Time')).toHaveProperty(
    'valueAsNumber',
    Date.UTC(2026, 1, 1, 8, 2, 3)
  )
  expect(screen.getByLabelText('End Time')).toHaveProperty(
    'valueAsNumber',
    Date.UTC(2026, 1, 8, 8, 2, 3)
  )
  await user.click(screen.getByRole('button', { name: 'Last 7 days' }))
  await user.keyboard('{Escape}')
  await user.click(await screen.findByRole('button', { name: 'Filter' }))
  expect(
    screen.getByRole('button', { name: 'Custom', pressed: true })
  ).toBeVisible()
})

it('drops a hidden username and reads new parent selections on reopen', async () => {
  const user = userEvent.setup()
  const props = {
    preferences,
    currentFilters: {
      ...buildDefaultDashboardFilters(preferences),
      username: 'admin-only',
    },
    onFilterChange: vi.fn(),
    onReset: vi.fn(),
    allowUsernameFilter: false,
  }
  const view = render(<ModelsFilter {...props} />)
  await user.click(screen.getByRole('button', { name: 'Filter' }))
  expect(screen.queryByLabelText('Username')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Apply Filters' }))
  expect(props.onFilterChange.mock.calls[0][0]).not.toHaveProperty('username')
  view.rerender(
    <ModelsFilter
      {...props}
      currentFilters={buildDefaultDashboardFilters(OVERVIEW_CHART_PREFERENCES)}
    />
  )
  await user.click(await screen.findByRole('button', { name: 'Filter' }))
  expect(
    screen.getByRole('button', { name: 'Last 7 days', pressed: true })
  ).toBeVisible()
})

it('reads legacy preferences without writing and saves only an explicitly changed choice', async () => {
  const user = userEvent.setup()
  const previous = {
    defaultTimeRangeDays: 29,
    defaultTimeGranularity: 'week',
    consumptionDistributionChart: 'area',
    modelAnalyticsChart: 'top',
  }
  const raw = JSON.stringify(previous)
  localStorage.setItem(DASHBOARD_CHART_PREFERENCES_STORAGE_KEY, raw)
  const dialog = () => (
    <ModelsChartPreferences
      preferences={getSavedChartPreferences()}
      onPreferencesChange={saveChartPreferences}
    />
  )
  const view = render(dialog())
  await user.click(screen.getByRole('button', { name: 'Preferences' }))
  expect(screen.getByLabelText('Default range')).toHaveTextContent(
    'Last 30 days'
  )
  expect(screen.getByLabelText('Default time granularity')).toHaveTextContent(
    'Weekly'
  )
  expect(screen.getByLabelText('Default consumption chart')).toHaveTextContent(
    'Area Chart'
  )
  expect(screen.getByLabelText('Default model call chart')).toHaveTextContent(
    'Call Count Ranking'
  )
  await user.click(screen.getByLabelText('Default time granularity'))
  await user.click(await screen.findByRole('option', { name: 'Monthly' }))
  await user.keyboard('{Escape}')
  expect(localStorage.getItem(DASHBOARD_CHART_PREFERENCES_STORAGE_KEY)).toBe(
    raw
  )
  await user.click(await screen.findByRole('button', { name: 'Preferences' }))
  expect(screen.getByLabelText('Default time granularity')).toHaveTextContent(
    'Weekly'
  )
  await user.click(screen.getByLabelText('Default time granularity'))
  await user.click(await screen.findByRole('option', { name: 'Monthly' }))
  await user.click(screen.getByRole('button', { name: 'Save Preferences' }))
  expect(getSavedChartPreferences()).toEqual({
    ...previous,
    defaultTimeRangeDays: 30,
    defaultTimeGranularity: 'month',
  })
  view.unmount()
  render(dialog())
  await user.click(screen.getByRole('button', { name: 'Preferences' }))
  expect(screen.getByLabelText('Default time granularity')).toHaveTextContent(
    'Monthly'
  )
})

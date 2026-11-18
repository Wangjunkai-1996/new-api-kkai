import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { PropsWithChildren } from 'react'
import { afterEach, assert, beforeEach, expect, it, vi } from 'vitest'

import { getUserQuotaDates } from '@/features/dashboard/api'
import { useAuthStore } from '@/stores/auth-store'

import { FlowCharts } from './components/flow/flow-charts'
import { ConsumptionDistributionChart } from './components/models/consumption-distribution-chart'
import { LogStatCards } from './components/models/log-stat-cards'
import { ModelCharts } from './components/models/model-charts'
import { ModelsChartPreferences } from './components/models/models-chart-preferences'
import { ModelsFilter } from './components/models/models-filter-dialog'
import { OverviewDashboard } from './components/overview/overview-dashboard'
import { UserCharts } from './components/users/user-charts'
import { Dashboard } from './index'
import { saveUserGranularity } from './lib'

const params = vi.hoisted(() => ({ section: 'models' }))
vi.mock('@/features/dashboard/api', () => ({
  getUserQuotaDates: vi.fn(),
}))
vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  getRouteApi: () => ({ useParams: () => params }),
  useNavigate: () => vi.fn(),
}))
vi.mock('@/components/layout', () => {
  const Slot = ({ children }: PropsWithChildren) => children
  return {
    SectionPageLayout: Object.assign(Slot, { Title: Slot, Content: Slot }),
  }
})
vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: { tenant_id: 0, hostname: 'test.example' } }),
}))
vi.mock('./components/models/models-filter-dialog', () => ({
  ModelsFilter: vi.fn(() => null),
}))
vi.mock('./components/models/models-chart-preferences', () => ({
  ModelsChartPreferences: vi.fn(() => null),
}))
vi.mock('./components/models/log-stat-cards', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('./components/models/log-stat-cards')>()
  return {
    ...actual,
    LogStatCards: vi.fn(actual.LogStatCards),
  }
})
vi.mock('./components/models/model-charts', () => ({
  ModelCharts: vi.fn(() => null),
}))
vi.mock('./components/models/consumption-distribution-chart', () => ({
  ConsumptionDistributionChart: vi.fn(() => null),
}))
vi.mock('./components/models/performance-overview', () => ({
  PerformanceOverview: () => null,
}))
vi.mock('./components/flow/flow-charts', () => ({
  FlowCharts: vi.fn(() => null),
}))
vi.mock('./components/users/user-charts', () => ({
  UserCharts: vi.fn(() => null),
}))
vi.mock('./components/overview/overview-dashboard', () => ({
  OverviewDashboard: vi.fn(() => null),
}))

afterEach(() => {
  cleanup()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  localStorage.clear()
  params.section = 'models'
})

beforeEach(() => {
  vi.mocked(getUserQuotaDates).mockReset()
  vi.mocked(getUserQuotaDates).mockResolvedValue({
    success: true,
    data: [],
  })
})

function setUser(id: number, role: number) {
  act(() =>
    useAuthStore.getState().auth.setUser({ id, role, username: `user-${id}` })
  )
}

it('preserves separate model, flow and user selections across section changes', async () => {
  setUser(1, 10)
  saveUserGranularity('week')
  params.section = 'models'
  const view = render(<Dashboard />)
  await waitFor(() => {
    expect(ModelCharts).toHaveBeenCalled()
    expect(ConsumptionDistributionChart).toHaveBeenCalled()
    expect(LogStatCards).toHaveBeenCalled()
  })
  const filter = () => {
    const props = vi.mocked(ModelsFilter).mock.lastCall?.[0]
    assert.isDefined(props)
    return props
  }
  const range = { start_timestamp: 3600, end_timestamp: 10800 }
  const selection = {
    start_timestamp: new Date(3607123),
    end_timestamp: new Date(10831999),
    username: 'model-user',
  }
  act(() =>
    filter().onFilterChange({ start_timestamp: selection.start_timestamp })
  )
  const end = vi.mocked(LogStatCards).mock.lastCall?.[0].filters?.end_timestamp
  assert.isDefined(end)
  expect(vi.mocked(ModelCharts).mock.lastCall?.[0].timeRange).toEqual({
    start_timestamp: range.start_timestamp,
    end_timestamp: end.getTime() / 1000,
  })
  act(() => filter().onFilterChange(selection))
  expect(vi.mocked(ModelCharts).mock.lastCall?.[0].timeRange).toEqual(range)
  expect(
    vi.mocked(ConsumptionDistributionChart).mock.lastCall?.[0].timeRange
  ).toEqual(range)

  params.section = 'flow'
  view.rerender(<Dashboard />)
  await waitFor(() => expect(FlowCharts).toHaveBeenCalled())
  expect(filter().currentFilters.username).toBeFalsy()
  act(() => filter().onFilterChange({ ...selection, username: 'flow-user' }))
  expect(vi.mocked(FlowCharts).mock.lastCall?.[0].filters?.username).toBe(
    'flow-user'
  )
  params.section = 'models'
  view.rerender(<Dashboard />)
  expect(filter().currentFilters).toEqual(selection)
  const preferences = vi.mocked(ModelsChartPreferences).mock.lastCall?.[0]
  assert.isDefined(preferences)
  act(() =>
    preferences.onPreferencesChange({
      ...preferences.preferences,
      defaultTimeRangeDays: 7,
    })
  )
  params.section = 'flow'
  view.rerender(<Dashboard />)
  expect(filter().currentFilters.username).toBe('flow-user')
  act(() => filter().onReset())
  expect(vi.mocked(FlowCharts).mock.lastCall?.[0].filters).toEqual(
    filter().currentFilters
  )
  expect(filter().currentFilters.username).toBeFalsy()
  params.section = 'users'
  view.rerender(<Dashboard />)
  await waitFor(() => expect(UserCharts).toHaveBeenCalled())
  expect(vi.mocked(UserCharts).mock.lastCall?.[0].filters.timeGranularity).toBe(
    'week'
  )
})

it.each([
  [2, 10],
  [1, 1],
])(
  'discards previous data and late updates on switch to id %i role %i',
  async (id, role) => {
    setUser(1, 10)
    params.section = 'models'
    render(<Dashboard />)
    await waitFor(() => expect(LogStatCards).toHaveBeenCalled())
    const oldUpdate = vi.mocked(LogStatCards).mock.lastCall?.[0].onDataUpdate
    assert.isDefined(oldUpdate)
    const rows = [{ created_at: 3600, model_name: 'private-model' }]
    act(() => oldUpdate(rows, false))
    expect(vi.mocked(ModelCharts).mock.lastCall?.[0].data).toEqual(rows)
    vi.mocked(ModelCharts).mockClear()
    setUser(id, role)
    act(() => oldUpdate(rows, false))
    const renders = vi.mocked(ModelCharts).mock.calls
    expect(renders.length).toBeGreaterThan(0)
    expect(renders.every(([props]) => props.data.length === 0)).toBe(true)
  }
)

it('renders the independent overview for a non-admin opening the users URL', () => {
  params.section = 'users'
  setUser(1, 1)
  render(<Dashboard />)
  expect(OverviewDashboard).toHaveBeenCalled()
  expect(UserCharts).not.toHaveBeenCalled()
})

it('shows a shared error state and retries the actual model stat-card request', async () => {
  const user = userEvent.setup()
  const rows = [
    {
      created_at: 3600,
      model_name: 'private-model',
      count: 2,
      quota: 3,
      token_used: 4,
    },
  ]
  vi.mocked(getUserQuotaDates)
    .mockRejectedValueOnce(new Error('request failed'))
    .mockResolvedValueOnce({ success: true, data: rows })
  setUser(1, 10)

  render(<Dashboard />)

  await waitFor(() => expect(getUserQuotaDates).toHaveBeenCalled())
  await waitFor(() =>
    expect(screen.getByText('Failed to load usage statistics')).toBeVisible()
  )
  expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
  expect(vi.mocked(getUserQuotaDates).mock.calls[0]?.[0]).toEqual(
    expect.objectContaining({ default_time: 'hour' })
  )

  await user.click(screen.getByRole('button', { name: 'Retry' }))

  await waitFor(() =>
    expect(
      screen.getByRole('group', { name: 'Total Count' })
    ).toHaveTextContent('2')
  )
  expect(
    screen.queryByRole('heading', { name: 'Failed to load usage statistics' })
  ).not.toBeInTheDocument()
  expect(getUserQuotaDates).toHaveBeenCalledTimes(2)
  expect(vi.mocked(ModelCharts).mock.lastCall?.[0].data).toEqual(rows)
})

it('treats a successful usage response with null data as an error', async () => {
  vi.mocked(getUserQuotaDates).mockResolvedValueOnce({
    success: true,
    data: null,
  } as never)
  setUser(1, 10)

  render(<Dashboard />)

  expect(
    await screen.findByText('Failed to load usage statistics')
  ).toBeVisible()
  expect(screen.getByRole('group', { name: 'Total Count' })).toHaveTextContent(
    '--'
  )
})

it('keeps empty results valid and does not refetch when only granularity changes', async () => {
  setUser(1, 10)
  render(<Dashboard />)

  await waitFor(() => expect(getUserQuotaDates).toHaveBeenCalledTimes(1))
  await waitFor(() =>
    expect(
      screen.getByRole('group', { name: 'Total Count' })
    ).toHaveTextContent('0')
  )
  expect(
    screen.queryByRole('heading', { name: 'Failed to load usage statistics' })
  ).not.toBeInTheDocument()

  const filter = () => {
    const props = vi.mocked(ModelsFilter).mock.lastCall?.[0]
    assert.isDefined(props)
    return props
  }
  act(() =>
    filter().onFilterChange({
      ...filter().currentFilters,
      time_granularity: 'month',
    })
  )

  await waitFor(() =>
    expect(vi.mocked(ModelCharts).mock.lastCall?.[0].timeGranularity).toBe(
      'month'
    )
  )
  expect(getUserQuotaDates).toHaveBeenCalledTimes(1)
})

it('refetches the same hourly data for range and admin username changes', async () => {
  setUser(1, 10)
  render(<Dashboard />)
  await waitFor(() => expect(getUserQuotaDates).toHaveBeenCalledTimes(1))

  const filter = () => {
    const props = vi.mocked(ModelsFilter).mock.lastCall?.[0]
    assert.isDefined(props)
    return props
  }
  const currentFilters = filter().currentFilters
  act(() =>
    filter().onFilterChange({
      ...currentFilters,
      username: 'admin-only',
      start_timestamp: new Date('2026-02-01T00:00:00Z'),
      end_timestamp: new Date('2026-02-02T00:00:00Z'),
      presetDays: undefined,
      time_granularity: 'day',
    })
  )

  await waitFor(() => expect(getUserQuotaDates).toHaveBeenCalledTimes(2))
  expect(vi.mocked(getUserQuotaDates).mock.calls[1]?.[0]).toEqual(
    expect.objectContaining({ default_time: 'hour', username: 'admin-only' })
  )
  expect(vi.mocked(getUserQuotaDates).mock.calls[1]?.[1]).toBe(true)

  act(() =>
    filter().onFilterChange({
      ...filter().currentFilters,
      username: undefined,
    })
  )
  await waitFor(() => expect(getUserQuotaDates).toHaveBeenCalledTimes(3))
  expect(vi.mocked(getUserQuotaDates).mock.calls[2]?.[0]).not.toHaveProperty(
    'username'
  )
})

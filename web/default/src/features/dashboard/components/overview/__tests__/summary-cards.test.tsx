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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { getUserQuotaDates } from '@/features/dashboard/api'
import type { QuotaDataItem } from '@/features/dashboard/types'
import { useAuthStore } from '@/stores/auth-store'

import { SummaryCards } from '../summary-cards'

vi.mock('@tanstack/react-router', () => ({
  Link: (props: ComponentProps<'a'> & { to: string }) => {
    const { to, ...anchorProps } = props
    return <a {...anchorProps} href={to} />
  },
}))

vi.mock('@/features/dashboard/api', () => ({
  getUserQuotaDates: vi.fn(),
}))

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({
    status: { display_in_currency: true },
    loading: false,
  }),
}))

vi.mock('@/lib/format', () => ({
  formatNumber: (value: number) => String(value),
  formatQuota: (value: number) => `quota:${value}`,
}))

let client: QueryClient

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-24T10:30:00+08:00'))
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'dashboard-user',
    role: 1,
    quota: 15000000,
    used_quota: 20000000,
    request_count: 42,
  })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  vi.mocked(getUserQuotaDates).mockResolvedValue({ success: true, data: [] })
})

afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  vi.restoreAllMocks()
  vi.useRealTimers()
})

async function renderSummary() {
  render(
    <QueryClientProvider client={client}>
      <SummaryCards />
    </QueryClientProvider>
  )
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1)
  })
}

describe('overview daily usage', () => {
  it('counts Beijing midnight and the current hour while retaining recent usage for runway', async () => {
    const now = Math.floor(Date.now() / 1000)
    const data: QuotaDataItem[] = [
      {
        created_at: Date.parse('2026-10-23T23:00:00+08:00') / 1000,
        quota: 6000000,
      },
      {
        created_at: Date.parse('2026-10-24T00:00:00+08:00') / 1000,
        quota: 1000000,
      },
      {
        created_at: Date.parse('2026-10-24T10:00:00+08:00') / 1000,
        quota: 500000,
      },
    ]
    vi.mocked(getUserQuotaDates).mockResolvedValue({ success: true, data })

    await renderSummary()

    expect(screen.getAllByText("Today's usage")).toHaveLength(2)
    expect(screen.getAllByText('quota:1500000')).toHaveLength(2)
    expect(screen.queryByText('quota:7500000')).not.toBeInTheDocument()
    expect(screen.getByText('~2 days')).toBeVisible()
    expect(screen.getByText('quota:20000000')).toBeVisible()
    expect(screen.getByText('Cumulative requests')).toBeVisible()
    expect(screen.getByText('42')).toBeVisible()
    expect(getUserQuotaDates).toHaveBeenCalledWith({
      start_timestamp: now - 24 * 3600,
      end_timestamp: now,
      default_time: 'hour',
    })
  })

  it('switches to a new Beijing day without reusing yesterday’s displayed total', async () => {
    vi.setSystemTime(new Date('2026-10-24T23:59:59+08:00'))
    const yesterday = {
      created_at: Date.parse('2026-10-24T23:00:00+08:00') / 1000,
      quota: 4000000,
    }
    vi.mocked(getUserQuotaDates).mockResolvedValue({
      success: true,
      data: [yesterday],
    })
    await renderSummary()
    expect(screen.getAllByText('quota:4000000')).toHaveLength(2)

    vi.mocked(getUserQuotaDates).mockResolvedValue({
      success: true,
      data: [
        yesterday,
        {
          created_at: Date.parse('2026-10-25T00:00:00+08:00') / 1000,
          quota: 500000,
        },
      ],
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1001)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    expect(screen.getAllByText('quota:500000')).toHaveLength(2)
    expect(screen.queryByText('quota:4000000')).not.toBeInTheDocument()
  })

  it('moves the query window and refreshes today’s usage each visible minute', async () => {
    const initialTime = Math.floor(Date.now() / 1000)
    const created_at = Date.parse('2026-10-24T10:00:00+08:00') / 1000
    vi.mocked(getUserQuotaDates).mockResolvedValue({
      success: true,
      data: [{ created_at, quota: 500000 }],
    })
    await renderSummary()
    expect(screen.getAllByText('quota:500000')).toHaveLength(2)

    vi.mocked(getUserQuotaDates).mockResolvedValue({
      success: true,
      data: [{ created_at, quota: 1000000 }],
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60000)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    expect(getUserQuotaDates).toHaveBeenLastCalledWith({
      start_timestamp: initialTime + 60 - 24 * 3600,
      end_timestamp: initialTime + 60,
      default_time: 'hour',
    })
    expect(screen.getAllByText('quota:1000000')).toHaveLength(2)
    expect(screen.queryByText('quota:500000')).not.toBeInTheDocument()
  })

  it('rechecks the Beijing date when the page becomes visible after midnight', async () => {
    vi.setSystemTime(new Date('2026-10-24T23:30:00+08:00'))
    vi.mocked(getUserQuotaDates).mockResolvedValue({
      success: true,
      data: [
        {
          created_at: Date.parse('2026-10-24T23:00:00+08:00') / 1000,
          quota: 4000000,
        },
      ],
    })
    await renderSummary()
    expect(screen.getAllByText('quota:4000000')).toHaveLength(2)

    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    fireEvent(document, new Event('visibilitychange'))
    vi.setSystemTime(new Date('2026-10-25T00:20:00+08:00'))
    vi.mocked(getUserQuotaDates).mockResolvedValue({ success: true, data: [] })
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    await act(async () => {
      fireEvent(document, new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(1)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })

    expect(screen.getAllByText('quota:0')).toHaveLength(2)
    expect(screen.queryByText('quota:4000000')).not.toBeInTheDocument()
  })

  it('fetches the next account’s usage instead of showing a fresh cached total', async () => {
    vi.mocked(getUserQuotaDates).mockResolvedValueOnce({
      success: true,
      data: [{ created_at: Date.now() / 1000 - 1800, quota: 1000000 }],
    })
    await renderSummary()
    expect(screen.getAllByText('quota:1000000')).toHaveLength(2)

    vi.mocked(getUserQuotaDates).mockResolvedValue({
      success: true,
      data: [{ created_at: Date.now() / 1000 - 1800, quota: 3000000 }],
    })
    await act(async () => {
      useAuthStore.getState().auth.setUser({
        id: 2,
        username: 'second-user',
        role: 1,
        quota: 15000000,
        used_quota: 20000000,
        request_count: 42,
      })
      await vi.advanceTimersByTimeAsync(1)
    })

    expect(screen.getAllByText('quota:3000000')).toHaveLength(2)
    expect(screen.queryByText('quota:1000000')).not.toBeInTheDocument()
    expect(getUserQuotaDates).toHaveBeenCalledTimes(2)
  })

  it('shows unavailable statistics rather than zero usage after a failed response', async () => {
    vi.mocked(getUserQuotaDates).mockResolvedValue({ success: false, data: [] })

    await renderSummary()

    expect(
      screen.getAllByText('Failed to load usage statistics').length
    ).toBeGreaterThan(0)
    expect(screen.queryByText('quota:0')).not.toBeInTheDocument()
    expect(screen.queryByText('No recent usage')).not.toBeInTheDocument()
    expect(screen.getAllByText('--').length).toBeGreaterThan(0)
  })

  it('shows zero usage only after a successful empty response', async () => {
    await renderSummary()

    expect(screen.getAllByText('quota:0')).toHaveLength(2)
    expect(screen.getByText('No recent usage')).toBeVisible()
    expect(
      screen.queryByText('Failed to load usage statistics')
    ).not.toBeInTheDocument()
  })
})

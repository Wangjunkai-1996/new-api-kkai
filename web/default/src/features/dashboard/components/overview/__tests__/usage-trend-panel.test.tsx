/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { getUserQuotaDates } from '@/features/dashboard/api'
import { formatQuota } from '@/lib/format'

import { UsageTrendPanel } from '../usage-trend-panel'

vi.mock('@/features/dashboard/api', () => ({ getUserQuotaDates: vi.fn() }))

vi.mock('@visactor/react-vchart', () => ({
  VChart: (props: {
    spec: { data: { values: { Count?: number; value?: number }[] }[] }
  }) => (
    <span>
      {props.spec.data[0].values.reduce(
        (total, row) => total + (row.Count ?? row.value ?? 0),
        0
      )}
    </span>
  ),
}))

const now = Date.parse('2026-11-18T10:00:00+08:00') / 1000
const day = 24 * 3600
const empty = { success: true, data: [] }
const request = vi.mocked(getUserQuotaDates)
let client: QueryClient

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(now * 1000)
  request.mockReset()
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})

afterEach(() => {
  cleanup()
  client.clear()
  vi.useRealTimers()
})

function renderPanel() {
  return render(
    <QueryClientProvider client={client}>
      <UsageTrendPanel
        userId={1}
        role={1}
        host='example.test'
        isAdmin={false}
      />
    </QueryClientProvider>
  )
}

function pendingResponse() {
  let resolve!: (value: Awaited<ReturnType<typeof getUserQuotaDates>>) => void
  const promise = new Promise<Awaited<ReturnType<typeof getUserQuotaDates>>>(
    (done) => {
      resolve = done
    }
  )
  return { promise, resolve }
}

it('shows loading, then an error with retry, then the empty state', async () => {
  const first = pendingResponse()
  request.mockReturnValueOnce(first.promise).mockResolvedValueOnce(empty)
  const user = userEvent.setup()
  renderPanel()

  expect(screen.getByRole('status', { name: 'Loading' })).toBeVisible()
  expect(screen.getByRole('button', { name: 'Refresh' })).toBeDisabled()
  expect(screen.queryByText('No data available')).not.toBeInTheDocument()
  first.resolve({ success: false, data: [] })
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Failed to load usage statistics'
  )
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByText('No data available')).toBeVisible()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  expect(screen.queryByRole('img')).not.toBeInTheDocument()
  expect(request).toHaveBeenCalledTimes(2)
})

it('keeps valid totals and both charts during a failed refresh and replaces them on retry', async () => {
  const refresh = pendingResponse()
  const row = {
    created_at: now,
    model_name: 'model-a',
    count: 4,
    token_used: 21,
    quota: 500000,
  }
  request
    .mockResolvedValueOnce({
      success: true,
      data: [
        row,
        ...[Number.NaN, 0, now - 0.5, now + 1, now - 7 * day - 1].map(
          (created_at) => ({ ...row, created_at })
        ),
      ],
    })
    .mockReturnValueOnce(refresh.promise)
    .mockResolvedValueOnce({ success: true, data: [{ ...row, count: 9 }] })
  const user = userEvent.setup()
  renderPanel()

  const trend = await screen.findByRole('img', { name: 'Call Trend' })
  const distribution = screen.getByRole('img', {
    name: 'Call Count Distribution',
  })
  expect(trend).toHaveTextContent(/^4$/)
  expect(distribution).toHaveTextContent(/^4$/)
  expect(
    screen.getByText('Request Count').nextElementSibling
  ).toHaveTextContent(/^4$/)
  expect(screen.getByText('Tokens').nextElementSibling).toHaveTextContent(
    /^21$/
  )
  expect(screen.getByText('Quota').nextElementSibling).toHaveTextContent(
    formatQuota(row.quota)
  )

  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  expect(
    await screen.findByRole('button', { name: 'Refreshing...' })
  ).toBeDisabled()
  expect(trend).toBeVisible()
  refresh.resolve({ success: false, data: [] })
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Refresh failed. Showing the latest available data.'
  )
  expect(distribution).toHaveTextContent(/^4$/)
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() => expect(trend).toHaveTextContent(/^9$/))
  expect(distribution).toHaveTextContent(/^9$/)
  expect(
    screen.getByText('Request Count').nextElementSibling
  ).toHaveTextContent(/^9$/)
})

it('rolls presets on every refresh, changes granularity locally, and pins an exact seven-day custom range', async () => {
  request.mockResolvedValue(empty)
  const user = userEvent.setup()
  const filter = () =>
    user.click(screen.getByRole('button', { name: 'Filter' }))
  const apply = () =>
    user.click(screen.getByRole('button', { name: 'Apply Filters' }))
  renderPanel()
  await screen.findByText('No data available')
  expect(screen.getByText('Last 7 days · Daily')).toBeVisible()
  expect(request).toHaveBeenLastCalledWith(
    {
      start_timestamp: now - 7 * day,
      end_timestamp: now,
      default_time: 'hour',
    },
    false,
    expect.any(AbortSignal)
  )

  await filter()
  expect(
    screen.queryByRole('textbox', { name: 'Username' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Last 30 days' }))
  await apply()
  await screen.findByText('Last 30 days · Daily')
  await filter()
  await user.click(screen.getByRole('combobox', { name: 'Time Granularity' }))
  await user.click(await screen.findByRole('option', { name: 'Monthly' }))
  await apply()
  expect(await screen.findByText('Last 30 days · Monthly')).toBeVisible()
  expect(request).toHaveBeenCalledTimes(2)

  for (const offset of [60, 120]) {
    vi.setSystemTime((now + offset) * 1000)
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(request).toHaveBeenLastCalledWith(
      {
        start_timestamp: Math.floor((now + offset - 30 * day) / 3600) * 3600,
        end_timestamp: Math.floor((now + offset) / 3600) * 3600,
        default_time: 'hour',
      },
      false,
      expect.any(AbortSignal)
    )
  }
  await filter()
  await user.click(screen.getByRole('button', { name: 'Last 7 days' }))
  await user.click(screen.getByRole('button', { name: 'Custom' }))
  await apply()
  expect(await screen.findByText('Custom Time Range · Daily')).toBeVisible()
  vi.setSystemTime((now + 180) * 1000)
  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  expect(request).toHaveBeenCalledTimes(6)
  expect(request).toHaveBeenLastCalledWith(
    {
      start_timestamp: Math.floor((now + 120 - 7 * day) / 3600) * 3600,
      end_timestamp: Math.floor((now + 120) / 3600) * 3600,
      default_time: 'hour',
    },
    false,
    expect.any(AbortSignal)
  )
})

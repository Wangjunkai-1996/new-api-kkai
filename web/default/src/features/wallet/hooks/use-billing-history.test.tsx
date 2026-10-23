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
import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { useBillingHistory } from './use-billing-history'

const api = vi.hoisted(() => ({ request: vi.fn() }))
vi.mock('@/lib/api', () => ({ api }))
vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: { tenant_id: 0 } }),
}))
let client: QueryClient
function wrapper(props: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
}
const counts = {
  all: 12,
  pending: 3,
  processing: 2,
  review: 4,
  completed: 2,
  closed: 1,
}
const response = (today = 52.5) => ({
  data: {
    success: true,
    data: {
      items: [],
      total: 12,
      counts,
      query_scope: 'wallet',
      server_time: 1_790_000_000,
      today_payment_total: today,
      today_payment_date: '2026-10-23',
    },
  },
})
beforeEach(() => {
  vi.resetAllMocks()
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
  useAuthStore
    .getState()
    .auth.setUser({ id: 11, username: 'admin-wallet', role: 10 })
  api.request.mockResolvedValue(response())
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
})
describe('explicit billing history scope', () => {
  it('keeps an administrator wallet on the self endpoint and uses one wallet range for every filter', async () => {
    const { result } = renderHook(() => useBillingHistory(), { wrapper })
    await waitFor(() => expect(result.current.total).toBe(12))
    expect(api.request.mock.calls[0]?.[0].url).toBe(
      '/api/user/topup/self?p=1&page_size=10&status_group=all&scope=wallet'
    )
    expect(result.current.counts).toEqual(counts)
    expect(result.current.todayPaymentTotal).toBe(52.5)
    act(() => result.current.handleStatusChange('review'))
    await waitFor(() => expect(api.request).toHaveBeenCalledTimes(2))
    expect(api.request.mock.calls[1]?.[0].url).toContain(
      'status_group=review&scope=wallet'
    )
  })
  it('uses the all-user endpoint only when explicitly opened as admin', async () => {
    const { result } = renderHook(() => useBillingHistory({ scope: 'admin' }), {
      wrapper,
    })
    await waitFor(() => expect(result.current.total).toBe(12))
    expect(api.request.mock.calls[0]?.[0].url).toBe(
      '/api/user/topup?p=1&page_size=10&status_group=all&scope=wallet'
    )
  })
  it('does not query a closed history dialog', () => {
    renderHook(() => useBillingHistory({ enabled: false }), { wrapper })
    expect(api.request).not.toHaveBeenCalled()
  })
  it('clears stale summary facts on a failed initial request instead of inventing zero', async () => {
    api.request.mockRejectedValue(new Error('unavailable'))
    const { result } = renderHook(() => useBillingHistory(), { wrapper })
    await waitFor(() => expect(result.current.error).toBe(true))
    expect(result.current.todayPaymentTotal).toBeNull()
    expect(result.current.records).toEqual([])
  })
})

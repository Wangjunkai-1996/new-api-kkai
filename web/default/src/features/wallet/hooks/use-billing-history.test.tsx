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
import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import type { ApiResponse, BillingHistoryResponse } from '../types'
import { useBillingHistory } from './use-billing-history'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/lib/api', () => ({ api: apiMocks }))

function historyResponse(
  amount: number,
  date = '2026-09-05'
): { data: ApiResponse<BillingHistoryResponse> } {
  return {
    data: {
      success: true,
      data: {
        items: [],
        total: 0,
        today_payment_total: amount,
        today_payment_date: date,
      },
    },
  }
}

describe('billing history daily payment summary', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    useAuthStore.getState().auth.setUser({
      id: 11,
      username: 'billing-user',
      role: ROLE.USER,
    })
  })

  afterEach(() => {
    useAuthStore.getState().auth.reset()
  })

  test.each([
    { role: ROLE.USER, endpoint: '/api/user/topup/self' },
    { role: ROLE.ADMIN, endpoint: '/api/user/topup' },
  ])(
    'loads the appropriate scope for role $role only when opened and refreshes on reopening',
    async ({ role, endpoint }) => {
      useAuthStore.getState().auth.setUser({
        id: 11,
        username: 'billing-user',
        role,
      })
      apiMocks.get
        .mockResolvedValueOnce(historyResponse(0))
        .mockResolvedValueOnce(historyResponse(52.5, '2026-09-06'))

      const { result, rerender } = renderHook(
        (props: { enabled: boolean }) => useBillingHistory(props),
        { initialProps: { enabled: false } }
      )

      expect(apiMocks.get).not.toHaveBeenCalled()
      expect(result.current.todayPaymentTotal).toBeNull()

      rerender({ enabled: true })
      await waitFor(() => expect(result.current.todayPaymentTotal).toBe(0))
      expect(result.current.todayPaymentDate).toBe('2026-09-05')
      expect(apiMocks.get).toHaveBeenCalledWith(`${endpoint}?p=1&page_size=10`)

      rerender({ enabled: false })
      expect(apiMocks.get).toHaveBeenCalledTimes(1)
      rerender({ enabled: true })

      await waitFor(() => expect(result.current.todayPaymentTotal).toBe(52.5))
      expect(result.current.todayPaymentDate).toBe('2026-09-06')
      expect(apiMocks.get).toHaveBeenCalledTimes(2)
    }
  )

  test.each([
    {
      scenario: 'missing summary fields',
      response: { success: true, data: { items: [], total: 0 } },
    },
    {
      scenario: 'a failed history request',
      response: { success: false, message: 'Billing history unavailable' },
    },
  ])(
    'clears the previous summary on $scenario without reporting zero',
    async ({ response }) => {
      apiMocks.get
        .mockResolvedValueOnce(historyResponse(52.5))
        .mockResolvedValueOnce({ data: response })
      const { result } = renderHook(() => useBillingHistory())
      await waitFor(() => expect(result.current.todayPaymentTotal).toBe(52.5))

      await act(async () => {
        await result.current.refresh()
      })

      expect(result.current.todayPaymentTotal).toBeNull()
      expect(result.current.todayPaymentDate).toBeNull()
      expect(result.current.loading).toBe(false)
    }
  )

  test('refreshes the daily payment total after an administrator completes an order', async () => {
    useAuthStore.getState().auth.setUser({
      id: 11,
      username: 'billing-admin',
      role: ROLE.ADMIN,
    })
    apiMocks.get
      .mockResolvedValueOnce(historyResponse(15))
      .mockResolvedValueOnce(historyResponse(52.5))
    apiMocks.post.mockResolvedValueOnce({ data: { success: true } })
    const { result } = renderHook(() => useBillingHistory())
    await waitFor(() => expect(result.current.todayPaymentTotal).toBe(15))

    await act(async () => {
      expect(await result.current.handleCompleteOrder('pending-order')).toBe(
        true
      )
    })

    expect(apiMocks.post).toHaveBeenCalledWith('/api/user/topup/complete', {
      trade_no: 'pending-order',
    })
    expect(apiMocks.get).toHaveBeenCalledTimes(2)
    expect(result.current.todayPaymentTotal).toBe(52.5)
    expect(result.current.todayPaymentDate).toBe('2026-09-05')
    expect(result.current.completing).toBe(false)
  })

  test('ignores a previous opening response that arrives after the reopened summary', async () => {
    let resolveOldRequest!: (
      response: ReturnType<typeof historyResponse>
    ) => void
    const oldRequest = new Promise<ReturnType<typeof historyResponse>>(
      (resolve) => {
        resolveOldRequest = resolve
      }
    )
    apiMocks.get
      .mockReturnValueOnce(oldRequest)
      .mockResolvedValueOnce(historyResponse(52.5, '2026-09-06'))
    const { result, rerender } = renderHook(
      (props: { enabled: boolean }) => useBillingHistory(props),
      { initialProps: { enabled: true } }
    )

    rerender({ enabled: false })
    rerender({ enabled: true })
    await waitFor(() => expect(result.current.todayPaymentTotal).toBe(52.5))

    await act(async () => {
      resolveOldRequest(historyResponse(15))
      await oldRequest
    })

    expect(result.current.todayPaymentTotal).toBe(52.5)
    expect(result.current.todayPaymentDate).toBe('2026-09-06')
    expect(result.current.loading).toBe(false)
  })
})

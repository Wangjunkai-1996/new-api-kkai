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

import {
  paymentOrder,
  paymentQuote,
  paymentWallet,
} from '../payment-flow-fixtures'
import type { WalletState } from '../payment-flow-types'
import { useWalletFundingSummary } from './use-wallet-funding-summary'
import { useWalletPaymentFlow } from './use-wallet-payment-flow'

const api = vi.hoisted(() => ({
  restoreWalletPayment: vi.fn(),
  getWalletPayment: vi.fn(),
  createWalletPayment: vi.fn(),
  resumeWalletPayment: vi.fn(),
  actOnWalletOrder: vi.fn(),
  acknowledgeWalletPayment: vi.fn(),
}))
const fundingApi = vi.hoisted(() => ({ getWalletFundingSummary: vi.fn() }))
vi.mock('../funding-summary-api', () => fundingApi)
vi.mock('../payment-flow-api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../payment-flow-api')>()),
  ...api,
}))
vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: { tenant_id: 0 } }),
}))
let client: QueryClient
function wrapper(props: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
}
beforeEach(() => {
  vi.resetAllMocks()
  client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { retry: false },
    },
  })
  useAuthStore
    .getState()
    .auth.setUser({ id: 11, username: 'wallet-user', role: 1 })
  vi.spyOn(window, 'open').mockReturnValue(null)
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})
describe('authoritative wallet flow', () => {
  it('refreshes mounted funding totals when a current-order refresh confirms payment', async () => {
    const pending = paymentOrder()
    api.restoreWalletPayment.mockResolvedValue(paymentWallet(pending))
    const funding = {
      cash_payments: [],
      confirmed_payment_count: 0,
      unknown_payment_count: 0,
      rebate_credited_quota: '0',
      rebate_credit_display: { amount: '0', unit: 'USD' },
      quota_epoch: 'usd_credit_v1',
      refund_adjusted: false,
      server_time: 1_790_000_000,
    }
    fundingApi.getWalletFundingSummary.mockResolvedValue(funding)
    const { result } = renderHook(
      () => ({
        flow: useWalletPaymentFlow(),
        funding: useWalletFundingSummary(),
      }),
      { wrapper }
    )
    await waitFor(() => {
      expect(result.current.flow.wallet?.flow.order?.display_status).toBe(
        'pending'
      )
      expect(result.current.funding.data?.cash_payments).toEqual([])
    })
    const received = { amount_minor: '1000', currency: 'CNY', exponent: 2 }
    const paid = paymentOrder({
      version: '2',
      display_status: 'success',
      status_group: 'completed',
      received_payment: received,
      allowed_actions: ['acknowledge'],
    })
    api.getWalletPayment.mockResolvedValue(
      paymentWallet(paid, {
        flow: { id: 'FLOW-1', revision: '2', phase: 'result', order: paid },
      })
    )
    fundingApi.getWalletFundingSummary.mockResolvedValue({
      ...funding,
      cash_payments: [received],
      confirmed_payment_count: 1,
    })
    act(() => result.current.flow.refresh())
    await waitFor(() =>
      expect(result.current.funding.data?.cash_payments).toEqual([received])
    )
  })
  it('keeps the local timeout result until acknowledgement succeeds', async () => {
    const expired = paymentOrder({
      display_status: 'timed_out',
      status_group: 'closed',
      allowed_actions: ['acknowledge'],
    })
    const resultWallet = paymentWallet(expired, {
      flow: { id: 'FLOW-1', revision: '2', phase: 'result', order: expired },
    })
    api.restoreWalletPayment.mockResolvedValue(resultWallet)
    let resolveAck: (value: WalletState) => void = () => {}
    api.acknowledgeWalletPayment.mockImplementation(
      () =>
        new Promise<WalletState>((resolve) => {
          resolveAck = resolve
        })
    )
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() =>
      expect(result.current.wallet?.flow.phase).toBe('result')
    )
    expect(api.acknowledgeWalletPayment).not.toHaveBeenCalled()
    act(() => result.current.acknowledge())
    await waitFor(() =>
      expect(api.acknowledgeWalletPayment).toHaveBeenCalledTimes(1)
    )
    expect(result.current.wallet?.flow.phase).toBe('result')
    await act(async () =>
      resolveAck(
        paymentWallet(null, {
          flow: { id: null, revision: '3', phase: 'idle', order: null },
        })
      )
    )
    await waitFor(() => expect(result.current.wallet?.flow.phase).toBe('idle'))
  })
  it('does not locally unlock when acknowledgement fails', async () => {
    const order = paymentOrder({
      display_status: 'success',
      status_group: 'completed',
      allowed_actions: ['acknowledge'],
    })
    api.restoreWalletPayment.mockResolvedValue(
      paymentWallet(order, {
        flow: { id: 'FLOW-1', revision: '2', phase: 'result', order },
      })
    )
    api.acknowledgeWalletPayment.mockRejectedValue(
      new Error('network unavailable')
    )
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() =>
      expect(result.current.wallet?.flow.phase).toBe('result')
    )
    act(() => result.current.acknowledge())
    await waitFor(() =>
      expect(result.current.notice).toBe('WALLET_STATE_UNAVAILABLE')
    )
    expect(result.current.wallet?.can_create).toBe(false)
    expect(result.current.wallet?.flow.phase).toBe('result')
  })
  it('returns to top-up immediately after ending the current order', async () => {
    const original = paymentOrder()
    api.restoreWalletPayment.mockResolvedValue(paymentWallet(original))
    const ended = paymentOrder({
      version: '2',
      display_status: 'ended',
      status_group: 'closed',
      allowed_actions: ['acknowledge'],
    })
    const endedWallet = paymentWallet(ended, {
      flow: { id: 'FLOW-1', revision: '2', phase: 'result', order: ended },
    })
    api.actOnWalletOrder.mockResolvedValue({
      order: ended,
      wallet: endedWallet,
    })
    api.acknowledgeWalletPayment.mockResolvedValue(
      paymentWallet(null, {
        flow: { id: null, revision: '3', phase: 'idle', order: null },
      })
    )
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() => expect(result.current.wallet?.flow.phase).toBe('order'))
    act(() => result.current.act(original, 'end'))
    await waitFor(() => expect(result.current.wallet?.can_create).toBe(true))
    expect(result.current.wallet?.flow.phase).toBe('idle')
    expect(api.acknowledgeWalletPayment).toHaveBeenCalledExactlyOnceWith(
      endedWallet
    )
  })
  it('preserves an ended result when automatic return fails and allows retry', async () => {
    const original = paymentOrder()
    api.restoreWalletPayment.mockResolvedValue(paymentWallet(original))
    const ended = paymentOrder({
      version: '2',
      display_status: 'ended',
      status_group: 'closed',
      allowed_actions: ['acknowledge'],
    })
    api.actOnWalletOrder.mockResolvedValue({
      order: ended,
      wallet: paymentWallet(ended, {
        flow: { id: 'FLOW-1', revision: '2', phase: 'result', order: ended },
      }),
    })
    api.acknowledgeWalletPayment
      .mockRejectedValueOnce(new Error('response lost'))
      .mockResolvedValueOnce(
        paymentWallet(null, {
          flow: { id: null, revision: '3', phase: 'idle', order: null },
        })
      )
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() => expect(result.current.wallet?.flow.phase).toBe('order'))
    act(() => result.current.act(original, 'end'))
    await waitFor(() =>
      expect(result.current.notice).toBe('WALLET_STATE_UNAVAILABLE')
    )
    expect(result.current.wallet?.flow.order?.display_status).toBe('ended')
    expect(result.current.wallet?.can_create).toBe(false)
    act(() => result.current.acknowledge())
    await waitFor(() => expect(result.current.wallet?.can_create).toBe(true))
    expect(api.actOnWalletOrder).toHaveBeenCalledTimes(1)
    expect(api.acknowledgeWalletPayment).toHaveBeenCalledTimes(2)
  })
  it.each(['success', 'credit_review', 'timed_out'] as const)(
    'does not automatically dismiss %s returned by an end request',
    async (displayStatus) => {
      const original = paymentOrder()
      api.restoreWalletPayment.mockResolvedValue(paymentWallet(original))
      const updated = paymentOrder({
        version: '2',
        display_status: displayStatus,
        allowed_actions: ['acknowledge'],
      })
      api.actOnWalletOrder.mockResolvedValue({
        order: updated,
        wallet: paymentWallet(updated, {
          flow: {
            id: 'FLOW-1',
            revision: '2',
            phase: 'result',
            order: updated,
          },
        }),
      })
      const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
      await waitFor(() =>
        expect(result.current.wallet?.flow.phase).toBe('order')
      )
      act(() => result.current.act(original, 'end'))
      await waitFor(() =>
        expect(result.current.wallet?.flow.order?.display_status).toBe(
          displayStatus
        )
      )
      expect(api.acknowledgeWalletPayment).not.toHaveBeenCalled()
    }
  )
  it('blocks simultaneous creates and reuses the original key after an unknown result', async () => {
    api.restoreWalletPayment.mockResolvedValue(paymentWallet())
    api.createWalletPayment
      .mockRejectedValueOnce(new Error('response lost'))
      .mockResolvedValueOnce({
        wallet: paymentWallet(paymentOrder()),
        order: paymentOrder(),
      })
    api.resumeWalletPayment.mockResolvedValue({
      wallet: paymentWallet(paymentOrder()),
      checkout: null,
    })
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() => expect(result.current.wallet?.can_create).toBe(true))
    act(() => {
      result.current.create(paymentQuote())
      result.current.create(paymentQuote())
    })
    await waitFor(() =>
      expect(result.current.notice).toBe('WALLET_STATE_UNAVAILABLE')
    )
    expect(api.createWalletPayment).toHaveBeenCalledTimes(1)
    expect(result.current.creating).toBe(true)
    act(() => result.current.retryCreate())
    await waitFor(() =>
      expect(api.createWalletPayment).toHaveBeenCalledTimes(2)
    )
    expect(api.createWalletPayment.mock.calls[0]?.[2]).toBe(
      api.createWalletPayment.mock.calls[1]?.[2]
    )
    await waitFor(() => expect(result.current.creating).toBe(false))
  })
  it('does not reopen or rebind a replayed old order when another order is current', async () => {
    api.restoreWalletPayment.mockResolvedValue(paymentWallet())
    const latest = paymentOrder({ trade_no: 'NEW-ORDER' })
    const wallet = paymentWallet(latest, {
      flow: { id: 'NEW-FLOW', revision: '5', phase: 'order', order: latest },
    })
    api.createWalletPayment.mockResolvedValue({
      wallet,
      order: paymentOrder({ trade_no: 'OLD-ORDER' }),
    })
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() => expect(result.current.wallet?.can_create).toBe(true))
    act(() => result.current.create(paymentQuote()))
    await waitFor(() =>
      expect(result.current.wallet?.flow.order?.trade_no).toBe('NEW-ORDER')
    )
    expect(api.resumeWalletPayment).not.toHaveBeenCalled()
  })
  it('keeps purchase unavailable when the new restore API is unavailable', async () => {
    api.restoreWalletPayment.mockRejectedValue(new Error('404'))
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() => expect(result.current.error).toBe(true))
    expect(result.current.wallet).toBeUndefined()
    act(() => result.current.create(paymentQuote()))
    expect(api.createWalletPayment).not.toHaveBeenCalled()
  })
  it('does not let a late end result replace a newer current order', async () => {
    const original = paymentOrder()
    api.restoreWalletPayment.mockResolvedValue(paymentWallet(original))
    let resolveEnd: (value: {
      wallet: WalletState
      order: ReturnType<typeof paymentOrder>
    }) => void = () => {}
    api.actOnWalletOrder.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveEnd = resolve
        })
    )
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() =>
      expect(result.current.wallet?.flow.order?.trade_no).toBe('ORDER-1')
    )
    act(() => result.current.act(original, 'end'))
    await waitFor(() => expect(api.actOnWalletOrder).toHaveBeenCalledTimes(1))
    const next = paymentOrder({ trade_no: 'NEW-ORDER' })
    const latest = paymentWallet(next, {
      flow: { id: 'NEW-FLOW', revision: '5', phase: 'order', order: next },
    })
    act(() => result.current.apply(latest))
    await waitFor(() =>
      expect(result.current.wallet?.flow.order?.trade_no).toBe('NEW-ORDER')
    )
    const ended = paymentOrder({
      display_status: 'ended',
      status_group: 'closed',
      allowed_actions: ['acknowledge'],
      local_end_reason: 'user_ended',
      local_ended_at: 1_790_000_010,
    })
    await act(async () =>
      resolveEnd({
        order: ended,
        wallet: paymentWallet(ended, {
          flow: { id: 'FLOW-1', revision: '2', phase: 'result', order: ended },
        }),
      })
    )
    await waitFor(() => expect(result.current.busy).toBe(false))
    expect(result.current.wallet?.flow.order?.trade_no).toBe('NEW-ORDER')
    expect(result.current.wallet?.flow.phase).toBe('order')
    expect(api.acknowledgeWalletPayment).not.toHaveBeenCalled()
  })
  it('does not acknowledge an end response after the signed-in user changes', async () => {
    const original = paymentOrder()
    api.restoreWalletPayment
      .mockResolvedValueOnce(paymentWallet(original))
      .mockResolvedValueOnce(paymentWallet())
    let resolveEnd: (value: {
      wallet: WalletState
      order: ReturnType<typeof paymentOrder>
    }) => void = () => {}
    api.actOnWalletOrder.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveEnd = resolve
        })
    )
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() => expect(result.current.wallet?.flow.phase).toBe('order'))
    act(() => result.current.act(original, 'end'))
    await waitFor(() => expect(api.actOnWalletOrder).toHaveBeenCalledTimes(1))
    act(() => {
      useAuthStore
        .getState()
        .auth.setUser({ id: 12, username: 'another-user', role: 1 })
    })
    await waitFor(() => expect(result.current.wallet?.flow.phase).toBe('idle'))
    const ended = paymentOrder({
      version: '2',
      display_status: 'ended',
      allowed_actions: ['acknowledge'],
    })
    await act(async () =>
      resolveEnd({
        order: ended,
        wallet: paymentWallet(ended, {
          flow: { id: 'FLOW-1', revision: '2', phase: 'result', order: ended },
        }),
      })
    )
    await waitFor(() => expect(result.current.busy).toBe(false))
    expect(api.acknowledgeWalletPayment).not.toHaveBeenCalled()
    expect(result.current.wallet?.flow.phase).toBe('idle')
  })
  it('preserves a new purchase retry when an earlier automatic return arrives late', async () => {
    const ended = paymentOrder({
      version: '2',
      display_status: 'ended',
      allowed_actions: ['acknowledge'],
    })
    const endedWallet = paymentWallet(ended, {
      flow: { id: 'FLOW-1', revision: '2', phase: 'result', order: ended },
    })
    const idle = paymentWallet(null, {
      flow: { id: null, revision: '3', phase: 'idle', order: null },
    })
    api.restoreWalletPayment.mockResolvedValue(endedWallet)
    let resolveAutomaticReturn: (wallet: WalletState) => void = () => {}
    api.acknowledgeWalletPayment
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveAutomaticReturn = resolve
          })
      )
      .mockResolvedValueOnce(idle)
    api.createWalletPayment.mockRejectedValue(new Error('response lost'))
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() =>
      expect(result.current.wallet?.flow.phase).toBe('result')
    )
    let automaticReturn: Promise<boolean>
    act(() => {
      automaticReturn = result.current.acceptOrderAction(
        { order: ended, wallet: endedWallet },
        'end'
      )
    })
    act(() => result.current.acknowledge())
    await waitFor(() => expect(result.current.wallet?.can_create).toBe(true))
    await waitFor(() => expect(result.current.busy).toBe(false))
    act(() => result.current.create(paymentQuote()))
    await waitFor(() =>
      expect(result.current.notice).toBe('WALLET_STATE_UNAVAILABLE')
    )
    await act(async () => {
      resolveAutomaticReturn(idle)
      await automaticReturn
    })
    expect(result.current.creating).toBe(true)
    act(() => result.current.retryCreate())
    await waitFor(() =>
      expect(api.createWalletPayment).toHaveBeenCalledTimes(2)
    )
    expect(api.createWalletPayment.mock.calls[0]?.[2]).toBe(
      api.createWalletPayment.mock.calls[1]?.[2]
    )
  })
  it('refreshes at the site deadline and waits for the server timeout result', async () => {
    vi.useFakeTimers()
    const order = paymentOrder({
      deadline_at: 1_790_000_002,
      deadline_source: 'local_policy',
    })
    api.restoreWalletPayment.mockResolvedValue(paymentWallet(order))
    let resolveCurrent: (wallet: WalletState) => void = () => {}
    api.getWalletPayment.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveCurrent = resolve
        })
    )
    const hook = renderHook(() => useWalletPaymentFlow(), { wrapper })
    try {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1)
      })
      expect(hook.result.current.wallet?.flow.phase).toBe('order')
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1998)
      })
      expect(api.getWalletPayment).not.toHaveBeenCalled()
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1)
      })
      expect(api.getWalletPayment).toHaveBeenCalledTimes(1)
      expect(api.actOnWalletOrder).not.toHaveBeenCalled()
      expect(hook.result.current.wallet?.flow.phase).toBe('order')
      const timedOut = paymentOrder({
        display_status: 'timed_out',
        status_group: 'closed',
        allowed_actions: ['acknowledge'],
        local_end_reason: 'timeout',
        local_ended_at: 1_790_000_002,
      })
      await act(async () => {
        resolveCurrent(
          paymentWallet(timedOut, {
            flow: {
              id: 'FLOW-1',
              revision: '2',
              phase: 'result',
              order: timedOut,
            },
          })
        )
        await vi.advanceTimersByTimeAsync(1)
      })
      expect(hook.result.current.wallet?.flow.phase).toBe('result')
      expect(hook.result.current.wallet?.flow.order?.display_status).toBe(
        'timed_out'
      )
    } finally {
      hook.unmount()
      vi.useRealTimers()
    }
  })
})

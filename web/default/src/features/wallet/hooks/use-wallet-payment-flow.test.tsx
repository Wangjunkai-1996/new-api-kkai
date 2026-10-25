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
  onlineManager,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { WalletPaymentError } from '../payment-flow-api'
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
  getWalletOrder: vi.fn(),
  createWalletPayment: vi.fn(),
  resumeWalletPayment: vi.fn(),
  actOnWalletOrder: vi.fn(),
  acknowledgeWalletPayment: vi.fn(),
}))
const fundingApi = vi.hoisted(() => ({ getWalletFundingSummary: vi.fn() }))
const site = vi.hoisted(() => ({ tenantId: 0 as number | undefined }))
vi.mock('../funding-summary-api', () => fundingApi)
vi.mock('../payment-flow-api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../payment-flow-api')>()),
  ...api,
}))
vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: { tenant_id: site.tenantId } }),
}))
let client: QueryClient
function wrapper(props: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
}
beforeEach(() => {
  vi.resetAllMocks()
  site.tenantId = 0
  client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { retry: false },
    },
  })
  useAuthStore.getState().auth.setBundle({
    access_token: 'wallet-token',
    token_type: 'Bearer',
    access_expires_at: 1_800_000_000,
    user: { id: 11, username: 'wallet-user', role: 1 },
    session: {
      sid: 'wallet-session',
      current: true,
      login_method: 'password',
      ip: '127.0.0.1',
      user_agent: 'wallet-test',
      created_at: 1_790_000_000,
      last_active_at: 1_790_000_000,
      expires_at: 1_800_000_000,
    },
  })
  vi.spyOn(window, 'open').mockReturnValue(null)
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})
describe('authoritative wallet flow', () => {
  it.each(['tenant', 'user', 'session'] as const)(
    'waits for the confirmed %s before current, returned, or manual reads',
    async (missing) => {
      const auth = useAuthStore.getState().auth
      if (missing === 'tenant') site.tenantId = undefined
      if (missing === 'user') {
        useAuthStore.setState({ auth: { ...auth, user: null } })
      }
      if (missing === 'session') {
        useAuthStore.setState({ auth: { ...auth, session: null } })
      }
      const historical = paymentOrder({ trade_no: 'RETURNED-ORDER' })
      api.getWalletPayment.mockResolvedValue(paymentWallet())
      api.getWalletOrder.mockResolvedValue({
        wallet: paymentWallet(),
        order: historical,
      })
      const { result, rerender } = renderHook(
        () => useWalletPaymentFlow(historical.trade_no),
        { wrapper }
      )
      act(() => {
        result.current.refresh()
        result.current.refreshReturned()
        window.dispatchEvent(new Event('focus'))
      })
      expect(api.getWalletPayment).not.toHaveBeenCalled()
      expect(api.getWalletOrder).not.toHaveBeenCalled()
      expect(result.current.writesReady).toBe(false)
      act(() => {
        site.tenantId = 0
        useAuthStore.setState({ auth })
      })
      rerender()
      await waitFor(() => expect(result.current.writesReady).toBe(true))
      expect(result.current.returnedOrder).toEqual(historical)
      expect(api.getWalletPayment).toHaveBeenCalledTimes(1)
      expect(api.getWalletOrder).toHaveBeenCalledTimes(1)
    }
  )
  it('requires a real response after Strict Mode replay, cancellation rollback, and manual cache writes', async () => {
    const queryKey = [
      'wallet-payment',
      window.location.host,
      0,
      11,
      'wallet-session',
      'current',
    ]
    client.setQueryData(queryKey, paymentWallet())
    let resolveCurrent!: (wallet: WalletState) => void
    api.getWalletPayment.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveCurrent = resolve
        })
    )
    const { result } = renderHook(() => useWalletPaymentFlow(), {
      wrapper,
      reactStrictMode: true,
    })
    await act(async () => client.cancelQueries({ queryKey, exact: true }))
    expect(result.current.wallet?.can_create).toBe(true)
    expect(result.current.writesReady).toBe(false)
    act(() => {
      result.current.apply(paymentWallet())
      result.current.create(paymentQuote())
    })
    expect(result.current.writesReady).toBe(false)
    expect(api.createWalletPayment).not.toHaveBeenCalled()
    act(() => result.current.refresh())
    await act(async () => resolveCurrent(paymentWallet()))
    await waitFor(() => expect(result.current.writesReady).toBe(true))
  })
  it.each([false, true])(
    'preserves read cooldown across remount with cached state=%s',
    async (cached) => {
      const observedAt = Date.now()
      const now = vi.spyOn(Date, 'now').mockReturnValue(observedAt)
      const limited = new WalletPaymentError('WALLET_RATE_LIMITED', undefined, {
        status: 429,
        retryAt: observedAt + 60_000,
        rateLimitScope: 'wallet_state',
      })
      if (cached) {
        client.setQueryData(
          [
            'wallet-payment',
            window.location.host,
            0,
            11,
            'wallet-session',
            'current',
          ],
          paymentWallet()
        )
      }
      api.getWalletPayment.mockRejectedValueOnce(limited)
      api.getWalletOrder.mockRejectedValueOnce(limited)
      const first = renderHook(() => useWalletPaymentFlow('RETURNED-ORDER'), {
        wrapper,
      })
      await waitFor(() => {
        expect(first.result.current.readError).toBe(limited)
        expect(first.result.current.returnedError).toBe(limited)
      })
      first.unmount()
      const second = renderHook(() => useWalletPaymentFlow('RETURNED-ORDER'), {
        wrapper,
      })
      act(() => {
        second.result.current.refresh()
        second.result.current.refreshReturned()
        window.dispatchEvent(new Event('focus'))
      })
      expect(second.result.current.writesReady).toBe(false)
      expect(api.getWalletPayment).toHaveBeenCalledTimes(1)
      expect(api.getWalletOrder).toHaveBeenCalledTimes(1)
      second.unmount()
      now.mockReturnValue(observedAt + 60_001)
      const historical = paymentOrder({
        trade_no: 'RETURNED-ORDER',
        display_status: 'success',
        status_group: 'completed',
      })
      api.getWalletPayment.mockResolvedValueOnce(paymentWallet())
      api.getWalletOrder.mockResolvedValueOnce({
        wallet: paymentWallet(),
        order: historical,
      })
      const third = renderHook(() => useWalletPaymentFlow('RETURNED-ORDER'), {
        wrapper,
      })
      await waitFor(() => expect(third.result.current.writesReady).toBe(true))
      expect(third.result.current.returnedOrder).toEqual(historical)
      expect(api.getWalletPayment).toHaveBeenCalledTimes(2)
      expect(api.getWalletOrder).toHaveBeenCalledTimes(2)
    }
  )
  it('starts current and returned reads while offline so request budgets can settle them', async () => {
    onlineManager.setOnline(false)
    api.getWalletPayment.mockRejectedValue(new Error('offline current'))
    api.getWalletOrder.mockRejectedValue(new Error('offline returned'))
    try {
      const { result } = renderHook(
        () => useWalletPaymentFlow('RETURNED-ORDER'),
        { wrapper }
      )
      await waitFor(() => {
        expect(result.current.readError).toBeDefined()
        expect(result.current.returnedError).toBeDefined()
      })
      expect(api.getWalletPayment).toHaveBeenCalledTimes(1)
      expect(api.getWalletOrder).toHaveBeenCalledTimes(1)
      expect(result.current.writesReady).toBe(false)
    } finally {
      onlineManager.setOnline(true)
    }
  })
  it('shows cached idle state but requires a network confirmation on every mount', async () => {
    client.setDefaultOptions({ queries: { staleTime: Infinity, retry: false } })
    api.getWalletPayment.mockResolvedValueOnce(paymentWallet())
    const first = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() => expect(first.result.current.writesReady).toBe(true))
    first.unmount()
    let resolveCurrent!: (wallet: WalletState) => void
    api.getWalletPayment.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveCurrent = resolve
        })
    )
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    expect(result.current.wallet?.can_create).toBe(true)
    expect(result.current.loading).toBe(true)
    expect(result.current.writesReady).toBe(false)
    expect(api.getWalletPayment).toHaveBeenCalledTimes(2)
    act(() => {
      result.current.apply(paymentWallet())
      result.current.create(paymentQuote())
    })
    expect(api.createWalletPayment).not.toHaveBeenCalled()
    expect(result.current.writesReady).toBe(false)
    await act(async () => resolveCurrent(paymentWallet()))
    await waitFor(() => expect(result.current.writesReady).toBe(true))
    expect(result.current.loading).toBe(false)
  })
  it('keeps confirmed content during background reads and blocks new payment after failure', async () => {
    api.getWalletPayment.mockResolvedValueOnce(paymentWallet())
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() => expect(result.current.writesReady).toBe(true))
    let rejectCurrent!: (error: Error) => void
    api.getWalletPayment.mockImplementationOnce(
      () =>
        new Promise((_resolve, reject) => {
          rejectCurrent = reject
        })
    )
    act(() => result.current.refresh())
    expect(result.current.writesReady).toBe(true)
    expect(result.current.loading).toBe(false)
    expect(result.current.wallet?.can_create).toBe(true)
    await act(async () => rejectCurrent(new Error('current unavailable')))
    await waitFor(() => expect(result.current.readError).toBeDefined())
    expect(result.current.writesReady).toBe(false)
    expect(result.current.wallet?.can_create).toBe(true)
    act(() => result.current.create(paymentQuote()))
    expect(api.createWalletPayment).not.toHaveBeenCalled()
  })
  it('does not auto-restore a cached candidate before the mounted current read succeeds', async () => {
    const cached = paymentWallet(null, {
      can_create: false,
      create_block_reason_code: 'RECOVERABLE_ORDER_EXISTS',
    })
    client.setQueryData(
      [
        'wallet-payment',
        window.location.host,
        0,
        11,
        'wallet-session',
        'current',
      ],
      cached
    )
    let resolveCurrent!: (wallet: WalletState) => void
    api.getWalletPayment.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveCurrent = resolve
        })
    )
    const order = paymentOrder()
    api.restoreWalletPayment.mockResolvedValue(
      paymentWallet(order, {
        flow: { id: 'FLOW-1', revision: '2', phase: 'order', order },
      })
    )
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    expect(result.current.wallet).toEqual(cached)
    expect(api.restoreWalletPayment).not.toHaveBeenCalled()
    await act(async () => resolveCurrent(cached))
    await waitFor(() =>
      expect(result.current.wallet?.flow.order?.trade_no).toBe('ORDER-1')
    )
    expect(api.restoreWalletPayment).toHaveBeenCalledTimes(1)
  })
  it('preserves the original unknown request while writes are disabled and confirms again on recovery', async () => {
    api.getWalletPayment.mockResolvedValue(paymentWallet())
    api.createWalletPayment.mockRejectedValue(new Error('response lost'))
    const { result, rerender } = renderHook(
      ({ enabled }) => useWalletPaymentFlow(undefined, enabled),
      { wrapper, initialProps: { enabled: true } }
    )
    await waitFor(() => expect(result.current.writesReady).toBe(true))
    act(() => result.current.create(paymentQuote()))
    await waitFor(() => expect(result.current.operationError).toBeDefined())
    const originalRequest = api.createWalletPayment.mock.calls[0]
    rerender({ enabled: false })
    expect(result.current.creating).toBe(true)
    act(() => {
      result.current.retryCreate()
      result.current.restore()
      result.current.retryRecovery()
      result.current.act(paymentOrder(), 'reconcile')
      result.current.act(paymentOrder(), 'admin-reconcile')
      result.current.refresh()
    })
    await waitFor(() => expect(api.getWalletPayment).toHaveBeenCalledTimes(2))
    expect(api.createWalletPayment).toHaveBeenCalledTimes(1)
    expect(api.restoreWalletPayment).not.toHaveBeenCalled()
    expect(api.actOnWalletOrder).not.toHaveBeenCalled()
    let resolveCurrent!: (wallet: WalletState) => void
    api.getWalletPayment.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveCurrent = resolve
        })
    )
    rerender({ enabled: true })
    await waitFor(() => expect(api.getWalletPayment).toHaveBeenCalledTimes(3))
    expect(result.current.writesReady).toBe(false)
    act(() => result.current.retryCreate())
    expect(api.createWalletPayment).toHaveBeenCalledTimes(1)
    await act(async () => resolveCurrent(paymentWallet()))
    await waitFor(() => expect(result.current.writesReady).toBe(true))
    act(() => result.current.retryCreate())
    await waitFor(() =>
      expect(api.createWalletPayment).toHaveBeenCalledTimes(2)
    )
    expect(api.createWalletPayment.mock.calls[1]).toEqual(originalRequest)
  })
  it('retries automatic recovery after a queued write is blocked by capability changes', async () => {
    const recoverable = paymentWallet(null, {
      can_create: false,
      create_block_reason_code: 'RECOVERABLE_ORDER_EXISTS',
    })
    const order = paymentOrder()
    api.getWalletPayment.mockResolvedValue(recoverable)
    api.restoreWalletPayment.mockResolvedValue(
      paymentWallet(order, {
        flow: { id: 'FLOW-1', revision: '2', phase: 'order', order },
      })
    )
    let finishCancellation!: () => void
    const cancellation = vi
      .spyOn(client, 'cancelQueries')
      .mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            finishCancellation = resolve
          })
      )
    const { result, rerender } = renderHook(
      ({ enabled }) => useWalletPaymentFlow(undefined, enabled),
      { wrapper, initialProps: { enabled: true } }
    )
    await waitFor(() => expect(cancellation).toHaveBeenCalledTimes(1))
    rerender({ enabled: false })
    await act(async () => finishCancellation())
    await waitFor(() => expect(result.current.busy).toBe(false))
    expect(api.restoreWalletPayment).not.toHaveBeenCalled()
    rerender({ enabled: true })
    await waitFor(() =>
      expect(result.current.wallet?.flow.order?.trade_no).toBe('ORDER-1')
    )
    expect(api.getWalletPayment).toHaveBeenCalledTimes(2)
    expect(api.restoreWalletPayment).toHaveBeenCalledTimes(1)
  })
  it('ignores an in-flight disabled read and waits for the new enabled read', async () => {
    let resolveDisabled!: (wallet: WalletState) => void
    let resolveEnabled!: (wallet: WalletState) => void
    api.getWalletPayment
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveDisabled = resolve
          })
      )
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveEnabled = resolve
          })
      )
    const { result, rerender } = renderHook(
      ({ enabled }) => useWalletPaymentFlow(undefined, enabled),
      { wrapper, initialProps: { enabled: false } }
    )
    rerender({ enabled: true })
    await waitFor(() => expect(api.getWalletPayment).toHaveBeenCalledTimes(2))
    await act(async () => resolveDisabled(paymentWallet()))
    expect(result.current.writesReady).toBe(false)
    act(() => result.current.create(paymentQuote()))
    expect(api.createWalletPayment).not.toHaveBeenCalled()
    await act(async () => resolveEnabled(paymentWallet()))
    await waitFor(() => expect(result.current.writesReady).toBe(true))
  })
  it('keeps returned orders readable while blocking every payment write during capability conflicts', async () => {
    const current = paymentOrder({ allowed_actions: ['resume', 'acknowledge'] })
    const historical = paymentOrder({ trade_no: 'RETURNED-ORDER' })
    api.getWalletPayment.mockResolvedValue(paymentWallet(current))
    api.getWalletOrder.mockResolvedValue({
      wallet: paymentWallet(current),
      order: historical,
    })
    const { result } = renderHook(
      () => useWalletPaymentFlow(historical.trade_no, false),
      { wrapper }
    )
    await waitFor(() =>
      expect(result.current.returnedOrder).toEqual(historical)
    )
    act(() => {
      result.current.resume()
      result.current.resumeReturned()
      result.current.acknowledge()
      result.current.act(current, 'end')
      result.current.act(current, 'cancel')
      result.current.restore(historical.trade_no)
      result.current.refreshReturned()
    })
    await waitFor(() => expect(api.getWalletOrder).toHaveBeenCalledTimes(2))
    expect(api.resumeWalletPayment).not.toHaveBeenCalled()
    expect(api.acknowledgeWalletPayment).not.toHaveBeenCalled()
    expect(api.actOnWalletOrder).not.toHaveBeenCalled()
    expect(api.restoreWalletPayment).not.toHaveBeenCalled()
  })
  it('does not continue a pending create with a resume write after capability is disabled', async () => {
    api.getWalletPayment.mockResolvedValue(paymentWallet())
    let resolveCreate!: (value: {
      wallet: WalletState
      order: ReturnType<typeof paymentOrder>
    }) => void
    api.createWalletPayment.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveCreate = resolve
        })
    )
    const { result, rerender } = renderHook(
      ({ enabled }) => useWalletPaymentFlow(undefined, enabled),
      { wrapper, initialProps: { enabled: true } }
    )
    await waitFor(() => expect(result.current.writesReady).toBe(true))
    act(() => result.current.create(paymentQuote()))
    await waitFor(() =>
      expect(api.createWalletPayment).toHaveBeenCalledTimes(1)
    )
    rerender({ enabled: false })
    const order = paymentOrder()
    await act(async () =>
      resolveCreate({
        wallet: paymentWallet(order, {
          flow: { id: 'FLOW-1', revision: '2', phase: 'order', order },
        }),
        order,
      })
    )
    await waitFor(() => expect(result.current.busy).toBe(false))
    expect(result.current.wallet?.flow.order?.trade_no).toBe('ORDER-1')
    expect(api.resumeWalletPayment).not.toHaveBeenCalled()
  })
  it('does not auto-acknowledge a late end response after capability is disabled', async () => {
    const order = paymentOrder()
    api.getWalletPayment.mockResolvedValue(paymentWallet(order))
    let resolveEnd!: (value: {
      wallet: WalletState
      order: ReturnType<typeof paymentOrder>
    }) => void
    api.actOnWalletOrder.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveEnd = resolve
        })
    )
    const { result, rerender } = renderHook(
      ({ enabled }) => useWalletPaymentFlow(undefined, enabled),
      { wrapper, initialProps: { enabled: true } }
    )
    await waitFor(() => expect(result.current.writesReady).toBe(true))
    act(() => result.current.act(order, 'end'))
    await waitFor(() => expect(api.actOnWalletOrder).toHaveBeenCalledTimes(1))
    rerender({ enabled: false })
    const ended = paymentOrder({
      display_status: 'ended',
      allowed_actions: ['acknowledge'],
    })
    await act(async () =>
      resolveEnd({
        wallet: paymentWallet(ended, {
          flow: { id: 'FLOW-1', revision: '2', phase: 'result', order: ended },
        }),
        order: ended,
      })
    )
    await waitFor(() => expect(result.current.busy).toBe(false))
    expect(result.current.wallet?.flow.phase).toBe('result')
    expect(api.acknowledgeWalletPayment).not.toHaveBeenCalled()
  })
  it('reads a returned historical order without restoring or replacing another current order', async () => {
    const current = paymentOrder({ trade_no: 'CURRENT-ORDER' })
    const historical = paymentOrder({
      trade_no: 'RETURNED-ORDER',
      display_status: 'success',
      status_group: 'completed',
      allowed_actions: [],
    })
    api.getWalletPayment.mockResolvedValue(paymentWallet(current))
    api.getWalletOrder.mockResolvedValue({
      wallet: paymentWallet(current),
      order: historical,
    })
    const { result } = renderHook(
      () => useWalletPaymentFlow('RETURNED-ORDER'),
      { wrapper }
    )
    await waitFor(() =>
      expect(result.current.returnedOrder?.display_status).toBe('success')
    )
    expect(result.current.wallet?.flow.order?.trade_no).toBe('CURRENT-ORDER')
    expect(api.restoreWalletPayment).not.toHaveBeenCalled()
    expect(api.acknowledgeWalletPayment).not.toHaveBeenCalled()
    act(() => result.current.resumeReturned())
    expect(api.resumeWalletPayment).not.toHaveBeenCalled()
    act(() => result.current.dismissReturned())
    expect(result.current.returnedOrder).toBeUndefined()
    expect(result.current.wallet?.flow.order?.trade_no).toBe('CURRENT-ORDER')
  })
  it('still displays the returned order when reading the current wallet fails', async () => {
    const order = paymentOrder({
      display_status: 'success',
      status_group: 'completed',
      allowed_actions: [],
    })
    api.getWalletPayment.mockRejectedValue(new Error('current unavailable'))
    api.getWalletOrder.mockResolvedValue({ wallet: paymentWallet(), order })
    const { result } = renderHook(() => useWalletPaymentFlow(order.trade_no), {
      wrapper,
    })
    await waitFor(() =>
      expect(result.current.returnedOrder?.display_status).toBe('success')
    )
    expect(result.current.readError).toBeDefined()
    expect(api.restoreWalletPayment).not.toHaveBeenCalled()
    act(() => result.current.resumeReturned())
    expect(api.resumeWalletPayment).not.toHaveBeenCalled()
  })
  it('restores an orphaned unfinished order once and does not retry a failed write on refresh', async () => {
    api.getWalletPayment.mockResolvedValue(
      paymentWallet(null, {
        can_create: false,
        create_block_reason_code: 'RECOVERABLE_ORDER_EXISTS',
      })
    )
    api.restoreWalletPayment.mockRejectedValue(new Error('network unavailable'))
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() => expect(result.current.operationError).toBeDefined())
    expect(api.restoreWalletPayment).toHaveBeenCalledTimes(1)
    act(() => result.current.refresh())
    await waitFor(() => expect(api.getWalletPayment).toHaveBeenCalledTimes(2))
    expect(api.restoreWalletPayment).toHaveBeenCalledTimes(1)
    expect(result.current.wallet?.can_create).toBe(false)
    const recovered = paymentOrder()
    api.restoreWalletPayment.mockResolvedValue(
      paymentWallet(recovered, {
        flow: {
          id: 'RECOVERED',
          revision: '2',
          phase: 'order',
          order: recovered,
        },
      })
    )
    act(() => result.current.retryRecovery())
    await waitFor(() =>
      expect(result.current.wallet?.flow.order?.trade_no).toBe('ORDER-1')
    )
    expect(api.restoreWalletPayment).toHaveBeenCalledTimes(2)
  })
  it('lets the user restore and resume a returned pending order with one action', async () => {
    const order = paymentOrder({ trade_no: 'RETURNED-ORDER' })
    const bound = paymentWallet(order, {
      flow: { id: 'RETURNED-FLOW', revision: '2', phase: 'order', order },
    })
    api.getWalletPayment.mockResolvedValue(paymentWallet())
    api.getWalletOrder.mockResolvedValue({ wallet: paymentWallet(), order })
    api.restoreWalletPayment.mockResolvedValue(bound)
    api.resumeWalletPayment.mockResolvedValue({ wallet: bound, checkout: null })
    const { result } = renderHook(
      () => useWalletPaymentFlow('RETURNED-ORDER'),
      { wrapper }
    )
    await waitFor(() =>
      expect(result.current.returnedOrder?.trade_no).toBe('RETURNED-ORDER')
    )
    expect(api.restoreWalletPayment).not.toHaveBeenCalled()
    act(() => result.current.resumeReturned())
    await waitFor(() =>
      expect(api.resumeWalletPayment).toHaveBeenCalledExactlyOnceWith(
        order,
        bound
      )
    )
    expect(api.restoreWalletPayment).toHaveBeenCalledExactlyOnceWith(
      'RETURNED-ORDER'
    )
    expect(result.current.wallet?.flow.order?.trade_no).toBe('RETURNED-ORDER')
  })
  it('does not resend a throttled create on focus and keeps current-state reads available', async () => {
    api.getWalletPayment.mockResolvedValue(paymentWallet())
    api.createWalletPayment.mockRejectedValue(
      new WalletPaymentError('WALLET_RATE_LIMITED', undefined, {
        status: 429,
        retryAt: Date.now() + 60_000,
        rateLimitScope: 'wallet_checkout',
      })
    )
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() => expect(result.current.wallet?.can_create).toBe(true))
    act(() => result.current.create(paymentQuote()))
    await waitFor(() =>
      expect(result.current.operationError).toBeInstanceOf(WalletPaymentError)
    )
    act(() => {
      window.dispatchEvent(new Event('focus'))
      result.current.retryCreate()
    })
    await waitFor(() => expect(api.getWalletPayment).toHaveBeenCalledTimes(2))
    expect(api.createWalletPayment).toHaveBeenCalledTimes(1)
  })
  it('stops automatic reads after 120 seconds without ending or reconciling the order', async () => {
    vi.useFakeTimers()
    api.getWalletPayment.mockResolvedValue(paymentWallet(paymentOrder()))
    const hook = renderHook(() => useWalletPaymentFlow(), { wrapper })
    try {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(120_001)
      })
      const count = api.getWalletPayment.mock.calls.length
      expect(hook.result.current.pollingPaused).toBe(true)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(30_000)
      })
      expect(api.getWalletPayment).toHaveBeenCalledTimes(count)
      expect(api.actOnWalletOrder).not.toHaveBeenCalled()
      expect(hook.result.current.wallet?.flow.order?.display_status).toBe(
        'pending'
      )
    } finally {
      hook.unmount()
      vi.useRealTimers()
    }
  })
  it('refreshes mounted funding totals when a current-order refresh confirms payment', async () => {
    const pending = paymentOrder()
    api.getWalletPayment.mockResolvedValue(paymentWallet(pending))
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
    api.getWalletPayment.mockResolvedValue(resultWallet)
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
    api.getWalletPayment.mockResolvedValue(
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
    api.getWalletPayment.mockResolvedValue(paymentWallet(original))
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
    api.getWalletPayment.mockResolvedValue(paymentWallet(original))
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
      api.getWalletPayment.mockResolvedValue(paymentWallet(original))
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
    api.getWalletPayment.mockResolvedValue(paymentWallet())
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
    api.getWalletPayment.mockResolvedValue(paymentWallet())
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
  it('keeps purchase unavailable when the current-state read is unavailable', async () => {
    api.getWalletPayment.mockRejectedValue(new Error('404'))
    const { result } = renderHook(() => useWalletPaymentFlow(), { wrapper })
    await waitFor(() => expect(result.current.error).toBe(true))
    expect(result.current.wallet).toBeUndefined()
    act(() => result.current.create(paymentQuote()))
    expect(api.createWalletPayment).not.toHaveBeenCalled()
  })
  it('does not let a late end result replace a newer current order', async () => {
    const original = paymentOrder()
    api.getWalletPayment.mockResolvedValue(paymentWallet(original))
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
    api.getWalletPayment
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
    api.getWalletPayment.mockResolvedValue(endedWallet)
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
    api.getWalletPayment.mockResolvedValueOnce(paymentWallet(order))
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
      expect(api.getWalletPayment).toHaveBeenCalledTimes(1)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1)
      })
      expect(api.getWalletPayment).toHaveBeenCalledTimes(2)
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

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
import {
  focusManager,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import { AxiosError } from 'axios'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { getWalletBalance } from '../api'
import type { UserWalletData } from '../types'
import { useWalletBalance } from './use-wallet-balance'

const identity = vi.hoisted(() => ({
  host: 'example.test',
  tenant: 0 as number | undefined,
  user: 1 as number | undefined,
  session: 'session' as string | undefined,
}))

vi.mock('../api', () => ({ getWalletBalance: vi.fn() }))
vi.mock('./use-wallet-payment-flow', () => ({
  useWalletPaymentIdentity: () => [
    identity.host,
    identity.tenant,
    identity.user,
    identity.session,
  ],
}))

let client: QueryClient

function wrapper(props: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
}

function balance(quota: number): UserWalletData {
  return { quota } as UserWalletData
}

function limitedBalanceError() {
  return new AxiosError('limited', undefined, undefined, undefined, {
    status: 429,
    headers: { 'retry-after': '600' },
    data: {},
  } as never)
}

beforeEach(() => {
  Object.assign(identity, {
    host: 'example.test',
    tenant: 0,
    user: 1,
    session: 'session',
  })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
  vi.mocked(getWalletBalance).mockResolvedValue({
    success: true,
    data: balance(125),
  })
})

afterEach(() => {
  client.clear()
  focusManager.setFocused(undefined)
  vi.resetAllMocks()
})

describe('useWalletBalance', () => {
  it('shares one in-flight request and preserves the wallet query key', async () => {
    let resolveBalance!: (value: {
      success: true
      data: UserWalletData
    }) => void
    const pending = new Promise<{ success: true; data: UserWalletData }>(
      (resolve) => {
        resolveBalance = resolve
      }
    )
    vi.mocked(getWalletBalance).mockReturnValue(pending)

    const first = renderHook(() => useWalletBalance(), { wrapper })
    const second = renderHook(() => useWalletBalance(), { wrapper })

    await waitFor(() => expect(getWalletBalance).toHaveBeenCalledTimes(1))
    expect(
      client.getQueryCache().find({
        queryKey: [
          'wallet-payment',
          'example.test',
          0,
          1,
          'session',
          'balance',
        ],
      })?.queryKey
    ).toEqual(['wallet-payment', 'example.test', 0, 1, 'session', 'balance'])

    await act(async () => resolveBalance({ success: true, data: balance(0) }))
    await waitFor(() => expect(first.result.current.data?.quota).toBe(0))
    expect(second.result.current.data?.quota).toBe(0)
  })

  it.each([
    { label: 'missing', data: undefined },
    { label: 'invalid', data: { quota: Number.NaN } as UserWalletData },
  ])('does not turn a $label quota into zero', async ({ data }) => {
    vi.mocked(getWalletBalance).mockResolvedValue({ success: true, data })
    const { result } = renderHook(() => useWalletBalance(), { wrapper })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.data).toBeUndefined()
  })

  it('keeps the cached balance visible when a refresh fails', async () => {
    const queryKey = [
      'wallet-payment',
      'example.test',
      0,
      1,
      'session',
      'balance',
    ]
    client.setQueryData(queryKey, balance(125))
    vi.mocked(getWalletBalance).mockRejectedValue(new Error('unavailable'))

    const { result } = renderHook(() => useWalletBalance(), { wrapper })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.data?.quota).toBe(125)
  })

  it('does not retry a rate-limited balance read during its retry-after window', async () => {
    vi.mocked(getWalletBalance).mockRejectedValue(limitedBalanceError())
    const first = renderHook(() => useWalletBalance(), { wrapper })

    await waitFor(() => expect(first.result.current.isError).toBe(true))
    expect(getWalletBalance).toHaveBeenCalledTimes(1)

    await act(async () => {
      focusManager.setFocused(false)
      focusManager.setFocused(true)
    })
    expect(getWalletBalance).toHaveBeenCalledTimes(1)

    first.unmount()
    renderHook(() => useWalletBalance(), { wrapper })
    await waitFor(() => expect(getWalletBalance).toHaveBeenCalledTimes(1))
  })

  it('does not expose the previous identity balance while the new identity loads', async () => {
    const pending = new Promise<{ success: true; data: UserWalletData }>(
      () => {}
    )
    const { result, rerender } = renderHook(() => useWalletBalance(), {
      wrapper,
    })
    await waitFor(() => expect(result.current.data?.quota).toBe(125))

    identity.user = 2
    vi.mocked(getWalletBalance).mockReturnValueOnce(pending)
    rerender()

    await waitFor(() => expect(getWalletBalance).toHaveBeenCalledTimes(2))
    expect(result.current.data).toBeUndefined()
    expect(
      client.getQueryCache().find({
        queryKey: [
          'wallet-payment',
          'example.test',
          0,
          2,
          'session',
          'balance',
        ],
      })?.queryKey
    ).toEqual(['wallet-payment', 'example.test', 0, 2, 'session', 'balance'])
  })
})

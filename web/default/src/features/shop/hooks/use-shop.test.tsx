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
import { webcrypto } from 'node:crypto'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { z } from 'zod'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { ShopError } from '../api'
import { usePrivateResult, useShopAction } from './use-shop'

vi.mock('@/lib/api', () => ({ api: { request: vi.fn() } }))
const request = vi.mocked(api.request)

beforeEach(() => {
  Object.defineProperty(globalThis, 'crypto', {
    configurable: true,
    value: webcrypto,
  })
  Object.defineProperty(document, 'hidden', {
    configurable: true,
    value: false,
  })
  useAuthStore.getState().auth.setUser({ id: 1, username: 'buyer', role: 1 })
  request.mockReset()
})

function fixture() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const wrapper = (props: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
  return { client, wrapper }
}

describe('controlled shop writes', () => {
  it('keeps the original key after uncertain writes and rejects changed content', async () => {
    const { client, wrapper } = fixture()
    const { result } = renderHook(useShopAction, { wrapper })
    request.mockRejectedValueOnce(new ShopError('SHOP_INTERNAL_ERROR'))
    await act(async () => {
      await result.current.run(
        '/api/shop/orders',
        z.object({ id: z.string() }),
        { password: 'sensitive' }
      )
    })
    const key = request.mock.calls[0][0].headers?.['Idempotency-Key']
    request.mockResolvedValueOnce({
      status: 200,
      data: { success: true, data: { status: 'not_observed' } },
    })
    await act(async () => {
      await result.current.run(
        '/api/shop/order-requests/key',
        z.object({ status: z.string() }),
        undefined,
        { read: true }
      )
    })
    await act(async () => {
      await result.current.run(
        '/api/shop/orders',
        z.object({ id: z.string() }),
        { password: 'different' }
      )
    })
    expect(result.current.error?.code).toBe('SHOP_REQUEST_UNRESOLVED')
    expect(request).toHaveBeenCalledTimes(2)
    request.mockResolvedValueOnce({
      status: 200,
      data: { success: true, data: { id: 'order' } },
    })
    await act(async () => {
      await result.current.run(
        '/api/shop/orders',
        z.object({ id: z.string() }),
        { password: 'sensitive' }
      )
    })
    expect(request.mock.calls[2][0].headers?.['Idempotency-Key']).toBe(key)
    expect(client.getMutationCache().getAll()).toHaveLength(0)
    expect(client.getQueryCache().getAll()).toHaveLength(0)
  })
  it('allows corrected input after a definitive validation rejection', async () => {
    const { wrapper } = fixture()
    const { result } = renderHook(useShopAction, { wrapper })
    const rejected = vi.fn()
    request.mockRejectedValueOnce(new ShopError('SHOP_VALIDATION_FAILED', 400))
    await act(async () => {
      await result.current.run(
        '/action',
        z.string(),
        { value: 'bad' },
        {
          onDefinitiveRejection: rejected,
        }
      )
    })
    expect(rejected).toHaveBeenCalledOnce()
    const firstKey = request.mock.calls[0][0].headers?.['Idempotency-Key']
    request.mockResolvedValueOnce({
      status: 200,
      data: { success: true, data: 'ok' },
    })
    await act(async () => {
      await result.current.run('/action', z.string(), { value: 'fixed' })
    })
    expect(request.mock.calls[1][0].headers?.['Idempotency-Key']).not.toBe(
      firstKey
    )
    expect(result.current.error).toBeNull()
  })
  it.each([
    { code: 'SHOP_INTERNAL_ERROR', status: 503 },
    { code: 'SHOP_REQUEST_UNRESOLVED', status: 409 },
  ])(
    'does not release recovery after $code followed by a rejection',
    async ({ code, status }) => {
      const { wrapper } = fixture()
      const { result } = renderHook(useShopAction, { wrapper })
      const rejected = vi.fn()
      request.mockRejectedValueOnce(new ShopError(code, status))
      request.mockRejectedValueOnce(new ShopError('SHOP_VERSION_CONFLICT', 409))
      await act(async () => {
        await result.current.run(
          '/action',
          z.string(),
          { value: 'original' },
          {
            onDefinitiveRejection: rejected,
          }
        )
      })
      expect(rejected).not.toHaveBeenCalled()
      await act(async () => {
        await result.current.run(
          '/action',
          z.string(),
          { value: 'original' },
          {
            onDefinitiveRejection: rejected,
          }
        )
      })
      expect(rejected).not.toHaveBeenCalled()
      expect(request.mock.calls[1][0].headers?.['Idempotency-Key']).toBe(
        request.mock.calls[0][0].headers?.['Idempotency-Key']
      )
    }
  )
})

describe('private response lifetime', () => {
  it('rejects a late response from a previous resource and clears values when hidden', async () => {
    const { client, wrapper } = fixture()
    const { result, rerender } = renderHook(
      ({ resource }) => usePrivateResult<string>(resource),
      { initialProps: { resource: 'first' }, wrapper }
    )
    let resolve: (value: string) => void = () => undefined
    const late = new Promise<string>((done) => {
      resolve = done
    })
    let read: Promise<string | undefined>
    act(() => {
      read = result.current.read(() => late)
    })
    rerender({ resource: 'second' })
    await act(async () => {
      resolve('previous secret')
      await read
    })
    expect(result.current.value).toBeNull()
    await act(async () => {
      await result.current.read(async () => 'current secret')
    })
    expect(result.current.value).toBe('current secret')
    act(() => {
      Object.defineProperty(document, 'hidden', {
        configurable: true,
        value: true,
      })
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(result.current.value).toBeNull()
    expect(client.getQueryCache().getAll()).toHaveLength(0)
    expect(client.getMutationCache().getAll()).toHaveLength(0)
  })
  it('aborts on unmount', async () => {
    const { wrapper } = fixture()
    const { result, unmount } = renderHook(
      () => usePrivateResult<string>('order'),
      { wrapper }
    )
    let signal: AbortSignal | undefined
    let resolve: (value: string) => void = () => undefined
    const late = new Promise<string>((done) => {
      resolve = done
    })
    let read: Promise<string | undefined>
    act(() => {
      read = result.current.read((current) => {
        signal = current
        return late
      })
    })
    unmount()
    expect(signal?.aborted).toBe(true)
    await act(async () => {
      resolve('secret')
      expect(await read).toBeUndefined()
    })
  })
  it('does not accept a response or issue a stale read after identity changes', async () => {
    const { wrapper } = fixture()
    const { result } = renderHook(() => usePrivateResult<string>('order'), {
      wrapper,
    })
    const previousRead = result.current.read
    let resolve: (value: string) => void = () => undefined
    const late = new Promise<string>((done) => {
      resolve = done
    })
    let read: Promise<string | undefined>
    act(() => {
      read = previousRead(() => late)
    })
    act(() => {
      useAuthStore
        .getState()
        .auth.setUser({ id: 2, username: 'other', role: 1 })
    })
    await act(async () => {
      resolve('secret')
      expect(await read).toBeUndefined()
    })
    expect(result.current.value).toBeNull()
    const staleRequest = vi.fn(async () => 'secret')
    await act(async () => {
      expect(await previousRead(staleRequest)).toBeUndefined()
    })
    expect(staleRequest).not.toHaveBeenCalled()
  })
})

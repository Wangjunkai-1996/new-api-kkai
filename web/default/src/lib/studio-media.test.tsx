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
import { AxiosError, CanceledError } from 'axios'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { api } from './api'
import { createPrivateQueryUserBoundary } from './private-query-cache'
import { signedStudioMediaUrl, useStudioMediaUrls } from './studio-media'

vi.mock('./api', () => ({ api: { post: vi.fn() } }))
const content = '/api/video-studio/assets/1/content'
const poster = '/api/video-studio/assets/1/poster'
const post = vi.mocked(api.post)
let client: QueryClient
function Wrapper(props: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
}
function denied(status: number) {
  return new AxiosError('Media unavailable', undefined, undefined, undefined, {
    status,
    data: {},
    statusText: 'Unavailable',
    headers: {},
    config: { headers: {} } as never,
  })
}
function signed(urls: Record<string, string>) {
  return {
    data: {
      success: true,
      data: { urls, expires_at: Math.floor(Date.now() / 1000) + 300 },
    },
  }
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  post.mockReset()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'one', role: 1 })
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
})
describe('Studio media authorization', () => {
  test('retains successful paths and revokes a denied path in a mixed refresh', async () => {
    post.mockResolvedValueOnce(
      signed({ [content]: 'video', [poster]: 'poster' })
    )
    const view = renderHook(() => useStudioMediaUrls([content, poster]), {
      wrapper: Wrapper,
    })
    await waitFor(() =>
      expect(view.result.current.data?.urls[content]).toBe('video')
    )
    post.mockRejectedValueOnce(denied(403))
    post.mockImplementation(async (_url, body) => {
      if ((body as { paths: string[] }).paths[0] === content) throw denied(404)
      return signed({ [poster]: 'new-poster' })
    })
    await act(async () => {
      await view.result.current.refetch()
    })
    await waitFor(() =>
      expect(view.result.current.data?.deniedPaths).toEqual({ [content]: 404 })
    )
    expect(view.result.current.data?.urls).toEqual({ [poster]: 'new-poster' })
    expect(
      signedStudioMediaUrl(
        content,
        { [content]: 'old' },
        view.result.current.data?.deniedPaths
      )
    ).toBeUndefined()
  })
  test('401 masks previous data without fallback and stays denied until fresh authorization', async () => {
    post.mockResolvedValueOnce(signed({ [content]: 'old-video' }))
    const view = renderHook(() => useStudioMediaUrls([content]), {
      wrapper: Wrapper,
    })
    await waitFor(() => expect(view.result.current.isSuccess).toBe(true))
    post.mockRejectedValueOnce(denied(401))
    await act(async () => {
      await view.result.current.refetch()
    })
    expect(post).toHaveBeenCalledTimes(2)
    await waitFor(() => expect(view.result.current.identityDenied).toBe(true))
    expect(view.result.current.data).toBeUndefined()
    expect(
      signedStudioMediaUrl(
        'https://legacy.test/video',
        undefined,
        undefined,
        view.result.current.identityDenied
      )
    ).toBeUndefined()
    post.mockRejectedValue(new Error('offline'))
    await act(async () => {
      await view.result.current.refetch()
    })
    expect(view.result.current.data).toBeUndefined()
    post.mockResolvedValueOnce(signed({ [content]: 'new-video' }))
    await act(async () => {
      await view.result.current.refetch()
    })
    await waitFor(() => expect(view.result.current.identityDenied).toBe(false))
    expect(view.result.current.data?.urls[content]).toBe('new-video')
  })
  test('a fallback 401 invalidates an otherwise successful mixed batch', async () => {
    post.mockRejectedValueOnce(denied(403))
    post.mockImplementation(async (_url, body) => {
      if ((body as { paths: string[] }).paths[0] === content) throw denied(401)
      return signed({ [poster]: 'poster' })
    })
    const view = renderHook(() => useStudioMediaUrls([content, poster]), {
      wrapper: Wrapper,
    })
    await waitFor(() => expect(view.result.current.isError).toBe(true))
    await waitFor(() => expect(view.result.current.identityDenied).toBe(true))
    expect(view.result.current.data).toBeUndefined()
  })
  test('network failure keeps healthy media and is not revocation', async () => {
    post.mockResolvedValueOnce(signed({ [content]: 'healthy-video' }))
    const view = renderHook(() => useStudioMediaUrls([content]), {
      wrapper: Wrapper,
    })
    await waitFor(() => expect(view.result.current.isSuccess).toBe(true))
    post.mockRejectedValue(new Error('offline'))
    await act(async () => {
      await view.result.current.refetch()
    })
    await waitFor(() => expect(view.result.current.isError).toBe(true))
    expect(view.result.current.identityDenied).toBe(false)
    expect(view.result.current.data?.urls[content]).toBe('healthy-video')
    expect(view.result.current.data?.deniedPaths).toEqual({})
  })
  test('cancelled requests propagate the signal and never start fallback', async () => {
    let signal: AbortSignal | undefined
    let reject!: (reason: unknown) => void
    post.mockImplementation((_url, _body, options) => {
      signal = options?.signal as AbortSignal
      return new Promise((_resolve, rejectPromise) => {
        reject = rejectPromise
      })
    })
    const view = renderHook(() => useStudioMediaUrls([content]), {
      wrapper: Wrapper,
    })
    await waitFor(() => expect(post).toHaveBeenCalledTimes(1))
    view.unmount()
    expect(signal?.aborted).toBe(true)
    await act(async () => reject(new CanceledError()))
    expect(post).toHaveBeenCalledTimes(1)
  })
  test('private-user keys and cancellation prevent previous account late responses', async () => {
    let finishOld!: (value: ReturnType<typeof signed>) => void
    post.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishOld = resolve
        })
    )
    const boundary = createPrivateQueryUserBoundary(client, 1)
    const view = renderHook(() => useStudioMediaUrls([content]), {
      wrapper: Wrapper,
    })
    await waitFor(() => expect(post).toHaveBeenCalledTimes(1))
    post.mockResolvedValueOnce(signed({ [content]: 'user-two-video' }))
    act(() => {
      boundary(2)
      useAuthStore.getState().auth.setUser({ id: 2, username: 'two', role: 1 })
    })
    await waitFor(() =>
      expect(view.result.current.data?.urls[content]).toBe('user-two-video')
    )
    await act(async () => finishOld(signed({ [content]: 'user-one-video' })))
    expect(view.result.current.data?.urls[content]).toBe('user-two-video')
    expect(
      client
        .getQueryCache()
        .getAll()
        .some(
          (query) =>
            query.queryKey[0] === 'private-user' && query.queryKey[1] === 1
        )
    ).toBe(false)
  })
})

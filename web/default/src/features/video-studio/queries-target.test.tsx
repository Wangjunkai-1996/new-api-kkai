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
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { AxiosError } from 'axios'
import type { PropsWithChildren } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { getVideoGeneration, getVideoGenerations } from './api'
import { useVideoGenerations } from './queries'
import type { VideoGeneration } from './types'

vi.mock('./api', () => ({
  getVideoGeneration: vi.fn(),
  getVideoGenerations: vi.fn(),
}))
const generation = (
  id: number,
  status: VideoGeneration['status'] = 'processing'
): VideoGeneration => ({
  id,
  task_id: `task_${id}`,
  model_profile_id: 1,
  model: 'video-model',
  mode: 'text_to_video',
  prompt: 'Test video',
  parameters: {},
  status,
  progress: '',
  quota: 0,
  created_at: 1,
  updated_at: 1,
})
let client: QueryClient
function Wrapper(props: PropsWithChildren) {
  return (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  useAuthStore.setState((state) => ({
    auth: { ...state.auth, user: { id: 1, username: 'test', role: 1 } },
  }))
  vi.mocked(getVideoGenerations).mockResolvedValue({
    items: [generation(50, 'ready')],
  })
  vi.mocked(getVideoGeneration).mockResolvedValue(generation(7))
})
afterEach(() => {
  cleanup()
  client.clear()
  vi.useRealTimers()
  vi.resetAllMocks()
})

describe('known video generation targets', () => {
  test('observes an active deep link outside the loaded head and updates its terminal result', async () => {
    const { result } = renderHook(
      () => useVideoGenerations({}, true, 'task_7', 7),
      { wrapper: Wrapper }
    )
    await waitFor(() =>
      expect(result.current.targetGeneration?.status).toBe('processing')
    )
    expect(getVideoGeneration).toHaveBeenCalledWith(7, { silent: true })
    vi.mocked(getVideoGeneration).mockResolvedValue(generation(7, 'ready'))
    await act(async () => {
      await result.current.refresh()
    })
    await waitFor(() =>
      expect(result.current.targetGeneration?.status).toBe('ready')
    )
    expect(result.current.items.map((item) => item.id)).toContain(7)
  })

  test('permission evidence removes a previously visible target and its media snapshot', async () => {
    const { result } = renderHook(
      () => useVideoGenerations({}, true, 'task_7', 7),
      { wrapper: Wrapper }
    )
    await waitFor(() => expect(result.current.targetGeneration?.id).toBe(7))
    const error = new AxiosError('Forbidden')
    error.response = { status: 403 } as AxiosError['response']
    vi.mocked(getVideoGeneration).mockRejectedValue(error)
    await act(async () => {
      await result.current.refresh()
    })
    await waitFor(() => expect(result.current.targetUnavailable).toBe(true))
    expect(result.current.targetGeneration).toBeUndefined()
    expect(result.current.items.some((item) => item.id === 7)).toBe(false)
  })

  test('task-only discovery stops after 30 seconds and manual lookup restarts the window', async () => {
    vi.useFakeTimers()
    const { result } = renderHook(
      () => useVideoGenerations({}, true, 'task_not_visible'),
      { wrapper: Wrapper }
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100)
    })
    expect(result.current.targetDiscoveryExpired).toBe(false)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(result.current.targetDiscoveryExpired).toBe(true)
    const calls = vi.mocked(getVideoGenerations).mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(9_000)
    })
    expect(getVideoGenerations).toHaveBeenCalledTimes(calls)
    await act(async () => {
      result.current.retryTargetDiscovery()
      await vi.advanceTimersByTimeAsync(100)
    })
    expect(result.current.targetDiscoveryExpired).toBe(false)
    expect(vi.mocked(getVideoGenerations).mock.calls.length).toBeGreaterThan(
      calls
    )
  })

  test('known target lookup works even when the history request fails', async () => {
    vi.mocked(getVideoGenerations).mockRejectedValue(
      new Error('Offline history')
    )
    const { result } = renderHook(
      () => useVideoGenerations({}, true, 'task_7', 7),
      { wrapper: Wrapper }
    )
    await waitFor(() => expect(result.current.targetGeneration?.id).toBe(7))
    expect(result.current.isError).toBe(true)
  })
})

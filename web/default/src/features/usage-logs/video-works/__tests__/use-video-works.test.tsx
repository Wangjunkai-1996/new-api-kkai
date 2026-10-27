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
  QueryClient,
  QueryClientProvider,
  focusManager,
  onlineManager,
} from '@tanstack/react-query'
import { act, cleanup, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { getTaskArtifacts, getVideoWorks } from '../../api'
import type { TaskLog, VideoWorksPage } from '../../types'
import { useSelectedVideoWork } from '../use-selected-video-work'
import { useVideoWorkArtifacts } from '../use-video-work-artifacts'
import { useVideoWorks } from '../use-video-works'

const environment = vi.hoisted(() => ({
  search: {} as Record<string, unknown>,
  tenantId: 0,
  siteError: null as Error | null,
  siteRetry: vi.fn(),
  access: 'admin',
}))
vi.mock('@tanstack/react-router', () => ({
  getRouteApi: () => ({ useSearch: () => environment.search }),
}))
vi.mock('@/features/site-config', () => ({
  getCurrentHost: () => 'test.example',
  useSiteConfig: () => ({
    data: environment.siteError
      ? undefined
      : { tenant_id: environment.tenantId },
    error: environment.siteError,
    refetch: environment.siteRetry,
  }),
}))
vi.mock('../../components/usage-logs-provider', () => ({
  useLogsViewScope: () => ({
    viewAccess: environment.access,
    isAdminView: environment.access !== 'self',
  }),
}))
vi.mock('../../api', () => ({
  getVideoWorks: vi.fn(),
  getTaskArtifacts: vi.fn(),
}))

const log: TaskLog = {
  id: 11,
  user_id: 7,
  task_id: 'task-video',
  platform: 'sora',
  action: 'text_to_video',
  channel_id: 60,
  group: 'default',
  quota: 320000,
  submit_time: 1,
  status: 'SUCCESS',
  billing: { amount_available: true, state: 'completed' },
}
function page(item: TaskLog): VideoWorksPage {
  return {
    items: [item],
    total: 1,
    page: 1,
    page_size: 20,
    status_counts: { all: 1, success: 1, processing: 0, failed: 0, unknown: 0 },
  }
}
const clients: QueryClient[] = []
function wrapper(props: { children: ReactNode }) {
  const client = clients.at(-1)
  if (!client) throw new Error('QueryClient fixture was not initialized')
  return (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
}
async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10)
  })
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-27T02:44:06Z'))
  vi.mocked(getVideoWorks).mockReset().mockResolvedValue(page(log))
  vi.mocked(getTaskArtifacts).mockReset()
  environment.search = {}
  environment.tenantId = 0
  environment.siteError = null
  environment.access = 'admin'
  environment.siteRetry.mockReset()
  useAuthStore.getState().auth.setUser({ id: 7, username: 'user', role: 10 })
  focusManager.setFocused(true)
  onlineManager.setOnline(true)
  clients.push(
    new QueryClient({ defaultOptions: { queries: { retry: false } } })
  )
})
afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
  vi.useRealTimers()
  focusManager.setFocused(undefined)
  onlineManager.setOnline(true)
})

test('polls active generation every 10 seconds, pending billing every 15, then stops at terminal state', async () => {
  vi.mocked(getVideoWorks)
    .mockResolvedValueOnce(
      page({
        ...log,
        status: 'IN_PROGRESS',
        billing: { state: 'reserved', amount_available: true },
      })
    )
    .mockResolvedValueOnce(
      page({ ...log, billing: { state: 'accepted', amount_available: true } })
    )
    .mockResolvedValue(page(log))
  const hook = renderHook(useVideoWorks, { wrapper })
  await settle()
  expect(hook.result.current.items[0].status).toBe('IN_PROGRESS')
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10_000)
  })
  expect(getVideoWorks).toHaveBeenCalledTimes(2)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(15_000)
  })
  expect(getVideoWorks).toHaveBeenCalledTimes(3)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000)
  })
  expect(getVideoWorks).toHaveBeenCalledTimes(3)
})

test('keeps media processing fresh even when generation and billing completed', async () => {
  vi.mocked(getVideoWorks)
    .mockResolvedValueOnce(
      page({ ...log, media: { source: 'studio', state: 'processing' } })
    )
    .mockResolvedValue(page(log))
  renderHook(useVideoWorks, { wrapper })
  await settle()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10_000)
  })
  expect(getVideoWorks).toHaveBeenCalledTimes(2)
})

test('does not show admin cache after scope, user, or tenant changes and omits channel in self queries', async () => {
  environment.search = {
    q: 'sora',
    status_group: 'success',
    channel: '60',
    page: 2,
    pageSize: 20,
    startTime: 100000,
    endTime: 200000,
  }
  const hook = renderHook(useVideoWorks, { wrapper })
  await settle()
  expect(hook.result.current.items[0].id).toBe(11)
  let resolveNext!: (data: VideoWorksPage) => void
  vi.mocked(getVideoWorks).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveNext = resolve
      })
  )
  environment.access = 'self'
  environment.tenantId = 9
  act(() => {
    useAuthStore.getState().auth.setUser({ id: 8, username: 'other', role: 1 })
  })
  hook.rerender()
  expect(hook.result.current.items).toEqual([])
  expect(getVideoWorks).toHaveBeenLastCalledWith(
    expect.objectContaining({
      q: 'sora',
      status_group: 'success',
      channel_id: undefined,
      start_timestamp: 100,
      end_timestamp: 200,
    }),
    false,
    expect.any(AbortSignal)
  )
  await act(async () => {
    resolveNext(page({ ...log, id: 12, user_id: 8 }))
  })
  await settle()
  expect(hook.result.current.items[0].id).toBe(12)
})

test('aborts an in-flight list request when hidden and does not issue background polls', async () => {
  let signal: AbortSignal | undefined
  vi.mocked(getVideoWorks).mockImplementation(
    (_params, _admin, requestSignal) => {
      signal = requestSignal
      return new Promise(() => {})
    }
  )
  renderHook(useVideoWorks, { wrapper })
  await settle()
  act(() => focusManager.setFocused(false))
  await settle()
  expect(signal?.aborted).toBe(true)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000)
  })
  expect(getVideoWorks).toHaveBeenCalledTimes(1)
})

test('backs transient errors off by 20/40/60 seconds and stops on forbidden', async () => {
  vi.mocked(getVideoWorks)
    .mockRejectedValueOnce(new Error('offline'))
    .mockRejectedValueOnce(new Error('offline'))
    .mockRejectedValueOnce(new Error('offline'))
    .mockRejectedValue({ response: { status: 403 } })
  const hook = renderHook(useVideoWorks, { wrapper })
  await settle()
  for (const delay of [20_000, 40_000, 60_000]) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(delay)
    })
  }
  expect(getVideoWorks).toHaveBeenCalledTimes(4)
  expect(hook.result.current.items).toEqual([])
  await act(async () => {
    await vi.advanceTimersByTimeAsync(120_000)
  })
  expect(getVideoWorks).toHaveBeenCalledTimes(4)
})

test('site identity failure is an error with a retry, not permanent loading', async () => {
  environment.siteError = new Error('site unavailable')
  const hook = renderHook(useVideoWorks, { wrapper })
  expect(hook.result.current.isLoading).toBe(false)
  expect(hook.result.current.error).toBe(environment.siteError)
  await hook.result.current.refetch()
  expect(environment.siteRetry).toHaveBeenCalledOnce()
  expect(getVideoWorks).not.toHaveBeenCalled()
})

test('loads a selected artifact lazily and renews an expired hosted URL at most once even when URL text is unchanged', async () => {
  const expires = Math.floor(Date.now() / 1000) + 60
  const hostedVideo = {
    task_record_id: 11,
    state: 'ready' as const,
    content_url: 'https://store.example/video?signature=test',
    download_url: 'https://store.example/video?signature=download',
    expires_at: expires,
  }
  vi.mocked(getTaskArtifacts).mockResolvedValue({ artifacts: [], hostedVideo })
  const hook = renderHook(
    (props: { open: boolean }) => useVideoWorkArtifacts(log, props.open),
    { wrapper, initialProps: { open: false } }
  )
  await settle()
  expect(getTaskArtifacts).not.toHaveBeenCalled()
  hook.rerender({ open: true })
  await settle()
  expect(getTaskArtifacts).toHaveBeenCalledExactlyOnceWith('task-video', {
    recordId: 11,
    signal: expect.any(AbortSignal),
    requireHosted: false,
  })
  expect(await hook.result.current.renewMedia()).toBe(false)
  vi.setSystemTime(new Date(expires * 1000))
  vi.mocked(getTaskArtifacts).mockResolvedValue({
    artifacts: [],
    hostedVideo: { ...hostedVideo, expires_at: expires + 60 },
  })
  let renewed = false
  await act(async () => {
    renewed = await hook.result.current.renewMedia()
  })
  expect(renewed).toBe(true)
  expect(getTaskArtifacts).toHaveBeenCalledTimes(2)
  expect(await hook.result.current.renewMedia()).toBe(false)
})

test('allows unavailable media to retry projection, but never queries restricted media', async () => {
  vi.mocked(getTaskArtifacts).mockResolvedValue({ artifacts: [] })
  const hook = renderHook(
    (props: { state: 'unavailable' | 'restricted' }) =>
      useVideoWorkArtifacts(
        { ...log, media: { source: 'studio', state: props.state } },
        true
      ),
    { wrapper, initialProps: { state: 'restricted' } }
  )
  await settle()
  expect(getTaskArtifacts).not.toHaveBeenCalled()
  hook.rerender({ state: 'unavailable' })
  await settle()
  expect(getTaskArtifacts).toHaveBeenCalledOnce()
})

test('a failed hosted renewal is reported as failure and cannot loop automatically', async () => {
  const expires = Math.floor(Date.now() / 1000) + 60
  vi.mocked(getTaskArtifacts)
    .mockResolvedValueOnce({
      artifacts: [],
      hostedVideo: {
        task_record_id: 11,
        state: 'ready',
        content_url: 'https://store.example/video?signature=test',
        download_url: 'https://store.example/video?signature=download',
        expires_at: expires,
      },
    })
    .mockRejectedValue({ response: { status: 403 } })
  const hook = renderHook(() => useVideoWorkArtifacts(log, true), { wrapper })
  await settle()
  vi.setSystemTime(new Date(expires * 1000))
  let renewed = true
  await act(async () => {
    renewed = await hook.result.current.renewMedia()
  })
  await settle()
  expect(renewed).toBe(false)
  expect(hook.result.current.data).toBeUndefined()
  expect(await hook.result.current.renewMedia()).toBe(false)
  expect(getTaskArtifacts).toHaveBeenCalledTimes(2)
})

test('expired posters reproject once with the same URL text and a renewed lease', async () => {
  const expires = Math.floor(Date.now() / 1000) + 60
  const original = {
    ...log,
    media: {
      source: 'studio' as const,
      state: 'ready' as const,
      poster_url: 'https://store.example/poster?signature=test',
      poster_expires_at: expires,
    },
  }
  vi.mocked(getVideoWorks)
    .mockResolvedValueOnce(page(original))
    .mockResolvedValue(
      page({
        ...original,
        media: { ...original.media, poster_expires_at: expires + 60 },
      })
    )
  const hook = renderHook(useVideoWorks, { wrapper })
  await settle()
  vi.setSystemTime(new Date(expires * 1000))
  let renewed = false
  await act(async () => {
    renewed = await hook.result.current.renewPoster(original)
  })
  expect(renewed).toBe(true)
  expect(await hook.result.current.renewPoster(original)).toBe(false)
  expect(getVideoWorks).toHaveBeenCalledTimes(2)
})

test('honors rate-limit retry-after beyond the normal backoff', async () => {
  vi.mocked(getVideoWorks)
    .mockRejectedValueOnce({
      isAxiosError: true,
      response: { status: 429, headers: { 'retry-after': '90' } },
    })
    .mockResolvedValue(page(log))
  renderHook(useVideoWorks, { wrapper })
  await settle()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000)
  })
  expect(getVideoWorks).toHaveBeenCalledTimes(1)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(30_000)
  })
  expect(getVideoWorks).toHaveBeenCalledTimes(2)
})

test('simultaneous expiry and media errors share one renewal result', async () => {
  const expires = Math.floor(Date.now() / 1000) + 60
  const hostedVideo = {
    task_record_id: 11,
    state: 'ready' as const,
    content_url: 'https://store.example/video?signature=test',
    download_url: 'https://store.example/video?signature=download',
    expires_at: expires,
  }
  vi.mocked(getTaskArtifacts).mockResolvedValueOnce({
    artifacts: [],
    hostedVideo,
  })
  let finishRenewal!: (value: {
    artifacts: []
    hostedVideo: typeof hostedVideo
  }) => void
  vi.mocked(getTaskArtifacts).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finishRenewal = resolve
      })
  )
  const hook = renderHook(() => useVideoWorkArtifacts(log, true), { wrapper })
  await settle()
  vi.setSystemTime(new Date(expires * 1000))
  const first = hook.result.current.renewMedia()
  const concurrent = hook.result.current.renewMedia()
  expect(getTaskArtifacts).toHaveBeenCalledTimes(2)
  let results: boolean[] = []
  await act(async () => {
    finishRenewal({
      artifacts: [],
      hostedVideo: { ...hostedVideo, expires_at: expires + 60 },
    })
    results = await Promise.all([first, concurrent])
  })
  expect(results).toEqual([true, true])
  expect(await hook.result.current.renewMedia()).toBe(false)
})

test('keeps selected details fresh when the record leaves its status filter without querying every visible row', async () => {
  const pending = { ...log, status: 'IN_PROGRESS' }
  const hook = renderHook(
    (props: { items: TaskLog[] }) => useSelectedVideoWork(pending, props.items),
    { wrapper, initialProps: { items: [pending] } }
  )
  await settle()
  expect(getVideoWorks).not.toHaveBeenCalled()
  hook.rerender({ items: [] })
  await settle()
  expect(getVideoWorks).toHaveBeenCalledExactlyOnceWith(
    {
      media_type: 'video',
      task_id: 'task-video',
      status_group: 'all',
      p: 1,
      page_size: 100,
    },
    true,
    expect.any(AbortSignal)
  )
  expect(hook.result.current.log?.status).toBe('SUCCESS')
  expect(hook.result.current.shouldClose).toBe(false)
})

test('never substitutes another numeric record with the same public task ID into selected details', async () => {
  vi.mocked(getVideoWorks).mockResolvedValue(page({ ...log, id: 12 }))
  const hook = renderHook(() => useSelectedVideoWork(log, []), { wrapper })
  await settle()
  expect(hook.result.current.shouldClose).toBe(true)
  expect(hook.result.current.log).toBeNull()
})

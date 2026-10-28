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
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'
import { useVideoStudioDraftStore } from '@/stores/video-studio-draft-store'

import type { VideoTokenGateState } from '../hooks/use-video-token-gate'
import type {
  CreateVideoRequest,
  VideoModelProfile,
  VideoQuoteRequest,
  VideoSample,
} from '../types'
import { VideoComposer } from './video-composer'

const apiMocks = vi.hoisted(() => ({
  quoteVideoGeneration: vi.fn(),
  createVideoGeneration: vi.fn(),
}))

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  ...apiMocks,
  createVideoGeneration: (
    request: CreateVideoRequest,
    key: string,
    beforeDispatch?: () => void
  ) => {
    beforeDispatch?.()
    return apiMocks.createVideoGeneration(request, key)
  },
}))

const models: VideoModelProfile[] = [1, 2].map((id) => ({
  id,
  model: `video-model-${String(id)}`,
  display_name: `Video Model ${String(id)}`,
  description: '',
  provider_label: 'Provider',
  specification_version: 1,
  specification: { version: 1, modes: ['text_to_video'], parameters: [] },
  default_parameters: {},
  enabled: true,
  sort_order: id,
  created_at: 1,
  updated_at: 1,
}))

const samples: VideoSample[] = models.map((profile) => ({
  id: profile.id + 10,
  model_profile_id: profile.id,
  model: profile.model,
  model_display_name: profile.display_name,
  title: `Sample ${String(profile.id)}`,
  prompt: `Prompt for ${profile.model}`,
  mode: 'text_to_video',
  model_version: 1,
  parameters: {},
  reference_asset_ids: [],
  reference_content_urls: [],
  video_asset_id: 1,
  video_url: '',
  poster_url: '',
  preview_url: '',
  aspect_ratio: 16 / 9,
  category: 'other',
  status: 'published',
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
}))

const videoTokenGate: VideoTokenGateState = {
  access: {
    kind: 'ready',
    requiredGroup: 'video',
    tokenId: 17,
    tokenName: 'Video',
  },
  tokenId: 17,
  effectiveGroup: 'video',
  requiredGroup: 'video',
  checking: false,
  preparing: false,
  checkFailed: false,
  gateAction: 'none',
  actionAvailable: false,
  queryFetching: false,
  creating: false,
  createError: null,
  blockAndRecheck: vi.fn(() => false),
  markTokenHealthy: vi.fn(),
  retry: vi.fn(),
  openOrRetry: vi.fn(),
  createAndContinue: vi.fn(async () => undefined),
}

let queryClient: QueryClient

beforeEach(() => {
  localStorage.clear()
  vi.stubGlobal('crypto', webcrypto)
  vi.useFakeTimers()
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
  useAuthStore.getState().auth.setUser({ id: 11, username: 'user-11', role: 1 })
  useVideoStudioDraftStore.getState().clearDraft()
  apiMocks.quoteVideoGeneration.mockImplementation(
    (request: VideoQuoteRequest) =>
      Promise.resolve({
        quota: 100,
        display_amount: '$1.00',
        request_hash: (request.model === models[0].model ? 'a' : 'b').repeat(
          64
        ),
        expires_at: Math.floor(Date.now() / 1000) + 600,
      })
  )
  apiMocks.createVideoGeneration.mockResolvedValue({
    task_id: 'task_video_test',
  })
})

afterEach(() => {
  cleanup()
  queryClient.clear()
  useAuthStore.getState().auth.reset()
  useVideoStudioDraftStore.getState().clearDraft()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

test('quotes and submits the selected sample without mixing the previous model and prompt', async () => {
  const onSubmitted = vi.fn()
  const view = render(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={models}
        sample={samples[0]}
        videoTokenGate={videoTokenGate}
      />
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(300))
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(apiMocks.quoteVideoGeneration).toHaveBeenCalledTimes(1)
  expect(screen.getByRole('button')).toBeEnabled()
  apiMocks.quoteVideoGeneration.mockClear()

  view.rerender(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={models}
        sample={samples[1]}
        videoTokenGate={videoTokenGate}
        onSubmitted={onSubmitted}
      />
    </QueryClientProvider>
  )

  expect(screen.getByRole('alertdialog')).toBeInTheDocument()
  await act(async () =>
    fireEvent.click(screen.getByRole('button', { name: 'Replace settings' }))
  )
  expect(screen.getByRole('textbox')).toHaveValue(samples[1].prompt)
  expect(screen.getByRole('button')).toBeDisabled()
  expect(apiMocks.quoteVideoGeneration).not.toHaveBeenCalled()

  await act(() => vi.advanceTimersByTimeAsync(300))
  await act(() => vi.advanceTimersByTimeAsync(0))
  const expectedRequest: VideoQuoteRequest = {
    token_id: 17,
    model: models[1].model,
    mode: 'text_to_video',
    prompt: samples[1].prompt,
    parameters: {},
    reference_assets: [],
    sample_id: samples[1].id,
  }
  expect(apiMocks.quoteVideoGeneration).toHaveBeenCalledExactlyOnceWith(
    expectedRequest
  )
  expect(screen.getByRole('button')).toBeEnabled()

  await act(async () => {
    fireEvent.click(screen.getByRole('button'))
  })
  expect(apiMocks.createVideoGeneration).toHaveBeenCalledExactlyOnceWith(
    {
      ...expectedRequest,
      max_quota: 100,
      quote_hash: 'b'.repeat(64),
      quote_expires_at: Math.floor(Date.now() / 1000) + 600,
    },
    expect.any(String)
  )
  expect(onSubmitted).toHaveBeenCalledWith({ task_id: 'task_video_test' })
})

test('persists the operation before POST and prevents repeating unknown content after editing away and back', async () => {
  const onSubmitted = vi.fn()
  apiMocks.createVideoGeneration.mockImplementation(() => {
    const raw = localStorage.getItem('video-studio-submissions:user:11')
    expect(raw).not.toBeNull()
    expect(JSON.parse(raw ?? '').records[0].state).toBe('sending')
    return Promise.reject(new Error('connection lost'))
  })
  const view = render(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={models}
        sample={samples[0]}
        videoTokenGate={videoTokenGate}
        onSubmitted={onSubmitted}
      />
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(300))
  await act(() => vi.advanceTimersByTimeAsync(0))
  await act(async () =>
    fireEvent.click(
      screen.getByRole('button', { name: 'videoStudio.generate' })
    )
  )
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(apiMocks.createVideoGeneration).toHaveBeenCalledTimes(1)
  expect(onSubmitted).not.toHaveBeenCalled()
  const input = screen.getByRole('textbox')
  fireEvent.change(input, { target: { value: 'A different scene' } })
  await act(() => vi.advanceTimersByTimeAsync(300))
  fireEvent.change(input, { target: { value: samples[0].prompt } })
  await act(() => vi.advanceTimersByTimeAsync(300))
  await act(() => vi.advanceTimersByTimeAsync(0))
  await act(async () =>
    fireEvent.click(
      screen.getByRole('button', { name: 'videoStudio.generate' })
    )
  )
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(apiMocks.createVideoGeneration).toHaveBeenCalledTimes(1)
  expect(screen.getByRole('alert')).toHaveTextContent('awaiting verification')
  view.unmount()
  render(
    <QueryClientProvider client={queryClient}>
      <VideoComposer models={models} videoTokenGate={videoTokenGate} />
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(300))
  expect(
    JSON.parse(localStorage.getItem('video-studio-submissions:user:11') ?? '')
      .records[0].state
  ).toBe('unknown')
})

test('storage failure blocks POST before any charge can be requested', async () => {
  render(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={models}
        sample={samples[0]}
        videoTokenGate={videoTokenGate}
      />
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(300))
  await act(() => vi.advanceTimersByTimeAsync(0))
  const originalSet = Storage.prototype.setItem
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(
    function (this: Storage, key, value) {
      if (key.startsWith('video-studio-submissions:')) {
        throw new Error('quota exceeded')
      }
      return originalSet.call(this, key, value)
    }
  )
  await act(async () =>
    fireEvent.click(
      screen.getByRole('button', { name: 'videoStudio.generate' })
    )
  )
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(apiMocks.createVideoGeneration).not.toHaveBeenCalled()
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Recovery storage is unavailable'
  )
})

test('an invalid successful receipt remains pending instead of announcing success', async () => {
  apiMocks.createVideoGeneration.mockResolvedValue({
    id: 'task_first',
    task_id: 'task_second',
  })
  const onSubmitted = vi.fn()
  render(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={models}
        sample={samples[0]}
        videoTokenGate={videoTokenGate}
        onSubmitted={onSubmitted}
      />
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(300))
  await act(() => vi.advanceTimersByTimeAsync(0))
  await act(async () =>
    fireEvent.click(
      screen.getByRole('button', { name: 'videoStudio.generate' })
    )
  )
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(onSubmitted).not.toHaveBeenCalled()
  expect(screen.getByRole('alert')).toHaveTextContent('awaiting verification')
  expect(
    JSON.parse(localStorage.getItem('video-studio-submissions:user:11') ?? '')
      .records[0]
  ).toMatchObject({ state: 'unknown' })
})

test('an account switch and return while hashing cancels the unsent operation', async () => {
  let finishDigest: ((digest: ArrayBuffer) => void) | undefined
  vi.spyOn(crypto.subtle, 'digest').mockImplementation(
    () =>
      new Promise<ArrayBuffer>((resolve) => {
        finishDigest = resolve
      })
  )
  render(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={models}
        sample={samples[0]}
        videoTokenGate={videoTokenGate}
      />
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(300))
  await act(() => vi.advanceTimersByTimeAsync(0))
  await act(async () => {
    fireEvent.click(
      screen.getByRole('button', { name: 'videoStudio.generate' })
    )
    const form = screen.getByRole('textbox').closest('form')
    expect(form).not.toBeNull()
    if (form) fireEvent.submit(form)
  })
  expect(crypto.subtle.digest).toHaveBeenCalledTimes(1)
  await act(async () => {
    useAuthStore.getState().auth.reset()
    useAuthStore
      .getState()
      .auth.setUser({ id: 11, username: 'user-11', role: 1 })
    finishDigest?.(new ArrayBuffer(32))
  })
  expect(apiMocks.createVideoGeneration).not.toHaveBeenCalled()
  expect(localStorage.getItem('video-studio-submissions:user:11')).toBeNull()
})

test('sample refetches do not reapply over the edited draft', async () => {
  const view = render(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={models}
        sample={samples[0]}
        videoTokenGate={videoTokenGate}
      />
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(300))
  fireEvent.change(screen.getByRole('textbox'), {
    target: { value: 'My edit' },
  })
  view.rerender(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={[...models]}
        sample={{ ...samples[0] }}
        videoTokenGate={videoTokenGate}
      />
    </QueryClientProvider>
  )
  expect(screen.getByRole('textbox', { hidden: true })).toHaveValue('My edit')
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  view.rerender(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={models}
        sample={samples[0]}
        sampleSelectionId={1}
        videoTokenGate={videoTokenGate}
      />
    </QueryClientProvider>
  )
  expect(screen.getByRole('alertdialog')).toBeInTheDocument()
  expect(screen.getByRole('textbox', { hidden: true })).toHaveValue('My edit')
  await act(async () =>
    fireEvent.click(screen.getByRole('button', { name: 'Replace settings' }))
  )
  expect(screen.getByRole('textbox')).toHaveValue(samples[0].prompt)
})

test.each(['quote expires', 'group changes', 'input changes'])(
  'rechecks the frozen context after hashing: %s',
  async (scenario) => {
    let finishDigest: ((digest: ArrayBuffer) => void) | undefined
    vi.spyOn(crypto.subtle, 'digest').mockImplementation(
      () =>
        new Promise<ArrayBuffer>((resolve) => {
          finishDigest = resolve
        })
    )
    const view = render(
      <QueryClientProvider client={queryClient}>
        <VideoComposer
          models={models}
          sample={samples[0]}
          videoTokenGate={videoTokenGate}
        />
      </QueryClientProvider>
    )
    await act(() => vi.advanceTimersByTimeAsync(300))
    await act(() => vi.advanceTimersByTimeAsync(0))
    await act(async () =>
      fireEvent.click(
        screen.getByRole('button', { name: 'videoStudio.generate' })
      )
    )
    if (scenario === 'quote expires') vi.setSystemTime(Date.now() + 601_000)
    if (scenario === 'group changes') {
      view.rerender(
        <QueryClientProvider client={queryClient}>
          <VideoComposer
            models={models}
            sample={samples[0]}
            videoTokenGate={{ ...videoTokenGate, effectiveGroup: 'changed' }}
          />
        </QueryClientProvider>
      )
    }
    if (scenario === 'input changes') {
      fireEvent.change(screen.getByRole('textbox'), {
        target: { value: 'Changed while preparing' },
      })
    }
    await act(async () => finishDigest?.(new ArrayBuffer(32)))
    expect(apiMocks.createVideoGeneration).not.toHaveBeenCalled()
    expect(localStorage.getItem('video-studio-submissions:user:11')).toBeNull()
    expect(screen.getByRole('alert')).toHaveTextContent(
      'input or access changed'
    )
  }
)

test('late accepted receipts update original evidence without touching a new session UI or cache', async () => {
  let finishRequest: ((receipt: { task_id: string }) => void) | undefined
  apiMocks.createVideoGeneration.mockImplementation(
    () =>
      new Promise<{ task_id: string }>((resolve) => {
        finishRequest = resolve
      })
  )
  const onSubmitted = vi.fn()
  render(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={models}
        sample={samples[0]}
        videoTokenGate={videoTokenGate}
        onSubmitted={onSubmitted}
      />
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(300))
  await act(() => vi.advanceTimersByTimeAsync(0))
  await act(async () =>
    fireEvent.click(
      screen.getByRole('button', { name: 'videoStudio.generate' })
    )
  )
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(apiMocks.createVideoGeneration).toHaveBeenCalledTimes(1)
  const invalidation = vi.spyOn(queryClient, 'invalidateQueries')
  await act(async () => {
    useAuthStore.getState().auth.reset()
    useAuthStore
      .getState()
      .auth.setUser({ id: 11, username: 'user-11', role: 1 })
    finishRequest?.({ task_id: 'task_late' })
  })
  expect(onSubmitted).not.toHaveBeenCalled()
  expect(invalidation).not.toHaveBeenCalled()
  expect(
    JSON.parse(localStorage.getItem('video-studio-submissions:user:11') ?? '')
      .records[0]
  ).toMatchObject({ state: 'accepted', taskId: 'task_late' })
})

test('leaving the page preserves evidence without running the old completion callback', async () => {
  let finishRequest: ((receipt: { task_id: string }) => void) | undefined
  apiMocks.createVideoGeneration.mockImplementation(
    () =>
      new Promise<{ task_id: string }>((resolve) => {
        finishRequest = resolve
      })
  )
  const onSubmitted = vi.fn()
  const view = render(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={models}
        sample={samples[0]}
        videoTokenGate={videoTokenGate}
        onSubmitted={onSubmitted}
      />
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(300))
  await act(() => vi.advanceTimersByTimeAsync(0))
  await act(async () =>
    fireEvent.click(
      screen.getByRole('button', { name: 'videoStudio.generate' })
    )
  )
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(apiMocks.createVideoGeneration).toHaveBeenCalledTimes(1)
  view.unmount()
  await act(async () => finishRequest?.({ task_id: 'task_after_leaving' }))
  expect(onSubmitted).not.toHaveBeenCalled()
  expect(
    JSON.parse(localStorage.getItem('video-studio-submissions:user:11') ?? '')
      .records[0]
  ).toMatchObject({ state: 'accepted', taskId: 'task_after_leaving' })
})

test('expired quote responses show an explicit refresh action and never loop or enable generation', async () => {
  const expiredQuote = {
    quota: 100,
    display_amount: '$1.00',
    request_hash: 'a'.repeat(64),
    expires_at: Math.floor(Date.now() / 1000) - 10,
  }
  apiMocks.quoteVideoGeneration.mockResolvedValue(expiredQuote)
  render(
    <QueryClientProvider client={queryClient}>
      <VideoComposer
        models={models}
        sample={samples[0]}
        videoTokenGate={videoTokenGate}
      />
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(300))
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(
    screen.getByRole('button', { name: 'videoStudio.generate' })
  ).toBeDisabled()
  expect(screen.getByText('videoStudio.quoteUnavailable')).toBeInTheDocument()
  expect(
    screen.queryByText('Describe your scene to get a quote.')
  ).not.toBeInTheDocument()
  expect(apiMocks.quoteVideoGeneration).toHaveBeenCalledTimes(1)
  await act(async () =>
    fireEvent.click(screen.getByRole('button', { name: 'Refresh quote' }))
  )
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(apiMocks.quoteVideoGeneration).toHaveBeenCalledTimes(2)
  expect(screen.getByText('videoStudio.quoteUnavailable')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Refresh quote' })).toBeEnabled()
  apiMocks.quoteVideoGeneration.mockRejectedValue(new Error('offline'))
  await act(async () =>
    fireEvent.click(screen.getByRole('button', { name: 'Refresh quote' }))
  )
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(apiMocks.quoteVideoGeneration).toHaveBeenCalledTimes(3)
  expect(screen.getByText('videoStudio.quoteUnavailable')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Refresh quote' })).toBeEnabled()
  expect(apiMocks.createVideoGeneration).not.toHaveBeenCalled()
})

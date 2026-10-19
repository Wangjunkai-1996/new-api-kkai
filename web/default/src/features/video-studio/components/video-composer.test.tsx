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
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'
import { useVideoStudioDraftStore } from '@/stores/video-studio-draft-store'

import type { VideoTokenGateState } from '../hooks/use-video-token-gate'
import type {
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

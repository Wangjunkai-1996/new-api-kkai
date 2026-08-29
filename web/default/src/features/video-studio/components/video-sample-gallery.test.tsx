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
import { render, waitFor } from '@testing-library/react'
import { AxiosError } from 'axios'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { VideoSample } from '../types'
import { VideoSampleGallery } from './video-sample-gallery'

const mocks = vi.hoisted(() => ({
  fetchNextPage: vi.fn(),
  onTokenError: vi.fn(),
  query: {} as Record<string, unknown>,
  refetch: vi.fn(),
  virtualizer: {
    getTotalSize: vi.fn(() => 0),
    getVirtualItems: vi.fn(() => []),
    measure: vi.fn(),
    measureElement: vi.fn(),
  },
}))

vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: () => mocks.virtualizer,
}))

vi.mock('../queries', () => ({
  useVideoSamples: () => mocks.query,
}))

const sample: VideoSample = {
  id: 1,
  model_profile_id: 1,
  model: 'video-model',
  model_display_name: 'Video Model',
  title: 'Sample',
  prompt: 'Prompt',
  mode: 'text_to_video',
  model_version: 1,
  parameters: {},
  reference_asset_ids: [],
  reference_content_urls: [],
  video_asset_id: 1,
  video_url: '/sample.mp4',
  poster_url: '/sample.jpg',
  preview_url: '/sample.mp4',
  aspect_ratio: 1,
  category: 'other',
  status: 'published',
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
}

const setSamplesQuery = (input: {
  error: unknown
  initialError?: boolean
  nextPageError?: boolean
}) => {
  Object.assign(mocks.query, {
    data: input.nextPageError
      ? { pages: [{ items: [sample] }], pageParams: [undefined] }
      : undefined,
    error: input.error,
    fetchNextPage: mocks.fetchNextPage,
    hasNextPage: false,
    isError: input.initialError ?? false,
    isFetchNextPageError: input.nextPageError ?? false,
    isFetchingNextPage: false,
    isLoading: false,
    refetch: mocks.refetch,
  })
}

const tokenInvalidError = () => {
  const error = new AxiosError('invalid video token')
  Object.assign(error, {
    response: { data: { code: 'video_token_invalid' } },
  })
  return error
}

describe('video sample token recovery', () => {
  beforeEach(() => {
    mocks.onTokenError.mockReturnValue(true)
  })

  test.each([
    { name: 'initial request', initialError: true },
    { name: 'next page', nextPageError: true },
  ])('reports a token-invalid $name failure', async (scenario) => {
    setSamplesQuery({ error: tokenInvalidError(), ...scenario })

    render(
      <VideoSampleGallery
        models={[]}
        tokenId={17}
        onTokenError={mocks.onTokenError}
        onTrySample={() => undefined}
      />
    )

    await waitFor(() => {
      expect(mocks.onTokenError).toHaveBeenCalledWith('token-invalid')
    })
  })

  test('reports a generic network failure without classifying it as token-invalid', async () => {
    mocks.onTokenError.mockReturnValue(false)
    setSamplesQuery({
      error: new Error('network unavailable'),
      initialError: true,
    })

    render(
      <VideoSampleGallery
        models={[]}
        tokenId={17}
        onTokenError={mocks.onTokenError}
        onTrySample={() => undefined}
      />
    )

    await waitFor(() => {
      expect(mocks.onTokenError).toHaveBeenCalledWith('request-failed')
    })
  })
})

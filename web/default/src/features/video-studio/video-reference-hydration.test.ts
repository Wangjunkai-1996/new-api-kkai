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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { VideoAsset } from './types'
import {
  getHydratedVideoReferences,
  getVideoReferenceHydrationRecovery,
  isUnavailableVideoReferenceError,
  shouldRetryVideoReferenceHydration,
} from './video-reference-hydration'

const axiosError = (status: number) => ({
  isAxiosError: true,
  response: { status },
})

const asset = (id: number): VideoAsset => ({
  id,
  scope: 'user',
  kind: 'reference',
  state: 'ready',
  original_filename: `reference-${id}.png`,
  mime_type: 'image/png',
  size_bytes: 1,
  width: 1,
  height: 1,
  duration_seconds: 0,
  codec: '',
  content_url: `/api/video-studio/assets/${id}/content`,
  created_at: 1,
  updated_at: 1,
})

describe('video reference hydration recovery', () => {
  for (const status of [403, 404, 410]) {
    test(`treats HTTP ${status} as a permanently unavailable reference`, () => {
      assert.equal(isUnavailableVideoReferenceError(axiosError(status)), true)
    })
  }

  test('keeps transient failures retryable', () => {
    assert.equal(isUnavailableVideoReferenceError(axiosError(500)), false)
    assert.equal(isUnavailableVideoReferenceError(new Error('offline')), false)
  })

  test('skips retries for unavailable references and bounds transient retries', () => {
    assert.equal(shouldRetryVideoReferenceHydration(0, axiosError(404)), false)
    assert.equal(shouldRetryVideoReferenceHydration(2, axiosError(503)), true)
    assert.equal(shouldRetryVideoReferenceHydration(3, axiosError(503)), false)
  })

  test('does not truncate a valid reference after a permanently unavailable one', () => {
    assert.equal(
      getVideoReferenceHydrationRecovery(
        [11, 12],
        [
          {
            error: axiosError(404),
            isFetchedAfterMount: true,
            isFetching: false,
          },
          {
            data: asset(12),
            error: null,
            isFetchedAfterMount: true,
            isFetching: false,
          },
        ]
      ),
      null
    )
  })

  test('does not truncate a transiently failed suffix after a permanently unavailable reference', () => {
    assert.equal(
      getVideoReferenceHydrationRecovery(
        [11, 12],
        [
          {
            error: axiosError(404),
            isFetchedAfterMount: true,
            isFetching: false,
          },
          {
            error: axiosError(503),
            isFetchedAfterMount: true,
            isFetching: false,
          },
        ]
      ),
      null
    )
  })

  test('does not truncate while a suffix reference is still loading', () => {
    assert.equal(
      getVideoReferenceHydrationRecovery(
        [11, 12],
        [
          {
            error: axiosError(404),
            isFetchedAfterMount: true,
            isFetching: false,
          },
          {
            error: null,
            isFetchedAfterMount: false,
            isFetching: true,
          },
        ]
      ),
      null
    )
  })

  test('removes a suffix only after every suffix reference is permanently unavailable', () => {
    assert.deepEqual(
      getVideoReferenceHydrationRecovery(
        [11, 12, 13],
        [
          {
            data: asset(11),
            error: null,
            isFetchedAfterMount: true,
            isFetching: false,
          },
          {
            error: axiosError(404),
            isFetchedAfterMount: true,
            isFetching: false,
          },
          {
            error: axiosError(410),
            isFetchedAfterMount: true,
            isFetching: false,
          },
        ]
      ),
      { retainedAssetIds: [11], retainedAssets: [asset(11)] }
    )
  })

  test('retains a hydrated first frame when only the last frame is unavailable', () => {
    assert.deepEqual(
      getVideoReferenceHydrationRecovery(
        [11, 12],
        [
          {
            data: asset(11),
            error: null,
            isFetchedAfterMount: true,
            isFetching: false,
          },
          {
            error: axiosError(403),
            isFetchedAfterMount: true,
            isFetching: false,
          },
        ]
      ),
      { retainedAssetIds: [11], retainedAssets: [asset(11)] }
    )
  })

  for (const status of [403, 404]) {
    test(`rejects stale cached data after background HTTP ${status} validation`, () => {
      const staleResult = {
        data: asset(11),
        error: axiosError(status),
        isFetchedAfterMount: true,
        isFetching: false,
      }

      assert.equal(getHydratedVideoReferences([11], [staleResult]), null)
      assert.deepEqual(
        getVideoReferenceHydrationRecovery([11], [staleResult]),
        { retainedAssetIds: [], retainedAssets: [] }
      )
    })
  }

  test('does not offer removal while an earlier reference is still loading', () => {
    assert.equal(
      getVideoReferenceHydrationRecovery(
        [11, 12],
        [
          {
            error: null,
            isFetchedAfterMount: false,
            isFetching: false,
          },
          {
            error: axiosError(404),
            isFetchedAfterMount: true,
            isFetching: false,
          },
        ]
      ),
      null
    )
  })

  test('accepts cached references only after background validation succeeds', () => {
    assert.equal(
      getHydratedVideoReferences(
        [11],
        [
          {
            data: asset(11),
            error: null,
            isFetchedAfterMount: false,
            isFetching: true,
          },
        ]
      ),
      null
    )
    assert.equal(
      getHydratedVideoReferences(
        [11],
        [
          {
            data: asset(11),
            error: null,
            isFetchedAfterMount: false,
            isFetching: false,
          },
        ]
      ),
      null
    )
    assert.deepEqual(
      getHydratedVideoReferences(
        [11],
        [
          {
            data: asset(11),
            error: null,
            isFetchedAfterMount: true,
            isFetching: false,
          },
        ]
      ),
      [asset(11)]
    )
  })

  test('does not offer removal for transient hydration failures', () => {
    assert.equal(
      getVideoReferenceHydrationRecovery(
        [11],
        [
          {
            error: axiosError(503),
            isFetchedAfterMount: true,
            isFetching: false,
          },
        ]
      ),
      null
    )
  })
})

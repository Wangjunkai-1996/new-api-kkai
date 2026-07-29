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

import {
  reduceVideoSamplePlayback,
  releaseVideoSamplePreview,
} from './video-sample-card'
import { reduceVideoSamplePreviewRegistry } from './video-sample-gallery'

const previewRegistryInitialState = {
  activeId: null,
  warmedIds: [] as readonly number[],
}

const playbackInitialState = {
  hasPlayingFrame: false,
  loading: false,
  error: false,
}

describe('video sample preview registry', () => {
  test('warms immediately without starting playback and retains only two previews', () => {
    const first = reduceVideoSamplePreviewRegistry(
      previewRegistryInitialState,
      { type: 'warm', id: 1 }
    )
    const second = reduceVideoSamplePreviewRegistry(first, {
      type: 'warm',
      id: 2,
    })
    const third = reduceVideoSamplePreviewRegistry(second, {
      type: 'warm',
      id: 3,
    })

    assert.equal(third.activeId, null)
    assert.deepEqual(third.warmedIds, [3, 2])
  })

  test('keeps one active preview and ignores a stale stop from an older card', () => {
    const first = reduceVideoSamplePreviewRegistry(
      previewRegistryInitialState,
      { type: 'start', id: 1 }
    )
    const second = reduceVideoSamplePreviewRegistry(first, {
      type: 'start',
      id: 2,
    })
    const staleStop = reduceVideoSamplePreviewRegistry(second, {
      type: 'stop',
      id: 1,
    })

    assert.equal(staleStop.activeId, 2)
    assert.deepEqual(staleStop.warmedIds, [2, 1])
  })

  test('stops playback without discarding the warmed video instance', () => {
    const active = reduceVideoSamplePreviewRegistry(
      previewRegistryInitialState,
      { type: 'start', id: 1 }
    )
    const stopped = reduceVideoSamplePreviewRegistry(active, {
      type: 'stop',
      id: 1,
    })

    assert.equal(stopped.activeId, null)
    assert.deepEqual(stopped.warmedIds, [1])
  })

  test('keeps the active video mounted while another card is only warming', () => {
    const active = reduceVideoSamplePreviewRegistry(
      previewRegistryInitialState,
      { type: 'start', id: 1 }
    )
    const second = reduceVideoSamplePreviewRegistry(active, {
      type: 'warm',
      id: 2,
    })
    const third = reduceVideoSamplePreviewRegistry(second, {
      type: 'warm',
      id: 3,
    })

    assert.equal(third.activeId, 1)
    assert.deepEqual(third.warmedIds, [3, 1])
  })
})

describe('video sample playback state', () => {
  test('keeps the poster visible until the video actually starts playing', () => {
    const loading = reduceVideoSamplePlayback(playbackInitialState, {
      type: 'activate',
    })
    const playing = reduceVideoSamplePlayback(loading, { type: 'playing' })

    assert.equal(loading.hasPlayingFrame, false)
    assert.equal(loading.loading, true)
    assert.equal(playing.hasPlayingFrame, true)
    assert.equal(playing.loading, false)
  })

  test('retains an already displayed frame while buffering and restores the poster on error', () => {
    const playing = reduceVideoSamplePlayback(playbackInitialState, {
      type: 'playing',
    })
    const waiting = reduceVideoSamplePlayback(playing, { type: 'waiting' })
    const failed = reduceVideoSamplePlayback(waiting, { type: 'error' })

    assert.equal(waiting.hasPlayingFrame, true)
    assert.equal(waiting.loading, true)
    assert.equal(failed.hasPlayingFrame, false)
    assert.equal(failed.error, true)
  })

  test('releases the media request when a warmed preview is evicted', () => {
    const calls: string[] = []
    releaseVideoSamplePreview({
      pause: () => calls.push('pause'),
      removeAttribute: (name) => calls.push(`remove:${name}`),
      load: () => calls.push('load'),
    })

    assert.deepEqual(calls, ['pause', 'remove:src', 'load'])
  })
})

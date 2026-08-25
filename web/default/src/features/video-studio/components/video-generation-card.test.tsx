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

import i18next, { type i18n } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, test } from 'vitest'

import type { VideoGeneration } from '../types'
import { VideoGenerationCard } from './video-generation-card'

let translations: i18n

beforeAll(async () => {
  translations = i18next.createInstance()
  await translations.init({
    lng: 'en',
    keySeparator: false,
    resources: {
      en: {
        translation: {
          Close: 'Close',
          'Loading...': 'Loading...',
          Retry: 'Retry',
          'videoStudio.delete': 'Delete',
          'videoStudio.download': 'Download',
          'videoStudio.failure.contentPolicy':
            'The request was blocked by the content policy.',
          'videoStudio.failure.copyrightRestriction':
            'The generated video may involve copyrighted content.',
          'videoStudio.failure.generic': 'Video generation failed.',
          'videoStudio.failure.privacyRestriction':
            'The content may include real-person or private information.',
          'videoStudio.play': 'Play',
          'videoStudio.status.archiving': 'Archiving',
          'videoStudio.status.failed': 'Failed',
          'videoStudio.status.processing': 'Processing',
          'videoStudio.status.queued': 'Queued',
          'videoStudio.status.ready': 'Ready',
        },
      },
    },
  })
})

const generation = (
  overrides: Partial<VideoGeneration> = {}
): VideoGeneration => ({
  id: 1,
  task_id: 'task_test',
  model_profile_id: 1,
  model: 'video-model',
  mode: 'text_to_video',
  prompt: 'A test video',
  parameters: {},
  status: 'ready',
  progress: '100%',
  quota: 1,
  video_url: '/video.mp4',
  poster_url: '/poster.jpg',
  download_url: '/download',
  created_at: 1,
  updated_at: 1,
  ...overrides,
})

const renderCard = (item: VideoGeneration, playing: boolean): string =>
  renderToStaticMarkup(
    <I18nextProvider i18n={translations}>
      <VideoGenerationCard
        generation={item}
        playing={playing}
        onPlay={() => undefined}
        onClose={() => undefined}
        onDelete={() => undefined}
      />
    </I18nextProvider>
  )

describe('video generation card media lifecycle', () => {
  test('renders only a lazy poster until the user starts playback', () => {
    const html = renderCard(generation(), false)

    assert.equal(html.includes('<video'), false)
    assert.match(html, /loading="lazy"/)
    assert.match(html, /aria-label="Play"/)
  })

  test('mounts exactly one player for the selected ready card', () => {
    const html = renderCard(generation(), true)

    assert.equal(html.match(/<video/g)?.length, 1)
    assert.match(html, /preload="metadata"/)
  })

  test('does not expose deletion as cancellation for active work', () => {
    const html = renderCard(
      generation({
        status: 'processing',
        progress: '30%',
        video_url: undefined,
        poster_url: undefined,
        download_url: undefined,
      }),
      false
    )

    assert.doesNotMatch(html, /aria-label="Delete"/)
    assert.match(html, /aria-valuenow="30"/)
  })

  test('renders a safe actionable reason for known generation failures', () => {
    const html = renderCard(
      generation({
        status: 'failed',
        failure_code: 'copyright_restriction',
        failure_reason: 'video generation failed',
        video_url: undefined,
        poster_url: undefined,
        download_url: undefined,
      }),
      false
    )

    assert.match(html, /The generated video may involve copyrighted content\./)
    assert.doesNotMatch(html, /video generation failed/)
  })
})

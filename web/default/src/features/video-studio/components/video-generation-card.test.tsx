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
import i18next, { type i18n } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, test } from 'vitest'

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

const renderCard = (item: VideoGeneration): string =>
  renderToStaticMarkup(
    <I18nextProvider i18n={translations}>
      <VideoGenerationCard
        generation={item}
        onOpen={() => undefined}
        onDelete={() => undefined}
      />
    </I18nextProvider>
  )

describe('video generation card', () => {
  test('renders only a lazy poster and a labeled details entry, never a hidden player', () => {
    const html = renderCard(generation())
    expect(html).not.toContain('<video')
    expect(html).toContain('loading="lazy"')
    expect(html).toContain('aria-label="View video details: A test video"')
  })

  test('does not expose deletion as cancellation for active work', () => {
    const html = renderCard(
      generation({
        status: 'processing',
        progress: '30%',
        video_url: undefined,
        poster_url: undefined,
        download_url: undefined,
      })
    )
    expect(html).not.toContain('aria-label="Delete"')
    expect(html).toContain('Generating video')
  })

  test('renders the safe public failure instead of the upstream raw reason', () => {
    const html = renderCard(
      generation({
        status: 'failed',
        failure_code: 'copyright_restriction',
        failure_reason: 'video generation failed',
        video_url: undefined,
        poster_url: undefined,
        download_url: undefined,
      })
    )
    expect(html).toContain(
      'The generated video may involve copyrighted content.'
    )
    expect(html).not.toContain('video generation failed')
    expect(html).toContain('aria-label="Delete"')
  })
})

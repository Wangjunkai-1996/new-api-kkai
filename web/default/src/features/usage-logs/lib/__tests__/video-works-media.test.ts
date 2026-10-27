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
import { describe, expect, test } from 'vitest'

import {
  getTaskArtifactDownloadUrl,
  parseTaskArtifactsResponse,
  parseTaskMediaSummary,
} from '../task-artifacts'

const contentUrl = `https://media.example/v1/tasks/task-public/artifacts/main/content?access=${'a'.repeat(43)}`
const signedUrl = 'https://storage.example/output.mp4?X-Amz-Signature=fixture'
const hosted = {
  task_record_id: 11,
  state: 'ready',
  content_url: signedUrl,
  download_url: `${signedUrl}&response-content-disposition=attachment`,
  expires_at: 2_000_000_000,
}

function project(hostedVideo: unknown, expectedRecordId = 11) {
  return parseTaskArtifactsResponse(
    { success: true, data: { hosted_video: hostedVideo } },
    expectedRecordId
  )
}

describe('video work media contracts', () => {
  test('constructs one attachment query without weakening task capability validation', () => {
    expect(getTaskArtifactDownloadUrl(contentUrl)).toBe(
      `${contentUrl}&download=1`
    )
    expect(getTaskArtifactDownloadUrl(`${contentUrl}&download=1`)).toBe(
      `${contentUrl}&download=1`
    )
    expect(() => getTaskArtifactDownloadUrl(signedUrl)).toThrow(
      'invalid_content_url'
    )
    expect(() =>
      getTaskArtifactDownloadUrl(`${contentUrl}&filename=private`)
    ).toThrow()
    expect(() =>
      getTaskArtifactDownloadUrl(`${contentUrl}&access=duplicate`)
    ).toThrow()
  })

  test('accepts hosted storage signatures only in the independent hosted contract', () => {
    expect(project(hosted).hostedVideo).toEqual(hosted)
    expect(
      project({ task_record_id: 11, state: 'restricted' }).hostedVideo
    ).toEqual({ task_record_id: 11, state: 'restricted' })
    expect(() => project(hosted, 12)).toThrow('invalid_hosted_video_identity')
    expect(() => project({ ...hosted, task_record_id: '11' })).toThrow()
    expect(() => project({ ...hosted, state: 'processing' })).toThrow(
      'invalid_hosted_video_state'
    )
    expect(() => project({ ...hosted, expires_at: 0 })).toThrow()
    expect(() => project({ ...hosted, download_url: undefined })).toThrow()
    expect(() =>
      parseTaskArtifactsResponse({
        success: true,
        data: {
          artifacts: [{ key: 'video', type: 'video', content_url: signedUrl }],
        },
      })
    ).toThrow()
  })

  test.each([
    'javascript:alert(1)',
    '//storage.example/video.mp4',
    'https://user:secret@storage.example/video.mp4',
    'https://storage.example/video.mp4#fragment',
    'https://storage.example/\\video.mp4',
    `${signedUrl}\n`,
  ])('rejects unsafe hosted URLs: %s', (url) => {
    expect(() => project({ ...hosted, content_url: url })).toThrow()
  })

  test('requires paired poster expiry and safe ready media without fabricating dimensions', () => {
    expect(
      parseTaskMediaSummary({
        source: 'studio',
        state: 'ready',
        poster_url: signedUrl,
        poster_expires_at: 2_000_000_000,
        width: 1280,
        height: 720,
        duration_seconds: 8,
      })
    ).toMatchObject({ width: 1280, height: 720, duration_seconds: 8 })
    expect(
      parseTaskMediaSummary({ source: 'studio', state: 'processing' })
    ).toEqual({ source: 'studio', state: 'processing' })
    expect(() =>
      parseTaskMediaSummary({
        source: 'studio',
        state: 'ready',
        poster_url: signedUrl,
      })
    ).toThrow()
    expect(() =>
      parseTaskMediaSummary({
        source: 'studio',
        state: 'restricted',
        poster_url: signedUrl,
        poster_expires_at: 2_000_000_000,
      })
    ).toThrow()
    expect(() =>
      parseTaskMediaSummary({
        source: 'studio',
        state: 'ready',
        duration_seconds: -8,
      })
    ).toThrow()
  })

  test('keeps a poster attached to its own output video and rejects unsafe cover URLs', () => {
    const projection = parseTaskArtifactsResponse({
      success: true,
      data: {
        artifacts: [
          {
            key: 'second-video',
            type: 'video',
            content_url: contentUrl,
            poster: {
              state: 'ready',
              url: signedUrl,
              expires_at: 2_000_000_000,
              artifact_key: 'second-video',
            },
          },
        ],
      },
    })
    expect(projection.artifacts[0].poster).toEqual({
      state: 'ready',
      url: signedUrl,
      expires_at: 2_000_000_000,
      artifact_key: 'second-video',
    })
    expect(() =>
      parseTaskArtifactsResponse({
        success: true,
        data: {
          artifacts: [
            {
              key: 'second-video',
              type: 'video',
              content_url: contentUrl,
              poster: {
                state: 'ready',
                url: 'javascript:alert(1)',
                expires_at: 2_000_000_000,
              },
            },
          ],
        },
      })
    ).toThrow('invalid_content_url')
  })

  test('does not expose an upstream fallback alongside a hosted result', () => {
    expect(() =>
      parseTaskArtifactsResponse(
        {
          success: true,
          data: { hosted_video: hosted, legacy_content_url: contentUrl },
        },
        11
      )
    ).toThrow('ambiguous_hosted_video_response')
  })
})

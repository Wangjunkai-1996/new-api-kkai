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

import { getImageGenerationErrorKey } from './image-generation-error'

describe('image generation user messages', () => {
  test.each([
    [
      'upstream_content_policy',
      'This generation was blocked by a content safety check. The check may apply to the prompt, reference image, or generated result.',
    ],
    [
      'image_service_unavailable',
      'Image generation is temporarily unavailable. Try another model or try again later.',
    ],
    [
      'image_rate_limited',
      'The image service is busy. Please try again later.',
    ],
    [
      'image_request_timeout',
      'Image generation timed out. Please try again later.',
    ],
    [
      'image_queue_expired',
      'Image generation timed out. Please try again later.',
    ],
    [
      'image_invalid_request',
      'The image settings are not supported. Adjust the settings and try again.',
    ],
    [
      'image_reference_invalid',
      'The reference image could not be processed. Replace it and try again.',
    ],
    [
      'image_result_unavailable',
      'The image results could not be delivered. Please contact support.',
    ],
    [
      'insufficient_user_quota',
      'Your account quota is insufficient. Top up and try again.',
    ],
    [
      'image_generation_failed',
      'Image generation could not be completed. Please try again later or contact support.',
    ],
    [
      'unknown-provider-code',
      'Image generation could not be completed. Please try again later or contact support.',
    ],
  ])('maps %s to a controlled user message', (code, message) => {
    expect(
      getImageGenerationErrorKey({ status: 'failed', error_code: code })
    ).toBe(message)
  })

  test('does not claim an uncertain request failed or invite resubmission', () => {
    expect(
      getImageGenerationErrorKey({
        status: 'unknown',
        error_code: 'image_request_timeout',
      })
    ).toBe(
      'The generation result could not be confirmed. Please contact support before submitting again.'
    )
  })

  test('distinguishes partial results from confirmed archival failures', () => {
    expect(getImageGenerationErrorKey({ status: 'partial' })).toBe(
      'Only some requested images are available. You can view the completed images.'
    )
    expect(
      getImageGenerationErrorKey({
        status: 'partial',
        error_code: 'partial_archive',
      })
    ).toBe('Some generated images could not be saved. Please contact support.')
    expect(getImageGenerationErrorKey({ status: 'archive_failed' })).toBe(
      'The image results could not be delivered. Please contact support.'
    )
  })

  test.each(['submitting', 'succeeded'] as const)(
    'does not display stale errors on %s tasks',
    (status) => {
      expect(
        getImageGenerationErrorKey({
          status,
          error_code: 'upstream_content_policy',
        })
      ).toBeNull()
    }
  )
})

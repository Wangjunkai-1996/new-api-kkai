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
import type { ImageGeneration } from './types'

export function getImageGenerationErrorKey(
  generation: Pick<ImageGeneration, 'status' | 'error_code'>
): string | null {
  if (generation.status === 'unknown') {
    return 'The generation result could not be confirmed. Please contact support before submitting again.'
  }
  if (generation.status === 'submitting' || generation.status === 'succeeded') {
    return null
  }

  switch (generation.error_code?.trim().toLowerCase()) {
    case 'upstream_content_policy':
    case 'content_policy':
    case 'content_policy_violation':
    case 'prompt_blocked':
      return 'This generation was blocked by a content safety check. The check may apply to the prompt, reference image, or generated result.'
    case 'image_service_unavailable':
      return 'Image generation is temporarily unavailable. Try another model or try again later.'
    case 'image_rate_limited':
      return 'The image service is busy. Please try again later.'
    case 'image_request_timeout':
    case 'image_queue_expired':
      return 'Image generation timed out. Please try again later.'
    case 'image_invalid_request':
      return 'The image settings are not supported. Adjust the settings and try again.'
    case 'image_reference_invalid':
      return 'The reference image could not be processed. Replace it and try again.'
    case 'image_result_unavailable':
    case 'partial_archive':
    case 'archive_failed':
      return generation.status === 'partial'
        ? 'Some generated images could not be saved. Please contact support.'
        : 'The image results could not be delivered. Please contact support.'
    case 'insufficient_user_quota':
      return 'Your account quota is insufficient. Top up and try again.'
    case 'image_partial_result':
      return 'Only some requested images are available. You can view the completed images.'
    default:
      if (generation.status === 'archive_failed') {
        return 'The image results could not be delivered. Please contact support.'
      }
      if (generation.status === 'partial') {
        return 'Only some requested images are available. You can view the completed images.'
      }
      return 'Image generation could not be completed. Please try again later or contact support.'
  }
}

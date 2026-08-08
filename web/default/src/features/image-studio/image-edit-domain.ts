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
import { isAxiosError } from 'axios'

import type {
  ImageEditQuoteRequest,
  ImageModelProfile,
  ImageQuoteRequest,
  ImageReferenceMetadata,
  ImageStudioApiError,
} from './types'

export const IMAGE_STUDIO_EDIT_MODEL = 'gpt-image-2'
export const IMAGE_STUDIO_MAX_REFERENCE_IMAGES = 4

export const findImageEditProfile = (
  profiles: ImageModelProfile[] | undefined
): ImageModelProfile | undefined =>
  profiles?.find((profile) => profile.model === IMAGE_STUDIO_EDIT_MODEL)

export const getImageProfileMaxReferenceImages = (
  profile: ImageModelProfile | undefined
): number => {
  const configured = profile?.specification.max_reference_images
  if (typeof configured !== 'number' || !Number.isInteger(configured)) return 1
  if (configured <= 0) return 1
  return Math.min(configured, IMAGE_STUDIO_MAX_REFERENCE_IMAGES)
}

export const isImageEditQuoteRequest = (
  request: ImageQuoteRequest | ImageEditQuoteRequest
): request is ImageEditQuoteRequest =>
  'reference' in request || 'references' in request

export const buildImageEditQuoteRequest = (
  request: ImageQuoteRequest,
  references: readonly ImageReferenceMetadata[]
): ImageEditQuoteRequest | null => {
  const firstReference = references.at(0)
  if (!firstReference) return null
  if (references.length === 1) return { ...request, reference: firstReference }
  return { ...request, references: [...references] }
}

export const isImageQuoteStaleError = (
  status: number | undefined,
  responseError?: ImageStudioApiError
): boolean => status === 409 && responseError?.code === 'quote_stale'

export const isImageQuoteStaleResponse = (error: unknown): boolean =>
  isAxiosError<ImageStudioApiError>(error) &&
  isImageQuoteStaleError(error.response?.status, error.response?.data)

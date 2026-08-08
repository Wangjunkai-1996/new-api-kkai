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
import { z } from 'zod'

import { api } from '@/lib/api'

import {
  imageAssetSchema,
  imageGenerationSchema,
  imageModelProfileSchema,
  imageQuoteSchema,
  imageSampleSchema,
  imageTokenCapabilitySchema,
  imageTokenCreateResultSchema,
} from './schemas'
import type {
  CreateImageEditRequest,
  CreateImageRequest,
  CursorPage,
  ImageEditQuoteRequest,
  ImageAsset,
  ImageGeneration,
  ImageGenerationStatus,
  ImageModelProfile,
  ImageModelProfileInput,
  ImageQuote,
  ImageQuoteRequest,
  ImageSample,
  ImageSampleInput,
  ImageTokenCapability,
  ImageTokenCreateResult,
} from './types'

type ApiEnvelope<T> = {
  success: boolean
  message?: string
  data?: T
}

const unwrapImageStudioResponse = <T>(payload: T | ApiEnvelope<T>): T => {
  if (
    payload &&
    typeof payload === 'object' &&
    'success' in payload &&
    typeof payload.success === 'boolean'
  ) {
    if (!payload.success || payload.data === undefined) {
      throw new Error(payload.message || 'Request failed')
    }
    return payload.data
  }
  return payload as T
}

const cursorPageSchema = <T extends z.ZodType>(itemSchema: T) =>
  z.object({
    items: z.array(itemSchema),
    next_cursor: z.string().optional(),
  })

export const getImageTokenCapability =
  async (): Promise<ImageTokenCapability> => {
    const response = await api.get('/api/image-studio/token', {
      disableDuplicate: true,
      skipErrorHandler: true,
    })
    return imageTokenCapabilitySchema.parse(
      unwrapImageStudioResponse<unknown>(response.data)
    )
  }

export const createImageToken = async (): Promise<ImageTokenCreateResult> => {
  const response = await api.post(
    '/api/image-studio/token',
    {},
    { skipErrorHandler: true }
  )
  return imageTokenCreateResultSchema.parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

export const getImageModels = async (
  tokenId: number
): Promise<ImageModelProfile[]> => {
  const response = await api.get('/api/image-studio/models', {
    params: { token_id: tokenId },
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return z
    .array(imageModelProfileSchema)
    .parse(unwrapImageStudioResponse<unknown>(response.data))
}

export const getImageSamples = async (
  tokenId: number,
  cursor?: string
): Promise<CursorPage<ImageSample>> => {
  const response = await api.get('/api/image-studio/samples', {
    params: { token_id: tokenId, cursor, limit: 24 },
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return cursorPageSchema(imageSampleSchema).parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

export const getImageSample = async (
  id: number,
  tokenId: number
): Promise<ImageSample> => {
  const response = await api.get(`/api/image-studio/samples/${id}`, {
    params: { token_id: tokenId },
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return imageSampleSchema.parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

export const quoteImageGeneration = async (
  request: ImageQuoteRequest
): Promise<ImageQuote> => {
  const response = await api.post('/pg/images/quote', request, {
    skipErrorHandler: true,
  })
  return imageQuoteSchema.parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

export const quoteImageEdit = async (
  request: ImageEditQuoteRequest
): Promise<ImageQuote> => {
  const response = await api.post('/pg/images/edits/quote', request, {
    skipErrorHandler: true,
  })
  return imageQuoteSchema.parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

const submitImageStudioRequest = async (
  path: string,
  body: CreateImageRequest | FormData,
  idempotencyKey: string
): Promise<ImageGeneration> => {
  try {
    const response = await api.post(path, body, {
      headers: { 'Idempotency-Key': idempotencyKey },
      skipErrorHandler: true,
    })
    return imageGenerationSchema.parse(
      unwrapImageStudioResponse<unknown>(response.data)
    )
  } catch (error) {
    if (isAxiosError(error)) {
      const generation = error.response?.data?.data
      const parsed = imageGenerationSchema.safeParse(generation)
      if (parsed.success) return parsed.data
    }
    throw error
  }
}

export const createImageGeneration = async (
  request: CreateImageRequest,
  idempotencyKey: string
): Promise<ImageGeneration> =>
  submitImageStudioRequest('/pg/images', request, idempotencyKey)

export const buildImageEditFormData = (
  request: CreateImageEditRequest,
  images: readonly File[]
): FormData => {
  const body = new FormData()
  body.append('request', JSON.stringify(request))
  for (const image of images) body.append('image', image)
  return body
}

export const createImageEdit = async (
  request: CreateImageEditRequest,
  images: readonly File[],
  idempotencyKey: string
): Promise<ImageGeneration> =>
  submitImageStudioRequest(
    '/pg/images/edits',
    buildImageEditFormData(request, images),
    idempotencyKey
  )

export const getImageGenerations = async (input: {
  cursor?: string
  model?: string
  status?: ImageGenerationStatus
}): Promise<CursorPage<ImageGeneration>> => {
  const response = await api.get('/api/image-studio/generations', {
    params: { ...input, limit: 24 },
    disableDuplicate: true,
  })
  return cursorPageSchema(imageGenerationSchema).parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

export const deleteImageGeneration = async (id: number): Promise<void> => {
  await api.delete(`/api/image-studio/generations/${id}`)
}

export const getAdminImageModels = async (): Promise<ImageModelProfile[]> => {
  const response = await api.get('/api/admin/image-studio/model-profiles')
  return z
    .array(imageModelProfileSchema)
    .parse(unwrapImageStudioResponse<unknown>(response.data))
}

export const getAdminImageModelCandidates = async (): Promise<string[]> => {
  const response = await api.get('/api/admin/image-studio/model-candidates')
  return z
    .array(z.string())
    .parse(unwrapImageStudioResponse<unknown>(response.data))
}

export const saveAdminImageModel = async (input: {
  id?: number
  values: ImageModelProfileInput
}): Promise<ImageModelProfile> => {
  const response = input.id
    ? await api.put(
        `/api/admin/image-studio/model-profiles/${input.id}`,
        input.values,
        { skipErrorHandler: true }
      )
    : await api.post('/api/admin/image-studio/model-profiles', input.values, {
        skipErrorHandler: true,
      })
  return imageModelProfileSchema.parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

export const deleteAdminImageModel = async (id: number): Promise<void> => {
  await api.delete(`/api/admin/image-studio/model-profiles/${id}`, {
    skipErrorHandler: true,
  })
}

export const getAdminImageSamples = async (
  cursor?: string
): Promise<CursorPage<ImageSample>> => {
  const response = await api.get('/api/admin/image-studio/samples', {
    params: { cursor, limit: 50 },
    disableDuplicate: true,
  })
  return cursorPageSchema(imageSampleSchema).parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

export const uploadAdminImageSampleAsset = async (
  file: File
): Promise<ImageAsset> => {
  const body = new FormData()
  body.append('file', file)
  const response = await api.post(
    '/api/admin/image-studio/sample-assets',
    body,
    { skipErrorHandler: true }
  )
  return imageAssetSchema.parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

export const saveAdminImageSample = async (input: {
  id?: number
  values: ImageSampleInput
}): Promise<ImageSample> => {
  const response = input.id
    ? await api.put(
        `/api/admin/image-studio/samples/${input.id}`,
        input.values,
        { skipErrorHandler: true }
      )
    : await api.post('/api/admin/image-studio/samples', input.values, {
        skipErrorHandler: true,
      })
  return imageSampleSchema.parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

export const deleteAdminImageSample = async (id: number): Promise<void> => {
  await api.delete(`/api/admin/image-studio/samples/${id}`, {
    skipErrorHandler: true,
  })
}

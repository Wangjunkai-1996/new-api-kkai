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
import { z } from 'zod'

import { api } from '@/lib/api'

import { unwrapImageStudioResponse } from './api'
import type { ImageReferenceMetadata } from './types'

const uploadAssetSchema = z.object({
  id: z.number().int().positive(),
  state: z.enum([
    'pending_upload',
    'validating',
    'ready',
    'failed',
    'deleting',
    'deleted',
  ]),
  size_bytes: z.number().int().positive(),
  mime_type: z.string(),
  sha256: z.string(),
  width: z.number().int().nonnegative().optional(),
  height: z.number().int().nonnegative().optional(),
  failure_reason: z.string().optional(),
})

const uploadReservationSchema = z.object({
  asset: uploadAssetSchema,
  upload_mode: z.literal('multipart'),
  part_size: z
    .number()
    .int()
    .min(5 * 1024 * 1024),
  expires_at: z.number().int().positive(),
  max_size_bytes: z.number().int().positive(),
})

export type ImageReferenceAsset = z.infer<typeof uploadAssetSchema>
export type ImageUploadReservation = z.infer<typeof uploadReservationSchema>

export const createImageUpload = async (
  request: ImageReferenceMetadata & { mime_type: string },
  idempotencyKey: string,
  signal?: AbortSignal
): Promise<ImageUploadReservation> => {
  const response = await api.post('/api/image-studio/uploads', request, {
    headers: { 'Idempotency-Key': idempotencyKey },
    signal,
    skipErrorHandler: true,
  })
  return uploadReservationSchema.parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

export const getImageUpload = async (
  id: number,
  signal?: AbortSignal
): Promise<ImageUploadReservation> => {
  const response = await api.get(`/api/image-studio/uploads/${id}`, {
    signal,
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return uploadReservationSchema.parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

export const signImageUploadPart = async (
  id: number,
  partNumber: number,
  signal?: AbortSignal
) => {
  const response = await api.post(
    `/api/image-studio/uploads/${id}/parts/${partNumber}`,
    {},
    {
      signal,
      skipErrorHandler: true,
    }
  )
  return z
    .object({
      method: z.literal('PUT'),
      url: z.url(),
      headers: z.record(z.string(), z.string()).default({}),
      expires_at: z.number().int().positive(),
    })
    .parse(unwrapImageStudioResponse<unknown>(response.data))
}

export const listImageUploadParts = async (
  id: number,
  signal?: AbortSignal
) => {
  const response = await api.get(`/api/image-studio/uploads/${id}/parts`, {
    signal,
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return z
    .object({
      parts: z.array(
        z.object({
          part_number: z.number().int().positive(),
          size_bytes: z.number().int().positive(),
          etag: z.string().min(1),
        })
      ),
    })
    .parse(unwrapImageStudioResponse<unknown>(response.data)).parts
}

export const completeImageUpload = async (
  id: number,
  parts: { part_number: number; etag: string }[],
  signal?: AbortSignal
): Promise<ImageReferenceAsset> => {
  const response = await api.post(
    `/api/image-studio/uploads/${id}/complete`,
    { parts },
    {
      signal,
      skipErrorHandler: true,
    }
  )
  return uploadAssetSchema.parse(
    unwrapImageStudioResponse<unknown>(response.data)
  )
}

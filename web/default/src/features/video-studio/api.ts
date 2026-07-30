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
import { api } from '@/lib/api'

import {
  videoTokenCapabilitySchema,
  videoTokenCreateResultSchema,
} from './schemas'
import type {
  CompleteVideoUploadRequest,
  CreateVideoRequest,
  CreateVideoUploadRequest,
  CursorPage,
  VideoAsset,
  VideoGeneration,
  VideoGenerationFilters,
  VideoModelProfile,
  VideoModelProfileInput,
  VideoQuote,
  VideoQuoteRequest,
  VideoSample,
  VideoSampleFilters,
  VideoSampleInput,
  VideoSubmissionReceipt,
  VideoTokenCapability,
  VideoTokenCreateResult,
  VideoUploadReservation,
  VideoUploadSignedRequest,
  VideoUploadedPart,
} from './types'

type ApiEnvelope<T> = {
  success: boolean
  message?: string
  data?: T
}

const unwrapVideoStudioResponse = <T>(payload: T | ApiEnvelope<T>): T => {
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

export const getVideoModels = async (
  tokenId: number
): Promise<VideoModelProfile[]> => {
  const response = await api.get('/api/video-studio/models', {
    params: { token_id: tokenId },
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return unwrapVideoStudioResponse<VideoModelProfile[]>(response.data)
}

export const getVideoSamples = async (
  tokenId: number,
  filters: VideoSampleFilters
): Promise<CursorPage<VideoSample>> => {
  const response = await api.get('/api/video-studio/samples', {
    params: { ...filters, token_id: tokenId },
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return unwrapVideoStudioResponse<CursorPage<VideoSample>>(response.data)
}

export const getVideoSample = async (
  id: number,
  tokenId: number
): Promise<VideoSample> => {
  const response = await api.get(`/api/video-studio/samples/${id}`, {
    params: { token_id: tokenId },
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return unwrapVideoStudioResponse<VideoSample>(response.data)
}

export const getVideoTokenCapability =
  async (): Promise<VideoTokenCapability> => {
    const response = await api.get('/api/video-studio/token', {
      disableDuplicate: true,
      skipErrorHandler: true,
    })
    return videoTokenCapabilitySchema.parse(
      unwrapVideoStudioResponse<unknown>(response.data)
    )
  }

export const createVideoToken = async (): Promise<VideoTokenCreateResult> => {
  const response = await api.post(
    '/api/video-studio/token',
    {},
    { skipErrorHandler: true }
  )
  return videoTokenCreateResultSchema.parse(
    unwrapVideoStudioResponse<unknown>(response.data)
  )
}

export const quoteVideoGeneration = async (
  request: VideoQuoteRequest
): Promise<VideoQuote> => {
  const response = await api.post('/pg/videos/quote', request, {
    skipErrorHandler: true,
  })
  return unwrapVideoStudioResponse<VideoQuote>(response.data)
}

export const createVideoGeneration = async (
  request: CreateVideoRequest,
  idempotencyKey: string
): Promise<VideoSubmissionReceipt> => {
  const response = await api.post('/pg/videos', request, {
    headers: { 'Idempotency-Key': idempotencyKey },
    skipErrorHandler: true,
  })
  return unwrapVideoStudioResponse<VideoSubmissionReceipt>(response.data)
}

export const getVideoGenerations = async (
  filters: VideoGenerationFilters
): Promise<CursorPage<VideoGeneration>> => {
  const response = await api.get('/api/video-studio/generations', {
    params: filters,
    disableDuplicate: true,
  })
  return unwrapVideoStudioResponse<CursorPage<VideoGeneration>>(response.data)
}

export const getVideoGeneration = async (
  id: number
): Promise<VideoGeneration> => {
  const response = await api.get(`/api/video-studio/generations/${id}`, {
    disableDuplicate: true,
  })
  return unwrapVideoStudioResponse<VideoGeneration>(response.data)
}

export const deleteVideoGeneration = async (id: number): Promise<void> => {
  await api.delete(`/api/video-studio/generations/${id}`)
}

export const createVideoUpload = async (
  request: CreateVideoUploadRequest,
  admin = false
): Promise<VideoUploadReservation> => {
  const path = admin
    ? '/api/admin/video-studio/uploads'
    : '/api/video-studio/uploads'
  const response = await api.post(path, request, {
    skipErrorHandler: true,
  })
  return unwrapVideoStudioResponse<VideoUploadReservation>(response.data)
}

export const completeVideoUpload = async (
  id: number,
  request: CompleteVideoUploadRequest = {},
  admin = false,
  signal?: AbortSignal
): Promise<VideoAsset> => {
  const path = admin
    ? `/api/admin/video-studio/uploads/${id}/complete`
    : `/api/video-studio/uploads/${id}/complete`
  const response = await api.post(path, request, {
    signal,
    skipErrorHandler: true,
  })
  return unwrapVideoStudioResponse<VideoAsset>(response.data)
}

export const signVideoUploadPart = async (
  id: number,
  partNumber: number,
  admin = false,
  signal?: AbortSignal
): Promise<VideoUploadSignedRequest> => {
  const path = admin
    ? `/api/admin/video-studio/uploads/${id}/parts/${partNumber}`
    : `/api/video-studio/uploads/${id}/parts/${partNumber}`
  const response = await api.post(
    path,
    {},
    {
      signal,
      skipErrorHandler: true,
    }
  )
  return unwrapVideoStudioResponse<VideoUploadSignedRequest>(response.data)
}

export const listVideoUploadParts = async (
  id: number,
  admin = false,
  signal?: AbortSignal
): Promise<VideoUploadedPart[]> => {
  const path = admin
    ? `/api/admin/video-studio/uploads/${id}/parts`
    : `/api/video-studio/uploads/${id}/parts`
  const response = await api.get(path, {
    signal,
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  const data = unwrapVideoStudioResponse<{ parts: VideoUploadedPart[] }>(
    response.data
  )
  return data.parts
}

export const abortVideoUpload = async (
  id: number,
  admin = false,
  signal?: AbortSignal
): Promise<void> => {
  const path = admin
    ? `/api/admin/video-studio/uploads/${id}`
    : `/api/video-studio/uploads/${id}`
  await api.delete(path, {
    signal,
    skipErrorHandler: true,
  })
}

export const getVideoUpload = async (
  id: number,
  admin = false
): Promise<VideoAsset> => {
  const path = admin
    ? `/api/admin/video-studio/uploads/${id}`
    : `/api/video-studio/uploads/${id}`
  const response = await api.get(path, {
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return unwrapVideoStudioResponse<VideoAsset>(response.data)
}

export const getVideoAsset = async (id: number): Promise<VideoAsset> => {
  const response = await api.get(`/api/video-studio/assets/${id}`, {
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return unwrapVideoStudioResponse<VideoAsset>(response.data)
}

export const deleteVideoAsset = async (id: number): Promise<void> => {
  await api.delete(`/api/video-studio/assets/${id}`, {
    skipErrorHandler: true,
  })
}

export const getVideoAssetContentUrl = (
  id: number,
  download = false
): string => {
  const suffix = download ? 'download' : 'content'
  return `/api/video-studio/assets/${encodeURIComponent(String(id))}/${suffix}`
}

export const getAdminVideoModels = async (): Promise<VideoModelProfile[]> => {
  const response = await api.get('/api/admin/video-studio/model-profiles')
  return unwrapVideoStudioResponse<VideoModelProfile[]>(response.data)
}

export const getAdminVideoModelCandidates = async (): Promise<string[]> => {
  const response = await api.get('/api/admin/video-studio/model-candidates')
  return unwrapVideoStudioResponse<string[]>(response.data)
}

export const createAdminVideoModel = async (
  input: VideoModelProfileInput
): Promise<VideoModelProfile> => {
  const response = await api.post(
    '/api/admin/video-studio/model-profiles',
    input
  )
  return unwrapVideoStudioResponse<VideoModelProfile>(response.data)
}

export const updateAdminVideoModel = async (
  id: number,
  input: VideoModelProfileInput
): Promise<VideoModelProfile> => {
  const response = await api.put(
    `/api/admin/video-studio/model-profiles/${id}`,
    input
  )
  return unwrapVideoStudioResponse<VideoModelProfile>(response.data)
}

export const deleteAdminVideoModel = async (id: number): Promise<void> => {
  await api.delete(`/api/admin/video-studio/model-profiles/${id}`)
}

export const getAdminVideoSamples = async (
  filters: VideoSampleFilters
): Promise<CursorPage<VideoSample>> => {
  const response = await api.get('/api/admin/video-studio/samples', {
    params: filters,
    disableDuplicate: true,
  })
  return unwrapVideoStudioResponse<CursorPage<VideoSample>>(response.data)
}

export const createAdminVideoSample = async (
  input: VideoSampleInput
): Promise<VideoSample> => {
  const response = await api.post('/api/admin/video-studio/samples', input)
  return unwrapVideoStudioResponse<VideoSample>(response.data)
}

export const updateAdminVideoSample = async (
  id: number,
  input: VideoSampleInput
): Promise<VideoSample> => {
  const response = await api.put(`/api/admin/video-studio/samples/${id}`, input)
  return unwrapVideoStudioResponse<VideoSample>(response.data)
}

export const deleteAdminVideoSample = async (id: number): Promise<void> => {
  await api.delete(`/api/admin/video-studio/samples/${id}`)
}

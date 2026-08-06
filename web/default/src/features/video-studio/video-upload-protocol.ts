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
import type { AwsS3Part, AwsS3UploadParameters } from '@uppy/aws-s3'

import {
  abortVideoUpload,
  completeVideoUpload,
  createVideoUpload,
  getVideoAssetContentUrl,
  listVideoUploadParts,
  signVideoUploadPart,
} from './api'
import type {
  CompleteVideoUploadRequest,
  CreateVideoUploadRequest,
  VideoAsset,
  VideoUploadMode,
  VideoUploadReservation,
  VideoUploadSignedRequest,
  VideoUploadedPart,
} from './types'

export type VideoUploadMeta = {
  assetId?: number
  uploadMode?: VideoUploadMode
  uploadPartSize?: number
  uploadExpiresAt?: number
  uploadAdmin?: boolean
}

export type PreparedVideoUploadMeta = {
  assetId: number
  uploadMode: VideoUploadMode
  uploadPartSize?: number
  uploadExpiresAt: number
  uploadMaxSizeBytes: number
  uploadAdmin: boolean
}

export type VideoUploadResponseBody = {
  [key: string]: unknown
  location?: string
  asset?: VideoAsset
}

type VideoUploadFile = {
  meta: VideoUploadMeta
}

type VideoUploadReservationEntry = {
  admin: boolean
  data: object
  reservation: VideoUploadReservation
}

export type VideoUploadProtocolApi = {
  create: (
    request: CreateVideoUploadRequest,
    admin: boolean
  ) => Promise<VideoUploadReservation>
  signPart: (
    id: number,
    partNumber: number,
    admin: boolean,
    signal?: AbortSignal
  ) => Promise<VideoUploadSignedRequest>
  listParts: (
    id: number,
    admin: boolean,
    signal?: AbortSignal
  ) => Promise<VideoUploadedPart[]>
  complete: (
    id: number,
    request: CompleteVideoUploadRequest,
    admin: boolean,
    signal?: AbortSignal
  ) => Promise<VideoAsset>
  abort: (id: number, admin: boolean, signal?: AbortSignal) => Promise<void>
}

const defaultVideoUploadProtocolApi: VideoUploadProtocolApi = {
  create: createVideoUpload,
  signPart: signVideoUploadPart,
  listParts: listVideoUploadParts,
  complete: completeVideoUpload,
  abort: abortVideoUpload,
}

export class VideoUploadProtocolError extends Error {
  override name = 'VideoUploadProtocolError'
}

const invalidReservation = () =>
  new VideoUploadProtocolError('Invalid video upload reservation')

const isPositiveInteger = (value: unknown): value is number =>
  typeof value === 'number' && Number.isSafeInteger(value) && value > 0

const assertSignedPutRequest = (
  request: VideoUploadSignedRequest | undefined
): VideoUploadSignedRequest => {
  if (
    request?.method !== 'PUT' ||
    typeof request.url !== 'string' ||
    request.url.length === 0 ||
    !isPositiveInteger(request.expires_at)
  ) {
    throw invalidReservation()
  }
  return request
}

const assertReservation = (reservation: VideoUploadReservation) => {
  if (
    !isPositiveInteger(reservation.asset?.id) ||
    !isPositiveInteger(reservation.expires_at) ||
    !isPositiveInteger(reservation.max_size_bytes)
  ) {
    throw invalidReservation()
  }
  if (reservation.upload_mode === 'single') {
    assertSignedPutRequest(reservation.request)
    return
  }
  if (
    reservation.upload_mode !== 'multipart' ||
    !isPositiveInteger(reservation.part_size)
  ) {
    throw invalidReservation()
  }
}

const toUploadParameters = (
  request: VideoUploadSignedRequest
): AwsS3UploadParameters => ({
  method: 'PUT',
  url: request.url,
  headers: request.headers,
})

export const createVideoUploadProtocol = ({
  admin,
  api = defaultVideoUploadProtocolApi,
}: {
  admin: () => boolean
  api?: VideoUploadProtocolApi
}) => {
  const reservations = new Map<number, VideoUploadReservationEntry>()
  const assetIdsByData = new WeakMap<object, number>()
  const aborts = new Map<number, Promise<void>>()

  const getEntry = (file: VideoUploadFile) => {
    const assetId = file.meta.assetId
    if (!isPositiveInteger(assetId)) throw invalidReservation()
    const entry = reservations.get(assetId)
    if (!entry) throw invalidReservation()
    return entry
  }

  const releaseEntry = (entry: VideoUploadReservationEntry) => {
    reservations.delete(entry.reservation.asset.id)
    assetIdsByData.delete(entry.data)
  }

  const registerReservation = (
    data: object,
    reservation: VideoUploadReservation,
    isAdmin: boolean
  ): PreparedVideoUploadMeta => {
    assertReservation(reservation)
    const entry = { admin: isAdmin, data, reservation }
    reservations.set(reservation.asset.id, entry)
    assetIdsByData.set(data, reservation.asset.id)
    return {
      assetId: reservation.asset.id,
      uploadMode: reservation.upload_mode,
      uploadPartSize: reservation.part_size,
      uploadExpiresAt: reservation.expires_at,
      uploadMaxSizeBytes: reservation.max_size_bytes,
      uploadAdmin: isAdmin,
    }
  }

  const prepare = async (
    data: object,
    request: Omit<CreateVideoUploadRequest, 'multipart'>
  ): Promise<PreparedVideoUploadMeta> => {
    const isAdmin = admin()
    const reservation = await api.create(
      { ...request, multipart: true },
      isAdmin
    )
    try {
      assertReservation(reservation)
      if (request.size_bytes > reservation.max_size_bytes) {
        throw invalidReservation()
      }
    } catch (error) {
      if (isPositiveInteger(reservation.asset?.id)) {
        void api.abort(reservation.asset.id, isAdmin).catch(() => undefined)
      }
      throw error
    }
    return registerReservation(data, reservation, isAdmin)
  }

  const resume = (
    data: object,
    reservation: VideoUploadReservation,
    isAdmin: boolean
  ): PreparedVideoUploadMeta => {
    if (reservation.upload_mode !== 'multipart') throw invalidReservation()
    return registerReservation(data, reservation, isAdmin)
  }

  const shouldUseMultipart = (file: VideoUploadFile) => {
    const entry = getEntry(file)
    if (file.meta.uploadMode !== entry.reservation.upload_mode) {
      throw invalidReservation()
    }
    return entry.reservation.upload_mode === 'multipart'
  }

  const getChunkSize = (data: object) => {
    const assetId = assetIdsByData.get(data)
    const entry = assetId ? reservations.get(assetId) : undefined
    if (
      !entry ||
      entry.reservation.upload_mode !== 'multipart' ||
      !isPositiveInteger(entry.reservation.part_size)
    ) {
      throw invalidReservation()
    }
    return entry.reservation.part_size
  }

  const getUploadParameters = (file: VideoUploadFile) => {
    const reservation = getEntry(file).reservation
    if (reservation.upload_mode !== 'single') throw invalidReservation()
    return toUploadParameters(assertSignedPutRequest(reservation.request))
  }

  const createMultipartUpload = (file: VideoUploadFile) => {
    const reservation = getEntry(file).reservation
    if (reservation.upload_mode !== 'multipart') throw invalidReservation()
    const opaqueAssetId = String(reservation.asset.id)
    return { uploadId: opaqueAssetId, key: opaqueAssetId }
  }

  const signPart = async (
    file: VideoUploadFile,
    partNumber: number,
    signal?: AbortSignal
  ) => {
    if (!isPositiveInteger(partNumber)) throw invalidReservation()
    const entry = getEntry(file)
    if (entry.reservation.upload_mode !== 'multipart') {
      throw invalidReservation()
    }
    const request = await api.signPart(
      entry.reservation.asset.id,
      partNumber,
      entry.admin,
      signal
    )
    return toUploadParameters(assertSignedPutRequest(request))
  }

  const listParts = async (
    file: VideoUploadFile,
    options: { signal?: AbortSignal } = {}
  ): Promise<AwsS3Part[]> => {
    const entry = getEntry(file)
    if (entry.reservation.upload_mode !== 'multipart') {
      throw invalidReservation()
    }
    const parts = await api.listParts(
      entry.reservation.asset.id,
      entry.admin,
      options.signal
    )
    return [...parts]
      .sort((left, right) => left.part_number - right.part_number)
      .map((part) => {
        if (
          !isPositiveInteger(part.part_number) ||
          !isPositiveInteger(part.size_bytes) ||
          typeof part.etag !== 'string' ||
          part.etag.length === 0
        ) {
          throw invalidReservation()
        }
        return {
          PartNumber: part.part_number,
          Size: part.size_bytes,
          ETag: part.etag,
        }
      })
  }

  const completeMultipartUpload = async (
    file: VideoUploadFile,
    options: { parts: AwsS3Part[]; signal?: AbortSignal }
  ): Promise<VideoUploadResponseBody> => {
    const entry = getEntry(file)
    if (entry.reservation.upload_mode !== 'multipart') {
      throw invalidReservation()
    }
    const seenPartNumbers = new Set<number>()
    const parts = options.parts
      .map((part) => {
        if (
          !isPositiveInteger(part.PartNumber) ||
          typeof part.ETag !== 'string' ||
          part.ETag.length === 0 ||
          seenPartNumbers.has(part.PartNumber)
        ) {
          throw invalidReservation()
        }
        seenPartNumbers.add(part.PartNumber)
        return { part_number: part.PartNumber, etag: part.ETag }
      })
      .sort((left, right) => left.part_number - right.part_number)
    const completedAsset = await api.complete(
      entry.reservation.asset.id,
      { parts },
      entry.admin,
      options.signal
    )
    releaseEntry(entry)
    return {
      asset: completedAsset,
      location:
        completedAsset.content_url ||
        getVideoAssetContentUrl(completedAsset.id),
    }
  }

  const completeSingleUpload = async (file: VideoUploadFile) => {
    const entry = getEntry(file)
    if (entry.reservation.upload_mode !== 'single') {
      throw invalidReservation()
    }
    const completedAsset = await api.complete(
      entry.reservation.asset.id,
      {},
      entry.admin
    )
    releaseEntry(entry)
    return completedAsset
  }

  const abortUpload = async (file: VideoUploadFile, signal?: AbortSignal) => {
    const assetId = file.meta.assetId
    if (!isPositiveInteger(assetId)) throw invalidReservation()
    const existingAbort = aborts.get(assetId)
    if (existingAbort) return existingAbort
    const entry = reservations.get(assetId)
    if (!entry) return
    const abortRequest = api
      .abort(entry.reservation.asset.id, entry.admin, signal)
      .then(() => releaseEntry(entry))
      .finally(() => {
        aborts.delete(assetId)
      })
    aborts.set(assetId, abortRequest)
    return abortRequest
  }

  return {
    prepare,
    resume,
    shouldUseMultipart,
    getChunkSize,
    getUploadParameters,
    createMultipartUpload,
    signPart,
    listParts,
    completeMultipartUpload,
    completeSingleUpload,
    abortUpload,
  }
}

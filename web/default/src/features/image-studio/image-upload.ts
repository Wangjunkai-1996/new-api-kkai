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
import AwsS3, { type AwsS3Options } from '@uppy/aws-s3'
import Uppy from '@uppy/core'
import { isAxiosError } from 'axios'

import {
  completeImageUpload,
  createImageUpload,
  getImageUpload,
  listImageUploadParts,
  signImageUploadPart,
  type ImageReferenceAsset,
  type ImageUploadReservation,
} from './image-upload-api'
import type { ImageReferenceMetadata } from './types'

export type ImageUploadProgress = {
  phase: 'uploading' | 'validating'
  progress: number
}
type UploadMeta = { assetId: number; partSize: number }
type UploadBody = { [key: string]: unknown; location: string }

export class ImageUploadError extends Error {
  constructor(readonly reason: 'failed' | 'validation_pending') {
    super(reason)
  }
}

const uploadKey = (userId: number, reference: ImageReferenceMetadata): string =>
  `image-studio-upload:v1:${userId}:${reference.sha256}:${reference.size_bytes}`

const getUploadAttemptKey = (storageKey: string): string => {
  try {
    const saved: unknown = JSON.parse(
      localStorage.getItem(storageKey) || 'null'
    )
    if (
      saved &&
      typeof saved === 'object' &&
      'key' in saved &&
      'expiresAt' in saved &&
      typeof saved.key === 'string' &&
      saved.key.length <= 128 &&
      typeof saved.expiresAt === 'number' &&
      saved.expiresAt > Date.now()
    ) {
      return saved.key
    }
  } catch {
    // Upload receipts are best-effort; generation submission receipts are separate.
  }
  const key = crypto.randomUUID()
  try {
    localStorage.setItem(
      storageKey,
      JSON.stringify({ key, expiresAt: Date.now() + 24 * 60 * 60 * 1000 })
    )
  } catch {
    // The current upload can continue without cross-page resumption.
  }
  return key
}

const validationDelay = (signal: AbortSignal): Promise<void> =>
  new Promise((resolve, reject) => {
    const abort = () => {
      clearTimeout(timer)
      reject(signal.reason)
    }
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', abort)
      resolve()
    }, 1500)
    signal.addEventListener('abort', abort, { once: true })
    if (signal.aborted) abort()
  })

export const uploadImageReferences = async (
  userId: number,
  files: readonly File[],
  metadata: readonly ImageReferenceMetadata[],
  onProgress: (progress: ImageUploadProgress) => void,
  signal: AbortSignal
): Promise<ImageReferenceAsset[]> => {
  if (files.length !== metadata.length || files.length === 0) {
    throw new ImageUploadError('failed')
  }
  signal.throwIfAborted()
  const reservations: ImageUploadReservation[] = []
  // Small control requests are serialized; Uppy limits storage traffic to two parts globally.
  for (const [index, file] of files.entries()) {
    const storageKey = uploadKey(userId, metadata[index])
    let reservation: ImageUploadReservation
    try {
      reservation = await createImageUpload(
        { ...metadata[index], mime_type: file.type },
        getUploadAttemptKey(storageKey),
        signal
      )
    } catch (error) {
      if (
        isAxiosError(error) &&
        (error.response?.status === 409 || error.response?.status === 410)
      ) {
        try {
          localStorage.removeItem(storageKey)
        } catch {
          /* Explicit retry can allocate a fresh upload. */
        }
      }
      throw error
    }
    if (
      reservation.asset.state === 'failed' ||
      reservation.asset.state === 'deleted' ||
      reservation.asset.state === 'deleting'
    ) {
      try {
        localStorage.removeItem(storageKey)
      } catch {
        /* Best-effort upload receipt cleanup. */
      }
      throw new ImageUploadError('failed')
    }
    if (
      reservation.asset.size_bytes !== file.size ||
      reservation.asset.sha256 !== metadata[index].sha256 ||
      reservation.asset.mime_type !== file.type ||
      file.size > reservation.max_size_bytes
    ) {
      throw new ImageUploadError('failed')
    }
    reservations.push(reservation)
  }
  signal.throwIfAborted()
  const uppy = new Uppy<UploadMeta, UploadBody>({ autoProceed: false })
  const options: AwsS3Options<UploadMeta, UploadBody> = {
    limit: 2,
    allowedMetaFields: [],
    shouldUseMultipart: true,
    getChunkSize: (data) => {
      const index = files.indexOf(data as File)
      if (index < 0) throw new ImageUploadError('failed')
      return reservations[index].part_size
    },
    createMultipartUpload: (file) => ({
      uploadId: String(file.meta.assetId),
      key: String(file.meta.assetId),
    }),
    signPart: async (file, options) => {
      const signed = await signImageUploadPart(
        file.meta.assetId,
        options.partNumber,
        options.signal
      )
      // Browsers set this forbidden header from the Blob; signed values must still match its size.
      const headers = Object.fromEntries(
        Object.entries(signed.headers).filter(
          ([key]) => key.toLowerCase() !== 'content-length'
        )
      )
      return { method: signed.method, url: signed.url, headers }
    },
    listParts: async (file, options) =>
      (await listImageUploadParts(file.meta.assetId, options.signal)).map(
        (part) => ({
          PartNumber: part.part_number,
          ETag: part.etag,
          Size: part.size_bytes,
        })
      ),
    completeMultipartUpload: async (file, options) => {
      const parts = options.parts.map((part) => {
        if (!part.PartNumber || !part.ETag) throw new ImageUploadError('failed')
        return { part_number: part.PartNumber, etag: part.ETag }
      })
      await completeImageUpload(file.meta.assetId, parts, options.signal)
      return { location: `/api/image-studio/uploads/${file.meta.assetId}` }
    },
    // Leaving the page must preserve uploaded parts so the same file can resume later.
    abortMultipartUpload: async () => undefined,
  }
  uppy.use(AwsS3<UploadMeta, UploadBody>, options)
  uppy.on('progress', (progress) =>
    onProgress({ phase: 'uploading', progress })
  )
  const cancel = () => uppy.cancelAll()
  signal.addEventListener('abort', cancel, { once: true })
  try {
    for (const [index, reservation] of reservations.entries()) {
      if (reservation.asset.state !== 'pending_upload') continue
      const file = files[index]
      const id = uppy.addFile({
        name: `${index}-${file.name}`,
        type: file.type,
        data: file,
        meta: {
          assetId: reservation.asset.id,
          partSize: reservation.part_size,
        },
      })
      const resumedState = {
        ...uppy.getFile(id),
        s3Multipart: {
          uploadId: String(reservation.asset.id),
          key: String(reservation.asset.id),
        },
      }
      uppy.setFileState(id, resumedState)
    }
    if (uppy.getFiles().length > 0) {
      onProgress({ phase: 'uploading', progress: 0 })
      const result = await uppy.upload()
      signal.throwIfAborted()
      if (!result || result.failed?.length) throw new ImageUploadError('failed')
    }
  } finally {
    signal.removeEventListener('abort', cancel)
    uppy.destroy()
  }
  onProgress({ phase: 'validating', progress: 100 })
  const deadline = Date.now() + 10 * 60 * 1000
  let assets = reservations.map((reservation) => reservation.asset)
  while (assets.some((asset) => asset.state !== 'ready')) {
    signal.throwIfAborted()
    assets = await Promise.all(
      assets.map(async (asset) =>
        asset.state === 'ready'
          ? asset
          : (await getImageUpload(asset.id, signal)).asset
      )
    )
    if (
      assets.some(
        (asset) =>
          !['pending_upload', 'validating', 'ready'].includes(asset.state)
      )
    ) {
      throw new ImageUploadError('failed')
    }
    if (assets.every((asset) => asset.state === 'ready')) break
    if (Date.now() >= deadline) throw new ImageUploadError('validation_pending')
    await validationDelay(signal)
  }
  return assets
}

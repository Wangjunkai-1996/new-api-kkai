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
import type { VideoStudioUploadLimits } from '@/features/auth/types'

import type {
  VideoAsset,
  VideoUploadPurpose,
  VideoUploadReservation,
} from './types'

export const VIDEO_REFERENCE_FALLBACK_MAX_BYTES = 20 * 1024 * 1024
export const VIDEO_REFERENCE_VIDEO_FALLBACK_MAX_BYTES = 1024 * 1024 * 1024
export const VIDEO_ADMIN_SAMPLE_FALLBACK_MAX_BYTES = 1024 * 1024 * 1024

export type VideoUploadFingerprint = {
  name: string
  type: string
  size: number
  lastModified: number
}

export type VideoUploadResumeRecord = {
  assetId: number
  admin: boolean
  purpose: VideoUploadPurpose
  uploadMode: 'multipart'
  partSize: number
  expiresAt: number
  maxSizeBytes: number
  fingerprint: VideoUploadFingerprint
}

type VideoUploadFingerprintSource = {
  name: string
  type: string
  size: number
  lastModified: number
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

const isPositiveInteger = (value: unknown): value is number =>
  typeof value === 'number' && Number.isSafeInteger(value) && value > 0

export const getVideoUploadMaxBytes = (
  purpose: VideoUploadPurpose,
  admin: boolean,
  limits?: VideoStudioUploadLimits
): number => {
  let serverLimit = limits?.reference_max_bytes
  if (purpose === 'reference_video') {
    serverLimit = limits?.archive_max_bytes
  } else if (admin && purpose === 'sample') {
    serverLimit = limits?.sample_max_bytes
  }
  if (isPositiveInteger(serverLimit)) return serverLimit
  if (purpose === 'reference_video') {
    return VIDEO_REFERENCE_VIDEO_FALLBACK_MAX_BYTES
  }
  return admin && purpose === 'sample'
    ? VIDEO_ADMIN_SAMPLE_FALLBACK_MAX_BYTES
    : VIDEO_REFERENCE_FALLBACK_MAX_BYTES
}

export const shouldDeleteVideoAssetOnRemove = (
  asset: VideoAsset,
  adminUpload: boolean
): boolean =>
  !adminUpload && asset.scope === 'user' && asset.kind === 'reference'

export const getVideoUploadFingerprint = (
  file: VideoUploadFingerprintSource
): VideoUploadFingerprint => ({
  name: file.name,
  type: file.type,
  size: file.size,
  lastModified: file.lastModified,
})

export const videoUploadFingerprintMatches = (
  left: VideoUploadFingerprint,
  right: VideoUploadFingerprint
): boolean =>
  left.name === right.name &&
  left.type === right.type &&
  left.size === right.size &&
  left.lastModified === right.lastModified

export const findVideoUploadResume = (
  records: VideoUploadResumeRecord[],
  fingerprint: VideoUploadFingerprint,
  purpose: VideoUploadPurpose,
  admin: boolean,
  nowSeconds: number,
  excludedAssetIds: ReadonlySet<number> = new Set()
): VideoUploadResumeRecord | undefined =>
  records.find(
    (record) =>
      record.admin === admin &&
      record.purpose === purpose &&
      record.expiresAt > nowSeconds &&
      !excludedAssetIds.has(record.assetId) &&
      videoUploadFingerprintMatches(record.fingerprint, fingerprint)
  )

export const sanitizeVideoUploadResumeRecords = (
  value: unknown,
  nowSeconds = 0
): VideoUploadResumeRecord[] => {
  if (!Array.isArray(value)) return []

  const records: VideoUploadResumeRecord[] = []
  const assetIds = new Set<number>()
  for (const candidate of value) {
    if (!isRecord(candidate) || !isRecord(candidate.fingerprint)) continue
    if (
      !isPositiveInteger(candidate.assetId) ||
      assetIds.has(candidate.assetId) ||
      typeof candidate.admin !== 'boolean' ||
      (candidate.purpose !== 'reference' &&
        candidate.purpose !== 'reference_video' &&
        candidate.purpose !== 'sample') ||
      candidate.uploadMode !== 'multipart' ||
      !isPositiveInteger(candidate.partSize) ||
      !isPositiveInteger(candidate.expiresAt) ||
      !isPositiveInteger(candidate.maxSizeBytes) ||
      candidate.expiresAt <= nowSeconds ||
      typeof candidate.fingerprint.name !== 'string' ||
      typeof candidate.fingerprint.type !== 'string' ||
      !Number.isSafeInteger(candidate.fingerprint.size) ||
      Number(candidate.fingerprint.size) < 0 ||
      !Number.isSafeInteger(candidate.fingerprint.lastModified) ||
      Number(candidate.fingerprint.lastModified) < 0
    ) {
      continue
    }

    assetIds.add(candidate.assetId)
    records.push({
      assetId: candidate.assetId,
      admin: candidate.admin,
      purpose: candidate.purpose,
      uploadMode: 'multipart',
      partSize: candidate.partSize,
      expiresAt: candidate.expiresAt,
      maxSizeBytes: candidate.maxSizeBytes,
      fingerprint: {
        name: candidate.fingerprint.name,
        type: candidate.fingerprint.type,
        size: Number(candidate.fingerprint.size),
        lastModified: Number(candidate.fingerprint.lastModified),
      },
    })
  }
  return records
}

export const getResumableVideoUploadReservation = (
  asset: VideoAsset,
  record: VideoUploadResumeRecord,
  nowSeconds: number
): VideoUploadReservation | null => {
  const expectedKind = record.purpose === 'sample' ? 'sample' : 'reference'
  if (
    asset.id !== record.assetId ||
    asset.kind !== expectedKind ||
    asset.state !== 'pending_upload' ||
    asset.upload_mode !== 'multipart' ||
    !isPositiveInteger(asset.upload_part_size) ||
    !isPositiveInteger(asset.upload_expires_at) ||
    asset.upload_expires_at <= nowSeconds
  ) {
    return null
  }

  return {
    asset,
    upload_mode: 'multipart',
    part_size: asset.upload_part_size,
    expires_at: asset.upload_expires_at,
    max_size_bytes: record.maxSizeBytes,
  }
}

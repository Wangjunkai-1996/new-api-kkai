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

import { getVideoUpload } from './api'
import type {
  CreateVideoUploadRequest,
  VideoAsset,
  VideoUploadPurpose,
  VideoUploadReservation,
} from './types'
import type { PreparedVideoUploadMeta } from './video-upload-protocol'
import {
  findVideoUploadResume,
  getResumableVideoUploadReservation,
  getVideoUploadFingerprint,
  type VideoUploadResumeRecord,
} from './video-upload-resume'

type VideoUploadPreparationProtocol = {
  prepare: (
    data: object,
    request: Omit<CreateVideoUploadRequest, 'multipart'>
  ) => Promise<PreparedVideoUploadMeta>
  resume: (
    data: object,
    reservation: VideoUploadReservation,
    admin: boolean
  ) => PreparedVideoUploadMeta
}

type PrepareVideoUploadSelectionInput = {
  file: File
  purpose: VideoUploadPurpose
  admin: boolean
  uploadResumes: VideoUploadResumeRecord[]
  claimedResumeAssetIds: ReadonlySet<number>
  protocol: VideoUploadPreparationProtocol
  removeUploadResume: (assetId: number) => void
  loadUpload?: (assetId: number, admin: boolean) => Promise<VideoAsset>
  nowSeconds?: number
}

export type PreparedVideoUploadSelection =
  | { kind: 'completed'; asset: VideoAsset }
  | {
      kind: 'upload'
      meta: PreparedVideoUploadMeta
      createdReservation: boolean
      resumeRecord?: VideoUploadResumeRecord
    }

const isCompletedUpload = (asset: VideoAsset): boolean =>
  asset.state === 'uploaded' ||
  asset.state === 'processing' ||
  asset.state === 'ready'

const getResumeRecord = (
  meta: PreparedVideoUploadMeta,
  purpose: VideoUploadPurpose,
  fingerprint: VideoUploadResumeRecord['fingerprint']
): VideoUploadResumeRecord | undefined => {
  if (meta.uploadMode !== 'multipart' || !meta.uploadPartSize) return undefined
  return {
    assetId: meta.assetId,
    admin: meta.uploadAdmin,
    purpose,
    uploadMode: 'multipart',
    partSize: meta.uploadPartSize,
    expiresAt: meta.uploadExpiresAt,
    maxSizeBytes: meta.uploadMaxSizeBytes,
    fingerprint,
  }
}

export const prepareVideoUploadSelection = async (
  input: PrepareVideoUploadSelectionInput
): Promise<PreparedVideoUploadSelection> => {
  const nowSeconds = input.nowSeconds ?? Math.floor(Date.now() / 1000)
  const fingerprint = getVideoUploadFingerprint(input.file)
  const resumeRecord = findVideoUploadResume(
    input.uploadResumes,
    fingerprint,
    input.purpose,
    input.admin,
    nowSeconds,
    input.claimedResumeAssetIds
  )

  if (resumeRecord) {
    try {
      const loadUpload = input.loadUpload ?? getVideoUpload
      const asset = await loadUpload(resumeRecord.assetId, resumeRecord.admin)
      if (isCompletedUpload(asset)) {
        input.removeUploadResume(resumeRecord.assetId)
        return { kind: 'completed', asset }
      }
      const reservation = getResumableVideoUploadReservation(
        asset,
        resumeRecord,
        nowSeconds
      )
      if (reservation) {
        const meta = input.protocol.resume(
          input.file,
          reservation,
          resumeRecord.admin
        )
        return {
          kind: 'upload',
          meta,
          createdReservation: false,
          resumeRecord: getResumeRecord(meta, input.purpose, fingerprint),
        }
      }
      input.removeUploadResume(resumeRecord.assetId)
    } catch (error) {
      const status = isAxiosError(error) ? error.response?.status : undefined
      if (status !== 403 && status !== 404 && status !== 410) throw error
      input.removeUploadResume(resumeRecord.assetId)
    }
  }

  const meta = await input.protocol.prepare(input.file, {
    filename: input.file.name || 'upload',
    mime_type: input.file.type || 'application/octet-stream',
    size_bytes: input.file.size,
    purpose: input.purpose,
  })
  return {
    kind: 'upload',
    meta,
    createdReservation: true,
    resumeRecord: getResumeRecord(meta, input.purpose, fingerprint),
  }
}

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
import assert from 'node:assert/strict'
import test from 'node:test'

import { sanitizeVideoStudioDraft } from '@/stores/video-studio-draft-store'

import type { VideoAsset, VideoUploadReservation } from './types'
import {
  createVideoUploadProtocol,
  type VideoUploadProtocolApi,
} from './video-upload-protocol'
import {
  findVideoUploadResume,
  getVideoUploadFingerprint,
  getVideoUploadMaxBytes,
  sanitizeVideoUploadResumeRecords,
  shouldDeleteVideoAssetOnRemove,
  VIDEO_ADMIN_SAMPLE_FALLBACK_MAX_BYTES,
  VIDEO_REFERENCE_FALLBACK_MAX_BYTES,
} from './video-upload-resume'
import { prepareVideoUploadSelection } from './video-upload-selection'

const asset = (id = 41): VideoAsset => ({
  id,
  scope: 'user',
  kind: 'reference',
  state: 'pending_upload',
  original_filename: 'reference.png',
  mime_type: 'image/png',
  size_bytes: 16 * 1024 * 1024,
  width: 0,
  height: 0,
  duration_seconds: 0,
  codec: '',
  created_at: 100,
  updated_at: 100,
})

const multipartReservation = (): VideoUploadReservation => ({
  asset: asset(),
  upload_mode: 'multipart',
  part_size: 8 * 1024 * 1024,
  expires_at: 200,
  max_size_bytes: VIDEO_REFERENCE_FALLBACK_MAX_BYTES,
})

const uploadRequest = {
  filename: 'reference.png',
  mime_type: 'image/png',
  size_bytes: 16 * 1024 * 1024,
  purpose: 'reference' as const,
}

const protocolApi = (
  overrides: Partial<VideoUploadProtocolApi> = {}
): VideoUploadProtocolApi => ({
  create: async () => multipartReservation(),
  signPart: async (_id, partNumber) => ({
    method: 'PUT',
    url: `https://upload.test/part-${partNumber}`,
    headers: {},
    expires_at: 200,
  }),
  listParts: async () => [],
  complete: async () => ({ ...asset(), state: 'uploaded' }),
  abort: async () => undefined,
  ...overrides,
})

test('multipart reservation controls Uppy mode and server chunk size', async () => {
  let requestedMultipart = false
  const data = new Blob(['video-data'])
  const protocol = createVideoUploadProtocol({
    admin: () => false,
    api: protocolApi({
      create: async (request) => {
        requestedMultipart = request.multipart
        return multipartReservation()
      },
    }),
  })

  const meta = await protocol.prepare(data, uploadRequest)

  assert.equal(requestedMultipart, true)
  assert.equal(protocol.shouldUseMultipart({ meta }), true)
  assert.equal(protocol.getChunkSize(data), 8 * 1024 * 1024)
  assert.deepEqual(meta, {
    assetId: 41,
    uploadMode: 'multipart',
    uploadPartSize: 8 * 1024 * 1024,
    uploadExpiresAt: 200,
    uploadMaxSizeBytes: VIDEO_REFERENCE_FALLBACK_MAX_BYTES,
    uploadAdmin: false,
  })
  assert.deepEqual(protocol.createMultipartUpload({ meta }), {
    uploadId: '41',
    key: '41',
  })
})

test('single PUT is enabled only when the backend explicitly selects it', async () => {
  const request = {
    method: 'PUT' as const,
    url: 'https://upload.test/single',
    headers: { 'content-type': 'image/png' },
    expires_at: 200,
  }
  const data = new Blob(['image-data'])
  const protocol = createVideoUploadProtocol({
    admin: () => false,
    api: protocolApi({
      create: async () => ({
        asset: asset(),
        upload_mode: 'single',
        expires_at: 200,
        max_size_bytes: VIDEO_REFERENCE_FALLBACK_MAX_BYTES,
        request,
      }),
    }),
  })

  const meta = await protocol.prepare(data, uploadRequest)

  assert.equal(protocol.shouldUseMultipart({ meta }), false)
  assert.deepEqual(protocol.getUploadParameters({ meta }), {
    method: 'PUT',
    url: request.url,
    headers: request.headers,
  })

  const invalidProtocol = createVideoUploadProtocol({
    admin: () => false,
    api: protocolApi({
      create: async () =>
        ({ ...multipartReservation(), upload_mode: 'unknown' }) as never,
    }),
  })
  await assert.rejects(
    invalidProtocol.prepare(new Blob(['invalid']), uploadRequest),
    /Invalid video upload reservation/
  )
})

test('multipart resume maps uploaded parts into Uppy part records', async () => {
  const protocol = createVideoUploadProtocol({
    admin: () => false,
    api: protocolApi({
      listParts: async () => [
        { part_number: 2, size_bytes: 5, etag: 'etag-2' },
        { part_number: 1, size_bytes: 8 * 1024 * 1024, etag: 'etag-1' },
      ],
    }),
  })
  const meta = await protocol.prepare(new Blob(['video-data']), uploadRequest)

  assert.deepEqual(await protocol.listParts({ meta }), [
    { PartNumber: 1, Size: 8 * 1024 * 1024, ETag: 'etag-1' },
    { PartNumber: 2, Size: 5, ETag: 'etag-2' },
  ])
})

test('multipart reservation can be restored without creating a new upload', async () => {
  let createCalls = 0
  const data = new File(['video-data'], 'reference.png', {
    type: 'image/png',
    lastModified: 123,
  })
  const protocol = createVideoUploadProtocol({
    admin: () => false,
    api: protocolApi({
      create: async () => {
        createCalls += 1
        return multipartReservation()
      },
      listParts: async () => [
        { part_number: 1, size_bytes: 5, etag: 'existing-etag' },
      ],
    }),
  })

  const selection = await prepareVideoUploadSelection({
    file: data,
    purpose: 'reference',
    admin: false,
    uploadResumes: [
      {
        assetId: 41,
        admin: false,
        purpose: 'reference',
        uploadMode: 'multipart',
        partSize: 8 * 1024 * 1024,
        expiresAt: 200,
        maxSizeBytes: VIDEO_REFERENCE_FALLBACK_MAX_BYTES,
        fingerprint: getVideoUploadFingerprint(data),
      },
    ],
    claimedResumeAssetIds: new Set(),
    protocol,
    removeUploadResume: () => undefined,
    loadUpload: async () => ({
      ...asset(),
      upload_mode: 'multipart',
      upload_part_size: 8 * 1024 * 1024,
      upload_expires_at: 200,
    }),
    nowSeconds: 100,
  })

  assert.equal(createCalls, 0)
  assert.equal(selection.kind, 'upload')
  if (selection.kind !== 'upload') throw new Error('Expected upload selection')
  const meta = selection.meta
  assert.equal(selection.createdReservation, false)
  assert.equal(protocol.getChunkSize(data), 8 * 1024 * 1024)
  assert.deepEqual(await protocol.listParts({ meta }), [
    { PartNumber: 1, Size: 5, ETag: 'existing-etag' },
  ])
})

test('inaccessible persisted upload is discarded before creating a new reservation', async () => {
  const data = new File(['video-data'], 'reference.png', {
    type: 'image/png',
    lastModified: 123,
  })
  const removedAssetIds: number[] = []
  let createCalls = 0
  const protocol = createVideoUploadProtocol({
    admin: () => false,
    api: protocolApi({
      create: async () => {
        createCalls += 1
        return { ...multipartReservation(), asset: asset(42) }
      },
    }),
  })
  const forbidden = Object.assign(new Error('forbidden'), {
    isAxiosError: true,
    response: { status: 403 },
  })

  const selection = await prepareVideoUploadSelection({
    file: data,
    purpose: 'reference',
    admin: false,
    uploadResumes: [
      {
        assetId: 41,
        admin: false,
        purpose: 'reference',
        uploadMode: 'multipart',
        partSize: 8 * 1024 * 1024,
        expiresAt: 200,
        maxSizeBytes: VIDEO_REFERENCE_FALLBACK_MAX_BYTES,
        fingerprint: getVideoUploadFingerprint(data),
      },
    ],
    claimedResumeAssetIds: new Set(),
    protocol,
    removeUploadResume: (assetId) => removedAssetIds.push(assetId),
    loadUpload: async () => {
      throw forbidden
    },
    nowSeconds: 100,
  })

  assert.equal(selection.kind, 'upload')
  if (selection.kind !== 'upload') throw new Error('Expected upload selection')
  assert.equal(selection.createdReservation, true)
  assert.equal(selection.meta.assetId, 42)
  assert.equal(createCalls, 1)
  assert.deepEqual(removedAssetIds, [41])
})

test('multipart completion sorts part numbers and preserves ETags', async () => {
  let completedParts: unknown
  const completedAsset = { ...asset(), state: 'uploaded' as const }
  const protocol = createVideoUploadProtocol({
    admin: () => false,
    api: protocolApi({
      complete: async (_id, request) => {
        completedParts = request.parts
        return completedAsset
      },
    }),
  })
  const meta = await protocol.prepare(new Blob(['video-data']), uploadRequest)

  const result = await protocol.completeMultipartUpload(
    { meta },
    {
      parts: [
        { PartNumber: 2, ETag: '"etag-2"' },
        { PartNumber: 1, ETag: '"etag-1"' },
      ],
    }
  )

  assert.deepEqual(completedParts, [
    { part_number: 1, etag: '"etag-1"' },
    { part_number: 2, etag: '"etag-2"' },
  ])
  assert.equal(result.asset, completedAsset)
})

test('multipart abort delegates to the upload DELETE endpoint', async () => {
  let abortedAssetId = 0
  const protocol = createVideoUploadProtocol({
    admin: () => false,
    api: protocolApi({
      abort: async (id) => {
        abortedAssetId = id
      },
    }),
  })
  const meta = await protocol.prepare(new Blob(['video-data']), uploadRequest)

  await protocol.abortUpload({ meta })

  assert.equal(abortedAssetId, 41)
})

test('failed multipart abort keeps the reservation available for retry', async () => {
  const firstFailure = new Error('temporary abort failure')
  let abortCalls = 0
  const protocol = createVideoUploadProtocol({
    admin: () => false,
    api: protocolApi({
      abort: async () => {
        abortCalls += 1
        if (abortCalls === 1) throw firstFailure
      },
    }),
  })
  const meta = await protocol.prepare(new Blob(['video-data']), uploadRequest)

  await assert.rejects(
    protocol.abortUpload({ meta }),
    (error) => error === firstFailure
  )
  await protocol.abortUpload({ meta })
  await protocol.abortUpload({ meta })

  assert.equal(abortCalls, 2)
})

test('reservation maximum remains authoritative after local selection', async () => {
  let abortedAssetId = 0
  const protocol = createVideoUploadProtocol({
    admin: () => false,
    api: protocolApi({
      create: async () => ({
        ...multipartReservation(),
        max_size_bytes: uploadRequest.size_bytes - 1,
      }),
      abort: async (id) => {
        abortedAssetId = id
      },
    }),
  })

  await assert.rejects(
    protocol.prepare(new Blob(['video-data']), uploadRequest),
    /Invalid video upload reservation/
  )
  assert.equal(abortedAssetId, 41)
})

test('each multipart signing attempt requests a fresh URL and preserves API errors', async () => {
  let signingAttempt = 0
  const apiError = new Error('access denied')
  const protocol = createVideoUploadProtocol({
    admin: () => false,
    api: protocolApi({
      signPart: async () => {
        signingAttempt += 1
        if (signingAttempt === 3) throw apiError
        return {
          method: 'PUT',
          url: `https://upload.test/signature-${signingAttempt}`,
          headers: {},
          expires_at: 200,
        }
      },
    }),
  })
  const meta = await protocol.prepare(new Blob(['video-data']), uploadRequest)

  const first = await protocol.signPart({ meta }, 1)
  const second = await protocol.signPart({ meta }, 1)
  assert.notEqual(first.url, second.url)
  await assert.rejects(
    protocol.signPart({ meta }, 1),
    (error) => error === apiError
  )
})

test('persisted drafts keep scalar parameters and asset IDs, never File or Blob data', () => {
  const unsafeDraft = {
    model_profile_id: 1,
    mode: 'image_to_video',
    prompt: 'Animate the reference',
    reference_asset_ids: [41],
    parameters: {
      duration: 5,
      enhance_prompt: true,
      transient_file: new Blob(['do-not-persist']),
    },
    source_file: new Blob(['do-not-persist']),
  }

  assert.deepEqual(sanitizeVideoStudioDraft(unsafeDraft), {
    model_profile_id: 1,
    mode: 'image_to_video',
    prompt: 'Animate the reference',
    reference_asset_ids: [41],
    parameters: { duration: 5, enhance_prompt: true },
  })
})

test('upload limits distinguish references from administrator sample videos', () => {
  const serverLimits = {
    reference_max_bytes: 7 * 1024 * 1024,
    sample_max_bytes: 384 * 1024 * 1024,
    archive_max_bytes: 2 * 1024 * 1024 * 1024,
  }
  assert.equal(
    getVideoUploadMaxBytes('reference', false),
    VIDEO_REFERENCE_FALLBACK_MAX_BYTES
  )
  assert.equal(
    getVideoUploadMaxBytes('reference', true),
    VIDEO_REFERENCE_FALLBACK_MAX_BYTES
  )
  assert.equal(
    getVideoUploadMaxBytes('sample', true),
    VIDEO_ADMIN_SAMPLE_FALLBACK_MAX_BYTES
  )
  assert.equal(
    getVideoUploadMaxBytes('reference', false, serverLimits),
    serverLimits.reference_max_bytes
  )
  assert.equal(
    getVideoUploadMaxBytes('sample', true, serverLimits),
    serverLimits.sample_max_bytes
  )
})

test('only completed user references use the user asset delete endpoint', () => {
  assert.equal(shouldDeleteVideoAssetOnRemove(asset(), false), true)
  assert.equal(
    shouldDeleteVideoAssetOnRemove({ ...asset(), scope: 'catalog' }, false),
    false
  )
  assert.equal(shouldDeleteVideoAssetOnRemove(asset(), true), false)
  assert.equal(
    shouldDeleteVideoAssetOnRemove({ ...asset(), kind: 'output' }, false),
    false
  )
})

test('resume metadata excludes binary data and requires an exact fingerprint', () => {
  const fingerprint = getVideoUploadFingerprint({
    name: 'sample.mp4',
    type: 'video/mp4',
    size: 1024,
    lastModified: 123,
  })
  const records = sanitizeVideoUploadResumeRecords([
    {
      assetId: 52,
      admin: true,
      purpose: 'sample',
      uploadMode: 'multipart',
      partSize: 8 * 1024 * 1024,
      expiresAt: 500,
      maxSizeBytes: VIDEO_ADMIN_SAMPLE_FALLBACK_MAX_BYTES,
      fingerprint,
      file: new Blob(['never persist']),
    },
  ])

  assert.deepEqual(records, [
    {
      assetId: 52,
      admin: true,
      purpose: 'sample',
      uploadMode: 'multipart',
      partSize: 8 * 1024 * 1024,
      expiresAt: 500,
      maxSizeBytes: VIDEO_ADMIN_SAMPLE_FALLBACK_MAX_BYTES,
      fingerprint,
    },
  ])
  assert.equal(
    findVideoUploadResume(records, fingerprint, 'sample', true, 100)?.assetId,
    52
  )
  const mismatchedFingerprints = [
    { ...fingerprint, name: 'other.mp4' },
    { ...fingerprint, type: 'video/webm' },
    { ...fingerprint, size: 1025 },
    { ...fingerprint, lastModified: 124 },
  ]
  for (const mismatch of mismatchedFingerprints) {
    assert.equal(
      findVideoUploadResume(records, mismatch, 'sample', true, 100),
      undefined
    )
  }
  assert.equal(
    findVideoUploadResume(records, fingerprint, 'sample', true, 500),
    undefined
  )
})

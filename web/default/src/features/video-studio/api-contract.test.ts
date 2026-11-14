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
import type { AxiosAdapter } from 'axios'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import * as studio from './api'
import type {
  CreateVideoRequest,
  CreateVideoUploadRequest,
  VideoAsset,
  VideoGeneration,
  VideoModelProfile,
  VideoModelProfileInput,
  VideoSample,
  VideoSampleInput,
} from './types'

const modelInput: VideoModelProfileInput = {
  model: 'verified-video',
  display_name: 'Verified video',
  description: '',
  provider_label: 'Provider',
  specification: { version: 2, modes: ['text_to_video'], parameters: [] },
  default_parameters: {},
  enabled: true,
  sort_order: 0,
}
const model: VideoModelProfile = {
  ...modelInput,
  id: 5,
  specification_version: 2,
  created_at: 1,
  updated_at: 1,
}
const sampleInput: VideoSampleInput = {
  model_profile_id: 5,
  title: 'Landscape',
  prompt: 'A quiet mountain scene',
  mode: 'text_to_video',
  parameters: {},
  reference_assets: [],
  video_asset_id: 12,
  aspect_ratio: 16 / 9,
  category: 'nature',
  status: 'draft',
  sort_order: 0,
}
const sample: VideoSample = {
  ...sampleInput,
  id: 9,
  model: model.model,
  model_display_name: model.display_name,
  model_version: 2,
  video_url: '',
  poster_url: '',
  preview_url: '',
  created_at: 1,
  updated_at: 1,
}
const asset: VideoAsset = {
  id: 12,
  scope: 'user',
  kind: 'reference',
  state: 'ready',
  original_filename: 'reference.png',
  mime_type: 'image/png',
  size_bytes: 1024,
  width: 512,
  height: 512,
  duration_seconds: 0,
  codec: '',
  created_at: 1,
  updated_at: 1,
}
const generation: VideoGeneration = {
  id: 8,
  task_id: 'task_contract',
  model_profile_id: 5,
  model: model.model,
  mode: 'text_to_video',
  prompt: sample.prompt,
  parameters: {},
  status: 'queued',
  progress: '0',
  quota: 100,
  created_at: 1,
  updated_at: 1,
}
const token = {
  required_group: 'video',
  has_usable_token: true,
  can_create: false,
  status: 'ready',
  token: { id: 17, name: 'Video Studio', group: 'video' },
}
const request: CreateVideoRequest = {
  token_id: 17,
  model: model.model,
  specification_version: 2,
  mode: 'text_to_video',
  prompt: sample.prompt,
  parameters: {},
  reference_assets: [],
  max_quota: 100,
  quote_hash: 'a'.repeat(64),
  quote_expires_at: 2_000_000_000,
}
const upload: CreateVideoUploadRequest = {
  filename: 'reference.png',
  mime_type: 'image/png',
  size_bytes: 1024,
  purpose: 'reference',
  multipart: true,
}
const signedRequest = {
  method: 'PUT',
  url: 'https://objects.example/signed-part',
  headers: { 'Content-Type': 'image/png' },
  expires_at: 2_000_000_000,
}
const signal = new AbortController().signal
const originalAdapter = api.defaults.adapter

type JsonCall = {
  name: string
  method: string
  url: string
  invoke: () => Promise<unknown>
  response: unknown
  idempotencyKey?: string
}
const jsonCalls: JsonCall[] = [
  {
    name: 'user model catalog',
    method: 'get',
    url: '/api/video-studio/models',
    invoke: () => studio.getVideoModels(17),
    response: [model],
  },
  {
    name: 'user sample catalog',
    method: 'get',
    url: '/api/video-studio/samples',
    invoke: () => studio.getVideoSamples(17, { limit: 10, model: model.model }),
    response: { items: [sample] },
  },
  {
    name: 'user sample detail',
    method: 'get',
    url: '/api/video-studio/samples/9',
    invoke: () => studio.getVideoSample(9, 17),
    response: sample,
  },
  {
    name: 'Studio token capability',
    method: 'get',
    url: '/api/video-studio/token',
    invoke: studio.getVideoTokenCapability,
    response: token,
  },
  {
    name: 'Studio token creation',
    method: 'post',
    url: '/api/video-studio/token',
    invoke: studio.createVideoToken,
    response: { ...token, created: true },
  },
  {
    name: 'generation quote',
    method: 'post',
    url: '/pg/videos/quote',
    invoke: () => studio.quoteVideoGeneration(request),
    response: {
      quota: 100,
      request_hash: request.quote_hash,
      expires_at: request.quote_expires_at,
    },
  },
  {
    name: 'paid generation submission',
    method: 'post',
    url: '/pg/videos',
    invoke: () => studio.createVideoGeneration(request, 'submit-key'),
    response: { task_id: 'task_contract' },
    idempotencyKey: 'submit-key',
  },
  {
    name: 'generation list',
    method: 'get',
    url: '/api/video-studio/generations',
    invoke: () => studio.getVideoGenerations({ limit: 10, status: 'queued' }),
    response: { items: [generation] },
  },
  {
    name: 'generation detail',
    method: 'get',
    url: '/api/video-studio/generations/8',
    invoke: () => studio.getVideoGeneration(8, { silent: true }),
    response: generation,
  },
  {
    name: 'generation deletion',
    method: 'delete',
    url: '/api/video-studio/generations/8',
    invoke: () => studio.deleteVideoGeneration(8),
    response: {},
  },
  {
    name: 'reference asset deletion',
    method: 'delete',
    url: '/api/video-studio/assets/12',
    invoke: () => studio.deleteVideoAsset(12),
    response: {},
  },
  {
    name: 'submission receipt lookup',
    method: 'get',
    url: '/api/video-studio/submissions/by-key',
    invoke: () => studio.getVideoSubmissionByKey('lookup-key'),
    response: {
      status: 'accepted',
      task_id: 'task_contract',
      generation_id: 8,
    },
    idempotencyKey: 'lookup-key',
  },
  {
    name: 'admin model profiles',
    method: 'get',
    url: '/api/admin/video-studio/model-profiles',
    invoke: studio.getAdminVideoModels,
    response: [model],
  },
  {
    name: 'admin model candidates',
    method: 'get',
    url: '/api/admin/video-studio/model-candidates',
    invoke: studio.getAdminVideoModelCandidates,
    response: [model.model],
  },
  {
    name: 'admin model creation',
    method: 'post',
    url: '/api/admin/video-studio/model-profiles',
    invoke: () => studio.createAdminVideoModel(modelInput),
    response: model,
  },
  {
    name: 'admin model update',
    method: 'put',
    url: '/api/admin/video-studio/model-profiles/5',
    invoke: () => studio.updateAdminVideoModel(5, modelInput),
    response: model,
  },
  {
    name: 'admin model deletion',
    method: 'delete',
    url: '/api/admin/video-studio/model-profiles/5',
    invoke: () => studio.deleteAdminVideoModel(5),
    response: {},
  },
  {
    name: 'admin sample catalog',
    method: 'get',
    url: '/api/admin/video-studio/samples',
    invoke: () => studio.getAdminVideoSamples({ limit: 10 }),
    response: { items: [sample] },
  },
  {
    name: 'admin sample detail',
    method: 'get',
    url: '/api/admin/video-studio/samples/9',
    invoke: () => studio.getAdminVideoSample(9),
    response: sample,
  },
  {
    name: 'admin sample creation',
    method: 'post',
    url: '/api/admin/video-studio/samples',
    invoke: () => studio.createAdminVideoSample(sampleInput),
    response: sample,
  },
  {
    name: 'admin sample update',
    method: 'put',
    url: '/api/admin/video-studio/samples/9',
    invoke: () => studio.updateAdminVideoSample(9, sampleInput),
    response: sample,
  },
  {
    name: 'admin sample deletion',
    method: 'delete',
    url: '/api/admin/video-studio/samples/9',
    invoke: () => studio.deleteAdminVideoSample(9),
    response: {},
  },
  ...[false, true].flatMap((admin): JsonCall[] => {
    const prefix = admin ? '/api/admin/video-studio' : '/api/video-studio'
    const owner = admin ? 'admin' : 'user'
    return [
      {
        name: `${owner} upload reservation`,
        method: 'post',
        url: `${prefix}/uploads`,
        invoke: () => studio.createVideoUpload(upload, admin),
        response: {
          asset,
          upload_mode: 'multipart',
          part_size: 8_388_608,
          expires_at: 2_000_000_000,
          max_size_bytes: 20_971_520,
        },
      },
      {
        name: `${owner} upload completion`,
        method: 'post',
        url: `${prefix}/uploads/12/complete`,
        invoke: () =>
          studio.completeVideoUpload(
            12,
            { parts: [{ part_number: 1, etag: 'part-etag' }] },
            admin,
            signal
          ),
        response: asset,
      },
      {
        name: `${owner} upload part signing`,
        method: 'post',
        url: `${prefix}/uploads/12/parts/1`,
        invoke: () => studio.signVideoUploadPart(12, 1, admin, signal),
        response: signedRequest,
      },
      {
        name: `${owner} uploaded part listing`,
        method: 'get',
        url: `${prefix}/uploads/12/parts`,
        invoke: () => studio.listVideoUploadParts(12, admin, signal),
        response: {
          parts: [{ part_number: 1, size_bytes: 1024, etag: 'part-etag' }],
        },
      },
      {
        name: `${owner} upload cancellation`,
        method: 'delete',
        url: `${prefix}/uploads/12`,
        invoke: () => studio.abortVideoUpload(12, admin, signal),
        response: {},
      },
      {
        name: `${owner} upload status`,
        method: 'get',
        url: `${prefix}/uploads/12`,
        invoke: () => studio.getVideoUpload(12, admin),
        response: asset,
      },
      {
        name: `${owner} asset detail`,
        method: 'get',
        url: `${prefix}/assets/12`,
        invoke: () => studio.getVideoAsset(12, admin),
        response: asset,
      },
    ]
  }),
]

beforeEach(() => {
  useAuthStore.getState().auth.setUser({ id: 7, username: 'seven', role: 10 })
})
afterEach(() => {
  api.defaults.adapter = originalAdapter
  useAuthStore.getState().auth.reset()
})

test.each(jsonCalls)(
  '$name declares Console API 4 at the JSON transport',
  async (entry) => {
    const transport = vi.fn<AxiosAdapter>(async (config) => ({
      status: 200,
      statusText: 'OK',
      data: { success: true, data: entry.response },
      headers: {},
      config,
    }))
    api.defaults.adapter = transport
    const result = await entry.invoke()
    expect(transport).toHaveBeenCalledTimes(1)
    const config = transport.mock.calls[0][0]
    expect(config.method).toBe(entry.method)
    expect(config.url).toBe(entry.url)
    expect(config.headers.get('X-KKAI-Console-API')).toBe('4')
    if (entry.idempotencyKey) {
      expect(config.headers.get('Idempotency-Key')).toBe(entry.idempotencyKey)
    }
    if (entry.response === signedRequest) expect(result).toEqual(signedRequest)
  }
)

test('concurrent receipt lookups with different keys remain separate requests', async () => {
  const transport = vi.fn<AxiosAdapter>(async (config) => ({
    status: 200,
    statusText: 'OK',
    data: {
      success: true,
      data: {
        status: 'accepted',
        task_id: `task_${config.headers.get('Idempotency-Key')}`,
      },
    },
    headers: {},
    config,
  }))
  api.defaults.adapter = transport
  const [first, second] = await Promise.all([
    studio.getVideoSubmissionByKey('first'),
    studio.getVideoSubmissionByKey('second'),
  ])
  expect(transport).toHaveBeenCalledTimes(2)
  expect(first.task_id).toBe('task_first')
  expect(second.task_id).toBe('task_second')
  for (const [config] of transport.mock.calls) {
    expect(config.disableDuplicate).toBe(true)
    expect(config.headers.get('X-KKAI-Console-API')).toBe('4')
  }
})

test('the Studio version does not leak into unrelated requests or media paths', async () => {
  const transport = vi.fn<AxiosAdapter>(async (config) => ({
    status: 200,
    statusText: 'OK',
    data: { success: true, data: [] },
    headers: {},
    config,
  }))
  api.defaults.adapter = transport
  await studio.getVideoModels(17)
  await api.get('/api/user/self')
  expect(transport.mock.calls[1][0].headers.has('X-KKAI-Console-API')).toBe(
    false
  )
  expect(studio.getVideoAssetContentUrl(12)).toBe(
    '/api/video-studio/assets/12/content'
  )
  expect(studio.getVideoAssetContentUrl(12, true)).toBe(
    '/api/video-studio/assets/12/download'
  )
})

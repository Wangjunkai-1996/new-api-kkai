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
import { AxiosError, type AxiosAdapter } from 'axios'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore, type AuthBundle } from '@/stores/auth-store'

import { createVideoGeneration } from './api'
import type { CreateVideoRequest } from './types'

const originalAdapter = api.defaults.adapter
const ownerKey = 'video-studio-submissions:user:7'
const accepted = {
  version: 1,
  userId: 7,
  operationId: 'operation-previous',
  requestFingerprint: 'a'.repeat(64),
  createdAt: 1,
  updatedAt: 2,
  state: 'accepted',
  taskId: 'task_previous',
}
const bundle: AuthBundle = {
  access_token: 'test-access',
  token_type: 'Bearer',
  access_expires_at: 2_000_000_000,
  user: { id: 7, username: 'seven', role: 1 },
  session: {
    sid: 'test-session',
    current: true,
    login_method: 'password',
    ip: '',
    user_agent: '',
    created_at: 1,
    last_active_at: 1,
    expires_at: 2_000_000_000,
  },
}
const request: CreateVideoRequest = {
  token_id: 17,
  model: 'video',
  mode: 'text_to_video',
  prompt: 'A quiet scene',
  parameters: {},
  reference_assets: [],
  max_quota: 100,
  quote_hash: 'a'.repeat(64),
  quote_expires_at: 2_000_000_000,
}
const transport = vi.fn<AxiosAdapter>(async (config) => ({
  status: 200,
  statusText: 'OK',
  data: { task_id: 'task_new' },
  headers: {},
  config,
}))
let interceptorId: number | undefined

beforeEach(() => {
  window.localStorage.clear()
  useAuthStore.getState().auth.setBundle(bundle)
  api.defaults.adapter = transport
})

afterEach(() => {
  api.defaults.adapter = originalAdapter
  if (interceptorId !== undefined) api.interceptors.request.eject(interceptorId)
  interceptorId = undefined
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
  window.localStorage.clear()
})

test.each([
  [
    'sending',
    JSON.stringify({
      version: 1,
      records: [{ ...accepted, state: 'sending' }],
    }),
  ],
  [
    'unknown',
    JSON.stringify({
      version: 1,
      records: [{ ...accepted, state: 'unknown' }],
    }),
  ],
  ['future storage', JSON.stringify({ version: 2, records: [] })],
  [
    'future record',
    JSON.stringify({ version: 1, records: [{ ...accepted, version: 2 }] }),
  ],
  ['corrupt JSON', '{'],
  [
    'missing task',
    JSON.stringify({
      version: 1,
      records: [{ ...accepted, taskId: undefined }],
    }),
  ],
  [
    'wrong owner',
    JSON.stringify({ version: 1, records: [{ ...accepted, userId: 8 }] }),
  ],
  [
    'duplicate operations',
    JSON.stringify({ version: 1, records: [accepted, accepted] }),
  ],
])(
  'rollback blocks %s records without erasing recovery data',
  async (_label, raw) => {
    window.localStorage.setItem(ownerKey, raw)
    await expect(
      createVideoGeneration(request, 'operation-new')
    ).rejects.toThrow('videoStudio.submissionUnknown')
    expect(transport).not.toHaveBeenCalled()
    expect(window.localStorage.getItem(ownerKey)).toBe(raw)
  }
)

test.each([null, JSON.stringify({ version: 1, records: [accepted] })])(
  'allows absent or fully accepted recovery state',
  async (raw) => {
    if (raw !== null) window.localStorage.setItem(ownerKey, raw)
    await expect(
      createVideoGeneration(request, 'operation-new')
    ).resolves.toEqual({
      task_id: 'task_new',
    })
    expect(transport).toHaveBeenCalledTimes(1)
    expect(transport.mock.calls[0][0].headers.get('Idempotency-Key')).toBe(
      'operation-new'
    )
    expect(window.localStorage.getItem(ownerKey)).toBe(raw)
  }
)

test('another account recovery record does not block the current account', async () => {
  window.localStorage.setItem('video-studio-submissions:user:8', '{')
  await expect(
    createVideoGeneration(request, 'operation-new')
  ).resolves.toEqual({
    task_id: 'task_new',
  })
  expect(transport).toHaveBeenCalledTimes(1)
  expect(window.localStorage.getItem('video-studio-submissions:user:8')).toBe(
    '{'
  )
})

test('rechecks newly unresolved recovery state after asynchronous interceptors', async () => {
  const raw = JSON.stringify({
    version: 1,
    records: [{ ...accepted, state: 'unknown' }],
  })
  interceptorId = api.interceptors.request.use(async (config) => {
    await Promise.resolve()
    window.localStorage.setItem(ownerKey, raw)
    return config
  })
  await expect(createVideoGeneration(request, 'operation-new')).rejects.toThrow(
    'videoStudio.submissionUnknown'
  )
  expect(transport).not.toHaveBeenCalled()
  expect(window.localStorage.getItem(ownerKey)).toBe(raw)
})

test('blocks a changed session even when the original account returns before dispatch', async () => {
  interceptorId = api.interceptors.request.use(async (config) => {
    await Promise.resolve()
    useAuthStore.getState().auth.reset()
    useAuthStore.getState().auth.setBundle(bundle)
    return config
  })
  await expect(createVideoGeneration(request, 'operation-new')).rejects.toThrow(
    'videoStudio.submissionUnknown'
  )
  expect(transport).not.toHaveBeenCalled()
})

test('fails closed when recovery storage cannot be read', async () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('Storage denied')
  })
  await expect(createVideoGeneration(request, 'operation-new')).rejects.toThrow(
    'videoStudio.submissionUnknown'
  )
  expect(transport).not.toHaveBeenCalled()
})

test('does not replay the paid request after an unauthorized response', async () => {
  transport.mockImplementationOnce(async (config) => {
    throw new AxiosError('Unauthorized', 'ERR_BAD_REQUEST', config, undefined, {
      status: 401,
      statusText: 'Unauthorized',
      data: { message: 'Expired' },
      headers: {},
      config,
    })
  })
  await expect(
    createVideoGeneration(request, 'operation-new')
  ).rejects.toBeInstanceOf(AxiosError)
  expect(transport).toHaveBeenCalledTimes(1)
})

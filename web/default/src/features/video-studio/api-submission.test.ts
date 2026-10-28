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
import { refreshAuthentication } from '@/lib/auth-session'
import { useAuthStore, type AuthBundle } from '@/stores/auth-store'

import { createVideoGeneration } from './api'
import type { CreateVideoRequest } from './types'

vi.mock('@/lib/auth-session', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/auth-session')>()),
  refreshAuthentication: vi.fn(),
}))

const originalAdapter = api.defaults.adapter
const bundle: AuthBundle = {
  access_token: 'test-access',
  token_type: 'Bearer',
  access_expires_at: 1,
  user: { id: 7, username: 'seven', role: 1 },
  session: {
    sid: 'test-session',
    current: true,
    login_method: 'password',
    ip: '',
    user_agent: '',
    created_at: 1,
    last_active_at: 1,
    expires_at: 1,
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
let interceptorId: number | undefined

beforeEach(() => {
  useAuthStore.getState().auth.setBundle(bundle)
  vi.mocked(refreshAuthentication).mockResolvedValue({
    kind: 'authenticated',
    bundle,
  })
})
afterEach(() => {
  api.defaults.adapter = originalAdapter
  if (interceptorId !== undefined) api.interceptors.request.eject(interceptorId)
  interceptorId = undefined
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})

test('a paid POST receiving 401 reaches the real HTTP transport only once and is never refreshed or replayed', async () => {
  const beforeDispatch = vi.fn()
  const transport = vi.fn<AxiosAdapter>(async (config) => {
    throw new AxiosError('Unauthorized', 'ERR_BAD_REQUEST', config, undefined, {
      status: 401,
      statusText: 'Unauthorized',
      data: { message: 'Expired' },
      headers: {},
      config,
    })
  })
  api.defaults.adapter = transport
  await expect(
    createVideoGeneration(request, 'operation-1', beforeDispatch)
  ).rejects.toBeInstanceOf(AxiosError)
  expect(transport).toHaveBeenCalledTimes(1)
  expect(transport.mock.calls[0][0].url).toBe('/pg/videos')
  expect(transport.mock.calls[0][0].headers.get('Idempotency-Key')).toBe(
    'operation-1'
  )
  expect(beforeDispatch).toHaveBeenCalledTimes(1)
  expect(refreshAuthentication).not.toHaveBeenCalled()
})

test('an asynchronous interceptor cannot send under another account, even when the original account returns', async () => {
  const transport = vi.fn<AxiosAdapter>(async (config) => ({
    status: 200,
    statusText: 'OK',
    data: { task_id: 'task_success' },
    headers: {},
    config,
  }))
  api.defaults.adapter = transport
  interceptorId = api.interceptors.request.use(async (config) => {
    await Promise.resolve()
    useAuthStore.getState().auth.reset()
    useAuthStore.getState().auth.setBundle(bundle)
    return config
  })
  const beforeDispatch = vi.fn()
  await expect(
    createVideoGeneration(request, 'operation-2', beforeDispatch)
  ).rejects.toThrow('identity changed')
  expect(beforeDispatch).not.toHaveBeenCalled()
  expect(transport).not.toHaveBeenCalled()
})

test('the final frozen-context guard runs after asynchronous interceptors and before the transport', async () => {
  const transport = vi.fn<AxiosAdapter>(async (config) => ({
    status: 200,
    statusText: 'OK',
    data: { task_id: 'task_success' },
    headers: {},
    config,
  }))
  api.defaults.adapter = transport
  let quoteValid = true
  interceptorId = api.interceptors.request.use(async (config) => {
    await Promise.resolve()
    quoteValid = false
    return config
  })
  const beforeDispatch = vi.fn(() => {
    if (!quoteValid) throw new Error('Quote expired before transport')
  })
  await expect(
    createVideoGeneration(request, 'operation-3', beforeDispatch)
  ).rejects.toThrow('Quote expired before transport')
  expect(beforeDispatch).toHaveBeenCalledTimes(1)
  expect(transport).not.toHaveBeenCalled()
})

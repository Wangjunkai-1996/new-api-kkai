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
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { clearAuthentication, refreshAuthentication } from '@/lib/auth-session'
import { markServerErrorHandled } from '@/lib/handle-server-error'
import { useAuthStore, type AuthBundle } from '@/stores/auth-store'

import { api } from './http-client'

vi.mock('@/lib/auth-session', () => ({
  applyAuthRotation: vi.fn(),
  clearAuthentication: vi.fn(),
  getFreshAuthHeaders: vi.fn(),
  refreshAuthentication: vi.fn(),
}))
vi.mock('@/lib/handle-server-error', () => ({
  handleServerError: vi.fn(),
  markServerErrorHandled: vi.fn(),
}))

const originalAdapter = api.defaults.adapter
const bundle: AuthBundle = {
  access_token: 'expired-owner-token',
  token_type: 'Bearer',
  access_expires_at: 1,
  user: { id: 7, username: 'owner', role: 1, tenant_owner: true },
  session: {
    sid: 'owner-session',
    current: true,
    login_method: 'password',
    ip: '',
    user_agent: '',
    created_at: 1,
    last_active_at: 1,
    expires_at: 1,
  },
}
const refreshedBundle: AuthBundle = {
  ...bundle,
  access_token: 'fresh-owner-token',
  session: { ...bundle.session, sid: 'refreshed-owner-session' },
}
const replace = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  useAuthStore.getState().auth.setBundle(bundle)
  vi.stubGlobal('window', {
    location: {
      origin: 'https://tenant.example',
      host: 'tenant.example',
      pathname: '/reseller/earnings',
      href: 'https://tenant.example/reseller/earnings?period=week#withdraw',
      replace,
    },
  })
  api.defaults.adapter = async (config) => {
    throw new AxiosError('HTTP 401', 'ERR_BAD_REQUEST', config, undefined, {
      data: { message: 'Expired' },
      status: 401,
      statusText: 'Unauthorized',
      headers: {},
      config,
    })
  }
})

afterEach(() => {
  api.defaults.adapter = originalAdapter
  vi.unstubAllGlobals()
  useAuthStore.getState().auth.reset()
})

it.each(['anonymous', 'out_of_sync'] as const)(
  'returns expired owners to their sign-in mode and deep destination after %s',
  async (kind) => {
    vi.mocked(refreshAuthentication).mockResolvedValue({ kind })
    await expect(
      api.get('/api/reseller/earnings', { skipErrorHandler: true })
    ).rejects.toBeInstanceOf(AxiosError)
    const target = new URL(replace.mock.calls[0][0], 'https://tenant.example')
    expect(target.pathname).toBe('/sign-in')
    expect(target.searchParams.get('owner_login')).toBe('true')
    expect(target.searchParams.get('redirect')).toBe(
      '/reseller/earnings?period=week#withdraw'
    )
  }
)

it('retains owner mode if the retried request also reports an expired session', async () => {
  await expect(
    api.get('/api/reseller/earnings', {
      authRetry: true,
      skipErrorHandler: true,
    })
  ).rejects.toBeInstanceOf(AxiosError)
  expect(clearAuthentication).toHaveBeenCalledExactlyOnceWith(false)
  expect(refreshAuthentication).not.toHaveBeenCalled()
  expect(
    new URL(
      replace.mock.calls[0][0],
      'https://tenant.example'
    ).searchParams.get('owner_login')
  ).toBe('true')
})

it('retries with the session adopted by a successful refresh', async () => {
  const unauthorized = api.defaults.adapter as AxiosAdapter
  const request = vi.fn<AxiosAdapter>(async (config) => {
    if (!config.authRetry) return unauthorized(config)
    expect(config.headers.get('Authorization')).toBe('Bearer fresh-owner-token')
    expect(config.authSessionSID).toBe(refreshedBundle.session.sid)
    return {
      data: { success: true },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  })
  api.defaults.adapter = request
  vi.mocked(refreshAuthentication).mockImplementation(async () => {
    useAuthStore.getState().auth.setBundle(refreshedBundle)
    return { kind: 'authenticated', bundle: refreshedBundle }
  })

  const response = await api.get('/api/reseller/earnings')

  expect(response.data).toEqual({ success: true })
  expect(request).toHaveBeenCalledTimes(2)
  expect(markServerErrorHandled).not.toHaveBeenCalled()
  expect(replace).not.toHaveBeenCalled()
})

it.each(['signed out', 'another session'] as const)(
  'does not replay a successful refresh after moving to %s',
  async (nextSession) => {
    const request = vi.fn(api.defaults.adapter as AxiosAdapter)
    api.defaults.adapter = request
    vi.mocked(refreshAuthentication).mockImplementation(async () => {
      useAuthStore.getState().auth.setBundle(refreshedBundle)
      if (nextSession === 'signed out') {
        useAuthStore.getState().auth.reset()
      } else {
        useAuthStore.getState().auth.setBundle({
          ...refreshedBundle,
          session: { ...refreshedBundle.session, sid: 'replacement-session' },
        })
      }
      return { kind: 'authenticated', bundle: refreshedBundle }
    })

    const error = await api
      .get('/api/reseller/earnings')
      .catch((error: unknown) => error)

    expect(error).toBeInstanceOf(AxiosError)
    expect(request).toHaveBeenCalledTimes(1)
    expect(markServerErrorHandled).toHaveBeenCalledExactlyOnceWith(error)
    expect(replace).not.toHaveBeenCalled()
  }
)

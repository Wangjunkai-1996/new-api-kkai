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
import { QueryClient } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it } from 'vitest'

import { useAuthStore, type AuthBundle } from '@/stores/auth-store'

import { installAuthQueryCacheBoundary } from './auth-query-cache'

const bundle: AuthBundle = {
  access_token: 'customer-token',
  token_type: 'Bearer',
  access_expires_at: 2_000_000_000,
  user: { id: 4, username: 'customer', role: 1 },
  session: {
    sid: 'customer-session',
    current: true,
    login_method: 'password',
    ip: '',
    user_agent: '',
    created_at: 1,
    last_active_at: 1,
    expires_at: 2_000_000_000,
  },
}
let client: QueryClient
let unsubscribe: () => void

beforeEach(() => {
  useAuthStore.getState().auth.setBundle(bundle)
  client = new QueryClient()
  unsubscribe = installAuthQueryCacheBoundary(client)
})

afterEach(() => {
  unsubscribe()
  client.clear()
  useAuthStore.getState().auth.reset()
})

it.each(['user switch', 'session replacement', 'expiry'])(
  'removes private and legacy query data on %s while preserving Host configuration',
  (change) => {
    const keys = [
      ['private-user', 4, 'keys'],
      ['dashboard', 'overview', 'api-keys'],
      ['reseller', 'tenant.example', 4, 'me'],
    ]
    for (const key of keys) client.setQueryData(key, { private: true })
    client.setQueryData(['site-config', 'tenant.example'], { tenant_id: 12 })
    client.setQueryData(['status', 'tenant.example'], {
      register_enabled: true,
    })
    client
      .getMutationCache()
      .build(client, { mutationKey: ['private-mutation'] })

    if (change === 'expiry') {
      useAuthStore.getState().auth.reset()
    } else {
      useAuthStore.getState().auth.setBundle({
        ...bundle,
        user:
          change === 'user switch'
            ? { id: 7, username: 'owner', role: 1, tenant_owner: true }
            : bundle.user,
        session: { ...bundle.session, sid: 'replacement-session' },
      })
    }

    for (const key of keys) expect(client.getQueryData(key)).toBeUndefined()
    expect(client.getQueryData(['site-config', 'tenant.example'])).toEqual({
      tenant_id: 12,
    })
    expect(client.getQueryData(['status', 'tenant.example'])).toEqual({
      register_enabled: true,
    })
    expect(client.getMutationCache().getAll()).toHaveLength(0)
  }
)

it('retains current-user data during access-token rotation and profile refresh', () => {
  client.setQueryData(['dashboard', 'overview', 'api-keys'], ['current-key'])
  useAuthStore
    .getState()
    .auth.setBundle({ ...bundle, access_token: 'rotated-token' })
  useAuthStore
    .getState()
    .auth.setUser({ ...bundle.user, display_name: 'Updated name' })
  expect(client.getQueryData(['dashboard', 'overview', 'api-keys'])).toEqual([
    'current-key',
  ])
})

it('clears previously anonymous personalized results when a user signs in', () => {
  useAuthStore.getState().auth.reset()
  client.setQueryData(['pricing'], ['anonymous-price'])
  useAuthStore.getState().auth.setBundle(bundle)
  expect(client.getQueryData(['pricing'])).toBeUndefined()
})

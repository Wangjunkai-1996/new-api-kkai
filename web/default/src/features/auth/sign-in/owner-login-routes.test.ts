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
import { beforeEach, expect, it, vi } from 'vitest'
import type { z } from 'zod'

import { getResellerMe } from '@/features/reseller/api'
import { resolveAuthentication } from '@/lib/auth-session'
import { Route as SignInRoute } from '@/routes/(auth)/sign-in'
import { Route as AuthenticatedRoute } from '@/routes/_authenticated/route'
import { Route as ResellerRoute } from '@/routes/reseller'
import { useAuthStore } from '@/stores/auth-store'

const site = vi.hoisted(() => ({ tenantID: 12 }))

vi.mock('@/features/auth/sign-in', () => ({ SignIn: () => null }))
vi.mock('@/components/layout', () => ({ AuthenticatedLayout: () => null }))
vi.mock('@/features/reseller/api', () => ({ getResellerMe: vi.fn() }))
vi.mock('@/lib/auth-session', () => ({ resolveAuthentication: vi.fn() }))
vi.mock('@/features/site-config', () => ({
  siteConfigQueryOptions: () => ({
    queryKey: ['site-config'],
    queryFn: async () => ({ tenant_id: site.tenantID }),
  }),
}))

type SignInGuard = (args: {
  search: { owner_login?: boolean; redirect?: string }
  context: { queryClient: QueryClient }
}) => Promise<void>
type ResellerGuard = (args: {
  location: { href: string }
  context: { queryClient: QueryClient }
}) => Promise<void>
const signInGuard = SignInRoute.options.beforeLoad as SignInGuard
const resellerGuard = ResellerRoute.options.beforeLoad as ResellerGuard
const authenticatedGuard = AuthenticatedRoute.options
  .beforeLoad as ResellerGuard

beforeEach(() => {
  vi.clearAllMocks()
  site.tenantID = 12
  useAuthStore.getState().auth.reset('complete')
})

it('accepts only a boolean owner mode, without coercing false-like strings', () => {
  const schema = SignInRoute.options.validateSearch as z.ZodType
  expect(schema.parse({ owner_login: true })).toEqual({ owner_login: true })
  expect(schema.parse({ owner_login: false })).toEqual({ owner_login: false })
  expect(schema.parse({ owner_login: 'false' })).toEqual({
    owner_login: undefined,
  })
})

it('sends anonymous management visitors to explicit owner sign-in', async () => {
  await expect(
    resellerGuard({
      location: { href: '/reseller/earnings' },
      context: { queryClient: new QueryClient() },
    })
  ).rejects.toMatchObject({
    options: {
      to: '/sign-in',
      search: { redirect: '/reseller/earnings', owner_login: true },
    },
  })
  expect(resolveAuthentication).toHaveBeenCalledOnce()
})

it.each([401, 403])(
  'lets a signed-in customer open owner sign-in after management returns %s',
  async (status) => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 4, username: 'customer', role: 1 })
    useAuthStore.setState((state) => ({
      auth: { ...state.auth, accessToken: 'customer-token' },
    }))
    vi.mocked(getResellerMe).mockRejectedValue({
      isAxiosError: true,
      response: { status },
    })
    await expect(
      resellerGuard({
        location: { href: '/reseller' },
        context: { queryClient: new QueryClient() },
      })
    ).rejects.toMatchObject({
      options: {
        to: '/sign-in',
        search: { redirect: '/reseller', owner_login: true },
      },
    })
    await expect(
      signInGuard({
        search: { owner_login: true },
        context: { queryClient: new QueryClient() },
      })
    ).resolves.toBeUndefined()
    expect(useAuthStore.getState().auth.user?.id).toBe(4)
  }
)

it.each([undefined, true])(
  'keeps authenticated owner navigation local and defaults to the workspace (mode=%s)',
  async (ownerLogin) => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 7, username: 'owner', role: 1, tenant_owner: true })
    useAuthStore.setState((state) => ({
      auth: { ...state.auth, accessToken: 'owner-token' },
    }))
    await expect(
      signInGuard({
        search: { owner_login: ownerLogin, redirect: 'https://evil.example/' },
        context: { queryClient: new QueryClient() },
      })
    ).rejects.toMatchObject({ options: { href: '/reseller', replace: true } })
    expect(getResellerMe).not.toHaveBeenCalled()
  }
)

it('lets an authenticated owner explicitly open customer login without silently returning to a dashboard', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 7, username: 'owner', role: 1, tenant_owner: true })
  useAuthStore.setState((state) => ({
    auth: { ...state.auth, accessToken: 'owner-token' },
  }))
  await expect(
    signInGuard({
      search: { owner_login: false },
      context: { queryClient: new QueryClient() },
    })
  ).resolves.toBeUndefined()
  expect(useAuthStore.getState().auth.user?.id).toBe(7)
})

it.each([0, 12])(
  'keeps ordinary signed-in users on their dashboard (tenant=%s)',
  async (tenantID) => {
    site.tenantID = tenantID
    useAuthStore
      .getState()
      .auth.setUser({ id: 4, username: 'customer', role: 1 })
    useAuthStore.setState((state) => ({
      auth: { ...state.auth, accessToken: 'customer-token' },
    }))
    await expect(
      signInGuard({ search: {}, context: { queryClient: new QueryClient() } })
    ).rejects.toMatchObject({ options: { href: '/dashboard', replace: true } })
  }
)

it('redirects a tenant owner away from customer-only authenticated routes', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 7, username: 'owner', role: 1, tenant_owner: true })
  useAuthStore.setState((state) => ({
    auth: { ...state.auth, accessToken: 'owner-token' },
  }))
  await expect(
    authenticatedGuard({
      location: { href: '/keys' },
      context: { queryClient: new QueryClient() },
    })
  ).rejects.toMatchObject({ options: { to: '/reseller', replace: true } })
})

it.each([false, true])(
  'rejects the platform Host without an owner-login loop (authenticated=%s)',
  async (authenticated) => {
    site.tenantID = 0
    if (authenticated) {
      useAuthStore
        .getState()
        .auth.setUser({ id: 7, username: 'platform-owner', role: 1 })
      useAuthStore.setState((state) => ({
        auth: { ...state.auth, accessToken: 'platform-token' },
      }))
    }
    await expect(
      resellerGuard({
        location: { href: '/reseller' },
        context: { queryClient: new QueryClient() },
      })
    ).rejects.toMatchObject({ options: { to: '/403' } })
    expect(getResellerMe).not.toHaveBeenCalled()
  }
)

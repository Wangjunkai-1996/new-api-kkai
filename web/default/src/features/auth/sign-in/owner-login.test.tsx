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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, expect, it, vi } from 'vitest'

import { login } from '../api'
import { SignIn } from './index'

const state = vi.hoisted(() => ({
  search: {} as { owner_login?: boolean; redirect?: string },
  tenantID: 12,
  handleLoginResult: vi.fn().mockResolvedValue(false),
}))

vi.mock('@tanstack/react-router', () => ({
  useSearch: () => state.search,
  Link: (props: {
    children: ReactNode
    to: string
    search?: Record<string, unknown>
  }) => (
    <a
      href={`${props.to}?${new URLSearchParams(
        Object.entries(props.search ?? {})
          .filter(([, value]) => value !== undefined)
          .map(([key, value]) => [key, String(value)])
      )}`}
    >
      {props.children}
    </a>
  ),
}))
vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: { tenant_id: state.tenantID } }),
}))
vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({ status: { register_enabled: true, github_oauth: true } }),
}))
vi.mock('../auth-layout', () => ({
  AuthLayout: (props: { children: ReactNode }) => props.children,
}))
vi.mock('../components/oauth-providers', () => ({
  OAuthProviders: () => <button type='button'>OAuth sign in</button>,
}))
vi.mock('../hooks/use-turnstile', () => ({
  useTurnstile: () => ({ validateTurnstile: () => true }),
}))
vi.mock('../hooks/use-auth-redirect', () => ({
  useAuthRedirect: () => ({ handleLoginResult: state.handleLoginResult }),
}))
vi.mock('@/lib/passkey', () => ({ isPasskeySupported: async () => false }))
vi.mock('../api', () => ({ login: vi.fn() }))

beforeEach(() => {
  state.tenantID = 12
  state.search = {}
  vi.clearAllMocks()
  vi.mocked(login).mockResolvedValue({
    success: true,
    message: '',
    data: undefined,
  })
})

it('keeps reseller management details out of the tenant customer login', () => {
  render(<SignIn />)
  expect(screen.getByRole('link', { name: 'Sign up' })).toBeVisible()
  expect(
    screen.queryByRole('link', { name: 'Reseller management sign in' })
  ).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'OAuth sign in' })).toBeVisible()
})

it('uses KKAI credentials in explicit owner mode and hands the reseller destination to shared verification', async () => {
  state.search = { owner_login: true }
  render(<SignIn />)
  expect(
    screen.getByRole('heading', { name: 'Reseller management sign in' })
  ).toBeVisible()
  expect(
    screen.getByText(
      'Use your KKAI main-site account to manage this reseller site.'
    )
  ).toBeVisible()
  expect(
    screen.queryByRole('link', { name: 'Sign up' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('link', { name: 'Forgot password?' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'OAuth sign in' })
  ).not.toBeInTheDocument()
  expect(
    screen.getByRole('link', { name: 'Customer sign in' })
  ).toHaveAttribute('href', '/sign-in?owner_login=false')
  const user = userEvent.setup()
  await user.type(
    screen.getByPlaceholderText('Enter your username or email'),
    'owner'
  )
  await user.type(
    screen.getByPlaceholderText('Enter password'),
    'owner-password'
  )
  await user.click(screen.getByRole('button', { name: 'Sign in' }))
  await waitFor(() =>
    expect(login).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        username: 'owner',
        password: 'owner-password',
        owner_login: true,
      })
    )
  )
  expect(state.handleLoginResult).toHaveBeenCalledExactlyOnceWith(
    undefined,
    '/reseller',
    true
  )
})

it('submits tenant customer credentials when the owner explicitly switches back', async () => {
  state.search = { owner_login: false }
  render(<SignIn />)
  const user = userEvent.setup()
  await user.type(
    screen.getByPlaceholderText('Enter your username or email'),
    'customer'
  )
  await user.type(
    screen.getByPlaceholderText('Enter password'),
    'customer-password'
  )
  await user.click(screen.getByRole('button', { name: 'Sign in' }))
  await waitFor(() =>
    expect(login).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        username: 'customer',
        password: 'customer-password',
        owner_login: false,
      })
    )
  )
  expect(state.handleLoginResult).toHaveBeenCalledExactlyOnceWith(
    undefined,
    undefined,
    false
  )
})

it('ignores owner mode on the platform Host', () => {
  state.search = { owner_login: true }
  state.tenantID = 0
  render(<SignIn />)
  expect(screen.getByRole('heading', { name: 'Sign in' })).toBeVisible()
  expect(screen.getByRole('link', { name: 'Sign up' })).toBeVisible()
  expect(
    screen.queryByRole('link', { name: 'Reseller management sign in' })
  ).not.toBeInTheDocument()
})

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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type React from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { QuickSetupDialog } from './quick-setup-dialog'

const mocks = vi.hoisted(() => ({
  createApiKey: vi.fn(),
  getApiKey: vi.fn(),
  getTokenModels: vi.fn(),
  fetchTokenKey: vi.fn(),
  getUserGroups: vi.fn(),
  useConsoleContract: vi.fn(),
  useSiteConfig: vi.fn(),
}))

vi.mock('../../keys/api', () => ({
  createApiKey: mocks.createApiKey,
  getApiKey: mocks.getApiKey,
  getTokenModels: mocks.getTokenModels,
  fetchTokenKey: mocks.fetchTokenKey,
}))

vi.mock('@/lib/api', () => ({ getUserGroups: mocks.getUserGroups }))
vi.mock('@/hooks/use-console-contract', () => ({
  useConsoleContract: mocks.useConsoleContract,
}))
vi.mock('@/features/site-config', () => ({
  useSiteConfig: mocks.useSiteConfig,
}))
vi.mock('@tanstack/react-router', () => ({
  Link: (props: React.ComponentProps<'a'>) => <a {...props} />,
}))

function renderDialog(preset: 'gpt' | 'claude-code' = 'gpt') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={queryClient}>
      <QuickSetupDialog preset={preset} onClose={onClose} />
    </QueryClientProvider>
  )
  return { onClose }
}

describe('QuickSetupDialog', () => {
  beforeEach(() => {
    useAuthStore.getState().auth.setUser({
      id: 7,
      username: 'guide-user',
      role: 1,
    })
    mocks.useConsoleContract.mockReturnValue({
      contract: { capabilities: ['token_create_result_id'] },
      loading: false,
      retry: vi.fn(),
    })
    mocks.useSiteConfig.mockReturnValue({ data: { tenant_id: 0 } })
    mocks.getUserGroups.mockResolvedValue({
      success: true,
      data: { 'GPT-稳定': {}, 'claudecode-满血企业': {} },
    })
    mocks.getTokenModels.mockResolvedValue({
      success: true,
      data: ['gpt-6-astra'],
    })
    mocks.getApiKey.mockResolvedValue({
      success: true,
      data: {
        id: 123,
        group: 'GPT-稳定',
        status: 1,
        expired_time: -1,
      },
    })
    mocks.fetchTokenKey.mockResolvedValue({
      success: true,
      data: { key: 'secret-key' },
    })
  })

  afterEach(() => {
    useAuthStore.getState().auth.reset()
    vi.restoreAllMocks()
  })

  test('uses the returned ID and opens CC Switch once after model lookup', async () => {
    const user = userEvent.setup()
    mocks.createApiKey.mockResolvedValue({ success: true, data: { id: 123 } })
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    renderDialog()

    await user.click(screen.getByRole('checkbox'))
    await user.click(
      screen.getByRole('button', { name: 'Create key and open CC Switch' })
    )

    await waitFor(() => expect(open).toHaveBeenCalledOnce())
    expect(mocks.getTokenModels).toHaveBeenCalledWith(123)
    expect(mocks.getApiKey).toHaveBeenCalledWith(123)
    expect(mocks.fetchTokenKey).toHaveBeenCalledWith(123)
    expect(String(open.mock.calls[0][0])).toContain('ccswitch://v1/import')
    expect(String(open.mock.calls[0][0])).toContain('apiKey=sk-secret-key')
  })

  test('does not retry creation when the successful response has no ID', async () => {
    const user = userEvent.setup()
    mocks.createApiKey.mockResolvedValue({ success: true })
    renderDialog()

    await user.click(screen.getByRole('checkbox'))
    await user.click(
      screen.getByRole('button', { name: 'Create key and open CC Switch' })
    )
    await waitFor(() =>
      expect(
        screen.getByText(
          'The key may have been created. Check API Keys before retrying.'
        )
      ).toBeVisible()
    )
    expect(mocks.createApiKey).toHaveBeenCalledOnce()
    expect(mocks.getTokenModels).not.toHaveBeenCalled()
    expect(mocks.fetchTokenKey).not.toHaveBeenCalled()
  })

  test('manual mode creates without the CC Switch confirmation and does not launch it', async () => {
    const user = userEvent.setup()
    mocks.createApiKey.mockResolvedValue({ success: true, data: { id: 123 } })
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    renderDialog()

    await user.click(
      screen.getByRole('button', { name: 'I want to configure manually' })
    )
    await user.click(screen.getByRole('button', { name: 'Create API key' }))

    await waitFor(() => expect(mocks.getTokenModels).toHaveBeenCalledWith(123))
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
    expect(open).not.toHaveBeenCalled()
    expect(mocks.fetchTokenKey).not.toHaveBeenCalled()
  })
})

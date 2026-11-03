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
import { act, render, screen, waitFor } from '@testing-library/react'
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
  copyToClipboard: vi.fn(),
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
vi.mock('@/lib/copy-to-clipboard', () => ({
  copyToClipboard: mocks.copyToClipboard,
}))
vi.mock('@tanstack/react-router', () => ({
  Link: (props: React.ComponentProps<'a'>) => <a {...props} />,
}))

function renderDialog(preset: 'gpt' | 'claude-code' = 'gpt') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const onClose = vi.fn()
  const tree = (value: 'gpt' | 'claude-code' | null) => (
    <QueryClientProvider client={queryClient}>
      <QuickSetupDialog preset={value} onClose={onClose} />
    </QueryClientProvider>
  )
  const { rerender } = render(tree(preset))
  return {
    onClose,
    queryClient,
    rerenderPreset: (value: 'gpt' | 'claude-code' | null) =>
      rerender(tree(value)),
  }
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
      data: {
        default: { display_name: 'GPT-稳定' },
        'claudecode-满血企业': {},
      },
    })
    mocks.getTokenModels.mockResolvedValue({
      success: true,
      data: ['gpt-6-astra'],
    })
    mocks.getApiKey.mockResolvedValue({
      success: true,
      data: {
        id: 123,
        group: 'default',
        status: 1,
        expired_time: -1,
      },
    })
    mocks.fetchTokenKey.mockResolvedValue({
      success: true,
      data: { key: 'secret-key' },
    })
    mocks.copyToClipboard.mockResolvedValue(true)
  })

  afterEach(() => {
    useAuthStore.getState().auth.reset()
    vi.restoreAllMocks()
  })

  test('offers official installation links for CC Switch and the selected client', () => {
    const { rerenderPreset } = renderDialog()

    expect(screen.getByText('Install CC Switch').closest('a')).toHaveAttribute(
      'href',
      'https://github.com/farion1231/cc-switch/releases/latest'
    )
    const codexLink = screen.getByText('Install Codex CLI').closest('a')
    expect(codexLink).toHaveAttribute(
      'href',
      'https://learn.chatgpt.com/docs/codex/cli'
    )
    expect(codexLink).toHaveAttribute('target', '_blank')
    expect(codexLink).toHaveAttribute('rel', 'noopener noreferrer')

    rerenderPreset('claude-code')
    expect(
      screen.getByText('Install Claude Code').closest('a')
    ).toHaveAttribute('href', 'https://code.claude.com/docs/en/setup')
    expect(screen.queryByText('Install Codex CLI')).not.toBeInTheDocument()
    expect(mocks.createApiKey).not.toHaveBeenCalled()
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
    expect(mocks.createApiKey).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'GPT-稳定', group: 'default' })
    )
    expect(mocks.getTokenModels).toHaveBeenCalledWith(123)
    expect(mocks.getApiKey).toHaveBeenCalledWith(123)
    expect(mocks.fetchTokenKey).toHaveBeenCalledWith(123)
    expect(String(open.mock.calls[0][0])).toContain('ccswitch://v1/import')
    expect(String(open.mock.calls[0][0])).toContain('apiKey=sk-secret-key')
  })

  test('does not guess a group when display names are duplicated', async () => {
    mocks.getUserGroups.mockResolvedValue({
      success: true,
      data: {
        default: { display_name: 'GPT-稳定' },
        secondary: { display_name: 'GPT-稳定' },
      },
    })
    renderDialog()

    expect(await screen.findByText('No groups available')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Create key and open CC Switch' })
    ).toBeDisabled()
    expect(mocks.createApiKey).not.toHaveBeenCalled()
  })

  test('does not use an auto group with the target display name', async () => {
    mocks.getUserGroups.mockResolvedValue({
      success: true,
      data: { auto: { display_name: 'GPT-稳定', is_auto: true } },
    })
    renderDialog()

    expect(await screen.findByText('No groups available')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Create key and open CC Switch' })
    ).toBeDisabled()
    expect(mocks.createApiKey).not.toHaveBeenCalled()
  })

  test('keeps the Claude group key when it matches its display name', async () => {
    const user = userEvent.setup()
    mocks.createApiKey.mockResolvedValue({ success: true, data: { id: 123 } })
    renderDialog('claude-code')

    await user.click(screen.getByRole('checkbox'))
    await user.click(
      screen.getByRole('button', { name: 'Create key and open CC Switch' })
    )

    await waitFor(() =>
      expect(mocks.createApiKey).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'claudecode-满血企业',
          group: 'claudecode-满血企业',
        })
      )
    )
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

  test('retries model lookup without recreating an API key after a network error', async () => {
    const user = userEvent.setup()
    mocks.createApiKey.mockResolvedValue({ success: true, data: { id: 123 } })
    mocks.getTokenModels
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce({ success: true, data: ['gpt-6-astra'] })
    renderDialog()

    await user.click(screen.getByRole('checkbox'))
    await user.click(
      screen.getByRole('button', { name: 'Create key and open CC Switch' })
    )

    expect(
      await screen.findByText('Model loading failed. Please try again.')
    ).toBeVisible()
    expect(mocks.createApiKey).toHaveBeenCalledOnce()
    await user.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => expect(mocks.getTokenModels).toHaveBeenCalledTimes(2))
    expect(mocks.getTokenModels).toHaveBeenNthCalledWith(2, 123)
    expect(mocks.createApiKey).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: 'Open CC Switch' })).toBeEnabled()
  })

  test('resumes with the created ID after closing during key creation', async () => {
    const user = userEvent.setup()
    let resolveCreate!: (result: {
      success: boolean
      data: { id: number }
    }) => void
    mocks.createApiKey.mockReturnValue(
      new Promise((resolve) => {
        resolveCreate = resolve
      })
    )
    const { rerenderPreset } = renderDialog()

    await user.click(screen.getByRole('checkbox'))
    await user.click(
      screen.getByRole('button', { name: 'Create key and open CC Switch' })
    )
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    rerenderPreset(null)
    rerenderPreset('gpt')
    expect(
      screen.getByText('The result is unknown. Check API Keys before retrying.')
    ).toBeVisible()

    await act(async () => resolveCreate({ success: true, data: { id: 123 } }))
    expect(screen.queryByText('Creating API key...')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
    expect(mocks.getTokenModels).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => expect(mocks.getTokenModels).toHaveBeenCalledWith(123))
    expect(mocks.createApiKey).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: 'Open CC Switch' })).toBeEnabled()
  })

  test('shows an unknown result after closing during a failed create request', async () => {
    const user = userEvent.setup()
    let rejectCreate!: (error: Error) => void
    mocks.createApiKey.mockReturnValue(
      new Promise((_, reject) => {
        rejectCreate = reject
      })
    )
    const { rerenderPreset } = renderDialog()

    await user.click(screen.getByRole('checkbox'))
    await user.click(
      screen.getByRole('button', { name: 'Create key and open CC Switch' })
    )
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    rerenderPreset(null)
    rerenderPreset('gpt')

    await act(async () => rejectCreate(new Error('network unavailable')))
    expect(screen.queryByText('Creating API key...')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open API Keys' })).toBeVisible()
    expect(mocks.createApiKey).toHaveBeenCalledOnce()
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

  test('manual setup can copy an active key without an available model', async () => {
    const user = userEvent.setup()
    mocks.createApiKey.mockResolvedValue({ success: true, data: { id: 123 } })
    mocks.getTokenModels.mockResolvedValue({ success: true, data: [] })
    const { queryClient } = renderDialog()

    await user.click(
      screen.getByRole('button', { name: 'I want to configure manually' })
    )
    await user.click(screen.getByRole('button', { name: 'Create API key' }))
    expect(
      await screen.findByText('No models are available for this API key yet.')
    ).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Copy' }))

    await waitFor(() =>
      expect(mocks.copyToClipboard).toHaveBeenCalledWith('sk-secret-key')
    )
    expect(mocks.getApiKey).toHaveBeenCalledWith(123)
    expect(mocks.fetchTokenKey).toHaveBeenCalledWith(123)
    expect(document.body.textContent).not.toContain('sk-secret-key')
    expect(JSON.stringify(queryClient.getQueryCache().getAll())).not.toContain(
      'sk-secret-key'
    )
  })
})

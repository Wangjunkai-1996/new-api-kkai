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
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { getSiteConfigQueryKey } from '@/features/site-config'
import { privateUserQueryKey } from '@/lib/private-query-cache'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { buildCCSwitchURL, CCSwitchDialog } from './cc-switch-dialog'

const apiMocks = vi.hoisted(() => ({
  getTokenModels: vi.fn(),
}))

vi.mock('../../api', () => ({
  getTokenModels: apiMocks.getTokenModels,
}))

const queryClients: QueryClient[] = []

function dialogTree(queryClient: QueryClient, tokenId: number, open = true) {
  return (
    <QueryClientProvider client={queryClient}>
      <CCSwitchDialog
        open={open}
        onOpenChange={vi.fn()}
        tokenKey='sk-test'
        tokenId={tokenId}
      />
    </QueryClientProvider>
  )
}

function renderDialog(tokenId = 1, siteName = 'KKAI', tenantId = 0) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClients.push(queryClient)
  queryClient.setQueryData(getSiteConfigQueryKey(), {
    tenant_id: tenantId,
    hostname: window.location.host,
    status: 'active',
    site_name: siteName,
  })

  return { queryClient, ...render(dialogTree(queryClient, tokenId)) }
}

describe('CC Switch dialog', () => {
  beforeEach(() => {
    useSystemConfigStore.getState().setConfig({ systemName: 'KKAI' })
    useAuthStore.getState().auth.setUser({
      id: 42,
      username: 'cc-switch-user',
      role: 1,
    })
  })

  afterEach(() => {
    useAuthStore.getState().auth.reset()
    for (const queryClient of queryClients) queryClient.clear()
    queryClients.length = 0
  })

  test.each([
    {
      app: 'Codex',
      model: 'gpt-6-astra',
      available: ['gpt-6-sol', 'gpt-6-astra', 'codex-auto-review'],
      endpoint: `${window.location.origin}/v1`,
    },
    {
      app: 'Claude',
      model: 'claude-opus-5-5',
      available: ['claude-fable-5-1', 'claude-opus-5-5'],
      endpoint: window.location.origin,
    },
  ])('imports the recommended $app configuration', async (entry) => {
    apiMocks.getTokenModels.mockResolvedValue({
      success: true,
      data: entry.available,
    })
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const user = userEvent.setup()
    renderDialog()

    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Primary Model' })
      ).toHaveValue(entry.model)
    )
    expect(screen.getByRole('radio', { name: entry.app })).toBeChecked()
    if (entry.app === 'Claude') {
      for (const name of ['Haiku Model', 'Sonnet Model', 'Opus Model']) {
        expect(screen.getByRole('combobox', { name })).toHaveValue('')
      }
    }
    await user.click(screen.getByRole('button', { name: 'Open CC Switch' }))

    const url = new URL(String(open.mock.calls[0][0]))
    expect(url.searchParams.get('app')).toBe(entry.app.toLowerCase())
    expect(url.searchParams.get('model')).toBe(entry.model)
    expect(url.searchParams.get('endpoint')).toBe(entry.endpoint)
    expect(url.searchParams.get('apiKey')).toBe('sk-test')
    expect(url.searchParams.has('haikuModel')).toBe(false)
    expect(url.searchParams.has('sonnetModel')).toBe(false)
    expect(url.searchParams.has('opusModel')).toBe(false)
  })

  test.each([
    { app: 'Codex', available: ['gpt-5.6-sol'], recommended: 'gpt-6-astra' },
    {
      app: 'Claude',
      available: ['claude-fable-5'],
      recommended: 'claude-opus-5-5',
    },
  ])(
    'selects $app but leaves an unavailable recommendation empty',
    async (entry) => {
      apiMocks.getTokenModels.mockResolvedValue({
        success: true,
        data: entry.available,
      })
      renderDialog()

      expect(
        await screen.findByText(
          `The recommended model ${entry.recommended} is unavailable for this API key. Please select another model.`
        )
      ).toBeVisible()
      expect(screen.getByRole('radio', { name: entry.app })).toBeChecked()
      expect(
        screen.getByRole('combobox', { name: 'Primary Model' })
      ).toHaveValue('')
      expect(
        screen.getByRole('button', { name: 'Open CC Switch' })
      ).toBeDisabled()
    }
  )

  test('keeps mixed families manual and clears Claude fields when switching applications', async () => {
    apiMocks.getTokenModels.mockResolvedValue({
      success: true,
      data: ['claude-opus-5-5', 'gpt-6-astra'],
    })
    const user = userEvent.setup()
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    renderDialog()
    await screen.findByText('Please select a primary model')
    expect(screen.getByRole('combobox', { name: 'Primary Model' })).toHaveValue(
      ''
    )
    await user.click(screen.getByText('Claude', { selector: 'label' }))
    expect(screen.getByRole('combobox', { name: 'Primary Model' })).toHaveValue(
      'claude-opus-5-5'
    )
    await user.click(screen.getByRole('combobox', { name: 'Haiku Model' }))
    await user.click(
      await screen.findByRole('option', { name: 'claude-opus-5-5' })
    )
    expect(screen.getByRole('combobox', { name: 'Primary Model' })).toHaveValue(
      'claude-opus-5-5'
    )
    await user.clear(screen.getByRole('textbox', { name: 'Name' }))
    await user.type(
      screen.getByRole('textbox', { name: 'Name' }),
      'My provider'
    )

    await user.click(screen.getByRole('radio', { name: 'Codex' }))
    expect(screen.getByRole('combobox', { name: 'Primary Model' })).toHaveValue(
      'gpt-6-astra'
    )
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue(
      'My provider'
    )
    await user.click(screen.getByRole('button', { name: 'Open CC Switch' }))
    expect(
      new URL(String(open.mock.calls[0][0])).searchParams.has('haikuModel')
    ).toBe(false)

    await user.click(screen.getByRole('radio', { name: 'Claude' }))
    expect(screen.getByRole('combobox', { name: 'Primary Model' })).toHaveValue(
      'claude-opus-5-5'
    )
    expect(screen.getByRole('combobox', { name: 'Haiku Model' })).toHaveValue(
      ''
    )
  })

  test('preserves manual choices on refresh and blocks models removed from the token', async () => {
    apiMocks.getTokenModels.mockResolvedValue({
      success: true,
      data: ['gpt-6-astra', 'gpt-6-sol'],
    })
    const user = userEvent.setup()
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const { queryClient } = renderDialog()
    const queryKey = privateUserQueryKey(42, 'api-keys', 'cc-switch-models', 1)
    await waitFor(() =>
      expect(screen.getByRole('radio', { name: 'Codex' })).toBeChecked()
    )
    await user.click(screen.getByRole('combobox', { name: 'Primary Model' }))
    await user.click(await screen.findByRole('option', { name: 'gpt-6-sol' }))

    apiMocks.getTokenModels.mockResolvedValue({
      success: true,
      data: ['gpt-6-astra', 'gpt-6-sol', 'claude-opus-5-5'],
    })
    await act(async () => {
      await queryClient.refetchQueries({ queryKey })
    })
    expect(screen.getByRole('radio', { name: 'Codex' })).toBeChecked()
    expect(screen.getByRole('combobox', { name: 'Primary Model' })).toHaveValue(
      'gpt-6-sol'
    )

    apiMocks.getTokenModels.mockResolvedValue({
      success: true,
      data: ['gpt-6-astra'],
    })
    await act(async () => {
      await queryClient.refetchQueries({ queryKey })
    })
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'A selected model no longer exists.'
    )
    expect(
      screen.getByRole('button', { name: 'Open CC Switch' })
    ).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Open CC Switch' }))
    expect(open).not.toHaveBeenCalled()
  })

  test.each([
    { app: 'Gemini', keyboard: false },
    { app: 'Claude', keyboard: false },
    { app: 'Claude', keyboard: true },
  ])(
    'preserves $app selected before loading completes (keyboard=$keyboard)',
    async ({ app, keyboard }) => {
      let resolveModels!: (value: { success: boolean; data: string[] }) => void
      const response = new Promise<{ success: boolean; data: string[] }>(
        (resolve) => {
          resolveModels = resolve
        }
      )
      apiMocks.getTokenModels.mockReturnValue(response)
      const user = userEvent.setup()
      renderDialog()

      const radio = screen.getByRole('radio', { name: app })
      if (keyboard) {
        radio.focus()
        await user.keyboard(' ')
      } else {
        await user.click(radio)
      }
      await act(async () => {
        resolveModels({ success: true, data: ['gpt-6-astra'] })
      })
      await waitFor(() =>
        expect(screen.queryByText('Loading...')).not.toBeInTheDocument()
      )
      expect(screen.getByRole('radio', { name: app })).toBeChecked()
      expect(
        screen.getByRole('combobox', { name: 'Primary Model' })
      ).toHaveValue('')
      expect(
        screen.getByRole('button', { name: 'Open CC Switch' })
      ).toBeDisabled()
    }
  )

  test('isolates late token responses and resets defaults when reopened', async () => {
    let resolveModels!: (value: { success: boolean; data: string[] }) => void
    const response = new Promise<{ success: boolean; data: string[] }>(
      (resolve) => {
        resolveModels = resolve
      }
    )
    apiMocks.getTokenModels
      .mockReturnValueOnce(response)
      .mockResolvedValue({ success: true, data: ['claude-opus-5-5'] })
    const user = userEvent.setup()
    const rendered = renderDialog(1)
    rendered.rerender(dialogTree(rendered.queryClient, 2))
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Primary Model' })
      ).toHaveValue('claude-opus-5-5')
    )
    await act(async () => {
      resolveModels({ success: true, data: ['gpt-6-astra'] })
    })
    expect(screen.getByRole('radio', { name: 'Claude' })).toBeChecked()
    expect(screen.getByRole('combobox', { name: 'Primary Model' })).toHaveValue(
      'claude-opus-5-5'
    )

    await user.click(screen.getByRole('radio', { name: 'Codex' }))
    rendered.rerender(dialogTree(rendered.queryClient, 2, false))
    rendered.rerender(dialogTree(rendered.queryClient, 2, true))
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Primary Model' })
      ).toHaveValue('claude-opus-5-5')
    )
    expect(screen.getByRole('radio', { name: 'Claude' })).toBeChecked()
  })

  test('keeps each token model list isolated in the query cache', async () => {
    apiMocks.getTokenModels.mockImplementation(async (tokenId: number) => ({
      success: true,
      data: tokenId === 1 ? ['default-model'] : ['vip-model'],
    }))
    const user = userEvent.setup()

    const rendered = renderDialog(1)

    await waitFor(() => expect(apiMocks.getTokenModels).toHaveBeenCalledWith(1))
    await user.click(screen.getAllByRole('combobox')[1])

    const defaultOption = await screen.findByRole('option', {
      name: 'default-model',
    })
    await user.click(defaultOption)
    expect(screen.getAllByRole('combobox')[1]).toHaveValue('default-model')

    rendered.rerender(dialogTree(rendered.queryClient, 2))

    await waitFor(() => expect(apiMocks.getTokenModels).toHaveBeenCalledWith(2))
    await waitFor(() =>
      expect(screen.getAllByRole('combobox')[1]).toHaveValue('')
    )
    await user.click(screen.getAllByRole('combobox')[1])
    expect(
      await screen.findByRole('option', { name: 'vip-model' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('option', { name: 'default-model' })
    ).not.toBeInTheDocument()
  })

  test('refetches the token model list whenever the dialog reopens', async () => {
    apiMocks.getTokenModels
      .mockResolvedValueOnce({ success: true, data: ['old-model'] })
      .mockResolvedValueOnce({ success: true, data: ['updated-model'] })
    const user = userEvent.setup()

    const rendered = renderDialog()
    await waitFor(() =>
      expect(apiMocks.getTokenModels).toHaveBeenCalledTimes(1)
    )

    rendered.rerender(dialogTree(rendered.queryClient, 1, false))
    rendered.rerender(dialogTree(rendered.queryClient, 1, true))

    await waitFor(() =>
      expect(apiMocks.getTokenModels).toHaveBeenCalledTimes(2)
    )
    await user.click(screen.getAllByRole('combobox')[1])
    expect(
      await screen.findByRole('option', { name: 'updated-model' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('option', { name: 'old-model' })
    ).not.toBeInTheDocument()
  })

  test('waits for a fresh model list before inferring defaults on reopen', async () => {
    apiMocks.getTokenModels.mockResolvedValueOnce({
      success: true,
      data: ['gpt-6-astra'],
    })
    const rendered = renderDialog()
    await waitFor(() =>
      expect(screen.getByRole('radio', { name: 'Codex' })).toBeChecked()
    )
    let resolveModels!: (value: { success: boolean; data: string[] }) => void
    apiMocks.getTokenModels.mockReturnValueOnce(
      new Promise<{ success: boolean; data: string[] }>((resolve) => {
        resolveModels = resolve
      })
    )
    rendered.rerender(dialogTree(rendered.queryClient, 1, false))
    rendered.rerender(dialogTree(rendered.queryClient, 1, true))

    expect(screen.getByText('Loading...')).toBeVisible()
    expect(screen.getByRole('radio', { name: 'Codex' })).not.toBeChecked()
    expect(screen.getByRole('combobox', { name: 'Primary Model' })).toHaveValue(
      ''
    )
    expect(
      screen.getByRole('button', { name: 'Open CC Switch' })
    ).toBeDisabled()
    await act(async () => {
      resolveModels({ success: true, data: ['claude-opus-5-5'] })
    })
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Primary Model' })
      ).toHaveValue('claude-opus-5-5')
    )
    expect(screen.getByRole('radio', { name: 'Claude' })).toBeChecked()
  })

  test('hides cached models when a reopen refresh fails', async () => {
    apiMocks.getTokenModels
      .mockResolvedValueOnce({ success: true, data: ['cached-model'] })
      .mockResolvedValueOnce({
        success: false,
        message: 'Model refresh failed',
      })
    const user = userEvent.setup()

    const rendered = renderDialog()
    await user.click(screen.getAllByRole('combobox')[1])
    expect(
      await screen.findByRole('option', { name: 'cached-model' })
    ).toBeInTheDocument()

    rendered.rerender(dialogTree(rendered.queryClient, 1, false))
    rendered.rerender(dialogTree(rendered.queryClient, 1, true))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Model refresh failed'
    )
    await user.click(screen.getAllByRole('combobox')[1])
    expect(
      screen.queryByRole('option', { name: 'cached-model' })
    ).not.toBeInTheDocument()
    await user.keyboard('{Escape}')
    expect(
      screen.getByRole('button', { name: 'Open CC Switch' })
    ).toBeDisabled()
  })

  test('shows token model lookup failures and prevents import', async () => {
    apiMocks.getTokenModels.mockResolvedValue({
      success: false,
      message: 'Model lookup failed',
    })

    renderDialog()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Model lookup failed'
    )
    expect(
      screen.getByRole('button', { name: 'Open CC Switch' })
    ).toBeDisabled()
  })

  test('uses KKAI as the default name for every application', async () => {
    apiMocks.getTokenModels.mockResolvedValue({
      success: true,
      data: [],
    })
    const user = userEvent.setup()

    renderDialog()

    expect(screen.getByDisplayValue('KKAI')).toBeInTheDocument()

    await user.click(screen.getByRole('radio', { name: 'Codex' }))
    expect(screen.getByDisplayValue('KKAI')).toBeInTheDocument()

    await user.click(screen.getByRole('radio', { name: 'Gemini' }))
    expect(screen.getByDisplayValue('KKAI')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Open CC Switch' })
    ).toBeDisabled()
  })

  test('imports the current reseller site name for every application', async () => {
    apiMocks.getTokenModels.mockResolvedValue({
      success: true,
      data: ['tenant-model'],
    })
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const user = userEvent.setup()
    renderDialog(1, 'KKTest', 7)

    for (const app of ['Claude', 'Gemini', 'Codex']) {
      await user.click(screen.getByRole('radio', { name: app }))
      expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue(
        'KKTest'
      )
    }
    await user.click(screen.getByRole('combobox', { name: 'Primary Model' }))
    await user.click(
      await screen.findByRole('option', { name: 'tenant-model' })
    )
    await user.click(screen.getByRole('button', { name: 'Open CC Switch' }))

    expect(open).toHaveBeenCalledTimes(1)
    const importedURL = new URL(String(open.mock.calls[0][0]))
    expect(importedURL.searchParams.get('name')).toBe('KKTest')
    expect(importedURL.searchParams.get('app')).toBe('codex')
    expect(importedURL.searchParams.get('endpoint')).toBe(
      `${window.location.origin}/v1`
    )
  })

  test('refreshes the default site name without replacing a user-entered name', async () => {
    apiMocks.getTokenModels.mockResolvedValue({ success: true, data: [] })
    const user = userEvent.setup()
    const rendered = renderDialog(1, '', 7)
    const input = screen.getByRole('textbox', { name: 'Name' })
    const key = getSiteConfigQueryKey()
    await act(async () => {
      rendered.queryClient.setQueryData(key, {
        tenant_id: 7,
        site_name: 'KKTest',
      })
    })
    await waitFor(() => expect(input).toHaveValue('KKTest'))

    await user.clear(input)
    await user.type(input, 'Development')
    await act(async () => {
      rendered.queryClient.setQueryData(key, {
        tenant_id: 7,
        site_name: 'KKTest renamed',
      })
    })
    await waitFor(() =>
      expect(input).toHaveAttribute('placeholder', 'KKTest renamed')
    )
    expect(input).toHaveValue('Development')

    rendered.rerender(dialogTree(rendered.queryClient, 1, false))
    rendered.rerender(dialogTree(rendered.queryClient, 1, true))
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue(
      'KKTest renamed'
    )
  })

  test('includes the balance query configuration without a duplicate usage key', () => {
    const importedURL = new URL(
      buildCCSwitchURL('claude', 'KKAI', { model: 'default-model' }, 'sk-test')
    )
    const params = importedURL.searchParams
    expect(params.get('usageEnabled')).toBe('true')
    expect(params.get('usageBaseUrl')).toBe(window.location.origin)
    expect(params.get('usageAutoInterval')).toBe('30')
    expect(params.get('usageApiKey')).toBeNull()
    expect(atob(params.get('usageScript') ?? '')).toContain('/api/usage/token/')
  })
})

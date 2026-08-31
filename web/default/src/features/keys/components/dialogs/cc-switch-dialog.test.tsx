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
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { CCSwitchDialog } from './cc-switch-dialog'

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

function renderDialog(tokenId = 1) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClients.push(queryClient)

  return { queryClient, ...render(dialogTree(queryClient, tokenId)) }
}

describe('CC Switch dialog', () => {
  beforeEach(() => {
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
})

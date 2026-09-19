/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

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

import type { ApiKey } from '../types'
import { ApiKeyGroupCell } from './api-key-group-cell'

const apiMocks = vi.hoisted(() => ({
  getUserGroups: vi.fn(),
  updateApiKeyGroup: vi.fn(),
}))

const contextMocks = vi.hoisted(() => ({
  setCurrentRow: vi.fn(),
  setOpen: vi.fn(),
  triggerRefresh: vi.fn(),
}))

const toastMocks = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
}))

const contractState = vi.hoisted(() => ({ supportsInlineGroup: true }))

vi.mock('@/lib/api', () => ({
  getUserGroups: apiMocks.getUserGroups,
}))

vi.mock('../api', () => ({
  updateApiKeyGroup: apiMocks.updateApiKeyGroup,
}))

vi.mock('./api-keys-provider', () => ({
  useApiKeys: () => contextMocks,
}))

vi.mock('sonner', () => ({ toast: toastMocks }))

vi.mock('@/hooks/use-console-contract', () => ({
  useConsoleContract: () => ({
    contract: {
      capabilities: contractState.supportsInlineGroup
        ? ['token_group_inline']
        : [],
    },
  }),
}))

const userGroups = {
  default: {
    display_name: 'Default',
    desc: 'Balanced general purpose access',
    ratio: 1,
  },
  premium: {
    display_name: 'Premium',
    desc: 'Faster models for priority requests',
    ratio: 2,
  },
  auto2: {
    display_name: 'Balanced route',
    desc: 'Automatically selects an available group',
    ratio: 'auto',
    is_auto: true,
  },
}

const apiKey: ApiKey = {
  id: 17,
  name: 'Primary key',
  key: 'sk-****',
  status: 1,
  remain_quota: 100,
  used_quota: 0,
  unlimited_quota: false,
  expired_time: -1,
  created_time: 1,
  accessed_time: 1,
  group: 'default',
  cross_group_retry: false,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: '',
}

const queryClients: QueryClient[] = []

function renderCell(row: ApiKey = apiKey) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClients.push(queryClient)

  return render(
    <QueryClientProvider client={queryClient}>
      <ApiKeyGroupCell apiKey={row} />
    </QueryClientProvider>
  )
}

async function openGroupMenu(user: ReturnType<typeof userEvent.setup>) {
  const trigger = screen.getByRole('combobox', { name: 'Group: Primary key' })
  await waitFor(() => expect(trigger).not.toBeDisabled())
  await user.click(trigger)
  return trigger
}

describe('ApiKeyGroupCell', () => {
  beforeEach(() => {
    apiMocks.getUserGroups.mockResolvedValue({
      success: true,
      data: userGroups,
    })
    apiMocks.updateApiKeyGroup.mockResolvedValue({
      success: true,
      data: apiKey,
    })
  })

  afterEach(() => {
    vi.clearAllMocks()
    contractState.supportsInlineGroup = true
    for (const queryClient of queryClients) queryClient.clear()
    queryClients.length = 0
  })

  test('does not issue a request when the current group is selected again', async () => {
    const user = userEvent.setup()
    renderCell()

    await openGroupMenu(user)
    await user.click(screen.getByRole('option', { name: /Default/ }))

    expect(apiMocks.updateApiKeyGroup).not.toHaveBeenCalled()
    expect(contextMocks.triggerRefresh).not.toHaveBeenCalled()
  })

  test('sends only the selected canonical group key through the PATCH mutation', async () => {
    const user = userEvent.setup()
    renderCell()

    await openGroupMenu(user)
    await user.click(screen.getByRole('option', { name: /Premium/ }))

    await waitFor(() =>
      expect(apiMocks.updateApiKeyGroup).toHaveBeenCalledWith(17, 'premium')
    )
    await waitFor(() =>
      expect(contextMocks.triggerRefresh).toHaveBeenCalledTimes(1)
    )
    expect(apiMocks.updateApiKeyGroup).toHaveBeenCalledTimes(1)
  })

  test('locks the trigger while changing groups and recovers after a failed response', async () => {
    const user = userEvent.setup()
    let resolveUpdate: ((value: unknown) => void) | undefined
    apiMocks.updateApiKeyGroup.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveUpdate = resolve
        })
    )
    renderCell()

    const trigger = await openGroupMenu(user)
    await user.click(screen.getByRole('option', { name: /Premium/ }))

    await waitFor(() => expect(trigger).toBeDisabled())
    resolveUpdate?.({
      success: false,
      message: 'Group update rejected',
    })
    await waitFor(() => expect(trigger).not.toBeDisabled())
    expect(contextMocks.triggerRefresh).not.toHaveBeenCalled()
    expect(toastMocks.error).toHaveBeenCalledWith('Group update rejected')
    expect(trigger).toHaveTextContent('Default')
  })

  test('opens the full editor when the backend lacks inline group support', async () => {
    contractState.supportsInlineGroup = false
    const user = userEvent.setup()
    renderCell()

    const fallback = await screen.findByRole('button', {
      name: 'Group: Primary key',
    })
    await user.click(fallback)

    expect(contextMocks.setCurrentRow).toHaveBeenCalledWith(apiKey)
    expect(contextMocks.setOpen).toHaveBeenCalledWith('update')
    expect(apiMocks.updateApiKeyGroup).not.toHaveBeenCalled()
  })
})

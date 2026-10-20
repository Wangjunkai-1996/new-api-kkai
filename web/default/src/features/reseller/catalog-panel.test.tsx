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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'

import type { GroupPrice } from './api'
import { CatalogPanel } from './catalog-panel'

vi.mock('@/lib/handle-server-error', () => ({ handleServerError: vi.fn() }))
vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))
const clients: QueryClient[] = []
afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

const defaultPrice: GroupPrice = {
  id: 0,
  group: 'default',
  sales_multiplier: '1',
  platform_multiplier: '1',
  minimum_multiplier: '1',
  version: 0,
  inherited: true,
}
const proPrice: GroupPrice = {
  id: 4,
  group: 'pro',
  sales_multiplier: '2',
  platform_multiplier: '1.5',
  minimum_multiplier: '1.5',
  version: 4,
  inherited: false,
}

function renderCatalog() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <CatalogPanel />
    </QueryClientProvider>
  )
}

test.each(['1', '0'])(
  'group ratios inherit platform ratio %s without requiring a write',
  async (platformRatio) => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: [
          {
            ...defaultPrice,
            sales_multiplier: platformRatio,
            platform_multiplier: platformRatio,
            minimum_multiplier: platformRatio,
          },
          {
            ...proPrice,
            id: 0,
            sales_multiplier: '1.5',
            version: 0,
            inherited: true,
          },
        ],
      },
    })
    const patch = vi.spyOn(api, 'patch')
    renderCatalog()
    expect(await screen.findByLabelText('Group ratio for default')).toHaveValue(
      platformRatio
    )
    expect(screen.getByLabelText('Group ratio for default')).toHaveAttribute(
      'aria-invalid',
      'false'
    )
    expect(screen.getByLabelText('Group ratio for pro')).toHaveValue('1.5')
    expect(screen.getAllByText('Inherited')).toHaveLength(2)
    for (const button of screen.getAllByRole('button', { name: /^Save$/ })) {
      expect(button).toBeDisabled()
    }
    for (const button of screen.getAllByRole('button', {
      name: 'Reset to default',
    })) {
      expect(button).toBeDisabled()
    }
    expect(get).toHaveBeenCalledExactlyOnceWith('/api/reseller/prices', {
      params: { scope: 'group' },
    })
    expect(patch).not.toHaveBeenCalled()
  }
)

test('saving one group refreshes its version while preserving another group draft', async () => {
  const savedDefault: GroupPrice = {
    ...defaultPrice,
    id: 1,
    sales_multiplier: '1.5',
    version: 1,
    inherited: false,
  }
  const savedAgain = {
    ...savedDefault,
    id: 2,
    sales_multiplier: '1.7',
    version: 2,
  }
  vi.spyOn(api, 'get')
    .mockResolvedValueOnce({
      data: { success: true, data: [defaultPrice, proPrice] },
    })
    .mockResolvedValueOnce({
      data: { success: true, data: [savedDefault, proPrice] },
    })
    .mockResolvedValueOnce({
      data: { success: true, data: [savedAgain, proPrice] },
    })
    .mockResolvedValue({
      data: {
        success: true,
        data: [
          savedAgain,
          { ...proPrice, id: 5, sales_multiplier: '2.4', version: 5 },
        ],
      },
    })
  const patch = vi.spyOn(api, 'patch').mockResolvedValue({
    data: { success: true, data: [] },
  })
  renderCatalog()
  await screen.findByLabelText('Group ratio for default')
  const user = userEvent.setup()
  await user.clear(screen.getByLabelText('Group ratio for pro'))
  await user.type(screen.getByLabelText('Group ratio for pro'), '2.4')
  await user.clear(screen.getByLabelText('Group ratio for default'))
  await user.type(screen.getByLabelText('Group ratio for default'), '1.5')
  await user.click(
    within(screen.getByRole('row', { name: /^default / })).getByRole('button', {
      name: /^Save$/,
    })
  )
  await waitFor(() =>
    expect(patch).toHaveBeenNthCalledWith(1, '/api/reseller/prices', {
      prices: [
        { sales_group: 'default', sale_multiplier: '1.5', expected_version: 0 },
      ],
    })
  )
  await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
  expect(screen.getByLabelText('Group ratio for pro')).toHaveValue('2.4')

  await user.clear(screen.getByLabelText('Group ratio for default'))
  await user.type(screen.getByLabelText('Group ratio for default'), '1.7')
  await user.click(
    within(screen.getByRole('row', { name: /^default / })).getByRole('button', {
      name: /^Save$/,
    })
  )
  await waitFor(() =>
    expect(patch).toHaveBeenNthCalledWith(2, '/api/reseller/prices', {
      prices: [
        { sales_group: 'default', sale_multiplier: '1.7', expected_version: 1 },
      ],
    })
  )
  await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(2))
  expect(screen.getByLabelText('Group ratio for pro')).toHaveValue('2.4')
  await user.click(
    within(screen.getByRole('row', { name: /^pro / })).getByRole('button', {
      name: /^Save$/,
    })
  )
  await waitFor(() =>
    expect(patch).toHaveBeenNthCalledWith(3, '/api/reseller/prices', {
      prices: [
        { sales_group: 'pro', sale_multiplier: '2.4', expected_version: 4 },
      ],
    })
  )
})

test('resetting a group restores inheritance with the observed version', async () => {
  const custom = {
    ...defaultPrice,
    id: 7,
    sales_multiplier: '1.6',
    version: 7,
    inherited: false,
  }
  const inherited = { ...defaultPrice, id: 8, version: 8 }
  vi.spyOn(api, 'get')
    .mockResolvedValueOnce({ data: { success: true, data: [custom] } })
    .mockResolvedValue({ data: { success: true, data: [inherited] } })
  const patch = vi.spyOn(api, 'patch').mockResolvedValue({
    data: { success: true, data: [] },
  })
  renderCatalog()
  await screen.findByDisplayValue('1.6')
  await userEvent
    .setup()
    .click(screen.getByRole('button', { name: 'Reset to default' }))
  await waitFor(() =>
    expect(patch).toHaveBeenCalledExactlyOnceWith('/api/reseller/prices', {
      prices: [
        { sales_group: 'default', sale_multiplier: '', expected_version: 7 },
      ],
    })
  )
  expect(await screen.findByText('Inherited')).toBeVisible()
  expect(screen.getByLabelText('Group ratio for default')).toHaveValue('1')
  expect(screen.getByRole('button', { name: /^Save$/ })).toBeDisabled()
  expect(
    screen.getByRole('button', { name: 'Reset to default' })
  ).toBeDisabled()
})

test('a price conflict refreshes the group version before the next save', async () => {
  const latestDefault: GroupPrice = {
    ...defaultPrice,
    id: 1,
    sales_multiplier: '1.7',
    version: 1,
    inherited: false,
  }
  const savedDefault = {
    ...latestDefault,
    id: 2,
    sales_multiplier: '1.8',
    version: 2,
  }
  const conflict = {
    response: {
      status: 409,
      data: {
        success: false,
        code: 'TENANT_PRICE_VERSION_CONFLICT',
        message: 'price configuration changed; reload and retry',
      },
    },
  }
  vi.spyOn(api, 'get')
    .mockResolvedValueOnce({
      data: { success: true, data: [defaultPrice, proPrice] },
    })
    .mockResolvedValueOnce({
      data: { success: true, data: [latestDefault, proPrice] },
    })
    .mockResolvedValue({
      data: { success: true, data: [savedDefault, proPrice] },
    })
  const patch = vi
    .spyOn(api, 'patch')
    .mockRejectedValueOnce(conflict)
    .mockResolvedValue({
      data: { success: true, data: [savedDefault, proPrice] },
    })
  renderCatalog()
  await screen.findByLabelText('Group ratio for default')
  const user = userEvent.setup()
  await user.clear(screen.getByLabelText('Group ratio for pro'))
  await user.type(screen.getByLabelText('Group ratio for pro'), '2.4')
  await user.clear(screen.getByLabelText('Group ratio for default'))
  await user.type(screen.getByLabelText('Group ratio for default'), '1.5')
  await user.click(
    within(screen.getByRole('row', { name: /^default / })).getByRole('button', {
      name: /^Save$/,
    })
  )
  await waitFor(() =>
    expect(patch).toHaveBeenNthCalledWith(1, '/api/reseller/prices', {
      prices: [
        { sales_group: 'default', sale_multiplier: '1.5', expected_version: 0 },
      ],
    })
  )
  await waitFor(() =>
    expect(screen.getByLabelText('Group ratio for default')).toHaveValue('1.7')
  )
  expect(handleServerError).toHaveBeenCalledWith(conflict)
  expect(toast.success).not.toHaveBeenCalled()
  expect(screen.getByLabelText('Group ratio for pro')).toHaveValue('2.4')
  await user.clear(screen.getByLabelText('Group ratio for default'))
  await user.type(screen.getByLabelText('Group ratio for default'), '1.8')
  await user.click(
    within(screen.getByRole('row', { name: /^default / })).getByRole('button', {
      name: /^Save$/,
    })
  )
  await waitFor(() =>
    expect(patch).toHaveBeenNthCalledWith(2, '/api/reseller/prices', {
      prices: [
        { sales_group: 'default', sale_multiplier: '1.8', expected_version: 1 },
      ],
    })
  )
  await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
  expect(screen.getByLabelText('Group ratio for default')).toHaveValue('1.8')
  expect(screen.getByLabelText('Group ratio for pro')).toHaveValue('2.4')
})

test('invalid ratios cannot be saved and a failed save preserves the valid draft', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: [defaultPrice] },
  })
  const error = new Error('Price version conflict')
  const patch = vi.spyOn(api, 'patch').mockRejectedValue(error)
  renderCatalog()
  const ratio = await screen.findByLabelText('Group ratio for default')
  const save = screen.getByRole('button', { name: /^Save$/ })
  const user = userEvent.setup()
  await user.clear(ratio)
  expect(save).toBeDisabled()
  for (const invalid of ['0', '0.5', '1000001', 'Infinity']) {
    await user.type(ratio, invalid)
    expect(ratio).toHaveAttribute('aria-invalid', 'true')
    expect(save).toBeDisabled()
    await user.clear(ratio)
  }
  expect(patch).not.toHaveBeenCalled()
  await user.type(ratio, '1.5')
  await user.click(save)
  await waitFor(() => expect(handleServerError).toHaveBeenCalledWith(error))
  expect(ratio).toHaveValue('1.5')
  expect(save).toBeEnabled()
  expect(toast.success).not.toHaveBeenCalled()
})

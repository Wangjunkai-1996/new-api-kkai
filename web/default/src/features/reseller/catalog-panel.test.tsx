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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import * as resellerApi from './api'
import { CatalogPanel } from './catalog-panel'

vi.mock('@/lib/handle-server-error', () => ({ handleServerError: vi.fn() }))
vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))
let client: QueryClient
afterEach(() => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
})

test('price saves carry each observed version and refresh it before the next edit', async () => {
  vi.spyOn(resellerApi, 'getResellerCatalog').mockResolvedValue([
    {
      sales_group: 'default',
      model: 'existing',
      status: 'enabled',
      is_default: true,
      sort_order: 0,
    },
    {
      sales_group: 'default',
      model: 'new',
      status: 'enabled',
      is_default: false,
      sort_order: 1,
    },
  ])
  vi.spyOn(resellerApi, 'getResellerSupply').mockResolvedValue(
    ['existing', 'new'].map((model) => ({
      sales_group: 'default',
      model,
      supplier_price_minor: 100,
      minimum_multiplier: '1.1',
    }))
  )
  const price = {
    id: 1,
    model: 'existing',
    group: 'default',
    supplier_price_minor: 100,
    sales_multiplier: '2',
    version: 7,
  }
  vi.spyOn(resellerApi, 'getResellerPrices')
    .mockResolvedValueOnce([price])
    .mockResolvedValue([
      { ...price, sales_multiplier: '2.2', version: 8 },
      { ...price, id: 2, model: 'new', sales_multiplier: '2.2', version: 1 },
    ])
  const patch = vi
    .spyOn(api, 'patch')
    .mockResolvedValue({ data: { success: true, data: [] } })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <CatalogPanel />
    </QueryClientProvider>
  )
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole('button', { name: 'Save Sales Multiplier' })
  )
  await waitFor(() =>
    expect(patch).toHaveBeenNthCalledWith(1, '/api/reseller/prices', {
      prices: [
        {
          sales_group: 'default',
          model: 'existing',
          sale_multiplier: '2',
          expected_version: 7,
        },
        {
          sales_group: 'default',
          model: 'new',
          sale_multiplier: '2',
          expected_version: 0,
        },
      ],
    })
  )
  await screen.findByDisplayValue('2.2')
  await user.click(
    screen.getByRole('button', { name: 'Save Sales Multiplier' })
  )
  await waitFor(() =>
    expect(patch).toHaveBeenNthCalledWith(2, '/api/reseller/prices', {
      prices: [
        {
          sales_group: 'default',
          model: 'existing',
          sale_multiplier: '2.2',
          expected_version: 8,
        },
        {
          sales_group: 'default',
          model: 'new',
          sale_multiplier: '2.2',
          expected_version: 1,
        },
      ],
    })
  )
})

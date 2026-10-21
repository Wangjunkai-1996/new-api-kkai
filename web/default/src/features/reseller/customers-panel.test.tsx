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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { CustomersPanel } from './customers-panel'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { language: 'en' } }),
}))

const clients: QueryClient[] = []
let originalConfig = useSystemConfigStore.getState().config
beforeEach(() => {
  originalConfig = useSystemConfigStore.getState().config
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG },
  })
})
afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
  useSystemConfigStore.getState().setConfig(originalConfig)
})

test('loads the next customer page using the server total', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (_url, config) => {
    const page = (config?.params as { p: number } | undefined)?.p
    return {
      data: {
        success: true,
        total: 21,
        data: [
          {
            id: page,
            username: page === 1 ? 'first-customer' : 'last-customer',
            display_name: '',
            status: 1,
            created_at: 1,
            quota: 0,
            used_quota: 0,
            total_credited_quota: 0,
            total_refunded_quota: 0,
          },
        ],
      },
    }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <CustomersPanel />
    </QueryClientProvider>
  )
  expect(await screen.findByText('first-customer')).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Next page' }))
  expect(await screen.findByText('last-customer')).toBeVisible()
  expect(get).toHaveBeenLastCalledWith('/api/reseller/customers', {
    params: { p: 2, page_size: 20 },
  })
  expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Previous page' })).toBeEnabled()
})

test('shows customer quota balances and credited quota separately from refunds and cash', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      total: 2,
      data: [
        {
          id: 1,
          username: 'funded-customer',
          display_name: '',
          status: 1,
          created_at: 1,
          quota: 1000000,
          used_quota: 500000,
          total_credited_quota: 2000000,
          total_refunded_quota: 250000,
          paid_minor: 999999,
        },
        {
          id: 2,
          username: 'new-customer',
          display_name: '',
          status: 1,
          created_at: 1,
          quota: 0,
          used_quota: 0,
          total_credited_quota: 0,
          total_refunded_quota: 0,
        },
      ],
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <CustomersPanel />
    </QueryClientProvider>
  )

  const fundedRow = await screen.findByRole('row', { name: /funded-customer/ })
  expect(
    screen.getByRole('columnheader', { name: 'Available Balance' })
  ).toBeVisible()
  expect(
    screen.getByRole('columnheader', { name: 'Total credited quota' })
  ).toBeVisible()
  expect(screen.getByRole('columnheader', { name: 'Total Used' })).toBeVisible()
  expect(within(fundedRow).getByRole('cell', { name: '$2' })).toBeVisible()
  expect(
    within(fundedRow).getByRole('cell', { name: '$4 Refunded quota: $0.5' })
  ).toBeVisible()
  expect(within(fundedRow).getByRole('cell', { name: '$1' })).toBeVisible()
  expect(fundedRow).not.toHaveTextContent('999999')
  expect(fundedRow).not.toHaveTextContent('¥')

  const newRow = screen.getByRole('row', { name: /new-customer/ })
  expect(within(newRow).getAllByRole('cell', { name: '$0' })).toHaveLength(3)
  expect(newRow).not.toHaveTextContent('Refunded quota')
  expect(
    screen.getByText(
      'Available balance excludes active reservations; total used includes settled charges only.'
    )
  ).toBeVisible()
})

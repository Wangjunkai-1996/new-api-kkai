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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SalesAccounts } from '../components/admin/sales-accounts'

const rebates = {
  cooling: 101,
  available: 202,
  pendingApproval: 303,
  approved: 404,
  issued: 505,
  locked: 606,
  closed: 707,
  pendingCalculationCount: 1,
}
const performance = {
  invitedCount: 7,
  effectiveInvitedCount: 2,
  rechargeAmount: 1234,
  unverifiedRechargeCount: 1,
  rebates,
}
const account = {
  userId: 1,
  userName: 'seller-1',
  active: false,
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-02T00:00:00Z',
  note: 'Historical seller',
  performance,
}
const response = {
  items: [account],
  total: 1,
  page: 1,
  pageSize: 20,
  creditCurrency: 'usd_credit_v1',
  summary: performance,
}

function renderSalesAccounts() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
  return render(
    <QueryClientProvider client={client}>
      <SalesAccounts />
    </QueryClientProvider>
  )
}

test('keeps all-account totals across server pages and shows inactive seller history without per-row requests', async () => {
  const user = userEvent.setup()
  const accounts = Array.from({ length: 41 }, (_, index) => ({
    ...account,
    userId: index + 1,
    userName: `seller-${index + 1}`,
  }))
  const get = vi.spyOn(api, 'get').mockImplementation(async (_url, config) => {
    const params = config?.params as { page?: number } | undefined
    const page = Number(params?.page ?? 1)
    return {
      data: {
        success: true,
        data: {
          ...response,
          items: accounts.slice((page - 1) * 20, page * 20),
          total: 41,
          page,
          summary: {
            ...performance,
            invitedCount: 287,
            effectiveInvitedCount: 82,
            rechargeAmount: 50594,
            unverifiedRechargeCount: 41,
          },
        },
      },
    }
  })
  renderSalesAccounts()

  await screen.findByText('Page 1 of 3')
  expect(screen.getAllByRole('row')).toHaveLength(21)
  expect(screen.getByText('287')).toBeInTheDocument()
  expect(screen.getByText('82')).toBeInTheDocument()
  expect(screen.getByText('$505.94')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Previous page' })).toBeDisabled()
  const inactiveRow = within(screen.getByRole('row', { name: /seller-1 #1 / }))
  expect(inactiveRow.getByText('Inactive')).toBeInTheDocument()
  expect(inactiveRow.getByText('7')).toBeInTheDocument()
  expect(inactiveRow.getByText('2')).toBeInTheDocument()
  expect(inactiveRow.getByText('$12.34')).toBeInTheDocument()
  for (const amount of [
    '$1.01',
    '$2.02',
    '$3.03',
    '$4.04',
    '$5.05',
    '$6.06',
    '$7.07',
  ]) {
    expect(inactiveRow.getByText(amount)).toBeInTheDocument()
  }
  expect(
    inactiveRow.getByText('1 historical recharge amounts need verification')
  ).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Next page' }))
  await screen.findByText('Page 2 of 3')
  expect(screen.getAllByRole('row')).toHaveLength(21)
  expect(screen.getByText('seller-21')).toBeInTheDocument()
  expect(screen.queryByText('seller-1')).not.toBeInTheDocument()
  expect(screen.getByText('$505.94')).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Next page' }))
  await screen.findByText('Page 3 of 3')
  expect(screen.getAllByRole('row')).toHaveLength(2)
  expect(screen.getByText('seller-41')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled()
  expect(screen.getByText('287')).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Previous page' }))
  await screen.findByText('Page 2 of 3')
  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  await waitFor(() => expect(get).toHaveBeenCalledTimes(5))
  expect(
    get.mock.calls.map(([, config]) => {
      const params = config?.params as { page?: number } | undefined
      return params?.page
    })
  ).toEqual([1, 2, 3, 2, 2])
  for (const call of get.mock.calls) {
    expect(call).toEqual([
      '/invitations/api/admin/sales-accounts',
      { params: { page: expect.any(Number), pageSize: 20 } },
    ])
  }
})

test.each([
  ['usd_credit_v1', '$0.00', '$12.34'],
  ['legacy_075', '0.00', '12.34'],
])(
  'renders real zero performance using %s units',
  async (creditCurrency, zero, amount) => {
    const zeroPerformance = {
      invitedCount: 0,
      effectiveInvitedCount: 0,
      rechargeAmount: 0,
      unverifiedRechargeCount: 0,
      rebates: {
        cooling: 0,
        available: 0,
        pendingApproval: 0,
        approved: 0,
        issued: 0,
        locked: 0,
        closed: 0,
        pendingCalculationCount: 0,
      },
    }
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: {
          ...response,
          creditCurrency,
          items: [{ ...account, performance: zeroPerformance }],
        },
      },
    })
    renderSalesAccounts()
    const row = within(await screen.findByRole('row', { name: /seller-1 #1 / }))
    expect(row.getAllByText('0')).toHaveLength(2)
    expect(row.getAllByText(zero)).toHaveLength(8)
    expect(screen.getByText(amount)).toBeInTheDocument()
    expect(row.queryByText(/need verification/)).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Next page' })
    ).not.toBeInTheDocument()
  }
)

test('shows unavailable performance for an older backend instead of inventing zero totals', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        items: [
          {
            userId: 1,
            userName: 'seller-1',
            active: false,
            createdAt: account.createdAt,
            updatedAt: account.updatedAt,
            note: account.note,
          },
        ],
        total: 1,
        page: 1,
        pageSize: 20,
      },
    },
  })
  renderSalesAccounts()
  const row = within(await screen.findByRole('row', { name: /seller-1 #1 / }))
  expect(screen.getAllByText(/unavailable/i).length).toBeGreaterThan(0)
  expect(row.getAllByText('—').length).toBeGreaterThanOrEqual(3)
  expect(row.queryByText('0')).not.toBeInTheDocument()
  expect(screen.queryByText('$0.00')).not.toBeInTheDocument()
  expect(row.getByRole('button', { name: 'Edit' })).toBeEnabled()
})

test('keeps sales access editing functional and refreshes statistics after saving', async () => {
  const user = userEvent.setup()
  const get = vi
    .spyOn(api, 'get')
    .mockResolvedValue({ data: { success: true, data: response } })
  const put = vi.spyOn(api, 'put').mockResolvedValue({
    data: { success: true, data: { active: true, registered: true } },
  })
  renderSalesAccounts()
  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  const dialog = within(screen.getByRole('dialog'))
  expect(dialog.getByLabelText('User ID')).toHaveAttribute('readonly')
  await user.click(dialog.getByLabelText('Sales Access Enabled'))
  await user.clear(dialog.getByLabelText('Note'))
  await user.type(dialog.getByLabelText('Note'), 'Resumed sales access')
  await user.click(dialog.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/invitations/api/admin/sales-accounts/1',
      { active: true, note: 'Resumed sales access' },
      { skipErrorHandler: true }
    )
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(get).toHaveBeenCalledTimes(2)
})

test('can retry a failed performance request without displaying fabricated results', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'get')
    .mockRejectedValueOnce(new Error('Statistics unavailable'))
    .mockResolvedValue({ data: { success: true, data: response } })
  renderSalesAccounts()
  await screen.findByText('Failed to load sales accounts')
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
  expect(screen.queryByText('$0.00')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  await screen.findByText('seller-1')
  expect(screen.getAllByText('$12.34').length).toBeGreaterThan(0)
})

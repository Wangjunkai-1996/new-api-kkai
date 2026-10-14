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
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { SalesCustomers } from '../components/user/sales-customers'
import { formatSalesAmount } from '../format'
import type { SalesCustomers as SalesCustomersResponse } from '../types'

const rebates = {
  cooling: 100,
  available: 200,
  pendingApproval: 300,
  approved: 400,
  issued: 500,
  locked: 0,
  closed: 0,
  pendingCalculationCount: 1,
}
const data: SalesCustomersResponse = {
  creditCurrency: 'usd_credit_v1',
  total: 21,
  page: 1,
  pageSize: 20,
  summary: {
    invitedCount: 21,
    effectiveInvitedCount: 12,
    rechargeAmount: 4500,
    unverifiedRechargeCount: 1,
    rebates,
  },
  items: [
    {
      userId: 42,
      userName: 'customer-42',
      invitedAt: '2026-10-01T00:00:00Z',
      firstRechargeAt: '2026-10-02T00:00:00Z',
      effective: true,
      rechargeAmount: 4500,
      unverifiedRechargeCount: 1,
      rebates,
    },
  ],
}

test('uses authoritative customer totals and shows unknown historical amounts without inventing zero', async () => {
  const user = userEvent.setup()
  useAuthStore.getState().auth.setUser({ id: 4, username: 'seller', role: 1 })
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/invitations/api/invitations/sales-customers/42/recharges') {
      return {
        data: {
          success: true,
          data: {
            total: 1,
            page: 1,
            pageSize: 10,
            items: [
              {
                id: 3,
                completedAt: '2026-10-02T00:00:00Z',
                amount: null,
                originalAmount: '100',
                paymentProvider: 'legacy',
                creditEpoch: 'legacy_075',
                rebateAmount: null,
                rebateStatus: 'not_generated',
                effectiveAt: null,
              },
            ],
          },
        },
      }
    }
    return { data: { success: true, data } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <SalesCustomers />
    </QueryClientProvider>
  )
  await screen.findByText('Sales Overview')
  expect(screen.getByText('21')).toBeInTheDocument()
  expect(screen.getByText('12')).toBeInTheDocument()
  expect(screen.getAllByText('$45.00').length).toBeGreaterThan(0)
  expect(
    screen.getAllByText('1 historical recharge amounts need verification')
      .length
  ).toBeGreaterThan(0)
  await user.click(
    screen.getAllByRole('button', { name: 'Recharge Details' })[0]
  )
  await screen.findByText('Historical Amount Unverified')
  expect(screen.getByText('Awaiting Rebate Calculation')).toBeInTheDocument()
  await waitFor(() =>
    expect(get).toHaveBeenCalledWith(
      '/invitations/api/invitations/sales-customers/42/recharges',
      { params: { page: 1, pageSize: 10 } }
    )
  )
})

test('does not label legacy credit cents as dollars', () => {
  expect(formatSalesAmount(1234, 'legacy_075')).toBe('12.34')
  expect(formatSalesAmount(1234, 'usd_credit_v1')).toBe('$12.34')
})

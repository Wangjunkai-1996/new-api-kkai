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
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { RebateOrderRecords } from '../components/admin/rebate-order-records'
import { RebatePayoutRecords } from '../components/admin/rebate-payout-records'
import { RebateTransfer } from '../components/user/rebate-transfer'
import type { AdminRebateOrderRecord, RebateRecord } from '../types'

const clients: QueryClient[] = []
afterEach(() => clients.splice(0).forEach((client) => client.clear()))

const order: AdminRebateOrderRecord = {
  orderId: 7,
  orderType: 'topup',
  inviterId: 4,
  inviteeId: 5,
  userGroup: 'default',
  orderAmount: 10000,
  rebateAmount: 1500,
  status: 'pending',
  ruleMissing: false,
  localRebateRecordId: 7,
  orderTime: '2026-10-01T00:00:00Z',
  effectiveAt: '2026-10-08T00:00:00Z',
  initializationEndsAt: '2026-10-08T00:00:00Z',
  adminAdjusted: false,
  canModify: true,
  canClose: false,
  canReopen: false,
  canEndInitialization: false,
  canExtendInitialization: false,
}
const payout: RebateRecord = {
  id: 7,
  inviterId: 4,
  userName: 'seller-four',
  orderType: 'topup',
  orderAmount: 10000,
  rebateAmount: 1500,
  status: 'pending',
  canPay: true,
  canReverse: false,
  unlockRequired: false,
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-01T00:00:00Z',
}

test.each([
  {
    name: 'order adjustment',
    Component: RebateOrderRecords,
    record: order,
    row: 'topup #7',
    action: 'Adjust Rebate',
    query: 'order-records',
    error: 'Failed to load rebate order records',
  },
  {
    name: 'balance payout',
    Component: RebatePayoutRecords,
    record: payout,
    row: 'seller-four',
    action: 'Pay to Balance',
    query: 'payout-records',
    error: 'Failed to load payout records',
  },
])(
  'closes $name and removes cached actions after a refresh error',
  async (scenario) => {
    let failing = false
    vi.spyOn(api, 'get').mockImplementation(async () => ({
      data: failing
        ? { success: false, message: 'Database unavailable' }
        : {
            success: true,
            data: {
              items: [scenario.record],
              total: 21,
              page: 1,
              pageSize: 20,
            },
          },
    }))
    const post = vi.spyOn(api, 'post')
    const patch = vi.spyOn(api, 'patch')
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    clients.push(client)
    const user = userEvent.setup()
    render(
      <QueryClientProvider client={client}>
        <scenario.Component />
      </QueryClientProvider>
    )
    await screen.findByText(scenario.row)
    await user.click(screen.getByRole('button', { name: 'Actions' }))
    await user.click(screen.getByRole('menuitem', { name: scenario.action }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    failing = true
    await act(async () => {
      await client.refetchQueries({
        queryKey: ['kkai', 'invitations', 'admin', scenario.query],
      })
    })
    await screen.findByText(scenario.error)
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Next page' })
    ).not.toBeInTheDocument()
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    expect(post).not.toHaveBeenCalled()
    expect(patch).not.toHaveBeenCalled()

    failing = false
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    await screen.findByText(scenario.row)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(post).not.toHaveBeenCalled()
    expect(patch).not.toHaveBeenCalled()
  }
)

test.each(['refresh failure', 'sales access disabled'])(
  'closes an open rebate request after %s',
  async (reason) => {
    let failing = false
    vi.spyOn(api, 'get').mockImplementation(async () => ({
      data: failing
        ? { success: false, message: 'Database unavailable' }
        : {
            success: true,
            data: {
              amount: 1500,
              recordIds: [7],
              creditCurrency: 'usd_credit_v1',
            },
          },
    }))
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true, data: { id: 8 } } })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    clients.push(client)
    const user = userEvent.setup()
    const view = render(
      <QueryClientProvider client={client}>
        <RebateTransfer sales />
      </QueryClientProvider>
    )
    await screen.findByText('$15.00')
    await user.click(
      screen.getByRole('button', { name: 'Request Rebate Review' })
    )
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeEnabled()

    if (reason === 'refresh failure') {
      failing = true
      await act(async () => {
        await client.refetchQueries({
          queryKey: ['kkai', 'invitations', 'available-rebates'],
        })
      })
      await screen.findByText('Failed to load available rebates')
    } else {
      view.rerender(
        <QueryClientProvider client={client}>
          <RebateTransfer sales disabled />
        </QueryClientProvider>
      )
      expect(
        screen.getByText(
          'Sales access is inactive. Existing requests remain available.'
        )
      ).toBeInTheDocument()
    }
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(post).not.toHaveBeenCalled()

    failing = false
    if (reason === 'refresh failure') {
      await user.click(screen.getByRole('button', { name: 'Retry' }))
    } else {
      view.rerender(
        <QueryClientProvider client={client}>
          <RebateTransfer sales />
        </QueryClientProvider>
      )
    }
    await screen.findByText('$15.00')
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    await user.click(
      screen.getByRole('button', { name: 'Request Rebate Review' })
    )
    await user.click(screen.getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(post).toHaveBeenCalledTimes(1))
    expect(post).toHaveBeenCalledWith(
      '/invitations/api/invitations/rebate-requests',
      { amount: 1500, rebateRecordIds: [7] },
      { skipErrorHandler: true }
    )
  }
)

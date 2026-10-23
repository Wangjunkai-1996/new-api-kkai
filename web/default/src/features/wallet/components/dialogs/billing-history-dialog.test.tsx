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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { actOnWalletOrder, getWalletOrder } from '../../payment-flow-api'
import { paymentOrder, paymentWallet } from '../../payment-flow-fixtures'
import type { PaymentOrder } from '../../payment-flow-types'
import { BillingHistoryDialog } from './billing-history-dialog'

const history = vi.hoisted(() => ({
  records: [] as import('../../payment-flow-types').PaymentOrder[],
}))
vi.mock('../../hooks/use-billing-history', () => ({
  useBillingHistory: () => ({
    records: history.records,
    total: history.records.length,
    page: 1,
    pageSize: 10,
    keyword: '',
    status: 'all',
    exact: false,
    loading: false,
    error: false,
    serverTime: 1_790_000_100,
    handlePageChange: vi.fn(),
    handlePageSizeChange: vi.fn(),
    handleSearch: vi.fn(),
    handleStatusChange: vi.fn(),
    handleExactChange: vi.fn(),
    refresh: vi.fn(),
  }),
}))
vi.mock('../../hooks/use-wallet-payment-flow', () => ({
  useWalletPaymentIdentity: () => ['test', 0, 1, 'session'],
}))
vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: { tenant_id: 0 } }),
}))
vi.mock('../../payment-flow-api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../payment-flow-api')>()),
  actOnWalletOrder: vi.fn(),
  getWalletOrder: vi.fn(),
}))
let client: QueryClient
beforeEach(() => {
  vi.resetAllMocks()
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
  useAuthStore.getState().auth.setUser({ id: 1, role: 100, username: 'root' })
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
})
it.each([
  { status: 'ended', scope: 'self' },
  { status: 'timed_out', scope: 'self' },
  { status: 'ended', scope: 'admin' },
  { status: 'timed_out', scope: 'admin' },
] as const)(
  'keeps $status history read-only in $scope even if an old response contains actions',
  async ({ status, scope }) => {
    const order = paymentOrder({
      display_status: status,
      status_group: 'closed',
      allowed_actions: [
        'resume',
        'end',
        'cancel',
        'reconcile',
        'contact_support',
      ],
      local_end_reason: status === 'ended' ? 'user_ended' : 'timeout',
      local_ended_at: 1_790_000_050,
    })
    history.records = [order]
    vi.mocked(getWalletOrder).mockResolvedValue({
      order,
      wallet: paymentWallet(),
    })
    render(
      <QueryClientProvider client={client}>
        <BillingHistoryDialog
          open
          onOpenChange={vi.fn()}
          scope={scope}
          onRestore={vi.fn()}
        />
      </QueryClientProvider>
    )
    for (const name of [
      'Continue payment',
      'End this top-up',
      'Cancel order',
      'Check payment',
      'Verify payment',
    ]) {
      expect(screen.queryByRole('button', { name })).not.toBeInTheDocument()
    }
    fireEvent.click(screen.getByRole('button', { name: 'Order details' }))
    await waitFor(() => expect(getWalletOrder).toHaveBeenCalledTimes(1))
    expect(
      screen.getByText(
        'Do not pay through the previous payment page. If a payment was actually received, it will still be verified and credited.'
      )
    ).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Copy order number' })
    ).toBeVisible()
    expect(actOnWalletOrder).not.toHaveBeenCalled()
  }
)
it('ends an active historical top-up without restoring it over the current order', async () => {
  const original = paymentOrder()
  history.records = [original]
  const ended: PaymentOrder = {
    ...original,
    display_status: 'ended',
    status_group: 'closed',
    allowed_actions: ['contact_support'],
    local_end_reason: 'user_ended',
    local_ended_at: 1_790_000_050,
  }
  const wallet = paymentWallet(paymentOrder({ trade_no: 'CURRENT-ORDER' }))
  vi.mocked(actOnWalletOrder).mockResolvedValue({ order: ended, wallet })
  const apply = vi.fn(),
    restore = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <BillingHistoryDialog
        open
        onOpenChange={vi.fn()}
        onWalletChange={apply}
        onRestore={restore}
      />
    </QueryClientProvider>
  )
  fireEvent.click(screen.getByRole('button', { name: 'End this top-up' }))
  expect(actOnWalletOrder).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Confirm ending' }))
  await waitFor(() => expect(apply).toHaveBeenCalledWith(wallet))
  expect(actOnWalletOrder).toHaveBeenCalledWith(original, 'end', '')
  expect(restore).not.toHaveBeenCalled()
  expect(screen.getByText('Top-up ended')).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'Continue payment' })
  ).not.toBeInTheDocument()
})
it('shows authorized funds review after a late payment without reopening the ended checkout', () => {
  history.records = [
    paymentOrder({
      display_status: 'credit_review',
      status_group: 'review',
      allowed_actions: [
        'resume',
        'end',
        'cancel',
        'reconcile',
        'contact_support',
      ],
      local_end_reason: 'timeout',
      local_ended_at: 1_790_000_050,
    }),
  ]
  render(
    <QueryClientProvider client={client}>
      <BillingHistoryDialog
        open
        onOpenChange={vi.fn()}
        scope='admin'
        onRestore={vi.fn()}
      />
    </QueryClientProvider>
  )
  expect(screen.getByRole('button', { name: 'Verify payment' })).toBeVisible()
  for (const name of ['Continue payment', 'End this top-up', 'Cancel order']) {
    expect(screen.queryByRole('button', { name })).not.toBeInTheDocument()
  }
})
it('keeps an order with an unknown creation time read-only for administrators', () => {
  history.records = [
    paymentOrder({
      display_status: 'unavailable',
      status_group: 'all',
      allowed_actions: ['contact_support'],
      created_at: 0,
      deadline_at: null,
    }),
  ]
  render(
    <QueryClientProvider client={client}>
      <BillingHistoryDialog open onOpenChange={vi.fn()} scope='admin' />
    </QueryClientProvider>
  )
  expect(screen.getByRole('button', { name: 'Order details' })).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'Verify payment' })
  ).not.toBeInTheDocument()
})

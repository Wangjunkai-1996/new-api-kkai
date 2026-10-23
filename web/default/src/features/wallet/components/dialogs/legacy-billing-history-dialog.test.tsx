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
import type { ReactNode } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { getAllBillingHistory, getUserBillingHistory } from '../../api'
import { LegacyBillingHistoryDialog } from './legacy-billing-history-dialog'

vi.mock('../../api', () => ({
  getAllBillingHistory: vi.fn(),
  getUserBillingHistory: vi.fn(),
}))
vi.mock('../../hooks/use-wallet-payment-flow', () => ({
  useWalletPaymentIdentity: () => ['example.test', 12, 1, 'session'],
}))
vi.mock('@/features/site-config', () => ({
  useSiteConfig: () => ({ data: { tenant_id: 12 } }),
}))
vi.mock('@/components/dialog', () => ({
  Dialog: (props: { open: boolean; children: ReactNode }) =>
    props.open ? <section>{props.children}</section> : null,
}))
let client: QueryClient
beforeEach(() => {
  vi.resetAllMocks()
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, role: ROLE.SUPER_ADMIN, username: 'root' })
  const response = {
    success: true,
    data: {
      items: [
        {
          id: 1,
          user_id: 1,
          amount: 10,
          money: 10,
          trade_no: 'old-pending-order',
          payment_method: 'alipay',
          create_time: 1,
          status: 'pending' as const,
        },
      ],
      total: 1,
      today_payment_total: 0,
      today_payment_date: '2026-10-24',
    },
  }
  vi.mocked(getUserBillingHistory).mockResolvedValue(response)
  vi.mocked(getAllBillingHistory).mockResolvedValue(response)
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
})
it.each(['self', 'admin'] as const)(
  'reads legacy %s history without exposing manual completion for old pending orders',
  async (scope) => {
    render(
      <QueryClientProvider client={client}>
        <LegacyBillingHistoryDialog open onOpenChange={vi.fn()} scope={scope} />
      </QueryClientProvider>
    )
    expect(await screen.findByText('old-pending-order')).toBeVisible()
    const requested =
      scope === 'self' ? getUserBillingHistory : getAllBillingHistory
    const unused =
      scope === 'self' ? getAllBillingHistory : getUserBillingHistory
    expect(requested).toHaveBeenCalledWith(1, 10, '')
    expect(unused).not.toHaveBeenCalled()
    for (const action of [
      'Complete Order',
      'Continue payment',
      'Check payment',
      'Verify payment',
      'Cancel order',
    ]) {
      expect(
        screen.queryByRole('button', { name: action })
      ).not.toBeInTheDocument()
    }
  }
)
it('does not fetch administrator history without billing read permission', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, role: ROLE.ADMIN, username: 'restricted' })
  render(
    <QueryClientProvider client={client}>
      <LegacyBillingHistoryDialog open onOpenChange={vi.fn()} scope='admin' />
    </QueryClientProvider>
  )
  expect(screen.getByRole('alert')).toHaveTextContent('Admin access required')
  await waitFor(() => expect(getAllBillingHistory).not.toHaveBeenCalled())
  expect(getUserBillingHistory).not.toHaveBeenCalled()
})

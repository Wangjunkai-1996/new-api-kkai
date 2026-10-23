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
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { getWalletFundingSummary } from '../funding-summary-api'
import { WalletStatsCard } from './wallet-stats-card'

vi.mock('../funding-summary-api', () => ({
  getWalletFundingSummary: vi.fn(),
}))
vi.mock('../hooks/use-wallet-payment-flow', () => ({
  useWalletPaymentIdentity: () => ['example.test', 0, 1, 'session'],
}))

let client: QueryClient
beforeEach(() => {
  vi.clearAllMocks()
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
})
afterEach(() => client.clear())

it('does not request schema18 funding data for the legacy wallet, even with cached statistics', () => {
  client.setQueryData(
    ['wallet-payment', 'example.test', 0, 1, 'session', 'funding-summary'],
    { cash_payments: [{ amount_minor: '1000', currency: 'USD', exponent: 2 }] }
  )
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={null} />
    </QueryClientProvider>
  )
  expect(screen.getByText('Current Balance')).toBeVisible()
  expect(screen.queryByText('$10.00')).not.toBeInTheDocument()
  expect(getWalletFundingSummary).not.toHaveBeenCalled()
})

it('requests and displays funding statistics when the lifecycle wallet enables them', async () => {
  vi.mocked(getWalletFundingSummary).mockResolvedValue({
    cash_payments: [{ amount_minor: '1000', currency: 'USD', exponent: 2 }],
    confirmed_payment_count: 1,
    unknown_payment_count: 0,
    rebate_credited_quota: '0',
    rebate_credit_display: { amount: '0', unit: 'USD' },
    quota_epoch: 'usd_credit_v1',
    refund_adjusted: false,
    server_time: 1_790_000_000,
  })
  render(
    <QueryClientProvider client={client}>
      <WalletStatsCard user={null} fundingSummaryEnabled />
    </QueryClientProvider>
  )
  expect(await screen.findByText('$10.00')).toBeVisible()
  expect(getWalletFundingSummary).toHaveBeenCalledTimes(1)
})

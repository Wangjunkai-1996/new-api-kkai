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
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'

import type { WalletFundingSummary as FundingData } from '../funding-summary-api'
import type { UserWalletData } from '../types'
import { WalletFundingSummary } from './wallet-funding-summary'
import { WalletStatsCard } from './wallet-stats-card'

const funding = vi.hoisted(() => ({
  data: undefined as FundingData | undefined,
  isPending: false,
  isError: false,
  refetch: vi.fn(),
}))
vi.mock('../hooks/use-wallet-funding-summary', () => ({
  useWalletFundingSummary: () => funding,
}))
beforeEach(() => {
  funding.data = {
    cash_payments: [
      { amount_minor: '368000', currency: 'CNY', exponent: 2 },
      { amount_minor: '2000', currency: 'USD', exponent: 2 },
    ],
    confirmed_payment_count: 9,
    unknown_payment_count: 3,
    rebate_credited_quota: '9000000',
    rebate_credit_display: { amount: '18', unit: 'USD' },
    quota_epoch: 'usd_credit_v1',
    refund_adjusted: false,
    server_time: 1_790_000_000,
  }
  funding.isError = false
  funding.isPending = false
})

it('keeps cash payments distinct from wallet balance and usage', () => {
  const user: UserWalletData = {
    id: 1,
    username: 'test',
    quota: 42300000,
    used_quota: 206700000,
    request_count: 28412,
    aff_quota: 0,
    aff_history_quota: 0,
    aff_count: 0,
    group: 'default',
  }
  render(<WalletStatsCard user={user} fundingSummaryEnabled />)
  expect(screen.getByText('CN¥3,680.00')).toBeVisible()
  expect(screen.getByText('$20.00')).toBeVisible()
  expect(screen.getByText('Includes top-ups, rebates and gifts')).toBeVisible()
  expect(
    screen.getByText('Excludes rebates and gifts · Before refunds')
  ).toBeVisible()
  expect(screen.getByText('Total consumed credits')).toBeVisible()
  expect(screen.getByText('28,412')).toBeVisible()
})

it('shows all-time confirmed counts, unverified history and exact net rebate credits together', () => {
  render(<WalletFundingSummary enabled />)
  expect(
    screen.getByText(
      '9 confirmed payments · Original currencies, excluding rebates, gifts and redemption codes. Before refunds.'
    )
  ).toBeVisible()
  expect(
    screen.getByText(
      /3 historical orders have unverified payments and are not included/
    )
  ).toHaveTextContent('Verified net rebates credited: $18.00 credits.')
})

it('shows an unavailable state and retry instead of a zero after a summary failure', () => {
  funding.data = undefined
  funding.isError = true
  render(<WalletFundingSummary enabled />)
  expect(screen.getByText('Not available')).toBeVisible()
  expect(screen.getByText('Payment totals unavailable')).toBeVisible()
  expect(screen.queryByText('$0.00')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(funding.refetch).toHaveBeenCalledTimes(1)
})

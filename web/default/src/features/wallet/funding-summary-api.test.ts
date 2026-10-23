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
import { expect, it, vi } from 'vitest'

import { getWalletFundingSummary } from './funding-summary-api'

const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/lib/api', () => ({ api }))

it('reads the authenticated summary envelope without narrowing it to a history page', async () => {
  const summary = {
    cash_payments: [
      { amount_minor: '368000', currency: 'CNY', exponent: 2 },
      { amount_minor: '2000', currency: 'USD', exponent: 2 },
    ],
    confirmed_payment_count: 9,
    unknown_payment_count: 3,
    rebate_credited_quota: '-9000000',
    rebate_credit_display: { amount: '-18', unit: 'USD' },
    quota_epoch: 'usd_credit_v1',
    refund_adjusted: false,
    server_time: 1_790_000_000,
  }
  api.get.mockResolvedValue({ data: { success: true, data: summary } })
  const signal = new AbortController().signal
  await expect(getWalletFundingSummary(signal)).resolves.toEqual(summary)
  expect(api.get).toHaveBeenCalledWith('/api/user/topup/summary', { signal })
})

it('rejects a failed summary instead of creating a zero payment total', async () => {
  api.get.mockResolvedValue({
    data: { success: false, message: 'Unavailable' },
  })
  await expect(getWalletFundingSummary()).rejects.toThrow('Unavailable')
})

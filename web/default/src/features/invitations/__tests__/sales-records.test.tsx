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
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { RebateRecords } from '../components/user/rebate-records'
import type { RebateRecord } from '../types'

const record: RebateRecord = {
  id: 1,
  inviterId: 4,
  orderType: 'subscription',
  orderAmount: 0,
  rebateAmount: 1234,
  creditCurrency: 'legacy_075',
  status: 'approved',
  displayStatus: 'paid',
  issuedAt: '2026-10-14T00:00:00Z',
  unlockRequired: false,
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-14T00:00:00Z',
}

function renderRecords(sales: boolean) {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { items: [record], total: 1, page: 1, pageSize: 10 },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RebateRecords sales={sales} />
    </QueryClientProvider>
  )
}

test('sales keeps all rebate sources and formats legacy credit without an invented order payment amount', async () => {
  renderRecords(true)
  await screen.findByText('Legacy Site Credits')
  expect(screen.queryByText('Order Amount')).not.toBeInTheDocument()
  expect(screen.getAllByText('subscription')).toHaveLength(2)
  expect(screen.getAllByText('12.34')).toHaveLength(2)
  expect(screen.queryByText('$12.34')).not.toBeInTheDocument()
  expect(screen.getAllByText('Issue Confirmed')).toHaveLength(2)
})

test('ordinary records keep the existing order amount column', async () => {
  renderRecords(false)
  expect((await screen.findAllByText('Order Amount')).length).toBeGreaterThan(0)
  expect(screen.getAllByText('$12.34')).toHaveLength(2)
})

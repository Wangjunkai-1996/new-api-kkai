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
import { render, screen, within } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { RebateRecords } from '../components/user/rebate-records'
import { formatRebateRate } from '../format'
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

afterEach(() => useAuthStore.getState().auth.reset())

function renderRecords(sales: boolean, records: RebateRecord[] = [record]) {
  useAuthStore.getState().auth.setUser({ id: 4, username: 'qa-user', role: 1 })
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { items: records, total: records.length, page: 1, pageSize: 10 },
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
  return get
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

test('each order and signup reward shows its own rate next to its amount in both layouts', async () => {
  const cases: [Partial<RebateRecord>, string][] = [
    [{ orderType: 'topup', rebateRatio: 0.05 }, 'Rate for this record: 5%'],
    [{ orderType: 'subscription', rebateRatio: 0 }, 'Rate for this record: 0%'],
    [
      {
        orderType: 'redemption',
        rebateRatio: 0.075,
        status: 'initializing',
        displayStatus: 'confirming',
      },
      'Rate for this record: 7.5%',
    ],
    [
      {
        orderType: 'topup',
        rebateRatio: null,
        status: 'initializing',
        displayStatus: 'confirming',
      },
      'Rate pending confirmation',
    ],
    [
      {
        orderType: 'subscription',
        status: 'pending',
        displayStatus: 'estimated',
      },
      'Rate pending confirmation',
    ],
    [
      { orderType: 'redemption', status: 'completed', displayStatus: 'paid' },
      'No rate recorded',
    ],
    [
      { orderType: 'invite_inviter', rebateRatio: 0 },
      'Fixed reward (not percentage-based)',
    ],
    [
      { orderType: 'invite_invitee', rebateRatio: 0.15 },
      'Fixed reward (not percentage-based)',
    ],
    [
      { orderType: 'topup', status: 'rejected', displayStatus: 'estimated' },
      'No rate recorded',
    ],
    [
      {
        orderType: 'topup',
        status: 'initializing',
        displayStatus: 'initializing',
        issuedAt: record.issuedAt,
      },
      'No rate recorded',
    ],
  ]
  const records = cases.map(([fields], index) => ({
    ...record,
    id: index + 1,
    rebateAmount: (index + 1) * 100 + 34,
    issuedAt: null,
    ...fields,
  }))
  const get = renderRecords(false, records)
  await screen.findAllByText('Rate for this record: 5%')
  for (const [index, [, label]] of cases.entries()) {
    const amounts = screen.getAllByText(`$${index + 1}.34`)
    expect(amounts.map((amount) => amount.tagName)).toEqual(['TD', 'DD'])
    for (const amount of amounts) {
      expect(within(amount).getByText(label)).toBeInTheDocument()
    }
  }
  expect(get).toHaveBeenCalledTimes(1)
})

test('sales history preserves its currency and distinguishes invalid, unknown and locked records', async () => {
  const base = {
    ...record,
    creditCurrency: 'usd_credit_v1' as const,
    status: 'pending' as const,
    displayStatus: 'estimated',
    issuedAt: null,
  }
  renderRecords(true, [
    { ...base, id: 1, rebateRatio: -0.1 },
    { ...base, id: 2, orderType: 'unknown', rebateRatio: 0.05 },
    { ...base, id: 3, displayStatus: 'waiting_unlock' },
  ])
  expect(await screen.findAllByText('No rate recorded')).toHaveLength(6)
  expect(screen.getAllByText('$12.34')).toHaveLength(6)
  expect(
    screen.queryByText('Rate pending confirmation')
  ).not.toBeInTheDocument()
})

test('formats only finite fractions in the interface locale', () => {
  for (const value of [
    null,
    undefined,
    '0.075',
    true,
    Number.NaN,
    Infinity,
    -0.01,
    1.01,
  ]) {
    expect(formatRebateRate(value, 'zhCN')).toBeNull()
  }
  expect(
    [0, -0, 0.0001, 0.075, 1].map((value) => formatRebateRate(value, 'zhTW'))
  ).toEqual(['0%', '0%', '0.01%', '7.5%', '100%'])
  expect(formatRebateRate(0.075, 'fr')).toBe(
    new Intl.NumberFormat('fr', {
      style: 'percent',
      maximumFractionDigits: 1,
    }).format(0.075)
  )
})

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
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  getResellerCustomers,
  getResellerEarnings,
  getResellerWithdrawals,
} from './api'

afterEach(() => {
  vi.restoreAllMocks()
})

describe.each([
  {
    name: 'customers',
    path: '/api/reseller/customers',
    load: getResellerCustomers,
  },
  {
    name: 'withdrawals',
    path: '/api/reseller/withdrawals',
    load: getResellerWithdrawals,
  },
])('$name pagination compatibility', ({ path, load }) => {
  test('slices the second page of a legacy response and retains its total', async () => {
    const items = Array.from({ length: 21 }, (_, index) => ({ id: index + 1 }))
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: items },
    })

    expect(await load(2, 20)).toEqual({
      items: [{ id: 21 }],
      total: 21,
    })
    expect(get).toHaveBeenCalledExactlyOnceWith(path, {
      params: { p: 2, page_size: 20 },
    })
  })

  test.each([
    { items: [{ id: 21 }], total: 21 },
    { items: [], total: 0 },
  ])(
    'preserves server pagination with total $total',
    async ({ items, total }) => {
      vi.spyOn(api, 'get').mockResolvedValue({
        data: { success: true, data: items, total },
      })

      const result = await load(2, 20)
      expect(result).toEqual({ items, total })
    }
  )
})

describe('earnings pagination compatibility', () => {
  test('slices the second legacy ledger page without changing wallet or lifetime totals', async () => {
    const ledger = Array.from({ length: 21 }, (_, index) => ({ id: index + 1 }))
    const data = {
      wallet: {
        pending_minor: 123,
        available_minor: 456,
        frozen_minor: 78,
        debt_minor: 9,
      },
      ledger,
      estimated_profit_minor: 100,
      total_profit_minor: 200,
      withdrawal_fee_minor: 30,
    }
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data },
    })

    expect(await getResellerEarnings(2, 20)).toEqual({
      ...data,
      ledger: [{ id: 21 }],
      ledger_total: 21,
    })
    expect(get).toHaveBeenCalledExactlyOnceWith('/api/reseller/earnings', {
      params: { p: 2, page_size: 20 },
    })
  })

  test.each([
    { ledger: [{ id: 21 }], ledger_total: 21 },
    { ledger: [], ledger_total: 0 },
  ])(
    'preserves server ledger pagination with total $ledger_total',
    async (page) => {
      const data = {
        wallet: {
          pending_minor: 0,
          available_minor: 0,
          frozen_minor: 0,
          debt_minor: 0,
        },
        estimated_profit_minor: 0,
        ...page,
      }
      vi.spyOn(api, 'get').mockResolvedValue({
        data: { success: true, data },
      })

      const result = await getResellerEarnings(2, 20)
      expect(result).toEqual(data)
    }
  )
})

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
import { describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { getMembershipOrder, resolveMembershipOrder } from './api'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))

it('reads the authenticated membership order by its encoded trade number', async () => {
  const order = { id: 9, trade_no: 'MEMBER-9-return', status: 'success' }
  vi.mocked(api.get).mockResolvedValue({ data: { success: true, data: order } })
  await expect(getMembershipOrder('MEMBER-9-return')).resolves.toEqual(order)
  expect(api.get).toHaveBeenCalledExactlyOnceWith(
    '/api/membership/orders/MEMBER-9-return'
  )
})

describe('membership order resolution transport', () => {
  it('addresses the encoded trade number and keeps resolution facts in the body', async () => {
    vi.mocked(api.post).mockResolvedValue({ data: { success: true } })
    await resolveMembershipOrder({
      trade_no: 'MEMBER-9-reference/with space',
      action: 'dismiss',
      reason: 'Additional receipt handled separately',
      operation_id: 'resolution-1',
    })
    expect(api.post).toHaveBeenCalledExactlyOnceWith(
      '/api/membership/admin/orders/MEMBER-9-reference%2Fwith%20space/resolve',
      {
        action: 'dismiss',
        reason: 'Additional receipt handled separately',
        operation_id: 'resolution-1',
      }
    )
  })

  it('rejects a failed resolution instead of reporting the order as handled', async () => {
    vi.mocked(api.post).mockResolvedValue({
      data: { success: false, message: 'Membership order conflict' },
    })
    await expect(
      resolveMembershipOrder({
        trade_no: 'MEMBER-9-stale',
        action: 'grant',
        reason: 'Verified receipt',
        operation_id: 'resolution-2',
      })
    ).rejects.toThrow('Membership order conflict')
  })
})

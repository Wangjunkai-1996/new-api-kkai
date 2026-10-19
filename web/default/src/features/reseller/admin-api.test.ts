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
  getManagedResellers,
  getManagedWithdrawals,
  provisionDomain,
  reviewApplication,
  reviewWithdrawal,
  updateTenantStatus,
} from './admin-api'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), patch: vi.fn() },
}))
afterEach(() => vi.clearAllMocks())

describe('platform reseller administration API contracts', () => {
  test('lists applications and their provisioned tenants from one response', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: {
        success: true,
        data: { applications: [{ id: 3 }], tenants: [{ id: 47, domains: [] }] },
      },
    })
    await expect(getManagedResellers()).resolves.toEqual({
      applications: [{ id: 3 }],
      tenants: [{ id: 47, domains: [] }],
    })
    expect(api.get).toHaveBeenCalledExactlyOnceWith('/api/admin/resellers')
  })
  test('uses application IDs for review and tenant IDs for provisioning', async () => {
    vi.mocked(api.patch).mockResolvedValue({ data: { success: true } })
    vi.mocked(api.post).mockResolvedValue({ data: { success: true } })
    await reviewApplication(3, 'approved', '')
    await provisionDomain(47, 'pilot.example.com')
    await updateTenantStatus(47, 'active')
    expect(api.patch).toHaveBeenNthCalledWith(
      1,
      '/api/admin/resellers/applications/3',
      { status: 'approved', reason: '' }
    )
    expect(api.post).toHaveBeenCalledExactlyOnceWith(
      '/api/admin/resellers/47/domain',
      { hostname: 'pilot.example.com' }
    )
    expect(api.patch).toHaveBeenNthCalledWith(
      2,
      '/api/admin/resellers/47/status',
      { status: 'active' }
    )
  })
  test('preserves the total and sends the requested withdrawal page', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: { success: true, data: { items: [{ id: 11 }], total: 41 } },
    })
    await expect(getManagedWithdrawals(2, 20)).resolves.toEqual({
      items: [{ id: 11 }],
      total: 41,
    })
    expect(api.get).toHaveBeenCalledExactlyOnceWith(
      '/api/admin/resellers/withdrawals',
      { params: { p: 2, page_size: 20 } }
    )
  })
  test('records confirmed payout reviews against the withdrawal ID', async () => {
    vi.mocked(api.patch).mockResolvedValue({ data: { success: true } })
    await reviewWithdrawal(11, {
      status: 'paid',
      payment_ref: 'alipay-2026-11',
      note: 'Matched transfer',
    })
    expect(api.patch).toHaveBeenCalledExactlyOnceWith(
      '/api/admin/resellers/withdrawals/11',
      {
        status: 'paid',
        payment_ref: 'alipay-2026-11',
        note: 'Matched transfer',
      }
    )
  })
})

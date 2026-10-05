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
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/auth-session', () => ({
  applyAuthRotation: vi.fn(),
  clearAuthentication: vi.fn(),
  getFreshAuthHeaders: vi.fn(),
  refreshAuthentication: vi.fn(),
}))

describe('credit currency attached to an open document', () => {
  beforeEach(() => {
    vi.resetModules()
    window.localStorage.clear()
  })

  it('does not adopt a new currency from another tab or background status refresh', async () => {
    const { api, bindPageCreditEpoch } = await import('./http-client')
    bindPageCreditEpoch('legacy_075')
    window.localStorage.setItem(
      'status',
      JSON.stringify({ credit_epoch: 'usd_credit_v1:new' })
    )
    bindPageCreditEpoch('usd_credit_v1:new')
    const headers: unknown[] = []
    api.defaults.adapter = async (config) => {
      headers.push(config.headers.get('X-KKAI-Credit-Epoch'))
      return { status: 200, statusText: 'OK', headers: {}, config, data: {} }
    }
    await api.post('/api/user/pay', { amount: 10 })
    expect(headers).toEqual(['legacy_075'])
  })

  it('binds fresh documents to their first network currency and never trusts cached status alone', async () => {
    window.localStorage.setItem(
      'status',
      JSON.stringify({ credit_epoch: 'legacy_075' })
    )
    const { api, bindPageCreditEpoch } = await import('./http-client')
    const headers: unknown[] = []
    api.defaults.adapter = async (config) => {
      headers.push(config.headers.get('X-KKAI-Credit-Epoch'))
      return { status: 200, statusText: 'OK', headers: {}, config, data: {} }
    }
    await api.post('/api/user/pay', { amount: 10 })
    bindPageCreditEpoch('usd_credit_v1:new')
    await api.post('/api/user/pay', { amount: 10 })
    expect(headers).toEqual([undefined, 'usd_credit_v1:new'])
  })
})

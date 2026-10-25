/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { getTopupInfo, getWalletBalance } from './api'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

beforeEach(() => vi.resetAllMocks())

it('bounds the wallet configuration request so initial loading can recover', async () => {
  vi.mocked(api.get).mockResolvedValue({ data: { success: true } })

  await getTopupInfo()

  expect(api.get).toHaveBeenCalledWith('/api/user/topup/info', {
    timeout: 20_000,
    signal: undefined,
    disableDuplicate: true,
  })
})

it.each([
  ['configuration', getTopupInfo, '/api/user/topup/info'],
  ['balance', getWalletBalance, '/api/user/self'],
] as const)(
  'keeps the %s query abort signal independent from other GET callers',
  async (_name, read, url) => {
    const controller = new AbortController()
    const data = { success: true, data: { quota: 500_000 } }
    vi.mocked(api.get).mockResolvedValue({ data })
    await expect(read(controller.signal)).resolves.toEqual(data)
    expect(api.get).toHaveBeenCalledExactlyOnceWith(
      url,
      expect.objectContaining({
        signal: controller.signal,
        timeout: 20_000,
        disableDuplicate: true,
      })
    )
  }
)

it('surfaces a failed balance read without retrying or synthesizing a zero balance', async () => {
  const error = new Error('balance unavailable')
  vi.mocked(api.get).mockRejectedValue(error)
  await expect(getWalletBalance()).rejects.toBe(error)
  expect(api.get).toHaveBeenCalledTimes(1)
})

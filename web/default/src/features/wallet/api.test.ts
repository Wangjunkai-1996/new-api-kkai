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

import { getTopupInfo } from './api'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

beforeEach(() => vi.resetAllMocks())

it('bounds the wallet configuration request so initial loading can recover', async () => {
  vi.mocked(api.get).mockResolvedValue({ data: { success: true } })

  await getTopupInfo()

  expect(api.get).toHaveBeenCalledWith('/api/user/topup/info', {
    timeout: 20_000,
  })
})

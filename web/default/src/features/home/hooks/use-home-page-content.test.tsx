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
import { renderHook, waitFor } from '@testing-library/react'
import { toast } from 'sonner'
import { describe, expect, test, vi } from 'vitest'

import { getHomePageContent } from '../api'
import { useHomePageContent } from './use-home-page-content'

vi.mock('../api', () => ({ getHomePageContent: vi.fn() }))
vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

describe('home content without browser storage', () => {
  test.each([
    { method: 'getItem' as const, data: 'Fresh home content' },
    { method: 'setItem' as const, data: 'Fresh home content' },
    { method: 'removeItem' as const, data: '' },
  ])(
    'completes the API result when $method is rejected',
    async ({ method, data }) => {
      vi.spyOn(Storage.prototype, method).mockImplementation(() => {
        throw new DOMException('Storage blocked', 'SecurityError')
      })
      vi.mocked(getHomePageContent).mockResolvedValue({ success: true, data })
      const { result } = renderHook(() => useHomePageContent())

      await waitFor(() => expect(result.current.isLoaded).toBe(true))
      expect(getHomePageContent).toHaveBeenCalledOnce()
      expect(result.current.content).toBe(data)
      expect(toast.error).not.toHaveBeenCalled()
    }
  )
})

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
import { describe, expect, it } from 'vitest'

import { requireInvitationData, requireInvitationPage } from './result'

describe('invitation API response validation', () => {
  it('rejects a successful response with null data', () => {
    expect(() =>
      requireInvitationData(
        { success: true, data: null },
        'Failed to load invitation data'
      )
    ).toThrow('Failed to load invitation data')
  })

  it.each([
    { items: null, total: 0, page: 1, pageSize: 10 },
    { items: [null], total: 1, page: 1, pageSize: 10 },
    { items: [], total: -1, page: 1, pageSize: 10 },
    { items: [], total: 0, page: 0, pageSize: 10 },
    { items: [], total: 0, page: 1, pageSize: 0 },
  ])('rejects malformed page data %#', (data) => {
    expect(() =>
      requireInvitationPage(
        { success: true, data: data as never },
        'Failed to load invitation page'
      )
    ).toThrow('Failed to load invitation page')
  })

  it('accepts a valid page response', () => {
    const data = {
      items: [{ id: 1 }],
      total: 1,
      page: 1,
      pageSize: 10,
    }

    expect(requireInvitationPage({ success: true, data }, 'failed')).toBe(data)
  })

  it('applies a caller-provided item contract', () => {
    expect(() =>
      requireInvitationPage(
        {
          success: true,
          data: { items: [{}], total: 1, page: 1, pageSize: 10 },
        },
        'Failed to load invitation page',
        (value): value is { id: number } =>
          typeof value === 'object' &&
          value !== null &&
          typeof (value as { id?: unknown }).id === 'number'
      )
    ).toThrow('Failed to load invitation page')
  })
})

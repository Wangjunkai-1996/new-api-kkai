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

import { describe, expect, test } from 'vitest'

import { formatGroupCacheHitRate } from './format'
import type { GroupCacheStats } from './types'

describe('group cache hit rate formatting', () => {
  test.each([
    { hitRate: 92.84, expected: '92.8%' },
    { hitRate: 0, expected: '0.0%' },
    { hitRate: 100, expected: '100.0%' },
  ])('formats $hitRate with exactly one decimal place', (testCase) => {
    expect(
      formatGroupCacheHitRate({
        status: 'ok',
        sample_count: 20,
        request_hit_rate: testCase.hitRate,
      })
    ).toBe(testCase.expected)
  })

  test.each([
    { status: 'empty', request_hit_rate: null },
    { status: 'unavailable', request_hit_rate: null },
    { status: 'ok', request_hit_rate: null },
    { status: 'ok', request_hit_rate: Number.NaN },
  ] satisfies Array<Pick<GroupCacheStats, 'status' | 'request_hit_rate'>>)(
    'returns a placeholder for $status with rate $request_hit_rate',
    (testCase) => {
      expect(
        formatGroupCacheHitRate({
          ...testCase,
          sample_count: 0,
        })
      ).toBe('-')
    }
  )
})

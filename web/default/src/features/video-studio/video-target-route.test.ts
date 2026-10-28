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
import { describe, expect, test, vi } from 'vitest'

import { videoCreateSearchSchema } from '@/routes/_authenticated/video-studio/create'
import { videoLibrarySearchSchema } from '@/routes/_authenticated/video-studio/library'

vi.mock('@/features/video-studio', () => ({
  VideoCreatePage: () => null,
  VideoLibraryPage: () => null,
}))

describe('video target route search', () => {
  test.each([videoCreateSearchSchema, videoLibrarySearchSchema])(
    'accepts task identity and known generation identity as separate values',
    (schema) => {
      expect(
        schema.parse({ task: 'task_abc-123', generation: '17' })
      ).toMatchObject({ task: 'task_abc-123', generation: 17 })
    }
  )
  test.each([
    0,
    -1,
    1.5,
    Number.MAX_SAFE_INTEGER + 1,
    'task_17',
    true,
    ['17'],
    null,
  ])('discards an invalid numeric generation target: %s', (generation) => {
    expect(
      videoCreateSearchSchema.parse({ generation }).generation
    ).toBeUndefined()
    expect(
      videoLibrarySearchSchema.parse({ generation }).generation
    ).toBeUndefined()
  })
  test('does not reinterpret malformed task IDs as numeric generation IDs', () => {
    expect(videoLibrarySearchSchema.parse({ task: '17' })).toEqual({
      task: undefined,
    })
    expect(
      videoCreateSearchSchema.parse({ sample: '7', task: 'task_safe' })
    ).toEqual({ sample: 7, task: 'task_safe' })
  })
})

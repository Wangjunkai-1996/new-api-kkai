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

import type { VideoGeneration } from './types'
import { acceptVideoLibraryOrder } from './video-library-order'

const generation = (id: number, status: VideoGeneration['status'] = 'ready') =>
  ({ id, status }) as VideoGeneration

describe('accepted library order', () => {
  test('holds new head rows until refresh while pagination appends older rows', () => {
    const initial = acceptVideoLibraryOrder(
      { status: '', ids: [], pageCount: 0, initialized: false },
      [generation(3), generation(2)],
      [generation(3), generation(2)],
      1,
      '',
      true
    )
    const refreshedHead = acceptVideoLibraryOrder(
      initial,
      [generation(4), generation(3), generation(2)],
      [generation(3), generation(2)],
      1,
      '',
      true
    )
    expect(refreshedHead.ids).toEqual([3, 2])
    const nextPage = acceptVideoLibraryOrder(
      refreshedHead,
      [generation(4), generation(3), generation(2), generation(1)],
      [generation(3), generation(2), generation(1)],
      2,
      '',
      true
    )
    expect(nextPage.ids).toEqual([3, 2, 1])
  })

  test('a new server filter establishes its own order and error does not establish an empty library', () => {
    const waiting = {
      status: '' as const,
      ids: [],
      pageCount: 0,
      initialized: false,
    }
    expect(
      acceptVideoLibraryOrder(waiting, [], [], 0, '', false).initialized
    ).toBe(false)
    expect(
      acceptVideoLibraryOrder(
        { status: '', ids: [3, 2], pageCount: 2, initialized: true },
        [generation(2)],
        [generation(2)],
        1,
        'ready',
        true
      ).ids
    ).toEqual([2])
  })
})

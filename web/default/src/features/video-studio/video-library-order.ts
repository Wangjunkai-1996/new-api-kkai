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
import type { VideoGeneration } from './types'

export type LibraryOrder = {
  status: '' | 'ready'
  ids: number[]
  pageCount: number
  initialized: boolean
}

/** Keep already accepted rows in place; only user refresh or pagination admits new IDs. */
export function acceptVideoLibraryOrder(
  current: LibraryOrder,
  items: VideoGeneration[],
  pageItems: VideoGeneration[],
  pageCount: number,
  status: '' | 'ready',
  successful: boolean
): LibraryOrder {
  if (current.status !== status || (!current.initialized && successful)) {
    return {
      status,
      ids: items.map((item) => item.id),
      pageCount,
      initialized: successful,
    }
  }
  if (pageCount > current.pageCount) {
    const ids = new Set(current.ids)
    for (const item of pageItems) ids.add(item.id)
    return { ...current, ids: [...ids], pageCount }
  }
  return current
}

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
import type { QueryClient } from '@tanstack/react-query'

const privateUserQueryRoot = 'private-user'

export const privateUserQueryKey = <Segments extends readonly unknown[]>(
  userId: number,
  ...segments: Segments
): readonly [typeof privateUserQueryRoot, number, ...Segments] => [
  privateUserQueryRoot,
  userId,
  ...segments,
]

export const createPrivateQueryUserBoundary = (
  queryClient: QueryClient,
  initialUserId?: number | null
): ((nextUserId?: number | null) => void) => {
  let currentUserId = initialUserId ?? null

  return (nextUserId) => {
    const normalizedNextUserId = nextUserId ?? null
    if (currentUserId !== null && currentUserId !== normalizedNextUserId) {
      const previousUserId = currentUserId
      queryClient.removeQueries({
        predicate: (query) =>
          query.queryKey[0] === privateUserQueryRoot &&
          query.queryKey[1] === previousUserId,
      })
    }
    currentUserId = normalizedNextUserId
  }
}

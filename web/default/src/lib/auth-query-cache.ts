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

import { useAuthStore } from '@/stores/auth-store'

export const installAuthQueryCacheBoundary = (
  queryClient: QueryClient
): (() => void) => {
  return useAuthStore.subscribe((state, previousState) => {
    if (
      state.auth.user?.id === previousState.auth.user?.id &&
      state.auth.session?.sid === previousState.auth.session?.sid
    ) {
      return
    }
    // Legacy feature queries also carry private data without a user key.
    // Keep only Host-scoped public configuration across identity changes.
    queryClient.removeQueries({
      predicate: (query) =>
        query.queryKey[0] !== 'site-config' && query.queryKey[0] !== 'status',
    })
    queryClient.getMutationCache().clear()
  })
}

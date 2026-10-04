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
import { useQuery } from '@tanstack/react-query'
import { useCallback } from 'react'

import type { TwoFAStatus } from '@/features/profile/types'
import { AuthOperationError } from '@/lib/secure-verification'
import { useAuthStore } from '@/stores/auth-store'

import { get2FAStatus } from '../api'

// ============================================================================
// Two-FA Hook
// ============================================================================

const DEFAULT_STATUS: TwoFAStatus = {
  enabled: false,
  locked: false,
  backup_codes_remaining: 0,
}

export function useTwoFA(enabled = true) {
  const userId = useAuthStore((state) => state.auth.user?.id)
  const sessionId = useAuthStore((state) => state.auth.session?.sid)
  const query = useQuery({
    queryKey: ['security', 'two-fa', userId, sessionId],
    queryFn: get2FAStatus,
    enabled,
    retry: false,
  })

  const reload = query.refetch
  const refetch = useCallback(async () => {
    await reload()
  }, [reload])

  return {
    status: query.data ?? DEFAULT_STATUS,
    loading: query.isFetching,
    error: query.error
      ? AuthOperationError.from(query.error).message
      : undefined,
    refetch,
  }
}

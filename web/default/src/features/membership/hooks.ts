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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useState } from 'react'

import { useAuthStore } from '@/stores/auth-store'

import { getMembershipCatalog, getMembershipSelf } from './api'

export function useMembership() {
  const userId = useAuthStore((state) => state.auth.user?.id)
  const client = useQueryClient()
  const catalog = useQuery({
    queryKey: ['membership', userId, 'catalog'],
    queryFn: getMembershipCatalog,
    staleTime: 30_000,
  })
  const self = useQuery({
    queryKey: ['membership', userId, 'self'],
    queryFn: getMembershipSelf,
    staleTime: 15_000,
  })
  const refresh = useCallback(async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ['membership', userId] }),
      client.invalidateQueries({ queryKey: ['pricing'] }),
    ])
  }, [client, userId])
  const [clock, setClock] = useState(() => Date.now())
  const serverTime = self.data?.server_time
  const now = serverTime
    ? serverTime + Math.max(0, clock - self.dataUpdatedAt) / 1000
    : clock / 1000
  const expiresAt = self.data?.account?.expires_at
  useEffect(() => {
    if (!expiresAt || !serverTime || self.data?.account?.status !== 'active') {
      return
    }
    const delay = Math.max(
      0,
      expiresAt * 1000 - serverTime * 1000 - (Date.now() - self.dataUpdatedAt)
    )
    if (delay > 2_147_000_000) return
    const timer = window.setTimeout(() => {
      setClock(Date.now())
      void refresh()
    }, delay + 100)
    return () => window.clearTimeout(timer)
  }, [
    expiresAt,
    serverTime,
    self.data?.account?.status,
    self.dataUpdatedAt,
    refresh,
  ])
  return { catalog, self, refresh, now, userId }
}

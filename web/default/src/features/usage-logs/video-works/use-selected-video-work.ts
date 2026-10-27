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
import { useEffect, useRef } from 'react'

import { getServerErrorStatus } from '@/lib/server-error-message'

import { getVideoWorks } from '../api'
import type { TaskLog } from '../types'
import {
  videoWorksErrorInterval,
  videoWorksRefreshInterval,
} from './use-video-works'
import { useVideoWorksIdentity } from './use-video-works-identity'

// A task can leave the active status filter while its details remain open.
// Read only that public task ID, then match its numeric record identity.
export function useSelectedVideoWork(
  snapshot: TaskLog | null,
  currentItems: TaskLog[]
) {
  const identity = useVideoWorksIdentity()
  const client = useQueryClient()
  const current = currentItems.find((item) => item.id === snapshot?.id)
  const recordId = snapshot?.id
  const taskId = snapshot?.task_id
  const queryKey = [
    'video-works',
    ...identity.key,
    'selected',
    recordId,
    taskId,
  ] as const
  const requestKey = JSON.stringify(queryKey)
  const failures = useRef({ key: '', count: 0 })
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (failures.current.key !== requestKey) {
        failures.current = { key: requestKey, count: 0 }
      }
      try {
        const data = await getVideoWorks(
          {
            media_type: 'video',
            task_id: taskId,
            status_group: 'all',
            p: 1,
            page_size: 100,
          },
          identity.isAdminView,
          signal
        )
        if (failures.current.key === requestKey) failures.current.count = 0
        return data.items.find((item) => item.id === recordId) ?? null
      } catch (error) {
        if (!signal.aborted && failures.current.key === requestKey) {
          failures.current.count++
        }
        throw error
      }
    },
    enabled: (state) =>
      Boolean(
        snapshot &&
        !current &&
        identity.ready &&
        identity.active &&
        ![401, 403, 404].includes(getServerErrorStatus(state.state.error) ?? 0)
      ),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: true,
    refetchOnReconnect: true,
    refetchIntervalInBackground: false,
    refetchInterval: (state) => {
      if (!identity.active || current || !snapshot) return false
      if (state.state.error) {
        return videoWorksErrorInterval(
          state.state.error,
          failures.current.count
        )
      }
      return state.state.data
        ? videoWorksRefreshInterval([state.state.data])
        : false
    },
  })
  useEffect(() => {
    if (!identity.active || current || !snapshot) {
      void client.cancelQueries({
        queryKey: JSON.parse(requestKey),
        exact: true,
      })
    }
  }, [client, current, identity.active, requestKey, snapshot])
  const status = getServerErrorStatus(query.error)
  const shouldClose = Boolean(
    snapshot &&
    !current &&
    ([401, 403, 404].includes(status ?? 0) ||
      (query.isSuccess && query.data === null))
  )
  return {
    log: shouldClose ? null : (current ?? query.data ?? snapshot),
    shouldClose,
    error: query.error,
    isLoading: query.isLoading,
  }
}

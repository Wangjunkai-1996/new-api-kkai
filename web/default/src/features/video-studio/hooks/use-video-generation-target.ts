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
import { useState } from 'react'

import { useAuthStore } from '@/stores/auth-store'

import {
  isVideoGenerationTombstoneError,
  type useVideoGenerations,
} from '../queries'
import type { VideoGeneration } from '../types'

type TargetStatus =
  | 'idle'
  | 'loading'
  | 'locating'
  | 'missing'
  | 'unavailable'
  | 'error'
  | 'ready'

type VideoGenerationTargetInput = {
  generationsQuery: Pick<
    ReturnType<typeof useVideoGenerations>,
    | 'items'
    | 'targetGeneration'
    | 'targetError'
    | 'targetUnavailable'
    | 'targetDiscoveryExpired'
    | 'isError'
    | 'isLoading'
    | 'isSuccess'
    | 'retryTargetDiscovery'
  >
  targetTaskId?: string
  targetGenerationId?: number
}

/** Resolves a page-owned query without creating another list or polling owner. */
export function useVideoGenerationTarget(props: VideoGenerationTargetInput) {
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const [selection, setSelection] = useState<{
    userId: number
    id: number | null
  }>({ userId, id: null })
  const query = props.generationsQuery
  const explicitTarget = Boolean(props.targetGenerationId || props.targetTaskId)
  if (
    !explicitTarget &&
    query.isSuccess &&
    (selection.userId !== userId ||
      !query.items.some((item) => item.id === selection.id)) &&
    query.items[0]
  ) {
    setSelection({ userId, id: query.items[0].id })
  }
  let generation: VideoGeneration | null = null
  let status: TargetStatus = 'idle'
  let isStale = false

  if (props.targetGenerationId) {
    const candidate = query.targetGeneration
    const mismatch = Boolean(
      candidate &&
      props.targetTaskId &&
      candidate.task_id !== props.targetTaskId
    )
    if (
      query.targetUnavailable ||
      isVideoGenerationTombstoneError(query.targetError) ||
      mismatch
    ) {
      status = 'unavailable'
    } else if (candidate) {
      generation = candidate
      status = 'ready'
      isStale = Boolean(query.targetError)
    } else {
      status = query.targetError ? 'error' : 'loading'
    }
  } else if (props.targetTaskId) {
    generation =
      query.items.find((item) => item.task_id === props.targetTaskId) ?? null
    if (generation) status = 'ready'
    else if (query.targetDiscoveryExpired) status = 'missing'
    else status = query.isError ? 'error' : 'locating'
    isStale = Boolean(generation && query.isError)
  } else {
    generation =
      query.items.find(
        (item) => selection.userId === userId && item.id === selection.id
      ) ??
      query.items[0] ??
      null
    if (generation) status = 'ready'
    else if (query.isError) status = 'error'
    else if (query.isLoading) status = 'loading'
    isStale = Boolean(generation && query.isError)
  }

  return {
    generation,
    selectedId: generation?.id ?? null,
    selectGeneration: (id: number | null) => setSelection({ userId, id }),
    status,
    isStale,
    explicitTarget,
    retryTargetDiscovery: query.retryTargetDiscovery,
  }
}

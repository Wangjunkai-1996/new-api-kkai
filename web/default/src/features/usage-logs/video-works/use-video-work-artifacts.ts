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
import { useCallback, useEffect, useRef } from 'react'

import { getServerErrorStatus } from '@/lib/server-error-message'

import { getTaskArtifacts } from '../api'
import { shouldLoadTaskArtifacts } from '../lib/task-artifacts'
import type { TaskLog } from '../types'
import { useVideoWorksIdentity } from './use-video-works-identity'

interface MediaRenewal {
  key: string
  used: boolean
  promise?: Promise<boolean>
}

export function useVideoWorkArtifacts(log: TaskLog | null, open: boolean) {
  const identity = useVideoWorksIdentity()
  const client = useQueryClient()
  const blocked = log?.media?.state === 'restricted'
  const enabled = Boolean(
    log &&
    shouldLoadTaskArtifacts(log, open) &&
    !blocked &&
    identity.ready &&
    identity.active
  )
  const taskId = log?.task_id ?? ''
  const recordId = log?.id
  const requireHosted = log?.media?.source === 'studio'
  const queryKey = [
    'video-works',
    ...identity.key,
    'artifacts',
    recordId,
    taskId,
    requireHosted,
  ] as const
  const requestKey = JSON.stringify(queryKey)
  const renewalKey = `${requestKey}:${open}`
  const renewal = useRef<MediaRenewal>({ key: renewalKey, used: false })
  useEffect(() => {
    renewal.current = { key: renewalKey, used: false }
  }, [renewalKey])
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) =>
      getTaskArtifacts(taskId, { recordId, signal, requireHosted }),
    enabled,
    retry: false,
    staleTime: 30_000,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchIntervalInBackground: false,
    refetchInterval: (current) =>
      enabled &&
      !current.state.error &&
      (current.state.data?.hostedVideo?.state === 'processing' ||
        current.state.data?.artifacts.some(
          (artifact) => artifact.poster?.state === 'pending'
        ))
        ? 10_000
        : false,
  })
  const hosted = query.data?.hostedVideo
  const { refetch } = query
  const renewMedia = useCallback((): Promise<boolean> => {
    const attempt = renewal.current
    if (!enabled || attempt.key !== renewalKey) return Promise.resolve(false)
    if (attempt.promise) return attempt.promise
    const status = getServerErrorStatus(query.error)
    if (
      attempt.used ||
      hosted?.state !== 'ready' ||
      !hosted.expires_at ||
      hosted.expires_at * 1000 > Date.now() + 1000 ||
      status === 401 ||
      status === 403
    ) {
      return Promise.resolve(false)
    }
    attempt.used = true
    attempt.promise = refetch({ cancelRefetch: false })
      .then(
        (result) =>
          renewal.current === attempt &&
          !result.isError &&
          result.data?.hostedVideo?.state === 'ready' &&
          (result.data.hostedVideo.expires_at ?? 0) * 1000 > Date.now()
      )
      .finally(() => {
        attempt.promise = undefined
      })
    return attempt.promise
  }, [enabled, hosted, query.error, refetch, renewalKey])
  useEffect(() => {
    if (!enabled || renewal.current.used || !hosted?.expires_at) return
    const remaining = hosted.expires_at * 1000 - Date.now()
    const timer = window.setTimeout(
      () => {
        void renewMedia()
      },
      Math.min(Math.max(0, remaining), 2_147_483_647)
    )
    return () => window.clearTimeout(timer)
  }, [enabled, hosted?.expires_at, renewMedia])
  useEffect(() => {
    if (!open || !identity.active) {
      void client.cancelQueries({
        queryKey: JSON.parse(requestKey),
        exact: true,
      })
    }
  }, [client, identity.active, open, requestKey])

  const status = getServerErrorStatus(query.error)
  const retryFetch = useCallback(
    () => refetch({ cancelRefetch: false }),
    [refetch]
  )
  return {
    data:
      open && !blocked && status !== 401 && status !== 403
        ? query.data
        : undefined,
    isLoading: enabled && query.isLoading,
    isFetching: query.isFetching,
    error: query.error,
    refetch: retryFetch,
    renewMedia,
  }
}

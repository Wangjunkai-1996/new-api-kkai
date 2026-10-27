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
import { getRouteApi } from '@tanstack/react-router'
import { isAxiosError } from 'axios'
import { useEffect, useRef } from 'react'

import { getServerErrorStatus } from '@/lib/server-error-message'

import { getVideoWorks, prepareVideoPosters } from '../api'
import type {
  GetTaskLogsParams,
  TaskLog,
  VideoWorksStatusCounts,
  VideoWorksPage,
} from '../types'
import { useVideoWorksIdentity } from './use-video-works-identity'

const route = getRouteApi('/_authenticated/usage-logs/$section')
const activeStatuses = new Set([
  'NOT_START',
  'SUBMITTED',
  'QUEUED',
  'IN_PROGRESS',
])
const pendingBilling = new Set([
  'pending',
  'reserved',
  'dispatching',
  'accepted',
  'ambiguous',
])
const emptyCounts: VideoWorksStatusCounts = {
  all: 0,
  success: 0,
  processing: 0,
  failed: 0,
  unknown: 0,
}

export function videoWorksRefreshInterval(items: TaskLog[]): number | false {
  if (
    items.some(
      (item) =>
        activeStatuses.has(item.status) ||
        item.media?.state === 'processing' ||
        item.poster?.state === 'pending'
    )
  ) {
    return 10_000
  }
  if (items.some((item) => pendingBilling.has(item.billing?.state ?? ''))) {
    return 15_000
  }
  return false
}

export function videoWorksErrorInterval(
  error: unknown,
  failureCount: number
): number | false {
  const status = getServerErrorStatus(error)
  if (status === 400 || status === 401 || status === 403 || status === 404) {
    return false
  }
  const delay = Math.min(Math.max(failureCount, 1) * 20_000, 60_000)
  const retryAfter = isAxiosError(error)
    ? error.response?.headers['retry-after']
    : undefined
  if (
    status === 429 &&
    (typeof retryAfter === 'string' || typeof retryAfter === 'number')
  ) {
    const seconds = Number(retryAfter)
    const retryDelay = Number.isFinite(seconds)
      ? seconds * 1000
      : Date.parse(String(retryAfter)) - Date.now()
    if (Number.isFinite(retryDelay)) return Math.max(delay, retryDelay)
  }
  return delay
}

export function useVideoWorks() {
  const search = route.useSearch()
  const identity = useVideoWorksIdentity()
  const client = useQueryClient()
  const page = search.page ?? 1
  const pageSize = search.pageSize ?? 20
  const params: GetTaskLogsParams = {
    p: page,
    page_size: pageSize,
    media_type: 'video',
    q: search.q || undefined,
    status_group: search.status_group ?? 'all',
    task_id: search.filter || undefined,
    channel_id: identity.isAdminView ? search.channel || undefined : undefined,
    start_timestamp:
      search.startTime === undefined
        ? undefined
        : Math.floor(search.startTime / 1000),
    end_timestamp:
      search.endTime === undefined
        ? undefined
        : Math.floor(search.endTime / 1000),
  }
  const queryKey = ['video-works', ...identity.key, 'list', params] as const
  // Query-local failures reset when filters or identity change, and after success.
  const requestKey = JSON.stringify(queryKey)
  const failures = useRef({ key: '', count: 0 })
  const renewedPosters = useRef({ key: '', ids: new Set<number>() })
  const preparedPosters = useRef({ key: '', ids: new Set<number>() })
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (failures.current.key !== requestKey) {
        failures.current = { key: requestKey, count: 0 }
      }
      try {
        const data = await getVideoWorks(params, identity.isAdminView, signal)
        if (failures.current.key === requestKey) failures.current.count = 0
        return data
      } catch (error) {
        if (!signal.aborted && failures.current.key === requestKey) {
          failures.current.count++
        }
        throw error
      }
    },
    enabled: (current) =>
      identity.ready &&
      identity.active &&
      ![401, 403].includes(getServerErrorStatus(current.state.error) ?? 0),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: true,
    refetchOnReconnect: true,
    refetchIntervalInBackground: false,
    refetchInterval: (current) => {
      if (!identity.active) return false
      if (current.state.error) {
        return videoWorksErrorInterval(
          current.state.error,
          failures.current.count
        )
      }
      return videoWorksRefreshInterval(current.state.data?.items ?? [])
    },
  })
  const status = getServerErrorStatus(query.error)
  const data = status === 401 || status === 403 ? undefined : query.data
  useEffect(() => {
    if (!identity.active) {
      void client.cancelQueries({
        queryKey: JSON.parse(requestKey),
        exact: true,
      })
    }
  }, [client, identity.active, requestKey])
  const posterIds = JSON.stringify(
    (data?.items ?? [])
      .filter(
        (item) =>
          item.status === 'SUCCESS' &&
          !item.media &&
          !item.result_discarded &&
          !item.poster
      )
      .map((item) => item.id)
  )
  useEffect(() => {
    if (!identity.ready || !identity.active || query.error) return
    if (preparedPosters.current.key !== requestKey) {
      preparedPosters.current = { key: requestKey, ids: new Set() }
    }
    const attempt = preparedPosters.current
    const ids = (JSON.parse(posterIds) as number[]).filter(
      (id) => !attempt.ids.has(id)
    )
    if (!ids.length) return
    ids.forEach((id) => attempt.ids.add(id))
    const controller = new AbortController()
    const batches = Array.from({ length: Math.ceil(ids.length / 20) }, (_, i) =>
      ids.slice(i * 20, (i + 1) * 20)
    )
    void Promise.allSettled(
      batches.map((batch) =>
        prepareVideoPosters(batch, identity.isAdminView, controller.signal)
      )
    )
      .then((results) => {
        if (controller.signal.aborted || preparedPosters.current !== attempt) {
          return
        }
        const posters = results.flatMap((result) =>
          result.status === 'fulfilled' ? result.value : []
        )
        const byId = new Map(posters.map((item) => [item.id, item.poster]))
        client.setQueryData<VideoWorksPage>(
          JSON.parse(requestKey),
          (current) =>
            current && {
              ...current,
              items: current.items.map((item) =>
                byId.has(item.id)
                  ? { ...item, poster: byId.get(item.id) }
                  : item
              ),
            }
        )
      })
      // Poster preparation is optional: old backends and failed cache work must
      // not hide video records or trigger an automatic POST retry storm.
      .catch(() => {})
    return () => controller.abort()
  }, [
    client,
    identity.active,
    identity.isAdminView,
    identity.ready,
    posterIds,
    query.error,
    requestKey,
  ])
  return {
    items: data?.items ?? [],
    total: data?.total ?? 0,
    statusCounts: data?.status_counts ?? emptyCounts,
    mediaWarning: data?.media_warning,
    isLoading: query.isLoading || (!identity.ready && !identity.error),
    isFetching: query.isFetching,
    error: identity.error ?? query.error,
    refetch: async () => {
      if (identity.error) return identity.retryIdentity()
      return query.refetch({ cancelRefetch: false })
    },
    page,
    pageSize,
    search,
    identityKey: identity.identityKey,
    renewPoster: async (log: TaskLog): Promise<boolean> => {
      if (renewedPosters.current.key !== requestKey) {
        renewedPosters.current = { key: requestKey, ids: new Set() }
      }
      const expiry = log.media?.poster_expires_at ?? log.poster?.expires_at
      if (
        !identity.ready ||
        !identity.active ||
        !expiry ||
        expiry * 1000 > Date.now() + 1000 ||
        renewedPosters.current.ids.has(log.id)
      ) {
        return false
      }
      renewedPosters.current.ids.add(log.id)
      const result = await query.refetch({ cancelRefetch: false })
      const updated = result.data?.items.find((item) => item.id === log.id)
      return (
        !result.isError &&
        Boolean(updated?.media?.poster_url ?? updated?.poster?.url) &&
        (updated?.media?.poster_expires_at ??
          updated?.poster?.expires_at ??
          0) *
          1000 >
          Date.now()
      )
    },
  }
}

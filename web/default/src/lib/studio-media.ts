import { useQuery } from '@tanstack/react-query'
import { isAxiosError, isCancel } from 'axios'
import { useState } from 'react'

import { useAuthStore } from '@/stores/auth-store'

import { api } from './api'
import { privateUserQueryKey } from './private-query-cache'

export type StudioMediaDenials = Readonly<Record<string, 403 | 404>>

type StudioMediaResponse = {
  success: boolean
  data?: {
    urls: Record<string, string>
    expires_at: number
  }
  message?: string
}

const isProtectedStudioPath = (value: string): boolean =>
  value.startsWith('/api/image-studio/assets/') ||
  value.startsWith('/api/video-studio/assets/')

export function useStudioMediaUrls(paths: readonly (string | undefined)[]) {
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const uniquePaths = [
    ...new Set(
      paths.filter(
        (path): path is string =>
          path !== undefined && isProtectedStudioPath(path)
      )
    ),
  ].sort()
  const query = useQuery({
    queryKey: privateUserQueryKey(userId, 'studio-media-urls', ...uniquePaths),
    queryFn: async ({ signal }) => {
      const batches: string[][] = []
      for (let index = 0; index < uniquePaths.length; index += 100) {
        batches.push(uniquePaths.slice(index, index + 100))
      }
      const signBatch = async (batch: string[]) => {
        const response = await api.post<StudioMediaResponse>(
          '/api/studio/media-urls',
          { paths: batch },
          { skipErrorHandler: true, disableDuplicate: true, signal }
        )
        if (!response.data.success || !response.data.data) {
          throw new Error(
            response.data.message || 'Failed to sign studio media'
          )
        }
        return response.data.data
      }
      const deniedPaths: Record<string, 403 | 404> = {}
      const unavailablePaths: string[] = []
      let unavailableError: unknown
      const signed = await Promise.all(
        batches.map(async (batch) => {
          try {
            return await signBatch(batch)
          } catch (error) {
            signal.throwIfAborted()
            if (
              isCancel(error) ||
              (isAxiosError(error) && error.response?.status === 401)
            ) {
              throw error
            }
            const settled = await Promise.allSettled(
              batch.map((path) => signBatch([path]))
            )
            signal.throwIfAborted()
            const available: NonNullable<StudioMediaResponse['data']>[] = []
            for (const [index, result] of settled.entries()) {
              if (result.status === 'fulfilled') {
                available.push(result.value)
                continue
              }
              const status = isAxiosError(result.reason)
                ? result.reason.response?.status
                : undefined
              if (isCancel(result.reason) || status === 401) throw result.reason
              if (status === 403 || status === 404) {
                deniedPaths[batch[index]] = status
              } else {
                unavailablePaths.push(batch[index])
                unavailableError = result.reason
              }
            }
            return {
              urls: Object.assign(
                {},
                ...available.map((item) => item.urls)
              ) as Record<string, string>,
              expires_at: Math.min(...available.map((item) => item.expires_at)),
            }
          }
        })
      )
      signal.throwIfAborted()
      const available = signed.filter((item) =>
        Number.isFinite(item.expires_at)
      )
      if (
        available.length === 0 &&
        Object.keys(deniedPaths).length === 0 &&
        unavailablePaths.length > 0
      ) {
        throw unavailableError
      }
      return {
        deniedPaths,
        unavailablePaths,
        urls: Object.assign(
          {},
          ...available.map((item) => item.urls)
        ) as Record<string, string>,
        expires_at:
          available.length > 0
            ? Math.min(...available.map((item) => item.expires_at))
            : Math.floor(Date.now() / 1000) + 60,
      }
    },
    enabled: userId > 0 && uniquePaths.length > 0,
    staleTime: (query) =>
      Math.max(
        0,
        (query.state.data?.expires_at ?? 0) * 1000 - Date.now() - 30_000
      ),
    refetchInterval: (query) => {
      const expiresAt = query.state.data?.expires_at
      if (!expiresAt) return false
      return Math.max(30_000, expiresAt * 1000 - Date.now() - 30_000)
    },
    retry: false,
    meta: { errorToast: false },
  })
  const [denial, setDenial] = useState<{
    userId: number
    dataUpdatedAt: number
  }>()
  const rejectedIdentity =
    isAxiosError(query.error) && query.error.response?.status === 401
  if (
    rejectedIdentity &&
    (denial?.userId !== userId || denial.dataUpdatedAt !== query.dataUpdatedAt)
  ) {
    setDenial({ userId, dataUpdatedAt: query.dataUpdatedAt })
  }
  const identityDenied =
    userId <= 0 ||
    rejectedIdentity ||
    (denial?.userId === userId && query.dataUpdatedAt <= denial.dataUpdatedAt)
  // React Query retains previous data on a failed refresh; auth failure must not.
  return {
    ...query,
    data: identityDenied ? undefined : query.data,
    identityDenied,
  }
}

export function signedStudioMediaUrl(
  url: string | undefined,
  signedUrls: Record<string, string> | undefined,
  deniedPaths?: StudioMediaDenials,
  identityDenied = false
): string | undefined {
  if (identityDenied || !url || deniedPaths?.[url]) return undefined
  if (!isProtectedStudioPath(url)) return url
  return signedUrls?.[url]
}

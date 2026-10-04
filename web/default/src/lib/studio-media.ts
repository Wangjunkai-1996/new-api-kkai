import { useQuery } from '@tanstack/react-query'

import { useAuthStore } from '@/stores/auth-store'

import { api } from './api'

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
  return useQuery({
    queryKey: ['studio-media-urls', userId, ...uniquePaths],
    queryFn: async () => {
      const batches: string[][] = []
      for (let index = 0; index < uniquePaths.length; index += 100) {
        batches.push(uniquePaths.slice(index, index + 100))
      }
      const signBatch = async (batch: string[]) => {
        const response = await api.post<StudioMediaResponse>(
          '/api/studio/media-urls',
          { paths: batch },
          { skipErrorHandler: true, disableDuplicate: true }
        )
        if (!response.data.success || !response.data.data) {
          throw new Error(
            response.data.message || 'Failed to sign studio media'
          )
        }
        return response.data.data
      }
      const signed = await Promise.all(
        batches.map(async (batch) => {
          try {
            return await signBatch(batch)
          } catch {
            const settled = await Promise.allSettled(
              batch.map((path) => signBatch([path]))
            )
            const available = settled.flatMap((result) =>
              result.status === 'fulfilled' ? [result.value] : []
            )
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
      const available = signed.filter((item) =>
        Number.isFinite(item.expires_at)
      )
      return {
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
}

export function signedStudioMediaUrl(
  url: string | undefined,
  signedUrls: Record<string, string> | undefined
): string | undefined {
  if (!url) return undefined
  if (!isProtectedStudioPath(url)) return url
  return signedUrls?.[url]
}

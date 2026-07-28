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
import {
  type MutationFilters,
  type QueryClient,
  useInfiniteQuery,
  useIsMutating,
  useMutation,
  useQuery,
  useQueryClient,
  useQueries,
} from '@tanstack/react-query'

import { privateUserQueryKey } from '@/lib/private-query-cache'
import { useAuthStore } from '@/stores/auth-store'

import {
  createAdminVideoModel,
  createAdminVideoSample,
  createVideoGeneration,
  createVideoToken,
  deleteAdminVideoModel,
  deleteAdminVideoSample,
  deleteVideoGeneration,
  getAdminVideoModels,
  getAdminVideoSamples,
  getVideoAsset,
  getVideoGeneration,
  getVideoGenerations,
  getVideoModels,
  getVideoSample,
  getVideoSamples,
  getVideoTokenCapability,
  quoteVideoGeneration,
  updateAdminVideoModel,
  updateAdminVideoSample,
} from './api'
import type {
  CreateVideoRequest,
  VideoGenerationFilters,
  VideoModelProfileInput,
  VideoQuoteRequest,
  VideoSampleFilters,
  VideoSampleInput,
  VideoTokenCreateResult,
} from './types'
import { getVideoTaskPollInterval } from './video-domain'
import { shouldRetryVideoReferenceHydration } from './video-reference-hydration'

export const videoStudioQueryKeys = {
  all: ['video-studio'] as const,
  privateAll: (userId: number) => privateUserQueryKey(userId, 'video-studio'),
  models: () => ['video-studio', 'models'] as const,
  samples: (filters: Omit<VideoSampleFilters, 'cursor'>) =>
    ['video-studio', 'samples', filters] as const,
  sample: (id: number) => ['video-studio', 'sample', id] as const,
  tokens: (userId: number) =>
    privateUserQueryKey(userId, 'video-studio', 'token'),
  token: (userId: number, model: string) =>
    [...videoStudioQueryKeys.tokens(userId), model] as const,
  quote: (userId: number, request: VideoQuoteRequest) =>
    privateUserQueryKey(userId, 'video-studio', 'quote', request),
  generations: (
    userId: number,
    filters: Omit<VideoGenerationFilters, 'cursor'>
  ) => privateUserQueryKey(userId, 'video-studio', 'generations', filters),
  generation: (userId: number, id: number) =>
    privateUserQueryKey(userId, 'video-studio', 'generation', id),
  asset: (userId: number, id: number) =>
    privateUserQueryKey(userId, 'video-studio', 'asset', id),
  upload: (userId: number, admin: boolean, id: number) =>
    privateUserQueryKey(
      userId,
      'video-studio',
      'upload',
      admin ? 'admin' : 'user',
      id
    ),
  adminModels: (userId: number) =>
    privateUserQueryKey(userId, 'video-studio', 'admin', 'models'),
  adminSamples: (userId: number) =>
    privateUserQueryKey(userId, 'video-studio', 'admin', 'samples'),
}

export const videoStudioMutationKeys = {
  createToken: ['video-studio', 'create-token'] as const,
}

const useVideoStudioUserId = (): number =>
  useAuthStore((state) => state.auth.user?.id ?? 0)

export const useVideoModels = () =>
  useQuery({
    queryKey: videoStudioQueryKeys.models(),
    queryFn: getVideoModels,
    staleTime: 60_000,
  })

export const useVideoSamples = (
  filters: Omit<VideoSampleFilters, 'cursor'> = {}
) =>
  useInfiniteQuery({
    queryKey: videoStudioQueryKeys.samples(filters),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      getVideoSamples({ ...filters, cursor: pageParam, limit: 24 }),
    getNextPageParam: (lastPage) => lastPage.next_cursor,
  })

export const useVideoSample = (id?: number) =>
  useQuery({
    queryKey: videoStudioQueryKeys.sample(id ?? 0),
    queryFn: () => getVideoSample(id ?? 0),
    enabled: Boolean(id),
  })

export const useVideoTokenCapability = (model?: string) => {
  const userId = useVideoStudioUserId()
  return useQuery({
    queryKey: videoStudioQueryKeys.token(userId, model ?? ''),
    queryFn: () => {
      if (!model) throw new Error('Video model is unavailable')
      return getVideoTokenCapability(model)
    },
    enabled: userId > 0 && Boolean(model),
    retry: false,
    staleTime: 30_000,
  })
}

export type CreateVideoTokenMutationVariables = Readonly<{
  userId: number
  model: string
}>

export type CreateVideoTokenMutationContext = Readonly<{
  userId: number
  model: string
}>

export const captureVideoTokenMutationContext = (
  variables: Pick<CreateVideoTokenMutationVariables, 'userId' | 'model'>
): CreateVideoTokenMutationContext =>
  Object.freeze({ userId: variables.userId, model: variables.model })

const isVideoTokenCreateMutationVariables = (
  value: unknown
): value is CreateVideoTokenMutationVariables => {
  if (!value || typeof value !== 'object') return false
  const variables = value as Partial<CreateVideoTokenMutationVariables>
  return (
    Number.isInteger(variables.userId) && typeof variables.model === 'string'
  )
}

export const videoTokenCreateMutationFilters = (
  userId: number,
  model?: string
): MutationFilters => ({
  mutationKey: videoStudioMutationKeys.createToken,
  predicate: (mutation) => {
    const variables = mutation.state.variables
    return (
      isVideoTokenCreateMutationVariables(variables) &&
      variables.userId === userId &&
      variables.model === model
    )
  },
})

export const useIsCreatingVideoToken = (
  userId: number,
  model?: string
): boolean => useIsMutating(videoTokenCreateMutationFilters(userId, model)) > 0

export const canStartVideoTokenCreate = (
  queryClient: QueryClient,
  userId: number,
  model: string
): boolean =>
  queryClient.isMutating(videoTokenCreateMutationFilters(userId, model)) === 0

export const applyVideoTokenCreateSuccess = async (
  queryClient: QueryClient,
  capability: VideoTokenCreateResult,
  context: CreateVideoTokenMutationContext | undefined,
  activeUserId: number
): Promise<boolean> => {
  if (!context || activeUserId !== context.userId) return false

  const tokenQueries = videoStudioQueryKeys.tokens(context.userId)
  queryClient.removeQueries({
    queryKey: tokenQueries,
    type: 'inactive',
  })
  queryClient.setQueryData(
    videoStudioQueryKeys.token(context.userId, context.model),
    capability
  )
  await queryClient.invalidateQueries({
    queryKey: tokenQueries,
    refetchType: 'active',
    predicate: (query) => query.queryKey.at(-1) !== context.model,
  })
  return true
}

export const useCreateVideoToken = () => {
  const queryClient = useQueryClient()
  return useMutation({
    mutationKey: videoStudioMutationKeys.createToken,
    mutationFn: (variables: CreateVideoTokenMutationVariables) =>
      createVideoToken(variables.model),
    onMutate: captureVideoTokenMutationContext,
    onSuccess: (capability, _variables, context) =>
      applyVideoTokenCreateSuccess(
        queryClient,
        capability,
        context,
        useAuthStore.getState().auth.user?.id ?? 0
      ),
  })
}

export const useVideoQuote = (
  request: VideoQuoteRequest | null,
  enabled: boolean
) => {
  const userId = useVideoStudioUserId()
  return useQuery({
    queryKey: videoStudioQueryKeys.quote(
      userId,
      request ?? {
        token_id: 0,
        model: '',
        mode: 'text_to_video',
        prompt: '',
        reference_assets: [],
        parameters: {},
      }
    ),
    queryFn: () => {
      if (!request) throw new Error('Video quote request is unavailable')
      return quoteVideoGeneration(request)
    },
    enabled: userId > 0 && enabled && request !== null,
    retry: false,
    staleTime: 15_000,
  })
}

export const useCreateVideoGeneration = () => {
  const queryClient = useQueryClient()
  const userId = useVideoStudioUserId()
  return useMutation({
    mutationFn: (input: {
      request: CreateVideoRequest
      idempotencyKey: string
    }) => createVideoGeneration(input.request, input.idempotencyKey),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.privateAll(userId),
      })
    },
  })
}

export const useVideoGenerations = (
  filters: Omit<VideoGenerationFilters, 'cursor'> = {},
  adaptivePolling = false
) => {
  const userId = useVideoStudioUserId()
  return useInfiniteQuery({
    queryKey: videoStudioQueryKeys.generations(userId, filters),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      getVideoGenerations({ ...filters, cursor: pageParam, limit: 24 }),
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    enabled: userId > 0,
    refetchInterval: (query) => {
      if (!adaptivePolling) return false
      const generations = query.state.data?.pages.flatMap((page) => page.items)
      if (!generations) return false
      const visible =
        typeof document === 'undefined' ||
        document.visibilityState === 'visible'
      return getVideoTaskPollInterval(
        generations,
        Math.floor(Date.now() / 1000),
        visible
      )
    },
    refetchOnWindowFocus: adaptivePolling,
  })
}

export const useVideoGeneration = (id?: number) => {
  const userId = useVideoStudioUserId()
  return useQuery({
    queryKey: videoStudioQueryKeys.generation(userId, id ?? 0),
    queryFn: () => getVideoGeneration(id ?? 0),
    enabled: userId > 0 && Boolean(id),
  })
}

export const useVideoReferenceAssetHydration = (assetIds: number[]) => {
  const userId = useVideoStudioUserId()
  return useQueries({
    queries: assetIds.map((assetId) => ({
      queryKey: videoStudioQueryKeys.asset(userId, assetId),
      queryFn: () => getVideoAsset(assetId),
      enabled: userId > 0,
      retry: shouldRetryVideoReferenceHydration,
      refetchOnMount: 'always' as const,
    })),
  })
}

export const useDeleteVideoGeneration = () => {
  const queryClient = useQueryClient()
  const userId = useVideoStudioUserId()
  return useMutation({
    mutationFn: deleteVideoGeneration,
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: [...videoStudioQueryKeys.privateAll(userId), 'generations'],
      })
    },
  })
}

export const useAdminVideoModels = () => {
  const userId = useVideoStudioUserId()
  return useQuery({
    queryKey: videoStudioQueryKeys.adminModels(userId),
    queryFn: getAdminVideoModels,
    enabled: userId > 0,
  })
}

export const useAdminVideoSamples = () => {
  const userId = useVideoStudioUserId()
  return useInfiniteQuery({
    queryKey: videoStudioQueryKeys.adminSamples(userId),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      getAdminVideoSamples({ cursor: pageParam, limit: 50 }),
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    enabled: userId > 0,
  })
}

export const useSaveAdminVideoModel = () => {
  const queryClient = useQueryClient()
  const userId = useVideoStudioUserId()
  return useMutation({
    mutationFn: (input: { id?: number; values: VideoModelProfileInput }) =>
      input.id
        ? updateAdminVideoModel(input.id, input.values)
        : createAdminVideoModel(input.values),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.adminModels(userId),
      })
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.models(),
      })
    },
  })
}

export const useDeleteAdminVideoModel = () => {
  const queryClient = useQueryClient()
  const userId = useVideoStudioUserId()
  return useMutation({
    mutationFn: deleteAdminVideoModel,
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.adminModels(userId),
      })
    },
  })
}

export const useSaveAdminVideoSample = () => {
  const queryClient = useQueryClient()
  const userId = useVideoStudioUserId()
  return useMutation({
    mutationFn: (input: { id?: number; values: VideoSampleInput }) =>
      input.id
        ? updateAdminVideoSample(input.id, input.values)
        : createAdminVideoSample(input.values),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.adminSamples(userId),
      })
      void queryClient.invalidateQueries({
        queryKey: ['video-studio', 'samples'],
      })
    },
  })
}

export const useDeleteAdminVideoSample = () => {
  const queryClient = useQueryClient()
  const userId = useVideoStudioUserId()
  return useMutation({
    mutationFn: deleteAdminVideoSample,
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.adminSamples(userId),
      })
    },
  })
}

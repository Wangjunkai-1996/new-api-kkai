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
  type InfiniteData,
  type MutationFilters,
  type QueryClient,
  type UseQueryResult,
  useInfiniteQuery,
  useIsMutating,
  useMutation,
  useQuery,
  useQueryClient,
  useQueries,
} from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

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
  getAdminVideoModelCandidates,
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
  CursorPage,
  VideoGeneration,
  VideoGenerationFilters,
  VideoModelProfileInput,
  VideoQuoteRequest,
  VideoSampleFilters,
  VideoSampleInput,
  VideoTokenCreateResult,
} from './types'
import {
  getVideoSamplePreparationPollInterval,
  getVideoTaskPollInterval,
  isVideoGenerationActive,
} from './video-domain'
import { shouldRetryVideoReferenceHydration } from './video-reference-hydration'

export const videoStudioQueryKeys = {
  all: ['video-studio'] as const,
  privateAll: (userId: number) => privateUserQueryKey(userId, 'video-studio'),
  modelsAll: (userId: number) =>
    privateUserQueryKey(userId, 'video-studio', 'models'),
  models: (userId: number, tokenId: number) =>
    [...videoStudioQueryKeys.modelsAll(userId), tokenId] as const,
  samplesAll: (userId: number) =>
    privateUserQueryKey(userId, 'video-studio', 'samples'),
  samples: (
    userId: number,
    tokenId: number,
    filters: Omit<VideoSampleFilters, 'cursor'>
  ) => [...videoStudioQueryKeys.samplesAll(userId), tokenId, filters] as const,
  sampleAll: (userId: number) =>
    privateUserQueryKey(userId, 'video-studio', 'sample'),
  sample: (userId: number, tokenId: number, id: number) =>
    [...videoStudioQueryKeys.sampleAll(userId), tokenId, id] as const,
  token: (userId: number) =>
    privateUserQueryKey(userId, 'video-studio', 'token'),
  quoteAll: (userId: number) =>
    privateUserQueryKey(userId, 'video-studio', 'quote'),
  quote: (userId: number, request: VideoQuoteRequest) =>
    [...videoStudioQueryKeys.quoteAll(userId), request] as const,
  generationsAll: (userId: number) =>
    privateUserQueryKey(userId, 'video-studio', 'generations'),
  generations: (
    userId: number,
    filters: Omit<VideoGenerationFilters, 'cursor'>
  ) =>
    [
      ...videoStudioQueryKeys.generationsAll(userId),
      'history',
      filters,
    ] as const,
  generationHead: (
    userId: number,
    filters: Omit<VideoGenerationFilters, 'cursor'>
  ) =>
    [...videoStudioQueryKeys.generationsAll(userId), 'head', filters] as const,
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
  adminModelCandidates: (userId: number) =>
    privateUserQueryKey(userId, 'video-studio', 'admin', 'model-candidates'),
  adminSamples: (userId: number) =>
    privateUserQueryKey(userId, 'video-studio', 'admin', 'samples'),
  adminSample: (userId: number, id: number) =>
    [...videoStudioQueryKeys.adminSamples(userId), id] as const,
}

export const videoStudioMutationKeys = {
  createToken: ['video-studio', 'create-token'] as const,
}

const useVideoStudioUserId = (): number =>
  useAuthStore((state) => state.auth.user?.id ?? 0)

export const useVideoModels = (tokenId?: number | null) => {
  const userId = useVideoStudioUserId()
  return useQuery({
    queryKey: videoStudioQueryKeys.models(userId, tokenId ?? 0),
    queryFn: () => getVideoModels(tokenId ?? 0),
    enabled: userId > 0 && Boolean(tokenId),
    retry: false,
    staleTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: 'always',
  })
}

export const useVideoSamples = (
  tokenId?: number | null,
  filters: Omit<VideoSampleFilters, 'cursor'> = {}
) => {
  const userId = useVideoStudioUserId()
  return useInfiniteQuery({
    queryKey: videoStudioQueryKeys.samples(userId, tokenId ?? 0, filters),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      getVideoSamples(tokenId ?? 0, {
        ...filters,
        cursor: pageParam,
        limit: 24,
      }),
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    enabled: userId > 0 && Boolean(tokenId),
  })
}

export const useVideoSample = (id?: number, tokenId?: number | null) => {
  const userId = useVideoStudioUserId()
  return useQuery({
    queryKey: videoStudioQueryKeys.sample(userId, tokenId ?? 0, id ?? 0),
    queryFn: () => getVideoSample(id ?? 0, tokenId ?? 0),
    enabled: userId > 0 && Boolean(tokenId) && Boolean(id),
  })
}

export const useVideoTokenCapability = () => {
  const userId = useVideoStudioUserId()
  return useQuery({
    queryKey: videoStudioQueryKeys.token(userId),
    queryFn: getVideoTokenCapability,
    enabled: false,
    retry: false,
    staleTime: 30_000,
  })
}

export type CreateVideoTokenMutationVariables = Readonly<{
  userId: number
}>

export type CreateVideoTokenMutationContext = Readonly<{
  userId: number
}>

export const captureVideoTokenMutationContext = (
  variables: Pick<CreateVideoTokenMutationVariables, 'userId'>
): CreateVideoTokenMutationContext =>
  Object.freeze({ userId: variables.userId })

const isVideoTokenCreateMutationVariables = (
  value: unknown
): value is CreateVideoTokenMutationVariables => {
  if (!value || typeof value !== 'object') return false
  const variables = value as Partial<CreateVideoTokenMutationVariables>
  return Number.isInteger(variables.userId)
}

export const videoTokenCreateMutationFilters = (
  userId: number
): MutationFilters => ({
  mutationKey: videoStudioMutationKeys.createToken,
  predicate: (mutation) => {
    const variables = mutation.state.variables
    return (
      isVideoTokenCreateMutationVariables(variables) &&
      variables.userId === userId
    )
  },
})

export const useIsCreatingVideoToken = (userId: number): boolean =>
  useIsMutating(videoTokenCreateMutationFilters(userId)) > 0

export const canStartVideoTokenCreate = (
  queryClient: QueryClient,
  userId: number
): boolean =>
  queryClient.isMutating(videoTokenCreateMutationFilters(userId)) === 0

export const applyVideoTokenCreateSuccess = async (
  queryClient: QueryClient,
  capability: VideoTokenCreateResult,
  context: CreateVideoTokenMutationContext | undefined,
  activeUserId: number
): Promise<boolean> => {
  if (!context || activeUserId !== context.userId) return false

  queryClient.setQueryData(
    videoStudioQueryKeys.token(context.userId),
    capability
  )
  await queryClient.invalidateQueries({
    queryKey: videoStudioQueryKeys.modelsAll(context.userId),
    refetchType: 'active',
  })
  await queryClient.invalidateQueries({
    queryKey: videoStudioQueryKeys.samplesAll(context.userId),
    refetchType: 'active',
  })
  return true
}

export const useCreateVideoToken = () => {
  const queryClient = useQueryClient()
  return useMutation({
    mutationKey: videoStudioMutationKeys.createToken,
    mutationFn: (_variables: CreateVideoTokenMutationVariables) =>
      createVideoToken(),
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

export const mergeVideoGenerationItems = (
  historyItems: VideoGeneration[],
  headItems: VideoGeneration[],
  detailItems: VideoGeneration[],
  observedItems: VideoGeneration[] = [],
  excludedIds?: ReadonlySet<number>
): VideoGeneration[] => {
  const mergedById = new Map(observedItems.map((item) => [item.id, item]))
  for (const item of historyItems) {
    mergedById.set(item.id, item)
  }
  for (const item of headItems) {
    mergedById.set(item.id, item)
  }
  for (const item of detailItems) {
    mergedById.set(item.id, item)
  }
  return [...mergedById.values()]
    .filter((item) => !excludedIds?.has(item.id))
    .sort((left, right) => right.id - left.id)
}

export type VideoGenerationObservation = {
  item: VideoGeneration
  terminalExpiresAt?: number
}

export type VideoGenerationDetailProbe =
  | { kind: 'found'; item: VideoGeneration }
  | { id: number; kind: 'not_found' }
  | { id: number; kind: 'transient_error' }

const VIDEO_GENERATION_PAGE_SIZE = 24
const VIDEO_GENERATION_TERMINAL_GAP_TTL_MS = 2 * 60 * 1_000
const VIDEO_GENERATION_TERMINAL_GAP_LIMIT = 48

const hasFiniteVideoGenerationTerminalExpiry = (
  observation: VideoGenerationObservation
): observation is VideoGenerationObservation & { terminalExpiresAt: number } =>
  typeof observation.terminalExpiresAt === 'number' &&
  Number.isFinite(observation.terminalExpiresAt)

export const reconcileVideoGenerationObservations = ({
  detailProbes,
  headItems,
  historyItems,
  nowMs,
  previous,
}: {
  detailProbes: VideoGenerationDetailProbe[]
  headItems: VideoGeneration[]
  historyItems: VideoGeneration[]
  nowMs: number
  previous: VideoGenerationObservation[]
}): VideoGenerationObservation[] => {
  const observationsById = new Map<number, VideoGenerationObservation>()
  for (const observation of previous) {
    if (
      hasFiniteVideoGenerationTerminalExpiry(observation) &&
      observation.terminalExpiresAt <= nowMs
    ) {
      continue
    }
    observationsById.set(observation.item.id, observation)
  }

  const canonicalItemsById = new Map<number, VideoGeneration>()
  for (const item of historyItems) canonicalItemsById.set(item.id, item)
  for (const item of headItems) canonicalItemsById.set(item.id, item)
  for (const item of canonicalItemsById.values()) {
    if (!isVideoGenerationActive(item)) {
      observationsById.delete(item.id)
      continue
    }
    const current = observationsById.get(item.id)
    if (current && !isVideoGenerationActive(current.item)) continue
    if (current?.item === item) continue
    observationsById.set(item.id, { item })
  }

  for (const [id, observation] of observationsById) {
    if (isVideoGenerationActive(observation.item)) continue
    const canonicalItem = canonicalItemsById.get(id)
    if (canonicalItem && isVideoGenerationActive(canonicalItem)) {
      if (hasFiniteVideoGenerationTerminalExpiry(observation)) {
        observationsById.set(id, { item: observation.item })
      }
      continue
    }
    if (
      !canonicalItem &&
      !hasFiniteVideoGenerationTerminalExpiry(observation)
    ) {
      observationsById.set(id, {
        item: observation.item,
        terminalExpiresAt: nowMs + VIDEO_GENERATION_TERMINAL_GAP_TTL_MS,
      })
    }
  }

  for (const probe of detailProbes) {
    if (probe.kind === 'transient_error') continue
    if (probe.kind === 'not_found') {
      observationsById.delete(probe.id)
      continue
    }
    const item = probe.item
    const canonicalItem = canonicalItemsById.get(item.id)
    if (canonicalItem && !isVideoGenerationActive(canonicalItem)) {
      observationsById.delete(item.id)
      continue
    }
    const current = observationsById.get(item.id)
    if (!current) continue
    if (isVideoGenerationActive(item)) {
      if (current.item !== item || current.terminalExpiresAt !== undefined) {
        observationsById.set(item.id, { item })
      }
      continue
    }
    let terminalExpiresAt: number | undefined
    if (!canonicalItem) {
      terminalExpiresAt = hasFiniteVideoGenerationTerminalExpiry(current)
        ? current.terminalExpiresAt
        : nowMs + VIDEO_GENERATION_TERMINAL_GAP_TTL_MS
    }
    if (
      current.item !== item ||
      current.terminalExpiresAt !== terminalExpiresAt
    ) {
      observationsById.set(
        item.id,
        terminalExpiresAt === undefined ? { item } : { item, terminalExpiresAt }
      )
    }
  }

  const active: VideoGenerationObservation[] = []
  const terminalOverrides: VideoGenerationObservation[] = []
  const terminalGaps: Array<
    VideoGenerationObservation & { terminalExpiresAt: number }
  > = []
  for (const observation of observationsById.values()) {
    if (isVideoGenerationActive(observation.item)) {
      active.push(observation)
    } else if (hasFiniteVideoGenerationTerminalExpiry(observation)) {
      if (observation.terminalExpiresAt > nowMs) terminalGaps.push(observation)
    } else {
      terminalOverrides.push(observation)
    }
  }
  terminalGaps.sort((left, right) => {
    const expiryOrder = right.terminalExpiresAt - left.terminalExpiresAt
    return expiryOrder === 0 ? right.item.id - left.item.id : expiryOrder
  })
  const previousById = new Map(
    previous.map((observation) => [observation.item.id, observation])
  )
  const next = [
    ...active,
    ...terminalOverrides,
    ...terminalGaps.slice(0, VIDEO_GENERATION_TERMINAL_GAP_LIMIT),
  ]
    .sort((left, right) => right.item.id - left.item.id)
    .map((observation) => {
      const prior = previousById.get(observation.item.id)
      return prior?.item === observation.item &&
        prior.terminalExpiresAt === observation.terminalExpiresAt
        ? prior
        : observation
    })

  return next.length === previous.length &&
    next.every((observation, index) => observation === previous[index])
    ? previous
    : next
}

export const forgetVideoGenerationObservation = (
  observations: VideoGenerationObservation[],
  generationId: number
): VideoGenerationObservation[] => {
  const index = observations.findIndex(
    (observation) => observation.item.id === generationId
  )
  if (index < 0) return observations
  return observations.filter(
    (observation) => observation.item.id !== generationId
  )
}

const removeVideoGenerationsFromPage = (
  page: CursorPage<VideoGeneration>,
  generationIds: ReadonlySet<number>
): CursorPage<VideoGeneration> => {
  const items = page.items.filter((item) => !generationIds.has(item.id))
  return items.length === page.items.length ? page : { ...page, items }
}

export const pruneVideoGenerationQueryCaches = async (
  queryClient: QueryClient,
  userId: number,
  filters: Omit<VideoGenerationFilters, 'cursor'>,
  generationIds: ReadonlySet<number>
): Promise<void> => {
  if (generationIds.size === 0) return

  const historyKey = videoStudioQueryKeys.generations(userId, filters)
  const headKey = videoStudioQueryKeys.generationHead(userId, filters)
  await Promise.all([
    queryClient.cancelQueries({ exact: true, queryKey: historyKey }),
    queryClient.cancelQueries({ exact: true, queryKey: headKey }),
  ])

  queryClient.setQueryData<
    InfiniteData<CursorPage<VideoGeneration>, string | undefined>
  >(historyKey, (current) => {
    if (!current) return current
    const pages = current.pages.map((page) =>
      removeVideoGenerationsFromPage(page, generationIds)
    )
    return pages.every((page, index) => page === current.pages[index])
      ? current
      : { ...current, pages }
  })
  queryClient.setQueryData<CursorPage<VideoGeneration>>(headKey, (current) =>
    current ? removeVideoGenerationsFromPage(current, generationIds) : current
  )
}

const EMPTY_VIDEO_GENERATIONS: VideoGeneration[] = []
const EMPTY_VIDEO_GENERATION_OBSERVATIONS: VideoGenerationObservation[] = []

const isVideoGenerationTombstoneError = (error: unknown): boolean =>
  isAxiosError(error) && [404, 410].includes(error.response?.status ?? 0)

export const useVideoGenerations = (
  filters: Omit<VideoGenerationFilters, 'cursor'> = {},
  adaptivePolling = false,
  targetTaskId?: string
) => {
  const userId = useVideoStudioUserId()
  const queryClient = useQueryClient()
  const filterLimit = filters.limit
  const filterStatus = filters.status
  const stableFilters = useMemo(() => {
    const value: Omit<VideoGenerationFilters, 'cursor'> = {}
    if (filterLimit !== undefined) value.limit = filterLimit
    if (filterStatus !== undefined) value.status = filterStatus
    return value
  }, [filterLimit, filterStatus])
  const observationScope = JSON.stringify(
    videoStudioQueryKeys.generations(userId, stableFilters)
  )
  const observationScopeRef = useRef(observationScope)
  observationScopeRef.current = observationScope
  const historyQuery = useInfiniteQuery({
    queryKey: videoStudioQueryKeys.generations(userId, stableFilters),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      getVideoGenerations({
        ...stableFilters,
        cursor: pageParam,
        limit: VIDEO_GENERATION_PAGE_SIZE,
      }),
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    enabled: userId > 0,
    refetchOnWindowFocus: adaptivePolling || Boolean(targetTaskId),
  })
  const historyItems = useMemo(
    () => historyQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [historyQuery.data]
  )
  const historyHeadItems =
    historyQuery.data?.pages[0]?.items ?? EMPTY_VIDEO_GENERATIONS
  const livePollingEnabled =
    userId > 0 && adaptivePolling && historyQuery.data !== undefined
  const [observationState, setObservationState] = useState<{
    observations: VideoGenerationObservation[]
    scope: string
  }>({ observations: [], scope: observationScope })
  const observations =
    livePollingEnabled && observationState.scope === observationScope
      ? observationState.observations
      : EMPTY_VIDEO_GENERATION_OBSERVATIONS
  const observationItems = useMemo(
    () => observations.map((observation) => observation.item),
    [observations]
  )
  const forgetObservedGeneration = useCallback(
    async (generationId: number): Promise<void> => {
      await pruneVideoGenerationQueryCaches(
        queryClient,
        userId,
        stableFilters,
        new Set([generationId])
      )
      if (observationScopeRef.current !== observationScope) return
      setObservationState((current) => {
        if (current.scope !== observationScope) return current
        const observations = forgetVideoGenerationObservation(
          current.observations,
          generationId
        )
        return observations === current.observations
          ? current
          : { ...current, observations }
      })
    },
    [observationScope, queryClient, stableFilters, userId]
  )
  const headQuery = useQuery({
    queryKey: videoStudioQueryKeys.generationHead(userId, stableFilters),
    queryFn: () =>
      getVideoGenerations({
        ...stableFilters,
        limit: VIDEO_GENERATION_PAGE_SIZE,
      }),
    enabled: livePollingEnabled,
    refetchInterval: (query) => {
      if (!adaptivePolling) return false
      const generations = query.state.data?.items ?? historyHeadItems
      const visible =
        typeof document === 'undefined' ||
        document.visibilityState === 'visible'
      if (!visible) return false
      if (
        targetTaskId &&
        ![...generations, ...historyItems, ...observationItems].some(
          (generation) => generation.task_id === targetTaskId
        )
      ) {
        return 3_000
      }
      return getVideoTaskPollInterval(
        generations,
        Math.floor(Date.now() / 1000),
        true
      )
    },
    refetchOnWindowFocus: adaptivePolling || Boolean(targetTaskId),
  })
  const effectiveHeadItems = livePollingEnabled
    ? (headQuery.data?.items ?? historyHeadItems)
    : historyHeadItems
  const detailTargets = useMemo(() => {
    const headIds = new Set(effectiveHeadItems.map((item) => item.id))
    return observations.filter(
      (observation) =>
        isVideoGenerationActive(observation.item) &&
        !headIds.has(observation.item.id)
    )
  }, [effectiveHeadItems, observations])
  const combineDetailQueries = useCallback(
    (queries: UseQueryResult<VideoGeneration>[]) => ({
      probes: queries.flatMap<VideoGenerationDetailProbe>((query, index) => {
        const observation = detailTargets[index]
        if (!observation) return []
        if (query.isError) {
          return [
            isVideoGenerationTombstoneError(query.error)
              ? { id: observation.item.id, kind: 'not_found' }
              : { id: observation.item.id, kind: 'transient_error' },
          ]
        }
        return query.data ? [{ item: query.data, kind: 'found' }] : []
      }),
      queries,
    }),
    [detailTargets]
  )
  const { probes: detailProbes, queries: detailQueries } = useQueries({
    queries: detailTargets.map((observation) => ({
      queryKey: videoStudioQueryKeys.generation(userId, observation.item.id),
      queryFn: () => getVideoGeneration(observation.item.id, { silent: true }),
      enabled: livePollingEnabled && isVideoGenerationActive(observation.item),
      retry: (failureCount: number, error: unknown) =>
        !isVideoGenerationTombstoneError(error) && failureCount < 3,
      refetchInterval: (query: { state: { data?: VideoGeneration } }) => {
        if (!isVideoGenerationActive(observation.item)) return false
        const visible =
          typeof document === 'undefined' ||
          document.visibilityState === 'visible'
        return getVideoTaskPollInterval(
          [query.state.data ?? observation.item],
          Math.floor(Date.now() / 1000),
          visible
        )
      },
      refetchOnWindowFocus:
        adaptivePolling && isVideoGenerationActive(observation.item),
    })),
    combine: combineDetailQueries,
  })
  const tombstonedGenerationIds = useMemo(
    () =>
      new Set(
        detailProbes.flatMap((probe) =>
          probe.kind === 'not_found' ? [probe.id] : []
        )
      ),
    [detailProbes]
  )
  const reconcileObservations = useCallback(
    async (nowMs: number): Promise<void> => {
      if (tombstonedGenerationIds.size > 0) {
        await pruneVideoGenerationQueryCaches(
          queryClient,
          userId,
          stableFilters,
          tombstonedGenerationIds
        )
      }
      if (observationScopeRef.current !== observationScope) return
      setObservationState((current) => {
        const previous =
          current.scope === observationScope
            ? current.observations
            : EMPTY_VIDEO_GENERATION_OBSERVATIONS
        const next = reconcileVideoGenerationObservations({
          detailProbes,
          headItems: effectiveHeadItems,
          historyItems,
          nowMs,
          previous,
        })
        const unchanged =
          current.scope === observationScope &&
          next.length === current.observations.length &&
          next.every(
            (observation, index) => observation === current.observations[index]
          )
        return unchanged
          ? current
          : { observations: next, scope: observationScope }
      })
    },
    [
      detailProbes,
      effectiveHeadItems,
      historyItems,
      observationScope,
      queryClient,
      stableFilters,
      tombstonedGenerationIds,
      userId,
    ]
  )
  useEffect(() => {
    if (!livePollingEnabled) return
    void reconcileObservations(Date.now())
  }, [livePollingEnabled, reconcileObservations])
  const nextTerminalExpiresAt = useMemo(() => {
    let next: number | undefined
    for (const observation of observations) {
      if (
        hasFiniteVideoGenerationTerminalExpiry(observation) &&
        (next === undefined || observation.terminalExpiresAt < next)
      ) {
        next = observation.terminalExpiresAt
      }
    }
    return next
  }, [observations])
  useEffect(() => {
    if (!livePollingEnabled || nextTerminalExpiresAt === undefined) return
    const timeout = window.setTimeout(
      () => void reconcileObservations(Date.now()),
      Math.max(0, nextTerminalExpiresAt - Date.now()) + 1
    )
    return () => window.clearTimeout(timeout)
  }, [livePollingEnabled, nextTerminalExpiresAt, reconcileObservations])
  const detailItems = [
    ...observations.flatMap((observation) =>
      isVideoGenerationActive(observation.item) ? [] : [observation.item]
    ),
    ...detailProbes.flatMap((probe) =>
      probe.kind === 'found' ? [probe.item] : []
    ),
  ]
  const items = mergeVideoGenerationItems(
    historyItems,
    effectiveHeadItems,
    detailItems,
    observationItems,
    tombstonedGenerationIds
  )
  const liveErrorQuery = detailQueries.find(
    (query) => query.isError && !isVideoGenerationTombstoneError(query.error)
  )
  const refresh = async (): Promise<void> => {
    const requests: Promise<unknown>[] = [historyQuery.refetch()]
    if (livePollingEnabled) {
      requests.push(headQuery.refetch())
      requests.push(
        ...detailQueries.flatMap((query, index) => {
          const observation = detailTargets[index]
          return observation && isVideoGenerationActive(observation.item)
            ? [query.refetch()]
            : []
        })
      )
    }
    await Promise.all(requests)
  }

  const liveQueryError = livePollingEnabled
    ? (headQuery.error ?? liveErrorQuery?.error ?? null)
    : null

  return {
    ...historyQuery,
    forgetObservedGeneration,
    items,
    refresh,
    error: historyQuery.error ?? liveQueryError,
    isError:
      historyQuery.isError ||
      (livePollingEnabled && (headQuery.isError || Boolean(liveErrorQuery))),
    isFetching:
      historyQuery.isFetching ||
      (livePollingEnabled &&
        (headQuery.isFetching ||
          detailQueries.some((query) => query.isFetching))),
  }
}

export const useVideoGeneration = (id?: number) => {
  const userId = useVideoStudioUserId()
  return useQuery({
    queryKey: videoStudioQueryKeys.generation(userId, id ?? 0),
    queryFn: () => getVideoGeneration(id ?? 0),
    enabled: userId > 0 && Boolean(id),
  })
}

export const useAdminVideoAsset = (id?: number, enabled = true) => {
  const userId = useVideoStudioUserId()
  return useQuery({
    queryKey: videoStudioQueryKeys.asset(userId, id ?? 0),
    queryFn: () => getVideoAsset(id ?? 0, true),
    enabled: userId > 0 && Boolean(id) && enabled,
    refetchInterval: (query) => {
      const asset = query.state.data
      return asset ? getVideoSamplePreparationPollInterval(asset) : false
    },
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
        queryKey: videoStudioQueryKeys.generationsAll(userId),
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

export const useAdminVideoModelCandidates = () => {
  const userId = useVideoStudioUserId()
  return useQuery({
    queryKey: videoStudioQueryKeys.adminModelCandidates(userId),
    queryFn: getAdminVideoModelCandidates,
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
        queryKey: videoStudioQueryKeys.adminModelCandidates(userId),
      })
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.adminModels(userId),
      })
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.modelsAll(userId),
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
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.modelsAll(userId),
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
        queryKey: videoStudioQueryKeys.adminModels(userId),
      })
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.samplesAll(userId),
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
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.adminModels(userId),
      })
      void queryClient.invalidateQueries({
        queryKey: videoStudioQueryKeys.samplesAll(userId),
      })
    },
  })
}

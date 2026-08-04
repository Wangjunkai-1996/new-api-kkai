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
import assert from 'node:assert/strict'
import { afterEach, beforeEach, describe, test } from 'node:test'

import {
  MutationObserver,
  QueryClient,
  QueryObserver,
} from '@tanstack/react-query'

import { installAuthQueryCacheBoundary } from '@/lib/auth-query-cache'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'

import {
  applyVideoTokenCreateSuccess,
  canStartVideoTokenCreate,
  captureVideoTokenMutationContext,
  forgetVideoGenerationObservation,
  mergeVideoGenerationItems,
  pruneVideoGenerationQueryCaches,
  reconcileVideoGenerationObservations,
  type VideoGenerationObservation,
  videoStudioMutationKeys,
  videoStudioQueryKeys,
  videoTokenCreateMutationFilters,
} from './queries'
import type { VideoGeneration, VideoTokenCreateResult } from './types'

const authUser = (id: number): AuthUser => ({
  id,
  username: `user-${id}`,
  role: 1,
})

const readyVideoToken = (id: number): VideoTokenCreateResult => ({
  required_group: 'Seedance 视频',
  has_usable_token: true,
  can_create: true,
  effective_models: ['model-a', 'model-b'],
  status: 'ready',
  token: { id, name: '视频工作室', group: 'Seedance 视频' },
  created: true,
})

const videoGeneration = (
  id: number,
  status: VideoGeneration['status'],
  progress: string = status
): VideoGeneration => ({
  id,
  task_id: `task-${id}`,
  model_profile_id: 1,
  model: 'video-model',
  mode: 'text_to_video',
  prompt: `generation-${id}`,
  parameters: {},
  status,
  progress,
  quota: 1,
  created_at: id,
  updated_at: id,
})

describe('video generation live query composition', () => {
  test('prefers head data over stale history data', () => {
    const items = mergeVideoGenerationItems(
      [videoGeneration(1, 'queued', 'history')],
      [videoGeneration(1, 'processing', 'head')],
      []
    )

    assert.equal(items.length, 1)
    assert.equal(items[0]?.status, 'processing')
    assert.equal(items[0]?.progress, 'head')
  })

  test('prefers detail data over head and history data', () => {
    const items = mergeVideoGenerationItems(
      [videoGeneration(1, 'queued', 'history')],
      [videoGeneration(1, 'processing', 'head')],
      [videoGeneration(1, 'ready', 'detail')]
    )

    assert.equal(items.length, 1)
    assert.equal(items[0]?.status, 'ready')
    assert.equal(items[0]?.progress, 'detail')
  })

  test('prepends new head rows while retaining loaded history without duplicate ids', () => {
    const items = mergeVideoGenerationItems(
      [
        videoGeneration(3, 'queued', 'history-3'),
        videoGeneration(2, 'ready'),
        videoGeneration(1, 'ready'),
      ],
      [
        videoGeneration(5, 'queued'),
        videoGeneration(4, 'processing'),
        videoGeneration(3, 'processing', 'head-3'),
      ],
      []
    )

    assert.deepEqual(
      items.map((item) => item.id),
      [5, 4, 3, 2, 1]
    )
    assert.equal(items.find((item) => item.id === 3)?.progress, 'head-3')
  })

  test('retains active rows across two consecutive head rollovers', () => {
    const nowMs = 10_000
    const historyItems = Array.from({ length: 24 }, (_, index) =>
      videoGeneration(100 - index, 'ready')
    )
    const firstHeadItems = Array.from({ length: 24 }, (_, index) =>
      videoGeneration(124 - index, index === 23 ? 'processing' : 'ready')
    )
    const firstObservations = reconcileVideoGenerationObservations({
      detailProbes: [],
      headItems: firstHeadItems,
      historyItems,
      nowMs,
      previous: [],
    })
    const secondHeadItems = Array.from({ length: 24 }, (_, index) =>
      videoGeneration(125 - index, index === 23 ? 'processing' : 'ready')
    )
    const secondObservations = reconcileVideoGenerationObservations({
      detailProbes: [],
      headItems: secondHeadItems,
      historyItems,
      nowMs,
      previous: firstObservations,
    })
    const thirdHeadItems = Array.from({ length: 24 }, (_, index) =>
      videoGeneration(126 - index, 'ready')
    )
    const thirdObservations = reconcileVideoGenerationObservations({
      detailProbes: [],
      headItems: thirdHeadItems,
      historyItems,
      nowMs,
      previous: secondObservations,
    })

    assert.deepEqual(
      thirdObservations.map((observation) => observation.item.id),
      [102, 101]
    )
  })

  test('retains the observed gap after history refreshes only its first page', () => {
    const refreshedFirstPage = Array.from({ length: 24 }, (_, index) =>
      videoGeneration(126 - index, 'ready')
    )
    const observedGap: VideoGenerationObservation[] = [
      { item: videoGeneration(102, 'processing') },
      { item: videoGeneration(101, 'processing') },
    ]
    const observations = reconcileVideoGenerationObservations({
      detailProbes: [],
      headItems: refreshedFirstPage,
      historyItems: refreshedFirstPage,
      nowMs: 10_000,
      previous: observedGap,
    })

    assert.deepEqual(
      observations.map((observation) => observation.item.id),
      [102, 101]
    )
  })

  test('tracks only active canonical rows and lets terminal rows take over', () => {
    const priorTerminalGap: VideoGenerationObservation = {
      item: videoGeneration(3, 'failed'),
      terminalExpiresAt: 50_000,
    }
    const observations = reconcileVideoGenerationObservations({
      detailProbes: [],
      headItems: [
        videoGeneration(4, 'processing'),
        videoGeneration(3, 'ready'),
      ],
      historyItems: [
        videoGeneration(2, 'queued'),
        videoGeneration(1, 'failed'),
      ],
      nowMs: 10_000,
      previous: [priorTerminalGap],
    })

    assert.deepEqual(
      observations.map((observation) => observation.item.id),
      [4, 2]
    )
  })

  test('updates an active observation from a successful detail probe', () => {
    const previous = { item: videoGeneration(7, 'queued', 'old') }
    const detail = videoGeneration(7, 'processing', 'detail')
    const observations = reconcileVideoGenerationObservations({
      detailProbes: [{ item: detail, kind: 'found' }],
      headItems: [],
      historyItems: [],
      nowMs: 10_000,
      previous: [previous],
    })

    assert.equal(observations[0]?.item, detail)
    assert.equal(observations[0]?.terminalExpiresAt, undefined)
  })

  test('is idempotent when detail overrides stale canonical data', () => {
    const detail = videoGeneration(7, 'processing', 'detail')
    const previous: VideoGenerationObservation[] = [{ item: detail }]
    const observations = reconcileVideoGenerationObservations({
      detailProbes: [{ item: detail, kind: 'found' }],
      headItems: [videoGeneration(7, 'queued', 'stale-head')],
      historyItems: [],
      nowMs: 10_000,
      previous,
    })

    assert.equal(observations, previous)
    assert.equal(observations[0], previous[0])
  })

  test('does not recreate a forgotten observation from a late detail result', () => {
    const observations = reconcileVideoGenerationObservations({
      detailProbes: [
        { item: videoGeneration(7, 'processing', 'late'), kind: 'found' },
      ],
      headItems: [],
      historyItems: [],
      nowMs: 10_000,
      previous: [],
    })

    assert.deepEqual(observations, [])
  })

  test('keeps a terminal detail result as a two-minute gap', () => {
    const nowMs = 10_000
    const terminal = videoGeneration(7, 'ready', 'detail')
    const observations = reconcileVideoGenerationObservations({
      detailProbes: [{ item: terminal, kind: 'found' }],
      headItems: [],
      historyItems: [],
      nowMs,
      previous: [{ item: videoGeneration(7, 'processing') }],
    })

    assert.equal(observations[0]?.item, terminal)
    assert.equal(observations[0]?.terminalExpiresAt, nowMs + 120_000)
  })

  test('keeps terminal detail over stale active history until history settles', () => {
    const nowMs = 10_000
    const staleHistory = videoGeneration(7, 'processing', 'stale-history')
    const terminal = videoGeneration(7, 'ready', 'detail')
    const override = reconcileVideoGenerationObservations({
      detailProbes: [{ item: terminal, kind: 'found' }],
      headItems: [],
      historyItems: [staleHistory],
      nowMs,
      previous: [{ item: staleHistory }],
    })

    assert.equal(override[0]?.item, terminal)
    assert.equal(override[0]?.terminalExpiresAt, undefined)

    const afterGapTtl = reconcileVideoGenerationObservations({
      detailProbes: [{ item: terminal, kind: 'found' }],
      headItems: [],
      historyItems: [staleHistory],
      nowMs: nowMs + 120_001,
      previous: override,
    })

    assert.equal(afterGapTtl, override)

    const settled = reconcileVideoGenerationObservations({
      detailProbes: [{ item: terminal, kind: 'found' }],
      headItems: [],
      historyItems: [terminal],
      nowMs: nowMs + 120_002,
      previous: afterGapTtl,
    })

    assert.deepEqual(settled, [])
  })

  test('starts a terminal gap ttl once its stale canonical row disappears', () => {
    const nowMs = 10_000
    const terminal = videoGeneration(7, 'ready', 'detail')
    const observations = reconcileVideoGenerationObservations({
      detailProbes: [{ item: terminal, kind: 'found' }],
      headItems: [],
      historyItems: [],
      nowMs,
      previous: [{ item: terminal }],
    })

    assert.equal(observations[0]?.item, terminal)
    assert.equal(observations[0]?.terminalExpiresAt, nowMs + 120_000)
  })

  test('removes a detail tombstone and retains an active transient failure', () => {
    const tombstoned = { item: videoGeneration(8, 'processing') }
    const transient = { item: videoGeneration(7, 'processing') }
    const observations = reconcileVideoGenerationObservations({
      detailProbes: [
        { id: tombstoned.item.id, kind: 'not_found' },
        { id: transient.item.id, kind: 'transient_error' },
      ],
      headItems: [],
      historyItems: [],
      nowMs: 10_000,
      previous: [tombstoned, transient],
    })

    assert.deepEqual(observations, [transient])
  })

  test('prunes a detail tombstone from history and head without rebuilding pagination', async () => {
    const queryClient = new QueryClient()
    const filters = {}
    const historyKey = videoStudioQueryKeys.generations(11, filters)
    const headKey = videoStudioQueryKeys.generationHead(11, filters)
    const retainedPage = {
      items: [videoGeneration(6, 'ready')],
      next_cursor: undefined,
    }
    const history = {
      pageParams: [undefined, 'page-2'],
      pages: [
        {
          items: [
            videoGeneration(8, 'processing'),
            videoGeneration(7, 'ready'),
          ],
          next_cursor: 'page-2',
        },
        retainedPage,
      ],
    }
    const head = {
      items: [
        videoGeneration(9, 'processing'),
        videoGeneration(8, 'processing'),
      ],
      next_cursor: 'head-cursor',
    }
    queryClient.setQueryData(historyKey, history)
    queryClient.setQueryData(headKey, head)

    await pruneVideoGenerationQueryCaches(
      queryClient,
      11,
      filters,
      new Set([8])
    )

    const prunedHistory = queryClient.getQueryData<typeof history>(historyKey)
    const prunedHead = queryClient.getQueryData<typeof head>(headKey)
    assert.deepEqual(
      prunedHistory?.pages.flatMap((page) => page.items.map((item) => item.id)),
      [7, 6]
    )
    assert.equal(prunedHistory?.pageParams, history.pageParams)
    assert.equal(prunedHistory?.pages[1], retainedPage)
    assert.equal(prunedHistory?.pages[0]?.next_cursor, 'page-2')
    assert.deepEqual(
      prunedHead?.items.map((item) => item.id),
      [9]
    )
    assert.equal(prunedHead?.next_cursor, 'head-cursor')
    assert.equal(
      mergeVideoGenerationItems(
        history.pages.flatMap((page) => page.items),
        head.items,
        [],
        [videoGeneration(8, 'processing')],
        new Set([8])
      ).some((item) => item.id === 8),
      false
    )

    const afterTombstone = reconcileVideoGenerationObservations({
      detailProbes: [{ id: 8, kind: 'not_found' }],
      headItems: prunedHead?.items ?? [],
      historyItems: prunedHistory?.pages.flatMap((page) => page.items) ?? [],
      nowMs: 10_000,
      previous: [{ item: videoGeneration(8, 'processing') }],
    })
    const nextReconcile = reconcileVideoGenerationObservations({
      detailProbes: [],
      headItems: prunedHead?.items ?? [],
      historyItems: prunedHistory?.pages.flatMap((page) => page.items) ?? [],
      nowMs: 10_001,
      previous: afterTombstone,
    })

    assert.deepEqual(
      afterTombstone.map((observation) => observation.item.id),
      [9]
    )
    assert.deepEqual(
      nextReconcile.map((observation) => observation.item.id),
      [9]
    )
    assert.equal(
      mergeVideoGenerationItems(
        prunedHistory?.pages.flatMap((page) => page.items) ?? [],
        prunedHead?.items ?? [],
        [],
        nextReconcile.map((observation) => observation.item)
      ).some((item) => item.id === 8),
      false
    )
  })

  test('waits for exact query cancellation before pruning cached rows', async () => {
    const queryClient = new QueryClient()
    const filters = {}
    const historyKey = videoStudioQueryKeys.generations(11, filters)
    const history = {
      pageParams: [undefined],
      pages: [
        {
          items: [videoGeneration(8, 'processing')],
          next_cursor: undefined,
        },
      ],
    }
    queryClient.setQueryData(historyKey, history)
    const releaseCancellations: Array<() => void> = []
    queryClient.cancelQueries = () =>
      new Promise<void>((resolve) => {
        releaseCancellations.push(resolve)
      })

    const pruning = pruneVideoGenerationQueryCaches(
      queryClient,
      11,
      filters,
      new Set([8])
    )

    assert.equal(releaseCancellations.length, 2)
    assert.equal(
      queryClient
        .getQueryData<typeof history>(historyKey)
        ?.pages[0]?.items.some((item) => item.id === 8),
      true
    )

    for (const release of releaseCancellations) release()
    await pruning

    assert.equal(
      queryClient
        .getQueryData<typeof history>(historyKey)
        ?.pages[0]?.items.some((item) => item.id === 8),
      false
    )
  })

  test('expires terminal gaps without expiring active observations', () => {
    const active = { item: videoGeneration(8, 'processing') }
    const terminal: VideoGenerationObservation = {
      item: videoGeneration(7, 'ready'),
      terminalExpiresAt: 20_000,
    }
    const observations = reconcileVideoGenerationObservations({
      detailProbes: [],
      headItems: [],
      historyItems: [],
      nowMs: terminal.terminalExpiresAt ?? 0,
      previous: [active, terminal],
    })

    assert.deepEqual(observations, [active])
  })

  test('caps terminal gaps at 48 while retaining every active observation', () => {
    const active: VideoGenerationObservation[] = Array.from(
      { length: 60 },
      (_, index) => ({ item: videoGeneration(index + 1, 'processing') })
    )
    const terminal: VideoGenerationObservation[] = Array.from(
      { length: 50 },
      (_, index) => ({
        item: videoGeneration(index + 100, 'ready'),
        terminalExpiresAt: index === 0 ? 60_000 : 50_000,
      })
    )
    const observations = reconcileVideoGenerationObservations({
      detailProbes: [],
      headItems: [],
      historyItems: [],
      nowMs: 10_000,
      previous: [...active, ...terminal],
    })

    assert.equal(
      observations.filter(
        (observation) => observation.terminalExpiresAt === undefined
      ).length,
      active.length
    )
    assert.deepEqual(
      observations
        .filter((observation) => observation.terminalExpiresAt !== undefined)
        .map((observation) => observation.item.id),
      [...Array.from({ length: 47 }, (_, index) => 149 - index), 100]
    )
  })

  test('forgets a deleted observation before refetched sources merge', () => {
    const deleted = { item: videoGeneration(101, 'processing') }
    const retained = { item: videoGeneration(100, 'processing') }
    const remainingObservations = forgetVideoGenerationObservation(
      [deleted, retained],
      deleted.item.id
    )
    const visibleItems = mergeVideoGenerationItems(
      [retained.item],
      [retained.item],
      [],
      remainingObservations.map((observation) => observation.item)
    )

    assert.deepEqual(
      visibleItems.map((item) => item.id),
      [retained.item.id]
    )
    assert.equal(
      forgetVideoGenerationObservation(remainingObservations, 999),
      remainingObservations
    )
  })

  test('keeps history and head keys distinct under one invalidation prefix', () => {
    const allKey = videoStudioQueryKeys.generationsAll(11)
    const historyKey = videoStudioQueryKeys.generations(11, {})
    const headKey = videoStudioQueryKeys.generationHead(11, {})
    const otherUserHistoryKey = videoStudioQueryKeys.generations(22, {})

    assert.notDeepEqual(historyKey, headKey)
    assert.notDeepEqual(historyKey, otherUserHistoryKey)
    assert.deepEqual(historyKey.slice(0, allKey.length), allKey)
    assert.deepEqual(headKey.slice(0, allKey.length), allKey)
  })
})

describe('video studio private query ownership', () => {
  let queryClient: QueryClient
  let unsubscribe: () => void = () => undefined

  beforeEach(() => {
    useAuthStore.getState().auth.reset()
    queryClient = new QueryClient()
    unsubscribe = () => undefined
  })

  afterEach(() => {
    unsubscribe()
    useAuthStore.getState().auth.reset()
    queryClient.clear()
  })

  test('removes user A private data on an A to B switch without touching B data', () => {
    useAuthStore.getState().auth.setUser(authUser(11))
    unsubscribe = installAuthQueryCacheBoundary(queryClient)
    const userAGenerations = videoStudioQueryKeys.generations(11, {})
    const userAAsset = videoStudioQueryKeys.asset(11, 101)
    const userAToken = videoStudioQueryKeys.token(11)
    const userBGenerations = videoStudioQueryKeys.generations(22, {})
    const userAModels = videoStudioQueryKeys.models(11, 17)
    const userBModels = videoStudioQueryKeys.models(22, 23)
    const userASamples = videoStudioQueryKeys.samples(11, 17, {})
    const userBSamples = videoStudioQueryKeys.samples(22, 23, {})

    queryClient.setQueryData(userAGenerations, ['user-a-generation'])
    queryClient.setQueryData(userAAsset, { id: 101 })
    queryClient.setQueryData(userAToken, { token: { id: 7 } })
    queryClient.setQueryData(userBGenerations, ['user-b-generation'])
    queryClient.setQueryData(userAModels, ['user-a-model'])
    queryClient.setQueryData(userBModels, ['user-b-model'])
    queryClient.setQueryData(userASamples, ['user-a-sample'])
    queryClient.setQueryData(userBSamples, ['user-b-sample'])

    useAuthStore.getState().auth.setUser(authUser(22))

    assert.equal(queryClient.getQueryData(userAGenerations), undefined)
    assert.equal(queryClient.getQueryData(userAAsset), undefined)
    assert.equal(queryClient.getQueryData(userAToken), undefined)
    assert.deepEqual(queryClient.getQueryData(userBGenerations), [
      'user-b-generation',
    ])
    assert.equal(queryClient.getQueryData(userAModels), undefined)
    assert.deepEqual(queryClient.getQueryData(userBModels), ['user-b-model'])
    assert.equal(queryClient.getQueryData(userASamples), undefined)
    assert.deepEqual(queryClient.getQueryData(userBSamples), ['user-b-sample'])
  })

  test('removes the authenticated user private data when the session is reset', () => {
    useAuthStore.getState().auth.setUser(authUser(11))
    unsubscribe = installAuthQueryCacheBoundary(queryClient)
    const privateQuote = videoStudioQueryKeys.quote(11, {
      token_id: 7,
      model: 'video-model',
      mode: 'text_to_video',
      prompt: 'test',
      parameters: {},
      reference_assets: [],
    })
    const privateModels = videoStudioQueryKeys.models(11, 7)

    queryClient.setQueryData(privateQuote, { request_hash: 'private' })
    queryClient.setQueryData(privateModels, ['private-model'])

    useAuthStore.getState().auth.reset()

    assert.equal(queryClient.getQueryData(privateQuote), undefined)
    assert.equal(queryClient.getQueryData(privateModels), undefined)
  })

  test('does not write a completed user A token mutation after switching to user B', async () => {
    const userAToken = videoStudioQueryKeys.token(11)
    const userBToken = videoStudioQueryKeys.token(22)
    const userAMissing = { status: 'missing' }
    const userBReady = { status: 'ready', token: { id: 22 } }
    queryClient.setQueryData(userAToken, userAMissing)
    queryClient.setQueryData(userBToken, userBReady)
    const userBObserver = new QueryObserver(queryClient, {
      queryKey: userBToken,
      queryFn: async () => userBReady,
      staleTime: Number.POSITIVE_INFINITY,
    })
    const unsubscribeObserver = userBObserver.subscribe(() => undefined)

    const context = captureVideoTokenMutationContext({ userId: 11 })
    const applied = await applyVideoTokenCreateSuccess(
      queryClient,
      readyVideoToken(17),
      context,
      22
    )

    assert.equal(Object.isFrozen(context), true)
    assert.equal(applied, false)
    assert.deepEqual(queryClient.getQueryData(userAToken), userAMissing)
    assert.deepEqual(queryClient.getQueryData(userBToken), userBReady)
    assert.deepEqual(userBObserver.getCurrentResult().data, userBReady)
    unsubscribeObserver()
  })

  test('caches the created key once for the active user', async () => {
    const tokenKey = videoStudioQueryKeys.token(11)
    const capability = readyVideoToken(17)
    queryClient.setQueryData(tokenKey, { status: 'missing' })
    const tokenObserver = new QueryObserver(queryClient, {
      queryKey: tokenKey,
      queryFn: async () => capability,
      staleTime: Number.POSITIVE_INFINITY,
    })
    const unsubscribeObserver = tokenObserver.subscribe(() => undefined)

    const applied = await applyVideoTokenCreateSuccess(
      queryClient,
      capability,
      captureVideoTokenMutationContext({ userId: 11 }),
      11
    )

    assert.equal(applied, true)
    assert.deepEqual(queryClient.getQueryData(tokenKey), capability)
    assert.deepEqual(tokenObserver.getCurrentResult().data, capability)
    unsubscribeObserver()
  })

  test('isolates model and sample catalogs by both user and bound key', () => {
    assert.notDeepEqual(
      videoStudioQueryKeys.models(11, 17),
      videoStudioQueryKeys.models(11, 18)
    )
    assert.notDeepEqual(
      videoStudioQueryKeys.samples(11, 17, { model: 'model-a' }),
      videoStudioQueryKeys.samples(11, 18, { model: 'model-a' })
    )
    assert.notDeepEqual(
      videoStudioQueryKeys.samples(11, 17, {
        model: 'model-a',
        category: 'people',
      }),
      videoStudioQueryKeys.samples(11, 17, {
        model: 'model-a',
        category: 'animals',
      })
    )
    assert.notDeepEqual(
      videoStudioQueryKeys.sample(11, 17, 9),
      videoStudioQueryKeys.sample(11, 18, 9)
    )
    assert.notDeepEqual(
      videoStudioQueryKeys.models(11, 17),
      videoStudioQueryKeys.models(22, 17)
    )
  })

  test('tracks concurrent pending token creation independently for each user', async () => {
    const variablesA = { userId: 11 }
    const variablesB = { userId: 22 }
    let finishMutationA: (value: VideoTokenCreateResult) => void = () =>
      undefined
    let finishMutationB: (value: VideoTokenCreateResult) => void = () =>
      undefined
    const mutationResultA = new Promise<VideoTokenCreateResult>((resolve) => {
      finishMutationA = resolve
    })
    const mutationResultB = new Promise<VideoTokenCreateResult>((resolve) => {
      finishMutationB = resolve
    })
    const mutationObserverA = new MutationObserver(queryClient, {
      mutationKey: videoStudioMutationKeys.createToken,
      mutationFn: async (_variables: typeof variablesA) => mutationResultA,
    })
    const mutationObserverB = new MutationObserver(queryClient, {
      mutationKey: videoStudioMutationKeys.createToken,
      mutationFn: async (_variables: typeof variablesB) => mutationResultB,
    })
    const pendingA = mutationObserverA.mutate(variablesA)
    const pendingB = mutationObserverB.mutate(variablesB)
    await Promise.resolve()

    assert.equal(queryClient.isMutating(videoTokenCreateMutationFilters(11)), 1)
    assert.equal(queryClient.isMutating(videoTokenCreateMutationFilters(22)), 1)

    finishMutationB(readyVideoToken(22))
    await pendingB
    assert.equal(queryClient.isMutating(videoTokenCreateMutationFilters(11)), 1)
    assert.equal(queryClient.isMutating(videoTokenCreateMutationFilters(22)), 0)

    finishMutationA(readyVideoToken(17))
    await pendingA
    assert.equal(queryClient.isMutating(videoTokenCreateMutationFilters(11)), 0)
  })

  test('blocks a remounted instance while an earlier same-scope create is pending', async () => {
    const variables = { userId: 11 }
    let finishMutation: (value: VideoTokenCreateResult) => void = () =>
      undefined
    const mutationResult = new Promise<VideoTokenCreateResult>((resolve) => {
      finishMutation = resolve
    })
    const oldInstanceMutation = new MutationObserver(queryClient, {
      mutationKey: videoStudioMutationKeys.createToken,
      mutationFn: async (_variables: typeof variables) => mutationResult,
    })
    const pendingMutation = oldInstanceMutation.mutate(variables)
    await Promise.resolve()
    oldInstanceMutation.reset()

    assert.equal(queryClient.isMutating(videoTokenCreateMutationFilters(11)), 1)
    assert.equal(canStartVideoTokenCreate(queryClient, 11), false)

    finishMutation(readyVideoToken(17))
    await pendingMutation
    assert.equal(canStartVideoTokenCreate(queryClient, 11), true)
  })
})

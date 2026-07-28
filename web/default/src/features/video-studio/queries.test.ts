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
  videoStudioMutationKeys,
  videoStudioQueryKeys,
  videoTokenCreateMutationFilters,
} from './queries'
import type { VideoTokenCreateResult } from './types'

const authUser = (id: number): AuthUser => ({
  id,
  username: `user-${id}`,
  role: 1,
})

const readyVideoToken = (id: number): VideoTokenCreateResult => ({
  required_group: 'Seedance 视频',
  has_usable_token: true,
  can_create: true,
  status: 'ready',
  token: { id, name: '视频工作室', group: 'Seedance 视频' },
  created: true,
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

  test('removes user A private data on an A to B switch without touching B or public catalogs', () => {
    useAuthStore.getState().auth.setUser(authUser(11))
    unsubscribe = installAuthQueryCacheBoundary(queryClient)
    const userAGenerations = videoStudioQueryKeys.generations(11, {})
    const userAAsset = videoStudioQueryKeys.asset(11, 101)
    const userAToken = videoStudioQueryKeys.token(11, 'video-model')
    const userBGenerations = videoStudioQueryKeys.generations(22, {})
    const publicModels = videoStudioQueryKeys.models()
    const publicSamples = videoStudioQueryKeys.samples({})

    queryClient.setQueryData(userAGenerations, ['user-a-generation'])
    queryClient.setQueryData(userAAsset, { id: 101 })
    queryClient.setQueryData(userAToken, { token: { id: 7 } })
    queryClient.setQueryData(userBGenerations, ['user-b-generation'])
    queryClient.setQueryData(publicModels, ['public-model'])
    queryClient.setQueryData(publicSamples, ['public-sample'])

    useAuthStore.getState().auth.setUser(authUser(22))

    assert.equal(queryClient.getQueryData(userAGenerations), undefined)
    assert.equal(queryClient.getQueryData(userAAsset), undefined)
    assert.equal(queryClient.getQueryData(userAToken), undefined)
    assert.deepEqual(queryClient.getQueryData(userBGenerations), [
      'user-b-generation',
    ])
    assert.deepEqual(queryClient.getQueryData(publicModels), ['public-model'])
    assert.deepEqual(queryClient.getQueryData(publicSamples), ['public-sample'])
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
    const publicModels = videoStudioQueryKeys.models()

    queryClient.setQueryData(privateQuote, { request_hash: 'private' })
    queryClient.setQueryData(publicModels, ['public-model'])

    useAuthStore.getState().auth.reset()

    assert.equal(queryClient.getQueryData(privateQuote), undefined)
    assert.deepEqual(queryClient.getQueryData(publicModels), ['public-model'])
  })

  test('does not write a completed user A token mutation after switching to user B', async () => {
    const userAModel = videoStudioQueryKeys.token(11, 'model-a')
    const userBModel = videoStudioQueryKeys.token(22, 'model-a')
    const userAMissing = { status: 'missing' }
    const userBReady = { status: 'ready', token: { id: 22 } }
    queryClient.setQueryData(userAModel, userAMissing)
    queryClient.setQueryData(userBModel, userBReady)
    const userBObserver = new QueryObserver(queryClient, {
      queryKey: userBModel,
      queryFn: async () => userBReady,
      staleTime: Number.POSITIVE_INFINITY,
    })
    const unsubscribeObserver = userBObserver.subscribe(() => undefined)

    const context = captureVideoTokenMutationContext({
      userId: 11,
      model: 'model-a',
    })
    const applied = await applyVideoTokenCreateSuccess(
      queryClient,
      readyVideoToken(17),
      context,
      22
    )

    assert.equal(Object.isFrozen(context), true)
    assert.equal(applied, false)
    assert.deepEqual(queryClient.getQueryData(userAModel), userAMissing)
    assert.deepEqual(queryClient.getQueryData(userBModel), userBReady)
    assert.deepEqual(userBObserver.getCurrentResult().data, userBReady)
    unsubscribeObserver()
  })

  test('clears stale capabilities for every model before caching the created token', async () => {
    const modelA = videoStudioQueryKeys.token(11, 'model-a')
    const modelB = videoStudioQueryKeys.token(11, 'model-b')
    const capability = readyVideoToken(17)
    queryClient.setQueryData(modelA, { status: 'missing' })
    queryClient.setQueryData(modelB, { status: 'missing' })
    const modelBObserver = new QueryObserver(queryClient, {
      queryKey: modelB,
      queryFn: async () => capability,
      staleTime: Number.POSITIVE_INFINITY,
    })
    const unsubscribeObserver = modelBObserver.subscribe(() => undefined)

    const applied = await applyVideoTokenCreateSuccess(
      queryClient,
      capability,
      captureVideoTokenMutationContext({ userId: 11, model: 'model-b' }),
      11
    )

    assert.equal(applied, true)
    assert.equal(queryClient.getQueryData(modelA), undefined)
    assert.deepEqual(queryClient.getQueryData(modelB), capability)
    assert.deepEqual(modelBObserver.getCurrentResult().data, capability)
    unsubscribeObserver()
  })

  test('refetches the active model after another model finishes creating a token', async () => {
    const modelA = videoStudioQueryKeys.token(11, 'model-a')
    const modelB = videoStudioQueryKeys.token(11, 'model-b')
    const modelBReady = readyVideoToken(23)
    let modelBFetches = 0
    queryClient.setQueryData(modelA, { status: 'missing' })
    queryClient.setQueryData(modelB, { status: 'missing' })
    const modelBObserver = new QueryObserver(queryClient, {
      queryKey: modelB,
      queryFn: async () => {
        modelBFetches += 1
        return modelBReady
      },
      staleTime: Number.POSITIVE_INFINITY,
    })
    const unsubscribeObserver = modelBObserver.subscribe(() => undefined)

    const applied = await applyVideoTokenCreateSuccess(
      queryClient,
      readyVideoToken(17),
      captureVideoTokenMutationContext({ userId: 11, model: 'model-a' }),
      11
    )

    assert.equal(applied, true)
    assert.equal(modelBFetches, 1)
    assert.deepEqual(modelBObserver.getCurrentResult().data, modelBReady)
    unsubscribeObserver()
  })

  test('tracks concurrent pending token creation independently for each scope', async () => {
    const variablesA = { userId: 11, model: 'model-a' }
    const variablesB = { userId: 22, model: 'model-b' }
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

    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(11, 'model-a')),
      1
    )
    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(22, 'model-b')),
      1
    )

    finishMutationB(readyVideoToken(22))
    await pendingB
    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(11, 'model-a')),
      1
    )
    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(22, 'model-b')),
      0
    )

    finishMutationA(readyVideoToken(17))
    await pendingA
    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(11, 'model-a')),
      0
    )
  })

  test('blocks a remounted instance while an earlier same-scope create is pending', async () => {
    const variables = { userId: 11, model: 'model-a' }
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

    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(11, 'model-a')),
      1
    )
    assert.equal(canStartVideoTokenCreate(queryClient, 11, 'model-a'), false)

    finishMutation(readyVideoToken(17))
    await pendingMutation
    assert.equal(canStartVideoTokenCreate(queryClient, 11, 'model-a'), true)
  })
})

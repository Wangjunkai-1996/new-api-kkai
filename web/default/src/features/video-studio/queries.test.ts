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
  effective_models: ['model-a', 'model-b'],
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

    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(11)),
      1
    )
    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(22)),
      1
    )

    finishMutationB(readyVideoToken(22))
    await pendingB
    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(11)),
      1
    )
    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(22)),
      0
    )

    finishMutationA(readyVideoToken(17))
    await pendingA
    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(11)),
      0
    )
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

    assert.equal(
      queryClient.isMutating(videoTokenCreateMutationFilters(11)),
      1
    )
    assert.equal(canStartVideoTokenCreate(queryClient, 11), false)

    finishMutation(readyVideoToken(17))
    await pendingMutation
    assert.equal(canStartVideoTokenCreate(queryClient, 11), true)
  })
})

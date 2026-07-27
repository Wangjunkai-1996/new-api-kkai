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

import { QueryClient } from '@tanstack/react-query'

import { installAuthQueryCacheBoundary } from '@/lib/auth-query-cache'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'

import { videoStudioQueryKeys } from './queries'

const authUser = (id: number): AuthUser => ({
  id,
  username: `user-${id}`,
  role: 1,
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
    const userBGenerations = videoStudioQueryKeys.generations(22, {})
    const publicModels = videoStudioQueryKeys.models()
    const publicSamples = videoStudioQueryKeys.samples({})

    queryClient.setQueryData(userAGenerations, ['user-a-generation'])
    queryClient.setQueryData(userAAsset, { id: 101 })
    queryClient.setQueryData(userBGenerations, ['user-b-generation'])
    queryClient.setQueryData(publicModels, ['public-model'])
    queryClient.setQueryData(publicSamples, ['public-sample'])

    useAuthStore.getState().auth.setUser(authUser(22))

    assert.equal(queryClient.getQueryData(userAGenerations), undefined)
    assert.equal(queryClient.getQueryData(userAAsset), undefined)
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
})

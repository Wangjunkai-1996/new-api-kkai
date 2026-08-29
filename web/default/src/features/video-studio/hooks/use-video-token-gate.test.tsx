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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import { StrictMode, type PropsWithChildren } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { useAuthStore, type AuthUser } from '@/stores/auth-store'

import { videoStudioQueryKeys } from '../queries'
import type {
  VideoQuoteRequest,
  VideoTokenCapability,
  VideoTokenCreateResult,
} from '../types'
import { useVideoTokenGate } from './use-video-token-gate'

const apiMocks = vi.hoisted(() => ({
  createVideoToken: vi.fn(),
  getVideoTokenCapability: vi.fn(),
}))

vi.mock('../api', async (importOriginal) => {
  const original = await importOriginal<typeof import('../api')>()
  return {
    ...original,
    createVideoToken: apiMocks.createVideoToken,
    getVideoTokenCapability: apiMocks.getVideoTokenCapability,
  }
})

const authUser = (id: number): AuthUser => ({
  id,
  username: `user-${String(id)}`,
  role: 1,
})

const readyCapability = (tokenId: number): VideoTokenCreateResult => ({
  required_group: 'Seedance 视频',
  has_usable_token: true,
  can_create: true,
  effective_models: ['video-model'],
  status: 'ready',
  token: {
    id: tokenId,
    name: '视频工作室',
    group: 'Seedance 视频',
  },
  created: true,
})

const videoWorkspaceQueryKeys = (userId: number, tokenId: number) => {
  const quoteRequest: VideoQuoteRequest = {
    token_id: tokenId,
    model: 'video-model',
    mode: 'text_to_video',
    prompt: 'test',
    parameters: {},
    reference_assets: [],
  }
  return [
    videoStudioQueryKeys.models(userId, tokenId),
    videoStudioQueryKeys.samples(userId, tokenId, {}),
    videoStudioQueryKeys.sample(userId, tokenId, 7),
    videoStudioQueryKeys.quote(userId, quoteRequest),
  ]
}

const deferred = <Value,>() => {
  let resolve: (value: Value) => void = () => undefined
  const promise = new Promise<Value>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

describe('video token fast-path gate', () => {
  let queryClient: QueryClient

  const wrapper = (props: PropsWithChildren) => (
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        {props.children}
      </QueryClientProvider>
    </StrictMode>
  )

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    useAuthStore.getState().auth.setUser(authUser(11))
  })

  afterEach(() => {
    useAuthStore.getState().auth.reset()
    queryClient.clear()
  })

  test('ensures once in StrictMode without waiting for a capability GET', async () => {
    apiMocks.createVideoToken.mockResolvedValue(readyCapability(17))

    const { result } = renderHook(() => useVideoTokenGate(), { wrapper })

    await waitFor(() => expect(result.current.tokenId).toBe(17))
    expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(1)
    expect(apiMocks.getVideoTokenCapability).not.toHaveBeenCalled()
    expect(result.current.preparing).toBe(false)
  })

  test('preserves workspace caches on the initial fast-path ensure', async () => {
    apiMocks.createVideoToken.mockResolvedValue(readyCapability(17))
    const workspaceKeys = videoWorkspaceQueryKeys(11, 17)
    for (const key of workspaceKeys) queryClient.setQueryData(key, 'cached')

    const { result } = renderHook(() => useVideoTokenGate(), { wrapper })

    await waitFor(() => expect(result.current.tokenId).toBe(17))
    expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(1)
    for (const key of workspaceKeys) {
      expect(queryClient.getQueryData(key)).toBe('cached')
    }
  })

  test('offers one explicit retry after an automatic ensure failure', async () => {
    apiMocks.createVideoToken
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce(readyCapability(18))

    const { result } = renderHook(() => useVideoTokenGate(), { wrapper })

    await waitFor(() => {
      expect(result.current.createError).toBe(
        'videoStudio.workspace.prepareFailedDescription'
      )
    })
    expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(1)
    expect(result.current.preparing).toBe(false)

    act(() => result.current.retry())

    await waitFor(() => expect(result.current.tokenId).toBe(18))
    expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(2)
    expect(apiMocks.getVideoTokenCapability).not.toHaveBeenCalled()
  })

  test('does not show the previous account failure after switching accounts', async () => {
    const userBResult = deferred<VideoTokenCreateResult>()
    apiMocks.createVideoToken
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockReturnValueOnce(userBResult.promise)

    const { result } = renderHook(() => useVideoTokenGate(), { wrapper })

    await waitFor(() => {
      expect(result.current.createError).toBe(
        'videoStudio.workspace.prepareFailedDescription'
      )
    })

    act(() => useAuthStore.getState().auth.setUser(authUser(22)))

    expect(result.current.createError).toBeNull()
    expect(result.current.preparing).toBe(true)
    await waitFor(() =>
      expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(2)
    )

    userBResult.resolve(readyCapability(22))
    await waitFor(() => expect(result.current.tokenId).toBe(22))
  })

  test('keeps a terminal ensure result actionable without a GET loop', async () => {
    const terminalCapability: VideoTokenCreateResult = {
      required_group: 'Seedance 视频',
      has_usable_token: false,
      can_create: false,
      status: 'group_unavailable',
      token: null,
      created: false,
    }
    apiMocks.createVideoToken.mockResolvedValue(terminalCapability)

    const { result } = renderHook(() => useVideoTokenGate(), { wrapper })

    await waitFor(() => {
      expect(result.current.access?.kind).toBe('group-unavailable')
    })
    expect(result.current.gateAction).toBe('create')
    expect(result.current.preparing).toBe(false)
    expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(1)
    expect(apiMocks.getVideoTokenCapability).not.toHaveBeenCalled()
  })

  test('ensures again when an invalid-token recheck reports no usable token', async () => {
    const missingCapability: VideoTokenCapability = {
      required_group: 'Seedance 视频',
      has_usable_token: false,
      can_create: true,
      status: 'missing',
      token: null,
    }
    apiMocks.createVideoToken
      .mockResolvedValueOnce(readyCapability(17))
      .mockResolvedValueOnce(readyCapability(18))
    apiMocks.getVideoTokenCapability.mockResolvedValue(missingCapability)

    const { result } = renderHook(() => useVideoTokenGate(), { wrapper })
    await waitFor(() => expect(result.current.tokenId).toBe(17))

    act(() => {
      result.current.blockAndRecheck('token-invalid')
    })

    await waitFor(() => expect(result.current.tokenId).toBe(18))
    expect(apiMocks.getVideoTokenCapability).toHaveBeenCalledTimes(1)
    expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(2)
  })

  test('rechecks a token once until fresh models mark it healthy', async () => {
    apiMocks.createVideoToken.mockResolvedValue(readyCapability(17))
    apiMocks.getVideoTokenCapability.mockResolvedValue(readyCapability(17))
    const { result } = renderHook(() => useVideoTokenGate(), { wrapper })
    await waitFor(() => expect(result.current.tokenId).toBe(17))

    const recoveryKeys = videoWorkspaceQueryKeys(11, 17)
    for (const key of recoveryKeys) queryClient.setQueryData(key, 'stale-error')

    act(() => {
      result.current.blockAndRecheck('token-invalid')
      result.current.blockAndRecheck('token-invalid')
    })

    await waitFor(() => expect(result.current.tokenId).toBe(17))
    expect(apiMocks.getVideoTokenCapability).toHaveBeenCalledTimes(1)
    for (const key of recoveryKeys) {
      expect(queryClient.getQueryData(key)).toBeUndefined()
    }

    act(() => {
      result.current.blockAndRecheck('token-invalid')
    })
    expect(result.current.access?.kind).toBe('invalid')
    expect(apiMocks.getVideoTokenCapability).toHaveBeenCalledTimes(1)

    act(() => result.current.retry())
    await waitFor(() => expect(result.current.tokenId).toBe(17))
    expect(apiMocks.getVideoTokenCapability).toHaveBeenCalledTimes(2)

    act(() => {
      result.current.markTokenHealthy(17)
      result.current.blockAndRecheck('token-invalid')
    })
    await waitFor(() =>
      expect(apiMocks.getVideoTokenCapability).toHaveBeenCalledTimes(3)
    )
  })

  test.each([
    'group-unavailable',
    'limit-reached',
    'models-unavailable',
  ] as const)(
    'clears stale workspace queries when a %s blocker recovers through POST',
    async (errorKind) => {
      apiMocks.createVideoToken
        .mockResolvedValueOnce(readyCapability(17))
        .mockResolvedValueOnce(readyCapability(17))

      const { result } = renderHook(() => useVideoTokenGate(), { wrapper })
      await waitFor(() => expect(result.current.tokenId).toBe(17))

      const recoveryKeys = videoWorkspaceQueryKeys(11, 17)
      for (const key of recoveryKeys) {
        queryClient.setQueryData(key, 'stale-error')
      }

      act(() => {
        result.current.blockAndRecheck(errorKind)
      })

      expect(result.current.access?.kind).toBe(errorKind)
      expect(result.current.gateAction).toBe('create')
      expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(1)
      expect(apiMocks.getVideoTokenCapability).not.toHaveBeenCalled()

      act(() => result.current.retry())

      await waitFor(() => expect(result.current.tokenId).toBe(17))
      expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(2)
      expect(apiMocks.getVideoTokenCapability).not.toHaveBeenCalled()
      for (const key of recoveryKeys) {
        expect(queryClient.getQueryData(key)).toBeUndefined()
      }
    }
  )

  test('ignores an old account response while ensuring the new account', async () => {
    const userAResult = deferred<VideoTokenCreateResult>()
    const userBResult = deferred<VideoTokenCreateResult>()
    apiMocks.createVideoToken
      .mockReturnValueOnce(userAResult.promise)
      .mockReturnValueOnce(userBResult.promise)

    const { result } = renderHook(() => useVideoTokenGate(), { wrapper })
    await waitFor(() =>
      expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(1)
    )

    act(() => useAuthStore.getState().auth.setUser(authUser(22)))
    await waitFor(() =>
      expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(2)
    )

    userAResult.resolve(readyCapability(17))
    await waitFor(() => {
      expect(
        queryClient.getQueryData<VideoTokenCapability>(
          videoStudioQueryKeys.token(11)
        )
      ).toBeUndefined()
    })

    userBResult.resolve(readyCapability(22))
    await waitFor(() => expect(result.current.tokenId).toBe(22))
    expect(
      queryClient.getQueryData<VideoTokenCapability>(
        videoStudioQueryKeys.token(11)
      )
    ).toBeUndefined()

    apiMocks.createVideoToken.mockResolvedValueOnce(readyCapability(33))
    act(() => useAuthStore.getState().auth.setUser(authUser(11)))

    await waitFor(() => expect(result.current.tokenId).toBe(33))
    expect(apiMocks.createVideoToken).toHaveBeenCalledTimes(3)
  })
})

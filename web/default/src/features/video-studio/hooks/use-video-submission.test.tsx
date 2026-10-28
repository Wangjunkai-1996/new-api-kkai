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
import { webcrypto } from 'node:crypto'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, renderHook } from '@testing-library/react'
import type { AxiosAdapter } from 'axios'
import type { PropsWithChildren } from 'react'
import { useForm } from 'react-hook-form'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type {
  VideoComposerValues,
  VideoModelProfile,
  VideoQuoteRequest,
} from '../types'
import { getVideoSubmissionRequestKey } from '../video-domain'
import { useVideoSubmission } from './use-video-submission'
import type { VideoTokenGateState } from './use-video-token-gate'

const values: VideoComposerValues = {
  model_profile_id: 1,
  mode: 'text_to_video',
  prompt: 'A scene',
  reference_asset_ids: [],
  parameters: {},
}
const profile: VideoModelProfile = {
  id: 1,
  model: 'video',
  display_name: 'Video',
  description: '',
  provider_label: '',
  specification_version: 1,
  specification: { version: 1, modes: ['text_to_video'], parameters: [] },
  default_parameters: {},
  enabled: true,
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
}
const request: VideoQuoteRequest = {
  token_id: 17,
  model: 'video',
  mode: 'text_to_video',
  prompt: 'A scene',
  reference_assets: [],
  parameters: {},
}
const gate: VideoTokenGateState = {
  access: {
    kind: 'ready',
    requiredGroup: 'video',
    tokenId: 17,
    tokenName: 'Video',
  },
  tokenId: 17,
  effectiveGroup: 'video',
  requiredGroup: 'video',
  checking: false,
  preparing: false,
  checkFailed: false,
  gateAction: 'none',
  actionAvailable: false,
  queryFetching: false,
  creating: false,
  createError: null,
  blockAndRecheck: vi.fn(() => false),
  markTokenHealthy: vi.fn(),
  retry: vi.fn(),
  openOrRetry: vi.fn(),
  createAndContinue: vi.fn(async () => undefined),
}
const originalAdapter = api.defaults.adapter
let interceptorId: number | undefined
let client: QueryClient
function Wrapper(props: PropsWithChildren) {
  return (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
}

beforeEach(() => {
  vi.stubGlobal('crypto', webcrypto)
  localStorage.clear()
  useAuthStore.getState().auth.setUser({ id: 11, username: 'eleven', role: 1 })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(() => {
  cleanup()
  client.clear()
  api.defaults.adapter = originalAdapter
  if (interceptorId !== undefined) api.interceptors.request.eject(interceptorId)
  interceptorId = undefined
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test.each(['reference readiness', 'quote readiness', 'model profile'])(
  'a changed %s after the request enters async interceptors prevents transport dispatch',
  async (change) => {
    const transport = vi.fn<AxiosAdapter>(async (config) => ({
      status: 200,
      statusText: 'OK',
      data: { task_id: 'task_unexpected' },
      headers: {},
      config,
    }))
    api.defaults.adapter = transport
    let entered: () => void = () => {}
    const reachedInterceptor = new Promise<void>((resolve) => {
      entered = resolve
    })
    let release: () => void = () => {}
    interceptorId = api.interceptors.request.use(async (config) => {
      entered()
      await new Promise<void>((resolve) => {
        release = resolve
      })
      return config
    })
    const expiresAt = Math.floor(Date.now() / 1000) + 600
    const { result, rerender } = renderHook(
      (props) => {
        const form = useForm<VideoComposerValues>({ defaultValues: values })
        return useVideoSubmission({
          form,
          selectedProfile: { ...profile, id: props.profileId },
          videoTokenGate: gate,
          quote: {
            quota: 100,
            request_hash: 'a'.repeat(64),
            expires_at: expiresAt,
          },
          quoteRequest: request,
          quoteKey: `${'a'.repeat(64)}:${expiresAt}`,
          quoteRequestKey: getVideoSubmissionRequestKey(request),
          quoteReady: props.quoteReady,
          referencesReady: props.referencesReady,
          refetchQuote: vi.fn(async () => undefined),
          onQuoteInvalidated: vi.fn(),
        })
      },
      {
        wrapper: Wrapper,
        initialProps: { profileId: 1, quoteReady: true, referencesReady: true },
      }
    )
    let submission: Promise<void> | undefined
    await act(async () => {
      submission = result.current.submit(values)
      await reachedInterceptor
    })
    expect(
      JSON.parse(localStorage.getItem('video-studio-submissions:user:11') ?? '')
        .records[0].state
    ).toBe('sending')
    rerender({
      profileId: change === 'model profile' ? 2 : 1,
      quoteReady: change !== 'quote readiness',
      referencesReady: change !== 'reference readiness',
    })
    await act(async () => {
      release()
      await submission
    })
    expect(transport).not.toHaveBeenCalled()
    expect(result.current.error).toContain('input or access changed')
    expect(
      JSON.parse(localStorage.getItem('video-studio-submissions:user:11') ?? '')
        .records
    ).toEqual([])
  }
)

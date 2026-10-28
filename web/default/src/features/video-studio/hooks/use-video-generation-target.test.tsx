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
import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import type { VideoGeneration } from '../types'
import { useVideoGenerationTarget } from './use-video-generation-target'

const item = (id: number): VideoGeneration => ({
  id,
  task_id: `task_${id}`,
  model_profile_id: 1,
  model: 'video',
  mode: 'text_to_video',
  prompt: 'A video',
  parameters: {},
  status: 'ready',
  progress: '',
  quota: 0,
  created_at: 1,
  updated_at: 1,
})
const query = {
  items: [item(2), item(1)],
  targetGeneration: undefined as VideoGeneration | undefined,
  targetError: null as Error | null,
  targetUnavailable: false,
  targetDiscoveryExpired: false,
  isError: false,
  isLoading: false,
  isSuccess: true,
  retryTargetDiscovery: () => undefined,
}
afterEach(cleanup)

describe('video target selection', () => {
  test('the first successful latest result remains selected after background additions', () => {
    const { result, rerender } = renderHook(
      (items: VideoGeneration[]) =>
        useVideoGenerationTarget({ generationsQuery: { ...query, items } }),
      { initialProps: query.items }
    )
    expect(result.current.selectedId).toBe(2)
    rerender([item(3), item(2), item(1)])
    expect(result.current.selectedId).toBe(2)
  })

  test('new background results do not replace an explicit user selection', () => {
    useAuthStore.setState((state) => ({
      auth: { ...state.auth, user: { id: 1, username: 'test', role: 1 } },
    }))
    const { result, rerender } = renderHook(
      (items: VideoGeneration[]) =>
        useVideoGenerationTarget({ generationsQuery: { ...query, items } }),
      { initialProps: query.items }
    )
    act(() => result.current.selectGeneration(1))
    rerender([item(3), item(2), item(1)])
    expect(result.current.selectedId).toBe(1)
  })

  test('removing the selection accepts one fallback and does not follow later head additions', () => {
    const { result, rerender } = renderHook(
      (items: VideoGeneration[]) =>
        useVideoGenerationTarget({ generationsQuery: { ...query, items } }),
      { initialProps: query.items }
    )
    expect(result.current.selectedId).toBe(2)
    rerender([item(1)])
    expect(result.current.selectedId).toBe(1)
    rerender([item(3), item(1)])
    expect(result.current.selectedId).toBe(1)
  })

  test('an explicit unavailable or mismatched target never falls back to another video', () => {
    const { result, rerender } = renderHook(
      (task: string) =>
        useVideoGenerationTarget({
          generationsQuery: { ...query, targetGeneration: item(1) },
          targetGenerationId: 1,
          targetTaskId: task,
        }),
      { initialProps: 'task_other' }
    )
    expect(result.current.status).toBe('unavailable')
    expect(result.current.generation).toBeNull()
    rerender('task_1')
    expect(result.current.selectedId).toBe(1)
  })

  test('a transient error keeps the target snapshot but permission evidence hides it', () => {
    const { result, rerender } = renderHook(
      (unavailable: boolean) =>
        useVideoGenerationTarget({
          generationsQuery: {
            ...query,
            targetGeneration: item(1),
            targetError: new Error('Offline'),
            targetUnavailable: unavailable,
          },
          targetGenerationId: 1,
        }),
      { initialProps: false }
    )
    expect(result.current.generation?.id).toBe(1)
    expect(result.current.isStale).toBe(true)
    rerender(true)
    expect(result.current.status).toBe('unavailable')
    expect(result.current.generation).toBeNull()
  })

  test('discovery expiry is missing, never failure or an unrelated fallback result', () => {
    const { result } = renderHook(() =>
      useVideoGenerationTarget({
        generationsQuery: { ...query, targetDiscoveryExpired: true },
        targetTaskId: 'task_not_found',
      })
    )
    expect(result.current.status).toBe('missing')
    expect(result.current.generation).toBeNull()
  })
})

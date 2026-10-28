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
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import type { VideoGeneration } from '@/features/video-studio/types'
import { useAuthStore } from '@/stores/auth-store'

import {
  useVideoStudioSubmissionStore,
  type VideoSubmissionRecord,
} from './video-studio-submission-store'

const operation: VideoSubmissionRecord = {
  version: 1,
  userId: 11,
  operationId: 'operation-1',
  requestFingerprint: 'a'.repeat(64),
  createdAt: 1,
  updatedAt: 1,
  state: 'sending',
}
const storageKey = 'video-studio-submissions:user:11'
beforeEach(() => {
  localStorage.clear()
  useAuthStore.getState().auth.setUser({ id: 11, username: 'eleven', role: 1 })
  useVideoStudioSubmissionStore.getState().hydrate(11)
})
afterEach(() => {
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})

test('refresh preserves sending and prevents equivalent requests without storing request content', () => {
  const store = useVideoStudioSubmissionStore.getState()
  store.begin(operation)
  store.hydrate(11)
  expect(useVideoStudioSubmissionStore.getState().records).toEqual([operation])
  expect(() => store.begin({ ...operation, operationId: 'new-key' })).toThrow(
    'unresolved'
  )
  expect(JSON.parse(localStorage.getItem(storageKey) ?? '').records).toEqual([
    operation,
  ])
  expect(localStorage.getItem(storageKey)).not.toContain('prompt')
})
test('unknown stays guarded beyond 24 hours and only same-task success closes it', () => {
  const store = useVideoStudioSubmissionStore.getState()
  store.begin(operation)
  store.markUnknown(11, operation.operationId, 'task_original')
  const generation = { id: 4, task_id: 'task_original' } as VideoGeneration
  for (const status of ['queued', 'processing', 'failed'] as const) {
    store.confirmGenerations(11, [{ ...generation, status }])
    expect(useVideoStudioSubmissionStore.getState().records[0].state).toBe(
      'unknown'
    )
  }
  store.confirmGenerations(11, [
    { ...generation, task_id: 'task_other', status: 'ready' },
  ])
  expect(useVideoStudioSubmissionStore.getState().records[0].state).toBe(
    'unknown'
  )
  store.confirmGenerations(11, [{ ...generation, status: 'archiving' }])
  expect(useVideoStudioSubmissionStore.getState().records[0]).toMatchObject({
    state: 'accepted',
    generationId: 4,
  })
})
test('unreadable or invalid storage is not overwritten as empty', () => {
  localStorage.setItem(storageKey, '{broken')
  const store = useVideoStudioSubmissionStore.getState()
  store.hydrate(11)
  expect(useVideoStudioSubmissionStore.getState().storageError).toBe(true)
  expect(() => store.begin(operation)).toThrow('storage is unavailable')
  expect(localStorage.getItem(storageKey)).toBe('{broken')
})
test('failed writes block new operations and retain the in-memory guard on resolution', () => {
  const store = useVideoStudioSubmissionStore.getState()
  store.begin(operation)
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('full')
  })
  expect(() =>
    store.resolve(11, operation.operationId, 'task_success')
  ).toThrow('storage is unavailable')
  expect(useVideoStudioSubmissionStore.getState().records).toEqual([operation])
  expect(() =>
    store.begin({
      ...operation,
      operationId: 'second',
      requestFingerprint: 'b'.repeat(64),
    })
  ).toThrow('storage is unavailable')
  expect(useVideoStudioSubmissionStore.getState().records).toHaveLength(1)
})
test('late evidence remains owned by the original account', () => {
  const store = useVideoStudioSubmissionStore.getState()
  store.begin(operation)
  useAuthStore
    .getState()
    .auth.setUser({ id: 22, username: 'twenty-two', role: 1 })
  store.resolve(11, operation.operationId, 'task_late')
  expect(useVideoStudioSubmissionStore.getState().ownerId).toBe(22)
  expect(useVideoStudioSubmissionStore.getState().records).toEqual([])
  useAuthStore.getState().auth.setUser({ id: 11, username: 'eleven', role: 1 })
  expect(useVideoStudioSubmissionStore.getState().records[0]).toMatchObject({
    taskId: 'task_late',
    state: 'accepted',
  })
})

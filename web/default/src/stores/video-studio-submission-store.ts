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
import { z } from 'zod'
import { create } from 'zustand'

import { videoTaskIdSchema } from '@/features/video-studio/schemas'
import type { VideoGeneration } from '@/features/video-studio/types'
import { useAuthStore } from '@/stores/auth-store'

const submissionRecordSchema = z
  .object({
    version: z.literal(1),
    userId: z.number().int().positive().safe(),
    operationId: z.string().min(1).max(128),
    requestFingerprint: z.string().regex(/^[0-9a-f]{64}$/),
    createdAt: z.number().int().positive().safe(),
    updatedAt: z.number().int().positive().safe(),
    state: z.enum(['sending', 'unknown', 'accepted']),
    taskId: videoTaskIdSchema.optional(),
    generationId: z.number().int().positive().safe().optional(),
  })
  .strict()
  .refine(
    (record) => record.state !== 'accepted' || Boolean(record.taskId),
    'Accepted submissions require a task identity'
  )
const storageSchema = z
  .object({ version: z.literal(1), records: z.array(submissionRecordSchema) })
  .strict()
export type VideoSubmissionRecord = z.infer<typeof submissionRecordSchema>

export class VideoSubmissionStorageError extends Error {
  constructor() {
    super('Video recovery storage is unavailable')
    this.name = 'VideoSubmissionStorageError'
  }
}

const storageKey = (ownerId: number): string =>
  `video-studio-submissions:user:${ownerId}`

const readRecords = (ownerId: number): VideoSubmissionRecord[] => {
  if (!Number.isSafeInteger(ownerId) || ownerId <= 0) {
    throw new VideoSubmissionStorageError()
  }
  const raw = window.localStorage.getItem(storageKey(ownerId))
  if (raw === null) return []
  const parsed = storageSchema.parse(JSON.parse(raw))
  if (parsed.records.some((record) => record.userId !== ownerId)) {
    throw new VideoSubmissionStorageError()
  }
  if (
    new Set(parsed.records.map((record) => record.operationId)).size !==
    parsed.records.length
  ) {
    throw new VideoSubmissionStorageError()
  }
  return parsed.records
}

type SubmissionState = {
  ownerId: number
  records: VideoSubmissionRecord[]
  storageError: boolean
  hydrate: (ownerId: number) => void
  begin: (record: VideoSubmissionRecord) => void
  markUnknown: (ownerId: number, operationId: string, taskId?: string) => void
  resolve: (
    ownerId: number,
    operationId: string,
    taskId?: string,
    generationId?: number
  ) => void
  confirmGenerations: (ownerId: number, generations: VideoGeneration[]) => void
}

// A storage failure must not remove the in-memory guard or overwrite unreadable data.
const persistRecords = (
  ownerId: number,
  records: VideoSubmissionRecord[]
): void => {
  const value = storageSchema.parse({ version: 1, records })
  window.localStorage.setItem(storageKey(ownerId), JSON.stringify(value))
  if (useAuthStore.getState().auth.user?.id === ownerId) {
    useVideoStudioSubmissionStore.setState({
      ownerId,
      records: value.records,
      storageError: false,
    })
  }
}

const storageFailure = (ownerId: number): never => {
  if (useAuthStore.getState().auth.user?.id === ownerId) {
    useVideoStudioSubmissionStore.setState({ ownerId, storageError: true })
  }
  throw new VideoSubmissionStorageError()
}

export const useVideoStudioSubmissionStore = create<SubmissionState>(() => ({
  ownerId: 0,
  records: [],
  storageError: false,
  hydrate: (ownerId) => {
    if (ownerId <= 0) {
      useVideoStudioSubmissionStore.setState({
        ownerId: 0,
        records: [],
        storageError: false,
      })
      return
    }
    try {
      const records = readRecords(ownerId)
      useVideoStudioSubmissionStore.setState({
        ownerId,
        records,
        storageError: false,
      })
    } catch {
      const state = useVideoStudioSubmissionStore.getState()
      useVideoStudioSubmissionStore.setState({
        ownerId,
        records: state.ownerId === ownerId ? state.records : [],
        storageError: true,
      })
    }
  },
  begin: (record) => {
    try {
      const records = readRecords(record.userId)
      if (
        records.some(
          (existing) =>
            existing.state !== 'accepted' &&
            existing.requestFingerprint === record.requestFingerprint
        )
      ) {
        throw new Error('Equivalent video submission is unresolved')
      }
      const pending = records.filter(
        (existing) => existing.state !== 'accepted'
      )
      if (pending.length >= 100) throw new VideoSubmissionStorageError()
      const accepted = records
        .filter((existing) => existing.state === 'accepted')
        .slice(-19)
      persistRecords(record.userId, [...pending, ...accepted, record])
    } catch (error) {
      if (
        error instanceof Error &&
        error.message === 'Equivalent video submission is unresolved'
      ) {
        throw error
      }
      storageFailure(record.userId)
    }
  },
  markUnknown: (ownerId, operationId, taskId) => {
    try {
      const records = readRecords(ownerId)
      persistRecords(
        ownerId,
        records.map((record) =>
          record.operationId === operationId
            ? {
                ...record,
                state: 'unknown',
                updatedAt: Date.now(),
                ...(taskId ? { taskId } : {}),
              }
            : record
        )
      )
    } catch {
      storageFailure(ownerId)
    }
  },
  resolve: (ownerId, operationId, taskId, generationId) => {
    try {
      const records = readRecords(ownerId)
      const next = taskId
        ? records.map((record) =>
            record.operationId === operationId
              ? {
                  ...record,
                  state: 'accepted' as const,
                  taskId,
                  generationId,
                  updatedAt: Date.now(),
                }
              : record
          )
        : records.filter((record) => record.operationId !== operationId)
      persistRecords(ownerId, next)
    } catch {
      storageFailure(ownerId)
    }
  },
  confirmGenerations: (ownerId, generations) => {
    const state = useVideoStudioSubmissionStore.getState()
    if (state.ownerId !== ownerId || state.storageError) return
    for (const record of state.records) {
      if (record.state === 'accepted' || !record.taskId) continue
      const generation = generations.find(
        (item) =>
          item.task_id === record.taskId &&
          (item.status === 'archiving' || item.status === 'ready')
      )
      if (generation) {
        state.resolve(
          ownerId,
          record.operationId,
          generation.task_id,
          generation.id
        )
      }
    }
  },
}))

useAuthStore.subscribe((state, previous) => {
  if (state.auth.user?.id !== previous.auth.user?.id) {
    useVideoStudioSubmissionStore.getState().hydrate(state.auth.user?.id ?? 0)
  }
})
if (typeof window !== 'undefined') {
  window.addEventListener('storage', (event) => {
    const ownerId = useAuthStore.getState().auth.user?.id ?? 0
    if (
      ownerId > 0 &&
      (event.key === null || event.key === storageKey(ownerId))
    ) {
      useVideoStudioSubmissionStore.getState().hydrate(ownerId)
    }
  })
}

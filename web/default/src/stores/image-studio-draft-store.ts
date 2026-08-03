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
import { create } from 'zustand'

import { imageComposerSchema } from '@/features/image-studio/schemas'
import type { ImageComposerValues } from '@/features/image-studio/types'

type PendingImageStudioSubmission = {
  idempotencyKey: string
  expiresAt: number
}

const pendingSubmissionTTL = 24 * 60 * 60 * 1000
const requestFingerprintPattern = /^[a-f0-9]{64}$/

const pendingSubmissionPrefix = (userId: number): string =>
  `image-studio-pending:user:${String(userId)}:request:`

const pendingSubmissionStorageKey = (
  userId: number,
  requestFingerprint: string
): string => `${pendingSubmissionPrefix(userId)}${requestFingerprint}`

const readPendingSubmission = (
  key: string,
  now: number
): PendingImageStudioSubmission | null => {
  const raw = window.localStorage.getItem(key)
  if (!raw) return null
  try {
    const candidate = JSON.parse(raw) as unknown
    if (!candidate || typeof candidate !== 'object') {
      window.localStorage.removeItem(key)
      return null
    }
    const record = candidate as Record<string, unknown>
    if (
      typeof record.idempotencyKey !== 'string' ||
      record.idempotencyKey.length === 0 ||
      record.idempotencyKey.length > 128 ||
      typeof record.expiresAt !== 'number' ||
      !Number.isSafeInteger(record.expiresAt) ||
      record.expiresAt <= now
    ) {
      window.localStorage.removeItem(key)
      return null
    }
    return {
      idempotencyKey: record.idempotencyKey,
      expiresAt: record.expiresAt,
    }
  } catch {
    window.localStorage.removeItem(key)
    return null
  }
}

const prunePendingSubmissions = (userId: number, now: number): void => {
  const prefix = pendingSubmissionPrefix(userId)
  const keys: string[] = []
  for (let index = 0; index < window.localStorage.length; index += 1) {
    const key = window.localStorage.key(index)
    if (key?.startsWith(prefix)) keys.push(key)
  }
  for (const key of keys) readPendingSubmission(key, now)
}

export const getOrCreateImageStudioSubmissionKey = (
  userId: number,
  requestFingerprint: string
): string | null => {
  if (
    userId <= 0 ||
    !requestFingerprintPattern.test(requestFingerprint) ||
    typeof window === 'undefined' ||
    !globalThis.crypto?.randomUUID
  ) {
    return null
  }
  try {
    const now = Date.now()
    prunePendingSubmissions(userId, now)
    const key = pendingSubmissionStorageKey(userId, requestFingerprint)
    const existing = readPendingSubmission(key, now)
    if (existing) return existing.idempotencyKey
    const idempotencyKey = globalThis.crypto.randomUUID()
    window.localStorage.setItem(
      key,
      JSON.stringify({
        idempotencyKey,
        expiresAt: now + pendingSubmissionTTL,
      } satisfies PendingImageStudioSubmission)
    )
    return idempotencyKey
  } catch {
    return null
  }
}

export const clearImageStudioSubmissionKey = (
  userId: number,
  requestFingerprint: string
): void => {
  if (
    userId <= 0 ||
    !requestFingerprintPattern.test(requestFingerprint) ||
    typeof window === 'undefined'
  ) {
    return
  }
  try {
    window.localStorage.removeItem(
      pendingSubmissionStorageKey(userId, requestFingerprint)
    )
  } catch {
    // A stale receipt is safer than losing recovery for an uncertain request.
  }
}

type ImageStudioDraftState = {
  userId: number
  draft: ImageComposerValues | null
  hydrate: (userId: number) => void
  save: (userId: number, draft: ImageComposerValues) => void
  clear: (userId: number) => void
}

const storageKey = (userId: number): string =>
  `image-studio-draft:user:${String(userId)}`

const readDraft = (userId: number): ImageComposerValues | null => {
  if (userId <= 0 || typeof window === 'undefined') return null
  try {
    const raw = window.localStorage.getItem(storageKey(userId))
    if (!raw) return null
    const parsed = imageComposerSchema.safeParse(JSON.parse(raw))
    if (parsed.success) return parsed.data
    window.localStorage.removeItem(storageKey(userId))
  } catch {
    // Draft persistence is best-effort.
  }
  return null
}

export const useImageStudioDraftStore = create<ImageStudioDraftState>(
  (set) => ({
    userId: 0,
    draft: null,
    hydrate: (userId) => set({ userId, draft: readDraft(userId) }),
    save: (userId, draft) => {
      const parsed = imageComposerSchema.safeParse(draft)
      if (!parsed.success || userId <= 0) return
      try {
        window.localStorage.setItem(
          storageKey(userId),
          JSON.stringify(parsed.data)
        )
      } catch {
        // Draft persistence is best-effort.
      }
      set({ userId, draft: parsed.data })
    },
    clear: (userId) => {
      try {
        window.localStorage.removeItem(storageKey(userId))
      } catch {
        // Draft persistence is best-effort.
      }
      set({ userId, draft: null })
    },
  })
)

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
import { after, beforeEach, describe, test } from 'node:test'

import type { VideoComposerValues } from '@/features/video-studio/types'
import type { VideoUploadResumeRecord } from '@/features/video-studio/video-upload-resume'
import { type AuthUser, useAuthStore } from '@/stores/auth-store'
import { useVideoStudioDraftStore } from '@/stores/video-studio-draft-store'

class MemoryStorage implements Storage {
  private readonly values = new Map<string, string>()

  get length(): number {
    return this.values.size
  }

  clear(): void {
    this.values.clear()
  }

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  key(index: number): string | null {
    return [...this.values.keys()][index] ?? null
  }

  removeItem(key: string): void {
    this.values.delete(key)
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
  }
}

const storage = new MemoryStorage()
const originalWindow = globalThis.window

Object.defineProperty(globalThis, 'window', {
  configurable: true,
  value: { localStorage: storage },
})

const user = (id: number): AuthUser => ({
  id,
  username: `user-${String(id)}`,
  role: 1,
})

const draft = (
  prompt: string,
  referenceAssetIds: number[]
): VideoComposerValues => ({
  model_profile_id: 1,
  mode: 'image_to_video',
  prompt,
  reference_asset_ids: referenceAssetIds,
  parameters: { duration: 5 },
})

const uploadResume = (assetId: number): VideoUploadResumeRecord => ({
  assetId,
  admin: false,
  purpose: 'reference',
  uploadMode: 'multipart',
  partSize: 8 * 1024 * 1024,
  expiresAt: Math.floor(Date.now() / 1000) + 3_600,
  maxSizeBytes: 20 * 1024 * 1024,
  fingerprint: {
    name: `reference-${String(assetId)}.png`,
    type: 'image/png',
    size: 1024,
    lastModified: 123,
  },
})

describe('Video Studio draft user isolation', () => {
  beforeEach(() => {
    storage.clear()
    useVideoStudioDraftStore.setState({ draft: null, uploadResumes: [] })
    useAuthStore.getState().auth.reset()
  })

  after(() => {
    Object.defineProperty(globalThis, 'window', {
      configurable: true,
      value: originalWindow,
    })
  })

  test('restores prompt, references, and upload resumes only for their owner', () => {
    const userADraft = draft('private prompt for A', [101, 102])
    const userAResume = uploadResume(501)
    const userBDraft = draft('private prompt for B', [201])

    useAuthStore.getState().auth.setUser(user(11))
    useVideoStudioDraftStore.getState().saveDraft(userADraft)
    useVideoStudioDraftStore.getState().saveUploadResume(userAResume)

    useAuthStore.getState().auth.setUser(user(22))
    assert.equal(useVideoStudioDraftStore.getState().draft, null)
    assert.deepEqual(useVideoStudioDraftStore.getState().uploadResumes, [])

    useVideoStudioDraftStore.getState().saveDraft(userBDraft)
    useAuthStore.getState().auth.setUser(user(11))
    assert.deepEqual(useVideoStudioDraftStore.getState().draft, userADraft)
    assert.deepEqual(useVideoStudioDraftStore.getState().uploadResumes, [
      userAResume,
    ])

    useAuthStore.getState().auth.setUser(user(22))
    assert.deepEqual(useVideoStudioDraftStore.getState().draft, userBDraft)
    assert.deepEqual(useVideoStudioDraftStore.getState().uploadResumes, [])
  })

  test('sign-out restores the anonymous scope without exposing user data', () => {
    const anonymousDraft = draft('anonymous draft', [])
    const signedInDraft = draft('signed-in private draft', [301])

    useVideoStudioDraftStore.getState().saveDraft(anonymousDraft)
    useAuthStore.getState().auth.setUser(user(33))
    assert.equal(useVideoStudioDraftStore.getState().draft, null)

    useVideoStudioDraftStore.getState().saveDraft(signedInDraft)
    useAuthStore.getState().auth.reset()
    assert.deepEqual(useVideoStudioDraftStore.getState().draft, anonymousDraft)
    assert.deepEqual(useVideoStudioDraftStore.getState().uploadResumes, [])
  })

  test('discards the ownerless legacy draft instead of assigning it to a user', () => {
    const legacyDraft = draft('owner is unknown', [401])
    storage.setItem(
      'video-studio-draft',
      JSON.stringify({
        state: {
          draft: legacyDraft,
          uploadResumes: [uploadResume(601)],
        },
        version: 0,
      })
    )

    useAuthStore.getState().auth.setUser(user(44))

    assert.equal(storage.getItem('video-studio-draft'), null)
    assert.equal(useVideoStudioDraftStore.getState().draft, null)
    assert.deepEqual(useVideoStudioDraftStore.getState().uploadResumes, [])
  })
})

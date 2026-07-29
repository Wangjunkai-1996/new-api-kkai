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
import {
  createJSONStorage,
  persist,
  type StateStorage,
} from 'zustand/middleware'

import type {
  VideoComposerValues,
  VideoParameters,
} from '@/features/video-studio/types'
import {
  sanitizeVideoUploadResumeRecords,
  type VideoUploadResumeRecord,
} from '@/features/video-studio/video-upload-resume'
import { useAuthStore } from '@/stores/auth-store'

const videoGenerationModes = new Set([
  'text_to_video',
  'image_to_video',
  'first_last_frame',
])
const legacyVideoStudioDraftStorageKey = 'video-studio-draft'
const anonymousVideoStudioDraftStorageKey = 'video-studio-draft:anonymous'
const videoStudioDraftStorageVersion = 1

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

export const sanitizeVideoStudioDraft = (
  value: unknown
): VideoComposerValues | null => {
  if (
    !isRecord(value) ||
    !Number.isSafeInteger(value.model_profile_id) ||
    Number(value.model_profile_id) === 0 ||
    typeof value.mode !== 'string' ||
    !videoGenerationModes.has(value.mode) ||
    typeof value.prompt !== 'string' ||
    !Array.isArray(value.reference_asset_ids) ||
    !value.reference_asset_ids.every(
      (id) => Number.isSafeInteger(id) && Number(id) > 0
    ) ||
    !isRecord(value.parameters)
  ) {
    return null
  }

  const parameters: VideoParameters = {}
  for (const [key, parameter] of Object.entries(value.parameters)) {
    if (
      typeof parameter === 'string' ||
      typeof parameter === 'boolean' ||
      (typeof parameter === 'number' && Number.isFinite(parameter))
    ) {
      parameters[key] = parameter
    }
  }

  return {
    model_profile_id: Number(value.model_profile_id),
    mode: value.mode as VideoComposerValues['mode'],
    prompt: value.prompt,
    reference_asset_ids: value.reference_asset_ids.map(Number),
    parameters,
  }
}

type VideoStudioDraftState = {
  draft: VideoComposerValues | null
  uploadResumes: VideoUploadResumeRecord[]
  saveDraft: (draft: VideoComposerValues) => void
  clearDraft: () => void
  saveUploadResume: (record: VideoUploadResumeRecord) => void
  removeUploadResume: (assetId: number) => void
}

type PersistedVideoStudioDraftState = Pick<
  VideoStudioDraftState,
  'draft' | 'uploadResumes'
>

const getLocalStorage = (): Storage | null => {
  try {
    return typeof window === 'undefined' ? null : window.localStorage
  } catch {
    return null
  }
}

const sanitizeUserId = (userId: unknown): number | null =>
  Number.isSafeInteger(userId) && Number(userId) > 0 ? Number(userId) : null

const getVideoStudioDraftStorageKey = (userId: number | null): string =>
  userId === null
    ? anonymousVideoStudioDraftStorageKey
    : `video-studio-draft:user:${String(userId)}`

const removeLegacyVideoStudioDraft = (storage: Storage): void => {
  try {
    storage.removeItem(legacyVideoStudioDraftStorageKey)
  } catch {
    // Draft persistence is best-effort when browser storage is unavailable.
  }
}

const getInitialVideoStudioDraftUserId = (): number | null => {
  const storage = getLocalStorage()
  if (!storage) return null

  try {
    const storedUser = storage.getItem('user')
    if (!storedUser) return null
    const parsedUser: unknown = JSON.parse(storedUser)
    return isRecord(parsedUser) ? sanitizeUserId(parsedUser.id) : null
  } catch {
    return null
  }
}

let activeVideoStudioDraftUserId = getInitialVideoStudioDraftUserId()

const videoStudioDraftStateStorage: StateStorage = {
  getItem: () => {
    const storage = getLocalStorage()
    if (!storage) return null
    removeLegacyVideoStudioDraft(storage)

    const storageKey = getVideoStudioDraftStorageKey(
      activeVideoStudioDraftUserId
    )
    try {
      const storedDraft = storage.getItem(storageKey)
      if (storedDraft === null) return null
      JSON.parse(storedDraft)
      return storedDraft
    } catch {
      try {
        storage.removeItem(storageKey)
      } catch {
        // Draft persistence is best-effort when browser storage is unavailable.
      }
      return null
    }
  },
  setItem: (_name, value) => {
    const storage = getLocalStorage()
    if (!storage) return
    removeLegacyVideoStudioDraft(storage)

    try {
      storage.setItem(
        getVideoStudioDraftStorageKey(activeVideoStudioDraftUserId),
        value
      )
    } catch {
      // Draft persistence is best-effort when browser storage is unavailable.
    }
  },
  removeItem: () => {
    const storage = getLocalStorage()
    if (!storage) return
    removeLegacyVideoStudioDraft(storage)

    try {
      storage.removeItem(
        getVideoStudioDraftStorageKey(activeVideoStudioDraftUserId)
      )
    } catch {
      // Draft persistence is best-effort when browser storage is unavailable.
    }
  },
}

export const useVideoStudioDraftStore = create<VideoStudioDraftState>()(
  persist(
    (set) => ({
      draft: null,
      uploadResumes: [],
      saveDraft: (draft) => set({ draft: sanitizeVideoStudioDraft(draft) }),
      clearDraft: () => set({ draft: null }),
      saveUploadResume: (record) =>
        set((state) => ({
          uploadResumes: sanitizeVideoUploadResumeRecords(
            [
              ...state.uploadResumes.filter(
                (current) => current.assetId !== record.assetId
              ),
              record,
            ],
            Math.floor(Date.now() / 1000)
          ),
        })),
      removeUploadResume: (assetId) =>
        set((state) => ({
          uploadResumes: state.uploadResumes.filter(
            (record) => record.assetId !== assetId
          ),
        })),
    }),
    {
      name: 'video-studio-draft-scoped',
      storage: createJSONStorage(() => videoStudioDraftStateStorage),
      version: videoStudioDraftStorageVersion,
      partialize: (state): PersistedVideoStudioDraftState => ({
        draft: state.draft,
        uploadResumes: state.uploadResumes,
      }),
      merge: (persistedState, currentState) => {
        const persistedDraft = isRecord(persistedState)
          ? persistedState.draft
          : null
        const persistedUploadResumes = isRecord(persistedState)
          ? persistedState.uploadResumes
          : []
        return {
          ...currentState,
          draft: sanitizeVideoStudioDraft(persistedDraft),
          uploadResumes: sanitizeVideoUploadResumeRecords(
            persistedUploadResumes,
            Math.floor(Date.now() / 1000)
          ),
        }
      },
    }
  )
)

const setVideoStudioDraftUser = (userId: number | null): void => {
  const nextUserId = sanitizeUserId(userId)
  const storage = getLocalStorage()
  if (storage) removeLegacyVideoStudioDraft(storage)
  if (nextUserId === activeVideoStudioDraftUserId) return

  activeVideoStudioDraftUserId = nextUserId
  void useVideoStudioDraftStore.persist.rehydrate()
}

useAuthStore.subscribe((state, previousState) => {
  const userId = state.auth.user?.id ?? null
  const previousUserId = previousState.auth.user?.id ?? null
  if (userId === previousUserId) return
  setVideoStudioDraftUser(userId)
})

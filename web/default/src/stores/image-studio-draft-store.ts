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

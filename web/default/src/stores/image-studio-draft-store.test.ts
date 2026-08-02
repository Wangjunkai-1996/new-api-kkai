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

import type { ImageComposerValues } from '@/features/image-studio/types'
import { useImageStudioDraftStore } from '@/stores/image-studio-draft-store'

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

const draft = (prompt: string): ImageComposerValues => ({
  model_profile_id: 3,
  prompt,
  parameters: { size: '1024x1024', count: 1 },
})

describe('Image Studio draft user isolation', () => {
  beforeEach(() => {
    storage.clear()
    useImageStudioDraftStore.setState({ userId: 0, draft: null })
  })

  after(() => {
    Object.defineProperty(globalThis, 'window', {
      configurable: true,
      value: originalWindow,
    })
  })

  test('restores only the active user draft', () => {
    const userADraft = draft('private prompt for A')
    const userBDraft = draft('private prompt for B')

    useImageStudioDraftStore.getState().save(11, userADraft)
    useImageStudioDraftStore.getState().save(22, userBDraft)
    useImageStudioDraftStore.getState().hydrate(11)
    assert.deepEqual(useImageStudioDraftStore.getState().draft, userADraft)

    useImageStudioDraftStore.getState().hydrate(22)
    assert.deepEqual(useImageStudioDraftStore.getState().draft, userBDraft)
  })

  test('clearing one user does not remove another user draft', () => {
    const userADraft = draft('draft A')
    const userBDraft = draft('draft B')
    useImageStudioDraftStore.getState().save(11, userADraft)
    useImageStudioDraftStore.getState().save(22, userBDraft)

    useImageStudioDraftStore.getState().clear(11)
    useImageStudioDraftStore.getState().hydrate(22)
    assert.deepEqual(useImageStudioDraftStore.getState().draft, userBDraft)
  })

  test('discards malformed persisted data', () => {
    storage.setItem(
      'image-studio-draft:user:33',
      JSON.stringify({ model_profile_id: 0, prompt: '', parameters: {} })
    )
    useImageStudioDraftStore.getState().hydrate(33)
    assert.equal(useImageStudioDraftStore.getState().draft, null)
    assert.equal(storage.getItem('image-studio-draft:user:33'), null)
  })
})

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
import { act, renderHook } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import {
  useImageReferences,
  type ImageReferenceLimits,
} from './use-image-references'

const limits: ImageReferenceLimits = {
  scopeKey: 'model-a',
  maxReferenceImages: 1,
  maxReferenceBytes: 32,
  maxReferenceTotalBytes: 32,
}
const image = (name: string, content = 'image'): File =>
  new File([content], name, { type: 'image/png' })

describe('atomic reference replacement', () => {
  test('replaces a full reference set without appending and keeps it when validation fails', async () => {
    const { result } = renderHook(() => useImageReferences(limits))
    const original = image('original.png')
    await act(() => result.current.select([original]))
    const replacement = image('finished.png')
    const onCommit = vi.fn()
    await act(async () =>
      expect(await result.current.replace([replacement], { onCommit })).toBe(
        true
      )
    )
    expect(result.current.files).toEqual([replacement])
    expect(onCommit).toHaveBeenCalledOnce()
    const committedIds = result.current.fileIds
    await act(async () =>
      expect(
        await result.current.replace([image('empty.png', '')], { onCommit })
      ).toBe(false)
    )
    expect(result.current.files).toEqual([replacement])
    expect(result.current.fileIds).toEqual(committedIds)
    expect(onCommit).toHaveBeenCalledOnce()
    expect(result.current.error).not.toBeNull()
  })

  test('adopts a prepared model scope in the same commit without clearing the new files', async () => {
    const { result, rerender } = renderHook(
      (value: ImageReferenceLimits) => useImageReferences(value),
      { initialProps: limits }
    )
    const target = { ...limits, scopeKey: 'model-b', maxReferenceImages: 2 }
    const replacement = image('finished.png')
    await act(async () => {
      expect(
        await result.current.replace([replacement], {
          limits: target,
          onCommit: () => rerender(target),
        })
      ).toBe(true)
    })
    expect(result.current.files).toEqual([replacement])
    expect(result.current.processing).toBe(false)
  })

  test('does not commit a prepared image after the editor revision becomes stale', async () => {
    const { result } = renderHook(() => useImageReferences(limits))
    const original = image('original.png')
    await act(() => result.current.select([original]))
    const onCommit = vi.fn()
    await act(async () => {
      expect(
        await result.current.replace([image('finished.png')], {
          canCommit: () => false,
          onCommit,
        })
      ).toBe(false)
    })
    expect(result.current.files).toEqual([original])
    expect(onCommit).not.toHaveBeenCalled()
    expect(result.current.processing).toBe(false)
  })

  test('rejects a pending replacement when the model scope changes', async () => {
    const { result, rerender } = renderHook(
      (value: ImageReferenceLimits) => useImageReferences(value),
      { initialProps: limits }
    )
    let finish: ((data: ArrayBuffer) => void) | undefined
    const candidate = image('finished.png')
    Object.defineProperty(candidate, 'arrayBuffer', {
      value: () =>
        new Promise<ArrayBuffer>((resolve) => {
          finish = resolve
        }),
    })
    const onCommit = vi.fn()
    let pending: Promise<boolean> | undefined
    act(() => {
      pending = result.current.replace([candidate], { onCommit })
    })
    rerender({ ...limits, scopeKey: 'another-model' })
    await act(async () => {
      finish?.(new TextEncoder().encode('image').buffer)
      expect(await pending).toBe(false)
    })
    expect(result.current.files).toEqual([])
    expect(onCommit).not.toHaveBeenCalled()
    expect(result.current.processing).toBe(false)
  })
})

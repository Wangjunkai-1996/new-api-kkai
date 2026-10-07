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
import { expect, test } from 'vitest'

import type { ImageModelProfile } from '../types'
import { useImageEditReferences } from './use-image-edit-references'

test('applies the selected model reference limit and clears files when the model changes', async () => {
  const flare: ImageModelProfile = {
    id: 8,
    model: 'gpt-image-2.5-flare',
    display_name: 'Flare',
    description: '',
    provider_label: '',
    specification_version: 1,
    specification: { version: 1, max_reference_images: 2, parameters: [] },
    default_parameters: {},
    effective_max_outputs: 4,
    enabled: true,
    sort_order: 0,
    created_at: 1,
    updated_at: 1,
  }
  const sunburst: ImageModelProfile = {
    ...flare,
    id: 9,
    model: 'gpt-image-2.5-sunburst',
    specification: { ...flare.specification, max_reference_images: 1 },
  }
  const references = [
    new File(['first'], 'first.png', { type: 'image/png' }),
    new File(['second'], 'second.png', { type: 'image/png' }),
  ]
  const { result, rerender } = renderHook(
    (profile) => useImageEditReferences(profile, undefined),
    { initialProps: flare }
  )

  await act(() => result.current.references.select(references))
  expect(result.current.references.error).toBeNull()
  expect(result.current.references.files).toEqual(references)
  expect(result.current.references.metadata).toHaveLength(2)

  rerender(sunburst)

  expect(result.current.maxImages).toBe(1)
  expect(result.current.references.files).toEqual([])
  expect(result.current.references.metadata).toEqual([])
  await act(() => result.current.references.select(references))
  expect(result.current.references.error).not.toBeNull()
  expect(result.current.references.files).toEqual([])
})

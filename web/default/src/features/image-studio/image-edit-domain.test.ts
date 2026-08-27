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

import { describe, test } from 'vitest'

import {
  buildImageEditQuoteRequest,
  findImageEditProfile,
  getImageProfileMaxReferenceImages,
  isImageEditQuoteRequest,
  isImageQuoteStaleError,
} from './image-edit-domain'
import type { ImageModelProfile } from './types'

const profile: ImageModelProfile = {
  id: 7,
  model: 'gpt-image-1',
  display_name: 'GPT Image',
  description: '',
  provider_label: 'OpenAI',
  specification_version: 2,
  specification: {
    version: 2,
    max_reference_images: 4,
    parameters: [],
  },
  default_parameters: {},
  effective_max_outputs: 4,
  enabled: true,
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
}

describe('image edit domain', () => {
  test('enables editing only for the exact gpt-image-2 profile', () => {
    const editProfile = { ...profile, id: 8, model: 'gpt-image-2' }
    const suffixProfile = { ...profile, id: 9, model: 'gpt-image-2-2k' }

    assert.equal(
      findImageEditProfile([suffixProfile, editProfile]),
      editProfile
    )
    assert.equal(findImageEditProfile([suffixProfile]), undefined)
    assert.equal(
      findImageEditProfile([{ ...profile, model: 'gpt-image-2k' }]),
      undefined
    )
  })

  test('resolves the model reference limit with a fail-safe default', () => {
    assert.equal(getImageProfileMaxReferenceImages(profile), 4)
    for (const configured of [undefined, 0]) {
      assert.equal(
        getImageProfileMaxReferenceImages({
          ...profile,
          specification: {
            ...profile.specification,
            max_reference_images: configured,
          },
        }),
        1
      )
    }
    assert.equal(
      getImageProfileMaxReferenceImages({
        ...profile,
        specification: {
          ...profile.specification,
          max_reference_images: 99,
        },
      }),
      4
    )
  })

  test('recognizes only quote-stale conflict responses', () => {
    assert.equal(isImageQuoteStaleError(409, { code: 'quote_stale' }), true)
    assert.equal(isImageQuoteStaleError(400, { code: 'quote_stale' }), false)
    assert.equal(isImageQuoteStaleError(409, { code: 'other' }), false)
    assert.equal(isImageQuoteStaleError(409, undefined), false)
  })

  test('recognizes both single and multi-reference edit requests', () => {
    const base = {
      token_id: 3,
      model: 'gpt-image-2',
      prompt: 'change the lighting',
      parameters: {},
    }
    const reference = { sha256: 'a'.repeat(64), size_bytes: 9 }

    assert.equal(isImageEditQuoteRequest({ ...base, reference }), true)
    assert.equal(
      isImageEditQuoteRequest({ ...base, references: [reference] }),
      true
    )
    assert.equal(isImageEditQuoteRequest(base), false)
  })

  test('uses the legacy field for one reference and the array for multiple', () => {
    const request = {
      token_id: 3,
      model: 'gpt-image-2',
      prompt: 'change the lighting',
      parameters: {},
    }
    const first = { sha256: 'a'.repeat(64), size_bytes: 9 }
    const second = { sha256: 'b'.repeat(64), size_bytes: 10 }

    assert.equal(buildImageEditQuoteRequest(request, []), null)
    assert.deepEqual(buildImageEditQuoteRequest(request, [first]), {
      ...request,
      reference: first,
    })
    assert.deepEqual(buildImageEditQuoteRequest(request, [first, second]), {
      ...request,
      references: [first, second],
    })
  })
})

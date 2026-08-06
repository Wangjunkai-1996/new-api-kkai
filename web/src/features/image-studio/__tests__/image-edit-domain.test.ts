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
import { describe, test } from 'node:test'

import { getImageReferenceMetadata } from '../image-domain'
import {
  buildCreateImageEditRequest,
  findImageEditProfile,
} from '../image-edit-domain'
import type {
  ImageEditQuoteRequest,
  ImageModelProfile,
  ImageQuote,
} from '../types'

const profile = (id: number, model: string): ImageModelProfile => ({
  id,
  model,
  display_name: model,
  description: '',
  provider_label: 'OpenAI',
  specification_version: 1,
  specification: { version: 1, parameters: [] },
  default_parameters: {},
  enabled: true,
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
})

describe('image edit domain', () => {
  test('enables editing only for the exact gpt-image-2 profile', () => {
    const editProfile = profile(8, 'gpt-image-2')
    const suffixProfile = profile(9, 'gpt-image-2-2k')

    assert.equal(
      findImageEditProfile([suffixProfile, editProfile]),
      editProfile
    )
    assert.equal(findImageEditProfile([suffixProfile]), undefined)
    assert.equal(findImageEditProfile([profile(10, 'gpt-image-2k')]), undefined)
  })

  test('binds an edit quote and submission to the reference digest', async () => {
    const reference = await getImageReferenceMetadata(
      new Blob(['reference-image'], { type: 'image/png' })
    )
    assert.deepEqual(reference, {
      sha256:
        '4110dd12af975f556bdac0299d0bfa04d42fa22d94f56b8550f1762e48fff7fb',
      size_bytes: 15,
    })

    const quoteRequest: ImageEditQuoteRequest = {
      token_id: 3,
      model: 'gpt-image-2',
      prompt: 'preserve the subject and change the lighting',
      parameters: { size: '1024x1536', count: 2 },
      reference,
    }
    const quote: ImageQuote = {
      quota: 1_500_000,
      display_amount: '$3.00',
      quote_token: 'opaque.edit.quote',
      expires_at: 1_800_000_000,
    }

    assert.deepEqual(buildCreateImageEditRequest(quoteRequest, quote), {
      ...quoteRequest,
      quote_token: quote.quote_token,
    })
  })
})

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
  imageAssetSchema,
  imageModelProfileSchema,
  imageQuoteSchema,
  imageTokenCapabilitySchema,
} from './schemas'

describe('image API response schemas', () => {
  test('accepts custom size constraints on model profiles', () => {
    const parsed = imageModelProfileSchema.parse({
      id: 3,
      model: 'gpt-image-2',
      display_name: 'GPT Image 2',
      description: '',
      provider_label: 'OpenAI',
      specification_version: 1,
      specification: {
        version: 1,
        custom_size: {
          max_edge: 3840,
          step: 16,
          min_pixels: 655360,
          max_pixels: 8294400,
          max_aspect_ratio: 3,
        },
        parameters: [],
      },
      default_parameters: {},
      effective_max_outputs: 1,
      enabled: true,
      sort_order: 0,
      created_at: 1,
      updated_at: 1,
    })
    assert.equal(parsed.specification.custom_size?.step, 16)
  })

  test('accepts only local or HTTP media URLs', () => {
    const asset = {
      id: 3,
      position: 0,
      state: 'ready',
      thumbnail_state: 'ready',
      mime_type: 'image/png',
      size_bytes: 128,
      width: 64,
      height: 64,
      content_url: '/api/image-studio/assets/3',
      thumbnail_url: 'https://cdn.example.com/assets/3/thumbnail',
    }
    assert.equal(imageAssetSchema.parse(asset).id, 3)
    assert.equal(
      imageAssetSchema.safeParse({
        ...asset,
        content_url: 'javascript:alert(1)',
      }).success,
      false
    )
    assert.equal(
      imageAssetSchema.safeParse({
        ...asset,
        content_url: '//untrusted.example/asset',
      }).success,
      false
    )
  })

  test('strips secret fields and requires a ready token in the bound group', () => {
    const capability = imageTokenCapabilitySchema.parse({
      required_group: 'image',
      has_usable_token: true,
      can_create: true,
      effective_models: ['gpt-image-1'],
      max_reference_bytes: 32 << 20,
      max_reference_total_bytes: 64 << 20,
      status: 'ready',
      token: {
        id: 8,
        name: 'Image Studio',
        group: 'image',
        key: 'sk-must-not-reach-client-state',
      },
    })
    assert.deepEqual(capability.token, {
      id: 8,
      name: 'Image Studio',
      group: 'image',
    })
    assert.equal(
      imageTokenCapabilitySchema.safeParse({
        ...capability,
        token: { ...capability.token, group: 'default' },
      }).success,
      false
    )
    assert.equal(
      imageTokenCapabilitySchema.safeParse({
        ...capability,
        token: undefined,
      }).success,
      false
    )
  })

  test('requires a bounded opaque quote token', () => {
    const quote = {
      quota: 500_000,
      display_amount: '$1.00',
      quote_token: 'opaque.signed.quote',
      expires_at: 1_800_000_000,
    }

    assert.equal(imageQuoteSchema.parse(quote).quote_token, quote.quote_token)
    assert.equal(
      imageQuoteSchema.safeParse({ ...quote, quote_token: '' }).success,
      false
    )
    assert.equal(
      imageQuoteSchema.safeParse({ ...quote, quote_token: 'x'.repeat(8192) })
        .success,
      true
    )
    assert.equal(
      imageQuoteSchema.safeParse({ ...quote, quote_token: 'x'.repeat(8193) })
        .success,
      false
    )
  })
})

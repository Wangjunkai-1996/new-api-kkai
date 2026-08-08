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

import { imageModelProfileSchema, imageTokenCapabilitySchema } from './schemas'

describe('image reference response schemas', () => {
  test('accepts legacy reference defaults and bounds configured model limits', () => {
    const model = {
      id: 3,
      model: 'gpt-image-2',
      display_name: 'GPT Image 2',
      description: '',
      provider_label: 'OpenAI',
      specification_version: 1,
      specification: { version: 1, parameters: [] },
      default_parameters: {},
      enabled: true,
      sort_order: 0,
      created_at: 1,
      updated_at: 1,
    }

    assert.equal(imageModelProfileSchema.safeParse(model).success, true)
    for (const maxReferenceImages of [0, 4]) {
      assert.equal(
        imageModelProfileSchema.safeParse({
          ...model,
          specification: {
            ...model.specification,
            max_reference_images: maxReferenceImages,
          },
        }).success,
        true
      )
    }
    assert.equal(
      imageModelProfileSchema.safeParse({
        ...model,
        specification: { ...model.specification, max_reference_images: 5 },
      }).success,
      false
    )
  })

  test('defaults missing capability reference byte limits conservatively', () => {
    const capability = imageTokenCapabilitySchema.parse({
      required_group: 'image',
      has_usable_token: true,
      can_create: true,
      effective_models: ['gpt-image-1'],
      max_reference_bytes: 32 << 20,
      max_reference_total_bytes: 64 << 20,
      status: 'ready',
      token: { id: 8, name: 'Image Studio', group: 'image' },
    })

    assert.equal(capability.max_reference_bytes, 32 << 20)
    assert.equal(capability.max_reference_total_bytes, 64 << 20)
    assert.deepEqual(
      imageTokenCapabilitySchema.parse({
        ...capability,
        max_reference_bytes: undefined,
        max_reference_total_bytes: undefined,
      }),
      {
        ...capability,
        max_reference_bytes: 32 << 20,
        max_reference_total_bytes: 32 << 20,
      }
    )
    assert.equal(
      imageTokenCapabilitySchema.parse({
        ...capability,
        max_reference_bytes: undefined,
      }).max_reference_total_bytes,
      64 << 20
    )
  })
})

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

import {
  getImagePricingFormValues,
  imagePricingFormSchema,
  parseImagePricingPolicy,
  updateImagePricingPolicy,
} from '../image-pricing-policy'

const rawPolicy = JSON.stringify({
  version: '2026-08-04.v1',
  enabled: false,
  audit_label: 'keep-top-level',
  models: {
    'gpt-image-2': {
      default_size: '1024x1024',
      provider_hint: 'keep-model-field',
      tiers: {
        '1k': {
          unit_price: 0.67,
          sizes: ['1024x1024'],
          note: 'keep-tier-field',
        },
        '2k': {
          unit_price: 1,
          sizes: [
            '1536x1024',
            '1024x1536',
            '2048x2048',
            '2048x1152',
            '1152x2048',
          ],
        },
        '4k': {
          unit_price: 1.34,
          sizes: ['3840x2160', '2160x3840'],
        },
      },
    },
    'future-image-model': {
      default_size: '512x512',
      tiers: {
        standard: { unit_price: 0.25, sizes: ['512x512'] },
      },
    },
  },
})

describe('image pricing policy editor', () => {
  test('extracts only the fields exposed by the admin form', () => {
    const values = getImagePricingFormValues(parseImagePricingPolicy(rawPolicy))

    assert.deepEqual(values, {
      enabled: false,
      price1k: 0.67,
      price2k: 1,
      price4k: 1.34,
    })
  })

  test('updates prices immutably without losing sizes or extension fields', () => {
    const original = parseImagePricingPolicy(rawPolicy)
    const updated = updateImagePricingPolicy(
      original,
      { enabled: true, price1k: 0.7, price2k: 1.1, price4k: 1.5 },
      '2026-08-04T12:00:00.000Z'
    )

    assert.equal(original.enabled, false)
    assert.equal(original.models['gpt-image-2'].tiers['1k'].unit_price, 0.67)
    assert.equal(updated.version, '2026-08-04T12:00:00.000Z')
    assert.equal(updated.enabled, true)
    assert.equal(updated.models['gpt-image-2'].tiers['1k'].unit_price, 0.7)
    assert.equal(updated.models['gpt-image-2'].tiers['2k'].unit_price, 1.1)
    assert.equal(updated.models['gpt-image-2'].tiers['4k'].unit_price, 1.5)
    assert.deepEqual(
      updated.models['gpt-image-2'].tiers['2k'].sizes,
      original.models['gpt-image-2'].tiers['2k'].sizes
    )
    assert.equal(updated.audit_label, 'keep-top-level')
    assert.equal(
      updated.models['gpt-image-2'].provider_hint,
      'keep-model-field'
    )
    assert.equal(
      updated.models['gpt-image-2'].tiers['1k'].note,
      'keep-tier-field'
    )
    assert.deepEqual(
      updated.models['future-image-model'],
      original.models['future-image-model']
    )
  })

  test('rejects invalid policies and non-positive prices', () => {
    assert.throws(
      () => parseImagePricingPolicy('{invalid-json'),
      /imageStudio\.admin\.pricing\.invalidPolicy/
    )
    assert.throws(
      () =>
        parseImagePricingPolicy(
          JSON.stringify({
            version: 'missing-tiers',
            enabled: true,
            models: {
              'gpt-image-2': {
                default_size: '1024x1024',
                tiers: {
                  '1k': { unit_price: 1, sizes: ['1024x1024'] },
                },
              },
            },
          })
        ),
      /imageStudio\.admin\.pricing\.invalidPolicy/
    )
    assert.equal(
      imagePricingFormSchema.safeParse({
        enabled: true,
        price1k: 0,
        price2k: 1,
        price4k: 1,
      }).success,
      false
    )
  })
})

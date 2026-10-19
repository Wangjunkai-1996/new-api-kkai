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
import { describe, expect, test } from 'vitest'

import {
  getImagePricingFormValues,
  imagePricingFormSchema,
  parseImagePricingPolicy,
  updateImagePricingPolicy,
} from './image-pricing-policy'

const rawPolicy = JSON.stringify({
  version: '2026-08-04.v1',
  enabled: false,
  audit_label: 'keep-top-level',
  models: {
    'gpt-image-2': {
      default_size: '1024x1024',
      allow_custom_size: true,
      tiers: {
        '1k': { unit_price: 0.67, sizes: ['1024x1024'] },
        '2k': { unit_price: 1, sizes: ['2048x2048'] },
        '4k': { unit_price: 1.34, sizes: ['3840x2160'] },
      },
    },
    'gpt-image-2.5-flare': {
      default_size: '1024x1024',
      provider_hint: 'keep-model-field',
      tiers: {
        '1k': {
          unit_price: 0.12,
          sizes: ['1024x1024'],
          note: 'keep-tier-field',
        },
        '2k': { unit_price: 0.24, sizes: ['2048x2048'] },
        '4k': { unit_price: 0.48, sizes: ['3840x2160'] },
        extra: { unit_price: 0.01, sizes: ['512x512'] },
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
  test('reads each model independently without requiring gpt-image-2', () => {
    const policy = parseImagePricingPolicy(rawPolicy)
    expect(getImagePricingFormValues(policy, 'gpt-image-2')).toEqual({
      enabled: false,
      allow_custom_size: true,
      price1k: 0.67,
      price2k: 1,
      price4k: 1.34,
    })
    delete policy.models['gpt-image-2']
    expect(
      getImagePricingFormValues(
        parseImagePricingPolicy(JSON.stringify(policy)),
        'gpt-image-2.5-flare'
      )
    ).toEqual({
      enabled: false,
      allow_custom_size: false,
      price1k: 0.12,
      price2k: 0.24,
      price4k: 0.48,
    })
  })

  test('updates only the selected model without losing sizes or extension fields', () => {
    const original = parseImagePricingPolicy(rawPolicy)
    const updated = updateImagePricingPolicy(
      original,
      'gpt-image-2.5-flare',
      {
        enabled: true,
        allow_custom_size: true,
        price1k: 0.15,
        price2k: 0.3,
        price4k: 0.6,
      },
      '2026-08-04T12:00:00.000Z'
    )

    expect(original).toEqual(parseImagePricingPolicy(rawPolicy))
    expect(updated).toEqual({
      ...original,
      version: '2026-08-04T12:00:00.000Z',
      enabled: true,
      models: {
        ...original.models,
        'gpt-image-2.5-flare': {
          ...original.models['gpt-image-2.5-flare'],
          allow_custom_size: true,
          tiers: {
            ...original.models['gpt-image-2.5-flare'].tiers,
            '1k': {
              ...original.models['gpt-image-2.5-flare'].tiers['1k'],
              unit_price: 0.15,
            },
            '2k': {
              ...original.models['gpt-image-2.5-flare'].tiers['2k'],
              unit_price: 0.3,
            },
            '4k': {
              ...original.models['gpt-image-2.5-flare'].tiers['4k'],
              unit_price: 0.6,
            },
          },
        },
      },
    })
  })

  test.each(['missing-model', 'future-image-model'])(
    'rejects reading or updating unsupported tiers for %s',
    (modelName) => {
      const policy = parseImagePricingPolicy(rawPolicy)
      expect(() => getImagePricingFormValues(policy, modelName)).toThrow(
        'imageStudio.admin.pricing.invalidPolicy'
      )
      expect(() =>
        updateImagePricingPolicy(policy, modelName, {
          enabled: true,
          allow_custom_size: true,
          price1k: 1,
          price2k: 2,
          price4k: 4,
        })
      ).toThrow('imageStudio.admin.pricing.invalidPolicy')
      expect(policy).toEqual(parseImagePricingPolicy(rawPolicy))
    }
  )

  test('rejects invalid policies and non-positive prices', () => {
    expect(() => parseImagePricingPolicy('{invalid-json')).toThrow(
      'imageStudio.admin.pricing.invalidPolicy'
    )
    expect(
      imagePricingFormSchema.safeParse({
        enabled: true,
        price1k: 0,
        price2k: 1,
        price4k: 1,
      }).success
    ).toBe(false)
  })
})

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
  normalizeImageParameters,
  parseImageParameters,
} from './image-parameters'
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
    parameters: [
      {
        key: 'size',
        label: 'Size',
        request_key: 'size',
        control: 'select',
        required: true,
        options: [
          { label: 'Square', value: '1024x1024' },
          { label: 'Portrait', value: '1024x1536' },
        ],
      },
      {
        key: 'count',
        label: 'Count',
        request_key: 'n',
        control: 'integer',
        min: 1,
        max: 128,
      },
      {
        key: 'compression',
        label: 'Compression',
        request_key: 'output_compression',
        control: 'integer',
        min: 0,
        max: 100,
      },
      {
        key: 'watermark',
        label: 'Watermark',
        request_key: 'watermark',
        control: 'boolean',
      },
    ],
  },
  default_parameters: {},
  enabled: true,
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
}

describe('image parameters', () => {
  test('keeps only values allowed by the active model specification', () => {
    assert.deepEqual(
      normalizeImageParameters(profile, {
        size: '1024x1536',
        count: 4,
        compression: 101,
        watermark: true,
        unknown: 'must-not-pass',
      }),
      { size: '1024x1536', count: 4, watermark: true }
    )
    assert.deepEqual(normalizeImageParameters(profile, { size: 'auto' }), {})
  })

  test('enforces the four-output limit independently of the admin range', () => {
    assert.deepEqual(normalizeImageParameters(profile, { count: 5 }), {})
    assert.deepEqual(normalizeImageParameters(profile, { count: 4 }), {
      count: 4,
    })
  })

  test('parses specification fields and strips unknown parameters', () => {
    assert.deepEqual(
      parseImageParameters(profile, {
        size: '1024x1536',
        count: 4,
        compression: 80,
        watermark: false,
        unknown: 'must-not-pass',
      }),
      {
        success: true,
        parameters: {
          size: '1024x1536',
          count: 4,
          compression: 80,
          watermark: false,
        },
      }
    )
  })

  test('reports a missing required parameter', () => {
    const expected = {
      success: false,
      errors: [{ code: 'required', parameterKey: 'size' }],
    }
    assert.deepEqual(parseImageParameters(profile, {}), expected)
    assert.deepEqual(parseImageParameters(profile, { size: '' }), expected)
  })

  test('treats an empty optional select as omitted', () => {
    const optionalProfile: ImageModelProfile = {
      ...profile,
      specification: {
        ...profile.specification,
        parameters: profile.specification.parameters.map((parameter) =>
          parameter.key === 'size'
            ? { ...parameter, required: false }
            : parameter
        ),
      },
    }
    assert.deepEqual(parseImageParameters(optionalProfile, { size: '' }), {
      success: true,
      parameters: {},
    })
  })

  test('reports invalid select and boolean values by field', () => {
    assert.deepEqual(
      parseImageParameters(profile, { size: 'auto', watermark: 'true' }),
      {
        success: false,
        errors: [
          { code: 'invalid_option', parameterKey: 'size' },
          { code: 'invalid_boolean', parameterKey: 'watermark' },
        ],
      }
    )
  })

  test('distinguishes non-integers from values outside effective ranges', () => {
    assert.deepEqual(
      parseImageParameters(profile, { size: '1024x1024', count: 1.5 }),
      {
        success: false,
        errors: [{ code: 'invalid_integer', parameterKey: 'count' }],
      }
    )
    assert.deepEqual(
      parseImageParameters(profile, {
        size: '1024x1024',
        count: 5,
        compression: 101,
      }),
      {
        success: false,
        errors: [
          {
            code: 'out_of_range',
            parameterKey: 'count',
            min: 1,
            max: 4,
          },
          {
            code: 'out_of_range',
            parameterKey: 'compression',
            min: 0,
            max: 100,
          },
        ],
      }
    )
  })
})

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
  buildImageComposerValues,
  canAccessImageStudio,
  normalizeImageParameters,
  normalizeImageStudioAccessMode,
} from './image-domain'
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
  default_parameters: {
    size: '1024x1024',
    count: 12,
    compression: 80,
    watermark: false,
  },
  enabled: true,
  sort_order: 0,
  created_at: 1,
  updated_at: 1,
}

describe('image studio access', () => {
  test('fails closed for unknown values and respects the admin boundary', () => {
    assert.equal(normalizeImageStudioAccessMode('all'), 'all')
    assert.equal(normalizeImageStudioAccessMode('admin'), 'admin')
    assert.equal(normalizeImageStudioAccessMode('unexpected'), 'off')
    assert.equal(canAccessImageStudio('off', true), false)
    assert.equal(canAccessImageStudio('admin', false), false)
    assert.equal(canAccessImageStudio('admin', true), true)
    assert.equal(canAccessImageStudio('all', false), true)
  })
})

describe('image composer parameters', () => {
  test('keeps only values allowed by the active model specification', () => {
    assert.deepEqual(
      normalizeImageParameters(profile, {
        size: '1024x1536',
        count: 4,
        compression: 101,
        watermark: true,
        unknown: 'must-not-pass',
      }),
      {
        size: '1024x1536',
        count: 4,
        watermark: true,
      }
    )
  })

  test('enforces the first-phase four-image limit independently of admin range', () => {
    assert.deepEqual(normalizeImageParameters(profile, { count: 5 }), {})
    assert.deepEqual(normalizeImageParameters(profile, { count: 4 }), {
      count: 4,
    })
  })

  test('sanitizes profile defaults and stale drafts when building the form', () => {
    assert.deepEqual(
      buildImageComposerValues(profile, {
        prompt: '  retained for editing  ',
        parameters: {
          size: 'invalid-size',
          count: 2,
          unknown: true,
        },
        sample_id: 9,
      }),
      {
        model_profile_id: profile.id,
        prompt: '  retained for editing  ',
        parameters: {
          size: '1024x1024',
          count: 2,
          compression: 80,
          watermark: false,
        },
        sample_id: 9,
      }
    )
  })
})

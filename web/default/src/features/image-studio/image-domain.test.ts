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
  buildCreateImageRequest,
  buildImageComposerValues,
  canAccessImageStudio,
  getImageGenerationPollInterval,
  imageSubmissionFingerprint,
  normalizeImageStudioAccessMode,
  resolveImageGenerationStatus,
} from './image-domain'
import type { ImageModelProfile, ImageQuote, ImageQuoteRequest } from './types'

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
  effective_max_outputs: 4,
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

  test('clamps a draft output count by request field and effective limit', () => {
    const limitedProfile: ImageModelProfile = {
      ...profile,
      effective_max_outputs: 2,
      specification: {
        ...profile.specification,
        parameters: profile.specification.parameters.map((parameter) =>
          parameter.request_key === 'n'
            ? { ...parameter, key: 'variants' }
            : parameter
        ),
      },
      default_parameters: {
        size: '1024x1024',
        variants: 1,
        compression: 80,
        watermark: false,
      },
    }

    assert.deepEqual(
      buildImageComposerValues(limitedProfile, {
        prompt: 'bounded batch',
        parameters: { variants: 4 },
      }).parameters,
      {
        size: '1024x1024',
        variants: 2,
        compression: 80,
        watermark: false,
      }
    )
  })

  test('uses the target profile default when rebuilding after a model switch', () => {
    const targetProfile: ImageModelProfile = {
      ...profile,
      id: 8,
      default_parameters: {
        ...profile.default_parameters,
        count: 3,
      },
    }
    const profileWithoutCountDefault: ImageModelProfile = {
      ...targetProfile,
      id: 9,
      default_parameters: {
        size: '1024x1024',
        compression: 80,
        watermark: false,
      },
    }

    assert.equal(
      buildImageComposerValues(targetProfile, { prompt: 'retained' }).parameters
        .count,
      3
    )
    assert.equal(
      buildImageComposerValues(profileWithoutCountDefault, {
        prompt: 'retained',
      }).parameters.count,
      1
    )
  })
})

describe('image submission recovery', () => {
  test('submits only the opaque quote token with the quoted request', () => {
    const quoteRequest: ImageQuoteRequest = {
      token_id: 3,
      model: 'gpt-image-1',
      prompt: 'private lighthouse prompt',
      parameters: { size: '1024x1536', count: 2 },
    }
    const quote: ImageQuote = {
      quota: 750_000,
      display_amount: '$1.50',
      quote_token: 'opaque.signed.quote',
      expires_at: 1_800_000_000,
    }

    const request = buildCreateImageRequest(quoteRequest, quote)
    assert.deepEqual(request, {
      ...quoteRequest,
      quote_token: quote.quote_token,
    })
    assert.equal('max_quota' in request, false)
    assert.equal('quote_hash' in request, false)
    assert.equal('quote_expires_at' in request, false)
  })

  test('clears drafts only after a completed generation', () => {
    assert.deepEqual(resolveImageGenerationStatus('succeeded'), {
      outcome: 'success',
      clearDraft: true,
    })
    assert.deepEqual(resolveImageGenerationStatus('partial'), {
      outcome: 'success',
      clearDraft: true,
    })
    assert.deepEqual(resolveImageGenerationStatus('submitting'), {
      outcome: 'pending',
      clearDraft: false,
    })
    for (const status of ['failed', 'archive_failed', 'unknown'] as const) {
      assert.deepEqual(resolveImageGenerationStatus(status), {
        outcome: 'failure',
        clearDraft: false,
      })
    }
  })

  test('polls only visible pages containing an active generation', () => {
    const active = [
      { status: 'submitting' as const, started_at: 90, created_at: 90 },
    ]
    assert.equal(getImageGenerationPollInterval(active, 100, true), 3_000)
    assert.equal(getImageGenerationPollInterval(active, 150, true), 5_000)
    assert.equal(getImageGenerationPollInterval(active, 300, true), 10_000)
    assert.equal(getImageGenerationPollInterval(active, 100, false), false)
    assert.equal(
      getImageGenerationPollInterval(
        [{ status: 'succeeded', started_at: 90, created_at: 90 }],
        100,
        true
      ),
      false
    )
  })

  test('hashes a canonical request without persisting its raw prompt', async () => {
    const first: ImageQuoteRequest = {
      token_id: 3,
      model: 'gpt-image-1',
      prompt: 'private lighthouse prompt',
      parameters: { size: '1024x1024', count: 2 },
    }
    const reordered: ImageQuoteRequest = {
      prompt: first.prompt,
      parameters: { count: 2, size: '1024x1024' },
      model: first.model,
      token_id: first.token_id,
    }

    const firstDigest = await imageSubmissionFingerprint(first)
    const secondDigest = await imageSubmissionFingerprint(reordered)
    assert.equal(firstDigest, secondDigest)
    assert.equal(
      firstDigest,
      'a0bd239c23e605d792cf92be10c54f4163ff67cd0dbe161a0d96786f26f96e1a'
    )
    assert.match(firstDigest, /^[a-f0-9]{64}$/)
    assert.equal(firstDigest.includes(first.prompt), false)
  })

  test('uses the selected size in the submission fingerprint', async () => {
    const request: ImageQuoteRequest = {
      token_id: 3,
      model: 'gpt-image-1',
      prompt: 'same prompt',
      parameters: { size: '1024x1024', count: 1 },
    }
    const largerRequest: ImageQuoteRequest = {
      ...request,
      parameters: { ...request.parameters, size: '1024x1536' },
    }

    assert.notEqual(
      await imageSubmissionFingerprint(request),
      await imageSubmissionFingerprint(largerRequest)
    )
  })
})

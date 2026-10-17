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

import { describe, test, expect, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  buildImageEditFormData,
  createAsyncImageGeneration,
  lookupImageSubmission,
  quoteAsyncImageGeneration,
} from './api'
import type { CreateImageEditRequest } from './types'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))

describe('image edit transport', () => {
  test('sends the legacy reference field for one image', () => {
    const request: CreateImageEditRequest = {
      token_id: 3,
      model: 'gpt-image-2',
      prompt: 'change the lighting',
      parameters: { size: '1024x1024', count: 1 },
      reference: { sha256: 'a'.repeat(64), size_bytes: 9 },
      quote_token: 'opaque.edit.quote',
    }
    const image = new File(['reference'], 'reference.png', {
      type: 'image/png',
    })

    const body = buildImageEditFormData(request, [image])

    assert.deepEqual(JSON.parse(body.get('request') as string), request)
    assert.equal(body.getAll('image').length, 1)
  })

  test('sends eight references as ordered repeated image fields', async () => {
    const images = Array.from({ length: 8 }, (_, index) => {
      const format = index === 1 ? 'webp' : 'png'
      return new File(
        [`reference-${index + 1}`],
        `reference-${index + 1}.${format}`,
        { type: `image/${format}` }
      )
    })
    const request: CreateImageEditRequest = {
      token_id: 3,
      model: 'gpt-image-2',
      prompt: 'change the lighting',
      parameters: { size: '1024x1024', count: 1 },
      references: images.map((image, index) => ({
        sha256: String(index + 1).repeat(64),
        size_bytes: image.size,
      })),
      quote_token: 'opaque.edit.quote',
    }

    const body = buildImageEditFormData(request, images)

    assert.deepEqual(
      [...body.keys()],
      ['request', ...images.map(() => 'image')]
    )
    assert.equal(body.getAll('request').length, 1)
    assert.equal(body.getAll('image').length, 8)
    const encodedRequest = body.get('request')
    assert.equal(typeof encodedRequest, 'string')
    assert.deepEqual(JSON.parse(encodedRequest as string), request)
    const encodedImages = body.getAll('image')
    for (const [index, encodedImage] of encodedImages.entries()) {
      assert.ok(encodedImage instanceof File)
      assert.equal(encodedImage.name, images[index].name)
      assert.equal(encodedImage.type, images[index].type)
      assert.deepEqual(
        new Uint8Array(await encodedImage.arrayBuffer()),
        new Uint8Array(await images[index].arrayBuffer())
      )
    }
  })
})

describe('durable image submissions', () => {
  test('looks up a receipt without replaying files or creating a request', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: { success: true, data: { state: 'not_found' } },
    })
    expect(await lookupImageSubmission('receipt-key')).toEqual({
      state: 'not_found',
    })
    expect(api.get).toHaveBeenCalledWith(
      '/api/image-studio/submissions/lookup',
      {
        headers: { 'Idempotency-Key': 'receipt-key' },
        disableDuplicate: true,
        skipErrorHandler: true,
      }
    )
    expect(api.post).not.toHaveBeenCalled()
  })

  test('quotes ready assets and submits only their ordered IDs with the receipt key', async () => {
    const quote = {
      quota: 5,
      display_amount: '$0.05',
      quote_token: 'quote',
      expires_at: 1234,
    }
    const generation = {
      id: 1,
      model_profile_id: 1,
      specification_version: 1,
      model: 'gpt-image-2',
      prompt: 'edit',
      parameters: {},
      request_id: 'request',
      status: 'submitting',
      requested_count: 1,
      succeeded_count: 0,
      final_quota: 0,
      started_at: 0,
      finished_at: 0,
      created_at: 1,
      assets: [],
    }
    vi.mocked(api.post)
      .mockResolvedValueOnce({ data: { success: true, data: quote } })
      .mockResolvedValueOnce({ data: { success: true, data: generation } })
    const request = {
      token_id: 11,
      model: 'gpt-image-2',
      prompt: 'edit',
      parameters: {},
      mode: 'edit' as const,
      asset_ids: [8, 3],
    }
    expect(await quoteAsyncImageGeneration(request)).toEqual(quote)
    expect(
      await createAsyncImageGeneration(
        { ...request, quote_token: quote.quote_token },
        'receipt-key'
      )
    ).toEqual(generation)
    expect(api.post).toHaveBeenLastCalledWith(
      '/api/image-studio/generations',
      { ...request, quote_token: 'quote' },
      {
        headers: { 'Idempotency-Key': 'receipt-key' },
        skipErrorHandler: true,
      }
    )
  })

  test('rejects a found receipt without its generation instead of allowing resubmission', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: { success: true, data: { state: 'found' } },
    })
    await expect(lookupImageSubmission('receipt-key')).rejects.toThrow()
    expect(api.post).not.toHaveBeenCalled()
  })
})

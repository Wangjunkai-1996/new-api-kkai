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

import { buildCreateImageEditRequest } from './image-domain'
import {
  getImageReferenceBatchMetadata,
  getImageReferenceMetadata,
  ImageReferenceEmptyError,
  ImageReferenceTotalTooLargeError,
  ImageReferenceTooLargeError,
} from './image-reference-metadata'
import type { ImageEditQuoteRequest, ImageQuote } from './types'

class TrackingBlob extends Blob {
  arrayBufferCalled = false

  override async arrayBuffer(): Promise<ArrayBuffer> {
    this.arrayBufferCalled = true
    return super.arrayBuffer()
  }
}

describe('image reference metadata', () => {
  test('binds an edit quote and submission to the ordered digests', async () => {
    const reference = await getImageReferenceMetadata(
      new Blob(['reference-image'], { type: 'image/png' }),
      32 << 20
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
      references: [reference],
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

  test('rejects oversized references before reading their bytes', async () => {
    const reference = new TrackingBlob(['oversized-reference'])
    await assert.rejects(
      getImageReferenceMetadata(reference, reference.size - 1),
      (error: unknown) => {
        assert.ok(error instanceof ImageReferenceTooLargeError)
        assert.equal(error.sizeBytes, reference.size)
        assert.equal(error.maxBytes, reference.size - 1)
        return true
      }
    )
    assert.equal(reference.arrayBufferCalled, false)
  })

  test('rejects empty references before reading their bytes', async () => {
    const reference = new TrackingBlob([])
    await assert.rejects(
      getImageReferenceMetadata(reference, 32 << 20),
      ImageReferenceEmptyError
    )
    assert.equal(reference.arrayBufferCalled, false)
  })

  test('preserves reference order while hashing a batch', async () => {
    const references = await getImageReferenceBatchMetadata(
      [new Blob(['first']), new Blob(['second-reference'])],
      32 << 20,
      64 << 20
    )

    assert.deepEqual(
      references.map((reference) => reference.size_bytes),
      [5, 16]
    )
    assert.notEqual(references[0].sha256, references[1].sha256)
  })

  test('rejects an oversized reference batch before reading bytes', async () => {
    const first = new TrackingBlob(['first'])
    const second = new TrackingBlob(['second'])
    await assert.rejects(
      getImageReferenceBatchMetadata([first, second], 10, 8),
      (error: unknown) => {
        assert.ok(error instanceof ImageReferenceTotalTooLargeError)
        assert.equal(error.sizeBytes, 11)
        assert.equal(error.maxBytes, 8)
        return true
      }
    )
    assert.equal(first.arrayBufferCalled, false)
    assert.equal(second.arrayBufferCalled, false)
  })
})

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

import { buildImageEditFormData } from './api'
import type { CreateImageEditRequest } from './types'

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

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

  test('sends one request field and ordered repeated image fields', async () => {
    const request: CreateImageEditRequest = {
      token_id: 3,
      model: 'gpt-image-2',
      prompt: 'change the lighting',
      parameters: { size: '1024x1024', count: 1 },
      references: [
        { sha256: 'a'.repeat(64), size_bytes: 9 },
        { sha256: 'b'.repeat(64), size_bytes: 10 },
      ],
      quote_token: 'opaque.edit.quote',
    }
    const images = [
      new File(['reference-1'], 'reference-1.png', { type: 'image/png' }),
      new File(['reference-2'], 'reference-2.webp', { type: 'image/webp' }),
    ]

    const body = buildImageEditFormData(request, images)

    assert.deepEqual([...body.keys()], ['request', 'image', 'image'])
    assert.equal(body.getAll('request').length, 1)
    assert.equal(body.getAll('image').length, 2)
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

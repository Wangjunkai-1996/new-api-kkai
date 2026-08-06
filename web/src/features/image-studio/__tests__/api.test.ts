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

import { buildImageEditFormData } from '../api'
import type { CreateImageEditRequest } from '../types'

describe('image edit transport', () => {
  test('sends exactly one request field and one reference image file', async () => {
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

    const body = buildImageEditFormData(request, image)

    assert.deepEqual([...body.keys()], ['request', 'image'])
    assert.equal(body.getAll('request').length, 1)
    assert.equal(body.getAll('image').length, 1)
    const encodedRequest = body.get('request')
    assert.equal(typeof encodedRequest, 'string')
    assert.deepEqual(JSON.parse(encodedRequest as string), request)
    const encodedImage = body.get('image')
    assert.ok(encodedImage instanceof File)
    assert.equal(encodedImage.name, image.name)
    assert.equal(encodedImage.type, image.type)
    assert.deepEqual(
      new Uint8Array(await encodedImage.arrayBuffer()),
      new Uint8Array(await image.arrayBuffer())
    )
  })
})

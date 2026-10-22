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
import { AxiosError, AxiosHeaders } from 'axios'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { getImageOutputReference } from './image-output-reference'
import type { ImageAsset } from './types'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

const asset: ImageAsset = {
  id: 9,
  position: 0,
  state: 'ready',
  thumbnail_state: 'ready',
  mime_type: 'image/png',
  size_bytes: 4,
  width: 1,
  height: 1,
  content_url: '/api/image-studio/assets/9/content',
}
const limits = { maxBytes: 100, maxTotalBytes: 100 }

beforeEach(() => {
  vi.mocked(api.get).mockReset()
})

describe('preparing an output image for editing', () => {
  test('reads only the authenticated source endpoint and returns a validated File', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: new Blob(['data'], { type: 'image/png' }),
    })
    const controller = new AbortController()
    const file = await getImageOutputReference(asset, limits, controller.signal)
    expect(api.get).toHaveBeenCalledWith(
      '/api/image-studio/assets/9/edit-source',
      expect.objectContaining({
        responseType: 'blob',
        signal: controller.signal,
        disableDuplicate: true,
      })
    )
    expect(file).toBeInstanceOf(File)
    expect(file.name).toBe('image-9.png')
    expect(file.type).toBe('image/png')
    expect(await file.text()).toBe('data')
  })

  test('rejects missing or oversized assets before requesting any bytes', async () => {
    await expect(
      getImageOutputReference(
        { ...asset, state: 'deleted' },
        limits,
        new AbortController().signal
      )
    ).rejects.toMatchObject({ code: 'image_reference_invalid' })
    await expect(
      getImageOutputReference(
        asset,
        { maxBytes: 3, maxTotalBytes: 100 },
        new AbortController().signal
      )
    ).rejects.toMatchObject({ code: 'image_reference_invalid' })
    expect(api.get).not.toHaveBeenCalled()
  })

  test.each([
    [new Blob(['short'], { type: 'image/png' }), 'truncated source'],
    [new Blob(['data'], { type: 'text/html' }), 'unsupported source'],
  ])('rejects %s (%s) without returning a reference', async (blob) => {
    vi.mocked(api.get).mockResolvedValue({ data: blob })
    await expect(
      getImageOutputReference(asset, limits, new AbortController().signal)
    ).rejects.toMatchObject({ code: 'image_reference_invalid' })
  })

  test('extracts a stable error code from a Blob error response', async () => {
    const error = new AxiosError('request failed')
    error.response = {
      data: new Blob(
        [JSON.stringify({ success: false, code: 'image_edit_source_timeout' })],
        { type: 'application/json' }
      ),
      status: 504,
      statusText: 'Gateway timeout',
      headers: {},
      config: { headers: new AxiosHeaders() },
    }
    vi.mocked(api.get).mockRejectedValue(error)
    await expect(
      getImageOutputReference(asset, limits, new AbortController().signal)
    ).rejects.toMatchObject({ code: 'image_edit_source_timeout' })
  })

  test('discards a response received after cancellation', async () => {
    const controller = new AbortController()
    vi.mocked(api.get).mockImplementation(async () => {
      controller.abort()
      return { data: new Blob(['data'], { type: 'image/png' }) }
    })
    await expect(
      getImageOutputReference(asset, limits, controller.signal)
    ).rejects.toMatchObject({ name: 'AbortError' })
  })
})

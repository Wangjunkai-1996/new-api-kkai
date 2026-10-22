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
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { getImageOutputReference } from './image-output-reference'
import type { ImageAsset } from './types'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))

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
const storageUrl =
  'https://storage.example.test/output.png?signature=short-lived'
const fetchSource = vi.fn<typeof fetch>()
const decodeSource = vi.fn<typeof createImageBitmap>()

function endpointError(status: number, payload: unknown): AxiosError {
  const error = new AxiosError('request failed')
  error.response = {
    data: new Blob([JSON.stringify(payload)], { type: 'application/json' }),
    status,
    statusText: 'Request failed',
    headers: {},
    config: { headers: new AxiosHeaders() },
  }
  return error
}

function imageResponseBody(content = 'data'): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(content))
      controller.close()
    },
  })
}

beforeEach(() => {
  vi.mocked(api.get).mockReset()
  vi.mocked(api.post).mockReset()
  vi.stubGlobal('fetch', fetchSource.mockReset())
  vi.stubGlobal('createImageBitmap', decodeSource.mockReset())
})

afterEach(() => vi.unstubAllGlobals())

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
        signal: expect.any(AbortSignal),
        disableDuplicate: true,
      })
    )
    expect(file).toBeInstanceOf(File)
    expect(file.name).toBe('image-9.png')
    expect(file.type).toBe('image/png')
    expect(await file.text()).toBe('data')
    expect(api.post).not.toHaveBeenCalled()
    expect(fetchSource).not.toHaveBeenCalled()
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
    vi.mocked(api.get).mockRejectedValue(
      endpointError(504, { success: false, code: 'image_edit_source_timeout' })
    )
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

  test.each([404, 405])(
    'uses an authorized original on an older backend (%s)',
    async (status) => {
      vi.mocked(api.get).mockRejectedValue(
        endpointError(status, {
          error: { type: 'invalid_request_error', code: '' },
        })
      )
      vi.mocked(api.post).mockResolvedValue({
        data: {
          success: true,
          data: { urls: { '/api/image-studio/assets/9/content': storageUrl } },
        },
      })
      const blob = new Blob(['data'], { type: 'image/png' })
      fetchSource.mockResolvedValue({
        ok: true,
        headers: new Headers({
          'Content-Length': '4',
          'Content-Type': 'image/png',
        }),
        body: imageResponseBody(),
      } as Response)
      const close = vi.fn()
      decodeSource.mockResolvedValue({
        width: 1,
        height: 1,
        close,
      } as ImageBitmap)
      const file = await getImageOutputReference(
        { ...asset, content_url: 'https://untrusted.example.test/input' },
        limits,
        new AbortController().signal
      )
      expect(await file.text()).toBe('data')
      expect(api.post).toHaveBeenCalledWith(
        '/api/studio/media-urls',
        { paths: ['/api/image-studio/assets/9/content'] },
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
      expect(fetchSource).toHaveBeenCalledWith(storageUrl, {
        signal: expect.any(AbortSignal),
        credentials: 'omit',
        referrerPolicy: 'no-referrer',
        redirect: 'error',
      })
      expect(decodeSource).toHaveBeenCalledWith(blob)
      expect(close).toHaveBeenCalledOnce()
    }
  )

  test.each([
    [401, { success: false }],
    [403, { success: false }],
    [500, { success: false }],
    [504, { success: false }],
    [404, { success: false, code: 'image_studio_resource_not_found' }],
    [405, { error: { code: 'unsupported_operation' } }],
  ])(
    'does not fall back after authorization, asset or server errors (%s)',
    async (status, payload) => {
      vi.mocked(api.get).mockRejectedValue(endpointError(status, payload))
      await expect(
        getImageOutputReference(asset, limits, new AbortController().signal)
      ).rejects.toBeInstanceOf(Error)
      expect(api.post).not.toHaveBeenCalled()
      expect(fetchSource).not.toHaveBeenCalled()
    }
  )

  test('discards a signed source after cancellation and rejects mismatched dimensions', async () => {
    vi.mocked(api.get).mockRejectedValue(endpointError(404, {}))
    vi.mocked(api.post).mockResolvedValue({
      data: {
        success: true,
        data: { urls: { '/api/image-studio/assets/9/content': storageUrl } },
      },
    })
    const controller = new AbortController()
    fetchSource.mockImplementation(async (_url, options) => {
      controller.abort()
      expect(options?.signal?.aborted).toBe(true)
      return {
        ok: true,
        headers: new Headers({ 'Content-Type': 'image/png' }),
        body: imageResponseBody(),
      } as Response
    })
    await expect(
      getImageOutputReference(asset, limits, controller.signal)
    ).rejects.toMatchObject({ name: 'AbortError' })
    expect(decodeSource).not.toHaveBeenCalled()

    fetchSource.mockResolvedValue({
      ok: true,
      headers: new Headers({ 'Content-Type': 'image/png' }),
      body: imageResponseBody(),
    } as Response)
    const close = vi.fn()
    decodeSource.mockResolvedValue({
      width: 2,
      height: 1,
      close,
    } as ImageBitmap)
    await expect(
      getImageOutputReference(asset, limits, new AbortController().signal)
    ).rejects.toMatchObject({ code: 'image_reference_invalid' })
    expect(close).toHaveBeenCalledOnce()
  })

  test('keeps the original deadline when fetching a signed fallback', async () => {
    const deadline = new AbortController()
    vi.spyOn(AbortSignal, 'timeout').mockReturnValue(deadline.signal)
    vi.mocked(api.get).mockRejectedValue(endpointError(404, {}))
    vi.mocked(api.post).mockResolvedValue({
      data: {
        success: true,
        data: { urls: { '/api/image-studio/assets/9/content': storageUrl } },
      },
    })
    fetchSource.mockImplementation(async (_url, options) => {
      deadline.abort()
      options?.signal?.throwIfAborted()
      throw new Error('unreachable')
    })
    await expect(
      getImageOutputReference(asset, limits, new AbortController().signal)
    ).rejects.toMatchObject({ code: 'image_edit_source_timeout' })
    expect(decodeSource).not.toHaveBeenCalled()
  })

  test('bounds streamed fallback bytes even when the signed source lies about its length', async () => {
    vi.mocked(api.get).mockRejectedValue(endpointError(404, {}))
    vi.mocked(api.post).mockResolvedValue({
      data: {
        success: true,
        data: { urls: { '/api/image-studio/assets/9/content': storageUrl } },
      },
    })
    fetchSource.mockResolvedValue({
      ok: true,
      headers: new Headers({
        'Content-Length': '4',
        'Content-Type': 'image/png',
      }),
      body: imageResponseBody('data-too-large'),
    } as Response)
    await expect(
      getImageOutputReference(asset, limits, new AbortController().signal)
    ).rejects.toMatchObject({ code: 'image_reference_invalid' })
    expect(decodeSource).not.toHaveBeenCalled()
  })

  test.each([
    'http://storage.example.test/image',
    'https://user:secret@storage.example.test/image',
  ])('rejects unsafe signed URLs (%s)', async (url) => {
    vi.mocked(api.get).mockRejectedValue(endpointError(404, {}))
    vi.mocked(api.post).mockResolvedValue({
      data: {
        success: true,
        data: { urls: { '/api/image-studio/assets/9/content': url } },
      },
    })
    await expect(
      getImageOutputReference(asset, limits, new AbortController().signal)
    ).rejects.toMatchObject({ code: 'image_edit_source_unavailable' })
    expect(fetchSource).not.toHaveBeenCalled()
  })
})

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
import { isAxiosError } from 'axios'

import { api } from '@/lib/api'

import { getImageReferenceBatchMetadata } from './image-reference-metadata'
import type { ImageAsset } from './types'

export class ImageOutputReferenceError extends Error {
  constructor(readonly code: string) {
    super(code)
    this.name = 'ImageOutputReferenceError'
  }
}

export async function getImageOutputReference(
  asset: ImageAsset,
  limits: { maxBytes: number; maxTotalBytes: number },
  signal: AbortSignal
): Promise<File> {
  if (
    !Number.isSafeInteger(asset.id) ||
    asset.id <= 0 ||
    asset.state !== 'ready' ||
    asset.size_bytes <= 0 ||
    asset.size_bytes > Math.min(limits.maxBytes, limits.maxTotalBytes)
  ) {
    throw new ImageOutputReferenceError('image_reference_invalid')
  }
  const timeout = AbortSignal.timeout(75_000)
  const requestSignal = AbortSignal.any([signal, timeout])
  try {
    let blob: Blob
    let signedSource = false
    try {
      const response = await api.get<Blob>(
        `/api/image-studio/assets/${asset.id}/edit-source`,
        {
          responseType: 'blob',
          signal: requestSignal,
          timeout: 75_000,
          disableDuplicate: true,
          skipErrorHandler: true,
        }
      )
      blob = response.data
    } catch (error) {
      requestSignal.throwIfAborted()
      if (!isAxiosError(error)) throw error
      let payload: unknown = error.response?.data
      if (payload instanceof Blob) {
        try {
          payload = JSON.parse(await payload.text())
        } catch {
          payload = undefined
        }
      }
      if (payload && typeof payload === 'object') {
        if (
          'code' in payload &&
          typeof payload.code === 'string' &&
          payload.code
        ) {
          throw new ImageOutputReferenceError(payload.code)
        }
        if (
          'error' in payload &&
          payload.error &&
          typeof payload.error === 'object'
        ) {
          const detail = payload.error
          if (
            'code' in detail &&
            typeof detail.code === 'string' &&
            detail.code
          ) {
            throw new ImageOutputReferenceError(detail.code)
          }
        }
      }
      if (error.response?.status !== 404 && error.response?.status !== 405) {
        throw error
      }
      // Older rollback versions sign the same authorized original for direct GET.
      const path = `/api/image-studio/assets/${asset.id}/content`
      const signed = await api.post<{
        success: boolean
        code?: string
        data?: { urls: Record<string, string> }
      }>(
        '/api/studio/media-urls',
        { paths: [path] },
        {
          signal: requestSignal,
          timeout: 75_000,
          disableDuplicate: true,
          skipErrorHandler: true,
        }
      )
      const source = signed.data.data?.urls[path]
      if (!signed.data.success || !source) {
        throw new ImageOutputReferenceError(
          signed.data.code || 'image_edit_source_unavailable'
        )
      }
      const url = new URL(source)
      if (
        url.protocol !== 'https:' ||
        url.username ||
        url.password ||
        url.hash
      ) {
        throw new ImageOutputReferenceError('image_edit_source_unavailable')
      }
      // The dashboard API client must never send its Bearer token to storage.
      const response = await fetch(url.href, {
        signal: requestSignal,
        credentials: 'omit',
        referrerPolicy: 'no-referrer',
        redirect: 'error',
      })
      if (!response.ok) {
        throw new ImageOutputReferenceError('image_edit_source_unavailable')
      }
      const contentLength = response.headers.get('Content-Length')
      if (
        contentLength !== null &&
        Number(contentLength) !== asset.size_bytes
      ) {
        throw new ImageOutputReferenceError('image_reference_invalid')
      }
      if (!response.body) {
        throw new ImageOutputReferenceError('image_edit_source_unavailable')
      }
      const reader = response.body.getReader()
      const chunks: ArrayBuffer[] = []
      let size = 0
      try {
        while (true) {
          requestSignal.throwIfAborted()
          const { done, value } = await reader.read()
          if (done) break
          size += value.byteLength
          if (size > asset.size_bytes) {
            await reader.cancel()
            throw new ImageOutputReferenceError('image_reference_invalid')
          }
          const chunk = new Uint8Array(value.byteLength)
          chunk.set(value)
          chunks.push(chunk.buffer)
        }
      } finally {
        reader.releaseLock()
      }
      blob = new Blob(chunks, {
        type: response.headers.get('Content-Type') || '',
      })
      signedSource = true
    }
    requestSignal.throwIfAborted()
    const type = blob.type.toLowerCase().split(';')[0]
    const extensions: Record<string, string> = {
      'image/png': 'png',
      'image/jpeg': 'jpg',
      'image/webp': 'webp',
    }
    if (
      !extensions[type] ||
      type !== asset.mime_type.toLowerCase().split(';')[0] ||
      blob.size !== asset.size_bytes
    ) {
      throw new ImageOutputReferenceError('image_reference_invalid')
    }
    if (signedSource) {
      const image = await createImageBitmap(blob)
      try {
        if (image.width !== asset.width || image.height !== asset.height) {
          throw new ImageOutputReferenceError('image_reference_invalid')
        }
      } finally {
        image.close()
      }
    }
    const file = new File([blob], `image-${asset.id}.${extensions[type]}`, {
      type,
    })
    await getImageReferenceBatchMetadata(
      [file],
      limits.maxBytes,
      limits.maxTotalBytes
    )
    requestSignal.throwIfAborted()
    return file
  } catch (error) {
    signal.throwIfAborted()
    if (timeout.aborted) {
      throw new ImageOutputReferenceError('image_edit_source_timeout')
    }
    if (error instanceof ImageOutputReferenceError) throw error
    throw new ImageOutputReferenceError('image_edit_source_unavailable')
  }
}

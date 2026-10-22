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
    asset.state !== 'ready' ||
    asset.size_bytes <= 0 ||
    asset.size_bytes > Math.min(limits.maxBytes, limits.maxTotalBytes)
  ) {
    throw new ImageOutputReferenceError('image_reference_invalid')
  }
  let blob: Blob
  try {
    const response = await api.get<Blob>(
      `/api/image-studio/assets/${asset.id}/edit-source`,
      {
        responseType: 'blob',
        signal,
        timeout: 75_000,
        disableDuplicate: true,
        skipErrorHandler: true,
      }
    )
    blob = response.data
  } catch (error) {
    if (signal.aborted) throw error
    if (isAxiosError(error) && error.response?.data instanceof Blob) {
      try {
        const payload: unknown = JSON.parse(await error.response.data.text())
        if (payload && typeof payload === 'object' && 'code' in payload) {
          const code = payload.code
          if (typeof code === 'string') {
            throw new ImageOutputReferenceError(code)
          }
        }
      } catch (parseError) {
        if (parseError instanceof ImageOutputReferenceError) throw parseError
      }
    }
    throw new ImageOutputReferenceError('image_edit_source_unavailable')
  }
  signal.throwIfAborted()
  const type = blob.type.toLowerCase().split(';')[0]
  const extensions: Record<string, string> = {
    'image/png': 'png',
    'image/jpeg': 'jpg',
    'image/webp': 'webp',
  }
  if (!extensions[type] || blob.size !== asset.size_bytes) {
    throw new ImageOutputReferenceError('image_reference_invalid')
  }
  const file = new File([blob], `image-${asset.id}.${extensions[type]}`, {
    type,
  })
  await getImageReferenceBatchMetadata(
    [file],
    limits.maxBytes,
    limits.maxTotalBytes
  )
  signal.throwIfAborted()
  return file
}

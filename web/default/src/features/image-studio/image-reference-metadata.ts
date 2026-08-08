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
import { sha256Hex } from './image-hash'
import type { ImageReferenceMetadata } from './types'

export const IMAGE_REFERENCE_DEFAULT_MAX_BYTES = 32 << 20

export class ImageReferenceTooLargeError extends Error {
  readonly name = 'ImageReferenceTooLargeError'

  constructor(
    readonly sizeBytes: number,
    readonly maxBytes: number
  ) {
    super(`Image reference exceeds the ${maxBytes}-byte limit`)
  }
}

export class ImageReferenceEmptyError extends Error {
  readonly name = 'ImageReferenceEmptyError'
}

export class ImageReferenceTotalTooLargeError extends Error {
  readonly name = 'ImageReferenceTotalTooLargeError'

  constructor(
    readonly sizeBytes: number,
    readonly maxBytes: number
  ) {
    super(`Image references exceed the ${maxBytes}-byte total limit`)
  }
}

export const getImageReferenceMetadata = async (
  image: Blob,
  maxBytes: number
): Promise<ImageReferenceMetadata> => {
  if (image.size <= 0) throw new ImageReferenceEmptyError()
  if (image.size > maxBytes) {
    throw new ImageReferenceTooLargeError(image.size, maxBytes)
  }
  return {
    sha256: await sha256Hex(await image.arrayBuffer()),
    size_bytes: image.size,
  }
}

export const getImageReferenceBatchMetadata = async (
  images: readonly Blob[],
  maxBytes: number,
  maxTotalBytes: number,
  existingSizeBytes = 0
): Promise<ImageReferenceMetadata[]> => {
  for (const image of images) {
    if (image.size <= 0) throw new ImageReferenceEmptyError()
    if (image.size > maxBytes) {
      throw new ImageReferenceTooLargeError(image.size, maxBytes)
    }
  }
  const totalSizeBytes = images.reduce(
    (total, image) => total + image.size,
    existingSizeBytes
  )
  if (totalSizeBytes > maxTotalBytes) {
    throw new ImageReferenceTotalTooLargeError(totalSizeBytes, maxTotalBytes)
  }

  const metadata: ImageReferenceMetadata[] = []
  for (const image of images) {
    metadata.push(await getImageReferenceMetadata(image, maxBytes))
  }
  return metadata
}

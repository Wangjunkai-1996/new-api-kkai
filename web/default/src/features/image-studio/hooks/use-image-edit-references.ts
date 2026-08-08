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
import {
  findImageEditProfile,
  getImageProfileMaxReferenceImages,
} from '../image-edit-domain'
import { IMAGE_REFERENCE_DEFAULT_MAX_BYTES } from '../image-reference-metadata'
import type { ImageModelProfile, ImageTokenCapability } from '../types'
import { useImageReferences } from './use-image-references'

export function useImageEditReferences(
  profiles: ImageModelProfile[] | undefined,
  capability: ImageTokenCapability | undefined
) {
  const profile = findImageEditProfile(profiles)
  const maxImages = getImageProfileMaxReferenceImages(profile)
  const maxReferenceBytes =
    capability?.max_reference_bytes ?? IMAGE_REFERENCE_DEFAULT_MAX_BYTES
  const maxReferenceTotalBytes =
    capability?.max_reference_total_bytes ?? IMAGE_REFERENCE_DEFAULT_MAX_BYTES
  const scopeKey = [
    profile?.id ?? 0,
    profile?.specification_version ?? 0,
    maxImages,
    maxReferenceBytes,
    maxReferenceTotalBytes,
  ].join(':')
  const references = useImageReferences({
    scopeKey,
    maxReferenceImages: maxImages,
    maxReferenceBytes,
    maxReferenceTotalBytes,
  })
  return { profile, maxImages, references }
}

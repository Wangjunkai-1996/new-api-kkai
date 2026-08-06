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
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { getImageReferenceMetadata } from '../image-domain'
import type { ImageReferenceMetadata } from '../types'

const IMAGE_REFERENCE_MIME_TYPES = new Set([
  'image/jpeg',
  'image/png',
  'image/webp',
])

type ImageReferenceController = {
  file: File | null
  metadata: ImageReferenceMetadata | null
  processing: boolean
  error: string | null
  select: (file: File) => Promise<void>
  clear: () => void
}

export function useImageReference(): ImageReferenceController {
  const { t } = useTranslation()
  const [file, setFile] = useState<File | null>(null)
  const [metadata, setMetadata] = useState<ImageReferenceMetadata | null>(null)
  const [processing, setProcessing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const revisionRef = useRef(0)

  useEffect(
    () => () => {
      revisionRef.current += 1
    },
    []
  )

  const clear = (): void => {
    revisionRef.current += 1
    setFile(null)
    setMetadata(null)
    setProcessing(false)
    setError(null)
  }

  const select = async (nextFile: File): Promise<void> => {
    if (!IMAGE_REFERENCE_MIME_TYPES.has(nextFile.type)) {
      clear()
      setError(t('imageStudio.referenceTypeInvalid'))
      return
    }
    const revision = revisionRef.current + 1
    revisionRef.current = revision
    setFile(nextFile)
    setMetadata(null)
    setError(null)
    setProcessing(true)
    try {
      const nextMetadata = await getImageReferenceMetadata(nextFile)
      if (revisionRef.current !== revision) return
      setMetadata(nextMetadata)
    } catch {
      if (revisionRef.current !== revision) return
      setMetadata(null)
      setError(t('imageStudio.referenceProcessingFailed'))
    } finally {
      if (revisionRef.current === revision) setProcessing(false)
    }
  }

  return { file, metadata, processing, error, select, clear }
}

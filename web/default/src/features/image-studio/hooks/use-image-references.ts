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
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  getImageReferenceBatchMetadata,
  ImageReferenceEmptyError,
  ImageReferenceTotalTooLargeError,
  ImageReferenceTooLargeError,
} from '../image-reference-metadata'
import type { ImageReferenceMetadata } from '../types'

const IMAGE_REFERENCE_MIME_TYPES = new Set([
  'image/jpeg',
  'image/png',
  'image/webp',
])

export type ImageReferencesController = {
  files: File[]
  fileIds: number[]
  metadata: ImageReferenceMetadata[]
  processing: boolean
  error: string | null
  select: (files: readonly File[]) => Promise<void>
  replace: (
    files: readonly File[],
    options?: {
      limits?: ImageReferenceLimits
      onCommit?: () => void
      canCommit?: () => boolean
    }
  ) => Promise<boolean>
  remove: (index: number) => void
  move: (index: number, offset: -1 | 1) => void
  clear: () => void
}

type ImageReferenceItem = {
  id: number
  file: File
  metadata: ImageReferenceMetadata
}

export type ImageReferenceLimits = {
  scopeKey: string
  maxReferenceImages: number
  maxReferenceBytes: number
  maxReferenceTotalBytes: number
}

export function useImageReferences(
  limits: ImageReferenceLimits
): ImageReferencesController {
  const { t } = useTranslation()
  const [items, setItems] = useState<ImageReferenceItem[]>([])
  const [processing, setProcessing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const itemsRef = useRef<ImageReferenceItem[]>([])
  const nextItemIdRef = useRef(1)
  const revisionRef = useRef(0)
  const activeScopeRef = useRef(limits.scopeKey)
  const settledScopeRef = useRef(limits.scopeKey)
  useLayoutEffect(() => {
    activeScopeRef.current = limits.scopeKey
  }, [limits.scopeKey])

  const files = useMemo(() => items.map((item) => item.file), [items])
  const fileIds = useMemo(() => items.map((item) => item.id), [items])
  const metadata = useMemo(() => items.map((item) => item.metadata), [items])

  useEffect(
    () => () => {
      revisionRef.current += 1
    },
    []
  )

  useEffect(() => {
    if (settledScopeRef.current === limits.scopeKey) return
    settledScopeRef.current = limits.scopeKey
    revisionRef.current += 1
    itemsRef.current = []
    setItems([])
    setProcessing(false)
    setError(null)
  }, [limits.scopeKey])

  const clear = (): void => {
    revisionRef.current += 1
    itemsRef.current = []
    setItems([])
    setProcessing(false)
    setError(null)
  }

  const remove = (index: number): void => {
    if (index < 0 || index >= itemsRef.current.length) return
    revisionRef.current += 1
    const nextItems = itemsRef.current.filter(
      (_item, itemIndex) => itemIndex !== index
    )
    itemsRef.current = nextItems
    setItems(nextItems)
    setProcessing(false)
    setError(null)
  }

  const move = (index: number, offset: -1 | 1): void => {
    const target = index + offset
    if (
      index < 0 ||
      index >= itemsRef.current.length ||
      target < 0 ||
      target >= itemsRef.current.length
    ) {
      return
    }
    revisionRef.current += 1
    setProcessing(false)
    const nextItems = [...itemsRef.current]
    const current = nextItems[index]
    nextItems[index] = nextItems[target]
    nextItems[target] = current
    itemsRef.current = nextItems
    setItems(nextItems)
    setError(null)
  }

  const updateReferences = async (
    nextFiles: readonly File[],
    replacement?: {
      limits?: ImageReferenceLimits
      onCommit?: () => void
      canCommit?: () => boolean
    }
  ): Promise<boolean> => {
    if (!replacement && nextFiles.length === 0) return false
    const nextLimits = replacement?.limits ?? limits
    if (nextFiles.some((file) => !IMAGE_REFERENCE_MIME_TYPES.has(file.type))) {
      setError(t('imageStudio.referenceTypeInvalid'))
      return false
    }
    const currentItems = replacement ? [] : itemsRef.current
    if (
      currentItems.length + nextFiles.length >
      nextLimits.maxReferenceImages
    ) {
      setError(
        t('imageStudio.referenceLimitExceeded', {
          count: nextLimits.maxReferenceImages,
        })
      )
      return false
    }
    const selectionScope = activeScopeRef.current
    const revision = revisionRef.current + 1
    revisionRef.current = revision
    setError(null)
    setProcessing(true)
    try {
      const currentSizeBytes = currentItems.reduce(
        (total, item) => total + item.metadata.size_bytes,
        0
      )
      const nextMetadata = await getImageReferenceBatchMetadata(
        nextFiles,
        nextLimits.maxReferenceBytes,
        nextLimits.maxReferenceTotalBytes,
        currentSizeBytes
      )
      if (
        revisionRef.current !== revision ||
        activeScopeRef.current !== selectionScope ||
        replacement?.canCommit?.() === false
      ) {
        return false
      }
      const nextItems = [
        ...currentItems,
        ...nextFiles.map((file, index) => ({
          id: nextItemIdRef.current++,
          file,
          metadata: nextMetadata[index],
        })),
      ]
      if (replacement) {
        // Adopt the prepared model scope in the same commit as the form reset.
        activeScopeRef.current = nextLimits.scopeKey
        settledScopeRef.current = nextLimits.scopeKey
        replacement.onCommit?.()
      }
      itemsRef.current = nextItems
      setItems(nextItems)
      return true
    } catch (metadataError) {
      if (
        revisionRef.current !== revision ||
        activeScopeRef.current !== selectionScope
      ) {
        return false
      }
      let message = t('imageStudio.referenceProcessingFailed')
      if (metadataError instanceof ImageReferenceEmptyError) {
        message = t('imageStudio.referenceEmpty')
      } else if (metadataError instanceof ImageReferenceTooLargeError) {
        message = t('imageStudio.referenceTooLarge')
      } else if (metadataError instanceof ImageReferenceTotalTooLargeError) {
        message = t('imageStudio.referenceTotalTooLarge')
      }
      setError(message)
      return false
    } finally {
      if (
        revisionRef.current === revision &&
        (activeScopeRef.current === selectionScope ||
          activeScopeRef.current === replacement?.limits?.scopeKey)
      ) {
        setProcessing(false)
      }
    }
  }

  return {
    files,
    fileIds,
    metadata,
    processing,
    error,
    select: async (files) => {
      await updateReferences(files)
    },
    replace: (files, options = {}) => updateReferences(files, options),
    remove,
    move,
    clear,
  }
}

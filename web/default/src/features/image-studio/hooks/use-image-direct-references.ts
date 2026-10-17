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
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { ImageUploadProgress } from '../image-upload'
import type { ImageReferenceAsset } from '../image-upload-api'
import type { ImageReferenceMetadata } from '../types'

const EMPTY_ASSETS: ImageReferenceAsset[] = []

export function useImageDirectReferences(input: {
  enabled: boolean
  userId: number
  files: readonly File[]
  metadata: readonly ImageReferenceMetadata[]
}) {
  const { t } = useTranslation()
  const [attempt, setAttempt] = useState(0)
  const [result, setResult] = useState<{
    key: string
    assets: ImageReferenceAsset[]
    error: string | null
  }>({ key: '', assets: [], error: null })
  const [progress, setProgress] = useState<
    ImageUploadProgress & { key: string }
  >({
    key: '',
    phase: 'uploading',
    progress: 0,
  })
  const key = JSON.stringify([
    input.userId,
    input.enabled,
    input.metadata,
    attempt,
  ])
  const enabled =
    input.enabled &&
    input.userId > 0 &&
    input.files.length > 0 &&
    input.files.length === input.metadata.length

  useEffect(() => {
    if (!enabled) return
    const controller = new AbortController()
    void import('../image-upload')
      .then(async ({ uploadImageReferences, ImageUploadError }) => {
        try {
          const assets = await uploadImageReferences(
            input.userId,
            input.files,
            input.metadata,
            (next) => {
              if (!controller.signal.aborted) setProgress({ ...next, key })
            },
            controller.signal
          )
          if (!controller.signal.aborted) {
            setResult({ key, assets, error: null })
          }
        } catch (error) {
          if (controller.signal.aborted) return
          const message =
            error instanceof ImageUploadError &&
            error.reason === 'validation_pending'
              ? t(
                  'Reference image validation is taking longer. Check again to continue.'
                )
              : t('Reference image upload failed. Retry to resume the upload.')
          setResult({ key, assets: [], error: message })
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setResult({
            key,
            assets: [],
            error: t(
              'Reference image upload failed. Retry to resume the upload.'
            ),
          })
        }
      })
    return () => controller.abort()
  }, [enabled, input.files, input.metadata, input.userId, key, attempt, t])

  const assets = result.key === key ? result.assets : EMPTY_ASSETS
  const assetIds = useMemo(() => assets.map((asset) => asset.id), [assets])
  const error = enabled && result.key === key ? result.error : null
  return {
    assetIds,
    ready: enabled && assets.length === input.files.length,
    processing: enabled && !error && assets.length !== input.files.length,
    error,
    progress:
      progress.key === key ? progress : { phase: 'uploading', progress: 0 },
    retry: () => setAttempt((value) => value + 1),
  }
}

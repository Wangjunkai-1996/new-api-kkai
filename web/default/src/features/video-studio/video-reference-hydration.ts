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

import type { VideoAsset } from './types'

type VideoReferenceHydrationResult = {
  data?: VideoAsset
  error: unknown
  isFetchedAfterMount: boolean
  isFetching: boolean
}

type VideoReferenceHydrationRecovery = {
  retainedAssetIds: number[]
  retainedAssets: VideoAsset[]
}

const unavailableReferenceStatuses = new Set([403, 404, 410])

export const isUnavailableVideoReferenceError = (error: unknown): boolean =>
  isAxiosError(error) &&
  unavailableReferenceStatuses.has(error.response?.status ?? 0)

export const shouldRetryVideoReferenceHydration = (
  failureCount: number,
  error: unknown
): boolean => !isUnavailableVideoReferenceError(error) && failureCount < 3

export const getHydratedVideoReferences = (
  assetIds: number[],
  results: VideoReferenceHydrationResult[]
): VideoAsset[] | null => {
  if (results.length !== assetIds.length) return null

  const hydratedAssets: VideoAsset[] = []
  for (const [index, assetId] of assetIds.entries()) {
    const result = results[index]
    if (
      !result?.data ||
      result.data.id !== assetId ||
      !result.isFetchedAfterMount ||
      result.isFetching ||
      (result.error !== null && result.error !== undefined)
    ) {
      return null
    }
    hydratedAssets.push(result.data)
  }
  return hydratedAssets
}

export const getVideoReferenceHydrationRecovery = (
  assetIds: number[],
  results: VideoReferenceHydrationResult[]
): VideoReferenceHydrationRecovery | null => {
  if (
    results.length !== assetIds.length ||
    results.some((result) => !result.isFetchedAfterMount || result.isFetching)
  ) {
    return null
  }

  const unavailableIndex = results.findIndex((result, index) => {
    return (
      assetIds[index] !== undefined &&
      isUnavailableVideoReferenceError(result.error)
    )
  })
  if (unavailableIndex < 0) return null
  if (
    !results
      .slice(unavailableIndex)
      .every((result) => isUnavailableVideoReferenceError(result.error))
  ) {
    return null
  }

  const retainedAssets: VideoAsset[] = []
  for (let index = 0; index < unavailableIndex; index += 1) {
    const result = results[index]
    if (
      !result?.data ||
      result.data.id !== assetIds[index] ||
      (result.error !== null && result.error !== undefined)
    ) {
      return null
    }
    retainedAssets.push(result.data)
  }

  return {
    retainedAssetIds: assetIds.slice(0, retainedAssets.length),
    retainedAssets,
  }
}

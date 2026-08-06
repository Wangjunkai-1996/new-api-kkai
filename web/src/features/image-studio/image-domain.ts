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
import type {
  CreateImageRequest,
  ImageComposerValues,
  ImageGeneration,
  ImageGenerationStatus,
  ImageModelProfile,
  ImageParameters,
  ImageParameterValue,
  ImageQuote,
  ImageQuoteRequest,
  ImageReferenceMetadata,
  ImageStudioAccessMode,
} from './types'

export const IMAGE_STUDIO_MAX_OUTPUTS = 4

export const normalizeImageStudioAccessMode = (
  value: unknown
): ImageStudioAccessMode => {
  if (value === 'admin' || value === 'all') return value
  return 'off'
}

export const canAccessImageStudio = (
  mode: ImageStudioAccessMode,
  isAdmin: boolean
): boolean => mode === 'all' || (mode === 'admin' && isAdmin)

export type ImageGenerationOutcome = 'success' | 'pending' | 'failure'

export const classifyImageGenerationStatus = (
  status: ImageGenerationStatus
): ImageGenerationOutcome => {
  if (status === 'succeeded' || status === 'partial') return 'success'
  if (status === 'submitting') return 'pending'
  return 'failure'
}

export const isImageGenerationActive = (
  status: ImageGenerationStatus
): boolean => status === 'submitting'

export const getImageGenerationPollInterval = (
  generations: Array<
    Pick<ImageGeneration, 'status' | 'started_at' | 'created_at'>
  >,
  nowSeconds: number,
  pageVisible: boolean
): number | false => {
  if (!pageVisible) return false
  const activeGenerations = generations.filter((generation) =>
    isImageGenerationActive(generation.status)
  )
  if (activeGenerations.length === 0) return false
  const activeStartedAt = activeGenerations.reduce(
    (latest, generation) =>
      Math.max(latest, generation.started_at || generation.created_at),
    0
  )
  if (activeStartedAt === 0) return 3_000
  const ageSeconds = Math.max(0, nowSeconds - activeStartedAt)
  if (ageSeconds < 30) return 3_000
  if (ageSeconds < 120) return 5_000
  return 10_000
}

export const getImageProfileDefaults = (
  profile: ImageModelProfile
): ImageParameters => {
  const defaults: ImageParameters = {}
  for (const parameter of profile.specification.parameters) {
    const value = profile.default_parameters[parameter.key]
    if (value !== undefined) defaults[parameter.key] = value
  }
  return defaults
}

export const normalizeImageParameters = (
  profile: ImageModelProfile,
  values: ImageParameters
): ImageParameters => {
  const result: ImageParameters = {}
  for (const parameter of profile.specification.parameters) {
    const value = values[parameter.key]
    if (value === undefined) continue
    if (parameter.control === 'select') {
      if (
        typeof value === 'string' &&
        parameter.options.some((option) => option.value === value)
      ) {
        result[parameter.key] = value
      }
      continue
    }
    if (parameter.control === 'integer') {
      const effectiveMax =
        parameter.request_key === 'n'
          ? Math.min(parameter.max, IMAGE_STUDIO_MAX_OUTPUTS)
          : parameter.max
      if (
        typeof value === 'number' &&
        Number.isInteger(value) &&
        value >= parameter.min &&
        value <= effectiveMax
      ) {
        result[parameter.key] = value
      }
      continue
    }
    if (typeof value === 'boolean') result[parameter.key] = value
  }
  return result
}

export const buildImageComposerValues = (
  profile: ImageModelProfile,
  input?: Partial<ImageComposerValues>
): ImageComposerValues => {
  const defaults = getImageProfileDefaults(profile)
  const provided = normalizeImageParameters(profile, input?.parameters ?? {})
  return {
    model_profile_id: profile.id,
    prompt: input?.prompt ?? '',
    parameters: normalizeImageParameters(profile, {
      ...defaults,
      ...provided,
    }),
    sample_id: input?.sample_id,
  }
}

export const buildCreateImageRequest = (
  quoteRequest: ImageQuoteRequest,
  quote: ImageQuote
): CreateImageRequest => ({
  ...quoteRequest,
  quote_token: quote.quote_token,
})

export const imageRequestFingerprint = (value: unknown): string =>
  JSON.stringify(
    value,
    (_key, candidate: ImageParameterValue | unknown) => candidate
  )

const canonicalizeImageSubmissionValue = (value: unknown): unknown => {
  if (Array.isArray(value)) {
    return value.map(canonicalizeImageSubmissionValue)
  }
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>)
        .sort(([left], [right]) => {
          if (left < right) return -1
          if (left > right) return 1
          return 0
        })
        .map(([key, candidate]) => [
          key,
          canonicalizeImageSubmissionValue(candidate),
        ])
    )
  }
  return value
}

const sha256Hex = async (value: BufferSource): Promise<string> => {
  if (!globalThis.crypto?.subtle) {
    throw new Error('SHA-256 is unavailable')
  }
  const digest = await globalThis.crypto.subtle.digest('SHA-256', value)
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, '0')
  ).join('')
}

export const getImageReferenceMetadata = async (
  image: Blob
): Promise<ImageReferenceMetadata> => ({
  sha256: await sha256Hex(await image.arrayBuffer()),
  size_bytes: image.size,
})

export const imageSubmissionFingerprint = async (
  request: ImageQuoteRequest
): Promise<string> => {
  if (!globalThis.crypto?.subtle) {
    throw new Error('Image submission fingerprint is unavailable')
  }
  const canonical = JSON.stringify(canonicalizeImageSubmissionValue(request))
  return sha256Hex(new TextEncoder().encode(canonical))
}

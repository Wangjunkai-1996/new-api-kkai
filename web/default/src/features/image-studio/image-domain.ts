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
import {
  clampImageOutputCount,
  getImageOutputParameter,
  normalizeImageParameters,
} from './image-parameters'
import type {
  CreateImageEditRequest,
  CreateImageRequest,
  ImageComposerValues,
  ImageEditQuoteRequest,
  ImageGeneration,
  ImageGenerationStatus,
  ImageModelProfile,
  ImageParameters,
  ImageParameterValue,
  ImageQuote,
  ImageQuoteRequest,
  ImageStudioAccessMode,
} from './types'

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

export type ImageGenerationResolution = {
  outcome: ImageGenerationOutcome
  clearDraft: boolean
}

export const resolveImageGenerationStatus = (
  status: ImageGenerationStatus
): ImageGenerationResolution => {
  let outcome: ImageGenerationOutcome = 'failure'
  if (status === 'succeeded' || status === 'partial') outcome = 'success'
  else if (status === 'submitting') outcome = 'pending'
  return { outcome, clearDraft: outcome === 'success' }
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

export const buildImageComposerValues = (
  profile: ImageModelProfile,
  input?: Partial<ImageComposerValues>
): ImageComposerValues => {
  const defaults = getImageProfileDefaults(profile)
  const inputParameters = { ...input?.parameters }
  const outputParameter = getImageOutputParameter(profile)
  if (outputParameter) {
    const defaultCount = defaults[outputParameter.key]
    const fallback = typeof defaultCount === 'number' ? defaultCount : 1
    inputParameters[outputParameter.key] = clampImageOutputCount(
      profile,
      inputParameters[outputParameter.key],
      fallback
    )
    defaults[outputParameter.key] = clampImageOutputCount(profile, defaultCount)
  }
  const provided = normalizeImageParameters(profile, inputParameters)
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

export const buildCreateImageEditRequest = (
  quoteRequest: ImageEditQuoteRequest,
  quote: ImageQuote
): CreateImageEditRequest => ({
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

export const imageSubmissionFingerprint = async (
  request: ImageQuoteRequest
): Promise<string> => {
  if (!globalThis.crypto?.subtle) {
    throw new Error('Image submission fingerprint is unavailable')
  }
  const canonical = JSON.stringify(canonicalizeImageSubmissionValue(request))
  return sha256Hex(new TextEncoder().encode(canonical))
}

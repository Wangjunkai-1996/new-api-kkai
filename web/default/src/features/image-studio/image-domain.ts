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
  ImageComposerValues,
  ImageModelProfile,
  ImageParameters,
  ImageParameterValue,
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

export const imageRequestFingerprint = (value: unknown): string =>
  JSON.stringify(
    value,
    (_key, candidate: ImageParameterValue | unknown) => candidate
  )

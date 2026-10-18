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
  ImageIntegerParameter,
  ImageModelProfile,
  ImageParameterControl,
  ImageParameters,
  ImageParameterValue,
  ImageSizeConstraints,
  ImageSelectParameter,
} from './types'

export const IMAGE_STUDIO_MAX_OUTPUTS = 4

export const getImageSizeParameter = (
  profile: ImageModelProfile
): ImageSelectParameter | undefined => {
  const parameter = profile.specification.parameters.find(
    (candidate) => candidate.request_key === 'size'
  )
  return parameter?.control === 'select' ? parameter : undefined
}

export const getImageOutputParameter = (
  profile: ImageModelProfile
): ImageIntegerParameter | undefined => {
  const parameter = profile.specification.parameters.find(
    (candidate) => candidate.request_key === 'n'
  )
  return parameter?.control === 'integer' ? parameter : undefined
}

export const getImageProfileMaxOutputs = (
  profile: ImageModelProfile
): number => {
  const parameter = getImageOutputParameter(profile)
  if (!parameter) return 1
  return Math.max(
    1,
    Math.min(
      parameter.max,
      profile.effective_max_outputs,
      IMAGE_STUDIO_MAX_OUTPUTS
    )
  )
}

export const clampImageOutputCount = (
  profile: ImageModelProfile,
  value: unknown,
  fallback = 1
): number => {
  const max = getImageProfileMaxOutputs(profile)
  const candidate =
    typeof value === 'number' && Number.isInteger(value) ? value : fallback
  return Math.max(1, Math.min(candidate, max))
}

export const getImageOutputCount = (
  profile: ImageModelProfile,
  parameters: Readonly<Record<string, unknown>>
): number => {
  const parameter = getImageOutputParameter(profile)
  if (!parameter) return 1
  const defaultValue = profile.default_parameters[parameter.key]
  const fallback = typeof defaultValue === 'number' ? defaultValue : 1
  return clampImageOutputCount(profile, parameters[parameter.key], fallback)
}

export type ImageParameterValidationError = {
  code:
    | 'required'
    | 'invalid_option'
    | 'invalid_size'
    | 'invalid_integer'
    | 'out_of_range'
    | 'invalid_boolean'
  parameterKey: string
  min?: number
  max?: number
}

const IMAGE_SIZE_PATTERN = /^(\d+)x(\d+)$/

export const isValidImageCustomSize = (
  value: unknown,
  constraints: ImageSizeConstraints | undefined
): value is string => {
  if (typeof value !== 'string' || !constraints) return false
  const match = IMAGE_SIZE_PATTERN.exec(value)
  if (!match) return false
  const width = Number(match[1])
  const height = Number(match[2])
  if (`${width}x${height}` !== value) return false
  if (
    !Number.isSafeInteger(width) ||
    !Number.isSafeInteger(height) ||
    width <= 0 ||
    height <= 0 ||
    width > constraints.max_edge ||
    height > constraints.max_edge ||
    width % constraints.step !== 0 ||
    height % constraints.step !== 0
  ) {
    return false
  }
  const pixels = width * height
  const aspectRatio = Math.max(width, height) / Math.min(width, height)
  return (
    Number.isSafeInteger(pixels) &&
    pixels >= constraints.min_pixels &&
    pixels <= constraints.max_pixels &&
    aspectRatio <= constraints.max_aspect_ratio
  )
}

export type ImageParametersParseResult =
  | { success: true; parameters: ImageParameters }
  | { success: false; errors: ImageParameterValidationError[] }

export const validateImageParameterValue = (
  parameter: ImageParameterControl,
  value: unknown,
  maxOutputs = IMAGE_STUDIO_MAX_OUTPUTS,
  customSize?: ImageSizeConstraints
): ImageParameterValidationError | undefined => {
  if (value === undefined || value === '') {
    return parameter.required
      ? { code: 'required', parameterKey: parameter.key }
      : undefined
  }

  if (parameter.control === 'select') {
    if (typeof value !== 'string') {
      return { code: 'invalid_option', parameterKey: parameter.key }
    }
    if (parameter.options.some((option) => option.value === value)) {
      return undefined
    }
    return parameter.request_key === 'size' &&
      isValidImageCustomSize(value, customSize)
      ? undefined
      : {
          code:
            customSize && parameter.request_key === 'size'
              ? 'invalid_size'
              : 'invalid_option',
          parameterKey: parameter.key,
        }
  }

  if (parameter.control === 'integer') {
    if (typeof value !== 'number' || !Number.isInteger(value)) {
      return { code: 'invalid_integer', parameterKey: parameter.key }
    }
    const effectiveMax =
      parameter.request_key === 'n'
        ? Math.min(parameter.max, maxOutputs, IMAGE_STUDIO_MAX_OUTPUTS)
        : parameter.max
    if (value < parameter.min || value > effectiveMax) {
      return {
        code: 'out_of_range',
        parameterKey: parameter.key,
        min: parameter.min,
        max: effectiveMax,
      }
    }
    return undefined
  }

  return typeof value === 'boolean'
    ? undefined
    : { code: 'invalid_boolean', parameterKey: parameter.key }
}

export const parseImageParameters = (
  profile: ImageModelProfile,
  values: Record<string, unknown>
): ImageParametersParseResult => {
  const parameters: ImageParameters = {}
  const errors: ImageParameterValidationError[] = []

  for (const parameter of profile.specification.parameters) {
    const value = values[parameter.key]
    const error = validateImageParameterValue(
      parameter,
      value,
      profile.effective_max_outputs,
      profile.specification.custom_size
    )
    if (error) {
      errors.push(error)
      continue
    }
    if (value !== undefined && value !== '') {
      parameters[parameter.key] = value as ImageParameterValue
    }
  }

  return errors.length > 0
    ? { success: false, errors }
    : { success: true, parameters }
}

export const normalizeImageParameters = (
  profile: ImageModelProfile,
  values: ImageParameters
): ImageParameters => {
  const result: ImageParameters = {}
  for (const parameter of profile.specification.parameters) {
    const value = values[parameter.key]
    if (
      validateImageParameterValue(
        parameter,
        value,
        profile.effective_max_outputs,
        profile.specification.custom_size
      ) === undefined &&
      value !== undefined &&
      value !== ''
    ) {
      result[parameter.key] = value
    }
  }
  return result
}

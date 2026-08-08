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
  ImageModelProfile,
  ImageParameterControl,
  ImageParameters,
  ImageParameterValue,
} from './types'

export const IMAGE_STUDIO_MAX_OUTPUTS = 4

export type ImageParameterValidationError = {
  code:
    | 'required'
    | 'invalid_option'
    | 'invalid_integer'
    | 'out_of_range'
    | 'invalid_boolean'
  parameterKey: string
  min?: number
  max?: number
}

export type ImageParametersParseResult =
  | { success: true; parameters: ImageParameters }
  | { success: false; errors: ImageParameterValidationError[] }

export const validateImageParameterValue = (
  parameter: ImageParameterControl,
  value: unknown
): ImageParameterValidationError | undefined => {
  if (value === undefined || value === '') {
    return parameter.required
      ? { code: 'required', parameterKey: parameter.key }
      : undefined
  }

  if (parameter.control === 'select') {
    return typeof value !== 'string' ||
      !parameter.options.some((option) => option.value === value)
      ? { code: 'invalid_option', parameterKey: parameter.key }
      : undefined
  }

  if (parameter.control === 'integer') {
    if (typeof value !== 'number' || !Number.isInteger(value)) {
      return { code: 'invalid_integer', parameterKey: parameter.key }
    }
    const effectiveMax =
      parameter.request_key === 'n'
        ? Math.min(parameter.max, IMAGE_STUDIO_MAX_OUTPUTS)
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
    const error = validateImageParameterValue(parameter, value)
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
      validateImageParameterValue(parameter, value) === undefined &&
      value !== undefined &&
      value !== ''
    ) {
      result[parameter.key] = value
    }
  }
  return result
}

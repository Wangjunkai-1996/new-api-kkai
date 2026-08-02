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
import { z } from 'zod'

import type {
  ImageModelProfile,
  ImageModelProfileInput,
  ImageParameterControl,
  ImageParameterValue,
  ImageSample,
  ImageSampleInput,
} from '../types'

export const IMAGE_REQUEST_FIELDS = [
  'n',
  'size',
  'quality',
  'style',
  'background',
  'output_format',
  'output_compression',
  'moderation',
  'watermark',
] as const

export type ImageRequestField = (typeof IMAGE_REQUEST_FIELDS)[number]

const controlForRequestField = (
  field: ImageRequestField
): ImageParameterControl['control'] => {
  if (field === 'n' || field === 'output_compression') return 'integer'
  if (field === 'watermark') return 'boolean'
  return 'select'
}

export const imageParameterFormSchema = z.object({
  key: z
    .string()
    .trim()
    .regex(/^[a-z][a-z0-9_]{0,63}$/, 'imageStudio.validation.parameterKey'),
  label: z
    .string()
    .trim()
    .min(1, 'imageStudio.validation.parameterLabel')
    .max(128, 'imageStudio.validation.parameterLabel'),
  request_key: z.enum(IMAGE_REQUEST_FIELDS),
  required: z.boolean(),
  has_default: z.boolean(),
  default_value: z.union([z.string(), z.number().int(), z.boolean()]),
  options_text: z.string(),
  min: z.number().int(),
  max: z.number().int(),
})

export type ImageParameterFormValues = z.infer<typeof imageParameterFormSchema>

export const imageModelFormSchema = z
  .object({
    model: z.string().trim().min(1, 'imageStudio.validation.modelRequired'),
    display_name: z
      .string()
      .trim()
      .min(1, 'imageStudio.validation.nameRequired')
      .max(191, 'imageStudio.validation.nameTooLong'),
    description: z
      .string()
      .trim()
      .max(4000, 'imageStudio.validation.descriptionTooLong'),
    provider_label: z
      .string()
      .trim()
      .max(128, 'imageStudio.validation.providerTooLong'),
    enabled: z.boolean(),
    sort_order: z
      .number()
      .int()
      .min(-100000, 'imageStudio.validation.sortOutOfRange')
      .max(100000, 'imageStudio.validation.sortOutOfRange'),
    parameters: z
      .array(imageParameterFormSchema)
      .max(9, 'imageStudio.validation.parametersTooMany'),
  })
  .superRefine((values, context) => {
    const keys = new Set<string>()
    const requestKeys = new Set<string>()
    values.parameters.forEach((parameter, index) => {
      if (keys.has(parameter.key)) {
        context.addIssue({
          code: 'custom',
          path: ['parameters', index, 'key'],
          message: 'imageStudio.validation.parameterDuplicate',
        })
      }
      if (requestKeys.has(parameter.request_key)) {
        context.addIssue({
          code: 'custom',
          path: ['parameters', index, 'request_key'],
          message: 'imageStudio.validation.requestFieldDuplicate',
        })
      }
      keys.add(parameter.key)
      requestKeys.add(parameter.request_key)
      validateParameterDetails(parameter, index, context)
    })
  })

export type ImageModelFormValues = z.infer<typeof imageModelFormSchema>

const parseOptions = (value: string): Array<{ label: string; value: string }> =>
  value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const separator = line.indexOf('=')
      if (separator < 0) return { label: line, value: line }
      return {
        label: line.slice(0, separator).trim(),
        value: line.slice(separator + 1).trim(),
      }
    })

const validateParameterDetails = (
  parameter: ImageParameterFormValues,
  index: number,
  context: z.RefinementCtx
): void => {
  const control = controlForRequestField(parameter.request_key)
  if (parameter.required && !parameter.has_default) {
    context.addIssue({
      code: 'custom',
      path: ['parameters', index, 'default_value'],
      message: 'imageStudio.validation.requiredDefault',
    })
  }
  if (control === 'select') {
    const options = parseOptions(parameter.options_text)
    const optionValues = new Set(options.map((option) => option.value))
    if (
      options.length === 0 ||
      options.length > 32 ||
      optionValues.size !== options.length ||
      options.some(
        (option) =>
          !option.label ||
          !option.value ||
          option.label.length > 128 ||
          option.value.length > 128
      )
    ) {
      context.addIssue({
        code: 'custom',
        path: ['parameters', index, 'options_text'],
        message: 'imageStudio.validation.optionsRequired',
      })
    }
    if (
      parameter.has_default &&
      !options.some((option) => option.value === parameter.default_value)
    ) {
      context.addIssue({
        code: 'custom',
        path: ['parameters', index, 'default_value'],
        message: 'imageStudio.validation.defaultInvalid',
      })
    }
    return
  }
  if (control === 'integer') {
    const allowedMin = parameter.request_key === 'n' ? 1 : 0
    const allowedMax = parameter.request_key === 'n' ? 128 : 100
    if (
      parameter.min < allowedMin ||
      parameter.max > allowedMax ||
      parameter.min > parameter.max
    ) {
      context.addIssue({
        code: 'custom',
        path: ['parameters', index, 'min'],
        message: 'imageStudio.validation.rangeInvalid',
      })
    }
    if (
      parameter.has_default &&
      (typeof parameter.default_value !== 'number' ||
        parameter.default_value < parameter.min ||
        parameter.default_value > parameter.max)
    ) {
      context.addIssue({
        code: 'custom',
        path: ['parameters', index, 'default_value'],
        message: 'imageStudio.validation.defaultInvalid',
      })
    }
    return
  }
  if (
    control === 'boolean' &&
    parameter.has_default &&
    typeof parameter.default_value !== 'boolean'
  ) {
    context.addIssue({
      code: 'custom',
      path: ['parameters', index, 'default_value'],
      message: 'imageStudio.validation.defaultInvalid',
    })
  }
}

const parameterFormValues = (
  parameter: ImageParameterControl,
  defaultValue: ImageParameterValue | undefined
): ImageParameterFormValues => ({
  key: parameter.key,
  label: parameter.label,
  request_key: parameter.request_key as ImageRequestField,
  required: parameter.required ?? false,
  has_default: defaultValue !== undefined,
  default_value: defaultValue ?? (parameter.control === 'boolean' ? false : ''),
  options_text:
    parameter.control === 'select'
      ? parameter.options
          .map((option) => `${option.label}=${option.value}`)
          .join('\n')
      : '',
  min: parameter.control === 'integer' ? parameter.min : 0,
  max: parameter.control === 'integer' ? parameter.max : 100,
})

export const createImageModelFormValues = (
  profile?: ImageModelProfile,
  candidate = ''
): ImageModelFormValues => ({
  model: profile?.model ?? candidate,
  display_name: profile?.display_name ?? candidate,
  description: profile?.description ?? '',
  provider_label: profile?.provider_label ?? '',
  enabled: profile?.enabled ?? false,
  sort_order: profile?.sort_order ?? 0,
  parameters:
    profile?.specification.parameters.map((parameter) =>
      parameterFormValues(parameter, profile.default_parameters[parameter.key])
    ) ?? [],
})

const parameterFromForm = (
  parameter: ImageParameterFormValues
): ImageParameterControl => {
  const common = {
    key: parameter.key.trim(),
    label: parameter.label.trim(),
    request_key: parameter.request_key,
    required: parameter.required || undefined,
  }
  const control = controlForRequestField(parameter.request_key)
  if (control === 'select') {
    return { ...common, control, options: parseOptions(parameter.options_text) }
  }
  if (control === 'integer') {
    return { ...common, control, min: parameter.min, max: parameter.max }
  }
  return { ...common, control }
}

const parameterSpecificationFingerprint = (
  parameters: ImageParameterControl[]
): string =>
  JSON.stringify(
    parameters.map((parameter) => {
      const common = {
        key: parameter.key,
        label: parameter.label,
        request_key: parameter.request_key,
        required: parameter.required === true,
        control: parameter.control,
      }
      if (parameter.control === 'select') {
        return {
          ...common,
          options: parameter.options.map((option) => ({
            label: option.label,
            value: option.value,
          })),
        }
      }
      if (parameter.control === 'integer') {
        return { ...common, min: parameter.min, max: parameter.max }
      }
      return common
    })
  )

export const parseImageModelForm = (
  values: ImageModelFormValues,
  current?: ImageModelProfile
): ImageModelProfileInput => {
  const parameters = values.parameters.map(parameterFromForm)
  const defaults: Record<string, ImageParameterValue> = {}
  values.parameters.forEach((parameter) => {
    if (parameter.has_default) defaults[parameter.key] = parameter.default_value
  })
  const snapshot = parameterSpecificationFingerprint(parameters)
  const currentSnapshot = parameterSpecificationFingerprint(
    current?.specification.parameters ?? []
  )
  const version = current
    ? current.specification_version + (snapshot === currentSnapshot ? 0 : 1)
    : 1
  return {
    model: values.model.trim(),
    display_name: values.display_name.trim(),
    description: values.description.trim(),
    provider_label: values.provider_label.trim(),
    enabled: values.enabled,
    sort_order: values.sort_order,
    specification: { version, parameters },
    default_parameters: defaults,
  }
}

export const imageSampleFormSchema = z.object({
  model_profile_id: z
    .number()
    .int()
    .positive('imageStudio.validation.modelRequired'),
  image_asset_id: z
    .number()
    .int()
    .positive('imageStudio.validation.sampleImageRequired'),
  title: z
    .string()
    .trim()
    .min(1, 'imageStudio.validation.sampleTitleRequired')
    .max(191, 'imageStudio.validation.sampleTitleTooLong'),
  prompt: z
    .string()
    .trim()
    .min(1, 'imageStudio.validation.promptRequired')
    .max(8000, 'imageStudio.validation.promptTooLong'),
  parameters: z.record(
    z.string(),
    z.union([z.string(), z.number().finite(), z.boolean()])
  ),
  category: z
    .string()
    .trim()
    .regex(
      /^[a-z0-9][a-z0-9_-]{0,31}$/,
      'imageStudio.validation.categoryInvalid'
    ),
  status: z.enum(['draft', 'published']),
  sort_order: z
    .number()
    .int()
    .min(-100000, 'imageStudio.validation.sortOutOfRange')
    .max(100000, 'imageStudio.validation.sortOutOfRange'),
})

export type ImageSampleFormValues = z.infer<typeof imageSampleFormSchema>

export const createImageSampleFormValues = (
  sample?: ImageSample,
  profile?: ImageModelProfile
): ImageSampleFormValues => ({
  model_profile_id: sample?.model_profile_id ?? profile?.id ?? 0,
  image_asset_id: sample?.image_asset_id ?? 0,
  title: sample?.title ?? '',
  prompt: sample?.prompt ?? '',
  parameters: sample?.parameters ?? profile?.default_parameters ?? {},
  category: sample?.category ?? 'general',
  status: sample?.status ?? 'draft',
  sort_order: sample?.sort_order ?? 0,
})

export const parseImageSampleForm = (
  values: ImageSampleFormValues
): ImageSampleInput => ({
  ...values,
  title: values.title.trim(),
  prompt: values.prompt.trim(),
  category: values.category.trim().toLowerCase(),
})

export const controlForImageRequestField = controlForRequestField

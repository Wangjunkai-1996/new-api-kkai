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
  VideoComposerValues,
  VideoModelProfile,
  VideoModelProfileInput,
  VideoParameterValue,
  VideoSampleInput,
  VideoTokenCapability,
  VideoTokenCreateResult,
} from './types'
import { videoParameterAcceptsValue } from './video-domain'

export const VIDEO_GENERATION_MODES = [
  'text_to_video',
  'image_to_video',
  'first_last_frame',
] as const

export const VIDEO_PROMPT_MAX_LENGTH = 8_000

const videoTokenSummarySchema = z.object({
  id: z.number().int().positive(),
  name: z.string(),
  group: z.string(),
})

export const videoTokenCapabilitySchema = z.object({
  required_group: z.string().min(1),
  has_usable_token: z.boolean(),
  can_create: z.boolean(),
  effective_models: z.array(z.string().min(1)).optional(),
  token: videoTokenSummarySchema.nullish(),
  status: z.enum([
    'ready',
    'missing',
    'group_unavailable',
    'limit_reached',
    'models_unavailable',
  ]),
}) satisfies z.ZodType<VideoTokenCapability>

export const videoTokenCreateResultSchema = videoTokenCapabilitySchema.extend({
  created: z.boolean(),
}) satisfies z.ZodType<VideoTokenCreateResult>

const videoParameterValueSchema = z.union([
  z.string(),
  z.number().finite(),
  z.boolean(),
])

export const videoComposerSchema = z.object({
  model_profile_id: z
    .number()
    .int()
    .refine((value) => value !== 0, 'videoStudio.validation.modelRequired'),
  mode: z.enum(VIDEO_GENERATION_MODES),
  prompt: z
    .string()
    .trim()
    .min(1, 'videoStudio.validation.promptRequired')
    .max(VIDEO_PROMPT_MAX_LENGTH, 'videoStudio.validation.promptTooLong'),
  reference_asset_ids: z.array(z.number().int().positive()).max(2),
  parameters: z.record(z.string(), videoParameterValueSchema),
}) satisfies z.ZodType<VideoComposerValues>

export const videoModelSpecSchema = z.object({
  version: z.number().int().positive(),
  modes: z.array(z.enum(VIDEO_GENERATION_MODES)).min(1),
  parameters: z.array(
    z.discriminatedUnion('control', [
      z.object({
        control: z.enum(['segmented', 'select']),
        key: z.string().min(1),
        label: z.string().min(1),
        request_key: z.string().optional(),
        modes: z.array(z.enum(VIDEO_GENERATION_MODES)).optional(),
        required: z.boolean().optional(),
        default: videoParameterValueSchema.optional(),
        options: z
          .array(
            z.object({
              label: z.string().min(1),
              value: videoParameterValueSchema,
            })
          )
          .min(1),
      }),
      z.object({
        control: z.enum(['slider', 'number']),
        key: z.string().min(1),
        label: z.string().min(1),
        request_key: z.string().optional(),
        modes: z.array(z.enum(VIDEO_GENERATION_MODES)).optional(),
        required: z.boolean().optional(),
        default: z.number().finite().optional(),
        min: z.number().finite(),
        max: z.number().finite(),
        step: z.number().positive(),
      }),
      z.object({
        control: z.literal('switch'),
        key: z.string().min(1),
        label: z.string().min(1),
        request_key: z.string().optional(),
        modes: z.array(z.enum(VIDEO_GENERATION_MODES)).optional(),
        required: z.boolean().optional(),
        default: z.boolean().optional(),
      }),
    ])
  ),
  reference_inputs: z
    .array(
      z.object({
        role: z.enum([
          'reference',
          'reference_video',
          'first_frame',
          'last_frame',
        ]),
        request_key: z.string().min(1),
        required: z.boolean(),
      })
    )
    .optional(),
})

export const videoModelProfileFormSchema = z.object({
  model: z.string().trim().min(1, 'videoStudio.validation.modelRequired'),
  display_name: z.string().trim().min(1, 'videoStudio.validation.nameRequired'),
  description: z.string().trim().optional(),
  provider_label: z.string().trim().optional(),
  enabled: z.boolean(),
  sort_order: z.number().int(),
  specification_json: z.string().min(1),
  default_parameters_json: z.string().min(1),
})

export type VideoModelProfileFormValues = z.infer<
  typeof videoModelProfileFormSchema
>

export const videoSampleFormSchema = z.object({
  model_profile_id: z
    .number()
    .int()
    .positive('videoStudio.validation.modelRequired'),
  title: z.string().trim().min(1, 'videoStudio.validation.nameRequired'),
  prompt: z
    .string()
    .trim()
    .min(1, 'videoStudio.validation.promptRequired')
    .max(VIDEO_PROMPT_MAX_LENGTH, 'videoStudio.validation.promptTooLong'),
  mode: z.enum(VIDEO_GENERATION_MODES),
  parameters_json: z.string().min(1),
  reference_asset_ids: z.array(z.number().int().positive()),
  video_asset_id: z
    .number()
    .int()
    .positive('videoStudio.validation.sampleVideoRequired'),
  status: z.enum(['draft', 'published']),
  sort_order: z.number().int(),
})

export type VideoSampleFormValues = z.infer<typeof videoSampleFormSchema>

const parseParameterRecord = (
  raw: string
): Record<string, VideoParameterValue> => {
  const parsed: unknown = JSON.parse(raw)
  return z.record(z.string(), videoParameterValueSchema).parse(parsed)
}

export const parseVideoModelProfileForm = (
  values: VideoModelProfileFormValues
): VideoModelProfileInput => {
  return {
    model: values.model,
    display_name: values.display_name,
    description: values.description ?? '',
    provider_label: values.provider_label ?? '',
    enabled: values.enabled,
    sort_order: values.sort_order,
    specification: videoModelSpecSchema.parse(
      JSON.parse(values.specification_json)
    ),
    default_parameters: parseParameterRecord(values.default_parameters_json),
  }
}

export const parseVideoSampleForm = (
  values: VideoSampleFormValues
): VideoSampleInput => {
  return {
    model_profile_id: values.model_profile_id,
    title: values.title,
    prompt: values.prompt,
    mode: values.mode,
    parameters: parseParameterRecord(values.parameters_json),
    reference_asset_ids: values.reference_asset_ids,
    video_asset_id: values.video_asset_id,
    aspect_ratio: 0,
    status: values.status,
    sort_order: values.sort_order,
  }
}

export const validateComposerForProfile = (
  values: VideoComposerValues,
  profile: VideoModelProfile
): string | null => {
  if (!profile.specification.modes.includes(values.mode)) {
    return 'videoStudio.validation.modeUnsupported'
  }

  if (values.prompt.trim().length > VIDEO_PROMPT_MAX_LENGTH) {
    return 'videoStudio.validation.promptTooLong'
  }

  const requiredReferences = (
    profile.specification.reference_inputs ?? []
  ).filter((input) => {
    if (!input.required || values.mode === 'text_to_video') return false
    if (values.mode === 'image_to_video') {
      return input.role === 'reference' || input.role === 'reference_video'
    }
    return input.role === 'first_frame' || input.role === 'last_frame'
  }).length
  if (values.reference_asset_ids.length !== requiredReferences) {
    return requiredReferences === 2
      ? 'videoStudio.validation.twoFramesRequired'
      : 'videoStudio.validation.imageRequired'
  }

  const controls = profile.specification.parameters.filter(
    (control) => !control.modes || control.modes.includes(values.mode)
  )
  const allowedKeys = new Set(controls.map((control) => control.key))
  if (Object.keys(values.parameters).some((key) => !allowedKeys.has(key))) {
    return 'videoStudio.validation.parameterInvalid'
  }

  for (const control of controls) {
    const value = values.parameters[control.key]
    if (control.required && value === undefined) {
      return 'videoStudio.validation.parameterRequired'
    }
    if (value === undefined) continue
    if (!videoParameterAcceptsValue(control, value)) {
      return control.control === 'slider' || control.control === 'number'
        ? 'videoStudio.validation.parameterOutOfRange'
        : 'videoStudio.validation.parameterInvalid'
    }
  }

  return null
}

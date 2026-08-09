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
  VideoAsset,
  VideoComposerValues,
  VideoGenerationMode,
  VideoModelProfile,
  VideoModelProfileInput,
  VideoParameterControl,
  VideoParameterOption,
  VideoParameterValue,
  VideoParameters,
  VideoReferenceInput,
  VideoSample,
  VideoSampleInput,
  VideoTokenCapability,
  VideoTokenCreateResult,
} from './types'
import {
  getVideoParametersForMode,
  getVideoReferenceRoles,
  videoParameterAcceptsValue,
} from './video-domain'
import { VIDEO_SAMPLE_CATEGORIES } from './video-sample-categories'

export const VIDEO_GENERATION_MODES = [
  'text_to_video',
  'image_to_video',
  'first_last_frame',
] as const

export const VIDEO_PROMPT_MAX_LENGTH = 8_000
export const VIDEO_MODEL_DISPLAY_NAME_MAX_LENGTH = 191
export const VIDEO_MODEL_DESCRIPTION_MAX_LENGTH = 4_000
export const VIDEO_MODEL_PROVIDER_LABEL_MAX_LENGTH = 128
export const VIDEO_MODEL_SORT_ORDER_MIN = -100_000
export const VIDEO_MODEL_SORT_ORDER_MAX = 100_000
export const VIDEO_SAMPLE_TITLE_MAX_LENGTH = 191
export const VIDEO_SAMPLE_SORT_ORDER_MIN = -100_000
export const VIDEO_SAMPLE_SORT_ORDER_MAX = 100_000

export const VIDEO_PARAMETER_LABEL_KEYS: Readonly<Record<string, string>> = {
  duration: 'videoStudio.parameter.durationSeconds',
  ratio: 'videoStudio.parameter.aspectRatio',
  resolution: 'videoStudio.admin.outputResolution',
  generate_audio: 'videoStudio.parameter.generateAudio',
}

export const VIDEO_PARAMETER_OPTION_LABEL_KEYS: Readonly<
  Record<string, Readonly<Record<string, string>>>
> = {
  ratio: {
    '16:9': 'videoStudio.ratio.landscape16x9',
    '9:16': 'videoStudio.ratio.portrait9x16',
    '1:1': 'videoStudio.ratio.square1x1',
    '4:3': 'videoStudio.ratio.landscape4x3',
    '3:4': 'videoStudio.ratio.portrait3x4',
    '21:9': 'videoStudio.ratio.ultrawide21x9',
    adaptive: 'videoStudio.ratio.adaptive',
  },
}

const SEEDANCE_SPECIAL_RATIOS = [
  '16:9',
  '9:16',
  '1:1',
  '4:3',
  '3:4',
  '21:9',
  'adaptive',
] as const

const SEEDANCE_25_RATIOS = ['16:9', '9:16', '1:1'] as const

const VIDEO_RATIO_LABELS: Readonly<Record<string, string>> = {
  '16:9': '横屏 16:9',
  '9:16': '竖屏 9:16',
  '1:1': '方形 1:1',
  '4:3': '横屏 4:3',
  '3:4': '竖屏 3:4',
  '21:9': '超宽屏 21:9',
  adaptive: '自动',
}

type VideoModelPresetDetails = {
  displayName: string
  description: string
  resolution: string
  requiresVideoReference: boolean
  durationRequestKey?: 'duration' | 'seconds'
  maxDuration?: number
  ratios?: readonly string[]
  resolutions?: readonly string[]
  supportsGenerateAudio?: boolean
}

const VIDEO_MODEL_PRESET_DETAILS = {
  'sd_2.0_fast_special_720p': {
    displayName: 'Seedance 2.0 Fast 720p',
    description: '快速 720p 文生 / 图生视频',
    resolution: '720p',
    requiresVideoReference: false,
  },
  'sd_2.0_special_720p': {
    displayName: 'Seedance 2.0 720p',
    description: '720p 文生 / 图生视频',
    resolution: '720p',
    requiresVideoReference: false,
  },
  'sd_2.0_special_1080p': {
    displayName: 'Seedance 2.0 1080p',
    description: '1080p 文生 / 图生视频',
    resolution: '1080p',
    requiresVideoReference: false,
  },
  'sd_2.0_special_2k': {
    displayName: 'Seedance 2.0 2K',
    description: '2K 文生 / 图生视频',
    resolution: '2K',
    requiresVideoReference: false,
  },
  'sd_2.0_special_4k': {
    displayName: 'Seedance 2.0 4K',
    description: '4K 文生 / 图生视频',
    resolution: '4K',
    requiresVideoReference: false,
  },
  'sd_2.0_fast_special_720p_with_video_ref': {
    displayName: 'Seedance 2.0 Fast 720p · 视频参考',
    description: '快速 720p 视频参考生成',
    resolution: '720p',
    requiresVideoReference: true,
  },
  'sd_2.0_special_720p_with_video_ref': {
    displayName: 'Seedance 2.0 720p · 视频参考',
    description: '720p 视频参考生成',
    resolution: '720p',
    requiresVideoReference: true,
  },
  'sd_2.0_special_1080p_with_video_ref': {
    displayName: 'Seedance 2.0 1080p · 视频参考',
    description: '1080p 视频参考生成',
    resolution: '1080p',
    requiresVideoReference: true,
  },
  'sd_2.0_special_2k_with_video_ref': {
    displayName: 'Seedance 2.0 2K · 视频参考',
    description: '2K 视频参考生成',
    resolution: '2K',
    requiresVideoReference: true,
  },
  'sd_2.0_special_4k_with_video_ref': {
    displayName: 'Seedance 2.0 4K · 视频参考',
    description: '4K 视频参考生成',
    resolution: '4K',
    requiresVideoReference: true,
  },
} as const satisfies Record<string, VideoModelPresetDetails>

const displayResolution = (resolution: string): string =>
  resolution === '2k' || resolution === '4k'
    ? resolution.toUpperCase()
    : resolution

const getSeedance25PresetDetails = (
  model: string
): VideoModelPresetDetails | undefined => {
  const match = /^seedance-2\.5(?:-(480p|720p|1080p|2k|4k))?$/.exec(model)
  if (!match) return undefined
  const resolution = match[1] ?? '720p'
  const resolutionLabel = displayResolution(resolution)
  return {
    displayName: `Seedance 2.5 ${resolutionLabel}`,
    description: `${resolutionLabel} 文生 / 图生视频`,
    resolution,
    requiresVideoReference: false,
    durationRequestKey: 'duration',
    maxDuration: 30,
    ratios: SEEDANCE_25_RATIOS,
    resolutions: [resolution],
    supportsGenerateAudio: false,
  }
}

const getVideoModelPresetDetails = (
  model: string
): VideoModelPresetDetails | undefined =>
  VIDEO_MODEL_PRESET_DETAILS[
    model as keyof typeof VIDEO_MODEL_PRESET_DETAILS
  ] ?? getSeedance25PresetDetails(model)

export type VideoModelPreset = VideoModelProfileInput & {
  resolution: string
}

export const getVideoModelPreset = (
  model: string
): VideoModelPreset | undefined => {
  const details = getVideoModelPresetDetails(model)
  if (!details) return undefined

  const modes: VideoGenerationMode[] = details.requiresVideoReference
    ? ['image_to_video']
    : ['text_to_video', 'image_to_video']
  const parameters: VideoParameterControl[] = [
    {
      key: 'duration',
      label: '时长（秒）',
      request_key: details.durationRequestKey ?? 'seconds',
      control: 'number',
      required: true,
      min: 4,
      max: details.maxDuration ?? 15,
      step: 1,
    },
    {
      key: 'ratio',
      label: '画幅比例',
      control: 'select',
      required: true,
      options: (details.ratios ?? SEEDANCE_SPECIAL_RATIOS).map((ratio) => ({
        label: VIDEO_RATIO_LABELS[ratio] ?? ratio,
        value: ratio,
      })),
    },
  ]
  const defaultParameters: VideoParameters = {
    duration: 5,
    ratio: '16:9',
  }
  if (details.resolutions) {
    parameters.push({
      key: 'resolution',
      label: '输出分辨率',
      control: 'select',
      required: true,
      options: details.resolutions.map((resolution) => ({
        label: displayResolution(resolution),
        value: resolution,
      })),
    })
    defaultParameters.resolution = details.resolution
  }
  if (details.supportsGenerateAudio !== false) {
    parameters.push({
      key: 'generate_audio',
      label: '生成音频',
      control: 'switch',
      required: true,
    })
    defaultParameters.generate_audio = true
  }
  return {
    model,
    display_name: details.displayName,
    description: details.description,
    provider_label: 'Seedance Global',
    enabled: false,
    sort_order: 0,
    resolution: details.resolution,
    specification: {
      version: 1,
      modes,
      parameters,
      reference_inputs: details.requiresVideoReference
        ? [
            {
              role: 'reference_video',
              request_key: 'reference_video',
              required: true,
            },
          ]
        : [
            {
              role: 'reference',
              request_key: 'reference_image',
              required: true,
            },
          ],
    },
    default_parameters: defaultParameters,
  }
}

export const getVideoModelCandidateLabel = (model: string): string =>
  getVideoModelPreset(model)?.display_name ?? model

const getVideoModelPresetDefaults = (
  preset: VideoModelPreset,
  configured: VideoParameters = {}
): VideoParameters =>
  Object.fromEntries(
    preset.specification.parameters.flatMap((parameter) => {
      const configuredValue = configured[parameter.key]
      const value =
        configuredValue !== undefined &&
        videoParameterAcceptsValue(parameter, configuredValue)
          ? configuredValue
          : preset.default_parameters[parameter.key]
      return value === undefined ? [] : [[parameter.key, value]]
    })
  )

const parseVideoRatioOption = (
  value: VideoParameterValue
): number | undefined => {
  if (typeof value !== 'string') return undefined
  const match = /^(\d+):(\d+)$/.exec(value)
  if (!match) return undefined
  const width = Number(match[1])
  const height = Number(match[2])
  return width > 0 && height > 0 ? width / height : undefined
}

export const getVideoSampleAssetParameterOverrides = (
  profile: Pick<VideoModelProfile, 'specification'>,
  asset: Pick<VideoAsset, 'duration_seconds' | 'width' | 'height'>
): VideoParameters => {
  const overrides: VideoParameters = {}
  const durationParameter = profile.specification.parameters.find(
    (parameter) =>
      parameter.key === 'duration' || parameter.request_key === 'seconds'
  )
  const duration = Math.round(asset.duration_seconds)
  if (
    durationParameter &&
    duration > 0 &&
    videoParameterAcceptsValue(durationParameter, duration)
  ) {
    overrides[durationParameter.key] = duration
  }

  const audioParameter = profile.specification.parameters.find(
    (parameter) => parameter.key === 'generate_audio'
  )
  if (audioParameter && videoParameterAcceptsValue(audioParameter, true)) {
    overrides[audioParameter.key] = true
  }

  const ratioParameter = profile.specification.parameters.find(
    (parameter) => parameter.key === 'ratio'
  )
  if (
    asset.width <= 0 ||
    asset.height <= 0 ||
    !ratioParameter ||
    (ratioParameter.control !== 'segmented' &&
      ratioParameter.control !== 'select')
  ) {
    return overrides
  }

  const assetRatio = asset.width / asset.height
  const ratioOptions = ratioParameter.options.flatMap((option) => {
    const ratio = parseVideoRatioOption(option.value)
    return ratio === undefined ? [] : [{ option, ratio }]
  })
  const closest = ratioOptions.reduce<
    | { option: (typeof ratioParameter.options)[number]; distance: number }
    | undefined
  >((best, candidate) => {
    const distance = Math.abs(candidate.ratio - assetRatio) / assetRatio
    return !best || distance < best.distance
      ? { option: candidate.option, distance }
      : best
  }, undefined)
  const selectedOption =
    closest && closest.distance <= 0.03
      ? closest.option
      : ratioParameter.options.find((option) => option.value === 'adaptive')
  if (
    selectedOption &&
    videoParameterAcceptsValue(ratioParameter, selectedOption.value)
  ) {
    overrides[ratioParameter.key] = selectedOption.value
  }
  return overrides
}

export const getUnsupportedVideoSampleDuration = (
  profile: Pick<VideoModelProfile, 'specification'>,
  asset: Pick<VideoAsset, 'duration_seconds'>
): number | undefined => {
  const durationParameter = profile.specification.parameters.find(
    (parameter) =>
      parameter.key === 'duration' || parameter.request_key === 'seconds'
  )
  const duration = Math.round(asset.duration_seconds)
  if (
    !durationParameter ||
    asset.duration_seconds <= 0 ||
    duration <= 0 ||
    videoParameterAcceptsValue(durationParameter, duration)
  ) {
    return undefined
  }
  return asset.duration_seconds
}

export const getVideoSampleParametersForAsset = (
  profile: VideoModelProfile,
  mode: VideoGenerationMode,
  asset: Pick<VideoAsset, 'duration_seconds' | 'width' | 'height'>,
  currentParameters: VideoParameters = {}
): VideoParameters => ({
  ...getVideoParametersForMode(profile, mode, currentParameters),
  ...getVideoSampleAssetParameterOverrides(profile, asset),
})

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

const VIDEO_PARAMETER_CONTROLS = [
  'segmented',
  'select',
  'slider',
  'number',
  'switch',
] as const

const VIDEO_PARAMETER_VALUE_TYPES = ['string', 'number', 'boolean'] as const

const videoParameterOptionFormSchema = z
  .object({
    label: z
      .string()
      .trim()
      .min(1, 'videoStudio.validation.optionLabelRequired'),
    value_type: z.enum(VIDEO_PARAMETER_VALUE_TYPES),
    value: videoParameterValueSchema,
  })
  .superRefine((option, context) => {
    if (typeof option.value !== option.value_type) {
      context.addIssue({
        code: 'custom',
        path: ['value'],
        message: 'videoStudio.validation.optionValueInvalid',
      })
    }
  })

const videoModelParameterFormSchema = z
  .object({
    key: z
      .string()
      .trim()
      .regex(
        /^[a-z][a-z0-9_]{0,63}$/,
        'videoStudio.validation.parameterKeyInvalid'
      ),
    label: z
      .string()
      .trim()
      .min(1, 'videoStudio.validation.parameterLabelRequired'),
    request_key: z.string().trim(),
    modes: z
      .array(z.enum(VIDEO_GENERATION_MODES))
      .min(1, 'videoStudio.validation.parameterModeRequired'),
    modes_explicit: z.boolean(),
    control: z.enum(VIDEO_PARAMETER_CONTROLS),
    required: z.boolean(),
    has_default: z.boolean(),
    default_source: z.enum(['inline', 'profile']),
    default_value: videoParameterValueSchema.optional(),
    preserved_inline_default: videoParameterValueSchema.optional(),
    options: z.array(videoParameterOptionFormSchema),
    min: z.number().finite(),
    max: z.number().finite(),
    step: z.number().positive('videoStudio.validation.parameterStepInvalid'),
  })
  .superRefine((parameter, context) => {
    const isChoice =
      parameter.control === 'segmented' || parameter.control === 'select'
    const isNumeric =
      parameter.control === 'slider' || parameter.control === 'number'

    if (isChoice && parameter.options.length === 0) {
      context.addIssue({
        code: 'custom',
        path: ['options'],
        message: 'videoStudio.validation.parameterOptionRequired',
      })
    }
    if (isNumeric && parameter.min > parameter.max) {
      context.addIssue({
        code: 'custom',
        path: ['max'],
        message: 'videoStudio.validation.parameterRangeInvalid',
      })
    }
    if (parameter.required && !parameter.has_default) {
      context.addIssue({
        code: 'custom',
        path: ['has_default'],
        message: 'videoStudio.validation.parameterDefaultRequired',
      })
      return
    }
    if (!parameter.has_default) return
    if (parameter.default_value === undefined) {
      context.addIssue({
        code: 'custom',
        path: ['default_value'],
        message: 'videoStudio.validation.parameterDefaultRequired',
      })
      return
    }

    let acceptsDefault = false
    if (isChoice) {
      acceptsDefault = parameter.options.some(
        (option) => option.value === parameter.default_value
      )
    } else if (isNumeric && typeof parameter.default_value === 'number') {
      const steps = (parameter.default_value - parameter.min) / parameter.step
      acceptsDefault =
        parameter.default_value >= parameter.min &&
        parameter.default_value <= parameter.max &&
        Math.abs(steps - Math.round(steps)) <= 1e-8
    } else if (parameter.control === 'switch') {
      acceptsDefault = typeof parameter.default_value === 'boolean'
    }
    if (!acceptsDefault) {
      context.addIssue({
        code: 'custom',
        path: ['default_value'],
        message: 'videoStudio.validation.parameterDefaultInvalid',
      })
    }
  })

export const videoModelProfileFormSchema = z
  .object({
    model: z.string().trim().min(1, 'videoStudio.validation.modelRequired'),
    display_name: z
      .string()
      .trim()
      .min(1, 'videoStudio.validation.nameRequired')
      .max(
        VIDEO_MODEL_DISPLAY_NAME_MAX_LENGTH,
        'videoStudio.validation.nameTooLong'
      ),
    description: z
      .string()
      .trim()
      .max(
        VIDEO_MODEL_DESCRIPTION_MAX_LENGTH,
        'videoStudio.validation.descriptionTooLong'
      )
      .optional(),
    provider_label: z
      .string()
      .trim()
      .max(
        VIDEO_MODEL_PROVIDER_LABEL_MAX_LENGTH,
        'videoStudio.validation.providerTooLong'
      )
      .optional(),
    enabled: z.boolean(),
    sort_order: z
      .number()
      .int()
      .min(VIDEO_MODEL_SORT_ORDER_MIN, 'videoStudio.validation.sortOutOfRange')
      .max(VIDEO_MODEL_SORT_ORDER_MAX, 'videoStudio.validation.sortOutOfRange'),
    specification_version: z.number().int().positive(),
    modes: z
      .array(z.enum(VIDEO_GENERATION_MODES))
      .min(1, 'videoStudio.validation.modeRequired'),
    parameters: z.array(videoModelParameterFormSchema).max(32),
    image_reference_role: z.enum(['reference', 'reference_video']),
    image_reference_request_key: z.string().trim(),
    first_frame_request_key: z.string().trim(),
    last_frame_request_key: z.string().trim(),
  })
  .superRefine((values, context) => {
    const modes = new Set(values.modes)
    const keys = new Set<string>()
    const requestKeys = new Set<string>()
    values.parameters.forEach((parameter, index) => {
      if (keys.has(parameter.key)) {
        context.addIssue({
          code: 'custom',
          path: ['parameters', index, 'key'],
          message: 'videoStudio.validation.parameterKeyDuplicate',
        })
      }
      keys.add(parameter.key)
      if (parameter.request_key) {
        if (requestKeys.has(parameter.request_key)) {
          context.addIssue({
            code: 'custom',
            path: ['parameters', index, 'request_key'],
            message: 'videoStudio.validation.parameterRequestKeyDuplicate',
          })
        }
        requestKeys.add(parameter.request_key)
      }
      if (parameter.modes.some((mode) => !modes.has(mode))) {
        context.addIssue({
          code: 'custom',
          path: ['parameters', index, 'modes'],
          message: 'videoStudio.validation.parameterModeInvalid',
        })
      }
    })

    if (
      modes.has('image_to_video') &&
      values.image_reference_request_key.length === 0
    ) {
      context.addIssue({
        code: 'custom',
        path: ['image_reference_request_key'],
        message: 'videoStudio.validation.referenceRequestKeyRequired',
      })
    }
    if (
      modes.has('first_last_frame') &&
      values.first_frame_request_key.length === 0
    ) {
      context.addIssue({
        code: 'custom',
        path: ['first_frame_request_key'],
        message: 'videoStudio.validation.referenceRequestKeyRequired',
      })
    }
    if (
      modes.has('first_last_frame') &&
      values.last_frame_request_key.length === 0
    ) {
      context.addIssue({
        code: 'custom',
        path: ['last_frame_request_key'],
        message: 'videoStudio.validation.referenceRequestKeyRequired',
      })
    }
  })

export type VideoModelProfileFormValues = z.infer<
  typeof videoModelProfileFormSchema
>

export const videoSampleFormSchema = z.object({
  model_profile_id: z
    .number()
    .int()
    .positive('videoStudio.validation.modelRequired'),
  title: z
    .string()
    .trim()
    .min(1, 'videoStudio.validation.nameRequired')
    .max(
      VIDEO_SAMPLE_TITLE_MAX_LENGTH,
      'videoStudio.validation.sampleTitleTooLong'
    ),
  prompt: z
    .string()
    .trim()
    .min(1, 'videoStudio.validation.promptRequired')
    .max(VIDEO_PROMPT_MAX_LENGTH, 'videoStudio.validation.promptTooLong'),
  mode: z.enum(VIDEO_GENERATION_MODES),
  parameters: z.record(z.string(), videoParameterValueSchema),
  reference_asset_ids: z.array(z.number().int().positive()),
  video_asset_id: z
    .number()
    .int()
    .positive('videoStudio.validation.sampleVideoRequired'),
  category: z.enum(VIDEO_SAMPLE_CATEGORIES),
  status: z.enum(['draft', 'published']),
  sort_order: z
    .number()
    .int()
    .min(VIDEO_SAMPLE_SORT_ORDER_MIN, 'videoStudio.validation.sortOutOfRange')
    .max(VIDEO_SAMPLE_SORT_ORDER_MAX, 'videoStudio.validation.sortOutOfRange'),
})

export type VideoSampleFormValues = z.infer<typeof videoSampleFormSchema>

export type VideoModelParameterFormValues =
  VideoModelProfileFormValues['parameters'][number]

const videoParameterValueType = (
  value: VideoParameterValue
): (typeof VIDEO_PARAMETER_VALUE_TYPES)[number] => {
  if (typeof value === 'number') return 'number'
  if (typeof value === 'boolean') return 'boolean'
  return 'string'
}

const modelParameterFormValues = (
  parameter: VideoParameterControl,
  modes: VideoGenerationMode[],
  defaults: VideoParameters
): VideoModelParameterFormValues => {
  const defaultValue =
    defaults[parameter.key] !== undefined
      ? defaults[parameter.key]
      : parameter.default
  const hasProfileDefault = defaults[parameter.key] !== undefined
  return {
    key: parameter.key,
    label: parameter.label,
    request_key: parameter.request_key ?? '',
    modes: parameter.modes?.length ? parameter.modes : [...modes],
    modes_explicit: parameter.modes !== undefined,
    control: parameter.control,
    required: parameter.required ?? false,
    has_default: defaultValue !== undefined,
    default_source: hasProfileDefault ? 'profile' : 'inline',
    default_value: defaultValue,
    preserved_inline_default: hasProfileDefault ? parameter.default : undefined,
    options:
      parameter.control === 'segmented' || parameter.control === 'select'
        ? parameter.options.map((option) => ({
            label: option.label,
            value: option.value,
            value_type: videoParameterValueType(option.value),
          }))
        : [],
    min:
      parameter.control === 'slider' || parameter.control === 'number'
        ? parameter.min
        : 0,
    max:
      parameter.control === 'slider' || parameter.control === 'number'
        ? parameter.max
        : 10,
    step:
      parameter.control === 'slider' || parameter.control === 'number'
        ? parameter.step
        : 1,
  }
}

const referenceInput = (
  inputs: VideoReferenceInput[] | undefined,
  roles: VideoReferenceInput['role'][]
): VideoReferenceInput | undefined =>
  inputs?.find((input) => roles.includes(input.role))

export const createVideoModelProfileFormValues = (
  profile?: VideoModelProfile,
  candidate = ''
): VideoModelProfileFormValues => {
  const preset = getVideoModelPreset(profile?.model ?? candidate)
  const specification = preset?.specification ?? profile?.specification
  const configuredDefaults: VideoParameters = {}
  if (preset && profile) {
    for (const parameter of profile.specification.parameters) {
      if (parameter.default !== undefined) {
        configuredDefaults[parameter.key] = parameter.default
      }
    }
    Object.assign(configuredDefaults, profile.default_parameters)
  }
  const defaults = preset
    ? getVideoModelPresetDefaults(preset, configuredDefaults)
    : (profile?.default_parameters ?? {})
  const modes = specification?.modes ?? ['text_to_video']
  const imageReference = referenceInput(specification?.reference_inputs, [
    'reference',
    'reference_video',
  ])
  const firstFrame = referenceInput(specification?.reference_inputs, [
    'first_frame',
  ])
  const lastFrame = referenceInput(specification?.reference_inputs, [
    'last_frame',
  ])
  const modelName = profile?.model ?? preset?.model ?? candidate
  return {
    model: modelName,
    display_name: profile?.display_name ?? preset?.display_name ?? modelName,
    description: profile?.description ?? preset?.description ?? '',
    provider_label: profile?.provider_label ?? preset?.provider_label ?? '',
    enabled: profile?.enabled ?? false,
    sort_order: profile?.sort_order ?? preset?.sort_order ?? 0,
    specification_version:
      profile?.specification_version ?? preset?.specification.version ?? 1,
    modes: [...modes],
    parameters:
      specification?.parameters.map((parameter) =>
        modelParameterFormValues(parameter, modes, defaults)
      ) ?? [],
    image_reference_role:
      imageReference?.role === 'reference_video'
        ? 'reference_video'
        : 'reference',
    image_reference_request_key:
      imageReference?.request_key ?? 'reference_image',
    first_frame_request_key: firstFrame?.request_key ?? 'first_frame',
    last_frame_request_key: lastFrame?.request_key ?? 'last_frame',
  }
}

export const filterVideoModelCandidates = (
  candidates: string[],
  profiles: VideoModelProfile[]
): string[] => {
  const configured = new Set(profiles.map((profile) => profile.model))
  return [...new Set(candidates)]
    .filter(
      (candidate) =>
        getVideoModelPreset(candidate) !== undefined &&
        !configured.has(candidate)
    )
    .sort((left, right) => left.localeCompare(right))
}

export const canToggleVideoModelEnabled = (
  profile?: Pick<VideoModelProfile, 'enabled' | 'has_published_sample'> | null
): boolean =>
  Boolean(profile && (profile.enabled || profile.has_published_sample))

const parameterControlFromForm = (
  parameter: VideoModelParameterFormValues
): VideoParameterControl => {
  let inlineDefault: VideoParameterValue | undefined
  if (parameter.has_default) {
    inlineDefault =
      parameter.default_source === 'inline'
        ? parameter.default_value
        : parameter.preserved_inline_default
  }
  const common = {
    key: parameter.key.trim(),
    label: parameter.label.trim(),
    request_key: parameter.request_key.trim() || undefined,
    modes: parameter.modes_explicit ? parameter.modes : undefined,
    required: parameter.required || undefined,
  }
  if (parameter.control === 'segmented' || parameter.control === 'select') {
    return {
      ...common,
      control: parameter.control,
      options: parameter.options.map(
        (option): VideoParameterOption => ({
          label: option.label.trim(),
          value: option.value,
        })
      ),
      default: inlineDefault,
    }
  }
  if (parameter.control === 'slider' || parameter.control === 'number') {
    return {
      ...common,
      control: parameter.control,
      min: parameter.min,
      max: parameter.max,
      step: parameter.step,
      default: typeof inlineDefault === 'number' ? inlineDefault : undefined,
    }
  }
  return {
    ...common,
    control: 'switch',
    default: typeof inlineDefault === 'boolean' ? inlineDefault : undefined,
  }
}

const referenceInputsFromForm = (
  values: VideoModelProfileFormValues
): VideoReferenceInput[] => {
  const inputs: VideoReferenceInput[] = []
  if (values.modes.includes('image_to_video')) {
    inputs.push({
      role: values.image_reference_role,
      request_key: values.image_reference_request_key.trim(),
      required: true,
    })
  }
  if (values.modes.includes('first_last_frame')) {
    inputs.push(
      {
        role: 'first_frame',
        request_key: values.first_frame_request_key.trim(),
        required: true,
      },
      {
        role: 'last_frame',
        request_key: values.last_frame_request_key.trim(),
        required: true,
      }
    )
  }
  return inputs
}

const specificationSnapshot = (
  modes: VideoGenerationMode[],
  parameters: VideoParameterControl[],
  referenceInputs: VideoReferenceInput[]
): string =>
  JSON.stringify({
    modes,
    parameters: parameters.map((parameter) => ({
      key: parameter.key,
      label: parameter.label,
      control: parameter.control,
      request_key: parameter.request_key,
      modes: parameter.modes,
      required: parameter.required,
      default: parameter.default,
      options:
        parameter.control === 'segmented' || parameter.control === 'select'
          ? parameter.options
          : undefined,
      min:
        parameter.control === 'slider' || parameter.control === 'number'
          ? parameter.min
          : undefined,
      max:
        parameter.control === 'slider' || parameter.control === 'number'
          ? parameter.max
          : undefined,
      step:
        parameter.control === 'slider' || parameter.control === 'number'
          ? parameter.step
          : undefined,
    })),
    reference_inputs:
      referenceInputs.length > 0
        ? referenceInputs.map((input) => ({
            role: input.role,
            request_key: input.request_key,
            required: input.required,
          }))
        : undefined,
  })

export const parseVideoModelProfileForm = (
  values: VideoModelProfileFormValues,
  current?: VideoModelProfile
): VideoModelProfileInput => {
  const parameters = values.parameters.map(parameterControlFromForm)
  const defaultParameters: VideoParameters = {}
  values.parameters.forEach((parameter) => {
    if (
      parameter.has_default &&
      parameter.default_source === 'profile' &&
      parameter.default_value !== undefined
    ) {
      defaultParameters[parameter.key.trim()] = parameter.default_value
    }
  })
  const referenceInputs = referenceInputsFromForm(values)
  const nextSnapshot = specificationSnapshot(
    values.modes,
    parameters,
    referenceInputs
  )
  const currentSnapshot = current
    ? specificationSnapshot(
        current.specification.modes,
        current.specification.parameters,
        current.specification.reference_inputs ?? []
      )
    : ''
  const version = current
    ? current.specification_version + (nextSnapshot === currentSnapshot ? 0 : 1)
    : 1

  return {
    model: values.model,
    display_name: values.display_name,
    description: values.description ?? '',
    provider_label: values.provider_label ?? '',
    enabled: current ? values.enabled : false,
    sort_order: values.sort_order,
    specification: videoModelSpecSchema.parse({
      version,
      modes: values.modes,
      parameters,
      reference_inputs:
        referenceInputs.length > 0 ? referenceInputs : undefined,
    }),
    default_parameters: defaultParameters,
  }
}

export type VideoSampleProfileState = Pick<
  VideoSampleFormValues,
  'model_profile_id' | 'mode' | 'parameters' | 'reference_asset_ids'
>

export const buildVideoSampleProfileState = (
  profile: VideoModelProfile,
  preferredMode?: VideoGenerationMode,
  currentParameters: VideoParameters = {},
  referenceAssetIds: number[] = []
): VideoSampleProfileState => {
  const mode =
    preferredMode && profile.specification.modes.includes(preferredMode)
      ? preferredMode
      : profile.specification.modes[0]
  const normalizedMode = mode ?? 'text_to_video'
  return {
    model_profile_id: profile.id,
    mode: normalizedMode,
    parameters: getVideoParametersForMode(
      profile,
      normalizedMode,
      currentParameters
    ),
    reference_asset_ids: referenceAssetIds.slice(
      0,
      getVideoReferenceRoles(profile, normalizedMode).length
    ),
  }
}

export const createVideoSampleFormValues = (
  sample?: VideoSample,
  profile?: VideoModelProfile
): VideoSampleFormValues => {
  const profileState = profile
    ? buildVideoSampleProfileState(
        profile,
        sample?.mode,
        sample?.parameters,
        sample?.reference_asset_ids
      )
    : {
        model_profile_id: sample?.model_profile_id ?? 0,
        mode: sample?.mode ?? ('text_to_video' as const),
        parameters: sample?.parameters ?? {},
        reference_asset_ids: sample?.reference_asset_ids ?? [],
      }
  return {
    ...profileState,
    title: sample?.title ?? '',
    prompt: sample?.prompt ?? '',
    video_asset_id: sample?.video_asset_id ?? 0,
    category: sample?.category ?? 'other',
    status: sample?.status ?? 'draft',
    sort_order: sample?.sort_order ?? 0,
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
    parameters: values.parameters,
    reference_asset_ids: values.reference_asset_ids,
    video_asset_id: values.video_asset_id,
    aspect_ratio: 0,
    category: values.category,
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

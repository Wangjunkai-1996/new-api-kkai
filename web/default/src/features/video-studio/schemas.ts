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
  VideoModelSpec,
  VideoParameterControl,
  VideoParameterOption,
  VideoParameterValue,
  VideoParameters,
  VideoReferencePolicy,
  VideoReferenceMediaPolicy,
  VideoSample,
  VideoSampleInput,
  VideoTokenCapability,
  VideoTokenCreateResult,
} from './types'
import {
  getVideoParametersForMode,
  videoParameterAcceptsValue,
  trimVideoInput,
  videoPromptByteLength,
} from './video-domain'
import { VIDEO_SAMPLE_CATEGORIES } from './video-sample-categories'

export const VIDEO_GENERATION_MODES = [
  'text_to_video',
  'reference_to_video',
  'first_frame_to_video',
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
  referencePolicy?: VideoReferencePolicy
  durationRequestKey?: 'duration' | 'seconds'
  maxDuration?: number
  ratios?: readonly string[]
  resolutions?: readonly string[]
  supportsGenerateAudio?: boolean
  generateAudioRequired?: boolean
}

const VIDEO_MODEL_PRESET_DETAILS = {
  'sd_2.0_fast_special_720p': {
    displayName: 'Seedance 2.0 Fast 720p',
    description: '快速 720p 文生 / 多模态参考视频',
    resolution: '720p',
    requiresVideoReference: false,
  },
  'sd_2.0_special_720p': {
    displayName: 'Seedance 2.0 720p',
    description: '720p 文生 / 多模态参考视频',
    resolution: '720p',
    requiresVideoReference: false,
  },
  'sd_2.0_special_1080p': {
    displayName: 'Seedance 2.0 1080p',
    description: '1080p 文生 / 多模态参考视频',
    resolution: '1080p',
    requiresVideoReference: false,
  },
  'sd_2.0_special_2k': {
    displayName: 'Seedance 2.0 2K',
    description: '2K 文生 / 多模态参考视频',
    resolution: '2K',
    requiresVideoReference: false,
  },
  'sd_2.0_special_4k': {
    displayName: 'Seedance 2.0 4K',
    description: '4K 文生 / 多模态参考视频',
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

const SEEDANCE_25_MODEL_PRESET_DETAILS = {
  'seedance-2.5': {
    displayName: 'Seedance 2.5 720p',
    description: '720p 文生 / 多模态参考视频',
    resolution: '720p',
    requiresVideoReference: false,
  },
} as const satisfies Record<string, VideoModelPresetDetails>

const getSeedance25PresetDetails = (
  model: string
): VideoModelPresetDetails | undefined => {
  const details =
    SEEDANCE_25_MODEL_PRESET_DETAILS[
      model as keyof typeof SEEDANCE_25_MODEL_PRESET_DETAILS
    ]
  if (!details) return undefined
  return {
    ...details,
    durationRequestKey: 'duration',
    maxDuration: 30,
    ratios: SEEDANCE_SPECIAL_RATIOS,
    resolutions: [details.resolution],
    supportsGenerateAudio: true,
    generateAudioRequired: false,
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
    ? ['reference_to_video']
    : ['text_to_video', 'reference_to_video']
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
      required: details.generateAudioRequired ?? true,
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
      reference_policy: details.referencePolicy ?? {
        // Kept for compatibility; reference limits apply to each media kind.
        max_total_count: null,
        requires_visual: true,
        requires_video: details.requiresVideoReference,
        audio_requires_generate_audio: true,
        image: {
          allowed_mime_types: ['image/jpeg', 'image/png', 'image/webp'],
          max_count: 9,
          max_size_bytes: 20 * 1024 * 1024,
          min_width: 301,
          max_width: 5999,
          min_height: 301,
          max_height: 5999,
          min_aspect_ratio_exclusive: 0.4,
          max_aspect_ratio_exclusive: 2.5,
        },
        video: details.requiresVideoReference
          ? {
              allowed_mime_types: ['video/mp4', 'video/quicktime'],
              max_count: 3,
              max_size_bytes: 49_999_999,
              min_duration_seconds: 2,
              max_duration_seconds: 15,
              max_total_duration_seconds: 15,
              min_frame_rate: 24,
              max_frame_rate: 60,
            }
          : undefined,
        audio: {
          allowed_mime_types: ['audio/mpeg', 'audio/wav'],
          max_count: 3,
          max_size_bytes: 14_999_999,
          min_duration_seconds: 2,
          max_duration_seconds: 15,
          max_total_duration_seconds: 15,
        },
      },
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
    .transform(trimVideoInput)
    .refine(
      (value) => value.length > 0,
      'videoStudio.validation.promptRequired'
    )
    .refine(
      (value) => videoPromptByteLength(value) <= VIDEO_PROMPT_MAX_LENGTH,
      'videoStudio.validation.promptTooLong'
    ),
  references: z.array(
    z.object({
      client_id: z.string().min(1),
      asset_id: z.number().int().positive(),
      role: z.enum([
        'reference_image',
        'reference_video',
        'reference_audio',
        'first_frame',
        'last_frame',
      ]),
      position: z.number().int().nonnegative(),
      included: z.boolean(),
    })
  ),
  reference_asset_ids: z.array(z.number().int().positive()).optional(),
  parameters: z.record(z.string(), videoParameterValueSchema),
})

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
          'reference_image',
          'reference_video',
          'reference_audio',
          'first_frame',
          'last_frame',
        ]),
        request_key: z.string().min(1).optional(),
        required: z.boolean(),
      })
    )
    .optional(),
  reference_policy: z
    .object({
      max_total_count: z.number().int().nullable(),
      requires_visual: z.boolean(),
      requires_video: z.boolean(),
      audio_requires_generate_audio: z.boolean(),
      image: z
        .object({
          allowed_mime_types: z.array(z.string().min(1)).min(1),
          max_count: z.number().int().positive(),
          max_size_bytes: z.number().int().positive(),
          min_width: z.number().int().positive().optional(),
          max_width: z.number().int().positive().optional(),
          min_height: z.number().int().positive().optional(),
          max_height: z.number().int().positive().optional(),
          min_aspect_ratio_exclusive: z.number().positive().optional(),
          max_aspect_ratio_exclusive: z.number().positive().optional(),
        })
        .optional(),
      video: z
        .object({
          allowed_mime_types: z.array(z.string().min(1)).min(1),
          max_count: z.number().int().positive(),
          max_size_bytes: z.number().int().positive(),
          min_duration_seconds: z.number().positive().optional(),
          max_duration_seconds: z.number().positive().optional(),
          max_total_duration_seconds: z.number().positive().optional(),
          min_frame_rate: z.number().positive().optional(),
          max_frame_rate: z.number().positive().optional(),
        })
        .optional(),
      audio: z
        .object({
          allowed_mime_types: z.array(z.string().min(1)).min(1),
          max_count: z.number().int().positive(),
          max_size_bytes: z.number().int().positive(),
          min_duration_seconds: z.number().positive().optional(),
          max_duration_seconds: z.number().positive().optional(),
          max_total_duration_seconds: z.number().positive().optional(),
        })
        .optional(),
    })
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
    reference_policy: videoModelSpecSchema.shape.reference_policy,
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

    if (modes.has('reference_to_video') && !values.reference_policy) {
      context.addIssue({
        code: 'custom',
        path: ['reference_policy'],
        message: 'videoStudio.validation.referencePolicyRequired',
      })
    }
    const presetPolicy = getVideoModelPreset(values.model)?.specification
      .reference_policy
    if (presetPolicy && values.reference_policy) {
      for (const key of [
        'requires_visual',
        'requires_video',
        'audio_requires_generate_audio',
      ] as const) {
        if (presetPolicy[key] && !values.reference_policy[key]) {
          context.addIssue({
            code: 'custom',
            path: ['reference_policy', key],
            message: 'videoStudio.validation.parameterOutOfRange',
          })
        }
      }
    }
    for (const kind of ['image', 'video', 'audio'] as const) {
      const media: VideoReferenceMediaPolicy | undefined =
        values.reference_policy?.[kind]
      if (!media) continue
      const accepted = presetPolicy?.[kind]
      if (
        media.allowed_mime_types.some(
          (mime) =>
            !mime.startsWith(`${kind}/`) ||
            (accepted && !accepted.allowed_mime_types.includes(mime))
        )
      ) {
        context.addIssue({
          code: 'custom',
          path: ['reference_policy', kind, 'allowed_mime_types'],
          message: 'videoStudio.validation.referenceMimeUnsupported',
        })
      }
      if (accepted) {
        for (const [key, value] of Object.entries(media)) {
          const bound = accepted[key as keyof VideoReferenceMediaPolicy]
          if (typeof value !== 'number' || typeof bound !== 'number') continue
          if (
            (key.startsWith('max_') && value > bound) ||
            (key.startsWith('min_') && value < bound)
          ) {
            context.addIssue({
              code: 'custom',
              path: ['reference_policy', kind, key],
              message: 'videoStudio.validation.parameterOutOfRange',
            })
          }
        }
      } else if (presetPolicy) {
        context.addIssue({
          code: 'custom',
          path: ['reference_policy', kind],
          message: 'videoStudio.validation.referenceMimeUnsupported',
        })
      }
      const ranges: [
        keyof VideoReferenceMediaPolicy,
        keyof VideoReferenceMediaPolicy,
      ][] =
        kind === 'image'
          ? [
              ['min_width', 'max_width'],
              ['min_height', 'max_height'],
              ['min_aspect_ratio_exclusive', 'max_aspect_ratio_exclusive'],
            ]
          : [
              ['min_duration_seconds', 'max_duration_seconds'],
              ['max_duration_seconds', 'max_total_duration_seconds'],
            ]
      if (kind === 'video') ranges.push(['min_frame_rate', 'max_frame_rate'])
      for (const [minimum, maximum] of ranges) {
        const min = media[minimum]
        const max = media[maximum]
        if (
          typeof min !== 'number' ||
          typeof max !== 'number' ||
          max < min ||
          (minimum === 'min_aspect_ratio_exclusive' && max === min)
        ) {
          context.addIssue({
            code: 'custom',
            path: ['reference_policy', kind, maximum],
            message: 'videoStudio.validation.parameterOutOfRange',
          })
        }
      }
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
    .transform(trimVideoInput)
    .refine(
      (value) => value.length > 0,
      'videoStudio.validation.promptRequired'
    )
    .refine(
      (value) => videoPromptByteLength(value) <= VIDEO_PROMPT_MAX_LENGTH,
      'videoStudio.validation.promptTooLong'
    ),
  mode: z.enum(VIDEO_GENERATION_MODES),
  parameters: z.record(z.string(), videoParameterValueSchema),
  reference_assets: z.array(
    z.object({
      asset_id: z.number().int().positive(),
      role: z.enum([
        'reference_image',
        'reference_video',
        'reference_audio',
        'first_frame',
        'last_frame',
      ]),
      position: z.number().int().nonnegative(),
    })
  ),
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
    modes: parameter.modes?.length
      ? (parameter.modes as VideoModelParameterFormValues['modes'])
      : ([...modes] as VideoModelProfileFormValues['modes']),
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

export const createVideoModelProfileFormValues = (
  profile?: VideoModelProfile,
  candidate = ''
): VideoModelProfileFormValues => {
  const preset = getVideoModelPreset(profile?.model ?? candidate)
  const specification = profile?.specification ?? preset?.specification
  const configuredDefaults: VideoParameters = {}
  if (preset && profile) {
    for (const parameter of profile.specification.parameters) {
      if (parameter.default !== undefined) {
        configuredDefaults[parameter.key] = parameter.default
      }
    }
    Object.assign(configuredDefaults, profile.default_parameters)
  }
  const defaults =
    profile?.default_parameters ??
    (preset ? getVideoModelPresetDefaults(preset, configuredDefaults) : {})
  const modes = (specification?.modes ?? [
    'text_to_video',
  ]) as VideoModelProfileFormValues['modes']
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
    reference_policy: specification?.reference_policy
      ? {
          ...specification.reference_policy,
          max_total_count:
            specification.reference_policy.max_total_count ?? null,
        }
      : undefined,
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

export const applyVideoModelReferencePreset = (
  values: VideoModelProfileFormValues
): VideoModelProfileFormValues => {
  const preset = createVideoModelProfileFormValues(undefined, values.model)
  if (!preset.reference_policy || values.reference_policy) return values
  const parameters = values.parameters.map((parameter) => ({
    ...parameter,
    modes: [
      ...new Set(
        parameter.modes.map((mode) =>
          String(mode) === 'image_to_video'
            ? ('reference_to_video' as const)
            : mode
        )
      ),
    ].filter((mode) => preset.modes.includes(mode)),
  }))
  const existingKeys = new Set(parameters.map((parameter) => parameter.key))
  return {
    ...values,
    modes: preset.modes,
    reference_policy: preset.reference_policy,
    parameters: [
      ...parameters,
      ...preset.parameters.filter(
        (parameter) => !existingKeys.has(parameter.key)
      ),
    ],
  }
}

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
      options: parameter.options.map((option): VideoParameterOption => ({
        label: option.label.trim(),
        value: option.value,
      })),
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

const specificationSnapshot = (
  modes: VideoGenerationMode[],
  parameters: VideoParameterControl[],
  referencePolicy?: VideoReferencePolicy
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
    reference_policy: referencePolicy,
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
  const nextSnapshot = specificationSnapshot(
    values.modes,
    parameters,
    values.reference_policy
      ? {
          ...values.reference_policy,
          max_total_count: values.reference_policy.max_total_count ?? null,
        }
      : undefined
  )
  const currentSnapshot = current
    ? specificationSnapshot(
        current.specification.modes,
        current.specification.parameters,
        current.specification.reference_policy
          ? {
              ...current.specification.reference_policy,
              max_total_count:
                current.specification.reference_policy.max_total_count ?? null,
            }
          : undefined
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
    enabled: values.enabled,
    sort_order: values.sort_order,
    specification: videoModelSpecSchema.parse({
      version,
      modes: values.modes as VideoModelSpec['modes'],
      parameters,
      reference_policy: values.reference_policy
        ? {
            ...values.reference_policy,
            max_total_count: values.reference_policy.max_total_count ?? null,
          }
        : undefined,
    }),
    default_parameters: defaultParameters,
  }
}

export type VideoSampleProfileState = Pick<
  VideoSampleFormValues,
  'model_profile_id' | 'mode' | 'parameters' | 'reference_assets'
>

export const buildVideoSampleProfileState = (
  profile: VideoModelProfile,
  preferredMode?: VideoGenerationMode,
  currentParameters: VideoParameters = {},
  referenceAssets: VideoSampleFormValues['reference_assets'] = []
): VideoSampleProfileState => {
  const mode =
    preferredMode && profile.specification.modes.includes(preferredMode)
      ? preferredMode
      : profile.specification.modes[0]
  const normalizedMode = (mode ??
    'text_to_video') as VideoSampleFormValues['mode']
  return {
    model_profile_id: profile.id,
    mode: normalizedMode,
    parameters: getVideoParametersForMode(
      profile,
      normalizedMode,
      currentParameters
    ),
    reference_assets: referenceAssets,
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
        sample?.reference_assets?.filter(
          (reference) => reference.role !== 'reference'
        ) as VideoSampleFormValues['reference_assets'] | undefined
      )
    : {
        model_profile_id: sample?.model_profile_id ?? 0,
        mode: (sample?.mode ??
          'text_to_video') as VideoSampleFormValues['mode'],
        parameters: sample?.parameters ?? {},
        reference_assets: (sample?.reference_assets?.filter(
          (reference) => reference.role !== 'reference'
        ) ?? []) as VideoSampleFormValues['reference_assets'],
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
    reference_assets: values.reference_assets,
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

  if (videoPromptByteLength(values.prompt) > VIDEO_PROMPT_MAX_LENGTH) {
    return 'videoStudio.validation.promptTooLong'
  }

  const included = (values.references ?? []).filter(
    (reference) => reference.included
  )
  const policy = profile.specification.reference_policy
  if (values.mode !== 'text_to_video') {
    if (!policy) {
      return 'videoStudio.validation.referencePolicyRequired'
    }
    const firstFrameCount = included.filter(
      (reference) => reference.role === 'first_frame'
    ).length
    const lastFrameCount = included.filter(
      (reference) => reference.role === 'last_frame'
    ).length
    if (firstFrameCount > 1 || lastFrameCount > 1) {
      return 'videoStudio.validation.referenceLimitExceeded'
    }
    const imageCount =
      included.filter((reference) => reference.role === 'reference_image')
        .length +
      firstFrameCount +
      lastFrameCount
    const videoCount = included.filter(
      (reference) => reference.role === 'reference_video'
    ).length
    const audioCount = included.filter(
      (reference) => reference.role === 'reference_audio'
    ).length
    if (policy.requires_visual && imageCount + videoCount === 0) {
      return 'videoStudio.validation.visualReferenceRequired'
    }
    if (policy.requires_video && videoCount === 0) {
      return 'videoStudio.validation.videoReferenceRequired'
    }
    if (
      audioCount > 0 &&
      policy.audio_requires_generate_audio &&
      values.parameters.generate_audio !== true
    ) {
      return 'videoStudio.validation.audioOutputRequired'
    }
    for (const [kind, count] of [
      ['image', imageCount],
      ['video', videoCount],
      ['audio', audioCount] as const,
    ]) {
      const mediaKind = kind as 'image' | 'video' | 'audio'
      const limit = policy[mediaKind]?.max_count ?? 0
      if (Number(count) > limit) {
        return 'videoStudio.validation.referenceLimitExceeded'
      }
    }
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

export const videoTaskIdSchema = z
  .string()
  .trim()
  .max(128)
  .regex(/^task_[0-9A-Za-z_-]+$/)

export const videoQuoteSchema = z.object({
  quota: z.number().int().nonnegative().safe(),
  request_hash: z.string().regex(/^[0-9a-fA-F]{64}$/),
  expires_at: z.number().int().positive().safe(),
  amount: z.number().finite().nonnegative().optional(),
  display_amount: z.string().optional(),
  other_ratios: z.record(z.string(), z.number().finite()).optional(),
})

export const videoSubmissionReceiptSchema = z
  .object({
    id: videoTaskIdSchema.optional(),
    task_id: videoTaskIdSchema.optional(),
    object: z.string().optional(),
    model: z.string().optional(),
    status: z.enum(['queued', 'in_progress', 'completed', 'failed']).optional(),
    progress: z.number().finite().optional(),
    created_at: z.number().finite().optional(),
  })
  .passthrough()
  .superRefine((value, context) => {
    if (!value.id && !value.task_id) {
      context.addIssue({
        code: 'custom',
        message: 'Missing video task identity',
      })
    }
    if (value.id && value.task_id && value.id !== value.task_id) {
      context.addIssue({
        code: 'custom',
        message: 'Conflicting video task identities',
      })
    }
    if (value.error != null || value.code != null || value.success === false) {
      context.addIssue({
        code: 'custom',
        message: 'Uncertain video submission response',
      })
    }
  })

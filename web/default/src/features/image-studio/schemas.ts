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

const imageMediaUrlSchema = z.string().refine((value) => {
  if (value.startsWith('/') && !value.startsWith('//')) return true
  try {
    const url = new URL(value)
    return url.protocol === 'https:' || url.protocol === 'http:'
  } catch {
    return false
  }
}, 'imageStudio.validation.mediaUrlInvalid')

export const imageParameterValueSchema = z.union([
  z.string(),
  z.number().finite(),
  z.boolean(),
])

export const imageParametersSchema = z.record(
  z.string(),
  imageParameterValueSchema
)

const imageParameterBaseSchema = z.object({
  key: z.string().min(1).max(64),
  label: z.string().min(1).max(128),
  request_key: z.string().min(1).max(64),
  required: z.boolean().optional(),
})

export const imageParameterSchema = z.discriminatedUnion('control', [
  imageParameterBaseSchema.extend({
    control: z.literal('select'),
    options: z
      .array(z.object({ label: z.string().min(1), value: z.string().min(1) }))
      .min(1),
  }),
  imageParameterBaseSchema.extend({
    control: z.literal('integer'),
    min: z.number().int(),
    max: z.number().int(),
  }),
  imageParameterBaseSchema.extend({ control: z.literal('boolean') }),
])

export const imageModelProfileSchema = z.object({
  id: z.number().int().positive(),
  model: z.string().min(1),
  display_name: z.string().min(1),
  description: z.string(),
  provider_label: z.string(),
  specification_version: z.number().int().positive(),
  specification: z.object({
    version: z.number().int().positive(),
    parameters: z.array(imageParameterSchema),
  }),
  default_parameters: imageParametersSchema,
  has_published_sample: z.boolean().optional(),
  enabled: z.boolean(),
  sort_order: z.number().int(),
  created_at: z.number().int(),
  updated_at: z.number().int(),
})

export const imageAssetSchema = z.object({
  id: z.number().int().positive(),
  position: z.number().int().default(0),
  state: z.enum(['staging', 'ready', 'failed', 'deleted']),
  thumbnail_state: z.enum(['pending', 'ready', 'failed']),
  mime_type: z.string(),
  size_bytes: z.number().int().nonnegative(),
  width: z.number().int().nonnegative(),
  height: z.number().int().nonnegative(),
  failure_reason: z.string().optional(),
  content_url: imageMediaUrlSchema.optional(),
  thumbnail_url: imageMediaUrlSchema.optional(),
  download_url: imageMediaUrlSchema.optional(),
})

export const imageSampleSchema = z.object({
  id: z.number().int().positive(),
  model_profile_id: z.number().int().positive(),
  image_asset_id: z.number().int().positive(),
  model: z.string().min(1),
  title: z.string().min(1),
  prompt: z.string().min(1),
  model_version: z.number().int().positive(),
  parameters: imageParametersSchema,
  category: z.string().min(1),
  status: z.enum(['draft', 'published']),
  sort_order: z.number().int(),
  asset: imageAssetSchema,
  created_at: z.number().int(),
  updated_at: z.number().int(),
})

export const imageGenerationSchema = z.object({
  id: z.number().int().positive(),
  model_profile_id: z.number().int().positive(),
  sample_id: z.number().int().positive().optional(),
  specification_version: z.number().int().positive(),
  model: z.string().min(1),
  prompt: z.string(),
  parameters: imageParametersSchema,
  request_id: z.string(),
  status: z.enum([
    'submitting',
    'succeeded',
    'partial',
    'failed',
    'archive_failed',
    'unknown',
  ]),
  requested_count: z.number().int().nonnegative(),
  succeeded_count: z.number().int().nonnegative(),
  final_quota: z.number().int().nonnegative(),
  failure_stage: z.string().optional(),
  error_code: z.string().optional(),
  error_message: z.string().optional(),
  started_at: z.number().int(),
  finished_at: z.number().int(),
  created_at: z.number().int(),
  assets: z.array(imageAssetSchema),
})

const imageTokenCapabilityFields = {
  required_group: z.string().min(1),
  has_usable_token: z.boolean(),
  can_create: z.boolean(),
  effective_models: z.array(z.string()),
  status: z.enum([
    'ready',
    'missing',
    'group_unavailable',
    'limit_reached',
    'models_unavailable',
  ]),
  token: z
    .object({
      id: z.number().int().positive(),
      name: z.string(),
      group: z.string(),
    })
    .optional(),
}

const validateImageTokenCapability = (
  value: z.infer<z.ZodObject<typeof imageTokenCapabilityFields>>,
  context: z.RefinementCtx
): void => {
  if (
    value.status === 'ready' &&
    (!value.has_usable_token ||
      !value.token ||
      value.token.group !== value.required_group)
  ) {
    context.addIssue({
      code: 'custom',
      path: ['token'],
      message: 'imageStudio.validation.tokenInvalid',
    })
  }
}

export const imageTokenCapabilitySchema = z
  .object(imageTokenCapabilityFields)
  .superRefine(validateImageTokenCapability)

export const imageTokenCreateResultSchema = z
  .object({ ...imageTokenCapabilityFields, created: z.boolean() })
  .superRefine(validateImageTokenCapability)

export const imageQuoteSchema = z.object({
  quota: z.number().int().nonnegative(),
  display_amount: z.string(),
  request_hash: z.string().length(64),
  expires_at: z.number().int().positive(),
  other_ratios: z.record(z.string(), z.number()).optional(),
})

export const imageComposerSchema = z.object({
  model_profile_id: z
    .number()
    .int()
    .positive('imageStudio.validation.modelRequired'),
  prompt: z
    .string()
    .trim()
    .min(1, 'imageStudio.validation.promptRequired')
    .max(8000, 'imageStudio.validation.promptTooLong'),
  parameters: imageParametersSchema,
  sample_id: z.number().int().positive().optional(),
})

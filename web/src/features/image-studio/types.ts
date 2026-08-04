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
export type ImageStudioAccessMode = 'off' | 'admin' | 'all'
export type ImageParameterValue = string | number | boolean
export type ImageParameters = Record<string, ImageParameterValue>

export type ImageParameterOption = {
  label: string
  value: string
}

type ImageParameterBase = {
  key: string
  label: string
  request_key: string
  required?: boolean
}

export type ImageSelectParameter = ImageParameterBase & {
  control: 'select'
  options: ImageParameterOption[]
}

export type ImageIntegerParameter = ImageParameterBase & {
  control: 'integer'
  min: number
  max: number
}

export type ImageBooleanParameter = ImageParameterBase & {
  control: 'boolean'
}

export type ImageParameterControl =
  | ImageSelectParameter
  | ImageIntegerParameter
  | ImageBooleanParameter

export type ImageModelSpec = {
  version: number
  parameters: ImageParameterControl[]
}

export type ImageModelProfile = {
  id: number
  model: string
  display_name: string
  description: string
  provider_label: string
  specification_version: number
  specification: ImageModelSpec
  default_parameters: ImageParameters
  has_published_sample?: boolean
  enabled: boolean
  sort_order: number
  created_at: number
  updated_at: number
}

export type ImageAsset = {
  id: number
  position: number
  state: 'staging' | 'ready' | 'failed' | 'deleted'
  thumbnail_state: 'pending' | 'ready' | 'failed'
  mime_type: string
  size_bytes: number
  width: number
  height: number
  failure_reason?: string
  content_url?: string
  thumbnail_url?: string
  download_url?: string
}

export type ImageSample = {
  id: number
  model_profile_id: number
  image_asset_id: number
  model: string
  title: string
  prompt: string
  model_version: number
  parameters: ImageParameters
  category: string
  status: 'draft' | 'published'
  sort_order: number
  asset: ImageAsset
  created_at: number
  updated_at: number
}

export type ImageGenerationStatus =
  | 'submitting'
  | 'succeeded'
  | 'partial'
  | 'failed'
  | 'archive_failed'
  | 'unknown'

export type ImageGeneration = {
  id: number
  model_profile_id: number
  sample_id?: number
  specification_version: number
  model: string
  prompt: string
  parameters: ImageParameters
  request_id: string
  status: ImageGenerationStatus
  requested_count: number
  succeeded_count: number
  final_quota: number
  failure_stage?: string
  error_code?: string
  error_message?: string
  started_at: number
  finished_at: number
  created_at: number
  assets: ImageAsset[]
}

export type CursorPage<T> = {
  items: T[]
  next_cursor?: string
}

export type ImageTokenStatus =
  | 'ready'
  | 'missing'
  | 'group_unavailable'
  | 'limit_reached'
  | 'models_unavailable'

export type ImageTokenCapability = {
  required_group: string
  has_usable_token: boolean
  can_create: boolean
  effective_models: string[]
  status: ImageTokenStatus
  token?: { id: number; name: string; group: string }
}

export type ImageTokenCreateResult = ImageTokenCapability & {
  created: boolean
}

export type ImageQuoteRequest = {
  token_id: number
  model: string
  prompt: string
  parameters: ImageParameters
  sample_id?: number
}

export type ImageQuote = {
  quota: number
  display_amount: string
  request_hash: string
  expires_at: number
  other_ratios?: Record<string, number>
}

export type CreateImageRequest = ImageQuoteRequest & {
  max_quota: number
  quote_hash: string
  quote_expires_at: number
}

export type ImageComposerValues = {
  model_profile_id: number
  prompt: string
  parameters: ImageParameters
  sample_id?: number
}

export type ImageModelProfileInput = {
  model: string
  display_name: string
  description: string
  provider_label: string
  specification: ImageModelSpec
  default_parameters: ImageParameters
  enabled: boolean
  sort_order: number
}

export type ImageSampleInput = {
  model_profile_id: number
  image_asset_id: number
  title: string
  prompt: string
  parameters: ImageParameters
  category: string
  status: 'draft' | 'published'
  sort_order: number
}

export type ImageStudioApiError = {
  code?: string
  message?: string
  data?: unknown
}

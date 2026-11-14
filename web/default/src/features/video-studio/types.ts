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
import type { VideoStudioUploadLimits } from '@/features/auth/types'

import type { VideoSampleCategory } from './video-sample-categories'

export type VideoGenerationMode =
  | 'text_to_video'
  | 'reference_to_video'
  | 'first_frame_to_video'
  | 'first_last_frame'
  /** @deprecated Only retained for rendering historical task records. */
  | 'image_to_video'

export type VideoStudioAccessMode = 'off' | 'admin' | 'all'

export type VideoParameterValue = string | number | boolean
export type VideoParameters = Record<string, VideoParameterValue>

export type VideoParameterOption = {
  label: string
  value: VideoParameterValue
}

type VideoParameterBase = {
  key: string
  label: string
  request_key?: string
  modes?: VideoGenerationMode[]
  required?: boolean
}

export type VideoChoiceParameter = VideoParameterBase & {
  control: 'segmented' | 'select'
  default?: VideoParameterValue
  options: VideoParameterOption[]
}

export type VideoNumericParameter = VideoParameterBase & {
  control: 'slider' | 'number'
  default?: number
  min: number
  max: number
  step: number
}

export type VideoSwitchParameter = VideoParameterBase & {
  control: 'switch'
  default?: boolean
}

export type VideoParameterControl =
  | VideoChoiceParameter
  | VideoNumericParameter
  | VideoSwitchParameter

export type VideoReferenceRole =
  | 'reference_image'
  | 'reference_video'
  | 'reference_audio'
  | 'first_frame'
  | 'last_frame'
  /** @deprecated Only retained for historical task records. */
  | 'reference'

export type VideoReferenceInput = {
  role: VideoReferenceRole
  request_key?: string
  required: boolean
}

export type VideoReferencePolicy = {
  max_total_count: number | null
  requires_visual: boolean
  requires_video: boolean
  audio_requires_generate_audio: boolean
  image?: VideoReferenceMediaPolicy
  video?: VideoReferenceMediaPolicy
  audio?: VideoReferenceMediaPolicy
}

export type VideoReferenceMediaPolicy = {
  allowed_mime_types: string[]
  max_count: number
  max_size_bytes: number
  min_duration_seconds?: number
  max_duration_seconds?: number
  max_total_duration_seconds?: number
  min_width?: number
  max_width?: number
  min_height?: number
  max_height?: number
  min_aspect_ratio_exclusive?: number
  max_aspect_ratio_exclusive?: number
  min_frame_rate?: number
  max_frame_rate?: number
}

export type VideoModelSpec = {
  version: number
  modes: VideoGenerationMode[]
  parameters: VideoParameterControl[]
  reference_inputs?: VideoReferenceInput[]
  reference_policy?: VideoReferencePolicy
}

export type VideoModelProfile = {
  id: number
  model: string
  display_name: string
  description: string
  provider_label: string
  specification_version: number
  specification: VideoModelSpec
  default_parameters: VideoParameters
  has_published_sample?: boolean
  enabled: boolean
  sort_order: number
  created_at: number
  updated_at: number
}

export type VideoAssetState =
  | 'pending_upload'
  | 'uploaded'
  | 'processing'
  | 'ready'
  | 'failed'
  | 'deleting'
  | 'deleted'

export type VideoAsset = {
  id: number
  scope: 'user' | 'catalog'
  kind: 'reference' | 'output' | 'sample'
  state: VideoAssetState
  original_filename: string
  mime_type: string
  size_bytes: number
  width: number
  height: number
  duration_seconds: number
  codec: string
  failure_reason?: string
  upload_mode?: VideoUploadMode
  upload_part_size?: number
  upload_expires_at?: number
  content_url?: string
  poster_url?: string
  preview_url?: string
  created_at: number
  updated_at: number
}

export type VideoSampleStatus = 'draft' | 'published'

export type VideoSample = {
  id: number
  model_profile_id: number
  model: string
  model_display_name: string
  title: string
  prompt: string
  mode: VideoGenerationMode
  model_version: number
  parameters: VideoParameters
  reference_assets?: VideoSampleReferenceAsset[]
  /** @deprecated Historical catalog fields; never used to construct new requests. */
  reference_asset_ids?: number[]
  /** @deprecated Historical catalog fields; never used to construct new requests. */
  reference_content_urls?: string[]
  video_asset_id: number
  video_url: string
  poster_url: string
  preview_url: string
  aspect_ratio: number
  category: VideoSampleCategory
  status: VideoSampleStatus
  sort_order: number
  created_at: number
  updated_at: number
}

export type VideoGenerationStatus =
  | 'queued'
  | 'processing'
  | 'archiving'
  | 'ready'
  | 'failed'

export type VideoGenerationFailureCode =
  | 'copyright_restriction'
  | 'privacy_restriction'
  | 'content_policy_violation'
  | 'input_invalid'
  | 'unsupported_media'
  | 'media_unavailable'
  | 'billing_failure'
  | 'quota_exceeded'
  | 'temporary_failure'
  | 'task_expired'
  | 'submission_failure'

export type VideoGeneration = {
  id: number
  task_id: string
  model_profile_id: number
  sample_id?: number
  model: string
  specification_version?: number
  mode: VideoGenerationMode
  prompt: string
  parameters: VideoParameters
  reference_assets?: VideoGenerationReferenceAsset[]
  status: VideoGenerationStatus
  progress: string
  failure_reason?: string
  failure_code?: VideoGenerationFailureCode
  quota: number
  output_asset_id?: number
  video_url?: string
  poster_url?: string
  download_url?: string
  created_at: number
  updated_at: number
}

export type CursorPage<T> = {
  items: T[]
  next_cursor?: string
}

export type VideoSampleFilters = {
  cursor?: string
  limit?: number
  model?: string
  category?: VideoSampleCategory
}

export type VideoGenerationFilters = {
  cursor?: string
  limit?: number
  status?: VideoGenerationStatus
}

export type VideoComposerValues = {
  model_profile_id: number
  mode: VideoGenerationMode
  prompt: string
  references?: VideoComposerReference[]
  /** @deprecated Kept only while loading historical local drafts. */
  reference_asset_ids?: number[]
  parameters: VideoParameters
}

export type VideoComposerReference = {
  client_id: string
  asset_id: number
  role: VideoReferenceRole
  position: number
  included: boolean
  asset?: VideoAsset | null
}

export type VideoSampleReferenceAsset = {
  asset_id: number
  role: VideoReferenceRole
  position: number
  asset?: VideoAsset | null
}

export type VideoGenerationReferenceAsset = VideoSampleReferenceAsset

export type VideoReferenceAssetInput = {
  asset_id: number
  role: VideoReferenceRole
  position: number
}

export type VideoQuoteRequest = {
  token_id: number
  model: string
  specification_version: number
  mode: VideoGenerationMode
  prompt: string
  parameters: VideoParameters
  reference_assets: VideoReferenceAssetInput[]
  sample_id?: number
}

export type VideoQuote = {
  quota: number
  amount?: number
  display_amount?: string
  request_hash: string
  expires_at: number
  other_ratios?: Record<string, number>
}

export type VideoTokenSummary = {
  id: number
  name: string
  group: string
}

export type VideoTokenCapabilityStatus =
  | 'ready'
  | 'missing'
  | 'group_unavailable'
  | 'limit_reached'
  | 'models_unavailable'

export type VideoTokenCapability = {
  required_group: string
  has_usable_token: boolean
  can_create: boolean
  effective_models?: string[]
  token?: VideoTokenSummary | null
  status: VideoTokenCapabilityStatus
}

export type VideoTokenCreateResult = VideoTokenCapability & {
  created: boolean
}

export type CreateVideoRequest = VideoQuoteRequest & {
  max_quota: number
  quote_hash: string
  quote_expires_at: number
}

export type VideoSubmissionReceipt = {
  id?: string
  task_id?: string
  object?: string
  model?: string
  status?: string
  progress?: number
  created_at?: number
  [key: string]: unknown
}

export type VideoUploadPurpose =
  | 'reference'
  | 'reference_video'
  | 'reference_audio'
  | 'sample'

export type VideoUploadMode = 'single' | 'multipart'

export type VideoUploadSignedRequest = {
  method: 'PUT'
  url: string
  headers?: Record<string, string>
  expires_at: number
}

export type VideoUploadReservation = {
  asset: VideoAsset
  upload_mode: VideoUploadMode
  part_size?: number
  expires_at: number
  max_size_bytes: number
  upload_limits?: VideoStudioUploadLimits
  request?: VideoUploadSignedRequest
}

export type VideoUploadedPart = {
  part_number: number
  size_bytes: number
  etag: string
}

export type VideoCompletedPart = {
  part_number: number
  etag: string
}

export type CompleteVideoUploadRequest = {
  parts?: VideoCompletedPart[]
}

export type CreateVideoUploadRequest = {
  filename: string
  mime_type: string
  size_bytes: number
  purpose: VideoUploadPurpose
  multipart: true
}

export type VideoModelProfileInput = {
  model: string
  display_name: string
  description: string
  provider_label: string
  specification: VideoModelSpec
  default_parameters: VideoParameters
  enabled: boolean
  sort_order: number
}

export type VideoSampleInput = {
  model_profile_id: number
  title: string
  prompt: string
  mode: VideoGenerationMode
  parameters: VideoParameters
  reference_assets: VideoReferenceAssetInput[]
  video_asset_id: number
  aspect_ratio: number
  category: VideoSampleCategory
  status: VideoSampleStatus
  sort_order: number
}

export type VideoStudioApiError = {
  code?: string
  message?: string
  data?: unknown
}

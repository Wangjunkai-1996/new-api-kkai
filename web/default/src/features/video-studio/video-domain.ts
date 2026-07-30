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
  CreateVideoRequest,
  VideoAsset,
  VideoComposerValues,
  VideoGeneration,
  VideoGenerationMode,
  VideoModelProfile,
  VideoParameterControl,
  VideoParameterOption,
  VideoParameterValue,
  VideoParameters,
  VideoQuote,
  VideoQuoteRequest,
  VideoReferenceAssetInput,
  VideoReferenceRole,
  VideoSample,
  VideoStudioAccessMode,
  VideoStudioApiError,
} from './types'

const ACTIVE_GENERATION_STATUSES = new Set([
  'queued',
  'processing',
  'archiving',
])

export const VIDEO_MODE_LABEL_KEYS: Record<VideoGenerationMode, string> = {
  text_to_video: 'videoStudio.mode.text',
  image_to_video: 'videoStudio.mode.image',
  first_last_frame: 'videoStudio.mode.firstLast',
}

export const VIDEO_REFERENCE_ROLE_LABEL_KEYS: Record<
  VideoReferenceRole,
  string
> = {
  reference: 'videoStudio.referenceImage',
  reference_video: 'videoStudio.referenceVideo',
  first_frame: 'videoStudio.firstFrame',
  last_frame: 'videoStudio.lastFrame',
}

export const getVideoReferenceRoles = (
  profile: VideoModelProfile,
  mode: VideoGenerationMode
): VideoReferenceRole[] => {
  if (mode === 'text_to_video') return []

  const requiredRoles = new Set(
    (profile.specification.reference_inputs ?? [])
      .filter((input) => input.required)
      .map((input) => input.role)
  )
  if (mode === 'image_to_video') {
    if (requiredRoles.has('reference_video')) return ['reference_video']
    return requiredRoles.has('reference') ? ['reference'] : []
  }

  const roles: VideoReferenceRole[] = []
  if (requiredRoles.has('first_frame')) roles.push('first_frame')
  if (requiredRoles.has('last_frame')) roles.push('last_frame')
  return roles
}

export const normalizeVideoStudioAccessMode = (
  value: unknown
): VideoStudioAccessMode => {
  if (value === 'admin' || value === 'all') return value
  return 'off'
}

export const canAccessVideoStudio = (
  mode: VideoStudioAccessMode,
  isAdmin: boolean
): boolean => mode === 'all' || (mode === 'admin' && isAdmin)

export const getProfileDefaultParameters = (
  profile: VideoModelProfile,
  mode?: VideoGenerationMode
): VideoParameters => {
  const defaults: VideoParameters = {}
  for (const parameter of profile.specification.parameters) {
    if (mode && parameter.modes && !parameter.modes.includes(mode)) continue
    if (profile.default_parameters[parameter.key] !== undefined) {
      defaults[parameter.key] = profile.default_parameters[parameter.key]
      continue
    }
    if (defaults[parameter.key] !== undefined) continue
    if (parameter.default !== undefined) {
      defaults[parameter.key] = parameter.default
    }
  }
  return defaults
}

export const videoParameterAcceptsValue = (
  control: VideoParameterControl,
  value: VideoParameterValue
): boolean => {
  if (control.control === 'segmented' || control.control === 'select') {
    return control.options.some((option) => option.value === value)
  }
  if (control.control === 'switch') return typeof value === 'boolean'
  if (control.control !== 'slider' && control.control !== 'number') return false
  if (
    typeof value !== 'number' ||
    !Number.isFinite(value) ||
    value < control.min ||
    value > control.max
  ) {
    return false
  }
  const steps = (value - control.min) / control.step
  return Math.abs(steps - Math.round(steps)) <= 1e-8
}

const requiredVideoParameterInitialValue = (
  control: VideoParameterControl
): VideoParameterValue | undefined => {
  if (!control.required) return undefined
  if (control.control === 'segmented' || control.control === 'select') {
    return control.options[0]?.value
  }
  if (control.control === 'switch') return false
  if (control.control === 'slider' || control.control === 'number') {
    return control.min
  }
  return undefined
}

export const getVideoParametersForMode = (
  profile: VideoModelProfile,
  mode: VideoGenerationMode,
  current: VideoParameters = {}
): VideoParameters => {
  const parameters = getProfileDefaultParameters(profile, mode)
  for (const control of profile.specification.parameters) {
    if (control.modes && !control.modes.includes(mode)) continue
    const value = current[control.key]
    if (value !== undefined && videoParameterAcceptsValue(control, value)) {
      parameters[control.key] = value
      continue
    }
    if (parameters[control.key] === undefined) {
      const initialValue = requiredVideoParameterInitialValue(control)
      if (initialValue !== undefined) {
        parameters[control.key] = initialValue
      }
    }
  }
  return parameters
}

export const restoreVideoComposerDraft = (
  profiles: VideoModelProfile[],
  draft: VideoComposerValues | null
): VideoComposerValues | null => {
  const profile =
    profiles.find((candidate) => candidate.id === draft?.model_profile_id) ??
    profiles[0]
  if (!profile) return null
  if (!draft || draft.model_profile_id !== profile.id) {
    return buildVideoComposerValues(profile)
  }

  const mode = profile.specification.modes.includes(draft.mode)
    ? draft.mode
    : (profile.specification.modes[0] ?? 'text_to_video')
  const referenceLimit = getVideoReferenceRoles(profile, mode).length
  return {
    model_profile_id: profile.id,
    mode,
    prompt: draft.prompt,
    reference_asset_ids: draft.reference_asset_ids.slice(0, referenceLimit),
    parameters: getVideoParametersForMode(profile, mode, draft.parameters),
  }
}

export const buildClearedVideoComposerValues = (
  profile: VideoModelProfile,
  mode: VideoGenerationMode
): VideoComposerValues => ({
  model_profile_id: profile.id,
  mode,
  prompt: '',
  reference_asset_ids: [],
  parameters: getVideoParametersForMode(profile, mode),
})

export const getSampleReferenceAssets = (
  sample: VideoSample,
  profile?: VideoModelProfile
): VideoAsset[] => {
  const roles = profile ? getVideoReferenceRoles(profile, sample.mode) : []
  return sample.reference_asset_ids.map((id, index) => ({
    id,
    scope: 'catalog',
    kind: 'reference',
    state: 'ready',
    original_filename: `reference-${index + 1}`,
    mime_type: roles[index] === 'reference_video' ? 'video/mp4' : 'image/jpeg',
    size_bytes: 0,
    width: 0,
    height: 0,
    duration_seconds: 0,
    codec: '',
    content_url: sample.reference_content_urls[index],
    created_at: sample.created_at,
    updated_at: sample.updated_at,
  }))
}

export const getSampleVideoAsset = (sample: VideoSample): VideoAsset => ({
  id: sample.video_asset_id,
  scope: 'catalog',
  kind: 'sample',
  state: 'ready',
  original_filename: sample.title,
  mime_type: 'video/mp4',
  size_bytes: 0,
  width: 0,
  height: 0,
  duration_seconds: 0,
  codec: '',
  content_url: sample.video_url,
  poster_url: sample.poster_url,
  preview_url: sample.preview_url,
  created_at: sample.created_at,
  updated_at: sample.updated_at,
})

export const buildVideoComposerValues = (
  profile: VideoModelProfile,
  sample?: VideoSample
): VideoComposerValues => {
  const preferredMode =
    sample?.mode ?? profile.specification.modes[0] ?? 'text_to_video'
  const mode = profile.specification.modes.includes(preferredMode)
    ? preferredMode
    : (profile.specification.modes[0] ?? 'text_to_video')
  const referenceLimit = getVideoReferenceRoles(profile, mode).length

  return {
    model_profile_id: profile.id,
    mode,
    prompt: sample?.prompt ?? '',
    reference_asset_ids:
      sample?.reference_asset_ids.slice(0, referenceLimit) ?? [],
    parameters: getVideoParametersForMode(profile, mode, sample?.parameters),
  }
}

export const buildVideoQuoteRequest = (
  values: VideoComposerValues,
  profile: VideoModelProfile,
  tokenId: number,
  sampleId?: number
): VideoQuoteRequest => {
  const roles = getVideoReferenceRoles(profile, values.mode)
  const referenceAssets = values.reference_asset_ids.reduce<
    VideoReferenceAssetInput[]
  >((result, assetId, index) => {
    const role = roles[index]
    if (role) result.push({ asset_id: assetId, role })
    return result
  }, [])

  return {
    token_id: tokenId,
    model: profile.model,
    mode: values.mode,
    prompt: values.prompt,
    parameters: values.parameters,
    reference_assets: referenceAssets,
    sample_id: sampleId,
  }
}

export const buildCreateVideoRequest = (
  quoteRequest: VideoQuoteRequest,
  quote: VideoQuote
): CreateVideoRequest => ({
  ...quoteRequest,
  max_quota: quote.quota,
  quote_hash: quote.request_hash,
  quote_expires_at: quote.expires_at,
})

export const getVideoSubmissionRequestKey = (
  request: VideoQuoteRequest
): string =>
  JSON.stringify({
    token_id: request.token_id,
    model: request.model,
    mode: request.mode,
    prompt: request.prompt,
    parameters: Object.entries(request.parameters).sort(([left], [right]) =>
      left.localeCompare(right)
    ),
    reference_assets: request.reference_assets,
    sample_id: request.sample_id ?? null,
  })

export const encodeVideoParameterOptionValue = (
  value: VideoParameterValue
): string => {
  if (typeof value === 'string') return `string:${value}`
  if (typeof value === 'number') return `number:${String(value)}`
  return `boolean:${String(value)}`
}

export const decodeVideoParameterOptionValue = (
  options: VideoParameterOption[],
  encodedValue: string
): VideoParameterValue | undefined =>
  options.find(
    (option) => encodeVideoParameterOptionValue(option.value) === encodedValue
  )?.value

export const isVideoGenerationActive = (generation: VideoGeneration): boolean =>
  ACTIVE_GENERATION_STATUSES.has(generation.status)

export const isVideoAssetInspectionPending = (asset: VideoAsset): boolean =>
  asset.state === 'uploaded' || asset.state === 'processing'

export const getVideoAssetInspectionPollInterval = (
  asset: VideoAsset,
  elapsedMs: number
): number | false => {
  if (!isVideoAssetInspectionPending(asset)) return false
  if (elapsedMs < 30_000) return 2_000
  if (elapsedMs < 60_000) return 5_000
  return 10_000
}

export const isVideoAssetInspectionTakingLong = (
  asset: VideoAsset,
  elapsedMs: number
): boolean => isVideoAssetInspectionPending(asset) && elapsedMs >= 60_000

export const shouldRenderVideoAssetMedia = (asset: VideoAsset): boolean =>
  asset.state === 'ready'

export type VideoSubmissionLock = {
  taskId: string | null
}

export const getVideoSubmissionLock = (
  error?: VideoStudioApiError
): VideoSubmissionLock | null => {
  if (error?.code !== 'task_submission_unknown') return null
  return {
    taskId:
      typeof error.data === 'string' && error.data.trim() !== ''
        ? error.data
        : null,
  }
}

export const getVideoTaskPollInterval = (
  generations: VideoGeneration[],
  nowSeconds: number,
  documentVisible: boolean
): number | false => {
  if (!documentVisible) return false
  const active = generations.filter(isVideoGenerationActive)
  if (active.length === 0) return false

  const newestActiveCreation = Math.max(
    ...active.map((generation) => generation.created_at)
  )
  return nowSeconds - newestActiveCreation < 60 ? 3_000 : 5_000
}

export const getVideoProgress = (generation: VideoGeneration): number => {
  const parsed = Number(generation.progress.replace('%', ''))
  return Number.isFinite(parsed) ? Math.min(100, Math.max(0, parsed)) : 0
}

export const videoQuoteHasExpired = (
  expiresAt: number,
  nowSeconds: number
): boolean => expiresAt <= nowSeconds + 2

export const getVideoQuoteRefreshDelay = (
  expiresAt: number,
  nowMilliseconds: number
): number => Math.max(0, expiresAt * 1000 - nowMilliseconds - 2_000)

export const isVideoQuoteStaleError = (
  status: number | undefined,
  error?: VideoStudioApiError
): boolean => status === 409 && error?.code === 'quote_stale'

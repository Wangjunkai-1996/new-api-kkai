import type {
  VideoAsset,
  VideoComposerReference,
  VideoModelProfile,
  VideoParameters,
} from './types'

const mediaKind = (asset: VideoAsset): 'image' | 'video' | 'audio' | null =>
  (() => {
    if (asset.mime_type.startsWith('image/')) return 'image'
    if (asset.mime_type.startsWith('video/')) return 'video'
    if (asset.mime_type.startsWith('audio/')) return 'audio'
    return null
  })()

export const validateVideoReferenceMedia = (
  references: VideoComposerReference[],
  profile: VideoModelProfile,
  parameters: VideoParameters = profile.default_parameters
): string | null => {
  const policy = profile.specification.reference_policy
  if (!policy) {
    return references.some((reference) => reference.included)
      ? 'videoStudio.validation.referencePolicyRequired'
      : null
  }
  const included = references.filter((reference) => reference.included)
  // Reference policy combination rules apply only when the request uses
  // references. A profile can support both text and reference generation;
  // an empty collection must remain valid for its text mode.
  if (included.length === 0) {
    return policy.requires_video
      ? 'videoStudio.validation.videoReferenceRequired'
      : null
  }
  if (policy.max_total_count === null) {
    return 'videoStudio.validation.referencePolicyRequired'
  }
  if (
    policy.max_total_count !== null &&
    included.length > policy.max_total_count
  ) {
    return 'videoStudio.validation.referenceLimitExceeded'
  }
  const totalDuration: Record<'video' | 'audio', number> = {
    video: 0,
    audio: 0,
  }
  const counts: Record<'image' | 'video' | 'audio', number> = {
    image: 0,
    video: 0,
    audio: 0,
  }
  for (const reference of included) {
    const asset = reference.asset
    if (!asset || asset.state !== 'ready') {
      return 'videoStudio.validation.referenceUnavailable'
    }
    const kind = mediaKind(asset)
    let expectedKind = 'image'
    if (reference.role === 'reference_video') expectedKind = 'video'
    if (reference.role === 'reference_audio') expectedKind = 'audio'
    const config = kind ? policy[kind] : undefined
    if (
      !kind ||
      kind !== expectedKind ||
      !config ||
      !config.allowed_mime_types.includes(asset.mime_type)
    ) {
      return 'videoStudio.validation.referenceMimeUnsupported'
    }
    counts[kind] += 1
    if (asset.size_bytes > config.max_size_bytes) {
      return 'videoStudio.validation.referenceSizeExceeded'
    }
    if (kind === 'image') {
      if (
        (config.min_width !== undefined && asset.width < config.min_width) ||
        (config.max_width !== undefined && asset.width > config.max_width) ||
        (config.min_height !== undefined && asset.height < config.min_height) ||
        (config.max_height !== undefined && asset.height > config.max_height)
      ) {
        return 'videoStudio.validation.referenceDimensionsInvalid'
      }
      const ratio = asset.height > 0 ? asset.width / asset.height : 0
      if (
        (config.min_aspect_ratio_exclusive !== undefined &&
          ratio <= config.min_aspect_ratio_exclusive) ||
        (config.max_aspect_ratio_exclusive !== undefined &&
          ratio >= config.max_aspect_ratio_exclusive)
      ) {
        return 'videoStudio.validation.referenceAspectInvalid'
      }
    } else {
      if (
        (config.min_duration_seconds !== undefined &&
          asset.duration_seconds < config.min_duration_seconds) ||
        (config.max_duration_seconds !== undefined &&
          asset.duration_seconds > config.max_duration_seconds)
      ) {
        return 'videoStudio.validation.referenceDurationInvalid'
      }
      totalDuration[kind] += asset.duration_seconds
    }
  }
  if (policy.requires_visual && counts.image + counts.video === 0) {
    return 'videoStudio.validation.visualReferenceRequired'
  }
  if (policy.requires_video && counts.video === 0) {
    return 'videoStudio.validation.videoReferenceRequired'
  }
  if (
    (policy.image && counts.image > policy.image.max_count) ||
    (policy.video && counts.video > policy.video.max_count) ||
    (policy.audio && counts.audio > policy.audio.max_count)
  ) {
    return 'videoStudio.validation.referenceLimitExceeded'
  }
  if (
    policy.video?.max_total_duration_seconds !== undefined &&
    totalDuration.video > policy.video.max_total_duration_seconds
  ) {
    return 'videoStudio.validation.referenceDurationTotalExceeded'
  }
  if (
    policy.audio?.max_total_duration_seconds !== undefined &&
    totalDuration.audio > policy.audio.max_total_duration_seconds
  ) {
    return 'videoStudio.validation.referenceDurationTotalExceeded'
  }
  if (
    counts.audio > 0 &&
    policy.audio_requires_generate_audio &&
    parameters.generate_audio !== true
  ) {
    return 'videoStudio.validation.audioOutputRequired'
  }
  return null
}

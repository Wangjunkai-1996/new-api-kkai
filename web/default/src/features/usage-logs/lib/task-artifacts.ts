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
import { TASK_STATUS } from '../constants'
import type {
  AudioClip,
  HostedTaskVideo,
  TaskArtifact,
  TaskArtifactProjection,
  TaskArtifactsResponse,
  TaskLog,
  TaskMediaState,
  TaskMediaSummary,
  TaskPluginAuthor,
} from '../types'

const taskArtifactTypes = new Set(['image', 'video', 'audio', 'file'])
const safeArtifactKeyPattern = /^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$/
const taskArtifactContentPathPattern =
  /\/v1\/tasks\/[^/]+\/artifacts\/[^/]+\/content$/
const taskArtifactAccessTokenPattern = /^[A-Za-z0-9_-]{43}$/
const maxTaskArtifacts = 64
export type TaskPreviewMode =
  | 'plugin'
  | 'legacy-suno'
  | 'legacy-video'
  | 'discarded'
  | 'none'

export class TaskArtifactApiError extends Error {
  constructor(
    message: string,
    public code?: string
  ) {
    super(message)
    this.name = 'TaskArtifactApiError'
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

// Studio URLs are storage signatures, not task capabilities. Only the URL
// safety rules are shared; task URLs keep their stricter path/query contract.
export function parseHostedMediaUrl(value: unknown): string {
  if (typeof value !== 'string') {
    throw new TaskArtifactApiError('invalid_content_url')
  }

  const contentUrl = value.trim()
  if (
    contentUrl.length === 0 ||
    contentUrl.length > 8192 ||
    contentUrl !== value ||
    contentUrl.includes('#') ||
    !/^https?:\/\//i.test(contentUrl)
  ) {
    throw new TaskArtifactApiError('invalid_content_url')
  }
  for (const character of contentUrl) {
    const codePoint = character.codePointAt(0) ?? 0
    if (codePoint <= 0x1f || codePoint === 0x7f || character === '\\') {
      throw new TaskArtifactApiError('invalid_content_url')
    }
  }

  try {
    const url = new URL(contentUrl)
    const authorityStart = contentUrl.indexOf('//') + 2
    const pathStart = contentUrl.indexOf('/', authorityStart)
    const authority = contentUrl.slice(
      authorityStart,
      pathStart === -1 ? contentUrl.length : pathStart
    )
    if (
      (url.protocol !== 'https:' && url.protocol !== 'http:') ||
      url.hostname.length === 0 ||
      authority.includes('@') ||
      url.username ||
      url.password ||
      url.hash
    ) {
      throw new TaskArtifactApiError('invalid_content_url')
    }
    return contentUrl
  } catch (error) {
    if (error instanceof TaskArtifactApiError) throw error
    throw new TaskArtifactApiError('invalid_content_url')
  }
}

function parseContentUrl(value: unknown): string {
  const contentUrl = parseHostedMediaUrl(value)
  const url = new URL(contentUrl)
  const accessToken = url.searchParams.get('access')
  const download = url.searchParams.get('download')
  if (
    !taskArtifactContentPathPattern.test(url.pathname) ||
    accessToken == null ||
    !taskArtifactAccessTokenPattern.test(accessToken) ||
    url.searchParams.getAll('access').length !== 1 ||
    url.searchParams.getAll('download').length > 1 ||
    (download !== null && download !== '1') ||
    [...url.searchParams.keys()].some(
      (key) => key !== 'access' && key !== 'download'
    ) ||
    ![
      `?access=${accessToken}`,
      `?access=${accessToken}&download=1`,
      `?download=1&access=${accessToken}`,
    ].includes(url.search)
  ) {
    throw new TaskArtifactApiError('invalid_content_url')
  }
  return contentUrl
}

export function getTaskArtifactDownloadUrl(contentUrl: string): string {
  const url = new URL(parseContentUrl(contentUrl))
  url.searchParams.set('download', '1')
  return url.toString()
}

const mediaStates = new Set([
  'ready',
  'processing',
  'unavailable',
  'restricted',
])

function parseMediaDimensions(value: Record<string, unknown>) {
  const dimensions: Pick<
    TaskMediaSummary,
    'width' | 'height' | 'duration_seconds'
  > = {}
  for (const key of ['width', 'height', 'duration_seconds'] as const) {
    const number = value[key]
    if (number === undefined) continue
    if (typeof number !== 'number' || !Number.isFinite(number) || number <= 0) {
      throw new TaskArtifactApiError('invalid_media_dimensions')
    }
    dimensions[key] = number
  }
  return dimensions
}

export function parseTaskMediaSummary(value: unknown): TaskMediaSummary {
  if (
    !isRecord(value) ||
    value.source !== 'studio' ||
    typeof value.state !== 'string' ||
    !mediaStates.has(value.state)
  ) {
    throw new TaskArtifactApiError('invalid_media_summary')
  }
  const media: TaskMediaSummary = {
    source: 'studio',
    state: value.state as TaskMediaState,
    ...parseMediaDimensions(value),
  }
  if (value.poster_url != null || value.poster_expires_at != null) {
    if (
      media.state !== 'ready' ||
      !Number.isSafeInteger(value.poster_expires_at) ||
      Number(value.poster_expires_at) <= 0
    ) {
      throw new TaskArtifactApiError('invalid_media_summary')
    }
    media.poster_url = parseHostedMediaUrl(value.poster_url)
    media.poster_expires_at = Number(value.poster_expires_at)
  }
  return media
}

function parseHostedTaskVideo(
  value: unknown,
  expectedRecordId?: number
): HostedTaskVideo {
  if (
    !isRecord(value) ||
    typeof value.state !== 'string' ||
    !mediaStates.has(value.state) ||
    !Number.isSafeInteger(value.task_record_id) ||
    Number(value.task_record_id) <= 0 ||
    (expectedRecordId !== undefined &&
      value.task_record_id !== expectedRecordId)
  ) {
    throw new TaskArtifactApiError('invalid_hosted_video_identity')
  }
  const video: HostedTaskVideo = {
    task_record_id: Number(value.task_record_id),
    state: value.state as TaskMediaState,
    ...parseMediaDimensions(value),
  }
  if (video.state === 'ready') {
    if (
      !Number.isSafeInteger(value.expires_at) ||
      Number(value.expires_at) <= 0
    ) {
      throw new TaskArtifactApiError('invalid_hosted_video_expiry')
    }
    video.content_url = parseHostedMediaUrl(value.content_url)
    video.download_url = parseHostedMediaUrl(value.download_url)
    video.expires_at = Number(value.expires_at)
  } else if (
    value.content_url != null ||
    value.download_url != null ||
    value.expires_at != null
  ) {
    throw new TaskArtifactApiError('invalid_hosted_video_state')
  }
  return video
}

function parseTaskArtifact(value: unknown): TaskArtifact {
  if (!isRecord(value)) {
    throw new TaskArtifactApiError('invalid_artifact')
  }

  const key = typeof value.key === 'string' ? value.key : ''
  const type = typeof value.type === 'string' ? value.type : ''
  if (
    key !== key.trim() ||
    !safeArtifactKeyPattern.test(key) ||
    !taskArtifactTypes.has(type)
  ) {
    throw new TaskArtifactApiError('invalid_artifact')
  }

  const artifact: TaskArtifact = {
    key,
    type: type as TaskArtifact['type'],
    content_url: parseContentUrl(value.content_url),
  }
  if (typeof value.mime_type === 'string' && value.mime_type.trim()) {
    const mimeType = value.mime_type.trim()
    if (mimeType.length > 255 || /[\r\n]/.test(mimeType)) {
      throw new TaskArtifactApiError('invalid_artifact')
    }
    artifact.mime_type = mimeType
  }
  return artifact
}

// Legacy Suno tasks persisted their clip list as an array or as a JSON string
// holding one. Only clips with an audio URL can be previewed.
export function parseLegacyAudioClips(data: unknown): AudioClip[] {
  let values: unknown[] = []
  if (Array.isArray(data)) {
    values = data
  } else if (typeof data === 'string') {
    try {
      const parsed = JSON.parse(data)
      values = Array.isArray(parsed) ? parsed : []
    } catch {
      return []
    }
  }
  return values
    .filter(
      (value): value is AudioClip =>
        isRecord(value) && typeof value.audio_url === 'string'
    )
    .slice(0, maxTaskArtifacts)
}

export function parseTaskArtifactsResponse(
  response: TaskArtifactsResponse,
  expectedRecordId?: number
): TaskArtifactProjection {
  if (!response.success) {
    throw new TaskArtifactApiError(
      response.message || 'artifact_projection_failed',
      response.code
    )
  }

  const rawArtifacts = response.data?.artifacts
  if (rawArtifacts != null && !Array.isArray(rawArtifacts)) {
    throw new TaskArtifactApiError('invalid_artifact_response')
  }
  const artifactValues = rawArtifacts ?? []
  if (artifactValues.length > maxTaskArtifacts) {
    throw new TaskArtifactApiError('invalid_artifact_response')
  }

  const artifacts = artifactValues.map(parseTaskArtifact)
  const keys = new Set<string>()
  for (const artifact of artifacts) {
    if (keys.has(artifact.key)) {
      throw new TaskArtifactApiError('duplicate_artifact_key')
    }
    keys.add(artifact.key)
  }
  const projection: TaskArtifactProjection = { artifacts }
  if (response.data?.hosted_video != null) {
    projection.hostedVideo = parseHostedTaskVideo(
      response.data.hosted_video,
      expectedRecordId
    )
    if (
      artifacts.length ||
      response.data.legacy_content_url != null ||
      response.data.legacy_audio_clips != null
    ) {
      throw new TaskArtifactApiError('ambiguous_hosted_video_response')
    }
  }
  if (response.data?.legacy_content_url != null) {
    projection.legacyContentUrl = parseContentUrl(
      response.data.legacy_content_url
    )
  }
  if (response.data?.legacy_audio_clips != null) {
    projection.legacyAudioClips = parseLegacyAudioClips(
      response.data.legacy_audio_clips
    )
  }
  return projection
}

export function shouldLoadTaskArtifacts(
  log: TaskLog,
  dialogOpen: boolean
): boolean {
  return (
    dialogOpen &&
    log.status === TASK_STATUS.SUCCESS &&
    log.result_discarded !== true
  )
}

export function resolveTaskPreviewMode(
  log: TaskLog,
  hasProjectedArtifacts = false
): TaskPreviewMode {
  if (log.status !== TASK_STATUS.SUCCESS) return 'none'
  // A synchronous result was returned inline and never persisted; there is
  // nothing to project, so the viewer explains that instead of opening.
  if (log.result_discarded === true) return 'discarded'
  if (hasProjectedArtifacts) return 'plugin'
  if (log.admin_info?.task_plugin) return 'plugin'
  if (log.platform === 'suno') return 'legacy-suno'
  if (log.legacy_video_available) return 'legacy-video'
  return 'plugin'
}

export function getSafePluginAuthorUrl(
  author?: TaskPluginAuthor
): string | undefined {
  if (!author?.url) return undefined
  try {
    const url = new URL(author.url)
    if (url.protocol !== 'https:' && url.protocol !== 'http:') return undefined
    return url.toString()
  } catch {
    return undefined
  }
}

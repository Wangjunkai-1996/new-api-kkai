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
import { AxiosError } from 'axios'

import { useAuthStore } from '@/stores/auth-store'

import { videoSubmissionReceiptSchema, videoTaskIdSchema } from './schemas'
import type { VideoQuoteRequest, VideoSubmissionReceipt } from './types'
import { getVideoSubmissionRequestKey, trimVideoInput } from './video-domain'

export type VideoSubmissionIdentity = Readonly<{
  userId: number
  epoch: number
}>

let identityEpoch = 0
useAuthStore.subscribe((state, previous) => {
  if (
    state.auth.user?.id !== previous.auth.user?.id ||
    state.auth.session?.sid !== previous.auth.session?.sid
  ) {
    identityEpoch += 1
  }
})

export const getVideoSubmissionIdentity = (): VideoSubmissionIdentity => ({
  userId: useAuthStore.getState().auth.user?.id ?? 0,
  epoch: identityEpoch,
})

export const isCurrentVideoSubmissionIdentity = (
  identity: VideoSubmissionIdentity
): boolean =>
  identity.userId > 0 &&
  identity.userId === useAuthStore.getState().auth.user?.id &&
  identity.epoch === identityEpoch

export const normalizeVideoSubmissionRequest = (
  request: VideoQuoteRequest
): VideoQuoteRequest => {
  const references = [...request.reference_assets]
  if (
    new Set(references.map((reference) => reference.role)).size !==
    references.length
  ) {
    throw new Error('Duplicate video reference role')
  }
  return {
    ...request,
    model: trimVideoInput(request.model),
    prompt: trimVideoInput(request.prompt),
    parameters: { ...request.parameters },
    reference_assets: references.map((reference) => ({ ...reference })),
  }
}

export const getVideoSubmissionFingerprint = async (
  identity: VideoSubmissionIdentity,
  group: string,
  request: VideoQuoteRequest
): Promise<string> => {
  if (!identity.userId || !trimVideoInput(group)) {
    throw new Error('Missing video submission identity')
  }
  const canonical = JSON.stringify({
    version: 1,
    userId: identity.userId,
    group: trimVideoInput(group),
    request: getVideoSubmissionRequestKey(
      normalizeVideoSubmissionRequest(request)
    ),
  })
  const digest = await crypto.subtle.digest(
    'SHA-256',
    new TextEncoder().encode(canonical)
  )
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, '0')
  ).join('')
}

export class VideoSubmissionUncertainError extends Error {
  constructor(readonly taskId?: string) {
    super('Video submission could not be confirmed')
    this.name = 'VideoSubmissionUncertainError'
  }
}

export const parseVideoSubmissionReceipt = (
  value: unknown
): VideoSubmissionReceipt => {
  const parsed = videoSubmissionReceiptSchema.safeParse(value)
  if (parsed.success) return parsed.data
  // Conflicting identities cannot be used to locate an unknown submission.
  const record =
    value && typeof value === 'object' ? (value as Record<string, unknown>) : {}
  const task = videoTaskIdSchema.safeParse(record.task_id ?? record.id)
  const conflict =
    record.task_id !== undefined &&
    record.id !== undefined &&
    record.task_id !== record.id
  throw new VideoSubmissionUncertainError(
    !conflict && task.success ? task.data : undefined
  )
}

const rejectedVideoCodes: Readonly<Record<number, ReadonlySet<string>>> = {
  400: new Set([
    'video_quote_required',
    'idempotency_key_required',
    'invalid_video_studio_request',
    'video_token_required',
  ]),
  403: new Set([
    'video_token_invalid',
    'video_token_group_invalid',
    'video_token_model_forbidden',
    'video_token_ip_forbidden',
    'video_token_group_unavailable',
    'video_studio_access_denied',
  ]),
  409: new Set(['quote_stale']),
}

// Only call for the current POST /pg/videos; quote/token errors are not evidence.
export const isVideoSubmissionRejected = (error: unknown): boolean => {
  if (!(error instanceof AxiosError) || !error.response) return false
  const data: unknown = error.response.data
  if (
    !data ||
    typeof data !== 'object' ||
    !('code' in data) ||
    typeof data.code !== 'string'
  ) {
    return false
  }
  return rejectedVideoCodes[error.response.status]?.has(data.code) ?? false
}

export const getUnknownVideoSubmissionTaskId = (
  error: unknown
): string | undefined => {
  if (error instanceof VideoSubmissionUncertainError) return error.taskId
  if (!(error instanceof AxiosError)) return undefined
  const data: unknown = error.response?.data
  if (
    !data ||
    typeof data !== 'object' ||
    !('code' in data) ||
    data.code !== 'task_submission_unknown' ||
    !('data' in data)
  ) {
    return undefined
  }
  const parsed = videoTaskIdSchema.safeParse(data.data)
  return parsed.success ? parsed.data : undefined
}

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
import type { VideoTokenCapability } from './types'

export type VideoTokenAccess =
  | {
      kind: 'ready'
      requiredGroup: string
      tokenId: number
      tokenName: string
    }
  | { kind: 'missing'; requiredGroup: string }
  | { kind: 'group-unavailable'; requiredGroup: string }
  | { kind: 'limit-reached'; requiredGroup: string }
  | { kind: 'models-unavailable'; requiredGroup: string }
  | { kind: 'invalid'; requiredGroup: string }

export type VideoTokenErrorKind =
  | 'group-unavailable'
  | 'limit-reached'
  | 'models-unavailable'
  | 'token-invalid'
  | 'request-failed'

export type VideoTokenGateAction = 'create' | 'recheck' | 'none'

export type VideoTokenScopeBlocker = Readonly<{
  userId: number
  access: VideoTokenAccess
}>

const videoTokenInvalidCodes = new Set([
  'video_token_required',
  'video_token_invalid',
  'video_token_group_invalid',
  'video_token_model_forbidden',
  'video_token_ip_forbidden',
])

export const resolveVideoTokenAccess = (
  capability: VideoTokenCapability
): VideoTokenAccess => {
  const requiredGroup = capability.required_group.trim()
  if (!requiredGroup) return { kind: 'invalid', requiredGroup }

  if (capability.status === 'group_unavailable') {
    return { kind: 'group-unavailable', requiredGroup }
  }
  if (capability.status === 'limit_reached') {
    return { kind: 'limit-reached', requiredGroup }
  }
  if (capability.status === 'models_unavailable') {
    return { kind: 'models-unavailable', requiredGroup }
  }
  if (capability.status === 'missing') {
    return !capability.has_usable_token && capability.can_create
      ? { kind: 'missing', requiredGroup }
      : { kind: 'invalid', requiredGroup }
  }

  const token = capability.token
  if (
    !token ||
    !Number.isInteger(token.id) ||
    token.id <= 0 ||
    token.group.trim() !== requiredGroup
  ) {
    return { kind: 'invalid', requiredGroup }
  }

  return {
    kind: 'ready',
    requiredGroup,
    tokenId: token.id,
    tokenName: token.name,
  }
}

const videoTokenPromptScope = (userId: number, requiredGroup: string): string =>
  JSON.stringify([userId, requiredGroup.trim()])

export const shouldAutoPromptVideoToken = (
  promptedScopes: ReadonlySet<string>,
  userId: number,
  access: VideoTokenAccess | null
): boolean => {
  if (userId <= 0 || access?.kind !== 'missing') return false
  return !promptedScopes.has(
    videoTokenPromptScope(userId, access.requiredGroup)
  )
}

export const rememberVideoTokenAutoPrompt = (
  promptedScopes: Set<string>,
  userId: number,
  requiredGroup: string
): void => {
  if (userId <= 0 || !requiredGroup.trim()) return
  promptedScopes.add(videoTokenPromptScope(userId, requiredGroup))
}

export const getVideoTokenTerminalAccess = (
  errorKind: VideoTokenErrorKind,
  requiredGroup: string
): VideoTokenAccess | null => {
  if (errorKind === 'group-unavailable') {
    return { kind: 'group-unavailable', requiredGroup }
  }
  if (errorKind === 'limit-reached') {
    return { kind: 'limit-reached', requiredGroup }
  }
  if (errorKind === 'models-unavailable') {
    return { kind: 'models-unavailable', requiredGroup }
  }
  return null
}

export const getVideoTokenRequestFailureAccess = (
  errorKind: VideoTokenErrorKind,
  requiredGroup: string
): VideoTokenAccess | null => {
  if (errorKind === 'token-invalid') {
    return { kind: 'invalid', requiredGroup }
  }
  return getVideoTokenTerminalAccess(errorKind, requiredGroup)
}

export const getVideoTokenScopeAccess = (
  queriedAccess: VideoTokenAccess | null,
  blocker: VideoTokenScopeBlocker | null,
  userId: number
): VideoTokenAccess | null => {
  if (blocker?.userId !== userId) {
    return queriedAccess
  }
  return blocker.access
}

export const releaseVideoTokenScopeBlocker = (
  blocker: VideoTokenScopeBlocker | null,
  userId: number,
  checkSucceeded: boolean
): VideoTokenScopeBlocker | null => {
  if (!checkSucceeded || blocker?.userId !== userId) {
    return blocker
  }
  return null
}

export const getVideoTokenGateAction = (
  access: VideoTokenAccess | null,
  checkFailed: boolean
): VideoTokenGateAction => {
  if (access?.kind === 'missing') return 'create'
  if (checkFailed || (access !== null && access.kind !== 'ready')) {
    return 'recheck'
  }
  return 'none'
}

export const getVideoTokenErrorKind = (code?: string): VideoTokenErrorKind => {
  if (code === 'video_token_group_unavailable') return 'group-unavailable'
  if (code === 'video_token_limit_reached') return 'limit-reached'
  if (code === 'video_token_models_unavailable') return 'models-unavailable'
  if (code && videoTokenInvalidCodes.has(code)) return 'token-invalid'
  return 'request-failed'
}

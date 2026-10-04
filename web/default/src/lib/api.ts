import type { UserGroupInfo } from './group-display'
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
import { api } from './http-client'
import { authRequestOptions, authResult } from './secure-verification'

export { api }
export type { ApiRequestConfig } from './http-client'
export {
  applyAuthBundle,
  applyAuthRotation,
  bootstrapAuthentication,
  clearAuthenticatedClientState,
  clearAuthentication,
  getCommonHeaders,
  getFreshAuthHeaders,
  isAuthBundle,
  refreshAuthentication,
  resolveAuthentication,
  AuthRotationError,
} from './auth-session'
export type { AuthTokenRotation, RefreshOutcome } from './auth-session'
// Common API Functions
// ============================================================================

// ----------------------------------------------------------------------------
// User APIs
// ----------------------------------------------------------------------------

// Get current user info
export async function getSelf() {
  const res = await api.get('/api/user/self', {
    // Avoid global 401 toast during guards/preloads
    skipErrorHandler: true,
  })
  return res.data
}

// Get user available models
export async function getUserModels(): Promise<{
  success: boolean
  message?: string
  data?: string[]
}> {
  const res = await api.get('/api/user/models')
  return res.data
}

// Get user groups with descriptions and ratios
export async function getUserGroups(): Promise<{
  success: boolean
  message?: string
  data?: Record<string, UserGroupInfo>
}> {
  const res = await api.get('/api/user/self/groups')
  return res.data
}

// ----------------------------------------------------------------------------
// System APIs
// ----------------------------------------------------------------------------

// Get system status
export async function getStatus() {
  const res = await api.get('/api/status')
  return res.data?.data as Record<string, unknown>
}

// Get system notice
export async function getNotice(): Promise<{
  success: boolean
  message?: string
  data?: string
}> {
  const res = await api.get('/api/notice')
  return res.data
}

// ----------------------------------------------------------------------------
// 2FA Management APIs
// ----------------------------------------------------------------------------

// Get 2FA status
export async function get2FAStatus() {
  const res = await api.get('/api/user/2fa/status')
  return res.data
}

// Setup 2FA
export async function setup2FA() {
  const res = await api.post('/api/user/2fa/setup')
  return res.data
}

// Enable 2FA with verification code
export async function enable2FA(code: string) {
  const res = await api.post('/api/user/2fa/enable', { code })
  return res.data
}

// Disable 2FA with verification code
export function disable2FA(
  code: string
): Promise<{ success: boolean; message?: string }>
export function disable2FA(
  proofToken: string,
  signal: AbortSignal
): Promise<{ notification_warning?: boolean }>
export function disable2FA(
  proofToken: string,
  signal?: AbortSignal
): Promise<unknown> {
  if (signal) {
    return authResult(
      api.post(
        '/api/user/2fa/disable',
        {},
        {
          ...authRequestOptions,
          headers: { 'X-Security-Proof': proofToken },
          acceptAuthRotation: true,
          singleUseAuthorization: true,
          signal,
        }
      )
    )
  }
  return api
    .post('/api/user/2fa/disable', { code: proofToken })
    .then((res) => res.data)
}

// Regenerate 2FA backup codes
export function regenerate2FABackupCodes(code: string): Promise<{
  success: boolean
  message?: string
  data?: { backup_codes: string[] }
}>
export function regenerate2FABackupCodes(
  proofToken: string,
  signal: AbortSignal
): Promise<{ backup_codes: string[]; notification_warning?: boolean }>
export function regenerate2FABackupCodes(
  proofToken: string,
  signal?: AbortSignal
): Promise<unknown> {
  if (signal) {
    return authResult(
      api.post(
        '/api/user/2fa/backup_codes',
        {},
        {
          ...authRequestOptions,
          headers: { 'X-Security-Proof': proofToken },
          acceptAuthRotation: true,
          singleUseAuthorization: true,
          signal,
        }
      )
    )
  }
  return api
    .post('/api/user/2fa/backup_codes', { code: proofToken })
    .then((res) => res.data)
}

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
import { api } from '@/lib/api'
import type { CustomOAuthBinding } from '@/lib/oauth'
import { authRequestOptions, authResult } from '@/lib/secure-verification'
import type { LoginSession } from '@/stores/auth-store'

import type {
  ApiResponse,
  UserProfile,
  UpdateUserRequest,
  UpdateUserSettingsRequest,
  DeleteAccountRequest,
  CheckinStatusResponse,
  CheckinResponse,
  AccountSecurityResult,
  EmailBindingFlow,
} from './types'

// ============================================================================
// User Profile APIs
// ============================================================================

/**
 * Get current user profile
 */
export async function getUserProfile(): Promise<ApiResponse<UserProfile>> {
  const res = await api.get('/api/user/self')
  return res.data
}

/**
 * Update user profile
 */
export async function updateUserProfile(
  data: UpdateUserRequest
): Promise<ApiResponse> {
  const res = await api.put('/api/user/self', data)
  return res.data
}

export function changeAccountPassword(
  data: UpdateUserRequest,
  proofToken: string,
  signal: AbortSignal
): Promise<AccountSecurityResult & { has_password: boolean }> {
  return authResult(
    api.put('/api/user/self', data, {
      ...authRequestOptions,
      headers: { 'X-Security-Proof': proofToken },
      acceptAuthRotation: true,
      singleUseAuthorization: true,
      signal,
    })
  )
}

/**
 * Update user settings
 */
export async function updateUserSettings(
  data: UpdateUserSettingsRequest
): Promise<ApiResponse> {
  const res = await api.put('/api/user/setting', data)
  return res.data
}

/**
 * Update interface language preference
 */
export async function updateUserLanguage(
  language: string
): Promise<ApiResponse> {
  const res = await api.put('/api/user/self', { language })
  return res.data
}

/**
 * Delete user account
 */
export async function deleteUserAccount(
  data?: DeleteAccountRequest
): Promise<ApiResponse>
export function deleteUserAccount(
  proof: string,
  signal: AbortSignal
): Promise<AccountSecurityResult>
export async function deleteUserAccount(
  dataOrProof?: DeleteAccountRequest | string,
  signal?: AbortSignal
): Promise<ApiResponse | AccountSecurityResult> {
  if (typeof dataOrProof === 'string' && signal) {
    return deleteUserAccountWithProof(dataOrProof, signal)
  }
  const res = await api.delete('/api/user/self', { data: dataOrProof })
  return res.data
}

export function deleteUserAccountWithProof(
  proof: string,
  signal: AbortSignal
): Promise<AccountSecurityResult> {
  return authResult(
    api.delete('/api/user/self', {
      ...authRequestOptions,
      headers: { 'X-Security-Proof': proof },
      singleUseAuthorization: true,
      signal,
    })
  )
}

/**
 * Generate/regenerate system access token
 */
export async function generateAccessToken(): Promise<ApiResponse<string>> {
  const res = await api.post('/api/user/token')
  return res.data
}

// ============================================================================
// Account Binding APIs
// ============================================================================

/**
 * Send email verification code
 */
export async function sendEmailVerification(
  email: string,
  turnstileToken?: string
): Promise<ApiResponse> {
  const params = new URLSearchParams({ email })
  if (turnstileToken) {
    params.append('turnstile', turnstileToken)
  }
  const res = await api.get(`/api/verification?${params}`)
  return res.data
}

/**
 * Bind email account
 */
export async function bindEmail(
  email: string,
  code: string
): Promise<ApiResponse>
export function bindEmail(
  flowToken: string,
  newCode: string,
  oldCode: string,
  signal: AbortSignal
): Promise<AccountSecurityResult>
export async function bindEmail(
  emailOrFlow: string,
  code: string,
  oldCode?: string,
  signal?: AbortSignal
): Promise<ApiResponse | AccountSecurityResult> {
  if (oldCode !== undefined && signal) {
    return authResult(
      api.post('/api/oauth/email/bind', {
        flow_token: emailOrFlow,
        new_code: code,
        old_code: oldCode,
      }, { ...authRequestOptions, singleUseAuthorization: true, signal })
    )
  }
  const res = await api.post('/api/oauth/email/bind', {
    email: emailOrFlow,
    code,
  })
  return res.data
}

export function startEmailBinding(
  email: string,
  proofToken: string,
  signal: AbortSignal
): Promise<EmailBindingFlow> {
  return authResult(
    api.post('/api/oauth/email/bind/start', { email }, {
      ...authRequestOptions,
      headers: { 'X-Security-Proof': proofToken },
      singleUseAuthorization: true,
      signal,
    })
  )
}

export function resendEmailBinding(
  flowToken: string,
  signal: AbortSignal
): Promise<EmailBindingFlow> {
  return authResult(
    api.post('/api/oauth/email/bind/resend', { flow_token: flowToken }, {
      ...authRequestOptions,
      singleUseAuthorization: true,
      signal,
    })
  )
}

/**
 * Bind WeChat account
 */
export async function bindWeChat(code: string): Promise<ApiResponse>
export function bindWeChat(
  code: string,
  proof: string,
  signal: AbortSignal
): Promise<AccountSecurityResult>
export async function bindWeChat(
  code: string,
  proof?: string,
  signal?: AbortSignal
): Promise<ApiResponse | AccountSecurityResult> {
  if (proof && signal) {
    return authResult(
      api.post('/api/oauth/wechat/bind', { code }, {
        ...authRequestOptions,
        headers: { 'X-Security-Proof': proof },
        singleUseAuthorization: true,
        signal,
      })
    )
  }
  const res = await api.post(
    '/api/oauth/wechat/bind',
    { code },
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return res.data
}

export async function startTelegramBind(): Promise<
  ApiResponse<{ flow_token: string; callback_url: string; expires_at?: number }>
> {
  const res = await api.post('/api/oauth/telegram/bind/start')
  return res.data
}

export async function getLoginSessions(): Promise<ApiResponse<LoginSession[]>> {
  const res = await api.get('/api/user/sessions')
  return res.data
}

export async function revokeLoginSession(sid: string): Promise<ApiResponse> {
  const res = await api.delete('/api/user/sessions/' + encodeURIComponent(sid))
  return res.data
}

export async function revokeOtherLoginSessions(): Promise<ApiResponse> {
  const res = await api.post('/api/user/sessions/revoke-others')
  return res.data
}

// ============================================================================
// Custom OAuth Binding APIs
// ============================================================================

/**
 * Get current user's custom OAuth bindings
 */
export async function getSelfOAuthBindings(): Promise<
  ApiResponse<CustomOAuthBinding[]>
> {
  const res = await api.get('/api/user/oauth/bindings')
  return res.data
}

/**
 * Unbind a custom OAuth provider for current user
 */
export async function unbindCustomOAuth(
  providerId: number
): Promise<ApiResponse>
export function unbindCustomOAuth(
  providerId: number,
  proof: string,
  signal: AbortSignal
): Promise<AccountSecurityResult>
export async function unbindCustomOAuth(
  providerId: number,
  proof?: string,
  signal?: AbortSignal
): Promise<ApiResponse | AccountSecurityResult> {
  if (proof && signal) {
    return authResult(
      api.delete(`/api/user/oauth/bindings/${providerId}`, {
        ...authRequestOptions,
        headers: { 'X-Security-Proof': proof },
        singleUseAuthorization: true,
        signal,
      })
    )
  }
  const res = await api.delete(`/api/user/oauth/bindings/${providerId}`)
  return res.data
}

// ============================================================================
// Checkin APIs
// ============================================================================

/**
 * Get checkin status for a specific month
 */
export async function getCheckinStatus(
  month: string
): Promise<ApiResponse<CheckinStatusResponse>> {
  const res = await api.get(`/api/user/checkin?month=${month}`)
  return res.data
}

/**
 * Perform daily checkin
 */
export async function performCheckin(
  turnstileToken?: string
): Promise<ApiResponse<CheckinResponse>> {
  const url = turnstileToken
    ? `/api/user/checkin?turnstile=${encodeURIComponent(turnstileToken)}`
    : '/api/user/checkin'
  const res = await api.post(url)
  return res.data
}

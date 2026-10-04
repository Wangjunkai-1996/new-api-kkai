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
import { useNavigate } from '@tanstack/react-router'
import i18n from 'i18next'
import { useCallback } from 'react'

import { getSavedLanguage, sanitizeAuthRedirect } from '@/features/auth/lib/auth-redirect'
import { applyAuthBundle, isAuthBundle } from '@/lib/auth-session'
import { useAuthStore, type AuthBundle, type LoginChallenge } from '@/stores/auth-store'

function isLoginChallenge(value: unknown): value is LoginChallenge {
  if (!value || typeof value !== 'object') return false
  const challenge = value as Partial<LoginChallenge>
  return (
    challenge.require_verification === true &&
    typeof challenge.flow_token === 'string' &&
    challenge.flow_token.length > 0 &&
    typeof challenge.expires_at === 'number' &&
    Number.isFinite(challenge.expires_at) &&
    Array.isArray(challenge.methods) &&
    challenge.methods.some(
      (method) =>
        (method.method === '2fa' || method.method === 'passkey') &&
        method.available === true
    )
  )
}

export function useAuthRedirect() {
  const navigate = useNavigate()

  const handleLoginSuccess = useCallback(
    async (bundle: AuthBundle, redirectTo?: string) => {
      applyAuthBundle(bundle)
      const savedLanguage = getSavedLanguage(bundle.user)
      if (savedLanguage && savedLanguage !== i18n.language) {
        await i18n.changeLanguage(savedLanguage)
      }
      const target =
        sanitizeAuthRedirect(redirectTo, window.location.origin) ?? '/dashboard'
      await navigate({ href: target, replace: true })
    },
    [navigate]
  )

  const handleLoginResult = useCallback(
    async (result: unknown, redirectTo?: string): Promise<boolean> => {
      if (isAuthBundle(result)) {
        await handleLoginSuccess(result, redirectTo)
        return true
      }
      if (!isLoginChallenge(result) || result.expires_at * 1000 <= Date.now()) {
        throw new Error('Login failed')
      }
      useAuthStore.getState().auth.setPendingLoginVerification({
        challenge: result,
        redirectTo:
          sanitizeAuthRedirect(redirectTo, window.location.origin) ?? undefined,
      })
      await navigate({ to: '/otp', replace: true })
      return false
    },
    [handleLoginSuccess, navigate]
  )

  const redirectToLogin = useCallback(() => {
    void navigate({ to: '/sign-in', replace: true })
  }, [navigate])

  const redirectToRegister = useCallback(() => {
    void navigate({ to: '/sign-up', replace: true })
  }, [navigate])

  return {
    handleLoginSuccess,
    handleLoginResult,
    redirectToLogin,
    redirectToRegister,
  }
}

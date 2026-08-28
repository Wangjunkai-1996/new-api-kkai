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
import { zodResolver } from '@hookform/resolvers/zod'
import { useSearch } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { z } from 'zod'

import { login } from '@/features/auth/api'
import { loginFormSchema } from '@/features/auth/constants'
import { useAuthRedirect } from '@/features/auth/hooks/use-auth-redirect'
import { useOAuthLogin } from '@/features/auth/hooks/use-oauth-login'
import { useTurnstile } from '@/features/auth/hooks/use-turnstile'
import { beginPasskeyLogin, finishPasskeyLogin } from '@/features/auth/passkey'
import { useStatus } from '@/hooks/use-status'
import {
  buildAssertionResult,
  isPasskeySupported as detectPasskeySupport,
  prepareCredentialRequestOptions,
} from '@/lib/passkey'

export type LinkAiLoginFields = z.infer<typeof loginFormSchema>

export function useLinkAiSignIn() {
  const { t } = useTranslation()
  const { redirect } = useSearch({ from: '/(auth)/sign-in' })
  const { status } = useStatus()
  const { handleLoginSuccess, redirectTo2FA } = useAuthRedirect()
  const oauth = useOAuthLogin(status)
  const [isLoading, setIsLoading] = useState(false)
  const [isPasskeyLoading, setIsPasskeyLoading] = useState(false)
  const [passkeySupported, setPasskeySupported] = useState(false)
  const [agreedToLegal, setAgreedToLegal] = useState(false)
  const [turnstileWidgetKey, setTurnstileWidgetKey] = useState(0)
  const turnstile = useTurnstile()
  const form = useForm<LinkAiLoginFields>({
    resolver: zodResolver(loginFormSchema),
    defaultValues: { username: '', password: '' },
  })

  const requiresLegalConsent = Boolean(
    status?.user_agreement_enabled || status?.privacy_policy_enabled
  )

  useEffect(() => {
    detectPasskeySupport()
      .then(setPasskeySupported)
      .catch(() => setPasskeySupported(false))
  }, [])

  async function onSubmit(data: LinkAiLoginFields) {
    if (requiresLegalConsent && !agreedToLegal) {
      toast.error(t('Please agree to the legal terms first'))
      return
    }
    if (!turnstile.validateTurnstile()) return

    const submittedTurnstileToken = turnstile.turnstileToken
    if (turnstile.isTurnstileEnabled) {
      turnstile.setTurnstileToken('')
      setTurnstileWidgetKey((current) => current + 1)
    }

    setIsLoading(true)
    try {
      const response = await login({
        username: data.username,
        password: data.password,
        turnstile: submittedTurnstileToken,
      })
      if (response.success) {
        if (response.data?.require_2fa) {
          redirectTo2FA()
          return
        }
        await handleLoginSuccess(
          response.data as { id?: number } | null,
          redirect
        )
        toast.success(t('Welcome back!'))
      }
    } finally {
      setIsLoading(false)
    }
  }

  const requireConfigured = (
    isConfigured: boolean,
    callback: () => void | Promise<void>
  ) => {
    if (!isConfigured) {
      toast.info(t('This sign-in method is not configured'))
      return
    }
    void callback()
  }

  async function handlePasskeyLogin() {
    if (requiresLegalConsent && !agreedToLegal) {
      toast.error(t('Please agree to the legal terms first'))
      return
    }
    if (!status?.passkey_login || !passkeySupported) {
      toast.info(t('Passkey sign-in is not available on this device'))
      return
    }

    setIsPasskeyLoading(true)
    try {
      const begin = await beginPasskeyLogin()
      if (!begin.success) {
        throw new Error(begin.message || t('Failed to start Passkey login'))
      }
      const publicKey = prepareCredentialRequestOptions(
        begin.data?.options ?? begin.data
      )
      const credential = (await navigator.credentials.get({
        publicKey,
      })) as PublicKeyCredential | null
      if (!credential) return

      const assertion = buildAssertionResult(credential)
      if (!assertion) throw new Error(t('Invalid Passkey response'))

      const finish = await finishPasskeyLogin(assertion)
      if (!finish.success || !finish.data) {
        throw new Error(finish.message || t('Failed to complete Passkey login'))
      }
      await handleLoginSuccess(finish.data as { id?: number } | null, redirect)
      toast.success(t('Signed in with Passkey'))
    } catch (error: unknown) {
      if (error instanceof DOMException && error.name === 'NotAllowedError') {
        toast.info(t('Passkey login was cancelled or timed out'))
      } else if (error instanceof Error) {
        toast.error(error.message)
      } else {
        toast.error(t('Passkey login failed'))
      }
    } finally {
      setIsPasskeyLoading(false)
    }
  }

  return {
    agreedToLegal,
    form,
    handlePasskeyLogin,
    isLoading,
    isPasskeyLoading,
    oauth,
    onSubmit,
    requireConfigured,
    requiresLegalConsent,
    setAgreedToLegal,
    status,
    turnstile,
    turnstileWidgetKey,
  }
}

export type LinkAiSignInState = ReturnType<typeof useLinkAiSignIn>

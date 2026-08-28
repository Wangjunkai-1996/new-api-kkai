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
import { useEffect, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { register } from '@/features/auth/api'
import { useAuthRedirect } from '@/features/auth/hooks/use-auth-redirect'
import { useEmailVerification } from '@/features/auth/hooks/use-email-verification'
import { useOAuthLogin } from '@/features/auth/hooks/use-oauth-login'
import { useTurnstile } from '@/features/auth/hooks/use-turnstile'
import {
  getAffiliateCode,
  saveAffiliateCode,
} from '@/features/auth/lib/storage'
import { useStatus } from '@/hooks/use-status'

export type LinkAiSignUpFields = {
  email: string
  password: string
}

export function useLinkAiSignUp() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const oauth = useOAuthLogin(status)
  const { redirectToLogin } = useAuthRedirect()
  const [isLoading, setIsLoading] = useState(false)
  const [agreedToLegal, setAgreedToLegal] = useState(false)
  const [verificationCode, setVerificationCode] = useState('')
  const [turnstileWidgetKey, setTurnstileWidgetKey] = useState(0)
  const turnstile = useTurnstile()
  const emailVerification = useEmailVerification({
    turnstileToken: turnstile.turnstileToken,
    validateTurnstile: turnstile.validateTurnstile,
  })
  const form = useForm<LinkAiSignUpFields>({
    defaultValues: { email: '', password: '' },
  })
  const email = useWatch({ control: form.control, name: 'email' })
  const emailVerificationRequired = Boolean(status?.email_verification)
  const passwordRegistrationEnabled =
    (status?.password_register_enabled ??
      status?.data?.password_register_enabled ??
      true) !== false
  const oauthRegistrationEnabled =
    (status?.oauth_register_enabled ??
      status?.data?.oauth_register_enabled ??
      true) !== false
  const requiresLegalConsent = Boolean(
    status?.user_agreement_enabled || status?.privacy_policy_enabled
  )

  useEffect(() => {
    const affiliateCode = new URLSearchParams(window.location.search)
      .get('aff')
      ?.trim()
    if (affiliateCode) saveAffiliateCode(affiliateCode)
  }, [])

  const resetTurnstile = () => {
    if (!turnstile.isTurnstileEnabled) return
    turnstile.setTurnstileToken('')
    setTurnstileWidgetKey((current) => current + 1)
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

  async function handleSendVerificationCode() {
    const sent = await emailVerification.sendCode(email)
    if (sent) resetTurnstile()
  }

  async function onSubmit(data: LinkAiSignUpFields) {
    if (requiresLegalConsent && !agreedToLegal) {
      toast.error(t('Please agree to the Terms of Service and Privacy Policy'))
      return
    }
    if (!passwordRegistrationEnabled || status?.register_enabled === false) {
      toast.info(t('Email registration is not available'))
      return
    }
    if (emailVerificationRequired && !verificationCode.trim()) {
      toast.error(t('Please enter the verification code'))
      return
    }
    if (!turnstile.validateTurnstile()) return

    const submittedTurnstileToken = turnstile.turnstileToken
    resetTurnstile()
    setIsLoading(true)
    try {
      const response = await register({
        username: data.email,
        email: data.email,
        password: data.password,
        verification_code: verificationCode || undefined,
        aff_code: getAffiliateCode(),
        turnstile: submittedTurnstileToken,
      })
      if (response?.success) {
        toast.success(t('Account created! Please sign in'))
        redirectToLogin()
      } else {
        toast.error(response?.message || t('Failed to create account'))
      }
    } finally {
      setIsLoading(false)
    }
  }

  return {
    agreedToLegal,
    email,
    emailVerification,
    emailVerificationRequired,
    form,
    handleSendVerificationCode,
    isLoading,
    oauth,
    oauthRegistrationEnabled,
    onSubmit,
    passwordRegistrationEnabled,
    requireConfigured,
    requiresLegalConsent,
    setAgreedToLegal,
    setVerificationCode,
    status,
    turnstile,
    turnstileWidgetKey,
    verificationCode,
  }
}

export type LinkAiSignUpState = ReturnType<typeof useLinkAiSignUp>

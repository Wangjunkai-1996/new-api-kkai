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
import { Link } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { z } from 'zod'

import { Turnstile } from '@/components/turnstile'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form'
import { sendPasswordResetEmail } from '@/features/auth/api'
import {
  forgotPasswordFormSchema,
  PASSWORD_RESET_COUNTDOWN,
} from '@/features/auth/constants'
import { useTurnstile } from '@/features/auth/hooks/use-turnstile'
import { useCountdown } from '@/hooks/use-countdown'

import { LinkAiAuthShell } from '../components/auth-shell'

type ForgotPasswordFields = z.infer<typeof forgotPasswordFormSchema>

export function LinkAiForgotPasswordPage() {
  const { t } = useTranslation()
  const [isLoading, setIsLoading] = useState(false)
  const [turnstileWidgetKey, setTurnstileWidgetKey] = useState(0)
  const {
    isTurnstileEnabled,
    turnstileSiteKey,
    turnstileToken,
    setTurnstileToken,
    validateTurnstile,
  } = useTurnstile()
  const {
    secondsLeft,
    isActive,
    start: startCountdown,
  } = useCountdown({ initialSeconds: PASSWORD_RESET_COUNTDOWN })

  const form = useForm<ForgotPasswordFields>({
    resolver: zodResolver(forgotPasswordFormSchema),
    defaultValues: { email: '' },
  })
  const turnstileReady = !isTurnstileEnabled || Boolean(turnstileToken)

  async function onSubmit(data: ForgotPasswordFields) {
    if (!validateTurnstile()) return

    const submittedTurnstileToken = turnstileToken
    if (isTurnstileEnabled) {
      setTurnstileToken('')
      setTurnstileWidgetKey((current) => current + 1)
    }

    setIsLoading(true)
    try {
      const response = await sendPasswordResetEmail(
        data.email,
        submittedTurnstileToken
      )
      if (response?.success) {
        form.reset()
        startCountdown()
        toast.success(t('Reset email sent, please check your inbox'))
      } else {
        toast.error(response?.message || t('Failed to send reset email'))
      }
    } catch {
      // Errors are handled by the shared API interceptor.
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <LinkAiAuthShell>
      <div className='mx-auto block w-full max-w-[555px] px-5 py-12 sm:px-0'>
        <div className='mx-auto flex h-[38px] w-[115px] items-center justify-center rounded-full border border-[#2a2a2a] bg-[#1a1a1a] px-4 text-base tracking-[0.2em] text-[#9b9b9b]'>
          {t('Reset password')}
        </div>

        <h1 className='mt-[18px] text-center text-[30px] leading-[43px] font-semibold text-[#592fe0] sm:text-[36px]'>
          {t('Forgot password')}
        </h1>
        <p className='mx-auto mt-[11px] max-w-[500px] text-center text-sm leading-[22px] text-[#9b9b9b] sm:text-lg'>
          {t(
            'Enter your registered email and we will send you a link to reset your password.'
          )}
        </p>

        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className='mt-14'>
            <FormField
              control={form.control}
              name='email'
              render={({ field }) => (
                <FormItem className='gap-[6px]'>
                  <label
                    className='text-lg leading-[22px] text-white'
                    htmlFor='linkai-reset-email'
                  >
                    {t('Enter email address')}
                  </label>
                  <FormControl>
                    <input
                      id='linkai-reset-email'
                      type='email'
                      autoComplete='email'
                      placeholder={t('Enter email address')}
                      className='h-[50px] w-full rounded-full border border-[#2a2a2a] bg-[#1a1a1a] px-[26px] text-lg text-white transition outline-none placeholder:text-[#9b9b9b] focus:border-[#7258ce] focus:ring-2 focus:ring-[#7258ce]/20'
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            {isTurnstileEnabled && (
              <div className='mt-5 flex justify-center'>
                <Turnstile
                  key={turnstileWidgetKey}
                  siteKey={turnstileSiteKey}
                  onVerify={setTurnstileToken}
                  onExpire={() => setTurnstileToken('')}
                />
              </div>
            )}

            <button
              type='submit'
              disabled={isLoading || isActive || !turnstileReady}
              className='mt-6 flex h-[49px] w-full items-center justify-center gap-2 rounded-full bg-[#e5e5e5] text-lg leading-[22px] font-bold text-black transition hover:bg-white focus-visible:ring-2 focus-visible:ring-[#7258ce] focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50'
            >
              {isLoading && <Loader2 className='h-4 w-4 animate-spin' />}
              {isActive
                ? t('Resend ({{seconds}}s)', { seconds: secondsLeft })
                : t('Send reset email')}
            </button>
          </form>
        </Form>

        <div className='mt-8 flex flex-col items-center justify-center gap-3 text-sm leading-[22px] text-[#9b9b9b] sm:flex-row sm:text-lg'>
          <Link
            to='/sign-in'
            className='text-[#eeeeee] transition hover:text-white hover:underline'
          >
            {t('Sign in to your account')}
          </Link>
          <span className='hidden text-[#4a4a4a] sm:inline'>•</span>
          <Link
            to='/sign-up'
            className='text-[#eeeeee] transition hover:text-white hover:underline'
          >
            {t('Register account')}
          </Link>
        </div>
      </div>
    </LinkAiAuthShell>
  )
}

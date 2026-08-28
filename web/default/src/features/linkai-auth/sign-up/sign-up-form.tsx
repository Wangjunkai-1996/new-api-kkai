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
import { Link } from '@tanstack/react-router'
import { Check, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Turnstile } from '@/components/turnstile'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form'

import type { LinkAiSignUpState } from './use-linkai-sign-up'

type LinkAiSignUpFormProps = {
  state: LinkAiSignUpState
}

export function LinkAiSignUpForm({ state }: LinkAiSignUpFormProps) {
  const { t } = useTranslation()
  const {
    agreedToLegal,
    email,
    emailVerification,
    emailVerificationRequired,
    form,
    handleSendVerificationCode,
    isLoading,
    onSubmit,
    passwordRegistrationEnabled,
    requiresLegalConsent,
    setAgreedToLegal,
    setVerificationCode,
    status,
    turnstile,
    turnstileWidgetKey,
    verificationCode,
  } = state
  const registrationEnabled =
    passwordRegistrationEnabled && status?.register_enabled !== false

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-[22px]'>
        <FormField
          control={form.control}
          name='email'
          rules={{
            required: t('Please enter your email'),
            pattern: {
              value: /^[^\s@]+@[^\s@]+\.[^\s@]+$/,
              message: t('Please enter a valid email address'),
            },
          }}
          render={({ field }) => (
            <FormItem className='gap-[6px]'>
              <label
                className='text-lg leading-[22px] text-white'
                htmlFor='linkai-register-email'
              >
                {t('Enter email address')}
              </label>
              <FormControl>
                <input
                  id='linkai-register-email'
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

        <FormField
          control={form.control}
          name='password'
          rules={{
            required: t('Please enter your password'),
            minLength: {
              value: 8,
              message: t('Password must be between 8 and 20 characters'),
            },
            maxLength: {
              value: 20,
              message: t('Password must be at most 20 characters long'),
            },
          }}
          render={({ field }) => (
            <FormItem className='gap-[6px]'>
              <label
                className='text-lg leading-[22px] text-white'
                htmlFor='linkai-register-password'
              >
                {t('Enter login password')}
              </label>
              <FormControl>
                <input
                  id='linkai-register-password'
                  type='password'
                  autoComplete='new-password'
                  placeholder={t('Enter password (8-20 characters)')}
                  className='h-[50px] w-full rounded-full border border-[#2a2a2a] bg-[#1a1a1a] px-[26px] text-lg text-white transition outline-none placeholder:text-[#9b9b9b] focus:border-[#7258ce] focus:ring-2 focus:ring-[#7258ce]/20'
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {emailVerificationRequired && (
          <div className='flex gap-2'>
            <input
              value={verificationCode}
              onChange={(event) => setVerificationCode(event.target.value)}
              placeholder={t('Verification code')}
              className='h-[50px] min-w-0 flex-1 rounded-full border border-[#2a2a2a] bg-[#1a1a1a] px-[26px] text-sm text-white outline-none focus:border-[#7258ce]'
            />
            <button
              type='button'
              disabled={
                emailVerification.isSending ||
                emailVerification.isActive ||
                !email
              }
              onClick={() => void handleSendVerificationCode()}
              className='rounded-full border border-[#37373d] px-5 text-sm text-white disabled:opacity-50'
            >
              {emailVerification.isActive
                ? t('Resend ({{seconds}}s)', {
                    seconds: emailVerification.secondsLeft,
                  })
                : t('Send code')}
            </button>
          </div>
        )}

        {turnstile.isTurnstileEnabled && (
          <Turnstile
            key={turnstileWidgetKey}
            siteKey={turnstile.turnstileSiteKey}
            onVerify={turnstile.setTurnstileToken}
            onExpire={() => turnstile.setTurnstileToken('')}
          />
        )}

        {requiresLegalConsent && (
          <label className='flex cursor-pointer items-center gap-[10px] px-4 text-sm text-[#9b9b9b] sm:text-base'>
            <input
              type='checkbox'
              checked={agreedToLegal}
              onChange={(event) => setAgreedToLegal(event.target.checked)}
              className='peer sr-only'
            />
            <span className='flex h-[19px] w-[19px] shrink-0 items-center justify-center rounded-[4px] border border-[#eeeeee] text-black peer-checked:bg-white'>
              {agreedToLegal && <Check className='h-4 w-4' strokeWidth={3} />}
            </span>
            <span>
              {t('I agree to the')}{' '}
              <Link
                to='/user-agreement'
                className='text-[#eeeeee] hover:underline'
              >
                {t('Terms of Service')}
              </Link>{' '}
              {t('and')}{' '}
              <Link
                to='/privacy-policy'
                className='text-[#eeeeee] hover:underline'
              >
                {t('Privacy Policy')}
              </Link>
            </span>
          </label>
        )}

        <button
          type='submit'
          disabled={
            isLoading ||
            !registrationEnabled ||
            (requiresLegalConsent && !agreedToLegal)
          }
          className='flex h-[49px] w-full items-center justify-center gap-2 rounded-full bg-[#e5e5e5] text-lg font-bold text-black transition hover:bg-white disabled:cursor-not-allowed disabled:opacity-50'
        >
          {isLoading && <Loader2 className='h-4 w-4 animate-spin' />}
          {t('Register with email')}
        </button>
      </form>
    </Form>
  )
}

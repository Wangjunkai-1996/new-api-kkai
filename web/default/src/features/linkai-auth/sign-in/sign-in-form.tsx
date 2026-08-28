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
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Turnstile } from '@/components/turnstile'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form'

import { LinkAiLegalConsent } from '../components/legal-consent'
import type { LinkAiSignInState } from './use-linkai-sign-in'

type LinkAiSignInFormProps = {
  state: LinkAiSignInState
}

export function LinkAiSignInForm({ state }: LinkAiSignInFormProps) {
  const { t } = useTranslation()
  const {
    agreedToLegal,
    form,
    handlePasskeyLogin,
    isLoading,
    isPasskeyLoading,
    onSubmit,
    requiresLegalConsent,
    setAgreedToLegal,
    status,
    turnstile,
    turnstileWidgetKey,
  } = state

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)}>
        <FormField
          control={form.control}
          name='username'
          render={({ field }) => (
            <FormItem className='gap-[6px]'>
              <label
                className='text-base leading-5 text-white'
                htmlFor='linkai-email'
              >
                {t('Enter email address')}
              </label>
              <FormControl>
                <input
                  id='linkai-email'
                  autoComplete='username'
                  placeholder={t('Enter email address')}
                  className='h-[46px] w-full rounded-full border border-[#2a2a2a] bg-[#1a1a1a] px-6 text-base text-white transition outline-none placeholder:text-[#9b9b9b] focus:border-[#7258ce] focus:ring-2 focus:ring-[#7258ce]/20'
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
          render={({ field }) => (
            <FormItem className='mt-6 gap-[6px]'>
              <label
                className='block text-base leading-5 text-white'
                htmlFor='linkai-password'
              >
                {t('Enter login password')}
              </label>
              <FormControl>
                <input
                  id='linkai-password'
                  type='password'
                  autoComplete='current-password'
                  placeholder={t('Enter login password')}
                  className='h-[46px] w-full rounded-full border border-[#2a2a2a] bg-[#1a1a1a] px-6 text-base text-white transition outline-none placeholder:text-[#9b9b9b] focus:border-[#7258ce] focus:ring-2 focus:ring-[#7258ce]/20'
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {turnstile.isTurnstileEnabled && (
          <div className='mt-5'>
            <Turnstile
              key={turnstileWidgetKey}
              siteKey={turnstile.turnstileSiteKey}
              onVerify={turnstile.setTurnstileToken}
              onExpire={() => turnstile.setTurnstileToken('')}
            />
          </div>
        )}

        {requiresLegalConsent && (
          <LinkAiLegalConsent
            id='linkai-sign-in-legal-consent'
            status={status}
            checked={agreedToLegal}
            onCheckedChange={setAgreedToLegal}
            className='mt-4'
          />
        )}

        <button
          type='submit'
          disabled={isLoading || (requiresLegalConsent && !agreedToLegal)}
          className='mt-5 flex h-[46px] w-full items-center justify-center gap-2 rounded-full bg-[#e5e5e5] text-base leading-5 font-bold text-black transition hover:bg-white disabled:cursor-not-allowed disabled:opacity-50'
        >
          {isLoading && <Loader2 className='h-4 w-4 animate-spin' />}
          {t('Sign in')}
        </button>

        <button
          type='button'
          onClick={handlePasskeyLogin}
          className='mt-4 flex h-[46px] w-full items-center justify-center rounded-full border border-[#2a2a2a] bg-[#1a1a1a] text-base text-white transition hover:border-[#7258ce] hover:bg-[#211a31] disabled:cursor-not-allowed disabled:opacity-50'
          disabled={isPasskeyLoading}
        >
          {isPasskeyLoading && (
            <Loader2 className='mr-2 h-4 w-4 animate-spin' />
          )}
          {t('Sign in with Passkey')}
        </button>
      </form>
    </Form>
  )
}

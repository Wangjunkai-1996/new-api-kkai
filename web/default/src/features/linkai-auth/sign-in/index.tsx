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
import { useTranslation } from 'react-i18next'

import { AUTH_ASSET_ROOT, LinkAiAuthShell } from '../components/auth-shell'
import { LinkAiSignInForm } from './sign-in-form'
import { useLinkAiSignIn } from './use-linkai-sign-in'

export function LinkAiSignInPage() {
  const { t } = useTranslation()
  const state = useLinkAiSignIn()

  return (
    <LinkAiAuthShell>
      <div className='mx-auto block h-full w-full max-w-[555px] [scrollbar-width:none] overflow-y-auto px-5 pt-12 sm:px-0 [&::-webkit-scrollbar]:hidden'>
        <div className='mx-auto flex h-[38px] w-[115px] items-center justify-center rounded-[17px] border border-[#2a2a2a] bg-[#1a1a1a] px-4 text-base tracking-[0.2em] text-[#9b9b9b]'>
          {t('User sign in')}
        </div>
        <h1 className='mt-[18px] text-center text-[30px] leading-[43px] font-semibold text-[#592fe0] sm:text-[36px]'>
          {t('Access your account')}
        </h1>
        <p className='mt-[11px] text-center text-sm leading-[22px] text-[#9b9b9b] sm:text-lg'>
          {t('Choose a social account or continue with email and password')}
        </p>

        <div className='mt-[38px] grid grid-cols-1 gap-3 sm:grid-cols-2'>
          <button
            type='button'
            onClick={() =>
              state.requireConfigured(
                Boolean(state.status?.github_oauth),
                state.oauth.handleGitHubLogin
              )
            }
            className='flex h-[50px] items-center justify-center gap-3 rounded-full border border-[#2a2a2a] bg-[#1a1a1a] text-lg text-white transition hover:-translate-y-0.5 hover:border-[#7258ce] hover:bg-[#211a31] focus-visible:ring-2 focus-visible:ring-[#7258ce] focus-visible:outline-none'
          >
            <img
              src={`${AUTH_ASSET_ROOT}/raw-07.png`}
              alt=''
              className='h-5 w-[18px] object-contain'
              aria-hidden='true'
            />
            {t('GitHub')}
          </button>
          <button
            type='button'
            onClick={() =>
              state.requireConfigured(
                Boolean(state.status?.oidc_enabled),
                state.oauth.handleOIDCLogin
              )
            }
            className='flex h-[50px] items-center justify-center gap-3 rounded-full border border-[#2a2a2a] bg-[#1a1a1a] text-lg text-white transition hover:-translate-y-0.5 hover:border-[#7258ce] hover:bg-[#211a31] focus-visible:ring-2 focus-visible:ring-[#7258ce] focus-visible:outline-none'
          >
            <img
              src={`${AUTH_ASSET_ROOT}/raw-09.png`}
              alt=''
              className='h-[22px] w-[22px] object-contain'
              aria-hidden='true'
            />
            {t('Google')}
          </button>
        </div>

        <div className='mt-12 mb-[23px] flex h-[23px] items-center gap-[18px] text-[15px] text-[#9b9b9b]'>
          <span className='h-[2px] flex-1 bg-[#2c2c2c]' />
          <span>{t('Or')}</span>
          <span className='h-[2px] flex-1 bg-[#2c2c2c]' />
        </div>

        <LinkAiSignInForm state={state} />

        <div className='mt-[23px] flex flex-col gap-3 px-1 text-sm leading-[22px] text-[#9b9b9b] sm:flex-row sm:items-center sm:justify-between sm:text-lg'>
          <p>
            {t("Don't have an account?")}{' '}
            <Link
              to='/sign-up'
              className='text-[#eeeeee] transition hover:text-white'
            >
              {t('Register account')}
            </Link>
          </p>
          <p>
            {t('Forgot password?')}{' '}
            <Link
              to='/forgot-password'
              className='text-[#eeeeee] underline underline-offset-2 transition hover:text-white'
            >
              {t('Reset password')}
            </Link>
          </p>
        </div>
      </div>
    </LinkAiAuthShell>
  )
}

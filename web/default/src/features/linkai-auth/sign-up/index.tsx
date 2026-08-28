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

import { LinkAiAuthShell } from '../components/auth-shell'
import { LinkAiSignUpForm } from './sign-up-form'
import { useLinkAiSignUp } from './use-linkai-sign-up'

const SIGN_UP_ASSET_ROOT = '/figma/linkai-auth/sign-up'

export function LinkAiSignUpPage() {
  const { t } = useTranslation()
  const state = useLinkAiSignUp()

  return (
    <LinkAiAuthShell
      assetRoot={SIGN_UP_ASSET_ROOT}
      backgroundFile='raw-01.png'
      backgroundOverlayFile='raw-06.png'
      panelFile='raw-05.png'
      splitLogo
    >
      <div className='mx-auto block h-full w-full max-w-[555px] [scrollbar-width:none] overflow-y-auto px-5 pt-12 sm:px-0 [&::-webkit-scrollbar]:hidden'>
        <div className='mx-auto flex h-[38px] w-[115px] items-center justify-center rounded-full border border-[#2a2a2a] bg-[#1a1a1a] px-4 text-base tracking-[0.2em] text-[#9b9b9b]'>
          {t('Register account')}
        </div>
        <h1 className='mt-[18px] text-center text-[30px] leading-[43px] font-semibold tracking-[-0.02em] text-[#592fe0] sm:text-[36px]'>
          {t('Create your account')}
        </h1>
        <p className='mt-[11px] text-center text-sm leading-[22px] text-[#9b9b9b] sm:text-lg'>
          {t('Choose a social account or register with email')}
        </p>

        <div className='mt-[38px] grid grid-cols-1 gap-3 sm:grid-cols-2'>
          <button
            type='button'
            onClick={() =>
              state.requireConfigured(
                state.oauthRegistrationEnabled &&
                  Boolean(state.status?.github_oauth),
                state.oauth.handleGitHubLogin
              )
            }
            className='flex h-[50px] items-center justify-center gap-3 rounded-full border border-[#2a2a2a] bg-[#1a1a1a] text-lg text-white transition hover:-translate-y-0.5 hover:border-[#7258ce] hover:bg-[#211a31] focus-visible:ring-2 focus-visible:ring-[#7258ce] focus-visible:outline-none'
          >
            <img
              src={`${SIGN_UP_ASSET_ROOT}/raw-09.png`}
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
                state.oauthRegistrationEnabled &&
                  Boolean(state.status?.oidc_enabled),
                state.oauth.handleOIDCLogin
              )
            }
            className='flex h-[50px] items-center justify-center gap-3 rounded-full border border-[#2a2a2a] bg-[#1a1a1a] text-lg text-white transition hover:-translate-y-0.5 hover:border-[#7258ce] hover:bg-[#211a31] focus-visible:ring-2 focus-visible:ring-[#7258ce] focus-visible:outline-none'
          >
            <img
              src={`${SIGN_UP_ASSET_ROOT}/raw-10.png`}
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

        <LinkAiSignUpForm state={state} />

        <p className='mt-8 text-center text-sm text-[#9b9b9b] sm:text-lg'>
          {t('Already have an account?')}{' '}
          <Link
            to='/sign-in'
            className='text-[#eeeeee] transition hover:text-white'
          >
            {t('Sign in to your account')}
          </Link>
        </p>
      </div>
    </LinkAiAuthShell>
  )
}

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
import { Link, useSearch } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { useSiteConfig } from '@/features/site-config'
import { useStatus } from '@/hooks/use-status'

import { AuthLayout } from '../auth-layout'
import { TermsFooter } from '../components/terms-footer'
import { UserAuthForm } from './components/user-auth-form'

export function SignIn() {
  const { t } = useTranslation()
  const { redirect, owner_login } = useSearch({ from: '/(auth)/sign-in' })
  const { status } = useStatus()
  const site = useSiteConfig()
  const isTenant = (site.data?.tenant_id ?? 0) > 0
  const ownerLogin = isTenant && owner_login === true

  return (
    <AuthLayout>
      <div className='w-full space-y-8'>
        <div className='space-y-2'>
          <h2 className='text-center text-2xl font-semibold tracking-tight sm:text-left'>
            {ownerLogin ? t('Reseller management sign in') : t('Sign in')}
          </h2>
          {ownerLogin && (
            <p className='text-muted-foreground text-sm'>
              {t(
                'Use your KKAI main-site account to manage this reseller site.'
              )}
            </p>
          )}
          {!ownerLogin &&
            !status?.self_use_mode_enabled &&
            status?.register_enabled !== false && (
              <p className='text-muted-foreground text-left text-sm sm:text-base'>
                {t("Don't have an account?")}{' '}
                <Link
                  to='/sign-up'
                  className='hover:text-primary font-medium underline underline-offset-4'
                >
                  {t('Sign up')}
                </Link>
                .
              </p>
            )}
          {isTenant && (
            <Link
              to='/sign-in'
              search={
                ownerLogin
                  ? { owner_login: false, redirect: undefined }
                  : { owner_login: true, redirect: '/reseller' }
              }
              className='text-primary inline-block text-sm font-medium underline underline-offset-4'
            >
              {ownerLogin
                ? t('Customer sign in')
                : t('Reseller management sign in')}
            </Link>
          )}
        </div>

        <UserAuthForm
          key={ownerLogin ? 'owner' : 'customer'}
          ownerLogin={ownerLogin}
          redirectTo={ownerLogin ? redirect || '/reseller' : redirect}
        />

        <TermsFooter
          variant='sign-in'
          status={status}
          className='text-center'
        />
      </div>
    </AuthLayout>
  )
}

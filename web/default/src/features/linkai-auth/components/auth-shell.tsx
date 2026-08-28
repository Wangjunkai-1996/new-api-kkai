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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

type LinkAiAuthShellProps = {
  children: ReactNode
  panelClassName?: string
  assetRoot?: string
  backgroundFile?: string
  backgroundOverlayFile?: string
  panelFile?: string
  splitLogo?: boolean
}

const AUTH_ASSET_ROOT = '/figma/linkai-auth/sign-in'

export function LinkAiAuthShell({
  children,
  panelClassName = '',
  assetRoot = AUTH_ASSET_ROOT,
  backgroundFile = 'raw-01.png',
  backgroundOverlayFile = 'raw-02.png',
  panelFile = 'raw-04.png',
  splitLogo = false,
}: LinkAiAuthShellProps) {
  const { t } = useTranslation()

  return (
    <main className='relative min-h-svh overflow-x-hidden bg-black text-white'>
      <div className='pointer-events-none absolute inset-0 min-h-[720px] overflow-hidden'>
        <img
          src={`${assetRoot}/${backgroundFile}`}
          alt=''
          className='h-full w-full object-cover object-center'
          aria-hidden='true'
        />
        <img
          src={`${assetRoot}/${backgroundOverlayFile}`}
          alt=''
          className='absolute inset-0 h-full w-full object-cover object-center opacity-[0.14]'
          aria-hidden='true'
        />
      </div>

      <div
        data-linkai-auth-logo
        className='absolute top-6 left-6 z-20 sm:top-8 sm:left-8 xl:top-10 xl:left-6 2xl:left-[88px]'
      >
        <Link
          to='/'
          className='block w-fit transition-opacity hover:opacity-80 focus-visible:ring-2 focus-visible:ring-violet-400 focus-visible:outline-none'
          aria-label={t('Back to home')}
        >
          {splitLogo ? (
            <span className='flex items-center gap-2.5'>
              <img
                src={`${assetRoot}/raw-02.png`}
                alt=''
                className='h-8 w-auto'
                aria-hidden='true'
              />
              <img
                src={`${assetRoot}/raw-07.png`}
                alt={t('LinkAI')}
                className='h-[30px] w-auto'
              />
            </span>
          ) : (
            <img
              src={`${assetRoot}/raw-06.png`}
              alt={t('LinkAI')}
              className='h-8 w-auto'
            />
          )}
        </Link>
      </div>

      <section
        className={`relative z-10 mx-auto flex min-h-svh w-[calc(100%-32px)] max-w-[600px] items-center py-16 sm:py-20 xl:py-[30px] ${panelClassName}`}
      >
        <div
          data-linkai-auth-panel
          className='relative isolate w-full min-w-0 overflow-hidden rounded-[20px] bg-black'
        >
          <img
            src={`${assetRoot}/${panelFile}`}
            alt=''
            className='pointer-events-none absolute inset-0 -z-10 h-full w-full object-fill'
            aria-hidden='true'
          />
          {children}
        </div>
      </section>
    </main>
  )
}

export { AUTH_ASSET_ROOT }

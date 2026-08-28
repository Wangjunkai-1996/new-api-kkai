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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { LanguageSwitcher } from '@/components/language-switcher'
import { NotificationPopover } from '@/components/notification-popover'
import { useNotifications } from '@/hooks/use-notifications'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { LINKAI_HOME_ASSETS } from './assets'
import {
  LINKAI_HEADER_DESKTOP_ACTIONS_CLASS,
  LINKAI_HEADER_EXPANDED_CONTAINER_CLASS,
  LINKAI_HEADER_EXPANDED_LOGO_CLASS,
  LINKAI_HEADER_EXPANDED_ROW_CLASS,
} from './header-geometry'
import {
  LinkAiDesktopNavigation,
  type LinkAiHeaderLink,
  LinkAiMobileNavigation,
} from './landing-navigation'

export function LinkAiLandingHeader() {
  const { t } = useTranslation()
  const [scrolled, setScrolled] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const user = useAuthStore((state) => state.auth.user)
  const notifications = useNotifications()

  const links: LinkAiHeaderLink[] = [
    { href: '/', label: t('Home') },
    { href: '/pricing', label: t('Model Square') },
    { href: '/rankings', label: t('Rankings') },
    { href: '/docs', label: t('Docs') },
    { href: '/about', label: t('About') },
  ]

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 20)
    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  useEffect(() => {
    if (!mobileOpen) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMobileOpen(false)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [mobileOpen])

  return (
    <header
      data-scrolled={scrolled}
      className='pointer-events-none fixed inset-x-0 top-0 z-50'
    >
      <div
        className={cn(
          'pointer-events-auto mx-auto transition-all duration-700 ease-[cubic-bezier(0.16,1,0.3,1)] motion-reduce:transition-none',
          scrolled
            ? 'max-w-[1200px] px-3 pt-3'
            : LINKAI_HEADER_EXPANDED_CONTAINER_CLASS
        )}
      >
        <div
          className={cn(
            'grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-5 border transition-all duration-700 ease-[cubic-bezier(0.16,1,0.3,1)] motion-reduce:transition-none',
            scrolled
              ? 'min-h-16 rounded-2xl border-white/12 bg-black/70 px-4 py-2 shadow-[0_12px_40px_-18px_rgba(0,0,0,0.9)] backdrop-blur-2xl'
              : LINKAI_HEADER_EXPANDED_ROW_CLASS
          )}
        >
          <Link
            to='/'
            className='col-start-1 row-start-1 flex shrink-0 items-center rounded-md focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white'
            aria-label={t('Home')}
          >
            <img
              src={LINKAI_HOME_ASSETS.brandLogoHeader}
              alt='LinkAI'
              width={242}
              height={40}
              className={cn(
                'h-auto transition-all duration-700 ease-[cubic-bezier(0.16,1,0.3,1)] motion-reduce:transition-none',
                scrolled
                  ? 'w-[130px] sm:w-[150px] lg:w-[170px]'
                  : LINKAI_HEADER_EXPANDED_LOGO_CLASS
              )}
            />
          </Link>

          <LinkAiDesktopNavigation links={links} scrolled={scrolled} />

          <div className={LINKAI_HEADER_DESKTOP_ACTIONS_CLASS}>
            <div className='[&_button]:text-white [&_button:hover]:bg-white/10'>
              <LanguageSwitcher />
            </div>
            <NotificationPopover
              open={notifications.popoverOpen}
              onOpenChange={notifications.setPopoverOpen}
              unreadCount={notifications.unreadCount}
              activeTab={notifications.activeTab}
              onTabChange={notifications.setActiveTab}
              notice={notifications.notice}
              announcements={notifications.announcements}
              loading={notifications.loading}
              className='text-white hover:bg-white/10 hover:text-white'
            />
            {user ? (
              <Link
                to='/dashboard'
                className={cn(
                  'ml-2 rounded-lg bg-white text-sm font-semibold text-black transition hover:bg-white/85 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white',
                  scrolled ? 'px-4 py-2' : 'px-5 py-2.5'
                )}
              >
                {t('Go to Dashboard')}
              </Link>
            ) : (
              <>
                <Link
                  to='/sign-in'
                  className={cn(
                    'ml-2 rounded-lg border border-white/20 bg-[#0f0f0f] text-sm text-white transition hover:border-white/45 hover:bg-white/10 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white',
                    scrolled ? 'px-5 py-2' : 'px-8 py-2.5'
                  )}
                >
                  {t('Sign in')}
                </Link>
                <Link
                  to='/sign-up'
                  className={cn(
                    'rounded-lg bg-white text-sm font-semibold text-black transition hover:bg-white/85 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white',
                    scrolled ? 'px-5 py-2' : 'px-8 py-2.5'
                  )}
                >
                  {t('Sign up')}
                </Link>
              </>
            )}
          </div>

          <button
            type='button'
            className='relative col-start-3 row-start-1 size-11 justify-self-end rounded-full border border-white/25 bg-black/35 backdrop-blur-md xl:hidden'
            aria-expanded={mobileOpen}
            aria-controls='linkai-mobile-navigation'
            aria-label={t('Toggle navigation menu')}
            onClick={() => setMobileOpen((open) => !open)}
          >
            <span
              className={cn(
                'absolute left-3 top-[14px] h-px w-5 bg-white transition-transform',
                mobileOpen && 'translate-y-[6px] rotate-45'
              )}
            />
            <span
              className={cn(
                'absolute left-3 top-5 h-px w-5 bg-white transition-opacity',
                mobileOpen && 'opacity-0'
              )}
            />
            <span
              className={cn(
                'absolute left-3 top-[26px] h-px w-5 bg-white transition-transform',
                mobileOpen && '-translate-y-[6px] -rotate-45'
              )}
            />
          </button>
        </div>

        <LinkAiMobileNavigation
          links={links}
          open={mobileOpen}
          authenticated={Boolean(user)}
          onNavigate={() => setMobileOpen(false)}
        />
      </div>
    </header>
  )
}

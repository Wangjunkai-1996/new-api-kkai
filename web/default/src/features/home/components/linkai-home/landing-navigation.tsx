/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

For commercial licensing, please contact support@quantumnous.com
*/
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { LINKAI_HEADER_EXPANDED_NAV_CLASS } from './header-geometry'

export type LinkAiHeaderLink = {
  href: string
  label: string
  external?: boolean
}

type NavigationProps = {
  links: LinkAiHeaderLink[]
  scrolled: boolean
}

export function LinkAiDesktopNavigation({ links, scrolled }: NavigationProps) {
  const { t } = useTranslation()

  return (
    <nav
      aria-label={t('Primary navigation')}
      className={cn(
        'col-start-2 row-start-1 hidden items-center justify-center justify-self-end gap-1 transition-all duration-700 ease-[cubic-bezier(0.16,1,0.3,1)] motion-reduce:transition-none xl:flex',
        scrolled
          ? 'mr-[clamp(170px,calc(230px-3.125vw),185px)] h-11 w-auto max-w-none rounded-xl border border-white/10 bg-white/[0.04] px-2'
          : LINKAI_HEADER_EXPANDED_NAV_CLASS
      )}
    >
      {links.map((link) => (
        <LinkAiNavigationItem key={link.label} link={link} desktop />
      ))}
    </nav>
  )
}

type MobileNavigationProps = {
  links: LinkAiHeaderLink[]
  open: boolean
  authenticated: boolean
  onNavigate: () => void
}

export function LinkAiMobileNavigation({
  links,
  open,
  authenticated,
  onNavigate,
}: MobileNavigationProps) {
  const { t } = useTranslation()

  return (
    <div
      id='linkai-mobile-navigation'
      inert={!open}
      aria-hidden={!open}
      className={cn(
        'mx-auto mt-3 overflow-hidden rounded-3xl border border-white/10 bg-black/90 px-5 transition-[max-height,opacity,padding] duration-300 motion-reduce:transition-none xl:hidden',
        open ? 'max-h-[36rem] py-5 opacity-100' : 'max-h-0 py-0 opacity-0'
      )}
    >
      <nav className='flex flex-col' aria-label={t('Primary navigation')}>
        {links.map((link) => (
          <LinkAiNavigationItem
            key={link.label}
            link={link}
            onNavigate={onNavigate}
          />
        ))}
      </nav>
      <div className='mt-5 grid grid-cols-2 gap-3'>
        <Link
          to={authenticated ? '/dashboard' : '/sign-in'}
          className='rounded-xl border border-white/20 px-4 py-3 text-center text-sm text-white'
          onClick={onNavigate}
        >
          {authenticated ? t('Go to Dashboard') : t('Sign in')}
        </Link>
        <Link
          to={authenticated ? '/dashboard' : '/sign-up'}
          className='rounded-xl bg-white px-4 py-3 text-center text-sm font-semibold text-black'
          onClick={onNavigate}
        >
          {authenticated ? t('Console') : t('Sign up')}
        </Link>
      </div>
    </div>
  )
}

type NavigationItemProps = {
  link: LinkAiHeaderLink
  desktop?: boolean
  onNavigate?: () => void
}

function LinkAiNavigationItem({
  link,
  desktop = false,
  onNavigate,
}: NavigationItemProps) {
  const className = desktop
    ? 'rounded-full px-[clamp(0.55rem,0.85vw,1rem)] py-1.5 text-base text-white/55 transition-colors hover:text-white focus-visible:outline-2 focus-visible:outline-white'
    : 'whitespace-nowrap border-b border-white/10 py-3 text-base text-white/75 last:border-0'

  if (link.external) {
    return (
      <a
        href={link.href}
        target='_blank'
        rel='noopener noreferrer'
        className={className}
        onClick={onNavigate}
      >
        {link.label}
      </a>
    )
  }

  return (
    <Link
      to={link.href}
      className={cn(className, desktop && link.href === '/' && 'text-white')}
      onClick={onNavigate}
    >
      {link.label}
    </Link>
  )
}

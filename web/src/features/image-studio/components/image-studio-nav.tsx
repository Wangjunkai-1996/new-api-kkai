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
import { Link, useLocation } from '@tanstack/react-router'
import { FolderOpen, ImagePlus } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

const LINKS = [
  {
    to: '/image-studio/create',
    label: 'imageStudio.create',
    icon: ImagePlus,
  },
  {
    to: '/image-studio/library',
    label: 'imageStudio.library',
    icon: FolderOpen,
  },
] as const

type ImageStudioNavProps = { action?: React.ReactNode }

export function ImageStudioNav(props: ImageStudioNavProps) {
  const { t } = useTranslation()
  const pathname = useLocation({ select: (location) => location.pathname })
  return (
    <header className='bg-background flex min-h-12 shrink-0 items-center gap-3 border-b px-3 sm:px-4'>
      <h1 className='hidden shrink-0 text-sm font-semibold sm:block'>
        {t('imageStudio.title')}
      </h1>
      <nav
        className='flex min-w-0 flex-1 items-center'
        aria-label={t('imageStudio.title')}
      >
        {LINKS.map((link) => {
          const active = pathname === link.to
          return (
            <Link
              key={link.to}
              to={link.to}
              className={cn(
                'text-muted-foreground hover:text-foreground relative inline-flex h-12 items-center gap-1.5 px-2.5 text-xs font-medium transition-colors sm:px-3 sm:text-sm',
                active &&
                  'text-foreground after:bg-primary after:absolute after:inset-x-2 after:bottom-0 after:h-0.5'
              )}
              aria-current={active ? 'page' : undefined}
            >
              <link.icon className='size-3.5' aria-hidden='true' />
              {t(link.label)}
            </Link>
          )
        })}
      </nav>
      {props.action}
    </header>
  )
}

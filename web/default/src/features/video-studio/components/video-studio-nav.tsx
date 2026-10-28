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
import { Film, FolderOpen, ListVideo } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import '@/styles/video-studio.css'

const VIDEO_STUDIO_LINKS = [
  { to: '/video-studio/create', label: 'videoStudio.create', icon: Film },
  {
    to: '/video-studio/library',
    label: 'videoStudio.library',
    icon: FolderOpen,
  },
  { to: '/video-studio/tasks', label: 'videoStudio.tasks', icon: ListVideo },
] as const

type VideoStudioNavProps = { action?: React.ReactNode }

export function VideoStudioNav(props: VideoStudioNavProps) {
  const { t } = useTranslation()
  const pathname = useLocation({ select: (location) => location.pathname })
  return (
    <header className='video-studio-header'>
      <div className='video-studio-heading'>
        <div>
          <p className='video-studio-eyebrow'>VIDEO STUDIO</p>
          <h1>
            {t('videoStudio.title')}
            <span aria-hidden='true' />
          </h1>
        </div>
        {props.action}
      </div>
      <div className='video-studio-navigation'>
        <nav aria-label={t('videoStudio.title')}>
          {VIDEO_STUDIO_LINKS.map((link) => (
            <Link
              key={link.to}
              to={link.to}
              className={cn(
                'video-studio-nav-link',
                pathname === link.to && 'is-active'
              )}
              aria-current={pathname === link.to ? 'page' : undefined}
            >
              <link.icon aria-hidden='true' />
              {t(link.label)}
            </Link>
          ))}
        </nav>
        <span className='video-studio-navigation-note'>
          {t('From an idea to the next frame.')}
        </span>
      </div>
    </header>
  )
}

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
import { ArrowLeft01Icon, ArrowRight01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import headerArt from '@/assets/video-works/header.webp'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { useLogsViewScope } from '../components/usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

export function VideoWorksHeading(props: { onScopeChange: () => void }) {
  const { t } = useTranslation()
  const scope = useLogsViewScope()
  const navigate = route.useNavigate()
  return (
    <header className='video-works-heading'>
      <div>
        <h1>{t('Video Works')}</h1>
        <p>
          {t(
            'Your videos, generation status and billing details in one place.'
          )}
        </p>
      </div>
      <img className='video-works-header-art' src={headerArt} alt='' />
      {scope.canManageScope && (
        <Tabs
          value={scope.viewScope}
          onValueChange={(value) => {
            if (value !== 'all' && value !== 'self') return
            props.onScopeChange()
            scope.setViewScope(value)
            void navigate({
              search: (previous) => ({
                ...previous,
                page: 1,
                channel: undefined,
              }),
            })
          }}
          className='video-works-scope'
        >
          <TabsList>
            <TabsTrigger value='all'>{t('All works')}</TabsTrigger>
            <TabsTrigger value='self'>{t('Only Mine')}</TabsTrigger>
          </TabsList>
        </Tabs>
      )}
    </header>
  )
}

export function VideoWorksPagination(props: {
  page: number
  pageSize: number
  total: number
  isFetching: boolean
}) {
  const { t } = useTranslation()
  const navigate = route.useNavigate()
  return (
    <footer className='video-works-pagination'>
      <span>{t('{{count}} videos', { count: props.total })}</span>
      <div>
        <label>
          <span className='sr-only'>{t('Rows per page')}</span>
          <select
            aria-label={t('Rows per page')}
            value={props.pageSize}
            onChange={(event) =>
              void navigate({
                search: (previous) => ({
                  ...previous,
                  page: 1,
                  pageSize: Number(event.target.value),
                }),
              })
            }
          >
            {[20, 50, 100].map((value) => (
              <option key={value} value={value}>
                {value} / {t('page')}
              </option>
            ))}
          </select>
        </label>
        <Button
          variant='outline'
          size='icon-sm'
          aria-label={t('Previous page')}
          disabled={props.page <= 1 || props.isFetching}
          onClick={() =>
            void navigate({
              search: (previous) => ({
                ...previous,
                page: props.page - 1,
              }),
            })
          }
        >
          <HugeiconsIcon icon={ArrowLeft01Icon} aria-hidden='true' />
        </Button>
        <span>
          {props.page} / {Math.max(1, Math.ceil(props.total / props.pageSize))}
        </span>
        <Button
          variant='outline'
          size='icon-sm'
          aria-label={t('Next page')}
          disabled={
            props.page * props.pageSize >= props.total || props.isFetching
          }
          onClick={() =>
            void navigate({
              search: (previous) => ({
                ...previous,
                page: props.page + 1,
              }),
            })
          }
        >
          <HugeiconsIcon icon={ArrowRight01Icon} aria-hidden='true' />
        </Button>
      </div>
    </footer>
  )
}

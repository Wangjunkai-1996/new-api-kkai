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
import { RefreshIcon, Search01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { getRouteApi, Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import emptyArt from '@/assets/video-works/empty.webp'
import { Main } from '@/components/layout/components/main'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useVideoStudioAccess } from '@/features/video-studio/hooks/use-video-studio-access'

import { useLogsViewScope } from '../components/usage-logs-provider'
import type { TaskLog } from '../types'
import { useSelectedVideoWork } from './use-selected-video-work'
import { useVideoWorkDownload } from './use-video-work-download'
import { useVideoWorks } from './use-video-works'
import { VideoWorkBillingSheet } from './video-work-billing-sheet'
import { VideoWorkItem } from './video-work-item'
import { VideoWorkPreview } from './video-work-preview'
import { VideoWorksHeading, VideoWorksPagination } from './video-works-controls'
import { VideoWorksFilterBar } from './video-works-filter-bar'

import './video-works.css'

const route = getRouteApi('/_authenticated/usage-logs/$section')

type Selection = {
  log: TaskLog
  kind: 'preview' | 'billing'
  query: string
} | null

export function VideoWorksPage() {
  const query = useVideoWorks()
  return <VideoWorksContent key={query.identityKey} query={query} />
}

function VideoWorksContent(props: { query: ReturnType<typeof useVideoWorks> }) {
  const { t } = useTranslation()
  const navigate = route.useNavigate()
  const scope = useLogsViewScope()
  const studio = useVideoStudioAccess()
  const [selection, setSelection] = useState<Selection>(null)
  const query = props.query
  const search = query.search
  const searchKey = JSON.stringify(search)
  const { download, downloadingId } = useVideoWorkDownload((log) => {
    setSelection({ log, kind: 'preview', query: searchKey })
  })
  const selectedQuery = useSelectedVideoWork(
    selection?.query === searchKey ? selection.log : null,
    query.items
  )
  const selected = selectedQuery.log
  if (
    selection &&
    (selectedQuery.shouldClose || selection.query !== searchKey)
  ) {
    setSelection(null)
  }
  const status = search.status_group || 'all'
  const filtered = Boolean(
    search.q ||
    search.filter ||
    search.channel ||
    search.startTime ||
    search.endTime ||
    status !== 'all'
  )
  const empty = !query.isLoading && !query.error && query.items.length === 0
  const tabs = [
    { value: 'all' as const, label: t('All videos') },
    { value: 'success' as const, label: t('Completed') },
    { value: 'processing' as const, label: t('Generating') },
    { value: 'failed' as const, label: t('Failed') },
  ]
  const reset = () =>
    void navigate({ search: { page: 1, pageSize: query.pageSize } })

  return (
    <Main className='video-works-main'>
      <div className='video-works-page'>
        <VideoWorksHeading onScopeChange={() => setSelection(null)} />
        <VideoWorksFilterBar
          key={`${scope.viewAccess}:${search.q || ''}:${search.filter || ''}:${search.channel || ''}`}
          isFetching={query.isFetching}
          identityKey={query.identityKey}
          filtered={filtered}
          onReset={reset}
          onRefresh={() => void query.refetch()}
        />
        <div
          className='video-works-status-tabs'
          aria-label={t('Generation status')}
        >
          {tabs.map((tab) => (
            <button
              type='button'
              key={tab.value}
              aria-pressed={status === tab.value}
              onClick={() =>
                void navigate({
                  search: (previous) => ({
                    ...previous,
                    status_group: tab.value,
                    page: 1,
                  }),
                })
              }
            >
              {tab.label}
              {query.statusCounts && (
                <span>{query.statusCounts[tab.value]}</span>
              )}
            </button>
          ))}
          {query.isFetching && !query.isLoading && (
            <span className='video-works-refreshing' role='status'>
              {t('Updating...')}
            </span>
          )}
        </div>
        {query.mediaWarning && (
          <p className='video-works-warning' role='status'>
            {t(
              'Some video previews are temporarily unavailable. Task and billing records are still available.'
            )}
          </p>
        )}
        {query.error ? (
          <div className='video-works-empty' role='alert'>
            <HugeiconsIcon
              icon={RefreshIcon}
              className='video-works-empty-icon'
              aria-hidden='true'
            />
            <h2>{t('Could not load video works')}</h2>
            <p>{t('Please try again. Your filters have been kept.')}</p>
            <Button onClick={() => void query.refetch()}>{t('Retry')}</Button>
          </div>
        ) : null}
        {!query.error && query.isLoading && (
          <div className='video-works-loading' aria-label={t('Loading...')}>
            {[0, 1, 2].map((index) => (
              <Skeleton key={index} className='h-[125px] rounded-xl' />
            ))}
          </div>
        )}
        {empty && (
          <div className='video-works-empty'>
            {filtered ? (
              <HugeiconsIcon
                icon={Search01Icon}
                className='video-works-empty-icon'
                aria-hidden='true'
              />
            ) : (
              <img src={emptyArt} alt='' />
            )}
            <h2>
              {filtered ? t('No matching videos') : t('No video works yet')}
            </h2>
            <p>
              {filtered
                ? t('Try another model or task ID, or adjust your filters.')
                : t('Your generated videos will appear here.')}
            </p>
            {filtered ? (
              <Button variant='outline' onClick={reset}>
                {t('Reset filters')}
              </Button>
            ) : (
              studio.available && (
                <Button
                  nativeButton={false}
                  render={<Link to='/video-studio/create' />}
                >
                  {t('Create a video')}
                </Button>
              )
            )}
          </div>
        )}
        {!query.error && !query.isLoading && query.items.length > 0 && (
          <>
            <div className='video-works-table-head' aria-hidden='true'>
              <span>{t('Cover')}</span>
              <span>{t('Video work')}</span>
              <span>{t('Status')}</span>
              <span>{t('Created')}</span>
              <span>{t('Cost')}</span>
              <span>{t('Actions')}</span>
            </div>
            <div className='video-works-list'>
              {query.items.map((log) => (
                <VideoWorkItem
                  key={log.id}
                  log={log}
                  isAdmin={scope.isAdminView}
                  renewPoster={() => query.renewPoster(log)}
                  onPreview={() =>
                    setSelection({
                      log,
                      kind: 'preview',
                      query: searchKey,
                    })
                  }
                  onDownload={() => void download(log)}
                  downloading={downloadingId === log.id}
                  onBilling={() =>
                    setSelection({
                      log,
                      kind: 'billing',
                      query: searchKey,
                    })
                  }
                />
              ))}
            </div>
            <VideoWorksPagination
              page={query.page}
              pageSize={query.pageSize}
              total={query.total}
              isFetching={query.isFetching}
            />
          </>
        )}
      </div>
      {selected && selection?.kind === 'preview' && (
        <VideoWorkPreview
          key={selected.id}
          log={selected}
          isAdmin={scope.isAdminView}
          isRoot={scope.isRootView}
          onClose={() => setSelection(null)}
          onBilling={() =>
            setSelection({ log: selected, kind: 'billing', query: searchKey })
          }
        />
      )}
      {selected && selection?.kind === 'billing' && (
        <VideoWorkBillingSheet
          log={selected}
          onClose={() => setSelection(null)}
        />
      )}
    </Main>
  )
}

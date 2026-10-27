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
import { getRouteApi } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { CompactDateTimeRangePicker } from '../components/compact-date-time-range-picker'
import { useLogsViewScope } from '../components/usage-logs-provider'
import { VideoWorksChannelPicker } from './video-works-channel-picker'

const route = getRouteApi('/_authenticated/usage-logs/$section')

export function VideoWorksFilterBar(props: {
  isFetching: boolean
  identityKey: string
  filtered: boolean
  onReset: () => void
  onRefresh: () => void
}) {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const { isAdminView } = useLogsViewScope()
  const [query, setQuery] = useState(search.q || search.filter || '')
  const [channel, setChannel] = useState(search.channel || '')

  return (
    <form
      className='video-works-filter'
      onSubmit={(event) => {
        event.preventDefault()
        void navigate({
          search: (previous) => ({
            ...previous,
            q: query.trim() || undefined,
            filter: undefined,
            channel: isAdminView ? channel.trim() || undefined : undefined,
            page: 1,
          }),
        })
      }}
    >
      <div className='video-works-search'>
        <HugeiconsIcon icon={Search01Icon} aria-hidden='true' />
        <Input
          aria-label={t('Search videos')}
          placeholder={t('Search model or task ID')}
          value={query}
          maxLength={191}
          onChange={(event) => setQuery(event.target.value)}
        />
      </div>
      <CompactDateTimeRangePicker
        className='video-works-date'
        start={search.startTime ? new Date(search.startTime) : undefined}
        end={search.endTime ? new Date(search.endTime) : undefined}
        onChange={({ start, end }) => {
          if (start && end && start > end) return
          void navigate({
            search: (previous) => ({
              ...previous,
              startTime: start?.getTime(),
              endTime: end?.getTime(),
              page: 1,
            }),
          })
        }}
      />
      {isAdminView && (
        <VideoWorksChannelPicker
          identityKey={props.identityKey}
          value={channel}
          onChange={setChannel}
        />
      )}
      <Button type='submit' variant='outline'>
        {t('Search')}
      </Button>
      {(props.filtered || query || channel) && (
        <Button
          type='button'
          variant='ghost'
          onClick={() => {
            setQuery('')
            setChannel('')
            props.onReset()
          }}
        >
          {t('Reset filters')}
        </Button>
      )}
      <Button
        type='button'
        variant='ghost'
        size='icon'
        aria-label={t('Refresh')}
        disabled={props.isFetching}
        onClick={props.onRefresh}
      >
        <HugeiconsIcon icon={RefreshIcon} aria-hidden='true' />
      </Button>
    </form>
  )
}

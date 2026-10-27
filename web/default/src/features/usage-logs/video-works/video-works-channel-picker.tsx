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
import { useInfiniteQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  useComboboxAnchor,
} from '@/components/ui/combobox'
import { searchChannels } from '@/features/channels/api'
import { useDebounce } from '@/hooks/use-debounce'
import { requireServerSuccess } from '@/lib/server-error-message'

export function VideoWorksChannelPicker(props: {
  identityKey: string
  value: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [term, setTerm] = useState('')
  const [selectedName, setSelectedName] = useState('')
  const keyword = useDebounce(term.trim(), 250)
  const anchor = useComboboxAnchor()
  const query = useInfiniteQuery({
    queryKey: ['video-works-channels', props.identityKey, keyword],
    initialPageParam: 1,
    queryFn: async ({ pageParam }) => {
      const response = requireServerSuccess(
        await searchChannels({
          keyword: keyword || undefined,
          p: pageParam,
          page_size: 30,
          sort_by: 'name',
          sort_order: 'asc',
        })
      )
      return {
        items:
          response.data?.items.map(({ id, name }) => ({
            id: String(id),
            name,
          })) ?? [],
        total: response.data?.total ?? 0,
      }
    },
    getNextPageParam: (last, pages) =>
      pages.reduce((count, page) => count + page.items.length, 0) < last.total
        ? pages.length + 1
        : undefined,
    enabled: open,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  })
  const channels = query.data?.pages.flatMap((page) => page.items) ?? []
  const names = new Map(channels.map((channel) => [channel.id, channel.name]))
  const selectedLabel = props.value
    ? names.get(props.value) ||
      selectedName ||
      `${t('Channel')} #${props.value}`
    : t('All channels')

  return (
    <Combobox
      items={['', ...channels.map((channel) => channel.id)]}
      value={props.value}
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) setTerm('')
      }}
      inputValue={open ? term : selectedLabel}
      onInputValueChange={(value, details) => {
        if (details.reason === 'input-change') setTerm(value)
      }}
      onValueChange={(value) => {
        if (value === null) return
        setSelectedName(names.get(value) || '')
        props.onChange(value)
      }}
      itemToStringLabel={(id) => names.get(id) || t('All channels')}
      filter={null}
    >
      <div ref={anchor} className='video-works-channel'>
        <ComboboxInput
          aria-label={t('Channel')}
          placeholder={t('Search channel name or ID')}
          className='h-full'
          triggerAriaLabel={t('All channels')}
        />
      </div>
      <ComboboxContent anchor={anchor}>
        {query.error ? (
          <div className='p-3 text-sm' role='alert'>
            <p>{t('Could not load channels')}</p>
            <Button
              size='sm'
              variant='ghost'
              onClick={() => void query.refetch()}
            >
              {t('Retry')}
            </Button>
          </div>
        ) : (
          <>
            <ComboboxEmpty>
              {query.isFetching ? t('Loading...') : t('No results found')}
            </ComboboxEmpty>
            <ComboboxList>
              {(id: string) => (
                <ComboboxItem key={id} value={id}>
                  {id ? `${names.get(id)} · #${id}` : t('All channels')}
                </ComboboxItem>
              )}
            </ComboboxList>
            {query.hasNextPage && (
              <Button
                className='w-full'
                variant='ghost'
                size='sm'
                disabled={query.isFetchingNextPage}
                onClick={() => void query.fetchNextPage()}
              >
                {t('Load more')}
              </Button>
            )}
            {query.isFetching && (
              <p
                className='text-muted-foreground px-3 py-2 text-xs'
                role='status'
              >
                {t('Loading...')}
              </p>
            )}
          </>
        )}
      </ComboboxContent>
    </Combobox>
  )
}

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
import { Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { DOCS_NAVIGATION } from './docs-data'

export function DocsSidebar(props: {
  activeId: string
  onSelect: (id: string) => void
}) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const items = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    if (!normalized) return DOCS_NAVIGATION
    return DOCS_NAVIGATION.filter((item) =>
      t(item.label).toLowerCase().includes(normalized)
    )
  }, [query, t])

  return (
    <aside className='border-b border-white/10 bg-black px-5 py-6 lg:sticky lg:top-[135px] lg:h-[calc(100svh-135px)] lg:overflow-y-auto lg:border-r lg:border-b-0 lg:px-6 lg:py-6'>
      <label className='flex h-[50px] items-center gap-2 rounded-[9px] border border-[#181818] bg-[#0a0a0a] px-4'>
        <Search className='size-5 text-[#606060]' aria-hidden />
        <span className='sr-only'>{t('Search documentation')}</span>
        <input
          type='search'
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={t('Search')}
          className='min-w-0 flex-1 bg-transparent text-lg text-white outline-none placeholder:text-[#606060]'
        />
      </label>

      <label className='mt-4 block lg:hidden'>
        <span className='sr-only'>{t('Documentation navigation')}</span>
        <select
          value={props.activeId}
          onChange={(event) => props.onSelect(event.target.value)}
          className='h-12 w-full rounded-[9px] border border-[#181818] bg-[#262626] px-4 text-base text-white outline-none'
        >
          {DOCS_NAVIGATION.map((item) => (
            <option key={item.id} value={item.id}>
              {t(item.label)}
            </option>
          ))}
        </select>
      </label>

      <nav
        aria-label={t('Documentation navigation')}
        className='mt-8 hidden space-y-1 lg:block'
      >
        {items.map((item) => (
          <button
            key={item.id}
            type='button'
            onClick={() => props.onSelect(item.id)}
            className={cn(
              'block min-h-[49px] w-full rounded-[9px] px-4 py-3 text-left text-base text-[#eee] transition hover:bg-white/10 lg:text-lg',
              props.activeId === item.id && 'bg-[#262626]'
            )}
          >
            {t(item.label)}
          </button>
        ))}
        <div className='px-4 pt-3 text-lg text-[#eee]'>
          {t('Model information')}
          <div className='mt-4 space-y-3 border-l border-white/15 pl-7 text-base'>
            <button type='button' className='block hover:text-white/65'>
              {t('List available models')}
            </button>
            <button type='button' className='block hover:text-white/65'>
              {t('Get a single model')}
            </button>
          </div>
        </div>
      </nav>
    </aside>
  )
}

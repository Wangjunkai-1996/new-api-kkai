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
import { ChevronDown, Search } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import {
  ModelSquareFilterPanel,
  type ModelSquareFilterOption,
} from './model-square-filter-panel'

type ModelSquareToolbarProps = {
  search: string
  vendor: string
  group: string
  tag: string
  quotaType: string
  context: string
  vendors: ModelSquareFilterOption[]
  groups: ModelSquareFilterOption[]
  tags: ModelSquareFilterOption[]
  onSearchChange: (value: string) => void
  onVendorChange: (value: string) => void
  onGroupChange: (value: string) => void
  onTagChange: (value: string) => void
  onQuotaTypeChange: (value: string) => void
  onContextChange: (value: string) => void
}

const PANEL_ID = 'model-square-filter-panel'

export function ModelSquareToolbar(props: ModelSquareToolbarProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const toolbarRef = useRef<HTMLDivElement>(null)
  const allOption = useMemo<ModelSquareFilterOption[]>(
    () => [{ label: t('All'), value: 'all' }],
    [t]
  )

  const columns = useMemo(
    () => [
      {
        id: 'vendor',
        label: t('Model Vendors'),
        value: props.vendor,
        options: [...allOption, ...props.vendors],
        onChange: props.onVendorChange,
      },
      {
        id: 'group',
        label: t('Groups'),
        value: props.group,
        options: [...allOption, ...props.groups],
        onChange: props.onGroupChange,
      },
      {
        id: 'tag',
        label: t('Tags'),
        value: props.tag,
        options: [...allOption, ...props.tags],
        onChange: props.onTagChange,
      },
      {
        id: 'billing',
        label: t('Billing'),
        value: props.quotaType,
        options: [
          ...allOption,
          { label: t('Token-based'), value: 'token' },
          { label: t('Per Request'), value: 'request' },
          { label: t('Per Second'), value: 'second' },
        ],
        onChange: props.onQuotaTypeChange,
      },
      {
        id: 'context',
        label: t('Context'),
        value: props.context,
        options: [
          ...allOption,
          { label: '1-4K', value: '1-4k' },
          { label: '4-16K', value: '4-16k' },
          { label: '16-64K', value: '16-64k' },
          { label: '64-128K', value: '64-128k' },
          { label: '128-200K', value: '128-200k' },
          { label: '200K+', value: '200k+' },
        ],
        onChange: props.onContextChange,
      },
    ],
    [
      allOption,
      props.context,
      props.group,
      props.groups,
      props.onContextChange,
      props.onGroupChange,
      props.onQuotaTypeChange,
      props.onTagChange,
      props.onVendorChange,
      props.quotaType,
      props.tag,
      props.tags,
      props.vendor,
      props.vendors,
      t,
    ]
  )

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: PointerEvent) => {
      if (!toolbarRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  const valueLabel = (value: string, options: ModelSquareFilterOption[]) =>
    options.find((option) => option.value === value)?.label || t('All')

  return (
    <div
      ref={toolbarRef}
      className='sticky top-[104px] z-40 border-b border-white/10 bg-black/95 text-white backdrop-blur-xl lg:top-[135px]'
    >
      <div className='mx-auto flex min-h-16 w-full max-w-[1920px] flex-col gap-3 px-5 py-3 sm:px-8 xl:flex-row xl:items-center xl:justify-between xl:px-[3.38vw]'>
        <div className='no-scrollbar flex items-center gap-8 overflow-x-auto pb-1 xl:gap-9 xl:pb-0'>
          {columns.map((column) => (
            <button
              key={column.id}
              type='button'
              aria-expanded={open}
              aria-controls={PANEL_ID}
              onClick={() => setOpen((value) => !value)}
              className='flex shrink-0 items-center gap-2 py-2 text-base text-[#eee] transition hover:text-white xl:text-lg'
            >
              <span>{column.label}</span>
              <span className='text-sm text-[#a3a3a3] xl:text-base'>
                {valueLabel(column.value, column.options)}
              </span>
              <ChevronDown
                className={cn(
                  'size-3 text-white/80 transition-transform',
                  open && 'rotate-180'
                )}
                aria-hidden
              />
            </button>
          ))}
        </div>

        <label className='flex h-12 w-full items-center gap-2 rounded-[9px] border border-[#181818] bg-[#0a0a0a] px-4 xl:w-[369px]'>
          <Search className='size-5 shrink-0 text-[#606060]' aria-hidden />
          <span className='sr-only'>{t('Search models')}</span>
          <input
            type='search'
            value={props.search}
            onChange={(event) => props.onSearchChange(event.target.value)}
            placeholder={t('Search models...')}
            className='min-w-0 flex-1 bg-transparent text-base text-white outline-none placeholder:text-[#606060]'
          />
        </label>
      </div>

      {open && <ModelSquareFilterPanel id={PANEL_ID} columns={columns} />}
    </div>
  )
}

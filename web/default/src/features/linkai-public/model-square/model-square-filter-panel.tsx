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
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

export type ModelSquareFilterOption = { label: string; value: string }

type FilterColumn = {
  id: string
  label: string
  value: string
  options: ModelSquareFilterOption[]
  onChange: (value: string) => void
}

type ModelSquareFilterPanelProps = {
  id: string
  columns: FilterColumn[]
}

export function ModelSquareFilterPanel(props: ModelSquareFilterPanelProps) {
  const { t } = useTranslation()

  return (
    <div
      id={props.id}
      className='absolute inset-x-0 top-full z-50 border-t border-white/10 bg-black px-3 sm:px-5'
    >
      <div
        className='mx-auto max-h-[min(68vh,486px)] w-full max-w-[1828px] overflow-auto rounded-b-[22px] border-x border-b border-white/10 bg-[#050505] shadow-[0_28px_90px_rgba(0,0,0,0.72)]'
        style={{
          backgroundImage:
            "url('/figma/linkai-model-square/filter-panel-background.png')",
          backgroundPosition: 'center',
          backgroundRepeat: 'no-repeat',
          backgroundSize: '100% 100%',
        }}
      >
        <div className='grid min-w-[1120px] grid-cols-[2.2fr_1.1fr_1.1fr_.5fr_.75fr]'>
          {props.columns.map((column) => (
            <section
              key={column.id}
              aria-labelledby={`${props.id}-${column.id}`}
              className='min-h-[484px] border-r border-white/10 p-5 last:border-r-0'
            >
              <h2
                id={`${props.id}-${column.id}`}
                className='mb-3 text-base font-medium text-[#e9e9e9]'
              >
                {column.label}
              </h2>
              <div
                className={cn(
                  'grid gap-1.5',
                  column.id === 'vendor' && 'grid-cols-4',
                  column.id === 'group' && 'grid-cols-2',
                  column.id === 'tag' && 'grid-cols-3',
                  (column.id === 'billing' || column.id === 'context') &&
                    'grid-cols-1'
                )}
              >
                {column.options.map((option) => {
                  const selected = option.value === column.value
                  return (
                    <button
                      key={option.value}
                      type='button'
                      aria-pressed={selected}
                      onClick={() => column.onChange(option.value)}
                      className={cn(
                        'min-h-10 rounded-xl border border-white/[0.04] bg-[#111] px-3 py-2 text-sm text-[#d8d8d8] transition hover:border-white/25 hover:bg-[#1b1b1b] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white',
                        selected &&
                          'border-white bg-[#ededed] font-semibold text-black hover:bg-white'
                      )}
                    >
                      {option.label || t('Unnamed')}
                    </button>
                  )
                })}
              </div>
            </section>
          ))}
        </div>
      </div>
    </div>
  )
}

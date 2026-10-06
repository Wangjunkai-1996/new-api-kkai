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
import { Check, ChevronsUpDown, Loader2 } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { useMediaQuery } from '@/hooks'
import { cn } from '@/lib/utils'

import {
  AUTO_GROUP_FRAME_CLASS_NAME,
  AutoGroupFlowBorder,
  GroupRatioBadge,
} from './auto-group-visuals'

export type ApiKeyGroupOption = {
  value: string
  label: string
  desc?: string
  ratio?: number | string
  isAuto?: boolean
  disabled?: boolean
}

type ApiKeyGroupComboboxProps = {
  options: ApiKeyGroupOption[]
  value?: string
  onValueChange: (value: string) => void
  placeholder?: string
  disabled?: boolean
  compact?: boolean
  pending?: boolean
  onOpen?: () => void
  statusContent?: ReactNode
  trigger?: ReactNode
  triggerAriaLabel?: string
}

function getDistinctDescription(
  option?: ApiKeyGroupOption
): string | undefined {
  const description = option?.desc?.trim()
  if (!description || description === option?.label?.trim()) return undefined
  return description
}

export function ApiKeyGroupCombobox({
  options,
  value,
  onValueChange,
  placeholder,
  disabled,
  compact = false,
  pending = false,
  onOpen,
  statusContent,
  trigger,
  triggerAriaLabel,
}: ApiKeyGroupComboboxProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [searchValue, setSearchValue] = useState('')
  const shouldReduceMotion = useMediaQuery('(prefers-reduced-motion: reduce)')
  const selectedOption = options.find((option) => option.value === value)
  const isAutoSelected =
    selectedOption?.value === 'auto' || Boolean(selectedOption?.isAuto)
  const selectedDescription = getDistinctDescription(selectedOption)

  const filteredOptions = useMemo(() => {
    const search = searchValue.trim().toLowerCase()
    if (!search) return options

    return options.filter((option) => {
      const ratioText = String(option.ratio ?? '').toLowerCase()
      return (
        option.value.toLowerCase().includes(search) ||
        option.label.toLowerCase().includes(search) ||
        option.desc?.toLowerCase().includes(search) ||
        ratioText.includes(search)
      )
    })
  }, [options, searchValue])

  const handleSelect = (selectedValue: string) => {
    if (selectedValue !== value) onValueChange(selectedValue)
    setOpen(false)
    setSearchValue('')
  }

  const handleOpenChange = (nextOpen: boolean) => {
    setOpen(nextOpen)
    if (!nextOpen) setSearchValue('')
    if (nextOpen) onOpen?.()
  }

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            role='combobox'
            aria-expanded={open}
            data-auto-group-effect={isAutoSelected ? 'trigger' : undefined}
            aria-label={triggerAriaLabel}
            disabled={disabled}
            className={cn(
              'relative',
              compact
                ? 'hover:bg-muted/70 data-popup-open:bg-muted/70 h-auto min-h-8 max-w-full min-w-0 justify-between gap-1 rounded-md border-transparent bg-transparent px-1.5 py-1 text-start shadow-none transition-[background-color,border-color,box-shadow] data-popup-open:border-ring data-popup-open:ring-ring/20 data-popup-open:ring-[3px]'
                : 'border-input bg-muted/40 hover:bg-muted/55 hover:text-foreground active:bg-background data-popup-open:border-ring data-popup-open:bg-background data-popup-open:ring-ring/20 h-auto min-h-14 w-full justify-between gap-2 rounded-lg px-3 py-2 text-start shadow-none transition-[background-color,border-color,box-shadow] duration-150 data-popup-open:ring-[3px] sm:min-h-20 sm:gap-3 sm:px-4 sm:py-3',
              isAutoSelected &&
                cn(
                  AUTO_GROUP_FRAME_CLASS_NAME,
                  'hover:border-primary/55 data-popup-open:border-primary/55 data-popup-open:ring-primary/20'
                )
            )}
          />
        }
      >
        {isAutoSelected && (
          <AutoGroupFlowBorder shouldReduceMotion={shouldReduceMotion} />
        )}
        {trigger || (
          <span className='flex min-w-0 flex-1 items-center justify-between gap-2 sm:gap-3'>
            <span className='min-w-0'>
              <span className='block truncate font-medium'>
                {selectedOption?.label || placeholder || t('Select a group')}
              </span>
              {selectedDescription && (
                <span className='text-muted-foreground block truncate text-[11px] sm:text-xs'>
                  {selectedDescription}
                </span>
              )}
            </span>
            <span className='hidden sm:block'>
              <GroupRatioBadge
                ratio={selectedOption?.ratio}
                isAuto={isAutoSelected}
                shouldReduceMotion={shouldReduceMotion}
              />
            </span>
          </span>
        )}
        {pending ? (
          <Loader2 className='text-muted-foreground h-3.5 w-3.5 shrink-0 animate-spin' />
        ) : (
          <ChevronsUpDown className='text-muted-foreground h-3.5 w-3.5 shrink-0' />
        )}
      </PopoverTrigger>
      <PopoverContent
        align='start'
        collisionPadding={12}
        sideOffset={8}
        className='data-closed:zoom-out-100 data-open:zoom-in-100 data-[side=bottom]:slide-in-from-top-0 data-[side=left]:slide-in-from-right-0 data-[side=right]:slide-in-from-left-0 data-[side=top]:slide-in-from-bottom-0 w-[min(24rem,calc(100vw-1.5rem))] max-w-[calc(100vw-1.5rem)] min-w-[min(18rem,calc(100vw-1.5rem))] overflow-hidden rounded-xl p-0.5 shadow-lg data-closed:duration-75 data-open:duration-100'
        onWheel={(event) => event.stopPropagation()}
        onTouchMove={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={t('Search...')}
            value={searchValue}
            onValueChange={setSearchValue}
            className='h-8'
          />
          <CommandList className='max-h-[360px]'>
            {statusContent || (
              <>
                <CommandEmpty>{t('No group found.')}</CommandEmpty>
                <CommandGroup>
                  {filteredOptions.map((option) => {
                    const isAutoOption =
                      option.value === 'auto' || Boolean(option.isAuto)
                    return (
                      <CommandItem
                        key={option.value}
                        value={option.value}
                        disabled={option.disabled}
                        data-auto-group-effect={
                          isAutoOption ? 'option' : undefined
                        }
                        onSelect={() => handleSelect(option.value)}
                        className={cn(
                          'data-[selected=true]:bg-muted/80 data-[selected=true]:border-border/60 items-start gap-3 rounded-lg border border-transparent px-3 py-2.5 transition-colors data-[disabled=true]:opacity-60 [&>svg:last-child]:hidden',
                          isAutoOption &&
                            cn(
                              AUTO_GROUP_FRAME_CLASS_NAME,
                              'border-primary/35 data-[selected=true]:border-primary/55'
                            )
                        )}
                      >
                        {isAutoOption && (
                          <AutoGroupFlowBorder
                            shouldReduceMotion={shouldReduceMotion}
                          />
                        )}
                        <Check
                          aria-hidden='true'
                          className={cn(
                            'mt-0.5 h-4 w-4',
                            value === option.value ? 'opacity-100' : 'opacity-0'
                          )}
                        />
                        <span className='min-w-0 flex-1'>
                          <span className='block truncate font-medium'>
                            {option.label}
                          </span>
                          {getDistinctDescription(option) && (
                            <span className='text-muted-foreground block truncate text-xs'>
                              {getDistinctDescription(option)}
                            </span>
                          )}
                        </span>
                        <GroupRatioBadge
                          ratio={option.ratio}
                          isAuto={isAutoOption}
                          shouldReduceMotion={shouldReduceMotion}
                        />
                      </CommandItem>
                    )
                  })}
                </CommandGroup>
              </>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

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
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentProps,
  type ReactNode,
} from 'react'
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
  normalizeApiKeyGroupSearch,
  resolveApiKeyGroupFamily,
} from '../lib/api-key-group-family'
import { ApiKeyGroupFamilyMenu } from './api-key-group-family-menu'
import { ApiKeyGroupSignal } from './api-key-group-signal'
import { ApiKeyGroupTrigger } from './api-key-group-trigger'
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

type ApiKeyGroupComboboxProps = Pick<
  ComponentProps<'button'>,
  'id' | 'aria-describedby' | 'aria-invalid'
> & {
  options: ApiKeyGroupOption[]
  value?: string
  onValueChange: (value: string) => void
  placeholder?: string
  disabled?: boolean
  compact?: boolean
  byModelFamily?: boolean
  crossGroupRetry?: boolean
  pending?: boolean
  onOpen?: () => void
  statusContent?: ReactNode
  trigger?: ReactNode
  triggerAriaLabel?: string
  'data-slot'?: string
  'data-form-root'?: string
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
  byModelFamily = false,
  crossGroupRetry,
  pending = false,
  onOpen,
  statusContent,
  trigger,
  triggerAriaLabel,
  id,
  'aria-describedby': describedBy,
  'aria-invalid': invalid,
  'data-slot': dataSlot,
  'data-form-root': formRoot,
}: ApiKeyGroupComboboxProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [searchValue, setSearchValue] = useState('')
  const [confirmedValue, setConfirmedValue] = useState<string>()
  const composing = useRef(false)
  const shouldReduceMotion = useMediaQuery('(prefers-reduced-motion: reduce)')
  const selectedOption = options.find((option) => option.value === value)
  const isAutoSelected =
    selectedOption?.value === 'auto' || Boolean(selectedOption?.isAuto)
  const selectedDescription = getDistinctDescription(selectedOption)

  const selectedFamily =
    byModelFamily && selectedOption
      ? resolveApiKeyGroupFamily(selectedOption)
      : undefined
  const specialFamilyLabels = {
    auto: t('Automatic routing'),
    mixed: t('Multiple providers'),
    other: t('Other groups'),
  }
  const familyLabel =
    selectedFamily?.kind === 'provider'
      ? selectedFamily.name
      : selectedFamily && specialFamilyLabels[selectedFamily.kind]

  useEffect(() => {
    if (!confirmedValue || confirmedValue !== value) return
    const timer = window.setTimeout(() => setConfirmedValue(undefined), 850)
    return () => window.clearTimeout(timer)
  }, [confirmedValue, value])

  const filteredOptions = useMemo(() => {
    const search = normalizeApiKeyGroupSearch(searchValue)
    if (byModelFamily || !search) return options
    const words = search.split(' ')
    return options.filter((option) => {
      const ratio =
        typeof option.ratio === 'number' ? `${option.ratio}x` : option.ratio
      const terms = normalizeApiKeyGroupSearch(
        [option.value, option.label, option.desc, ratio].join(' ')
      )
      return words.every((word) => terms.includes(word))
    })
  }, [byModelFamily, options, searchValue])

  const handleSelect = (selectedValue: string) => {
    if (!open) return
    if (selectedValue !== value) {
      if (byModelFamily) setConfirmedValue(selectedValue)
      onValueChange(selectedValue)
    }
    setOpen(false)
    setSearchValue('')
  }

  const handleOpenChange = (nextOpen: boolean) => {
    setOpen(nextOpen)
    if (!nextOpen) setSearchValue('')
    composing.current = false
    if (nextOpen) {
      setConfirmedValue(undefined)
      onOpen?.()
    }
  }

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen, details) => {
        if (
          details.reason === 'escape-key' &&
          (composing.current ||
            details.event.isComposing ||
            details.event.keyCode === 229)
        ) {
          details.cancel()
          return
        }
        handleOpenChange(nextOpen)
      }}
    >
      {byModelFamily ? (
        <PopoverTrigger
          id={id}
          disabled={disabled}
          render={
            <ApiKeyGroupTrigger
              role='combobox'
              aria-expanded={open}
              aria-label={triggerAriaLabel}
              aria-invalid={invalid}
              aria-describedby={describedBy}
              data-slot={dataSlot ?? 'button'}
              data-form-root={formRoot}
              data-auto-group-effect={isAutoSelected ? 'trigger' : undefined}
              option={selectedOption}
              family={selectedFamily}
              familyLabel={familyLabel}
              placeholder={placeholder}
              pending={pending}
              compact={compact}
              confirmed={Boolean(confirmedValue && confirmedValue === value)}
              crossGroupRetry={crossGroupRetry}
              health={
                selectedOption &&
                !selectedOption.disabled && (
                  <ApiKeyGroupSignal
                    group={selectedOption.value}
                    variant='trigger'
                  />
                )
              }
            />
          }
          onKeyDown={(event) => {
            if (
              !open &&
              (event.key === 'ArrowDown' || event.key === 'ArrowUp')
            ) {
              event.preventDefault()
              handleOpenChange(true)
            }
          }}
        />
      ) : (
        <PopoverTrigger
          id={id}
          render={
            <Button
              type='button'
              variant='outline'
              role='combobox'
              aria-expanded={open}
              data-auto-group-effect={isAutoSelected ? 'trigger' : undefined}
              aria-label={triggerAriaLabel}
              aria-describedby={describedBy}
              aria-invalid={invalid}
              data-slot={dataSlot ?? 'button'}
              data-form-root={formRoot}
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
      )}
      <PopoverContent
        align='start'
        collisionPadding={12}
        sideOffset={8}
        aria-label={byModelFamily ? t('Groups') : undefined}
        data-compact={byModelFamily && compact ? true : undefined}
        className={
          byModelFamily
            ? 'kgrp-popover'
            : 'data-closed:zoom-out-100 data-open:zoom-in-100 data-[side=bottom]:slide-in-from-top-0 data-[side=left]:slide-in-from-right-0 data-[side=right]:slide-in-from-left-0 data-[side=top]:slide-in-from-bottom-0 w-[min(24rem,calc(100vw-1.5rem))] max-w-[calc(100vw-1.5rem)] min-w-[min(18rem,calc(100vw-1.5rem))] overflow-hidden rounded-xl p-0.5 shadow-lg data-closed:duration-75 data-open:duration-100'
        }
        onKeyDownCapture={(event) => {
          if (
            composing.current ||
            event.nativeEvent.isComposing ||
            event.keyCode === 229
          ) {
            event.stopPropagation()
          }
        }}
        onWheel={(event) => event.stopPropagation()}
        onTouchMove={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        {byModelFamily ? (
          <ApiKeyGroupFamilyMenu
            options={options}
            selected={value}
            onSelect={handleSelect}
            open={open}
            statusContent={statusContent}
            onCompositionChange={(active) => {
              composing.current = active
            }}
            query={searchValue}
            onQueryChange={setSearchValue}
          />
        ) : (
          <Command shouldFilter={false} label={t('Search groups')}>
            <CommandInput
              placeholder={t('Search...')}
              value={searchValue}
              onValueChange={setSearchValue}
              className='h-8'
            />
            <CommandList className='max-h-[360px]' label={t('Groups')}>
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
                          <span className='mt-0.5 grid size-4 shrink-0 place-items-center'>
                            {value === option.value && (
                              <>
                                <Check aria-hidden='true' className='size-4' />
                                <span className='sr-only'>{t('selected')}</span>
                              </>
                            )}
                          </span>
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
        )}
      </PopoverContent>
    </Popover>
  )
}

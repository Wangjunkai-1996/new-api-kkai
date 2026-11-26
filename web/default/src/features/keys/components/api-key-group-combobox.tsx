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
import {
  Check,
  ChevronsUpDown,
  Layers,
  Loader2,
  Route,
  Search,
} from 'lucide-react'
import { useMemo, useState, type ComponentProps, type ReactNode } from 'react'
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
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import {
  normalizeApiKeyGroupSearch,
  resolveApiKeyGroupFamily,
  type ApiKeyGroupFamily,
} from '../lib/api-key-group-family'
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
  pending?: boolean
  onOpen?: () => void
  statusContent?: ReactNode
  trigger?: ReactNode
  triggerAriaLabel?: string
  'data-slot'?: string
  'data-form-root'?: string
}

type GroupSection = {
  family?: ApiKeyGroupFamily
  label?: string
  entries: { option: ApiKeyGroupOption; searchText: string }[]
}

function getDistinctDescription(
  option?: ApiKeyGroupOption
): string | undefined {
  const description = option?.desc?.trim()
  if (!description || description === option?.label?.trim()) return undefined
  return description
}

function GroupFamilyIcon({ family }: { family: ApiKeyGroupFamily }) {
  const Icon = family.kind === 'auto' ? Route : Layers
  return (
    <span
      aria-hidden='true'
      className='bg-muted/65 text-muted-foreground ring-border/70 grid size-6 shrink-0 place-items-center rounded-md ring-1 ring-inset'
    >
      {family.icon ? (
        getLobeIcon(family.icon, 14)
      ) : (
        <Icon className='size-3.5' />
      )}
    </span>
  )
}

export function ApiKeyGroupCombobox({
  options,
  value,
  onValueChange,
  placeholder,
  disabled,
  compact = false,
  byModelFamily = false,
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
  const shouldReduceMotion = useMediaQuery('(prefers-reduced-motion: reduce)')
  const selectedOption = options.find((option) => option.value === value)
  const isAutoSelected =
    selectedOption?.value === 'auto' || Boolean(selectedOption?.isAuto)
  const selectedDescription = getDistinctDescription(selectedOption)
  const selectedFamily =
    byModelFamily && selectedOption
      ? resolveApiKeyGroupFamily(selectedOption)
      : undefined

  const sections = useMemo(() => {
    const sectionsByFamily = new Map<string, GroupSection>()
    const labels = {
      auto: t('Automatic routing'),
      mixed: t('Multiple providers'),
      other: t('Other groups'),
    }
    for (const option of options) {
      const family = byModelFamily
        ? resolveApiKeyGroupFamily(option)
        : undefined
      const key = family?.name ?? ''
      let section = sectionsByFamily.get(key)
      if (!section) {
        const label =
          family &&
          (family.kind === 'provider' ? family.name : labels[family.kind])
        section = { family, label, entries: [] }
        sectionsByFamily.set(key, section)
      }
      const ratio =
        typeof option.ratio === 'number' ? `${option.ratio}x` : option.ratio
      const searchText = normalizeApiKeyGroupSearch(
        [
          section.label,
          family?.name,
          ...(family?.aliases ?? []),
          option.value,
          option.label,
          option.desc,
          ratio,
        ].join(' ')
      )
      section.entries.push({ option, searchText })
    }
    const rank = (family?: ApiKeyGroupFamily) => {
      if (family?.kind === 'auto') return 0
      if (family?.name === 'OpenAI') return 1
      if (family?.name === 'Claude') return 2
      if (family?.kind === 'provider') return 3
      if (family?.kind === 'mixed') return 4
      return 5
    }
    return [...sectionsByFamily.values()].sort(
      (a, b) =>
        rank(a.family) - rank(b.family) ||
        (a.family?.name ?? '').localeCompare(b.family?.name ?? '', 'en')
    )
  }, [byModelFamily, options, t])

  const search = normalizeApiKeyGroupSearch(searchValue)
  const visibleSections = useMemo(() => {
    if (!search) return sections
    const words = search.split(/\s+/)
    return sections
      .map((section) => ({
        ...section,
        entries: section.entries.filter(({ searchText }) =>
          words.every((word) => searchText.includes(word))
        ),
      }))
      .filter((section) => section.entries.length > 0)
  }, [search, sections])

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
              byModelFamily &&
                'bg-background data-popup-open:bg-background min-h-12 rounded-xl px-3 py-2 sm:min-h-12 sm:gap-2 sm:px-3 sm:py-2 dark:bg-input/25',
              isAutoSelected &&
                cn(
                  AUTO_GROUP_FRAME_CLASS_NAME,
                  'hover:border-primary/55 data-popup-open:border-primary/55 data-popup-open:ring-primary/20'
                )
            )}
          />
        }
      >
        {isAutoSelected && !byModelFamily && (
          <AutoGroupFlowBorder shouldReduceMotion={shouldReduceMotion} />
        )}
        {trigger || (
          <span className='flex min-w-0 flex-1 items-center justify-between gap-2 sm:gap-3'>
            <span className='flex min-w-0 items-center gap-2.5'>
              {selectedFamily && <GroupFamilyIcon family={selectedFamily} />}
              <span className='min-w-0'>
                <span
                  className={cn(
                    'block truncate font-medium',
                    byModelFamily && !selectedOption && 'text-muted-foreground'
                  )}
                >
                  {selectedOption?.label || placeholder || t('Select a group')}
                </span>
                {selectedDescription && (
                  <span className='text-muted-foreground block truncate text-[11px] sm:text-xs'>
                    {selectedDescription}
                  </span>
                )}
              </span>
            </span>
            <span className={byModelFamily ? 'shrink-0' : 'hidden sm:block'}>
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
          <ChevronsUpDown
            aria-hidden='true'
            className='text-muted-foreground size-3.5 shrink-0'
          />
        )}
      </PopoverTrigger>
      <PopoverContent
        align='start'
        aria-label={byModelFamily ? t('Groups') : undefined}
        collisionPadding={12}
        sideOffset={8}
        className={cn(
          'data-closed:zoom-out-100 data-open:zoom-in-100 data-[side=bottom]:slide-in-from-top-0 data-[side=left]:slide-in-from-right-0 data-[side=right]:slide-in-from-left-0 data-[side=top]:slide-in-from-bottom-0 w-[min(24rem,calc(100vw-1.5rem))] max-w-[calc(100vw-1.5rem)] min-w-[min(18rem,calc(100vw-1.5rem))] overflow-hidden rounded-xl p-0.5 shadow-lg data-closed:duration-75 data-open:duration-100',
          byModelFamily &&
            'ring-border/80 max-h-(--available-height) w-(--anchor-width) min-w-0 max-w-(--available-width) rounded-2xl p-0 shadow-xl motion-reduce:animate-none'
        )}
        onWheel={(event) => event.stopPropagation()}
        onTouchMove={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <Command
          label={t('Search groups')}
          shouldFilter={false}
          defaultValue={
            byModelFamily && !selectedOption?.disabled ? value : undefined
          }
          className={cn(
            byModelFamily &&
              'min-h-0 p-0 **:data-[slot=command-input-wrapper]:shrink-0 **:data-[slot=command-input-wrapper]:p-2.5 **:data-[slot=input-group]:h-10! **:data-[slot=input-group]:rounded-lg!'
          )}
        >
          <CommandInput
            placeholder={
              byModelFamily ? t('Search by provider or group') : t('Search...')
            }
            value={searchValue}
            onValueChange={setSearchValue}
            className={byModelFamily ? 'h-full text-base sm:text-sm' : 'h-8'}
          />
          <CommandList
            label={t('Groups')}
            className={cn(
              'max-h-[360px]',
              byModelFamily &&
                'min-h-0 max-h-[min(31rem,calc(var(--available-height)-3.75rem))] scroll-py-3 overscroll-contain px-2 pb-2'
            )}
          >
            {statusContent || (
              <>
                <CommandEmpty
                  className={
                    byModelFamily
                      ? 'text-muted-foreground flex flex-col items-center gap-2 py-7'
                      : undefined
                  }
                >
                  {byModelFamily && (
                    <Search aria-hidden='true' className='size-4' />
                  )}
                  {t('No group found.')}
                </CommandEmpty>
                {visibleSections.map(({ family, label, entries }) => (
                  <CommandGroup
                    key={family?.name ?? ''}
                    value={family?.name ?? 'groups'}
                    heading={
                      family && (
                        <span className='flex items-center gap-2 text-xs font-semibold'>
                          <GroupFamilyIcon family={family} />
                          {label}
                        </span>
                      )
                    }
                    className={
                      byModelFamily
                        ? 'not-first:border-border/65 overflow-visible p-0 not-first:mt-1.5 not-first:border-t not-first:pt-1.5 **:[[cmdk-group-heading]]:px-2 **:[[cmdk-group-heading]]:py-1.5'
                        : undefined
                    }
                  >
                    {entries.map(({ option }) => {
                      const isAutoOption =
                        option.value === 'auto' || Boolean(option.isAuto)
                      const isCurrent = value === option.value
                      const description = getDistinctDescription(option)
                      return (
                        <CommandItem
                          key={option.value}
                          value={option.value}
                          disabled={option.disabled}
                          data-current={
                            byModelFamily && isCurrent ? 'true' : undefined
                          }
                          data-auto-group-effect={
                            isAutoOption ? 'option' : undefined
                          }
                          onSelect={() => handleSelect(option.value)}
                          className={cn(
                            'data-[selected=true]:bg-muted/80 data-[selected=true]:border-border/60 items-start gap-3 rounded-lg border border-transparent px-3 py-2.5 transition-colors data-[disabled=true]:opacity-60 [&>svg:last-child]:hidden',
                            byModelFamily &&
                              'data-[current=true]:bg-primary/8 data-[current=true]:border-primary/15 data-[selected=true]:bg-muted data-[selected=true]:border-border min-h-11 items-center gap-2.5 px-2 py-1.5 motion-reduce:transition-none',
                            isAutoOption &&
                              !byModelFamily &&
                              cn(
                                AUTO_GROUP_FRAME_CLASS_NAME,
                                'border-primary/35 data-[selected=true]:border-primary/55'
                              )
                          )}
                        >
                          {isAutoOption && !byModelFamily && (
                            <AutoGroupFlowBorder
                              shouldReduceMotion={shouldReduceMotion}
                            />
                          )}
                          <span className='min-w-0 flex-1'>
                            <span
                              title={option.label}
                              className='block truncate font-medium'
                            >
                              {option.label}
                            </span>
                            {description && (
                              <span
                                title={description}
                                className='text-muted-foreground block truncate text-xs'
                              >
                                {description}
                              </span>
                            )}
                          </span>
                          <GroupRatioBadge
                            ratio={option.ratio}
                            isAuto={isAutoOption}
                            shouldReduceMotion={
                              byModelFamily || shouldReduceMotion
                            }
                          />
                          <span
                            className={cn(
                              'grid size-4 shrink-0 place-items-center',
                              byModelFamily
                                ? 'text-primary'
                                : 'order-first mt-0.5'
                            )}
                          >
                            {isCurrent && (
                              <>
                                <Check aria-hidden='true' className='size-4' />
                                <span className='sr-only'>{t('selected')}</span>
                              </>
                            )}
                          </span>
                        </CommandItem>
                      )
                    })}
                  </CommandGroup>
                ))}
              </>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

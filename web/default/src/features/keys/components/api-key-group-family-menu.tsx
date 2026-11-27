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
import { Check, Info, Search, X } from 'lucide-react'
import {
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'

import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'

import {
  resolveApiKeyGroupFamily,
  normalizeApiKeyGroupSearch,
  type ApiKeyGroupFamily,
} from '../lib/api-key-group-family'
import type { ApiKeyGroupOption } from './api-key-group-combobox'
import { ApiKeyGroupSignal } from './api-key-group-signal'
import { ApiKeyGroupBrand, ApiKeyGroupRatio } from './api-key-group-trigger'

type Section = {
  family: ApiKeyGroupFamily
  label: string
  entries: { option: ApiKeyGroupOption; search: string }[]
}

function familyRank(family: ApiKeyGroupFamily) {
  if (family.kind === 'auto') return 0
  if (family.name === 'OpenAI') return 1
  if (family.name === 'Claude') return 2
  if (family.kind === 'provider') return 3
  if (family.kind === 'mixed') return 4
  return 5
}

function revealInScrollport(container: HTMLElement, item: HTMLElement) {
  const box = container.getBoundingClientRect()
  const target = item.getBoundingClientRect()
  if (target.top < box.top) {
    container.scrollTop += target.top - box.top
  } else if (target.bottom > box.bottom) {
    container.scrollTop += target.bottom - box.bottom
  }
  if (target.left < box.left) {
    container.scrollLeft += target.left - box.left
  } else if (target.right > box.right) {
    container.scrollLeft += target.right - box.right
  }
}

export function ApiKeyGroupFamilyMenu(props: {
  options: ApiKeyGroupOption[]
  selected?: string
  onSelect: (key: string) => void
  query: string
  onQueryChange: (value: string) => void
  onCompositionChange: (value: boolean) => void
  open: boolean
  statusContent?: ReactNode
}) {
  const { t } = useTranslation()
  // Undefined follows the saved group until the user chooses a provider or All.
  const [familyFilter, setFamilyFilter] = useState<string | null>()
  const [cursor, setCursor] = useState<string>()
  const { query, onQueryChange: setQuery } = props
  const [details, setDetails] = useState(false)
  const detailsId = useId()
  const inputRef = useRef<HTMLInputElement>(null)
  const railRef = useRef<HTMLDivElement>(null)
  const plateRef = useRef<HTMLSpanElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const activeRowRef = useRef<HTMLDivElement>(null)
  const filterMotion = useRef<Animation | undefined>(undefined)
  const sections = useMemo(() => {
    const families = new Map<string, Section>()
    const labels = {
      auto: t('Automatic routing'),
      mixed: t('Multiple providers'),
      other: t('Other groups'),
    }
    for (const option of props.options) {
      const family = resolveApiKeyGroupFamily(option)
      const label =
        family.kind === 'provider' ? family.name : labels[family.kind]
      let section = families.get(family.name)
      if (!section) {
        section = { family, label, entries: [] }
        families.set(family.name, section)
      }
      const ratio =
        typeof option.ratio === 'number'
          ? `${String(option.ratio)}x`
          : option.ratio
      const search = normalizeApiKeyGroupSearch(
        [
          label,
          family.name,
          ...family.aliases,
          option.value,
          option.label,
          option.desc,
          ratio,
        ].join(' ')
      )
      section.entries.push({ option, search })
    }
    return [...families.values()].sort(
      (a, b) =>
        familyRank(a.family) - familyRank(b.family) ||
        a.label.localeCompare(b.label)
    )
  }, [props.options, t])
  const search = normalizeApiKeyGroupSearch(query)
  const selectedFamily = sections.find(({ entries }) =>
    entries.some(
      ({ option }) => option.value === props.selected && !option.disabled
    )
  )?.family.name
  const requestedFamily =
    familyFilter === undefined ? selectedFamily : familyFilter
  const activeFamily =
    !search &&
    requestedFamily &&
    sections.some((s) => s.family.name === requestedFamily)
      ? requestedFamily
      : null
  const visible = useMemo(() => {
    if (!search) {
      return activeFamily
        ? sections.filter((s) => s.family.name === activeFamily)
        : sections
    }
    const words = search.split(' ')
    return sections
      .map((section) => ({
        ...section,
        entries: section.entries.filter((entry) =>
          words.every((word) => entry.search.includes(word))
        ),
      }))
      .filter((s) => s.entries.length)
  }, [activeFamily, search, sections])

  const enabledKeys = useMemo(
    () =>
      visible.flatMap(({ entries }) =>
        entries
          .filter(({ option }) => !option.disabled)
          .map(({ option }) => option.value)
      ),
    [visible]
  )
  const startingKey =
    !search && props.selected && enabledKeys.includes(props.selected)
      ? props.selected
      : enabledKeys[0]
  const activeKey =
    cursor && enabledKeys.includes(cursor) ? cursor : startingKey
  const showingStatus = Boolean(props.statusContent)

  useLayoutEffect(() => {
    const list = listRef.current
    const row = activeRowRef.current
    if (!props.open || !list) return
    // cmdk sets this ID on navigation, but not for an externally supplied value.
    if (!row) {
      inputRef.current?.removeAttribute('aria-activedescendant')
      list.removeAttribute('aria-activedescendant')
      return
    }
    inputRef.current?.setAttribute('aria-activedescendant', row.id)
    list.setAttribute('aria-activedescendant', row.id)
    if (cursor) return
    const revealRow = () => revealInScrollport(list, row)
    revealRow()
    const observer = new ResizeObserver(revealRow)
    observer.observe(list)
    return () => observer.disconnect()
  }, [props.open, activeKey, cursor, showingStatus])

  // The rail switches from a vertical list to a horizontally scrollable strip.
  useLayoutEffect(() => {
    const rail = railRef.current
    const plate = plateRef.current
    const selected = rail?.querySelector<HTMLButtonElement>(
      'button[aria-pressed="true"]'
    )
    if (!props.open || !rail || !plate || !selected) return
    const measure = () => {
      plate.style.setProperty('--kgrp-plate-x', `${selected.offsetLeft}px`)
      plate.style.setProperty('--kgrp-plate-y', `${selected.offsetTop}px`)
      plate.style.setProperty('--kgrp-plate-w', `${selected.offsetWidth}px`)
      plate.style.setProperty('--kgrp-plate-h', `${selected.offsetHeight}px`)
      if (rail.parentElement) revealInScrollport(rail.parentElement, selected)
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(rail)
    ro.observe(selected)
    return () => ro.disconnect()
  }, [activeFamily, sections, props.open, showingStatus])

  const changeFamily = (family: string | null) => {
    setCursor(undefined)
    setFamilyFilter(family)
    setQuery('')
    filterMotion.current?.cancel()
    if (!window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      filterMotion.current = listRef.current?.animate?.(
        [
          { opacity: 0.55, transform: 'translateX(4px)' },
          { opacity: 1, transform: 'translateX(0)' },
        ],
        { duration: 155, easing: 'ease-out' }
      )
    }
  }

  return (
    <Command
      label={t('Search groups')}
      shouldFilter={false}
      loop
      value={activeKey ?? ''}
      onValueChange={setCursor}
      className='kgrp-command'
      onCompositionStart={() => props.onCompositionChange(true)}
      onCompositionEnd={() => props.onCompositionChange(false)}
    >
      <div className='kgrp-findbar'>
        <CommandInput
          ref={inputRef}
          value={query}
          onValueChange={(next) => {
            setCursor(undefined)
            setQuery(next)
          }}
          placeholder={t('Search by provider or group')}
          autoComplete='off'
        />
        {query && (
          <button
            type='button'
            className='kgrp-clear'
            aria-label={t('Clear search')}
            onKeyDown={(event) => {
              if (event.key !== 'Escape') event.stopPropagation()
            }}
            onClick={() => {
              setCursor(undefined)
              setQuery('')
              inputRef.current?.focus()
            }}
          >
            <X size={15} aria-hidden='true' />
          </button>
        )}
        <kbd aria-hidden='true'>esc</kbd>
      </div>
      <div
        className='kgrp-body'
        style={
          props.statusContent || !sections.length
            ? { gridTemplateColumns: 'minmax(0,1fr)' }
            : undefined
        }
      >
        {!props.statusContent && sections.length > 0 && (
          <div className='kgrp-side'>
            <div className='kgrp-side-label'>{t('Provider')}</div>
            <div
              className='kgrp-rail'
              ref={railRef}
              role='group'
              aria-label={t('Provider')}
              onKeyDown={(event) => {
                if (event.key !== 'Escape') event.stopPropagation()
              }}
            >
              <span ref={plateRef} className='kgrp-plate' aria-hidden='true' />
              <button
                type='button'
                data-family='all'
                aria-pressed={!activeFamily}
                onClick={() => changeFamily(null)}
              >
                <ApiKeyGroupBrand size={15} />
                <span>{t('All')}</span>
                <small aria-hidden='true'>{props.options.length}</small>
              </button>
              {sections.map(({ family, label, entries }) => (
                <button
                  key={family.name}
                  type='button'
                  data-family={family.name.toLowerCase()}
                  aria-pressed={activeFamily === family.name}
                  onClick={() => changeFamily(family.name)}
                >
                  <ApiKeyGroupBrand family={family} size={15} />
                  <span>{label}</span>
                  <small aria-hidden='true'>{entries.length}</small>
                </button>
              ))}
            </div>
          </div>
        )}
        <CommandList ref={listRef} label={t('Groups')} className='kgrp-list'>
          {props.statusContent ? (
            <div
              onKeyDown={(event) => {
                if (event.key !== 'Escape') event.stopPropagation()
              }}
            >
              {props.statusContent}
            </div>
          ) : (
            <>
              <CommandEmpty className='kgrp-empty'>
                <Search size={16} aria-hidden='true' />
                {t('No group found.')}
              </CommandEmpty>
              {visible.map(({ family, label, entries }, index) => (
                <CommandGroup
                  key={family.name}
                  value={family.name}
                  className='kgrp-section'
                  heading={
                    <span className='kgrp-heading'>
                      <ApiKeyGroupBrand family={family} size={14} />
                      {label}
                      {index === 0 && (
                        <small aria-hidden='true'>
                          {t('Recent requests · 5-minute success rate')}
                        </small>
                      )}
                    </span>
                  }
                >
                  {entries.map(({ option }) => {
                    const current = option.value === props.selected
                    const description = option.desc?.trim()
                    return (
                      <CommandItem
                        key={option.value}
                        ref={
                          option.value === activeKey ? activeRowRef : undefined
                        }
                        value={option.value}
                        disabled={option.disabled}
                        data-current={current || undefined}
                        data-auto-group-effect={
                          option.isAuto || option.value === 'auto'
                            ? 'option'
                            : undefined
                        }
                        onSelect={() => props.onSelect(option.value)}
                        className='kgrp-row'
                      >
                        <span className='kgrp-row-copy'>
                          <span className='kgrp-row-title' title={option.label}>
                            {option.label}
                          </span>
                          <span className='kgrp-sub'>
                            <ApiKeyGroupRatio ratio={option.ratio} />
                            {description &&
                              description !== option.label.trim() && (
                                <span className='kgrp-desc' title={description}>
                                  {description}
                                </span>
                              )}
                          </span>
                        </span>
                        {option.disabled ? (
                          <span />
                        ) : (
                          <ApiKeyGroupSignal group={option.value} />
                        )}
                        <span className='kgrp-tick'>
                          {current && (
                            <>
                              <Check aria-hidden='true' size={14} />
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
      </div>
      <div className='kgrp-foot'>
        <span className='kgrp-hints' aria-hidden='true'>
          <kbd>↑ ↓</kbd> {t('Browse')} <kbd>↵</kbd> {t('Select')}
        </span>
        <button
          type='button'
          aria-expanded={details}
          aria-controls={detailsId}
          onKeyDown={(event) => {
            if (event.key !== 'Escape') event.stopPropagation()
          }}
          onClick={() => setDetails((current) => !current)}
        >
          <Info aria-hidden='true' size={14} />
          {t('Group status guide')}
        </button>
      </div>
      <p id={detailsId} className='kgrp-details' hidden={!details}>
        {t(
          'Bars show the last 16 requests from left to right. Long green bars are successes, short orange bars are failures, and gray dots indicate missing samples. The success rate is calculated across all requests in the last 5 minutes.'
        )}
      </p>
    </Command>
  )
}

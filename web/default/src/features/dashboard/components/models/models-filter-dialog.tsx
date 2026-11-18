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
import { Filter, RotateCcw, Calendar, Search } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  MAX_DENSE_CHART_BUCKETS,
  TIME_GRANULARITY_OPTIONS,
  TIME_RANGE_PRESETS,
} from '@/features/dashboard/constants'
import {
  buildDefaultDashboardFilters,
  cleanFilters,
} from '@/features/dashboard/lib/filters'
import type {
  DashboardChartPreferences,
  DashboardFilters,
} from '@/features/dashboard/types'
import {
  dateToUnixTimestamp,
  formatDashboardDateTimeInput,
  getChartBucketCount,
  getRollingDateRange,
  parseDashboardDateTimeInput,
  type TimeGranularity,
} from '@/lib/time'

interface ModelsFilterProps {
  preferences: DashboardChartPreferences
  allowUsernameFilter?: boolean
  // The filters currently applied to the dashboard. The dialog edits a copy of
  // these so reopening it never discards a manually picked range.
  currentFilters: DashboardFilters
  onFilterChange: (filters: DashboardFilters) => void
  onReset: () => void
  titleKey?: string
  descriptionKey?: string
}

/**
 * Section divider component for better visual organization
 */
const SectionDivider = ({ label }: { label: string }) => (
  <div className='relative'>
    <div className='absolute inset-0 flex items-center'>
      <span className='w-full border-t' />
    </div>
    <div className='relative flex justify-center text-xs uppercase'>
      <span className='bg-background text-muted-foreground px-2'>{label}</span>
    </div>
  </div>
)

export function ModelsFilter(props: ModelsFilterProps) {
  const { t } = useTranslation()
  const id = useId()

  const [open, setOpen] = useState(false)
  const [filters, setFilters] = useState<DashboardFilters>(
    () =>
      props.currentFilters ?? buildDefaultDashboardFilters(props.preferences)
  )
  const isCustom = filters.presetDays === undefined
  const start =
    filters.start_timestamp && dateToUnixTimestamp(filters.start_timestamp)
  const end =
    filters.end_timestamp && dateToUnixTimestamp(filters.end_timestamp)
  const missingStart = !Number.isFinite(start)
  const missingEnd = !Number.isFinite(end)
  const reversed = start !== undefined && end !== undefined && end < start
  const granularity = filters.time_granularity ?? 'day'
  const chartBucketCount =
    start !== undefined &&
    end !== undefined &&
    !missingStart &&
    !missingEnd &&
    !reversed
      ? getChartBucketCount(start, end, granularity)
      : 0
  const tooManyBuckets = chartBucketCount > MAX_DENSE_CHART_BUCKETS
  let rangeError: string | undefined
  if (isCustom) {
    if (missingStart) rangeError = t('Select start time')
    else if (missingEnd) rangeError = t('Select end time')
    else if (reversed) rangeError = t('End time must be after start time')
    else if (tooManyBuckets) {
      const maxDays =
        granularity === 'hour'
          ? Math.floor((MAX_DENSE_CHART_BUCKETS - 1) / 24)
          : MAX_DENSE_CHART_BUCKETS
      rangeError = t(
        'Choose a time range of up to {{days}} days. The other filters on this page also apply.',
        { days: maxDays }
      )
    }
  }

  const handleOpenChange = (nextOpen: boolean) => {
    // Sync the editing state from the applied filters every time the dialog
    // opens so a previously applied manual range is preserved.
    if (nextOpen) {
      setFilters(
        props.currentFilters ?? buildDefaultDashboardFilters(props.preferences)
      )
    }
    setOpen(nextOpen)
  }

  const handleApply = () => {
    if (rangeError) return

    props.onFilterChange(
      cleanFilters({
        ...filters,
        username: props.allowUsernameFilter ? filters.username : undefined,
      })
    )
    setOpen(false)
  }

  const handleReset = () => {
    props.onReset()
    setOpen(false)
  }

  const updateFilters = (changes: Partial<DashboardFilters>) => {
    setFilters((previous) => ({ ...previous, ...changes }))
  }

  const handleQuickRange = (days: number) => {
    const { start, end } = getRollingDateRange(days)

    updateFilters({
      presetDays: days,
      start_timestamp: start,
      end_timestamp: end,
      time_granularity: days === 1 ? 'hour' : 'day',
    })
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      trigger={
        <Button variant='outline' size='sm'>
          <Filter className='mr-2 h-4 w-4' />
          {t('Filter')}
        </Button>
      }
      title={t(props.titleKey ?? 'Model Analytics Filters')}
      description={t(
        props.descriptionKey ??
          'Filter the model analytics view by time range and user.'
      )}
      contentClassName='max-sm:h-dvh max-sm:w-screen max-sm:max-w-none max-sm:rounded-none max-sm:p-4 sm:max-w-lg'
      contentHeight='min(48vh, 460px)'
      footerClassName='grid grid-cols-2 gap-2 sm:flex'
      footer={
        <>
          <Button onClick={handleReset} variant='outline' type='button'>
            <RotateCcw className='mr-2 h-4 w-4' />
            {t('Reset')}
          </Button>
          <Button
            onClick={handleApply}
            type='button'
            disabled={Boolean(rangeError)}
          >
            <Search className='mr-2 h-4 w-4' />
            {t('Apply Filters')}
          </Button>
        </>
      }
    >
      <div className='grid gap-2.5 py-2'>
        <fieldset className='min-w-0'>
          <legend className='mb-2 flex items-center gap-2 text-sm font-medium'>
            <Calendar className='h-4 w-4' />
            {t('Quick Range')}
          </legend>
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
            {TIME_RANGE_PRESETS.map((range) => (
              <Button
                key={range.days}
                type='button'
                size='sm'
                variant={
                  filters.presetDays === range.days ? 'default' : 'outline'
                }
                aria-pressed={filters.presetDays === range.days}
                onClick={() => handleQuickRange(range.days)}
              >
                {t(range.label)}
              </Button>
            ))}
            <Button
              type='button'
              size='sm'
              variant={isCustom ? 'default' : 'outline'}
              aria-pressed={isCustom}
              onClick={() => updateFilters({ presetDays: undefined })}
            >
              {t('Custom')}
            </Button>
          </div>
        </fieldset>

        {isCustom && (
          <div className='grid gap-2.5'>
            <SectionDivider label={t('Custom Time Range')} />
            <p id={`${id}-zone`} className='text-muted-foreground text-xs'>
              {t('Timezone')} · UTC+8
            </p>
            {(['start_timestamp', 'end_timestamp'] as const).map((field) => {
              const isStart = field === 'start_timestamp'
              const label = isStart ? t('Start Time') : t('End Time')
              const invalid = isStart ? missingStart : missingEnd || reversed
              return (
                <div key={field} className='grid gap-2'>
                  <Label htmlFor={`${id}-${field}`}>{label}</Label>
                  <Input
                    id={`${id}-${field}`}
                    type='datetime-local'
                    step={1}
                    required
                    aria-label={label}
                    aria-invalid={invalid}
                    aria-describedby={
                      invalid ? `${id}-zone ${id}-error` : `${id}-zone`
                    }
                    value={formatDashboardDateTimeInput(filters[field])}
                    onChange={(event) =>
                      updateFilters({
                        [field]: parseDashboardDateTimeInput(
                          event.currentTarget.value.replace(/\.0+$/, '')
                        ),
                      })
                    }
                  />
                </div>
              )
            })}
            {rangeError && (
              <p
                id={`${id}-error`}
                className='text-destructive text-sm'
                role='alert'
              >
                {rangeError}
              </p>
            )}
          </div>
        )}

        <SectionDivider label={t('Chart Settings')} />

        <div className='grid gap-2'>
          <Label htmlFor={`${id}-granularity`}>{t('Time Granularity')}</Label>
          <Select
            items={TIME_GRANULARITY_OPTIONS.map((option) => ({
              value: option.value,
              label: t(option.label),
            }))}
            value={filters.time_granularity}
            onValueChange={(value) =>
              updateFilters({ time_granularity: value as TimeGranularity })
            }
          >
            <SelectTrigger
              id={`${id}-granularity`}
              aria-label={t('Time Granularity')}
            >
              <SelectValue placeholder={t('Select time granularity')} />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {TIME_GRANULARITY_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {t(option.label)}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>

        {/* Admin-only fields */}
        {props.allowUsernameFilter && (
          <>
            <SectionDivider label={t('Admin Only')} />

            <div className='grid gap-2'>
              <Label htmlFor={`${id}-username`}>{t('Username')}</Label>
              <Input
                id={`${id}-username`}
                placeholder={t('Filter by username')}
                value={filters.username ?? ''}
                onChange={(event) =>
                  updateFilters({ username: event.target.value })
                }
              />
            </div>
          </>
        )}
      </div>
    </Dialog>
  )
}

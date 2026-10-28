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
import { ChevronDown, SlidersHorizontal } from 'lucide-react'
import { useId, useState } from 'react'
import { Controller, useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Slider } from '@/components/ui/slider'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import {
  getVideoModelPreset,
  VIDEO_PARAMETER_LABEL_KEYS,
  VIDEO_PARAMETER_OPTION_LABEL_KEYS,
} from '../schemas'
import type {
  VideoGenerationMode,
  VideoModelProfile,
  VideoNumericParameter,
  VideoParameters,
} from '../types'
import {
  decodeVideoParameterOptionValue,
  encodeVideoParameterOptionValue,
  videoParameterAcceptsValue,
} from '../video-domain'

type VideoParameterFieldsProps = {
  profile: VideoModelProfile
}

type VideoParameterFormValues = {
  mode: VideoGenerationMode
  parameters: VideoParameters
}

export function VideoParameterFields(props: VideoParameterFieldsProps) {
  const { t } = useTranslation()
  const fieldIdPrefix = useId()
  const form = useFormContext<VideoParameterFormValues>()
  const mode = useWatch({ control: form.control, name: 'mode' })
  const values = useWatch({ control: form.control, name: 'parameters' })
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const usesPresetLabels = Boolean(getVideoModelPreset(props.profile.model))
  const parameters = props.profile.specification.parameters.filter(
    (parameter) => !parameter.modes || parameter.modes.includes(mode)
  )

  const commonKeys = new Set([
    'aspect_ratio',
    'ratio',
    'resolution',
    'duration',
    'seconds',
  ])
  const common = parameters.filter((parameter) => commonKeys.has(parameter.key))
  const advanced = parameters.filter(
    (parameter) => !commonKeys.has(parameter.key)
  )
  const advancedInvalid = advanced.some((parameter) => {
    const value = values?.[parameter.key]
    if (value === undefined) return Boolean(parameter.required)
    return !videoParameterAcceptsValue(parameter, value)
  })
  const renderFields = (fields: typeof parameters) =>
    fields.map((parameter) => {
      const isRatio =
        parameter.key === 'aspect_ratio' || parameter.key === 'ratio'
      const value = values?.[parameter.key]
      let errorKey: string | undefined
      if ((value === undefined || value === '') && parameter.required) {
        errorKey = 'videoStudio.validation.parameterRequired'
      } else if (
        value !== undefined &&
        !videoParameterAcceptsValue(parameter, value)
      ) {
        errorKey =
          parameter.control === 'number' || parameter.control === 'slider'
            ? 'videoStudio.validation.parameterOutOfRange'
            : 'videoStudio.validation.parameterInvalid'
      }
      const name = `parameters.${parameter.key}` as const
      const inputId = `${fieldIdPrefix}-video-parameter-${parameter.key}`
      const parameterLabelKey = usesPresetLabels
        ? VIDEO_PARAMETER_LABEL_KEYS[parameter.key]
        : undefined
      const parameterLabel = parameterLabelKey
        ? t(parameterLabelKey, { defaultValue: parameter.label })
        : parameter.label
      const optionLabel = (value: string | number | boolean, label: string) => {
        const fallback = label || String(value)
        const labelKey = usesPresetLabels
          ? VIDEO_PARAMETER_OPTION_LABEL_KEYS[parameter.key]?.[String(value)]
          : undefined
        return labelKey ? t(labelKey, { defaultValue: fallback }) : fallback
      }

      return (
        <div key={parameter.key} className='space-y-2'>
          <Controller
            control={form.control}
            name={name}
            render={({ field }) => {
              if (parameter.control === 'switch') {
                return (
                  <div className='video-studio-switch-row flex items-center justify-between gap-3'>
                    <div className='min-w-0'>
                      <Label htmlFor={inputId}>{parameterLabel}</Label>
                    </div>
                    <Switch
                      id={inputId}
                      aria-invalid={Boolean(errorKey)}
                      aria-describedby={
                        errorKey ? `${inputId}-error` : undefined
                      }
                      checked={Boolean(field.value)}
                      onCheckedChange={field.onChange}
                    />
                  </div>
                )
              }

              if (
                parameter.control === 'segmented' ||
                (parameter.control === 'select' && isRatio)
              ) {
                return (
                  <div className='space-y-2'>
                    <Label id={`${inputId}-label`}>{parameterLabel}</Label>
                    <ToggleGroup
                      aria-labelledby={`${inputId}-label`}
                      aria-invalid={Boolean(errorKey)}
                      aria-describedby={
                        errorKey ? `${inputId}-error` : undefined
                      }
                      value={
                        field.value === undefined
                          ? []
                          : [encodeVideoParameterOptionValue(field.value)]
                      }
                      onValueChange={(values) => {
                        const next = values.at(0)
                        if (next !== undefined) {
                          const optionValue = decodeVideoParameterOptionValue(
                            parameter.options,
                            next
                          )
                          if (optionValue !== undefined) {
                            field.onChange(optionValue)
                          }
                        }
                      }}
                      variant='outline'
                      className={
                        isRatio
                          ? 'video-studio-ratio-options'
                          : 'video-studio-parameter-options'
                      }
                    >
                      {parameter.options.map((option) => (
                        <ToggleGroupItem
                          key={encodeVideoParameterOptionValue(option.value)}
                          value={encodeVideoParameterOptionValue(option.value)}
                          className='min-w-0 px-2'
                        >
                          {isRatio &&
                            /^\d+:\d+$/.test(String(option.value)) && (
                              <span
                                className='video-studio-ratio-shape'
                                aria-hidden='true'
                                style={{
                                  aspectRatio: String(option.value).replace(
                                    ':',
                                    '/'
                                  ),
                                }}
                              />
                            )}
                          <span>{optionLabel(option.value, option.label)}</span>
                        </ToggleGroupItem>
                      ))}
                    </ToggleGroup>
                  </div>
                )
              }

              if (parameter.control === 'select') {
                return (
                  <div className='space-y-2'>
                    <Label htmlFor={inputId}>{parameterLabel}</Label>
                    <NativeSelect
                      id={inputId}
                      aria-invalid={Boolean(errorKey)}
                      aria-describedby={
                        errorKey ? `${inputId}-error` : undefined
                      }
                      className='w-full'
                      value={
                        field.value === undefined
                          ? ''
                          : encodeVideoParameterOptionValue(field.value)
                      }
                      onChange={(event) => {
                        const optionValue = decodeVideoParameterOptionValue(
                          parameter.options,
                          event.target.value
                        )
                        if (optionValue !== undefined) {
                          field.onChange(optionValue)
                        }
                      }}
                    >
                      {parameter.options.map((option) => (
                        <NativeSelectOption
                          key={encodeVideoParameterOptionValue(option.value)}
                          value={encodeVideoParameterOptionValue(option.value)}
                        >
                          {optionLabel(option.value, option.label)}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </div>
                )
              }

              const numericParameter = parameter as VideoNumericParameter
              const numericValue =
                typeof field.value === 'number'
                  ? field.value
                  : (numericParameter.default ?? numericParameter.min)
              if (parameter.control === 'number') {
                return (
                  <div className='space-y-2'>
                    <Label htmlFor={inputId}>{parameterLabel}</Label>
                    <div className='flex items-center gap-2'>
                      <Input
                        id={inputId}
                        aria-invalid={Boolean(errorKey)}
                        aria-describedby={
                          errorKey ? `${inputId}-error` : undefined
                        }
                        type='number'
                        min={numericParameter.min}
                        max={numericParameter.max}
                        step={numericParameter.step}
                        value={
                          typeof field.value === 'number' ? field.value : ''
                        }
                        onChange={(event) => {
                          field.onChange(
                            event.target.value === ''
                              ? ''
                              : event.target.valueAsNumber
                          )
                        }}
                      />
                    </div>
                  </div>
                )
              }

              return (
                <div className='space-y-2'>
                  <div className='flex items-center justify-between gap-2'>
                    <Label htmlFor={inputId}>{parameterLabel}</Label>
                    <span className='text-muted-foreground text-xs tabular-nums'>
                      {numericValue}
                    </span>
                  </div>
                  <Slider
                    id={inputId}
                    aria-invalid={Boolean(errorKey)}
                    aria-describedby={errorKey ? `${inputId}-error` : undefined}
                    min={numericParameter.min}
                    max={numericParameter.max}
                    step={numericParameter.step}
                    value={numericValue}
                    onValueChange={(value) =>
                      field.onChange(Array.isArray(value) ? value[0] : value)
                    }
                    aria-label={parameterLabel}
                  />
                </div>
              )
            }}
          />
          {errorKey && (
            <p
              id={`${inputId}-error`}
              role='alert'
              className='text-destructive text-xs'
            >
              {t(errorKey)}
            </p>
          )}
        </div>
      )
    })
  return (
    <div className='video-studio-parameters'>
      <div className='video-studio-common-parameters'>
        {renderFields(common)}
      </div>
      {advanced.length > 0 && (
        <div className='video-studio-advanced'>
          <button
            type='button'
            aria-expanded={advancedOpen || advancedInvalid}
            aria-controls={`${fieldIdPrefix}-advanced`}
            onClick={() => setAdvancedOpen(!advancedOpen)}
          >
            <span>
              <SlidersHorizontal aria-hidden='true' />
              {t('Advanced settings')}
            </span>
            <ChevronDown aria-hidden='true' />
          </button>
          <div
            id={`${fieldIdPrefix}-advanced`}
            className='video-studio-advanced-fields'
            hidden={!advancedOpen && !advancedInvalid}
          >
            {renderFields(advanced)}
          </div>
        </div>
      )}
    </div>
  )
}

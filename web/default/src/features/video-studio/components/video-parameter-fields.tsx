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
import { useId } from 'react'
import { Controller, useFormContext, useWatch } from 'react-hook-form'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Slider } from '@/components/ui/slider'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import type {
  VideoGenerationMode,
  VideoModelProfile,
  VideoNumericParameter,
  VideoParameters,
} from '../types'
import {
  decodeVideoParameterOptionValue,
  encodeVideoParameterOptionValue,
} from '../video-domain'

type VideoParameterFieldsProps = {
  profile: VideoModelProfile
}

type VideoParameterFormValues = {
  mode: VideoGenerationMode
  parameters: VideoParameters
}

export function VideoParameterFields(props: VideoParameterFieldsProps) {
  const fieldIdPrefix = useId()
  const form = useFormContext<VideoParameterFormValues>()
  const mode = useWatch({ control: form.control, name: 'mode' })
  const parameters = props.profile.specification.parameters.filter(
    (parameter) => !parameter.modes || parameter.modes.includes(mode)
  )

  return (
    <div className='grid gap-4'>
      {parameters.map((parameter, index) => {
        const name = `parameters.${parameter.key}` as const
        const inputId = `${fieldIdPrefix}-video-parameter-${index}`

        return (
          <Controller
            key={parameter.key}
            control={form.control}
            name={name}
            render={({ field }) => {
              if (parameter.control === 'switch') {
                return (
                  <div className='flex items-start justify-between gap-3'>
                    <div className='min-w-0'>
                      <Label htmlFor={inputId}>{parameter.label}</Label>
                    </div>
                    <Switch
                      id={inputId}
                      checked={Boolean(field.value)}
                      onCheckedChange={field.onChange}
                    />
                  </div>
                )
              }

              if (parameter.control === 'segmented') {
                return (
                  <div className='space-y-2'>
                    <Label id={`${inputId}-label`}>{parameter.label}</Label>
                    <ToggleGroup
                      aria-labelledby={`${inputId}-label`}
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
                      className='grid w-full auto-cols-fr grid-flow-col'
                    >
                      {parameter.options.map((option) => (
                        <ToggleGroupItem
                          key={encodeVideoParameterOptionValue(option.value)}
                          value={encodeVideoParameterOptionValue(option.value)}
                          className='min-w-0 px-2'
                        >
                          <span className='truncate'>{option.label}</span>
                        </ToggleGroupItem>
                      ))}
                    </ToggleGroup>
                  </div>
                )
              }

              if (parameter.control === 'select') {
                return (
                  <div className='space-y-2'>
                    <Label htmlFor={inputId}>{parameter.label}</Label>
                    <NativeSelect
                      id={inputId}
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
                          {option.label}
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
                    <Label htmlFor={inputId}>{parameter.label}</Label>
                    <div className='flex items-center gap-2'>
                      <Input
                        id={inputId}
                        type='number'
                        min={numericParameter.min}
                        max={numericParameter.max}
                        step={numericParameter.step}
                        value={numericValue}
                        onChange={(event) => {
                          if (event.target.value !== '') {
                            field.onChange(event.target.valueAsNumber)
                          }
                        }}
                      />
                    </div>
                  </div>
                )
              }

              return (
                <div className='space-y-2'>
                  <div className='flex items-center justify-between gap-2'>
                    <Label htmlFor={inputId}>{parameter.label}</Label>
                    <span className='text-muted-foreground text-xs tabular-nums'>
                      {numericValue}
                    </span>
                  </div>
                  <Slider
                    id={inputId}
                    min={numericParameter.min}
                    max={numericParameter.max}
                    step={numericParameter.step}
                    value={[numericValue]}
                    onValueChange={(value) =>
                      field.onChange(Array.isArray(value) ? value[0] : value)
                    }
                    aria-label={parameter.label}
                  />
                </div>
              )
            }}
          />
        )
      })}
    </div>
  )
}

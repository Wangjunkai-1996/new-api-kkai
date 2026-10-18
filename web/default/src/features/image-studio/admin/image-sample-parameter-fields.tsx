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
import { Controller, type Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'

import type { ImageModelProfile } from '../types'
import type { ImageSampleFormValues } from './image-admin-forms'

export function ImageSampleParameterFields(props: {
  control: Control<ImageSampleFormValues>
  profile: ImageModelProfile
}) {
  const { t } = useTranslation()
  return (
    <div className='grid gap-4 sm:grid-cols-2'>
      {props.profile.specification.parameters.map((parameter) => (
        <Controller
          key={parameter.key}
          control={props.control}
          name={`parameters.${parameter.key}`}
          render={({ field }) => {
            const id = `sample-parameter-${parameter.key}`
            if (parameter.control === 'boolean') {
              return (
                <div className='flex min-h-10 items-center justify-between rounded-md border px-3'>
                  <Label htmlFor={id}>{parameter.label}</Label>
                  <Switch
                    id={id}
                    checked={field.value === true}
                    onCheckedChange={field.onChange}
                  />
                </div>
              )
            }
            if (parameter.control === 'select') {
              const selectedPreset =
                typeof field.value === 'string' &&
                parameter.options.some((option) => option.value === field.value)
                  ? field.value
                  : ''
              return (
                <div className='space-y-1.5'>
                  <Label htmlFor={id}>{parameter.label}</Label>
                  <NativeSelect
                    id={id}
                    value={selectedPreset}
                    onChange={(event) => field.onChange(event.target.value)}
                  >
                    <NativeSelectOption value=''>
                      {t('imageStudio.parameter.select')}
                    </NativeSelectOption>
                    {parameter.options.map((option) => (
                      <NativeSelectOption
                        key={option.value}
                        value={option.value}
                      >
                        {option.label}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                  {parameter.request_key === 'size' &&
                    props.profile.specification.custom_size && (
                      <Input
                        type='text'
                        value={
                          typeof field.value === 'string' &&
                          !parameter.options.some(
                            (option) => option.value === field.value
                          )
                            ? field.value
                            : ''
                        }
                        placeholder={t(
                          'imageStudio.parameter.customSizePlaceholder'
                        )}
                        aria-label={t('imageStudio.parameter.customSize')}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    )}
                </div>
              )
            }
            return (
              <div className='space-y-1.5'>
                <Label htmlFor={id}>{parameter.label}</Label>
                <Input
                  id={id}
                  type='number'
                  min={parameter.min}
                  max={parameter.max}
                  value={typeof field.value === 'number' ? field.value : ''}
                  onChange={(event) =>
                    field.onChange(Number(event.target.value))
                  }
                />
              </div>
            )
          }}
        />
      ))}
    </div>
  )
}
